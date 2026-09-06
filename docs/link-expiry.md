# Link validity and expiration

## Policy

Every newly created zibs link expires 90 days after creation. The expiry is
stored as a UTC Unix timestamp in SQLite and is returned in administrative
link listings.

Expiry controls link validity, not merely data retention: after the expiry
time, a visitor must no longer be redirected even if cleanup has not run.

## Enforcement

The redirect operation is the correctness boundary. It uses a single SQLite
`UPDATE ... RETURNING` statement that:

1. finds the requested code only when `expires_at` is later than the current
   UTC time;
2. increments its redirect count; and
3. returns the destination for the redirect.

If no qualifying row exists, zibs returns `404 Not Found`. Expired and unknown
codes deliberately have the same response, so the service does not disclose
whether a code existed in the past.

## Cleanup

Physical cleanup removes rows whose `expires_at` is now or earlier. It runs:

- once at application startup; and
- every 24 hours while the process remains running.

The cleanup worker shares the application's cancellation lifecycle. During
graceful shutdown, its context is canceled and the process waits for it to
finish before closing the database.

Cleanup is maintenance, not the validity mechanism. Missing a sweep can retain
expired rows temporarily but cannot make an expired link redirect.

## Observability and operations

The service exposes:

- `zibs_expired_links_deleted_total` for the cumulative number of
  removed rows; and
- `zibs_expiry_cleanup_duration_seconds` with a `result` label for
  sweep timing and failures.

The cleanup worker logs successful sweeps and failures. Alerting is not
configured yet; until it is, review cleanup failures in the private operator
dashboard and Loki logs. A successful redirect-time expiry check remains the
primary availability and correctness safeguard. See [deferred observability
work](v2-deferred-observability.md) for the criteria before alerting is added.

## Tests and deployment checks

Expiry behavior should remain covered by focused tests for:

- a future-expiry link redirecting normally;
- a past-expiry link returning `404`;
- cleanup deleting expired links while retaining live ones; and
- cleanup canceling cleanly during shutdown.

For a fresh database, the current schema creates the `expires_at` column. A
database created before expiry support is not automatically migrated; plan a
tested migration or begin with a fresh database before deployment.
