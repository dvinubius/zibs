#!/usr/bin/env bash

# Apply one zibs deployment on the VPS. GitHub Actions uploads an allowlisted
# bundle (compose.yaml, the telemetry configuration, grafana/, scripts/) to a
# staging directory and runs this script from there; operators run the
# installed copy for a manual rollback. The script snapshots the live files,
# installs only what the mode needs, verifies production, and records the
# deployment manifest only after every check passes. A failed check restores
# the snapshot.
#
# Usage:
#   remote-deploy.sh full <commit-sha> <ghcr.io/owner/zibs@sha256:digest>
#   remote-deploy.sh observability <commit-sha>
#   remote-deploy.sh dashboard <commit-sha>
#   remote-deploy.sh rollback <snapshot-name>
#
# With GHCR_USER set, full mode reads a short-lived registry token from stdin
# and uses it only for this pull. Otherwise it pulls with the host's own Docker
# credentials, which suffices for a public package.

set -euo pipefail

bundle_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
live_dir=${ZIBS_DIR:-/opt/zibs}
state_dir=$live_dir/.deploy
public_url=${ZIBS_PUBLIC_URL:-https://zibs.app}
health_attempts=${DEPLOY_HEALTH_ATTEMPTS:-30}
keep_snapshots=5
dashboard_paths=(grafana/dashboards/operator.json grafana/dashboards/public-metrics.json)
observability_entries=(prometheus.yml loki.yml config.alloy grafana)
live_entries=(compose.yaml "${observability_entries[@]}" scripts)

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

log() {
	printf '==> %s\n' "$*"
}

usage() {
	die 'Usage: remote-deploy.sh full <commit> <image> | observability <commit> | dashboard <commit> | rollback <snapshot>'
}

mode=${1:-}
commit=
image=
rollback_from=
case $mode in
full)
	[[ $# -eq 3 ]] || usage
	commit=$2
	image=$3
	;;
observability | dashboard)
	[[ $# -eq 2 ]] || usage
	commit=$2
	;;
rollback)
	[[ $# -eq 2 ]] || usage
	rollback_from=$2
	;;
*) usage ;;
esac

if [[ $mode != rollback && ! $commit =~ ^[0-9a-f]{40}$ ]]; then
	die 'Commit must be a full 40-character SHA.'
fi
if [[ $mode == full && ! $image =~ ^ghcr\.io/[a-z0-9][a-z0-9_.-]*/zibs@sha256:[0-9a-f]{64}$ ]]; then
	die 'Image must be ghcr.io/<owner>/zibs@sha256:<64 hex digits>.'
fi

ghcr_token=
if [[ $mode == full && -n ${GHCR_USER:-} ]]; then
	IFS= read -r ghcr_token || true
	[[ -n $ghcr_token ]] || die 'GHCR_USER is set, but no registry token arrived on stdin.'
fi

[[ -f $live_dir/.env ]] || die "$live_dir/.env is missing; bootstrap the host first."
[[ $public_url =~ ^https://[^[:space:]/]+$ ]] || die 'ZIBS_PUBLIC_URL must be an HTTPS origin.'

# Grafana runs as a non-root user and reads the installed provisioning files.
umask 022
mkdir -p "$state_dir/snapshots"
chmod 700 "$state_dir"
exec 9>"$state_dir/lock"
flock -n 9 || die 'Another zibs deployment holds the lock.'

if [[ $mode == rollback ]]; then
	[[ $rollback_from =~ ^[0-9A-Za-z-]+$ && -d $state_dir/snapshots/$rollback_from ]] ||
		die "Unknown snapshot: $rollback_from (see $state_dir/snapshots)."
fi

# Compose for the live project. Unlike scripts/compose.sh, this tolerates a
# missing .env.image so it can also restore a snapshot from before GHCR images.
live_compose() {
	local args=(--env-file .env)
	if [[ -f $live_dir/.env.image ]]; then
		args+=(--env-file .env.image)
	fi
	(cd "$live_dir" && docker compose "${args[@]}" --profile production "$@")
}

running_service() {
	docker ps -q \
		--filter label=com.docker.compose.project=zibs \
		--filter "label=com.docker.compose.service=$1"
}

current_image() {
	if [[ -f $live_dir/.env.image ]]; then
		sed -n 's/^ZIBS_IMAGE=//p' "$live_dir/.env.image" | tail -n 1
	fi
}

validate_bundle() {
	local entry
	for entry in "$bundle_dir"/* "$bundle_dir"/.[!.]*; do
		[[ -e $entry ]] || continue
		case ${entry##*/} in
		compose.yaml | prometheus.yml | loki.yml | config.alloy | grafana | scripts) ;;
		*) die "Unexpected bundle entry: ${entry##*/}" ;;
		esac
	done
	for entry in compose.yaml "${observability_entries[@]}" "${dashboard_paths[@]}" \
		grafana/provisioning scripts/compose.sh scripts/remote-deploy.sh \
		scripts/telemetry-smoke-test.sh scripts/backup.sh; do
		[[ -e $bundle_dir/$entry ]] || die "Bundle is missing $entry."
	done
	ZIBS_IMAGE=$1 docker compose --env-file "$live_dir/.env" \
		--project-directory "$bundle_dir" --file "$bundle_dir/compose.yaml" \
		--profile production config --quiet ||
		die 'The bundled compose.yaml does not render with the live environment.'
}

pull_image() {
	if [[ -z $ghcr_token ]]; then
		docker pull --quiet "$image" >/dev/null
	else
		local docker_config
		docker_config=$(mktemp -d)
		printf '%s\n' "$ghcr_token" |
			DOCKER_CONFIG=$docker_config docker login ghcr.io --username "$GHCR_USER" --password-stdin >/dev/null
		DOCKER_CONFIG=$docker_config docker pull --quiet "$image" >/dev/null || {
			rm -rf "$docker_config"
			return 1
		}
		rm -rf "$docker_config"
	fi
	docker image inspect "$image" >/dev/null
}

snapshot_dir=
take_snapshot() {
	local entry container
	snapshot_dir=$state_dir/snapshots/$(date -u +%Y%m%dT%H%M%SZ)-$mode
	[[ ! -e $snapshot_dir ]] || die "Snapshot already exists: $snapshot_dir"
	mkdir -p "$snapshot_dir"
	for entry in "${live_entries[@]}" .env.image; do
		if [[ -e $live_dir/$entry ]]; then
			cp -a "$live_dir/$entry" "$snapshot_dir/"
		fi
	done
	if [[ -f $state_dir/manifest ]]; then
		cp -a "$state_dir/manifest" "$snapshot_dir/manifest"
	fi
	# Before the first GHCR deployment the app runs a host-built image. Keep it
	# addressable so this snapshot can be restored.
	if [[ ! -f $snapshot_dir/.env.image ]]; then
		container=$(running_service zibs)
		if [[ -n $container ]]; then
			local tag=zibs:rollback-${snapshot_dir##*/}
			docker image tag "$(docker inspect --format '{{.Image}}' "$container")" "$tag"
			(umask 077 && printf 'ZIBS_IMAGE=%s\n' "$tag" >"$snapshot_dir/.env.image")
		fi
	fi
	log "Saved snapshot ${snapshot_dir##*/}."
}

prune_snapshots() {
	local snapshots=() name i
	while IFS= read -r name; do
		snapshots+=("$name")
	done < <(ls -1 "$state_dir/snapshots" | sort)
	for ((i = 0; i < ${#snapshots[@]} - keep_snapshots; i++)); do
		rm -rf "${state_dir:?}/snapshots/${snapshots[i]}"
	done
}

# Replace a live file or directory with the copy from a bundle or snapshot. A
# file is renamed into place within its directory, so Grafana's directory bind
# mount sees the new dashboard. A replaced directory, or a file that Compose
# bind-mounts on its own (prometheus.yml, loki.yml, config.alloy), requires
# recreating the containers that mount it.
install_entry() {
	local source_dir=$1 entry=$2
	local target=$live_dir/$entry
	mkdir -p "$(dirname -- "$target")"
	rm -rf "$target.new"
	cp -a "$source_dir/$entry" "$target.new"
	if [[ -d $target.new ]]; then
		rm -rf "$target"
	fi
	mv -f "$target.new" "$target"
}

install_entries() {
	local source_dir=$1 entry
	shift
	for entry in "$@"; do
		install_entry "$source_dir" "$entry"
	done
}

write_env_image() {
	(umask 077 && printf 'ZIBS_IMAGE=%s\n' "$1" >"$live_dir/.env.image.tmp")
	mv -f "$live_dir/.env.image.tmp" "$live_dir/.env.image"
}

verify_app() {
	local attempt
	for ((attempt = 1; ; attempt++)); do
		if curl --fail --silent --max-time 5 http://127.0.0.1:8080/health >/dev/null; then
			break
		fi
		if ((attempt >= health_attempts)); then
			printf '%s\n' 'zibs did not become healthy on 127.0.0.1:8080.' >&2
			return 1
		fi
		sleep 2
	done
	[[ -n $(running_service zibs) ]] || {
		printf '%s\n' 'zibs is not running.' >&2
		return 1
	}
	curl --fail --silent --show-error --max-time 10 "$public_url/health" >/dev/null
	log 'zibs is healthy and publicly reachable.'
}

# Prometheus, Loki, Alloy, and Grafana mount replaced configuration, so
# recreate them. node_exporter mounts none; it is only started if missing.
refresh_telemetry() {
	live_compose up -d --no-build --no-deps node-exporter
	live_compose up -d --no-build --no-deps --force-recreate prometheus loki alloy grafana
	bash "$live_dir/scripts/telemetry-smoke-test.sh" full
}

restore_everything() {
	local source_dir=$1 entry
	for entry in "${live_entries[@]}"; do
		if [[ -e $source_dir/$entry ]]; then
			install_entry "$source_dir" "$entry"
		fi
	done
	if [[ -f $source_dir/.env.image ]]; then
		write_env_image "$(sed -n 's/^ZIBS_IMAGE=//p' "$source_dir/.env.image" | tail -n 1)"
	else
		rm -f "$live_dir/.env.image"
	fi
	live_compose up -d --no-build --no-deps --force-recreate zibs
	verify_app
	refresh_telemetry
}

apply_full() {
	install_entries "$bundle_dir" "${live_entries[@]}"
	write_env_image "$image"
	live_compose up -d --no-build --no-deps zibs
	verify_app
	refresh_telemetry
}

apply_observability() {
	install_entries "$bundle_dir" "${observability_entries[@]}"
	refresh_telemetry
}

apply_dashboard() {
	install_entries "$bundle_dir" "${dashboard_paths[@]}"
	bash "$live_dir/scripts/telemetry-smoke-test.sh" dashboard
}

apply_rollback() {
	restore_everything "$state_dir/snapshots/$rollback_from"
}

restore_snapshot() {
	case $mode in
	dashboard)
		install_entries "$snapshot_dir" "${dashboard_paths[@]}"
		bash "$live_dir/scripts/telemetry-smoke-test.sh" dashboard
		;;
	observability)
		install_entries "$snapshot_dir" "${observability_entries[@]}"
		refresh_telemetry
		;;
	full | rollback) restore_everything "$snapshot_dir" ;;
	esac
}

diagnostics() {
	live_compose ps || true
	if [[ $mode == full || $mode == rollback ]]; then
		live_compose logs --no-color --tail=100 zibs || true
	fi
	if [[ $mode != full ]]; then
		live_compose logs --no-color --tail=30 prometheus loki alloy grafana || true
	fi
}

record_manifest() {
	local deployed_image=$1
	(
		umask 077
		printf 'commit=%s\nimage=%s\nmode=%s\ndeployed_at=%s\n' \
			"$commit" "$deployed_image" "$mode" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
			>"$state_dir/manifest.tmp"
	)
	mv -f "$state_dir/manifest.tmp" "$state_dir/manifest"
}

# Preconditions and validation make no change to live files or containers.
case $mode in
full)
	command -v curl >/dev/null || die 'curl is required on the host.'
	docker compose version >/dev/null
	docker network inspect zibs-edge >/dev/null || die 'The Hetzner-One-owned zibs-edge network is missing.'
	log "Pulling $image."
	pull_image
	validate_bundle "$image"
	;;
observability | dashboard)
	[[ -n $(current_image) ]] || die 'No GHCR image is deployed yet; run a full deployment first.'
	if [[ $mode == observability ]]; then
		[[ -n $(running_service zibs) ]] || die 'zibs must be running for an observability deployment.'
	else
		[[ -n $(running_service grafana) ]] || die 'Grafana must be running for a dashboard deployment.'
	fi
	validate_bundle "$(current_image)"
	;;
esac

take_snapshot
log "Applying $mode deployment."
set +e
(
	set -e
	"apply_$mode"
)
status=$?
set -e

if ((status == 0)); then
	if [[ $mode == rollback ]]; then
		if [[ -f $state_dir/snapshots/$rollback_from/manifest ]]; then
			cp -a "$state_dir/snapshots/$rollback_from/manifest" "$state_dir/manifest"
		else
			rm -f "$state_dir/manifest"
		fi
	else
		record_manifest "$(current_image)"
	fi
	prune_snapshots
	log "zibs $mode deployment verified."
	exit 0
fi

printf 'zibs %s deployment failed; restoring snapshot %s.\n' "$mode" "${snapshot_dir##*/}" >&2
diagnostics
set +e
(
	set -e
	restore_snapshot
)
restore_status=$?
set -e
if ((restore_status != 0)); then
	die "ROLLBACK FAILED after a failed $mode deployment. Inspect the host; snapshot ${snapshot_dir##*/} holds the previous files."
fi
die "zibs $mode deployment failed; the previous deployment was restored and verified."
