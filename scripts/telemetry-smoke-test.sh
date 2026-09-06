#!/usr/bin/env bash

set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
timeout_seconds=${SMOKE_TEST_TIMEOUT_SECONDS:-45}
grafana_user=${GRAFANA_ADMIN_USER:-admin}

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

service_ip() {
	local service=$1
	local container_id
	container_id=$(docker compose --profile production ps -q "$service")
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

prometheus_ip=$(service_ip prometheus)
loki_ip=$(service_ip loki)

prometheus_has_zibs_target() {
	curl --fail --silent --show-error --get \
		--data-urlencode 'query=up{job="zibs"} == 1' \
		"http://$prometheus_ip:9090/api/v1/query" | grep -q '"result":\[{'
}

prometheus_has_node_target() {
	curl --fail --silent --show-error --get \
		--data-urlencode 'query=up{job="node"} == 1' \
		"http://$prometheus_ip:9090/api/v1/query" | grep -q '"result":\[{'
}

loki_has_zibs_log() {
	curl --fail --silent --show-error --get \
		--data-urlencode 'query={service="zibs"}' \
		--data-urlencode 'limit=1' \
		"http://$loki_ip:3100/loki/api/v1/query_range" | grep -q '"result":\[{'
}

grafana_is_healthy() {
	curl --fail --silent --show-error http://127.0.0.1:3000/api/health >/dev/null
}

grafana_data_source_is_healthy() {
	local data_source_uid=$1
	curl --fail --silent --show-error \
		--user "$grafana_user:$grafana_password" \
		"http://127.0.0.1:3000/api/datasources/uid/$data_source_uid/health" >/dev/null
}

grafana_dashboard_exists() {
	local dashboard_uid=$1
	curl --fail --silent --show-error \
		--user "$grafana_user:$grafana_password" \
		"http://127.0.0.1:3000/api/dashboards/uid/$dashboard_uid" >/dev/null
}

curl --fail --silent --show-error http://127.0.0.1:8080/health >/dev/null
printf '%s\n' 'PASS: zibs accepted a health request.'

wait_for 'Prometheus reports zibs as up' prometheus_has_zibs_target
wait_for 'Prometheus reports node_exporter as up' prometheus_has_node_target
wait_for 'Loki contains a zibs log entry' loki_has_zibs_log
wait_for 'Grafana HTTP API is healthy' grafana_is_healthy
wait_for 'Grafana can query Prometheus' grafana_data_source_is_healthy prometheus
wait_for 'Grafana can query Loki' grafana_data_source_is_healthy loki
wait_for 'operator dashboard is provisioned' grafana_dashboard_exists zibs-operator
wait_for 'public metrics dashboard is provisioned' grafana_dashboard_exists zibs-public-metrics

printf '%s\n' 'Telemetry smoke test passed.'
