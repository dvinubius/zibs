# ADR 0003: Serve metrics on a separate private HTTP server

**Status:** Accepted

## Context

Prometheus needs an HTTP endpoint to scrape metrics. Exposing that endpoint on
the public application listener would reveal operational information and make
it easy to accidentally publish through the TLS reverse proxy. Scrape traffic
is infrastructure traffic and should not inflate user-facing request logs or
HTTP metrics.

## Decision

Run two `http.Server` instances in the same Go process:

| Server | Default listener | Handler | Access |
|---|---|---|---|
| Application | `:8080` | Public, creation, and admin routes with request telemetry | Reverse proxy / public internet |
| Metrics | `127.0.0.1:9091` by default; `0.0.0.0:9091` in Compose | Only `GET /metrics` from the service-specific Prometheus registry | Localhost or private network |

The servers share the same process lifetime and root cancellation context, but
their handlers are separate. The metrics server uses `promhttp.HandlerFor` with
the private registry and is not wrapped in request logging or HTTP-metrics
middleware.

If either server stops unexpectedly, the shared server context is canceled so
its peer also shuts down. On `SIGINT` or `SIGTERM`, both servers receive a
five-second graceful-shutdown window before the database closes.

In Docker Compose, the metrics port must not be published on the host. Only
the private Prometheus network may reach it. The public reverse proxy must not
route `/metrics`.

## Consequences

- Prometheus can collect telemetry without public exposure of metrics.
- Application metrics describe user-facing traffic rather than scrape volume.
- One process keeps the operational model simple while creating a clear network
  security boundary.
- Deployment must configure both listeners and ensure Prometheus can reach the
  private one.

## Alternatives considered

**Expose `/metrics` on the application server.** Rejected because it is too
easy to publish operational data and to pollute application measurements.

**Run a separate metrics-exporter process.** Rejected because the application
already owns the relevant counters, histograms, and database-pool state.

**Derive metrics from logs.** Rejected because it is delayed and brittle, and
does not provide reliable latency distributions.
