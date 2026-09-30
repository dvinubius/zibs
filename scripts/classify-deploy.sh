#!/usr/bin/env bash

# Choose the production deployment mode for every change between the last
# verified deployment and a target commit. Prints exactly one of: none,
# dashboard, observability, full. Exits non-zero, printing nothing on stdout,
# when the target cannot be verified or Git inspection fails; callers must then
# make no production change.
#
# Usage: classify-deploy.sh <deployed-sha-or-empty> <target-sha>

set -euo pipefail

die() {
	printf '%s\n' "$*" >&2
	exit 2
}

[[ $# -eq 2 ]] || die 'Usage: classify-deploy.sh <deployed-sha-or-empty> <target-sha>'
deployed=$1
target=$2

[[ $target =~ ^[0-9a-f]{40}$ ]] || die 'Target must be a full commit SHA.'
git cat-file -e "$target^{commit}" 2>/dev/null || die "Target commit is not available: $target"

# Without a trustworthy baseline, never guess a narrower mode.
if [[ ! $deployed =~ ^[0-9a-f]{40}$ ]] ||
	! git cat-file -e "$deployed^{commit}" 2>/dev/null ||
	! git merge-base --is-ancestor "$deployed" "$target" 2>/dev/null; then
	printf '%s\n' full
	exit 0
fi

# --no-renames reports both sides of a rename, so a moved file is classified
# by its old and its new path.
changed=$(git diff --name-only --no-renames "$deployed" "$target") || die 'git diff failed.'

# Paths that never reach the image, the VPS bundle, or the running services.
# Anything not listed here, including new operational files, deploys in full.
is_non_deployment() {
	case $1 in
	docs/* | .agents/* | .claude/* | .codex/* | .vscode/*) return 0 ;;
	README.md | AGENTS.md | CLAUDE.md | .devnotes.md | LICENSE | .gitignore) return 0 ;;
	scripts/*_test.sh | scripts/apply-grafana-v2-layout.sh) return 0 ;;
	cmd/zibs/*/*) return 1 ;;
	cmd/zibs/*_test.go) return 0 ;;
	esac
	return 1
}

dashboard=false
observability=false
while IFS= read -r path; do
	[[ -n $path ]] || continue
	case $path in
	grafana/dashboards/operator.json | grafana/dashboards/public-metrics.json | \
	grafana/dashboards/traffic-synthetic.json | grafana/dashboards/traffic-suspected-scan.json | \
	grafana/dashboards/traffic-other.json) dashboard=true ;;
	grafana/* | prometheus.yml | loki.yml | config.alloy) observability=true ;;
	*)
		if ! is_non_deployment "$path"; then
			printf '%s\n' full
			exit 0
		fi
		;;
	esac
done <<<"$changed"

if $dashboard && $observability; then
	printf '%s\n' full
elif $dashboard; then
	printf '%s\n' dashboard
elif $observability; then
	printf '%s\n' observability
else
	printf '%s\n' none
fi
