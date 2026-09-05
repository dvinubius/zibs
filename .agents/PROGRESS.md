# Progress: refine-observability

## Current state

- Branch: `refine-observability`
- Status: Phase 4 is complete and production-validated.

## Working files

- `.agents/refine-observability-analysis.md`
- `.agents/refine-observability-plan.md`
- `.agents/deferred-observability-work.md`
- `.agents/dashboard-usage-guide.md`

## Review decisions recorded

- Public dashboard may show aggregate active-link count and should be an
  interesting, safe portfolio/learning artifact.
- Alert notifications should go to `zibs_alerts@dinubarbu.com`; a sender and
  secret-storage approach are intentionally deferred to Phase 8.
- A one-minute `increase()` with the current 30-second Prometheus scrape
  interval extrapolates sparse counter changes (for example, seven observed
  HTTP requests may render as fourteen). Use selected-period counter totals
  for dashboard summaries and private Loki logs for exact short-window checks.
- Validate the in-flight gauge with a disposable creation token and a streamed,
  deliberately incomplete request body. `curl --data-binary @-` buffers stdin
  before connecting; `--upload-file -` with chunked transfer holds the request
  in the application long enough for a scrape.

## Next step

Phase 5 — host health.

Inspect the VM filesystem mounts and introduce private,
least-privilege host metrics for capacity and storage health.
