#!/usr/bin/env bash

set -euo pipefail

usage() {
	printf '%s\n' "Usage: $0 <v2-export.json> <classic-dashboard.json>" >&2
}

if [[ $# != 2 ]]; then
	usage
	exit 1
fi

v2_export=$1
classic_dashboard=$2

if ! command -v jq >/dev/null; then
	printf '%s\n' 'jq is required to convert a Grafana V2 dashboard layout.' >&2
	exit 1
fi

if [[ ! -f $v2_export ]]; then
	printf 'V2 export does not exist: %s\n' "$v2_export" >&2
	exit 1
fi

if [[ ! -f $classic_dashboard ]]; then
	printf 'Classic dashboard does not exist: %s\n' "$classic_dashboard" >&2
	exit 1
fi

v2_spec_filter='if .apiVersion == "dashboard.grafana.app/v2" and .kind == "Dashboard" then .spec else . end'

if ! jq -e "$v2_spec_filter | (.elements | type == \"object\") and (.layout.kind == \"GridLayout\") and (.layout.spec.items | type == \"array\")" "$v2_export" >/dev/null; then
	printf '%s\n' 'V2 export must contain a dashboard V2 GridLayout and elements object.' >&2
	exit 1
fi

if ! jq -e --slurpfile export "$v2_export" "
	def v2spec: $v2_spec_filter;
	(\$export[0] | v2spec) as \$v2 |
	([.panels[].id] | sort) as \$classic_ids |
	([
		\$v2.layout.spec.items[] |
		select(.kind == \"GridLayoutItem\") |
		.spec as \$item |
		select((\$item.element.kind == \"ElementReference\") and (\$item.element.name | test(\"^panel-[0-9]+$\"))) |
		select((\$item.x | type) == \"number\" and (\$item.y | type) == \"number\" and (\$item.width | type) == \"number\" and (\$item.height | type) == \"number\") |
		select(\$v2.elements[\$item.element.name].kind == \"Panel\") |
		(\$item.element.name | ltrimstr(\"panel-\") | tonumber)
	] | sort) as \$layout_ids |
	(\$classic_ids | length > 0) and
	(\$classic_ids | length == (\$classic_ids | unique | length)) and
	(\$layout_ids | length == (\$layout_ids | unique | length)) and
	(\$classic_ids == \$layout_ids)
" "$classic_dashboard" >/dev/null; then
	printf '%s\n' 'V2 layout items must map one-to-one to the Classic dashboard panel IDs.' >&2
	printf '%s\n' 'The target dashboard was not changed.' >&2
	exit 1
fi

target_dir=$(dirname "$classic_dashboard")
temporary_dashboard=$(mktemp "$target_dir/.dashboard-layout.XXXXXX")
trap 'rm -f "$temporary_dashboard"' EXIT
cp -p "$classic_dashboard" "$temporary_dashboard"

jq --slurpfile export "$v2_export" "
	def v2spec: $v2_spec_filter;
	(\$export[0] | v2spec) as \$v2 |
	reduce \$v2.layout.spec.items[] as \$layout (
		.;
		\$layout.spec as \$position |
		(\$position.element.name | ltrimstr(\"panel-\") | tonumber) as \$panel_id |
		.panels |= map(
			if .id == \$panel_id then
				.gridPos = {
					x: \$position.x,
					y: \$position.y,
					w: \$position.width,
					h: \$position.height
				}
			else . end
		)
	)
" "$classic_dashboard" >"$temporary_dashboard"

if cmp -s "$classic_dashboard" "$temporary_dashboard"; then
	printf '%s\n' 'Layout already matches the V2 export; target dashboard was not changed.'
	exit 0
fi

mv "$temporary_dashboard" "$classic_dashboard"
trap - EXIT
printf 'Updated Classic panel grid positions in %s. Review the diff before deploying.\n' "$classic_dashboard"
