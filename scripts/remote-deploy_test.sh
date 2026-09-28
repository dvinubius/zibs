#!/usr/bin/env bash

set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
temporary_dir=$(mktemp -d)
trap 'rm -rf "$temporary_dir"' EXIT

bin=$temporary_dir/bin
bundle=$temporary_dir/bundle
live=$temporary_dir/live
log=$temporary_dir/commands.log
mkdir -p "$bin"

commit=$(printf 'a%.0s' {1..40})
old_image=ghcr.io/example/zibs@sha256:$(printf '1%.0s' {1..64})
new_image=ghcr.io/example/zibs@sha256:$(printf '2%.0s' {1..64})

cat >"$bin/docker" <<'EOF'
#!/usr/bin/env bash
printf 'docker %s\n' "$*" >>"$REMOTE_TEST_LOG"
case " $* " in
*' login '*)
	read -r token
	printf 'login token=%s\n' "$token" >>"$REMOTE_TEST_LOG"
	;;
' ps -q '*)
	last=${!#}
	service=${last##*service=}
	[[ " ${REMOTE_TEST_STOPPED:-} " == *" $service "* ]] || printf 'cid-%s\n' "$service"
	;;
*' inspect --format '*) printf '%s\n' sha256:host-built ;;
esac
exit 0
EOF

# The candidate fails its health check only while the bad image is installed,
# so the restored deployment can verify successfully.
cat >"$bin/curl" <<'EOF'
#!/usr/bin/env bash
printf 'curl %s\n' "$*" >>"$REMOTE_TEST_LOG"
if [[ -n ${REMOTE_TEST_BAD_IMAGE:-} && $* == *127.0.0.1:8080/health* ]] &&
	grep -qF "$REMOTE_TEST_BAD_IMAGE" "$ZIBS_DIR/.env.image" 2>/dev/null; then
	exit 22
fi
exit 0
EOF

cat >"$bin/date" <<'EOF'
#!/usr/bin/env bash
count=$(($(cat "$REMOTE_TEST_DATE" 2>/dev/null || echo 0) + 1))
printf '%s\n' "$count" >"$REMOTE_TEST_DATE"
if [[ $* == *%Y%m%dT%H%M%SZ* ]]; then
	printf '20260925T1200%02dZ\n' "$count"
else
	printf '2026-09-25T12:00:%02dZ\n' "$count"
fi
EOF

printf '#!/usr/bin/env bash\nexit 0\n' >"$bin/flock"
printf '#!/usr/bin/env bash\nexit 0\n' >"$bin/sleep"
chmod +x "$bin"/*

make_bundle() {
	rm -rf "$bundle"
	mkdir -p "$bundle/scripts" "$bundle/grafana/dashboards" "$bundle/grafana/provisioning"
	printf 'name: zibs # new\n' >"$bundle/compose.yaml"
	printf 'new operator\n' >"$bundle/grafana/dashboards/operator.json"
	printf 'new public\n' >"$bundle/grafana/dashboards/public-metrics.json"
	printf 'new prometheus\n' >"$bundle/prometheus.yml"
	touch "$bundle/loki.yml" "$bundle/config.alloy"
	cp "$project_dir/scripts/remote-deploy.sh" "$project_dir/scripts/compose.sh" \
		"$project_dir/scripts/backup.sh" "$bundle/scripts/"
	# Telemetry fails while a configuration marked "bad" is installed.
	cat >"$bundle/scripts/telemetry-smoke-test.sh" <<'EOF'
#!/usr/bin/env bash
printf 'smoke %s\n' "$*" >>"$REMOTE_TEST_LOG"
! grep -rqs bad "$ZIBS_DIR/prometheus.yml" "$ZIBS_DIR/loki.yml" "$ZIBS_DIR/config.alloy" "$ZIBS_DIR/grafana"
EOF
}

# A live host deployed from GHCR, or with legacy=1 the host-built layout.
make_live() {
	rm -rf "$live" "$temporary_dir/date"
	mkdir -p "$live/scripts" "$live/grafana/dashboards"
	printf 'name: zibs # old\n' >"$live/compose.yaml"
	printf 'old operator\n' >"$live/grafana/dashboards/operator.json"
	printf 'old public\n' >"$live/grafana/dashboards/public-metrics.json"
	printf 'old prometheus\n' >"$live/prometheus.yml"
	cp "$bundle/scripts/telemetry-smoke-test.sh" "$live/scripts/"
	printf 'ADMIN_TOKEN=x\nGRAFANA_ADMIN_PASSWORD=y\n' >"$live/.env"
	if [[ ${1:-} != legacy ]]; then
		printf 'ZIBS_IMAGE=%s\n' "$old_image" >"$live/.env.image"
		mkdir -p "$live/.deploy"
		printf 'commit=%s\nimage=%s\nmode=full\n' "$(printf 'b%.0s' {1..40})" "$old_image" >"$live/.deploy/manifest"
	fi
	: >"$log"
}

deploy() {
	PATH="$bin:$PATH" ZIBS_DIR="$live" ZIBS_PUBLIC_URL=https://zibs.example REMOTE_TEST_LOG="$log" \
		REMOTE_TEST_DATE="$temporary_dir/date" DEPLOY_HEALTH_ATTEMPTS=2 \
		bash "$bundle/scripts/remote-deploy.sh" "$@"
}

fail() {
	printf 'FAIL: %s\n' "$*" >&2
	exit 1
}

expect_log() {
	grep -qF -- "$1" "$log" || fail "expected command log to contain: $1"
}

refute_log() {
	! grep -qF -- "$1" "$log" || fail "command log unexpectedly contains: $1"
}

file_mode() {
	stat -c '%a' "$1" 2>/dev/null || stat -f '%Lp' "$1"
}

manifest_value() {
	sed -n "s/^$1=//p" "$live/.deploy/manifest"
}

refute_host_build() {
	refute_log ' build'
	refute_log 'git '
}

test_full_deploy() {
	make_bundle
	make_live
	deploy full "$commit" "$new_image" >/dev/null
	expect_log "docker pull --quiet $new_image"
	expect_log 'network inspect zibs-edge'
	expect_log 'compose --env-file .env --env-file .env.image --profile production up -d --no-build --no-deps zibs'
	expect_log 'up -d --no-build --no-deps node-exporter'
	expect_log 'up -d --no-build --no-deps --force-recreate prometheus loki alloy grafana'
	expect_log 'curl --fail --silent --show-error --max-time 10 https://zibs.example/health'
	expect_log 'smoke full'
	refute_host_build
	[[ $(cat "$live/.env.image") == "ZIBS_IMAGE=$new_image" ]] || fail 'full deploy did not pin the new image'
	[[ $(file_mode "$live/.env.image") == 600 ]] || fail '.env.image is not mode 0600'
	[[ $(file_mode "$live/.deploy/manifest") == 600 ]] || fail 'manifest is not mode 0600'
	[[ $(manifest_value commit) == "$commit" && $(manifest_value image) == "$new_image" && $(manifest_value mode) == full ]] ||
		fail 'full deploy wrote the wrong manifest'
	grep -q '# new' "$live/compose.yaml" || fail 'full deploy did not install compose.yaml'
	grep -q 'new prometheus' "$live/prometheus.yml" || fail 'full deploy did not install prometheus.yml'
	grep -q 'new operator' "$live/grafana/dashboards/operator.json" || fail 'full deploy did not install dashboards'
	[[ -f $live/scripts/remote-deploy.sh ]] || fail 'full deploy did not install scripts'
	grep -q "$old_image" "$live/.deploy/snapshots/"*-full/.env.image || fail 'snapshot lost the previous image'
}

test_full_deploy_with_registry_token() {
	make_bundle
	make_live
	printf '%s\n' short-lived-token |
		GHCR_USER=actor deploy full "$commit" "$new_image" >/dev/null
	expect_log 'login ghcr.io --username actor --password-stdin'
	expect_log 'login token=short-lived-token'
	[[ $(grep -c short-lived-token "$log") -eq 1 ]] || fail 'registry token appeared in command arguments'
}

test_failed_full_deploy_restores_previous_image() {
	make_bundle
	make_live
	local previous_manifest output
	previous_manifest=$(cat "$live/.deploy/manifest")
	if output=$(REMOTE_TEST_BAD_IMAGE=$new_image deploy full "$commit" "$new_image" 2>&1); then
		fail 'full deploy accepted an unhealthy candidate'
	fi
	[[ $output == *'previous deployment was restored and verified'* ]] || fail "unexpected failure output: $output"
	[[ $(cat "$live/.env.image") == "ZIBS_IMAGE=$old_image" ]] || fail 'failed deploy did not restore the previous image'
	[[ $(cat "$live/.deploy/manifest") == "$previous_manifest" ]] || fail 'failed deploy changed the manifest'
	grep -q '# old' "$live/compose.yaml" || fail 'failed deploy did not restore compose.yaml'
	grep -q 'old prometheus' "$live/prometheus.yml" || fail 'failed deploy did not restore prometheus.yml'
	expect_log 'up -d --no-build --no-deps --force-recreate zibs'
	expect_log 'logs --no-color --tail=100 zibs'
}

test_first_ghcr_deploy_preserves_host_built_image() {
	make_bundle
	make_live legacy
	if REMOTE_TEST_BAD_IMAGE=$new_image deploy full "$commit" "$new_image" >/dev/null 2>&1; then
		fail 'first deploy accepted an unhealthy candidate'
	fi
	expect_log 'docker image tag sha256:host-built zibs:rollback-20260925T120001Z-full'
	[[ $(cat "$live/.env.image") == 'ZIBS_IMAGE=zibs:rollback-20260925T120001Z-full' ]] ||
		fail 'first deploy did not restore the host-built image'
	[[ ! -e $live/.deploy/manifest ]] || fail 'failed first deploy wrote a manifest'
}

test_observability_deploy() {
	make_bundle
	make_live
	deploy observability "$commit" >/dev/null
	grep -q 'new prometheus' "$live/prometheus.yml" || fail 'observability config not installed'
	grep -q '# old' "$live/compose.yaml" || fail 'observability deploy replaced compose.yaml'
	[[ $(cat "$live/.env.image") == "ZIBS_IMAGE=$old_image" ]] || fail 'observability deploy changed the image'
	expect_log 'up -d --no-build --no-deps --force-recreate prometheus loki alloy grafana'
	expect_log 'smoke full'
	refute_log 'docker pull'
	refute_log '--no-deps zibs'
	refute_host_build
	[[ $(manifest_value mode) == observability && $(manifest_value image) == "$old_image" ]] ||
		fail 'observability deploy wrote the wrong manifest'
}

test_failed_observability_deploy_restores_configuration() {
	make_bundle
	printf 'bad prometheus\n' >"$bundle/prometheus.yml"
	make_live
	if deploy observability "$commit" >/dev/null 2>&1; then
		fail 'observability deploy accepted a failing smoke test'
	fi
	grep -q 'old prometheus' "$live/prometheus.yml" || fail 'observability config not restored'
	[[ $(manifest_value mode) == full ]] || fail 'failed observability deploy changed the manifest'
}

test_dashboard_deploy() {
	make_bundle
	make_live
	deploy dashboard "$commit" >/dev/null
	grep -q 'new operator' "$live/grafana/dashboards/operator.json" || fail 'operator dashboard not installed'
	grep -q 'new public' "$live/grafana/dashboards/public-metrics.json" || fail 'public dashboard not installed'
	grep -q 'old prometheus' "$live/prometheus.yml" || fail 'dashboard deploy touched other configuration'
	expect_log 'smoke dashboard'
	refute_log ' up -d'
	refute_log 'docker pull'
	[[ $(manifest_value mode) == dashboard && $(manifest_value commit) == "$commit" ]] || fail 'dashboard deploy wrote the wrong manifest'
}

test_failed_dashboard_deploy_restores_dashboard() {
	make_bundle
	printf 'bad dashboard\n' >"$bundle/grafana/dashboards/operator.json"
	make_live
	if deploy dashboard "$commit" >/dev/null 2>&1; then
		fail 'dashboard deploy accepted a failing smoke test'
	fi
	grep -q 'old operator' "$live/grafana/dashboards/operator.json" || fail 'dashboard not restored'
	[[ $(manifest_value mode) == full ]] || fail 'failed dashboard deploy changed the manifest'
}

test_narrow_modes_require_running_services() {
	make_bundle
	make_live
	if REMOTE_TEST_STOPPED=grafana deploy dashboard "$commit" >/dev/null 2>&1; then
		fail 'dashboard deploy ran without Grafana'
	fi
	make_live legacy
	if deploy observability "$commit" >/dev/null 2>&1; then
		fail 'observability deploy ran before any GHCR image was deployed'
	fi
	[[ ! -e $live/.deploy/snapshots ]] || [[ -z $(ls "$live/.deploy/snapshots") ]] || fail 'refused deploy took a snapshot'
}

test_invalid_arguments_make_no_docker_call() {
	make_bundle
	make_live
	deploy full "$commit" ghcr.io/example/zibs:latest >/dev/null 2>&1 && fail 'accepted a mutable tag'
	deploy full "$commit" "docker.io/example/zibs@sha256:$(printf '2%.0s' {1..64})" >/dev/null 2>&1 &&
		fail 'accepted a non-GHCR image'
	deploy dashboard abc123 >/dev/null 2>&1 && fail 'accepted an abbreviated commit'
	[[ ! -s $log ]] || fail 'invalid arguments reached Docker or curl'
}

test_unexpected_bundle_entry_is_rejected() {
	make_bundle
	touch "$bundle/main.go"
	make_live
	deploy dashboard "$commit" >/dev/null 2>&1 && fail 'accepted a bundle containing source'
	grep -q 'old operator' "$live/grafana/dashboards/operator.json" || fail 'rejected bundle changed files'
}

test_manual_rollback_restores_snapshot_and_manifest() {
	make_bundle
	make_live
	local previous_manifest snapshot
	previous_manifest=$(cat "$live/.deploy/manifest")
	deploy full "$commit" "$new_image" >/dev/null
	snapshot=$(basename "$(ls -d "$live/.deploy/snapshots/"*-full)")
	: >"$log"
	PATH="$bin:$PATH" ZIBS_DIR="$live" ZIBS_PUBLIC_URL=https://zibs.example REMOTE_TEST_LOG="$log" \
		REMOTE_TEST_DATE="$temporary_dir/date" DEPLOY_HEALTH_ATTEMPTS=2 \
		bash "$live/scripts/remote-deploy.sh" rollback "$snapshot" >/dev/null
	[[ $(cat "$live/.env.image") == "ZIBS_IMAGE=$old_image" ]] || fail 'rollback did not restore the image'
	[[ $(cat "$live/.deploy/manifest") == "$previous_manifest" ]] || fail 'rollback manifest does not describe the restored deployment'
	grep -q '# old' "$live/compose.yaml" || fail 'rollback did not restore compose.yaml'
	grep -q 'old prometheus' "$live/prometheus.yml" || fail 'rollback did not restore prometheus.yml'
	expect_log 'up -d --no-build --no-deps --force-recreate zibs'
	refute_host_build
}

test_full_deploy
test_full_deploy_with_registry_token
test_failed_full_deploy_restores_previous_image
test_first_ghcr_deploy_preserves_host_built_image
test_observability_deploy
test_failed_observability_deploy_restores_configuration
test_dashboard_deploy
test_failed_dashboard_deploy_restores_dashboard
test_narrow_modes_require_running_services
test_invalid_arguments_make_no_docker_call
test_unexpected_bundle_entry_is_rejected
test_manual_rollback_restores_snapshot_and_manifest
printf '%s\n' 'remote deploy script tests passed'
