# ADR 0001: Separate public, creation, and administrative API access

**Status:** Accepted

## Context

Redirects must be publicly reachable, but listing links reveals destinations,
creation times, codes, and redirect counts. Deleting links and managing
creation access are administrative operations. A single public API surface
would expose destructive operations or encourage security through obscure
paths.

The service has no accounts or end-user authentication system. It needs a
small operational access model appropriate to one operator and occasional
visitor-created links.

## Decision

Use three access levels in the same public application server:

| Level | Routes | Protection |
|---|---|---|
| Public | `GET /`, `GET /static/{file}`, `GET /health`, `GET /{code}` | None; optionally restrict health at the network layer |
| Creation | `POST /links` | Short-lived, limited-use creation bearer token |
| Admin | `/admin/links` and `/admin/tokens` | `ADMIN_TOKEN` bearer token |

The administrator token is supplied through deployment secrets and compared in
constant time. It must not appear in logs, metrics, source control, or client
code. The creation token is the only credential accepted by `POST /links`;
the permanent admin token does not substitute for it.

Administrative routes remain same-origin for simplicity, but their protection
is explicit middleware rather than path obscurity. Reverse-proxy IP or VPN
restrictions may add defense in depth, but do not replace application-level
authorization.

## Consequences

- Public visitors can follow links and use the link-creation page only after
  receiving a temporary creation token out of band.
- Listing, deletion, issuance, and revocation are not exposed to anonymous
  callers.
- A compromised creation token has bounded lifetime and usage; a compromised
  admin token remains more serious and requires operational secret rotation.
- The design avoids adding account management while establishing a clear
  boundary for a future authentication system.

## Alternatives considered

**Keep `GET /links` and `DELETE /links/{code}` public.** Rejected because it
leaks link data and lets anyone delete content.

**Hide management endpoints at unguessable paths.** Rejected because a path is
not authorization.

**Add user accounts before deployment.** Rejected because it expands the
v1 scope substantially without a demonstrated need.
