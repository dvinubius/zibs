#!/usr/bin/env bash

set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
deploy_host=${DEPLOY_HOST:?Set DEPLOY_HOST to the server hostname or IP address.}
deploy_user=${DEPLOY_USER:-root}
deploy_path=/opt/zibs
deploy_all=${DEPLOY_ALL:-0}
admin_token=${ADMIN_TOKEN:?Set ADMIN_TOKEN without writing it to a file in this repository.}
grafana_admin_password=${GRAFANA_ADMIN_PASSWORD:-}

if [[ $admin_token == *$'\n'* ]]; then
	printf '%s\n' 'ADMIN_TOKEN must not contain a newline.' >&2
	exit 1
fi

if [[ $deploy_all != 0 && $deploy_all != 1 ]]; then
	printf '%s\n' 'DEPLOY_ALL must be either 0 or 1.' >&2
	exit 1
fi

if [[ $deploy_all == 1 && -z $grafana_admin_password ]]; then
	printf '%s\n' 'Set GRAFANA_ADMIN_PASSWORD when deploying the production profile.' >&2
	exit 1
fi

if [[ $grafana_admin_password == *$'\n'* ]]; then
	printf '%s\n' 'GRAFANA_ADMIN_PASSWORD must not contain a newline.' >&2
	exit 1
fi

ssh_options=(-o BatchMode=yes)
if [[ -n ${DEPLOY_SSH_KEY:-} ]]; then
	ssh_options+=(-i "$DEPLOY_SSH_KEY")
fi

ssh_command=$(printf '%q ' ssh "${ssh_options[@]}")
target="$deploy_user@$deploy_host"

ssh "${ssh_options[@]}" "$target" "install -d -m 0750 $deploy_path"

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

# Secrets travel over SSH and are written as a mode-0600 file on the VM.
if [[ $deploy_all == 1 ]]; then
	printf 'ADMIN_TOKEN=%s\nGRAFANA_ADMIN_PASSWORD=%s\n' "$admin_token" "$grafana_admin_password" | \
		ssh "${ssh_options[@]}" "$target" "umask 077; cat > $deploy_path/.env"
else
	printf 'ADMIN_TOKEN=%s\n' "$admin_token" | \
	ssh "${ssh_options[@]}" "$target" "umask 077; cat > $deploy_path/.env"
fi

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
		for service in zibs caddy prometheus loki alloy grafana; do
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
