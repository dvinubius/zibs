# Deployment runbook

This runbook deploys the current working tree to a prepared single Docker host.
It does not provision a VM, install Docker, configure DNS, or update host
packages.

## Prerequisites

- A reachable Linux VM with Docker and Docker Compose installed.
- SSH access for the deployment user (default: `root`).
- `zibs.app` DNS pointed at the VM and inbound TCP 80/443 plus UDP 443 allowed
  before the production profile is started.
- A local, untracked `.env.production` containing `DEPLOY_HOST`,
  `DEPLOY_SSH_KEY`, `ADMIN_TOKEN`, and a distinct high-entropy
  `GRAFANA_ADMIN_PASSWORD`. `DEPLOY_USER` is optional and defaults to `root`.

Do not commit `.env.production`. The script intentionally does not install or
upgrade host packages.

## Deploy

Load the deployment variables in the shell:

```bash
set -a
source .env.production
set +a
```

For an application-only deployment, which exposes zibs only on VM loopback:

```bash
./scripts/deploy.sh
```

For the complete production profile, including TLS reverse proxy and the
observability stack:

```bash
DEPLOY_ALL=1 ./scripts/deploy.sh
```

The script synchronizes source to `/opt/zibs`, excluding local databases,
environment files, documentation, and Git metadata. It writes the required
secrets to `/opt/zibs/.env` with mode `0600`, builds remotely, starts the
selected Compose services, and verifies `http://127.0.0.1:8080/health`.

With `DEPLOY_ALL=1`, it force-recreates profile containers without removing
named volumes, waits for Loki and Alloy readiness, then runs the telemetry
smoke test. See [Observability](observability.md) for what that test proves.

## Verify

From the VM, confirm the service is healthy:

```bash
curl --fail http://127.0.0.1:8080/health
cd /opt/zibs
docker compose --profile production ps
```

After DNS and certificates have settled, confirm the public endpoint:

```bash
curl --fail https://zibs.app/health
```

For a full-profile deployment, also run:

```bash
cd /opt/zibs
./scripts/telemetry-smoke-test.sh
```

## Roll back

There is no image registry or release tag in this intentionally small setup.
To roll back application code, check out the previously known-good Git commit
locally and redeploy it with the same procedure. Named volumes are preserved,
so this leaves the current SQLite data and Grafana/telemetry state intact.

If a database restore is needed, stop and assess the incident first; use the
isolated restore verification procedure in the [database backup runbook](database-backup-runbook.md).
Do not overwrite the live volume with an unverified backup.
