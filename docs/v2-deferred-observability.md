# Deferred observability work

This document records deliberately postponed observability work. These are not
rejected ideas: revisit each only when its stated trigger appears. They have a
cost, expand a data boundary, or do not yet provide enough diagnostic value.

## Build and lifecycle identity

**Potential change:** expose a build-info metric with compile-time Git labels,
assert it in the telemetry smoke test, and add a private operator-dashboard
card.

**Revisit when:** deployments, operators, or environments become numerous
enough that deployment history no longer identifies the running version.

**Why deferred:** the service is currently a one-person, single-service
project. Deployment history is sufficient.

## Telemetry-stack health

**Potential change:** have Prometheus scrape itself, Loki, and Alloy privately;
add panels for scrape-target health and Loki/Alloy ingest, write, and drop
failures; and extend the telemetry smoke test to require those targets.

**Revisit when:** traffic or operational consequences justify diagnosing the
monitoring pipeline as a separate system.

**Why deferred:** the extra network attachment, scrape targets, dashboard
queries, metric-version maintenance, and smoke-test complexity are not
justified at the current scale. Existing zibs and node_exporter checks, Loki
readiness, and the end-to-end log check provide sufficient signal.

## Alerting

**Potential change:** choose an SMTP or transactional-email sender, keep its
credentials outside the repository, and add alerts for zibs availability,
failed Prometheus scrapes, SQLite busy/locked errors, and low disk space. Add
Caddy-down and unexpected-restart alerts only after their corresponding safe
edge and lifecycle metrics exist.

**Revisit when:** an external sender and credential-storage approach have been
chosen, and delivery, acknowledgement or silencing, and resolved notifications
can be tested end to end.

**Why deferred:** notification delivery is an external operational dependency.
Thresholds must be absolute and appropriate for low traffic, and normal
operation must not create repeated notifications.

## Caddy versus Go request duration

**Potential change:** enable carefully scoped Caddy access-duration telemetry
and compare it with `zibs_http_request_duration_seconds`.

**Revisit when:** there is a demonstrated TLS, reverse-proxy, network, or
upstream-connection latency concern that application timing cannot explain.

**Why deferred:** Go timing already includes the handler and SQLite work.
Caddy access logs introduce a second client-facing log stream, retention
questions, and a privacy review.

## HTTP-duration status-class label

**Potential change:** add a bounded `status_class` label to the HTTP-duration
histogram.

**Revisit when:** traffic is sufficient to make a latency comparison between
successful redirects and errors useful.

**Why deferred:** the existing histogram answers the immediate questions. A
new metric label changes the schema without solving a known problem.

## Exact maximum or named slowest database operation

**Potential change:** record an explicit bounded slow-operation event or log,
or maintain a purpose-built maximum value.

**Revisit when:** p95 or average latency suggests a regression and an
individual outlier needs identification.

**Why deferred:** histograms provide counts, averages, and quantiles, not an
exact maximum. Private Loki logs are the first investigation path for a slow
completed request.

## Request and response size metrics

**Potential change:** add request- or response-byte counters or histograms.

**Revisit when:** bandwidth cost, unusually large API payloads, or response
bloat becomes a real concern.

**Why deferred:** the small API and static frontend have no present size
problem.

## SQLite one-connection pool policy

**Potential change:** call `SetMaxOpenConns(1)` and choose an intentional idle
connection policy.

**Revisit when:** a focused concurrent, temporary-SQLite integration test has
been run and observed waits, busy errors, or latency justify changing runtime
behaviour.

**Why deferred:** the refinement intentionally keeps the current unlimited
pool and observes it first.

## Container restart count

**Potential change:** collect container-runtime metrics through a carefully
scoped runtime collector.

**Revisit when:** process start time is no longer enough to distinguish an
application restart from a container replacement or quantify restart frequency.

**Why deferred:** `process_start_time_seconds` already provides useful restart
context. A runtime collector expands host and Docker visibility.

## Request IDs

**Potential change:** generate a request ID, attach it to request context, and
include it in relevant structured logs.

**Revisit when:** normal request processing emits multiple events that need a
join key.

**Why deferred:** a normal request currently produces one completion log line.
If added, the ID must remain a JSON field, never a Prometheus or Loki label.

## Caddy-down alert

**Potential change:** add a safe Caddy availability metric and an alert.

**Revisit when:** a safe edge-health metric is available.

**Why deferred:** Caddy does not currently expose a collected safe health
metric. Application-target health alone cannot establish public-edge health.

## Per-component telemetry storage panels

**Potential change:** report exact storage consumption for each telemetry
component or volume.

**Revisit when:** host-level free-space monitoring identifies a real
attribution need.

**Why deferred:** on a single VM, host filesystem free space is the reliable
first warning. Per-volume values can be misleading across Docker's storage
layout.

## Expanded alert set

**Potential change:** add low-disk, Caddy, and restart alerts beyond the
initial availability, scrape-failure, and SQLite-busy alerts.

**Revisit when:** the required host, edge, and lifecycle metrics exist and
each alert can be tested end to end.

**Why deferred:** every alert needs a dependable signal and a tested delivery
path before it is enabled.

## Public-dashboard edge rate limiting

**Potential change:** rate-limit the narrow Caddy routes that serve the public
Grafana dashboard and its anonymous shared-dashboard API.

**Revisit when:** public traffic or query load warrants a limit.

**Why deferred:** the dashboard is intentionally small, uses saved aggregate
queries, and has a fixed time range. Any future limit must not block required
Grafana assets or public-dashboard API requests.
