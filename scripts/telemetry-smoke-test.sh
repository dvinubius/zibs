#!/usr/bin/env bash

# Verify the deployed telemetry path. `full` (the default) checks zibs,
# Prometheus's zibs and node targets, Loki and Alloy readiness, a fresh zibs
# log in Loki, and Grafana with its data sources and dashboards. `dashboard`
# checks only that Grafana is healthy and serves both provisioned dashboards.
#
# Usage: scripts/telemetry-smoke-test.sh [full|dashboard]

set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
mode=${1:-full}
timeout_seconds=${SMOKE_TEST_TIMEOUT_SECONDS:-45}
grafana_user=${GRAFANA_ADMIN_USER:-admin}

if [[ $mode != full && $mode != dashboard ]]; then
	printf '%s\n' 'Usage: telemetry-smoke-test.sh [full|dashboard]' >&2
	exit 1
fi

if ! [[ $timeout_seconds =~ ^[1-9][0-9]*$ ]]; then
	printf '%s\n' 'SMOKE_TEST_TIMEOUT_SECONDS must be a positive integer.' >&2
	exit 1
fi

cd "$project_dir"

grafana_password=$(sed -n 's/^GRAFANA_ADMIN_PASSWORD=//p' .env)
if [[ -z $grafana_password ]]; then
	printf '%s\n' 'GRAFANA_ADMIN_PASSWORD is missing from .env.' >&2
	exit 1
fi

compose=(scripts/compose.sh)

service_ip() {
	local service=$1
	local container_id
	container_id=$("${compose[@]}" ps --status running -q "$service")
	if [[ -z $container_id ]]; then
		printf 'No running container found for %s.\n' "$service" >&2
		return 1
	fi
	docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "$container_id"
}

wait_for() {
	local description=$1
	shift
	local attempt=1
	while [[ $attempt -le $timeout_seconds ]]; do
		if "$@"; then
			printf 'PASS: %s\n' "$description"
			return 0
		fi
		sleep 1
		attempt=$((attempt + 1))
	done

	printf 'FAIL: %s did not succeed within %s seconds.\n' "$description" "$timeout_seconds" >&2
	return 1
}

grafana_get() {
	# Keep the credential out of curl's process arguments and diagnostic output.
	printf 'user = "%s:%s"\n' "$grafana_user" "$grafana_password" |
		curl --config - --fail --silent --show-error "$@"
}

grafana_is_healthy() {
	curl --fail --silent --show-error http://127.0.0.1:3000/api/health >/dev/null
}

grafana_data_source_is_healthy() {
	grafana_get "http://127.0.0.1:3000/api/datasources/uid/$1/health" >/dev/null
}

grafana_dashboard_exists() {
	grafana_get "http://127.0.0.1:3000/api/dashboards/uid/$1" | grep -q "\"uid\":\"$1\""
}

check_dashboards() {
	wait_for 'operator dashboard is provisioned' grafana_dashboard_exists zibs-operator
	wait_for 'public metrics dashboard is provisioned' grafana_dashboard_exists zibs-public-metrics
}

wait_for 'Grafana HTTP API is healthy' grafana_is_healthy
if [[ $mode == dashboard ]]; then
	# The file provider polls every 30 seconds (updateIntervalSeconds). Allow
	# one poll before checking so the previous copy does not satisfy the check.
	sleep 32
	check_dashboards
	printf '%s\n' 'Dashboard smoke test passed.'
	exit 0
fi

prometheus_ip=$(service_ip prometheus)
loki_ip=$(service_ip loki)
alloy_ip=$(service_ip alloy)

prometheus_target_is_up() {
	# Ask for the targets Prometheus scrapes now. An `up` query would still
	# return the last sample from before a restart for about five minutes.
	local targets
	targets=$(curl --fail --silent --show-error --get \
		--data-urlencode 'state=active' --data-urlencode "scrapePool=$1" \
		"http://$prometheus_ip:9090/api/v1/targets") || return 1
	grep -q '"health":"up"' <<<"$targets" && ! grep -qE '"health":"(down|unknown)"' <<<"$targets"
}

loki_is_ready() {
	curl --fail --silent "http://$loki_ip:3100/ready" >/dev/null
}

alloy_is_ready() {
	curl --fail --silent "http://$alloy_ip:12345/-/ready" >/dev/null
}

loki_has_new_zibs_log() {
	# Only a line logged during this test proves that the running Alloy and
	# Loki ship logs; without a start, older lines would satisfy the query.
	curl --fail --silent --show-error --get \
		--data-urlencode 'query={service="zibs"}' \
		--data-urlencode "start=$logs_since" \
		--data-urlencode 'limit=1' \
		"http://$loki_ip:3100/loki/api/v1/query_range" | grep -q '"result":\[{'
}

# The health request below logs a request line for Loki to receive.
logs_since=$(date -u +%Y-%m-%dT%H:%M:%SZ)
curl --fail --silent --show-error http://127.0.0.1:8080/health >/dev/null
printf '%s\n' 'PASS: zibs accepted a health request.'

wait_for 'Loki is ready' loki_is_ready
wait_for 'Alloy is ready' alloy_is_ready
wait_for 'Prometheus reports zibs as up' prometheus_target_is_up zibs
wait_for 'Prometheus reports node_exporter as up' prometheus_target_is_up node
wait_for 'Loki received a zibs log from this test' loki_has_new_zibs_log
wait_for 'Grafana can query Prometheus' grafana_data_source_is_healthy prometheus
wait_for 'Grafana can query Loki' grafana_data_source_is_healthy loki
check_dashboards

printf '%s\n' 'Telemetry smoke test passed.'
