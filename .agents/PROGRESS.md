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
- Keep the normal-latency panel visually capped for this low-traffic service,
  and show slow work separately as a selected-period count of requests over
  the histogram's two-second bucket. Use Loki for exact durations.
- Prometheus 3 normalizes the classic-histogram integer bucket label from the
  exporter's `le="2"` to stored `le="2.0"`; direct bucket selectors in
  dashboard queries must use the normalized value.

## Next step

Phase 5 — host health.

Phase 4 is complete: `zibs_build_info{version,commit}` identifies the running
binary, local builds use `version="dev"` and `commit="none"`, and deployment
passes the checked-out Git revision through Docker build arguments without
copying `.git` to the VM or image. Production verification confirmed the
deployed `HEAD` in the image build, Prometheus, the telemetry smoke test, and
the private operator dashboard. The dashboard uses an instant query so stale
historical build series do not appear as simultaneously running builds.

Next: Phase 5 — inspect the VM filesystem mounts and introduce private,
least-privilege host metrics for capacity and storage health. Dashboard version
7 is the final Phase 4 layout-only revision and should be deployed with
`DEPLOY_DASHBOARDS=1 ./scripts/deploy.sh`.
