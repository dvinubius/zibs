# Service architecture

## Purpose and scope

zibs is a small, single-instance Go link shortener. It creates short links,
redirects visitors, and keeps link data in SQLite. The intended production
shape is deliberately modest: one application process, one local durable
database volume, and no distributed components in the request path.

This design is appropriate for the expected low traffic and for SQLite's
single-writer model. Running more than one application replica against the
same database volume is not supported.

## Runtime components

```text
                           Internet
                               |
                     TLS reverse proxy
                               |
                       public listener :8080
                               |
                    +----------v-----------+
                    | zibs Go process       |
                    |                        |
                    | public/admin handler   |
                    | SQLite link store      |
                    | expiry cleanup worker  |
                    +----------+-----------+
                               |
               durable local volume: /data/zibs.db

  private network or localhost ---> metrics listener :9091
                                            |
                                       Prometheus
                                            |
                     Alloy ---> Loki <--- Grafana (operator access)
                                            |
                             externally shared metrics-only dashboard
```

The reverse proxy is the only public network entry point. It terminates TLS
and forwards normal application traffic to the public listener. The metrics
listener is private and is scraped by Prometheus; it is never routed through
the public proxy.

Prometheus, Loki, Alloy, and Grafana are deployed as the production telemetry
stack. The service exposes the private metrics listener and emits structured
JSON logs to standard output; see [Observability](observability.md) for the
current metric set, dashboards, access boundaries, and operating runbooks.

## Application structure

The application is one Go executable using `net/http`. Its main runtime flow
is:

1. Open SQLite and ensure the `links` and `creation_tokens` tables exist.
2. Create an isolated Prometheus registry and register application, Go
   runtime, process, and SQLite-pool metrics.
3. Open the public and metrics listeners.
4. Start the expiry-cleanup worker.
5. Serve both HTTP servers under one shared cancellation context.
6. On `SIGINT` or `SIGTERM`, gracefully stop both servers, cancel cleanup, and
   then close the database.

The public server records structured request logs and HTTP metrics. The
metrics server serves only `GET /metrics` and intentionally bypasses request
logging and HTTP request instrumentation, so Prometheus scrapes do not skew
application telemetry.

## Request and data flow

### Link creation

`POST /links` requires a short-lived creation bearer token. The service
validates the JSON body and destination URL, atomically consumes one allowed
token use, creates an eight-character cryptographically random base-62 code,
and inserts the link into SQLite. SQLite's primary-key constraint is the
uniqueness authority; a rare collision is retried. The response carries the
new code and `usesLeft`, the token's remaining uses, which the same statement
that consumed the use returns.

Each link records its destination, creation time, expiry time, and redirect
count. New links have a 90-day lifetime.

### Redirect

`GET /{code}` performs one SQLite statement that increments the redirect count
and returns the link only when `expires_at` is later than the current time. A
missing or expired code returns `404`; the distinction is deliberately not
revealed to visitors. A live link returns a `302 Found` redirect to its stored
destination.

### Administration

An `ADMIN_TOKEN` environment variable protects the `/admin` route group. It
is used to list and delete links, issue creation tokens, list token metadata,
and revoke tokens. The administrator credential should be a high-entropy
secret supplied by deployment configuration, never a value committed to the
repository.

### Persistence and backup boundary

SQLite is stored on a durable local volume. Mount the database directory,
not just the main database file, to preserve possible SQLite sidecar files
when WAL mode is enabled. Backups must use a SQLite-aware consistent backup
method and be restored into a fresh instance as part of routine verification.

## HTTP surface

| Surface | Routes | Access |
|---|---|---|
| Public application | `GET /`, `GET /static/{file}`, `GET /health`, `GET /{code}` | Public |
| Link creation | `POST /links` | Creation bearer token |
| Administration | `/admin/links`, `/admin/tokens` | Admin bearer token |
| Telemetry | `GET /metrics` on `127.0.0.1:9091` by default; `0.0.0.0:9091` in Compose | Private network only |

The frontend is embedded in the binary and served from the same origin. It
sends a visitor's creation token in the `Authorization` header and keeps it in
that browser's `localStorage` so it need not be retyped on the same device
(ADR 0002, amendment 2026-09-07). The admin token never reaches a browser.

## Operational constraints

- Run exactly one zibs replica per SQLite volume.
- Keep the database on local durable storage, not an arbitrary network
  filesystem.
- Keep the reverse proxy, admin token, and metrics firewall boundary in place
  before public deployment.
- Treat short URLs as identifiers, not access-control secrets. Sensitive
  destinations need an authentication model outside this service.

Related decisions: [public and admin API](adr/0001-public-and-admin-api.md),
[token generation and expiration](adr/0002-token-generation-and-expiration.md),
and [dual HTTP servers](adr/0003-dual-server-setup.md).
