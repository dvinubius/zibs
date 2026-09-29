#!/usr/bin/env bash

set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT

bin=$temporary_dir/bin
repo=$temporary_dir/repo
ssh_log=$temporary_dir/ssh.log
mkdir -p "$bin" "$repo/scripts" "$repo/grafana/dashboards"

# The mock records arguments and stdin; an uploaded bundle is listed instead.
cat >"$bin/ssh" <<'EOF'
#!/usr/bin/env bash
printf 'ARGS %s\n' "$*" >>"$CI_TEST_SSH_LOG"
[[ -z ${CI_TEST_SSH_FAIL:-} ]] || exit 255
command=${!#}
if [[ $command == *'tar -x'* ]]; then
	tar -t | sed 's/^/BUNDLE /' >>"$CI_TEST_SSH_LOG"
elif [[ $command == *remote-deploy.sh* ]]; then
	sed 's/^/STDIN /' >>"$CI_TEST_SSH_LOG"
elif [[ $command == *manifest* ]]; then
	cat "$CI_TEST_MANIFEST" 2>/dev/null || true
fi
EOF
chmod +x "$bin/ssh"

cp "$project_dir/scripts/ci-deploy.sh" "$project_dir/scripts/classify-deploy.sh" \
	"$project_dir/scripts/remote-deploy.sh" "$project_dir/scripts/compose.sh" \
	"$project_dir/scripts/apply-grafana-v2-layout.sh" "$repo/scripts/"
cp "$project_dir/grafana/dashboards/operator.json" "$project_dir/grafana/dashboards/public-metrics.json" \
	"$project_dir/grafana/dashboards/traffic-synthetic.json" \
	"$project_dir/grafana/dashboards/traffic-suspected-scan.json" \
	"$project_dir/grafana/dashboards/traffic-other.json" \
	"$repo/grafana/dashboards/"
touch "$repo/compose.yaml" "$repo/prometheus.yml" "$repo/loki.yml" "$repo/config.alloy" \
	"$repo/main.go" "$repo/README.md" "$repo/scripts/remote-deploy_test.sh"
printf 'key\n' >"$temporary_dir/key"
printf 'host key\n' >"$temporary_dir/known_hosts"

git init -q --bare "$temporary_dir/origin.git"
cd "$repo"
git init -q -b main
git config user.email test@example.invalid
git config user.name test
git config commit.gpgsign false
git remote add origin "$temporary_dir/origin.git"
git add -A
git commit -q -m base
base=$(git rev-parse HEAD)
printf 'docs\n' >>README.md
git commit -q -am docs
head=$(git rev-parse HEAD)
git push -q origin main

image_repository=ghcr.io/example/zibs
image=$image_repository@sha256:$(printf '3%.0s' {1..64})

ci() {
	PATH="$bin:$PATH" CI_TEST_SSH_LOG="$ssh_log" CI_TEST_MANIFEST="$temporary_dir/manifest" \
		DEPLOY_HOST=vps.example DEPLOY_USER=zibs-deploy \
		DEPLOY_SSH_KEY_FILE="$temporary_dir/key" DEPLOY_KNOWN_HOSTS_FILE="$temporary_dir/known_hosts" \
		ZIBS_IMAGE_REPOSITORY=$image_repository \
		bash scripts/ci-deploy.sh "$@"
}

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

reset() {
	rm -f "$ssh_log" "$temporary_dir/manifest"
}

expect_no_ssh() {
	[[ ! -e $ssh_log ]] || fail "$1 opened SSH"
}

reset
[[ $(ci plan "$head" 2>/dev/null) == full ]] || fail 'first deployment without a manifest is not full'
grep -q 'StrictHostKeyChecking=yes' "$ssh_log" || fail 'SSH does not use strict host-key checking'
grep -q "UserKnownHostsFile=$temporary_dir/known_hosts" "$ssh_log" || fail 'SSH ignores the verified known-hosts file'

reset
printf 'commit=%s\nimage=%s\n' "$base" "$image" >"$temporary_dir/manifest"
[[ $(ci plan "$head" 2>/dev/null) == none ]] || fail 'docs-only change since the manifest is not none'
[[ $(ci plan "$head" full 2>/dev/null) == full ]] || fail 'manual full deployment was not honored'

reset
if output=$(CI_TEST_SSH_FAIL=1 ci plan "$head" 2>/dev/null); then
	fail 'plan continued after an SSH failure'
fi
[[ -z $output ]] || fail "plan printed a mode after an SSH failure: $output"

reset
ci deploy dashboard "$base" >/dev/null 2>&1 && fail 'deployed a commit that is not the head of main'
expect_no_ssh 'stale target'

reset
ci deploy full "$head" "ghcr.io/other/zibs@sha256:$(printf '3%.0s' {1..64})" >/dev/null 2>&1 &&
	fail 'accepted an image from another repository'
ci deploy full "$head" "$image_repository:latest" >/dev/null 2>&1 && fail 'accepted a mutable tag'
expect_no_ssh 'invalid image'

reset
printf '{"uid":"wrong","panels":[]}\n' >grafana/dashboards/operator.json
ci deploy dashboard "$head" >/dev/null 2>&1 && fail 'accepted an invalid dashboard'
expect_no_ssh 'invalid dashboard'
git checkout -q -- grafana

reset
printf '{"apiVersion":"dashboard.grafana.app/v2","uid":"zibs-public-metrics","panels":[]}\n' \
	>grafana/dashboards/public-metrics.json
ci deploy dashboard "$head" >/dev/null 2>&1 && fail 'accepted a V2 dashboard'
expect_no_ssh 'V2 dashboard'
git checkout -q -- grafana

reset
GHCR_USER=github-actions GHCR_PULL_TOKEN=secret-token ci deploy full "$head" "$image" >/dev/null
grep -q '^BUNDLE compose.yaml$' "$ssh_log" || fail 'bundle is missing compose.yaml'
for entry in prometheus.yml loki.yml config.alloy grafana/dashboards/operator.json grafana/dashboards/public-metrics.json grafana/dashboards/traffic-synthetic.json grafana/dashboards/traffic-suspected-scan.json grafana/dashboards/traffic-other.json; do
	grep -q "^BUNDLE $entry\$" "$ssh_log" || fail "bundle is missing $entry"
done
grep -q '^BUNDLE scripts/remote-deploy.sh$' "$ssh_log" || fail 'bundle is missing the remote deploy script'
! grep -qE '^BUNDLE (main\.go|README\.md|scripts/.*_test\.sh|scripts/ci-deploy\.sh|scripts/classify-deploy\.sh|scripts/apply-grafana-v2-layout\.sh)$' "$ssh_log" ||
	fail 'bundle contains files the VPS does not need'
grep -q "umask 022 && rm -rf" "$ssh_log" || fail 'bundle extraction does not force a world-readable umask'
grep -q "remote-deploy.sh' full $head $image" "$ssh_log" || fail 'full deployment did not pass the exact image digest'
grep -q '^STDIN secret-token$' "$ssh_log" || fail 'registry token did not arrive on stdin'
! grep -q '^ARGS .*secret-token' "$ssh_log" || fail 'registry token appeared in SSH arguments'
! grep -qE '^ARGS .*(git |docker build|compose build)' "$ssh_log" || fail 'deployment ran Git or a build on the VPS'

reset
GHCR_USER=github-actions GHCR_PULL_TOKEN=secret-token ci deploy dashboard "$head" >/dev/null
! grep -q secret-token "$ssh_log" || fail 'dashboard deployment sent the registry token'
grep -q "remote-deploy.sh' dashboard $head ;" "$ssh_log" || fail 'dashboard deployment did not run the remote script'

[[ $(ci validate) == 'Dashboard JSON is valid.' ]] || fail 'validate rejected the tracked dashboard'
printf '%s\n' 'CI deploy script tests passed'
