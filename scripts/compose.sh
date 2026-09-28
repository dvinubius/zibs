#!/usr/bin/env bash

# Run Docker Compose for the deployed zibs project with its VPS-owned
# environment: the runtime secrets in .env plus .env.image, which pins
# ZIBS_IMAGE to a published image digest. Every operator and script Compose
# call goes through here so a reboot, backup, or manual `up` uses the same
# image and the production profile.
#
# Usage: scripts/compose.sh <compose arguments...>

set -euo pipefail

project_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$project_dir"

for file in .env .env.image; do
	[[ -f $file ]] || {
		printf '%s is missing in %s; deploy a published image first.\n' "$file" "$project_dir" >&2
		exit 1
	}
done
exec docker compose --env-file .env --env-file .env.image --profile production "$@"
