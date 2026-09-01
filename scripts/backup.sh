#!/usr/bin/env bash

set -euo pipefail

project_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
backup_dir=${BACKUP_DIR:?Set BACKUP_DIR to a directory outside the zibs data volume.}

if [[ ! -d $backup_dir ]]; then
	printf 'Backup directory does not exist: %s\n' "$backup_dir" >&2
	exit 1
fi

backup_name="zibs-$(date -u +%Y%m%dT%H%M%SZ).db"
backup_path="$backup_dir/$backup_name"

if [[ -e $backup_path ]]; then
	printf 'Backup destination already exists: %s\n' "$backup_path" >&2
	exit 1
fi

cd "$project_dir"
docker compose run --rm --no-deps --user 0 \
	--volume "$backup_dir:/backups" \
	--entrypoint /usr/local/bin/zibs \
	zibs backup --database /data/zibs.db --output "/backups/$backup_name"

sha256sum "$backup_path" > "$backup_path.sha256"
chmod 600 "$backup_path" "$backup_path.sha256"
printf 'Backup complete: %s\n' "$backup_path"
