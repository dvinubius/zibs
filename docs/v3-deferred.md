# Deferred Work for v3

## Off-host R2 backup automation

Create a private Cloudflare R2 bucket for zibs backup pairs (db + sha256). 
Do not make it public or serve application traffic from it.

Add a production-host scheduled job with an overlap lock. It must call the
SQLite-aware backup wrapper, run `integrity-check` against the newly created
snapshot on Hetzner-One, then upload the `.db` and `.sha256` files under one
timestamped R2 object prefix.

Treat successful uploads of both objects as the source-side transfer result.
On any failure, retain local staging for investigation, write non-sensitive
diagnostics, and signal the failure to the chosen operations channel.

## Topology improvements

### Network segmentation

The current `zibs-edge` connects zibs, Prometheus, Grafana. Separate ingress,
application scraping, and private telemetry queries while retaining zibs-owned Grafana.


- Existing Hetzner-One-owned `zibs-edge` for Caddy and zibs.
- A separate Hetzner-One-owned dashboard edge for Caddy and zibs Grafana,
  consumed externally by zibs.
- A zibs-owned private metrics network for zibs and Prometheus.
- A zibs-owned internal observability network for Prometheus, Alloy, Loki,
  and zibs Grafana.

Prometheus needs both its scrape and query paths. Grafana needs its private
query paths and Caddy's public-dashboard path. Hetzner-One's Grafana and
Hooklook's Grafana need no attachment to these zibs networks.

Docker bridges are bidirectional connectivity, not per-port firewalls.
`expose` does not restrict listeners. Keep the public application off the
private telemetry query network and verify replacement connections before
removing old attachments. Preserve Hetzner-One's ownership of both existing
application edge networks.

### Application logs and verification

Keep the zibs Alloy/Loki pipeline and retained data. A later hardening change
can restrict Docker discovery by both Compose project and service. Filtering
limits collection, not the host-wide access granted by the Docker socket;
changing that trust boundary is separate work.

zibs smoke tests continue verifying application health, scraping, log
ingestion, Grafana, both data sources, and its dashboards. Hetzner-One verifies
its own host monitoring and Grafana independently. Keep deployment scripts,
network assumptions, credentials, and runbooks consistent with each change.

### Completion criteria

- After segmentation, zibs's application edge, owned by Hetzner-One, carries 
  only Caddy and zibs; its Grafana is reachable through the separate dashboard edge.


## Observability

This document records deliberately postponed observability work. These are not
rejected ideas: revisit each only when its stated trigger appears. They have a
cost, expand a data boundary, or do not yet provide enough diagnostic value.

### Telemetry-stack health

**Potential change:** have Prometheus scrape itself, Loki, and Alloy privately;
add panels for scrape-target health and Loki/Alloy ingest, write, and drop
failures; and extend the telemetry smoke test to require those targets.

**Revisit when:** traffic or operational consequences justify diagnosing the
monitoring pipeline as a separate system.

**Why deferred:** the extra network attachment, scrape targets, dashboard
queries, metric-version maintenance, and smoke-test complexity are not
justified at the current scale. Existing zibs and node_exporter checks, Loki
readiness, and the end-to-end log check provide sufficient signal.

### Traces

**Potential change:** add private OpenTelemetry tracing: a root span for each
incoming HTTP request, child spans for database and future outbound or
asynchronous work, and trace IDs in structured logs. Send traces through a
private collector to a trace backend, with Grafana providing the operator-only
query interface.

**Revisit when:** a request crosses a meaningful external or asynchronous
boundary—such as an API, queue, worker, webhook, or multiple service
instances—or an intermittent latency problem cannot be isolated with metrics
and logs.

**Why deferred:** a request currently remains within one Go process and its
local SQLite database, so latency metrics and structured logs provide enough
diagnostic detail. Tracing would add collection, storage, retention, sampling,
and sensitive-attribute controls without proportionate operational value.

### HTTP-duration status-class label

**Potential change:** add a bounded `status_class` label to the HTTP-duration
histogram.

**Revisit when:** traffic is sufficient to make a latency comparison between
successful redirects and errors useful.

**Why deferred:** the existing histogram answers the immediate questions. A
new metric label changes the schema without solving a known problem.

### Exact maximum or named slowest database operation

**Potential change:** record an explicit bounded slow-operation event or log,
or maintain a purpose-built maximum value.

**Revisit when:** p95 or average latency suggests a regression and an
individual outlier needs identification.

**Why deferred:** histograms provide counts, averages, and quantiles, not an
exact maximum. Private Loki logs are the first investigation path for a slow
completed request.

### Request and response size metrics

**Potential change:** add request- or response-byte counters or histograms.

**Revisit when:** bandwidth cost, unusually large API payloads, or response
bloat becomes a real concern.

**Why deferred:** the small API and static frontend have no present size
problem.

### SQLite one-connection pool policy

**Potential change:** call `SetMaxOpenConns(1)` and choose an intentional idle
connection policy.

**Revisit when:** a focused concurrent, temporary-SQLite integration test has
been run and observed waits, busy errors, or latency justify changing runtime
behaviour.

**Why deferred:** the refinement intentionally keeps the current unlimited
pool and observes it first.

### Container restart count

**Potential change:** collect container-runtime metrics through a carefully
scoped runtime collector.

**Revisit when:** process start time is no longer enough to distinguish an
application restart from a container replacement or quantify restart frequency.

**Why deferred:** `process_start_time_seconds` already provides useful restart
context. A runtime collector expands host and Docker visibility.

### Request IDs

**Potential change:** generate a request ID, attach it to request context, and
include it in relevant structured logs.

**Revisit when:** normal request processing emits multiple events that need a
join key.

**Why deferred:** a normal request currently produces one completion log line.
If added, the ID must remain a JSON field, never a Prometheus or Loki label.

### Per-component telemetry storage panels

**Potential change:** report exact storage consumption for each telemetry
component or volume.

**Revisit when:** host-level free-space monitoring identifies a real
attribution need.

**Why deferred:** on a single VM, host filesystem free space is the reliable
first warning. Per-volume values can be misleading across Docker's storage
layout.

### Build and lifecycle identity

**Potential change:** expose a build-info metric with compile-time Git labels,
assert it in the telemetry smoke test, and add a private operator-dashboard
card.

**Revisit when:** deployments, operators, or environments become numerous
enough that deployment history no longer identifies the running version.

**Why deferred:** the service is currently a one-person, single-service
project. Deployment history is sufficient.