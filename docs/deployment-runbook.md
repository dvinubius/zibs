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
It also reads the local checked-out Git revision and passes it to the Docker
build as the `zibs_build_info` commit label; `.git` is never copied to the VM
or image. `ZIBS_BUILD_VERSION` optionally supplies a release-version label and
defaults to `dev`.

With `DEPLOY_ALL=1`, it force-recreates profile containers without removing
named volumes, waits for Loki and Alloy readiness, then runs the telemetry
smoke test. See [Observability](observability.md) for what that test proves.

### Convert a Grafana V2 layout export

Grafana 13.0.x can emit V2 dashboard JSON from **Save dashboard** even when
the drawer labels it Classic. Do not deploy that V2 JSON through this project's
Classic file provider. Instead, use its V2 grid layout to update only the
existing Classic dashboard's panel positions:

```bash
./scripts/apply-grafana-v2-layout.sh \
  ~/Downloads/zibs-operator-v2.json \
  grafana/dashboards/operator.json
git diff -- grafana/dashboards/operator.json
```

The first argument may be either Grafana's bare V2 dashboard body or its V2
Resource envelope. The converter requires a `GridLayout`, verifies that every
`elements.panel-N` item maps exactly once to the target Classic dashboard's
panel `id: N`, and changes only `gridPos`. It refuses tabs, rows, missing
panels, duplicate IDs, and malformed coordinates without changing the target.
It reformats the target JSON, so review the diff for semantic changes before
continuing.

### Update only Grafana dashboards

To upload only the provisioned dashboard definitions, without building or
recreating any containers, run:

```bash
DEPLOY_DASHBOARDS=1 ./scripts/deploy.sh
```

This validates and uploads only `grafana/dashboards/operator.json` and
`grafana/dashboards/public-metrics.json` to
`/opt/zibs/grafana/dashboards/` on the VM. Grafana sees the files through its
read-only bind mount and applies them within its 30-second provisioning scan;
the script makes the directory traversable and dashboard files world-readable
for Grafana's non-root container user, then refreshes their modification times
so that scan reliably detects the update. When changing an existing provisioned
dashboard, also increase its top-level JSON `version`; Grafana does not
overwrite a newer database dashboard with an equal or older file version. No
Grafana restart is needed.
This mode requires `DEPLOY_HOST` (and `DEPLOY_SSH_KEY` when applicable), but
does not require `ADMIN_TOKEN` or `GRAFANA_ADMIN_PASSWORD` and does not write
the VM `.env` file. It also requires local `jq` to validate the JSON. It cannot
be combined with `DEPLOY_ALL=1`. The script accepts only the project’s Classic
dashboard model, with the expected dashboard UID; it rejects a V2 Resource
export before connecting to the VM. Grafana 13.0.x has a known regression where
the **Save dashboard** drawer can emit V2 JSON even when its **Classic** model
option is selected; do not deploy that output directly. See the
[V2-layout conversion procedure](#convert-a-grafana-v2-layout) before deploying
layout changes.

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
