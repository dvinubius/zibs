# Database backup runbook

zibs stores its SQLite database in the `zibs-data` Docker volume. Backups must
be consistent SQLite snapshots, copied off the VM, and restore-verified.

## Create a backup

On the VM, choose a pre-existing directory outside Docker's `zibs-data` volume.
That directory should be copied or mounted to storage outside the VM; another
directory on the same VM does not protect against VM loss.

```bash
ssh root@"$DEPLOY_HOST"
cd /opt/zibs
BACKUP_DIR=/var/backups/zibs ./scripts/backup.sh
```

The script runs `zibs backup` in a one-off container against the live volume.
That command uses SQLite `VACUUM INTO` to create a transactionally consistent
snapshot. It writes a timestamped `.db` file plus a SHA-256 `.sha256` file,
sets both to mode `0600`, and never overwrites an existing backup.

Copy both files to separate storage. Verify the checksum after each copy:

```bash
sha256sum --check zibs-<timestamp>.db.sha256
```

Do not directly copy a live `zibs.db` file.

## Restore verification

Verify every backup before relying on it for recovery. Never restore a test
backup over the production database. Instead, copy it to a fresh local
directory and start an isolated container on a different port.

After loading the same local `.env.production` variables used for deployment:

```bash
backup_file="$HOME/backups/zibs/zibs-<timestamp>.db"
restore_dir=$(mktemp -d)
install -m 600 "$backup_file" "$restore_dir/zibs.db"

docker build -t zibs-restore-test .
docker run --detach --rm \
  --name zibs-restore-test \
  --user 0 \
  --read-only \
  --tmpfs /tmp \
  --env ADMIN_TOKEN="$ADMIN_TOKEN" \
  --publish 127.0.0.1:18080:8080 \
  --volume "$restore_dir:/data" \
  zibs-restore-test
```

Check a known live short code:

```bash
curl -I http://127.0.0.1:18080/<code>
```

It should return `302 Found` with the expected `Location` header. Optionally,
inspect administrative records through `GET /admin/links` with the admin bearer
token. When the check is reviewed, stop only the isolated container:

```bash
docker stop zibs-restore-test
```

Keep the restore directory until the drill is recorded. This test does not
modify production data.
