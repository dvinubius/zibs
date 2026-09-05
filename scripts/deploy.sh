#!/usr/bin/env bash

set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
deploy_host=${DEPLOY_HOST:?Set DEPLOY_HOST to the server hostname or IP address.}
deploy_user=${DEPLOY_USER:-root}
deploy_path=/opt/zibs
deploy_all=${DEPLOY_ALL:-0}
deploy_dashboards=${DEPLOY_DASHBOARDS:-0}

if [[ $deploy_all != 0 && $deploy_all != 1 ]] || [[ $deploy_dashboards != 0 && $deploy_dashboards != 1 ]]; then
	printf '%s\n' 'DEPLOY_ALL and DEPLOY_DASHBOARDS must each be either 0 or 1.' >&2
	exit 1
fi

if [[ $deploy_all == 1 && $deploy_dashboards == 1 ]]; then
	printf '%s\n' 'DEPLOY_ALL and DEPLOY_DASHBOARDS cannot both be 1.' >&2
	exit 1
fi

ssh_options=(-o BatchMode=yes)
if [[ -n ${DEPLOY_SSH_KEY:-} ]]; then
	ssh_options+=(-i "$DEPLOY_SSH_KEY")
fi

ssh_command=$(printf '%q ' ssh "${ssh_options[@]}")
target="$deploy_user@$deploy_host"

ssh "${ssh_options[@]}" "$target" "install -d -m 0750 $deploy_path"

if [[ $deploy_dashboards == 1 ]]; then
	if ! command -v jq >/dev/null; then
		printf '%s\n' 'DEPLOY_DASHBOARDS=1 requires jq to validate the dashboard JSON.' >&2
		exit 1
	fi
	jq -e . "$project_dir/grafana/dashboards/operator.json" >/dev/null
	jq -e . "$project_dir/grafana/dashboards/public-metrics.json" >/dev/null
	if ! jq -e --arg uid zibs-operator \
		'(.apiVersion | not) and (.panels | type == "array") and .uid == $uid' \
		"$project_dir/grafana/dashboards/operator.json" >/dev/null; then
		printf '%s\n' 'operator.json must use the Classic dashboard model and uid zibs-operator; do not deploy Grafana 13.0.x Save-dashboard V2 output directly.' >&2
		exit 1
	fi
	if ! jq -e --arg uid zibs-public-metrics \
		'(.apiVersion | not) and (.panels | type == "array") and .uid == $uid' \
		"$project_dir/grafana/dashboards/public-metrics.json" >/dev/null; then
		printf '%s\n' 'public-metrics.json must use the Classic dashboard model and uid zibs-public-metrics; do not deploy Grafana 13.0.x Save-dashboard V2 output directly.' >&2
		exit 1
	fi
	# Grafana runs as a non-root user and must be able to traverse the bind-mounted
	# directory to poll its provisioned dashboard files.
	ssh "${ssh_options[@]}" "$target" "install -d -m 0755 $deploy_path/grafana/dashboards"
	rsync -az \
		--chmod=Du=rwx,Dgo=rx,Fu=rw,Fgo=r \
		-e "$ssh_command" \
		"$project_dir/grafana/dashboards/operator.json" \
		"$project_dir/grafana/dashboards/public-metrics.json" \
		"$target:$deploy_path/grafana/dashboards/"
	# rsync archive mode preserves source modification times. Touch the uploaded
	# files so Grafana's polling file provider reliably detects this deploy,
	# including when a source file happens to retain its previous timestamp.
	ssh "${ssh_options[@]}" "$target" \
		"chmod 0755 $deploy_path/grafana/dashboards && touch $deploy_path/grafana/dashboards/operator.json $deploy_path/grafana/dashboards/public-metrics.json"
	printf '%s\n' 'Dashboard JSON uploaded. Grafana will apply it within 30 seconds.'
	exit 0
fi

admin_token=${ADMIN_TOKEN:?Set ADMIN_TOKEN without writing it to a file in this repository.}
# Required even for app-only deploys: compose interpolates the grafana
# service's ${GRAFANA_ADMIN_PASSWORD:?} at parse time regardless of the
# services being started, and the .env written below must stay complete.
grafana_admin_password=${GRAFANA_ADMIN_PASSWORD:?Set GRAFANA_ADMIN_PASSWORD in the deployment environment.}

