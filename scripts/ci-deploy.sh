#!/usr/bin/env bash

# GitHub Actions side of production deployment. It never builds on the VPS and
# never gives the VPS Git access: it reads the VPS deployment manifest, chooses
# a mode from every change since that verified deployment, and uploads an
# allowlisted bundle taken from the exact target commit.
#
# Usage:
#   ci-deploy.sh validate                      Validate the dashboard JSON.
#   ci-deploy.sh plan <target-sha> [full]      Print none|dashboard|observability|full.
#   ci-deploy.sh deploy <mode> <target-sha> [image]
#
# plan and deploy need DEPLOY_HOST, DEPLOY_USER, DEPLOY_SSH_KEY_FILE, and
# DEPLOY_KNOWN_HOSTS_FILE (a verified host-key entry). A full deploy needs
# ZIBS_IMAGE_REPOSITORY (ghcr.io/<owner>/zibs) and pulls with
# GHCR_USER/GHCR_PULL_TOKEN when both are set.

set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_dir"

remote_dir=/opt/zibs

die() {
	printf '%s\n' "$*" >&2
	exit 1
}

# Grafana 13.0.x can save V2 Resource JSON even when Classic is selected; the
# file provider only accepts the Classic model.
validate_dashboards() {
	python3 - <<'PY'
import json

for path, uid in (
    ("grafana/dashboards/operator.json", "zibs-operator"),
    ("grafana/dashboards/public-metrics.json", "zibs-public-metrics"),
    ("grafana/dashboards/traffic-synthetic.json", "zibs-traffic-synthetic"),
    ("grafana/dashboards/traffic-suspected-scan.json", "zibs-traffic-suspected"),
    ("grafana/dashboards/traffic-other.json", "zibs-traffic-other"),
):
    with open(path, encoding="utf-8") as source:
        dashboard = json.load(source)
    if dashboard.get("uid") != uid or not isinstance(dashboard.get("panels"), list) or "apiVersion" in dashboard:
        raise SystemExit(f"{path} must be a Classic Grafana dashboard with UID {uid}")
PY
}

require_target() {
	[[ $1 =~ ^[0-9a-f]{40}$ ]] || die 'Target must be a full commit SHA.'
	git cat-file -e "$1^{commit}" 2>/dev/null || die "Target commit is not available: $1"
}

# Production follows main only. A run whose commit is no longer the head of
# main is stale; the newer run will deploy from the same unchanged baseline.
require_main_head() {
	local head
	head=$(git ls-remote --exit-code origin refs/heads/main | cut -f1) || die 'Could not read the head of main.'
	[[ $head == "$1" ]] || die "Stale run: main is at $head, not $1."
}

ssh_options=()
require_ssh() {
	[[ -n ${DEPLOY_HOST:-} && -n ${DEPLOY_USER:-} ]] || die 'DEPLOY_HOST and DEPLOY_USER are required.'
	[[ -s ${DEPLOY_SSH_KEY_FILE:-} ]] || die 'DEPLOY_SSH_KEY_FILE must name the deployment key.'
	[[ -s ${DEPLOY_KNOWN_HOSTS_FILE:-} ]] || die 'DEPLOY_KNOWN_HOSTS_FILE must hold the verified host key.'
	ssh_options=(
		-i "$DEPLOY_SSH_KEY_FILE"
		-o BatchMode=yes
		-o ConnectTimeout=15
		-o IdentitiesOnly=yes
		-o StrictHostKeyChecking=yes
		-o UserKnownHostsFile="$DEPLOY_KNOWN_HOSTS_FILE"
	)
}

remote() {
	ssh "${ssh_options[@]}" "$DEPLOY_USER@$DEPLOY_HOST" "$@"
}

plan() {
	local target=$1 force=${2:-} manifest deployed
	require_target "$target"
	require_ssh
	# A missing manifest means no verified GHCR deployment yet; an SSH failure
	# stops the run instead of guessing.
	manifest=$(remote "cat '$remote_dir/.deploy/manifest' 2>/dev/null || true") ||
		die 'Could not read the deployment manifest over SSH.'
	deployed=$(sed -n 's/^commit=//p' <<<"$manifest" | tail -n 1)
	printf 'Last verified deployment: %s\n' "${deployed:-none}" >&2
	if [[ $force == full ]]; then
		printf '%s\n' full
		return
	fi
	bash scripts/classify-deploy.sh "$deployed" "$target"
}

build_bundle() {
	git archive --format=tar "$1" -- compose.yaml prometheus.yml loki.yml config.alloy grafana scripts \
		':(exclude)scripts/*_test.sh' \
		':(exclude)scripts/ci-deploy.sh' \
		':(exclude)scripts/classify-deploy.sh' \
		':(exclude)scripts/apply-grafana-v2-layout.sh'
}

deploy() {
	local mode=$1 target=$2 image=${3:-} staging command
	case $mode in
	full)
		[[ -n ${ZIBS_IMAGE_REPOSITORY:-} ]] || die 'ZIBS_IMAGE_REPOSITORY is required for a full deployment.'
		[[ $image =~ ^${ZIBS_IMAGE_REPOSITORY//./\\.}@sha256:[0-9a-f]{64}$ ]] ||
			die "Image must be $ZIBS_IMAGE_REPOSITORY@sha256:<digest>."
		;;
	observability | dashboard) [[ -z $image ]] || die "A $mode deployment does not take an image." ;;
	*) die 'Mode must be full, observability, or dashboard.' ;;
	esac
	require_target "$target"
	validate_dashboards
	require_ssh
	require_main_head "$target"

	# Grafana runs as a non-root user and reads the bind-mounted provisioning
	# files, so the extracted bundle must be world-readable.
	staging=$remote_dir/.deploy/staging/$target
	build_bundle "$target" |
		remote "umask 022 && rm -rf '$staging' && mkdir -p '$staging' && tar -x -C '$staging'"

	command="bash '$staging/scripts/remote-deploy.sh' $mode $target $image; status=\$?; rm -rf '$staging'; exit \$status"
	# The token goes on stdin, never in arguments. A here-string rather than a
	# process substitution: bash 5 closes a <(...) descriptor once the command
	# that created it ends, so it cannot be kept in a variable.
	if [[ $mode == full && -n ${GHCR_USER:-} && -n ${GHCR_PULL_TOKEN:-} ]]; then
		[[ $GHCR_USER =~ ^[A-Za-z0-9-]+(\[bot\])?$ ]] || die 'GHCR_USER is not a GitHub login.'
		remote "GHCR_USER='$GHCR_USER' $command" <<<"$GHCR_PULL_TOKEN"
	else
		remote "$command" </dev/null
	fi
}

case ${1:-} in
validate)
	validate_dashboards
	printf '%s\n' 'Dashboard JSON is valid.'
	;;
plan)
	[[ $# -ge 2 && $# -le 3 ]] || die 'Usage: ci-deploy.sh plan <target-sha> [full]'
	plan "$2" "${3:-}"
	;;
deploy)
	[[ $# -ge 3 && $# -le 4 ]] || die 'Usage: ci-deploy.sh deploy <mode> <target-sha> [image]'
	deploy "$2" "$3" "${4:-}"
	;;
*) die 'Usage: ci-deploy.sh validate | plan <target-sha> [full] | deploy <mode> <target-sha> [image]' ;;
esac
