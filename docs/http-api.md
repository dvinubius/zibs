# HTTP API

The public application listens on port 8080. Protected routes expect
`Authorization: Bearer <token>`. Use JSON request bodies where shown.

## Routes

| Method and path | Access | Purpose |
|---|---|---|
| `GET /` | Public | Link-creation page |
| `GET /static/{file}` | Public | Embedded frontend asset |
| `GET /health` | Public | Health check |
| `GET /{code}` | Public | Redirect to a live link |
| `POST /links` | Creation token | Create a short link |
| `GET /admin/links` | Admin token | List links |
| `DELETE /admin/links/{code}` | Admin token | Delete a link |
| `POST /admin/tokens` | Admin token | Issue a creation token |
| `GET /admin/tokens` | Admin token | List token metadata |
| `DELETE /admin/tokens/{id}` | Admin token | Revoke a creation token |

`GET /health` returns `200 OK` with a short plain-text body. The private
Prometheus endpoint is not part of this API; see [Observability](observability.md).

## Create a link

```bash
curl -X POST http://localhost:8080/links \
  -H "Authorization: Bearer <creation-token>" \
  -H 'Content-Type: application/json' \
  -d '{"url":"example.com/articles?tag=go"}'
```

Successful creation returns `201 Created`:

```json
{"code":"Ab3dE5fG","usesLeft":4}
```

`usesLeft` is how many uses the creation token has after this request; the
frontend uses it to warn before a token runs out.

The destination may omit its scheme, in which case zibs stores it as `https`.
Only absolute `http` and `https` URLs are allowed. The JSON body must contain
only `url`; malformed JSON, an invalid URL, or an exhausted, revoked, or unknown
creation token returns a clear 4xx response.

New links expire 90 days after creation. `GET /{code}` returns `302 Found` for
a live code, and `404 Not Found` for either a missing or expired code.

## Administration

Issue a creation token with an admin bearer token:

```bash
curl -X POST http://localhost:8080/admin/tokens \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"label":"local demo","maxUses":5}'
```

This returns `201 Created`, `Cache-Control: no-store`, and the token secret
exactly once:

```json
{
  "id":"token-id",
  "label":"local demo",
  "createdAt":"2026-08-31T12:00:00Z",
  "maxUses":5,
  "useCount":0,
  "token":"zib_<secret>"
}
```

`label` must contain 1–100 characters and `maxUses` must be 1–100. The service
stores only a SHA-256 hash of the secret. `GET /admin/tokens` returns metadata
without the secret or hash; `DELETE /admin/tokens/{id}` revokes a token and
returns `204 No Content`. Creation tokens do not expire, but each authorizes
only its configured number of requests.

`GET /admin/links` returns link objects with `code`, `url`, `createdAt`,
`expiresAt`, and `redirectCount`. `DELETE /admin/links/{code}` returns `204 No
Content`; a missing resource returns `404 Not Found`.

## Authentication and errors

The `ADMIN_TOKEN` environment variable is the permanent operator credential.
Creation tokens are limited-use credentials issued by the administrator. Never
put either token in a URL or in source control, and never put the admin token
in browser storage. The creation page does keep a visitor's own creation token
in that browser's `localStorage`, so it need not be retyped on that device —
see [frontend](frontend.md). Missing or invalid credentials receive an
authorization failure; malformed requests receive `400`; unexpected failures
receive `500`.
