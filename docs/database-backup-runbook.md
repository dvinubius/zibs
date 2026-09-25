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
