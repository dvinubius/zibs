# Manual diagnostics

The [telemetry smoke test](observability.md#telemetry-smoke-test) verifies the
whole metrics-and-logs path in one pass. When it fails — or when you need to
look at one component in isolation — the commands below check each link of the
chain individually.

All commands run from an operator machine with the deployment variables
loaded:

```bash
set -a
source .env.production
set +a
```

They connect as `root@$DEPLOY_HOST` using `$DEPLOY_SSH_KEY`, matching the
[deployment runbook](deployment-runbook.md). The telemetry services publish no
host ports, so everything goes through SSH and the VM's Docker networks.

## Application health

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" \
  'curl --fail --silent http://127.0.0.1:8080/health'
```

After DNS and certificates have settled, the public path should agree:

```bash
curl --fail https://zibs.app/health
```

## Container status

`ps` shows process-level state only — this Compose file defines no Docker
healthcheck blocks, so "running" does not prove readiness:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" \
  'cd /opt/zibs && docker compose --profile production ps'
```

Inspect recent startup or connection errors for a suspect service:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" \
  'cd /opt/zibs && docker compose --profile production logs --tail=100 loki alloy'
```

## Prometheus: are the zibs and node targets up?

Query Prometheus from inside its own container. Both target values should be
`1`; `node` is the private node_exporter target:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" \
  'cd /opt/zibs && docker compose --profile production exec -T prometheus \
  wget -qO- "http://127.0.0.1:9090/api/v1/query?query=up%7Bjob%3D~%22zibs%7Cnode%22%7D"'
```

List all scrape targets and their last errors:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" \
  'cd /opt/zibs && docker compose --profile production exec -T prometheus \
  wget -qO- http://127.0.0.1:9090/api/v1/targets'
```

## Host filesystem headroom

The operator dashboard's root-filesystem panels should match the VM's root
filesystem. Compare directly without exposing node_exporter outside Docker:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" 'df -B1 /'
```

The only expected `node_filesystem_*` series for capacity panels has
`mountpoint="/"`; Docker overlays, pseudo filesystems, and `/boot/efi` are
excluded. To verify the series through private Prometheus:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" \
  'cd /opt/zibs && docker compose --profile production exec -T prometheus \
  wget -qO- "http://127.0.0.1:9090/api/v1/query?query=node_filesystem_avail_bytes%7Bjob%3D%22node%22%2Cmountpoint%3D%22%2F%22%7D"'
```

## Loki and Alloy readiness

Loki and Alloy sit on the internal `observability` network with no host
ports. Query their readiness endpoints from the VM host by container IP:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" '
  cd /opt/zibs

  loki_id=$(docker compose ps -q loki)
  loki_ip=$(docker inspect -f "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}" "$loki_id")
  curl --fail --silent --show-error "http://$loki_ip:3100/ready"

  alloy_id=$(docker compose ps -q alloy)
  alloy_ip=$(docker inspect -f "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}" "$alloy_id")
  curl --fail --silent --show-error "http://$alloy_ip:12345/-/ready"
'
```

A successful exit means both are ready.

## Query recent zibs logs from Loki

Confirms the full log path (zibs stdout → Alloy → Loki) end to end:

```bash
ssh -i "$DEPLOY_SSH_KEY" "root@$DEPLOY_HOST" '
  cd /opt/zibs
  loki_id=$(docker compose ps -q loki)
  loki_ip=$(docker inspect -f "{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}" "$loki_id")

  curl --fail --silent --show-error --get \
    "http://$loki_ip:3100/loki/api/v1/query_range" \
    --data-urlencode '\''query={service="zibs"}'\'' \
    --data-urlencode '\''limit=20'\'' \
    --data-urlencode '\''direction=backward'\''
'
```

An empty result with ready components usually means Alloy is not discovering
the zibs container; check its logs and the Docker-socket configuration in
[`config.alloy`](../config.alloy).

## Grafana

Grafana is deliberately loopback-only. Open a tunnel and use the
[operator dashboard runbook](observability.md#operator-dashboard-runbook):

```bash
ssh -i "$DEPLOY_SSH_KEY" -L 3000:127.0.0.1:3000 "root@$DEPLOY_HOST"
```

Keep the session open and visit `http://localhost:3000`.

## Reading the JSON responses

Pipe any of the Prometheus or Loki API responses into `jq .` for readable
output — for example, just each target's labels and value from an instant
query:

```bash
... | jq '.data.result[] | {labels: .metric, value: .value}'
```
