# ADR 0002: Use cryptographic codes and bounded creation tokens

**Status:** Accepted, amended 2026-08-30 (see [Amendment](#amendment-2026-08-30))

## Context

The initial numeric short-code space was small and used non-cryptographic
randomness. Public codes should be difficult to enumerate casually and must
remain unique under concurrent inserts. The service also needs to authorize
link creation without exposing a permanent administrator credential in the
browser.

## Decision

### Short-link codes

Generate fixed-length, eight-character base-62 codes using `crypto/rand`.
The alphabet is uppercase letters, lowercase letters, and digits, giving
`62^8` possible codes (about 218 trillion). Generation uses rejection sampling
to avoid modulo bias.

SQLite's `links.code` primary key remains the final uniqueness authority. The
service inserts first and retries when SQLite reports a unique or primary-key
collision. It does not rely on a pre-insert availability check.

Short codes are identifiers, not authorization secrets. A sensitive
destination requires its own access control.

### Creation tokens

Only an administrator can issue creation tokens. A token consists of a random
identifier and a 32-byte cryptographically random secret, represented as a
URL-safe string with a `ust_` prefix. The raw token is returned exactly once.

SQLite stores only a SHA-256 hash of the raw token plus metadata: label,
creation time, expiry, maximum uses, use count, and optional revocation time.
Each creation request atomically increments the use count only when the token
hash matches, the token is not revoked, it has not expired, and it has remaining
uses.

Administrators choose a TTL from 1 to 1,440 minutes and a use limit from 1 to
100. Token listing returns metadata only; it never returns raw tokens or their
hashes. Issuing a token responds with `Cache-Control: no-store`.

### Link expiration

Links receive a fixed 30-day expiry. Redirect-time filtering enforces expiry
immediately, and a startup plus daily cleanup sweep removes expired rows.

## Consequences

- Code collisions are extremely unlikely and safely retried when they occur.
- Creation tokens can be given to visitors without persisting secrets in the
  browser or granting permanent administrative control.
- Operators can revoke a token early and can observe use counts without
  recovering its secret.
- Token compromise is time- and use-bounded, but tokens still require secure
  out-of-band delivery.

## Alternatives considered

**Numeric or `math/rand` short codes.** Rejected because of a smaller,
predictable namespace.

**Store raw creation tokens.** Rejected because database disclosure would then
immediately grant creation access.

**Use the admin token directly in the frontend.** Rejected because it would
expose permanent, broad administrative authority to a browser.

**Use link cleanup alone for expiry.** Rejected because a delayed or failed
cleanup run could leave expired links live.

## Amendment 2026-08-30

Creation tokens no longer expire by time. The product positioning changed to
"write me an email and get a personal token", so a
time-boxed token would force repeat requests without a matching security
benefit for this threat model. Tokens remain use-bounded (1 to 100 uses) and
revocable, which keeps compromise bounded; the `ttlMinutes` field and the
`expires_at` column on `creation_tokens` were removed. The fixed link expiry
was also lengthened from 30 to 90 days.
