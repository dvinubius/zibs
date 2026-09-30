#!/usr/bin/env bash

set -euo pipefail

classifier="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)/classify-deploy.sh"
repo=$(mktemp -d)
trap 'rm -rf "$repo"' EXIT

cd "$repo"
git init -q -b main
git config user.email test@example.invalid
git config user.name test
git config commit.gpgsign false

write() {
	mkdir -p "$(dirname -- "$1")"
	printf '%s\n' "${2:-$RANDOM}" >"$1"
}

commit() {
	git add -A
	git commit -q --allow-empty -m change
	git rev-parse HEAD
}

write cmd/zibs/main.go
write compose.yaml
write README.md
write docs/runbook.md
write prometheus.yml
write loki.yml
write config.alloy
write grafana/dashboards/operator.json
write grafana/dashboards/public-metrics.json
write grafana/dashboards/traffic-synthetic.json
write grafana/dashboards/traffic-suspected-scan.json
write grafana/dashboards/traffic-other.json
write grafana/provisioning/dashboards/dashboards.yaml
write scripts/remote-deploy.sh
write cmd/zibs/web/index.html
base=$(commit)

failures=0
expect() {
	local description=$1 expected=$2 deployed=$3 target=$4 actual
	actual=$(bash "$classifier" "$deployed" "$target" 2>/dev/null) || actual="exit $?"
	if [[ $actual != "$expected" ]]; then
		printf 'FAIL: %s: expected %s, got %s\n' "$description" "$expected" "$actual" >&2
		failures=$((failures + 1))
	fi
}

# Each case starts from the same verified deployment on its own branch.
case_from_base() {
	git checkout -q --detach "$base"
}

case_from_base
write README.md
write docs/runbook.md
write .agents/PROGRESS.md
write cmd/zibs/main_test.go
write scripts/remote-deploy_test.sh
write scripts/apply-grafana-v2-layout.sh
write LICENSE
expect 'docs and tests only' none "$base" "$(commit)"

case_from_base
write grafana/dashboards/operator.json
write README.md
expect 'dashboard with docs' dashboard "$base" "$(commit)"

case_from_base
write grafana/dashboards/operator.json
write grafana/dashboards/public-metrics.json
expect 'both dashboards' dashboard "$base" "$(commit)"

case_from_base
write grafana/dashboards/new.json
expect 'new dashboard file is observability' observability "$base" "$(commit)"

case_from_base
write prometheus.yml
write docs/runbook.md
expect 'observability with docs' observability "$base" "$(commit)"

case_from_base
write grafana/provisioning/dashboards/dashboards.yaml
expect 'grafana provisioning is observability' observability "$base" "$(commit)"

case_from_base
write config.alloy
write grafana/dashboards/operator.json
expect 'dashboard and observability' full "$base" "$(commit)"

case_from_base
write compose.yaml
expect 'compose only' full "$base" "$(commit)"

case_from_base
write compose.yaml
write grafana/dashboards/operator.json
expect 'compose wins over dashboard' full "$base" "$(commit)"

case_from_base
write cmd/zibs/main.go
expect 'application change' full "$base" "$(commit)"

case_from_base
write scripts/remote-deploy.sh
expect 'deployment script change' full "$base" "$(commit)"

case_from_base
write cmd/zibs/web/index.html
expect 'frontend change' full "$base" "$(commit)"

case_from_base
write Dockerfile
expect 'Dockerfile change' full "$base" "$(commit)"

case_from_base
write tools/new-operational-file.sh
expect 'unknown path' full "$base" "$(commit)"

case_from_base
write docs/nested/main_test.go
write nested/main_test.go
write main_test.go
write cmd/zibs/web/main_test.go
expect 'test outside the application package is not assumed harmless' full "$base" "$(commit)"

case_from_base
git rm -q loki.yml
expect 'deleted observability file' observability "$base" "$(commit)"

case_from_base
git rm -q cmd/zibs/main.go
expect 'deleted application file' full "$base" "$(commit)"

case_from_base
git mv prometheus.yml docs/prometheus.yml
expect 'rename out of observability counts the old path' observability "$base" "$(commit)"

case_from_base
git mv docs/runbook.md scripts/runbook.sh
expect 'rename into an operational path counts the new path' full "$base" "$(commit)"

# A failed deployment leaves the manifest at its old SHA, so the next run sees
# both commits: a dashboard change followed by a docs-only push stays dashboard.
case_from_base
write grafana/dashboards/public-metrics.json
commit >/dev/null
write README.md
expect 'cumulative after failed dashboard deploy' dashboard "$base" "$(commit)"

case_from_base
write grafana/dashboards/public-metrics.json
commit >/dev/null
write cmd/zibs/main.go
expect 'cumulative dashboard then app' full "$base" "$(commit)"

case_from_base
write grafana/dashboards/traffic-synthetic.json
expect 'new private dashboard is dashboard mode' dashboard "$base" "$(commit)"

case_from_base
expect 'target equals deployment' none "$base" "$base"

target=$(git rev-parse HEAD)
expect 'first deployment' full '' "$target"
expect 'malformed baseline' full 'not-a-sha' "$target"
expect 'unknown baseline' full "$(printf '%040d' 0)" "$target"

git checkout -q --orphan unrelated
git rm -q -r --cached . >/dev/null
write README.md
unrelated=$(commit)
expect 'baseline is not an ancestor' full "$unrelated" "$target"

expect 'unknown target stops' 'exit 2' "$base" "$(printf '%040d' 1)"
expect 'abbreviated target stops' 'exit 2' "$base" "${target:0:12}"

if ((failures > 0)); then
	exit 1
fi
printf '%s\n' 'deployment classifier tests passed'
