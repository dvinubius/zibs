# Documentation

zibs is intentionally small, but its production shape has a few important
boundaries. These documents describe the implementation and how to operate it.

## Service

- [Service architecture](architecture.md) — application design, data flow, and constraints.
- [HTTP API](http-api.md) — routes, authentication, request bodies, and responses.
- [Creation frontend](frontend.md) — browser flow and token-handling guarantees.
- [Link expiry](link-expiry.md) — expiry semantics and cleanup.

## Operations

- [Deployment runbook](deployment-runbook.md) — prepare, deploy, verify, and roll back the VM deployment.
- [Database backup runbook](database-backup-runbook.md) — create, copy, and restore-verify SQLite backups.
- [Deployment architecture](deployment-architecture.md) — public request path, networks, volumes, and security boundaries. It deliberately excludes telemetry.
- [Observability](observability.md) — metrics, logs, the Prometheus/Alloy/Loki/Grafana stack, smoke tests, and the operator dashboard.
- [Manual diagnostics](diagnostics.md) — per-component checks of the deployed service and telemetry chain for when the smoke test is not enough.
- [Deferred observability work](v2-deferred-observability.md) — intentionally postponed telemetry and alerting work, with the conditions for revisiting it.

## Decisions

- [ADR 0001: public and administrative API](adr/0001-public-and-admin-api.md)
- [ADR 0002: token generation and expiration](adr/0002-token-generation-and-expiration.md)
- [ADR 0003: dual-server setup](adr/0003-dual-server-setup.md)
