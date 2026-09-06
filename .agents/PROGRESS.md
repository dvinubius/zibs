# Progress: refine-observability

## Current state

- Branch: `refine-observability`
- Status: Phase 7 is complete and production-validated. Phases 4 and 6 are
  deferred until the service has materially higher usage.

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

Phase 8 — alerting.

Choose an SMTP or transactional-email sender for `zibs_alerts@dinubarbu.com`
and a credential-storage approach outside the repository. Then add and test
low-traffic-aware alerts for zibs availability, failed Prometheus scrapes,
SQLite busy/locked errors, and low disk space. Phases 4 and 6 remain deferred;
see `.agents/deferred-observability-work.md`.
