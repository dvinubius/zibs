# Shared Caddy network cutover

## Ownership after the 2026-09-24 cleanup

Hetzner-One, deployed at `/opt/caddy` with Compose project name `caddy`, owns
`hooklook-edge` and `zibs-edge`. Both application projects consume their
respective edge networks with `external: true`.

`zibs-edge` currently connects Caddy, zibs, Grafana, Prometheus, and
node_exporter. The internal `zibs_observability` network remains unchanged.
This cleanup transfers network ownership; the additional segmentation in
[the v2 topology plan](v2-topology-improvements.md) remains future work.
Caddy access/error log collection belongs to Hetzner-One. The zibs Alloy
pipeline still collects only zibs logs.

## Cutover procedure

1. Validate both Compose configurations. Save both live Compose files, the
   zibs deploy script, network membership, container IDs/start times/mounts,
   and the Caddyfile checksum before making changes.
2. Bootstrap `zibs-edge` with the ownership labels that match Hetzner-One
   Compose: `com.docker.compose.project=caddy` and
   `com.docker.compose.network=zibs-edge`. This allows a network-only cutover
   without recreating shared Caddy. On a fresh host, Hetzner-One Compose
   creates the network normally.
3. Connect the running zibs, Grafana, Prometheus, and node_exporter containers
   to `zibs-edge`, preserving each service-name DNS alias. Connect Caddy last.
   Keep every old attachment while validating public health and dashboard API.
4. Disconnect Caddy from `zibs_app-edge` and verify zibs and the public
   dashboard through the new path. Then disconnect the four zibs services
   from the old network. Install the revised Compose files and zibs deploy
   script. Do not replace the Caddyfile or rebuild images.
5. Run telemetry smoke tests, check public routes and protected boundaries,
   and compare container IDs, start times, and mounts (order-independent).
   Keep the empty `zibs_app-edge` network temporarily for rollback.

Hot network changes are persistent Docker configuration and survive container
or daemon restarts. Existing Compose configuration-hash labels still reflect
the containers' original creation. A subsequent normal Compose deployment may
recreate changed containers; no labels were modified to hide that difference.
Do not use `docker compose down` for this migration or remove named volumes.

## Rollback

The production backups are under
`/opt/caddy/rollback/zibs-edge-cleanup-20260924`. On the VPS, reconnect the old
network before restoring the files:

```bash
for service in zibs prometheus node-exporter grafana; do
  docker network connect --alias "$service" zibs_app-edge "zibs-$service-1"
done
docker network connect --alias caddy zibs_app-edge caddy-caddy-1
backup=/opt/caddy/rollback/zibs-edge-cleanup-20260924
cp "$backup/caddy-compose.yaml" /opt/caddy/compose.yaml
cp "$backup/zibs-compose.yaml" /opt/zibs/compose.yaml
cp "$backup/zibs-deploy.sh" /opt/zibs/scripts/deploy.sh
curl --fail https://zibs.app/health
curl --fail https://hooklook.app/health
```

If a container already has its old attachment, inspect it and skip that
connection. Verify the public dashboard and `/opt/zibs/scripts/telemetry-smoke-test.sh`
before optionally disconnecting the new network. No data or certificate volume
is replaced. Restore the corresponding local configuration before redeploying.

## Verified outcome

The cutover completed on 2026-09-24 without restarting any container. All 80
public health samples during the final transition returned 200. zibs and
Hooklook health, both gallery paths, the public dashboard page and API, and
an existing short-link redirect passed. Metrics, login, dashboards, and
datasource routes remained blocked publicly with 404 responses. All telemetry
smoke tests passed, and container IDs, start times, mounts, and Caddyfile
checksum were unchanged.

An initial order-sensitive comparison of Docker mounts triggered a rollback
even though only array order differed. The comparison was corrected and the
cutover repeated successfully. Both attempts' health logs and container
inventories are retained in the backup directory. Compose validation, a mocked
full deployment, and live Compose dry runs passed. No application code,
images, packages, database schema, or logging configuration changed.