if [[ $admin_token == *$'\n'* ]]; then
	printf '%s\n' 'ADMIN_TOKEN must not contain a newline.' >&2
	exit 1
fi

if [[ $grafana_admin_password == *$'\n'* ]]; then
	printf '%s\n' 'GRAFANA_ADMIN_PASSWORD must not contain a newline.' >&2
	exit 1
fi

rsync -az \
	--exclude '.git/' \
	--exclude '.agents/' \
	--exclude '.codex/' \
	--exclude 'docs/' \
	--exclude 'data/' \
	--exclude '*.db*' \
	--exclude '.env*' \
	--exclude 'bin/' \
	--exclude 'coverage.out' \
	-e "$ssh_command" \
	"$project_dir/" "$target:$deploy_path/"

# rsync archive mode preserves local modes. Dashboard files are bind-mounted
# into Grafana, which runs as a non-root user, so keep every provisioned input
# traversable/readable after both full and application-only deployments.
ssh "${ssh_options[@]}" "$target" \
	"chmod 0755 $deploy_path/grafana $deploy_path/grafana/dashboards $deploy_path/grafana/provisioning $deploy_path/grafana/provisioning/dashboards $deploy_path/grafana/provisioning/datasources && chmod 0644 $deploy_path/grafana/dashboards/operator.json $deploy_path/grafana/dashboards/public-metrics.json $deploy_path/grafana/provisioning/dashboards/dashboards.yaml $deploy_path/grafana/provisioning/datasources/datasources.yaml"

# Secrets travel over SSH and are written as a mode-0600 file on the VM.
# Both are always written: an app-only deploy must not clobber the Grafana
# password out of .env, and compose needs it defined even to start just zibs.
printf 'ADMIN_TOKEN=%s\nGRAFANA_ADMIN_PASSWORD=%s\n' "$admin_token" "$grafana_admin_password" | \
	ssh "${ssh_options[@]}" "$target" "umask 077; cat > $deploy_path/.env"

if [[ $deploy_all == 1 ]]; then
	ssh "${ssh_options[@]}" "$target" \
		"cd $deploy_path && docker compose --profile production up --build --detach --force-recreate"
else
	ssh "${ssh_options[@]}" "$target" \
		"cd $deploy_path && docker compose up --build --detach zibs"
fi

ssh "${ssh_options[@]}" "$target" \
	"curl --fail --silent --show-error http://127.0.0.1:8080/health"

if [[ $deploy_all == 1 ]]; then
	ssh "${ssh_options[@]}" "$target" "
		cd $deploy_path || exit 1
		for service in zibs caddy prometheus node-exporter loki alloy grafana; do
			status=\$(docker compose --profile production ps --status running --services \"\$service\")
			if [ \"\$status\" != \"\$service\" ]; then
				printf '%s\\n' \"expected \$service to be running, got: \$status\" >&2
				exit 1
			fi
		done

		loki_id=\$(docker compose --profile production ps -q loki)
		alloy_id=\$(docker compose --profile production ps -q alloy)
		loki_ip=\$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' \"\$loki_id\")
		alloy_ip=\$(docker inspect -f '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' \"\$alloy_id\")

		attempt=1
		while [ \"\$attempt\" -le 30 ]; do
			if curl --fail --silent \"http://\$loki_ip:3100/ready\" >/dev/null && \\
				curl --fail --silent \"http://\$alloy_ip:12345/-/ready\" >/dev/null; then
				exit 0
			fi
			sleep 1
			attempt=\$((attempt + 1))
		done

		printf '%s\\n' 'Loki or Alloy did not become ready within 30 seconds.' >&2
		exit 1
	"
	printf '%s\n' 'Running telemetry smoke tests...'
	ssh "${ssh_options[@]}" "$target" \
		"cd $deploy_path && ./scripts/telemetry-smoke-test.sh"
fi

printf '%s\n' 'Deployment succeeded.'
