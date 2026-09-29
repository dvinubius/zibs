# Using the zibs dashboards

This guide teaches you to answer real questions with the private dashboards in
Grafana's **Zibs** folder. [Observability](observability.md) covers the
telemetry stack, the metrics, and access; [Operator dashboard](operator-dashboard.md)
is the panel-by-panel reference with screenshots. This document covers
reading them.

Open Grafana through the SSH tunnel described in
[Open the private workspace](observability.md#open-the-private-workspace)
(`ssh -L 3000:127.0.0.1:3000 root@<host>`, then `http://localhost:3000`).

## 1. What the dashboards can see

zibs measures itself. A middleware around the application handler counts
every completed request once and times it. Each request is labelled with a
normalized route, its method, its status code, and a traffic class. The same
request writes one JSON log line that also carries the raw path. Everything
else on these dashboards comes from the store's database timings, the Go
runtime, or node_exporter on the VM.

Some traffic never reaches zibs, so it never appears here:

- Anything Caddy answers itself: the HTTP-to-HTTPS redirect, TLS handshakes
  that never become a request, and Grafana probe paths such as `/login`,
  `/grafana/*`, and `/api/v1/*`, which Caddy answers with 404.
- The public dashboard's paths, including `/favicon.ico`, which Caddy sends
  to Grafana.
- Requests that fail at Caddy while zibs is down. Those are 502s at Caddy,
  and a stopped zibs counts nothing. Hetzner-One's Caddy dashboard shows them.
- Prometheus scrapes, which use the separate metrics listener.

A request is counted when it finishes. While it runs, only
**In-flight requests** can see it.

### Retention and resolution

Prometheus scrapes zibs and node_exporter every 30 seconds and keeps the
default 15 days; the Compose file sets no retention flag. Loki keeps logs for
14 days. In practice:

- Rate and count time series use `$__rate_interval`, which is at least two
  minutes with a 30-second scrape. A 20-second burst is smeared over two
  minutes.
- **Expiry cleanup duration** asks for 30 days of history but can see only 15.
- Anything older than 15 days (metrics) or 14 days (logs) is gone. Write down
  numbers you want to compare over longer periods.

### How each kind of request is recorded

These rows follow from the handlers, the route labeller, and the traffic
classifier. Most scenarios below apply this table.

| What happened | `route` | Status | Link operation | Class |
| --- | --- | --- | --- | --- |
| A browser opens the creation page | `/`, then `/static` for assets | 200 | none | other |
| An active short link is followed | `/{code}` | 302 | follow `success` | caller's |
| An unknown, expired, or deleted code is followed | `/{code}` | 404 | follow `not_found` | caller's |
| Any other single-segment path: `/robots.txt`, `/.env`, `/admin`, `/wp-login.php`, a mangled code | `/{code}` | 404 | follow `not_found` | suspected_scan |
| A deeper path: `/wp-admin/setup-config.php`, `/.git/config` | `/{code}` | 404 | none | suspected_scan |
| A wrong method: `POST /graphql`, `POST /`, `GET /links` | `/{code}` | 405 | none | suspected_scan |
| A link is created | `/links` | 201 | create `success` | caller's |
| Creation without a valid token, or with a spent one | `/links` | 401 | none | caller's |
| Creation with a valid token but an invalid body | `/links` | 400 | none, but a token use is spent | caller's |
| An admin call without the admin token | `/admin/...` | 401 | none | caller's |
| An admin deletes a link | `/admin/links/{code}` | 204 | delete `success` | caller's |
| A health check: deploys, the smoke test, your `curl` | `/health` | 200 | none | other |
| SQLite fails during a follow | `/{code}` | 500 | follow `error` | caller's |

Three consequences are worth memorising:

1. **A link miss is any single-segment path that is not a live code.**
   The overview's **Link misses** counts all of them. The class dashboards
   split them by shape: a miss stays in other only when its path is shaped
   like a code (exactly eight letters or digits). Everything else, from
   `/robots.txt` to `/.env`, is a suspected scan.
   [Scenario 2](#scenario-2-link-misses-are-climbing-broken-links-or-noise)
   reads the misses that matter.
2. **Every follow writes to SQLite, hit or miss.** A follow is one
   `UPDATE … WHERE code = ? AND expires_at > ?` that increments the redirect
   count. A scanner walking single-segment paths therefore drives database
   writes, and scan bursts show up in **Database operation duration**.
3. **A creation request spends its token before the body is checked.** A 400
   on `/links` still used one token use. A run of 400s followed by 401s means
   someone spent their token on invalid input.

### Reading rules

- **Two kinds of stat panel.** Stats named "in selected period" follow the
  time picker. The **Business summary** row and the public dashboard's
  redirect and creation stats always show fixed 24-hour and 7-day windows.
- **Counts are estimates.** `increase()` extrapolates to the edges of its
  window, so a stat can read 11.7 requests. Over short windows and small
  numbers, treat ±1 as noise. Loki gives exact counts; see
  [section 5](#5-explore-recipes).
- **Time-series counts are per trailing window, not per point.**
  **Request count by route and status** plots, at each point, how many
  requests finished in the preceding `$__rate_interval`: about two minutes
  at "Last 24 hours", several more at "Last 7 days". Neighbouring points overlap,
  so do not add them up, and expect peaks to grow as you zoom out. Compare
  shapes, not heights, across time ranges.
- **Quantiles are interpolated between bucket edges.** HTTP and database
  buckets are 0.5, 1, 2, 3, 5, 10, 25, 50, 100, 250, 500 ms, 1 s and 2 s. A
  p95 of 40 ms means "between 25 and 50 ms". A p95 flat at exactly 2 s means
  more than 5% took longer than 2 s, and Prometheus cannot say how much
  longer. The latency panel's Loki link can.
- **Low traffic makes quantiles jumpy and gappy.** At a few requests a minute,
  one slow request moves p95 a long way, and the latency panel's automatic
  y-axis rescales around it. With no requests in a window, the lines break.
  Widen the range before believing a spike.
- **Gauges are snapshots taken every 30 seconds.** **In-flight requests**,
  **SQLite pool context**, and **Active links** show the value at scrape time.
  Millisecond-scale work nearly always falls between scrapes, so zero
  in-flight requests is normal.
- **"No data" is neither zero nor broken.** A labelled series such as
  `/links 500`, or a database-error kind, appears only after it first occurs
  since zibs started. Stats whose query ends in `or vector(0)` show 0 instead.
- **Restarts reset counters.** `increase()` compensates; **Process uptime**
  shows when it happened.
- **Traffic classes start at their deployment.** Earlier requests have no
  class. The class dashboards and **Request count by traffic class** ignore
  them; the overview's other panels include them.

### The five dashboards

| Dashboard | Scope | Use it to |
| --- | --- | --- |
| **zibs operator overview** | All traffic, plus database, runtime, host, and logs | Triage anything; it is the only view with global panels |
| **zibs synthetic activity** | `traffic_class="synthetic"` | Check that the traffic lab runs as scheduled |
| **zibs suspected scans** | `traffic_class="suspected_scan"` | See everything that cannot be ordinary use: probes, crawler files, wrong methods |
| **zibs other activity (unclassified)** | `traffic_class="other"` | Approximate real use: people, bots, health checks |
| **zibs public metrics** | All traffic, aggregated | See what visitors see; never use it to diagnose |

The class dashboards contain only request and link-operation panels.
Database, runtime, and host measurements are global because a database
operation cannot be attributed to the request class that caused it.

The **Business summary**, **Active links**, and the whole public dashboard
include synthetic traffic. For real use, read the other-activity dashboard.

## 2. What normal looks like

A dashboard is useful only against a baseline. These parts of the baseline
follow from how zibs and its traffic lab work; write down the rest (request
rate at a busy hour, typical scan volume, typical p95) the first few times you
look.

| Where | Normal | Why |
| --- | --- | --- |
| Synthetic creations | About `n` a day | The traffic lab issues one `n`-use token a day (`node set-n.js` sets `n`) |
| Synthetic follows | About `10n` a day, all 302 | Follows are sampled from recently created codes, all well inside the 90-day TTL |
| Synthetic `/admin/tokens` | One 201 shortly after 00:05 UTC | The daily scheduler provisions the token |
| Synthetic shape | Small night baseline, a European daytime peak, a Europe/US overlap peak | The scheduler's activity curve |
| Synthetic 4xx and 5xx | None | Anything else is scenario 4 |
| **Active links** | Roughly `90n` plus real links, once the traffic lab has run for 90 days | Synthetic links accumulate for the 90-day TTL |
| **Database operation duration** | A steady `count_active_links` series even when idle | Every scrape counts active links in SQLite |
| **SQLite pool context** | 1 or 2 open, usually 0 in use | `database/sql` keeps up to two idle connections between requests |
| **In-flight requests** | 0 | Requests finish between scrapes |
| **Host disk I/O** | Small periodic write bumps | Prometheus compacts its head block every two hours; Loki flushes and compacts chunks |

## 3. The two-minute check

Do this when you have not looked for a while.

1. Open **zibs operator overview** at **Last 24 hours**.
2. **Service up** is 1. Does **Process uptime** match your last deployment? An
   unexplained reset is a restart; see scenario 8.
3. **5xx responses** and **Busy or locked events** are 0.
4. Scroll to **Request count by traffic class**. Synthetic should show its
   daily shape; scan bursts are routine. A flat line where the synthetic curve
   should be is scenario 4.
5. **Request latency**: is p95 where it usually sits?
6. **Root filesystem headroom** and **Host memory utilization**: no new trend.
7. Open **zibs other activity**. Any creations? Those are people you gave a
   token to.

## 4. Scenarios

Each scenario starts from something you notice, then walks the panels in an
order that narrows the cause.

### Scenario 1: 5xx responses appeared. Which part failed?

**5xx responses in selected period** is above zero.

zibs itself returns only one server error: 500, and nearly always because a
SQLite operation failed. A 502, 503, or 504 seen by a visitor comes from Caddy,
not zibs; look at Hetzner-One's Caddy dashboard.

1. Narrow the range around the errors.
2. Open the stat's **Inspect server errors in Loki** link. Each line carries
   the route, raw path, method, and `duration_ms`.
3. In **Request count by route and status**, find which route produced the
   500s.
4. Check **Link operation count** for an `error` result and
   **Database errors by operation and kind** for the failing operation.
5. For the underlying error text, query Loki:
   `{service="zibs"} | json | event="database_operation_failed"`.

| You see | Meaning | Next step |
| --- | --- | --- |
| `/{code}` 500, follow `error`, kind `busy` | A follow waited five seconds for SQLite's lock and gave up | Scenario 7 |
| Kind `other`, error text mentions disk, full, or read-only | Storage trouble | **Root filesystem headroom**; scenario 10 |
| `/links` 500 with a `consume_creation_token` error | The token check failed before any link was created | The error text; the token was not spent |
| `/admin/...` 500 | An admin action failed | You probably caused it; the error text says why |
| A 500 with no database error at all | A path outside the store failed | The Loki line and application code |

### Scenario 2: link misses are climbing. Broken links or noise?

**Link misses in selected period** rose.

Most misses are not people. Every single-segment path that is not a live code
counts on the overview (see the table in section 1).

1. Compare **Link misses** across the class dashboards. Anything that cannot
   be a code is on the suspected-scans dashboard. The misses on
   **other activity** are all code-shaped, so only they can be people.
2. List the other-class miss paths in Explore (Loki):

   ```logql
   topk(20, sum by (path) (count_over_time({service="zibs"} | json | event="request_completed" | route="/{code}" | status="404" | traffic_class="other" [24h])))
   ```

3. Read the list:

   | Path looks like | Likely cause |
   | --- | --- |
   | A code, repeated | Someone following an expired, deleted, or mistyped link |
   | A code, once | A typo, or a one-off follow of an old link |
   | Many different codes, each once | Someone guessing codes; path shape cannot tell them from people |

A link mangled when copied, for example with a trailing `)` or `.` from a
chat app, is not code-shaped. Look for it in the suspected-scans dashboard's
**Most probed paths**.

A repeated code is the only case that affects a user.
`GET /admin/links` lists the links that still exist. If the code is missing
there, it expired (links live 90 days) and was cleaned up, it was deleted
(**Manual deletions** shows when, within the last 15 days), or it never
existed.

### Scenario 3: what are the scanners doing, and should I care?

Open **zibs suspected scans**. It holds every request that cannot be ordinary
use of zibs, so besides probes it shows crawler and browser files such as
`/robots.txt` and `/apple-touch-icon.png`, and the occasional mangled short
link. Those are not attacks; judge the class by its volume and its effects,
not by its name.

1. **Most probed paths in selected period** lists the top 20 paths with their
   method and status.
2. In **Request count by route and status**, the signatures are:
   - `/{code} 404` with follow `not_found` in **Link operation count**:
     single-segment paths such as `/xmlrpc.php` or `/.env`. Each is a
     database write.
   - `/{code} 404` with no link operation: deeper paths such as
     `/wp-admin/setup-config.php`.
   - `/{code} 405`: a known path with the wrong method, such as
     `POST /graphql` or `POST /`.
3. Scanning is background noise on any public address. Care only when a scan
   burst coincides with one of these on the overview:
   - **5xx responses** above zero;
   - **Busy or locked events** above zero (a single-segment scan is a burst of
     writes);
   - a p95 rise on the **other activity** dashboard in the same minutes, which
     means real requests were slowed;
   - **CPU usage** or **Host CPU utilization** clearly above its usual level.

zibs cannot identify or block the client: it logs no client addresses, and
blocking belongs at Caddy or the firewall, both owned by Hetzner-One.

### Scenario 4: is the traffic lab healthy?

Open **zibs synthetic activity** at **Last 7 days**.

1. **Links created in selected period** divided by seven should be close to
   `n`, and **Successful redirects** about ten times that. The curve should
   repeat its daily shape.
2. **Response count by status** should show only 201 and 302. The daily
   token provisioning adds one 201 on `/admin/tokens`.

| You see | Likely cause |
| --- | --- |
| Nothing since a certain time | Cron or the worker stopped. Read `/opt/zibs/test-traffic/logs` on the VM |
| A gap matching a zibs restart | Events due while zibs was down were recorded as `failed`; the worker does not retry them |
| `/links` 401 | The day's token was exhausted, revoked, or never provisioned |
| Follow `not_found` | A code in the lab's pool was deleted manually |
| Volume changed overnight | `n` changed; `set-n.js` affects the next daily plan, never the running one |

### Scenario 5: is anyone actually using zibs?

Open **zibs other activity** at **Last 7 days**.

- **Links created**: every creation needs a creation token, and you issue
  them. Other-class creations are therefore people you gave a token to, or
  you.
- **Successful redirects**: people following links, or link-preview bots
  fetching a link shared in a chat. A burst of one to three follows seconds
  after a creation is typically preview bots.
- `/` and `/static` requests are visits to the creation page.

To compare real and synthetic outcomes directly, run this in Explore
(Prometheus):

```promql
sum by (traffic_class, operation) (increase(zibs_link_operations_total{result="success"}[7d]))
```

The overview's **Business summary** and the public dashboard include the
synthetic volume; do not read them as usage.

### Scenario 6: requests got slower. The app, the database, or the machine?

**Request latency** p95 rose.

1. **Is it real?** Widen the range and check how many requests the spike
   covers. At low traffic one slow request is a spike.
2. **Which class?** Compare p95 on the three class dashboards. A rise
   confined to one class points at that traffic, not at the service.
3. **Which route?** The latency panel is not split by route. In Explore:

   ```promql
   histogram_quantile(0.95, sum by (le, route) (rate(zibs_http_request_duration_seconds_bucket[5m])))
   ```

4. **The database?** Open **Database operation duration**. If the same
   operation's p95 rose, the time is spent in SQLite.
5. **Lock waits?** Check **Busy or locked events** (scenario 7).
6. **The machine?** Check **Host CPU utilization**, **Host load average**
   against the VM's CPU count, and **Host disk I/O**. Hooklook and Caddy share
   this VM.
7. **Which requests?** The panel's **Inspect requests taking at least 1 s in
   Loki** link lists them. For a lower threshold, change `duration_ms >= 1000`
   in the query.

| Pattern | Likely cause |
| --- | --- |
| Request p95 up, database p95 flat | Work outside SQLite: CPU pressure or garbage collection (**CPU usage**, **Allocation rate**) |
| Both up, one operation only | That query got slower; `count_active_links` rising slowly over weeks means the links table grew |
| Both up, all write operations, pinned at 2 s | Lock contention; scenario 7 |
| Both up, host disk I/O saturated | Storage contention from another process on the VM |
| p95 up only on `/links` | Creation is two writes (token use, then insert), so it is the first route to feel write pressure |

### Scenario 7: a busy or locked event turned red

**Busy or locked events in selected period** is 1 or more. For this
single-instance service, one is notable.

zibs sets no connection-pool limit, so contention never appears as a
`database/sql` wait, and the dashboard has no pool-wait panels. It appears
inside SQLite instead. Only one writer can commit at a time, and the driver
waits up to five seconds (its default busy timeout) for the lock before
returning `SQLITE_BUSY`. Every follow, creation, token use, deletion, and
expiry cleanup is a write.

1. In **Database errors by operation and kind**, note which operation hit
   `busy`.
2. In **Database operation duration**, look for write operations pinned at
   the 2 s top bucket in the same minutes.
3. Look for what else was writing:
   - a request burst in **Request count by traffic class**, often a
     single-segment scan;
   - an expiry cleanup (`{service="zibs"} | json | event=~"expiry_cleanup_.*"`);
   - a backup you started: `zibs backup` runs `VACUUM INTO` on the live
     database.
4. **SQLite pool context** above its usual one or two open connections at
   that moment confirms concurrent work, but the 30-second snapshot often
   misses it.

Rare busy events after a known burst need no action. Repeated ones at ordinary
traffic are a design question (for example, WAL mode or a longer busy
timeout), not an operational fix.

### Scenario 8: reading a deployment on the dashboard

Deployment modes leave different fingerprints. Knowing them lets you tell a
deployment from an incident.

| Mode | What the dashboards show |
| --- | --- |
| full | **Process uptime** drops to zero; counters restart; resident memory and heap restart low and grow back as caches refill; an expiry cleanup runs at startup; a handful of `/health` 200s appear in other activity (loopback and public checks, then the smoke test) |
| observability | Prometheus is recreated: one or two missing scrapes leave a gap in every metric panel, zibs and host alike. **Process uptime** keeps counting. Logs pause and then catch up, because Alloy resumes from its saved positions. Grafana restarts, so reload the page |
| dashboard | Only panel definitions change, within 30 seconds of the file swap; reload the page |
| rollback | Same as full |

During a full deployment, requests that arrive while the container is
replaced never reach zibs. They are Caddy 502s, visible only on Hetzner-One's
Caddy dashboard, and the traffic lab records them as failed events.

A Hetzner-One Caddy deployment shows up here as a gap in real traffic and a
single `/` 200 in other activity from its `verify.sh` check.

After a full deployment, compare the next hours with the same hours before:
a step change in **Allocation rate** or **CPU usage** at the same traffic
points to the new code.

### Scenario 9: memory keeps growing

Set the range to **Last 7 days** and open the **Runtime** row.

- **Resident vs Go-managed memory**: resident memory includes SQLite's page
  cache, which lives outside the Go heap. The gap grows with the number of
  open connections and how much of the database each has cached, then levels
  off. A gap that keeps widening at steady traffic is worth investigating.
- **Heap vs next GC target**: a sawtooth is normal. Rising troughs over days
  mean the heap retains memory after each collection.
- **Go goroutines** and **Open file descriptors** track open connections,
  mostly Caddy's keep-alive connections plus SQLite files. Growth without a
  matching traffic change suggests a leak.
- A full deployment resets all of these. Compare like periods after it, not
  across it.

Then check **Host memory utilization**: zibs is one tenant among Hooklook,
Caddy, and the telemetry stack.

### Scenario 10: will the disk fill up?

Open **Root filesystem free space** at **Last 7 days**. The filesystem panels
intentionally cover only `/`: on this VM it is the ext4 filesystem that holds
Docker images and volumes, SQLite, backups, and telemetry state. To confirm a
reading, compare it with `df -B1 /` on the VM.

| Step or trend | Cause |
| --- | --- |
| A step down at each full deployment | A new image was pulled; old images are never deleted automatically |
| Steady decline for the first two weeks, then flat | Loki (14 days) and Prometheus (15 days) filling to their retention |
| A step down when you ran a backup | A backup file in the chosen directory |
| Steady decline after the first two weeks | Something without retention: images, backups, or another tenant |

To project it, run in Explore:

```promql
predict_linear(node_filesystem_avail_bytes{job="node",mountpoint="/"}[7d], 30 * 24 * 3600)
```

A negative result means the trend fills the disk within 30 days. On the VM,
`docker system df` shows image and volume usage. Keep images that the
deployment snapshots still reference (see
[Roll back](deployment-runbook.md#roll-back)), and never remove the named
volumes.

### Scenario 11: did expiry cleanup run?

Cleanup runs when zibs starts and then every 24 hours of process uptime, so
its time of day moves with each deployment.

1. **Expiry deletions** shows how many rows it removed in the last 24 hours
   and 7 days.
2. **Expiry cleanup duration** turns red above 50 ms.
3. Each run writes a log line with its `deleted` count:

   ```logql
   {service="zibs"} | json | event=~"expiry_cleanup_.*"
   ```

   `expiry_cleanup_failed` means the delete failed; see scenario 1 for
   database errors.

Cleanup does not change **Active links**. That gauge already excludes expired
rows, so it drops as links expire, and cleanup only removes rows that were
already inactive.

### Scenario 12: the dashboards look empty

Tell apart which part of the chain stopped from what still works.

| **Service up** | Host panels | Logs panel | Meaning |
| --- | --- | --- | --- |
| 0 | Data | Stopped | zibs is down or its metrics listener is unreachable |
| No data | No data | Data | Prometheus is down; Loki still works |
| 1 | Data | Empty | Alloy or Loki stopped; metrics are fine |
| No data | No data | Empty | Grafana cannot reach either data source, or the whole stack is down |

Then run the [telemetry smoke test](observability.md#telemetry-smoke-test)
and continue with [Manual diagnostics](diagnostics.md).

## 5. Explore recipes

Open **Explore**, choose the data source, and set the time picker to cover the
question. For instant results, switch the query type to **Instant**.

### Prometheus

How many requests of each class and status in the last day?

```promql
sort_desc(sum by (traffic_class, status) (increase(zibs_http_requests_total[24h])))
```

What share of requests finished within 25 ms, per route?

```promql
sum by (route) (rate(zibs_http_request_duration_seconds_bucket{le="0.025"}[1h]))
/ sum by (route) (rate(zibs_http_request_duration_seconds_count[1h]))
```

Which database operation is slowest over the last day?

```promql
sort_desc(histogram_quantile(0.95, sum by (le, operation) (increase(zibs_db_operation_duration_seconds_bucket[24h]))))
```

How many times did zibs restart in the last week?

```promql
changes(process_start_time_seconds{job="zibs"}[7d])
```

Did any scrape fail in the last day? 1 means none did.

```promql
min_over_time(up{job="zibs"}[24h])
```

Prometheus 3 stores integer bucket edges in a normalised form: select the 2 s
bucket with `le="2.0"`, not `le="2"`. If a bucket query returns nothing, list
the stored edges with `count by (le) (zibs_http_request_duration_seconds_bucket)`.

### Loki

Exact request counts by class and status, as an **Instant** query whose
window matches the question (compare them with the extrapolated stat panels):

```logql
sum by (traffic_class, status) (count_over_time({service="zibs"} | json | event="request_completed" [24h]))
```

Wrong-method probes and the paths they target:

```logql
{service="zibs"} | json | event="request_completed" | status="405"
```

Requests slower than 250 ms:

```logql
{service="zibs"} | json | event="request_completed" | duration_ms >= 250
```

Startup and shutdown:

```logql
{service="zibs"} | json | msg=~"server started|shutdown signal received|server stopped"
```

## 6. Practice drills

These send a handful of requests, well below any limit. They leave permanent
counts and 14 days of log lines, all in the other or suspected-scan class.

1. **Predict where your requests land.** Before sending them, use the table in
   section 1 to predict each request's route, status, link operation, and
   class:

   ```bash
   curl -s -o /dev/null -w '%{http_code}\n' https://zibs.app/Drill000
   curl -s -o /dev/null -w '%{http_code}\n' https://zibs.app/drill.php
   curl -s -o /dev/null -w '%{http_code}\n' https://zibs.app/drill/deeper
   curl -s -o /dev/null -w '%{http_code}\n' -X POST https://zibs.app/graphql
   curl -s -o /dev/null -w '%{http_code}\n' -I https://zibs.app/
   ```

   Expect 404, 404, 404, 405, and 200. Within about a minute, check your
   predictions on the overview, the other-activity dashboard, and the
   suspected-scans dashboard. Only `Drill000` is code-shaped, so it is the
   one link miss on other activity. `drill.php`, `drill/deeper`, and the
   `POST` appear in **Most probed paths**; `drill.php` is a link miss there
   too. The `HEAD` request is an ordinary `/` 200.
2. **Estimates versus exact counts.** Pick a 15-minute window after drill 1.
   Compare **Requests in selected period** with the exact Loki count for the
   same window, and explain the difference with the reading rules.
3. **Read the synthetic day.** On the synthetic dashboard at
   **Last 24 hours**, find the token provisioning, the night baseline, and
   both peaks. Check the creation count against `n`.
4. **Catch an in-flight request.** Normal requests are too brief to appear
   in a scrape, so hold one open instead: send a deliberately incomplete
   creation body and keep its input stream open for at least 70 seconds. The
   route authenticates first, then waits for the rest of the JSON body; it
   cannot create a link. The check spends one token use, so issue a
   disposable single-use creation token for it.

   In one terminal, keep the request open. Paste the token only at the silent
   `read` prompt, so it stays out of your shell history:

   ```bash
   read -rs ZIBS_CREATION_TOKEN
   export ZIBS_CREATION_TOKEN
   { printf '{'; sleep 70; } | curl --http1.1 --no-buffer --request POST --upload-file - \
     --header 'Expect:' \
     --header 'Transfer-Encoding: chunked' \
     --header "Authorization: Bearer $ZIBS_CREATION_TOKEN" \
     --header 'Content-Type: application/json' https://zibs.app/links
   unset ZIBS_CREATION_TOKEN
   ```

   While it is open, **In-flight requests** on the operator overview should
   read `1` within one scrape (up to 30 seconds), then return to `0` at the
   scrape after the stream closes. The request ends with HTTP 400 because its
   body is invalid, as expected; section 1 explains why the token use is still
   spent. The public dashboard deliberately has no in-flight panel.
5. **Find a deployment.** Using only the dashboards, find the most recent
   deployment of each mode in the last week and name its mode from its
   fingerprint (scenario 8). Confirm with `cat /opt/zibs/.deploy/manifest` and
   `ls /opt/zibs/.deploy/snapshots` on the VM.

## 7. When to leave these dashboards

| Question | Where |
| --- | --- |
| Which exact path, and when? | Loki, through Explore or the class dashboards' log panels |
| Which client? | Not recorded: zibs logs no client addresses, and Caddy access logs are not collected |
| Did requests fail before reaching zibs, for example 502s during a deployment? | Hetzner-One's Caddy dashboard |
| Is the site reachable right now? | `curl --fail https://zibs.app/health` |
| Is the telemetry chain itself healthy? | The [telemetry smoke test](observability.md#telemetry-smoke-test), then [Manual diagnostics](diagnostics.md) |
| What is deployed, and can I roll back? | The [deployment runbook](deployment-runbook.md#operating-the-deployed-stack) |
| Is the data safe? | The [database backup runbook](database-backup-runbook.md) |
