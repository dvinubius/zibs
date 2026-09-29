# Deployment runbook

Pushes to `main` on GitHub deploy zibs to the prepared VPS through the
[`Deploy production`](../.github/workflows/deploy.yml) workflow. The VPS pulls
a published container image from GHCR; it never clones the repository and
never builds zibs.

Pull requests targeting `main` run the workflow's `test` job, which reports
the required `test` check. The `plan`, `image`, and `deploy` jobs run only for
pushes to `main` or manual workflow dispatches; pull request tests do not
access the production environment.

> [!IMPORTANT]
> **zibs is not standalone.** Public ingress (Caddy, TLS, the `zibs.app`
> route) and the `zibs-edge` network are owned by the separate [Hetzner-One](https://github.com/dvinubius/hetzner-one)
> repository, deployed at `/opt/caddy`. This runbook never deploys, restarts,
> or reconfigures them. Deploy Hetzner-One first, following its own deployment
> runbook. Without it, a full deployment stops at the `zibs-edge` check.

This runbook does not provision a VM, install Docker, configure DNS, or change
Caddy.

## How a push deploys

1. **Test.** `go vet`, Go tests, the deployment shell tests, dashboard JSON
   validation, and Compose rendering. Nothing contacts the VPS before these
   pass.
2. **Plan.** Runs serialized in the `production-deploy` concurrency group. It
   reads `/opt/zibs/.deploy/manifest` over SSH and passes the last verified
   commit to [`classify-deploy.sh`](../scripts/classify-deploy.sh), which
   inspects every path changed since then:

   | Changed since the last verified deployment | Mode |
   | --- | --- |
   | Only docs, agent notes, `README.md`, `LICENSE`, root `*_test.go`, `scripts/*_test.sh`, `scripts/apply-grafana-v2-layout.sh` | none |
   | The five allowlisted dashboard JSON files in `grafana/dashboards/` (plus the above) | dashboard |
   | Other `grafana/` files, `prometheus.yml`, `loki.yml`, `config.alloy` (plus the above) | observability |
   | Dashboards and other observability files together | full |
   | `compose.yaml`, Go code, `web/`, `Dockerfile`, scripts, the workflow, or any unlisted path | full |
   | No manifest, or its commit is not an ancestor | full |

   If SSH or Git inspection fails, the run stops without changing production.
   A docs-only push leaves the manifest unchanged, so those paths are simply
   seen again next time.
3. **Image** (full mode only). Builds the Dockerfile for `linux/amd64`, pushes
   `ghcr.io/dvinubius/zibs:<commit>`, and passes on the immutable
   `ghcr.io/dvinubius/zibs@sha256:…` reference. Only this job can write
   packages.
4. **Deploy.** Confirms the commit is still the head of `main` (otherwise the
   run is stale and a newer run will deploy), validates all five dashboards,
   creates a bundle with `git archive` from that exact commit (`compose.yaml`,
   `prometheus.yml`, `loki.yml`, `config.alloy`, `grafana/`, and the
   operational `scripts/`, without their tests), uploads it to
   `/opt/zibs/.deploy/staging/<commit>`, and runs its
   [`remote-deploy.sh`](../scripts/remote-deploy.sh).

The remote script holds a host lock, runs its preconditions, saves a snapshot
of the live `compose.yaml`, telemetry configuration, `grafana/`, `scripts/`,
`.env.image`, and manifest, and then applies one mode:

- **full**: checks Docker, `curl`, and the `zibs-edge` network; pulls the
  exact digest (with the run's short-lived `GITHUB_TOKEN`, deleted after the
  pull); installs the bundle; atomically writes `.env.image`; recreates zibs
  with `--no-build`; checks loopback `/health`, the running container, and
  `https://zibs.app/health`; recreates Prometheus, Loki, Alloy, and Grafana
  (starting node_exporter if missing); and runs the full
  [telemetry smoke test](observability.md#telemetry-smoke-test).
- **observability**: requires a running zibs and a GHCR image already
  deployed; replaces only the telemetry configuration and `grafana/`,
  recreates the telemetry services, and runs the full telemetry smoke test. It
  does not pull or recreate the app.
- **dashboard**: requires a running Grafana; replaces only the five dashboard
  files in place and runs the dashboard smoke test, which checks that Grafana
  serves all five dashboard UIDs. It runs no Compose `up`.

Only after every check passes does it write the mode-0600 manifest (`commit`,
`image`, `mode`, `deployed_at`). On a failed check it prints bounded
diagnostics, restores the snapshot, and verifies the restored deployment; a
rollback that also fails is reported as `ROLLBACK FAILED`. Five snapshots are
kept in `/opt/zibs/.deploy/snapshots`.

Deployment never touches `.env`, the named volumes, Caddy, or Hooklook
(another application behind the same Caddy), and never runs
`docker compose down`.

## Change a Grafana dashboard

Edit an allowlisted file in `grafana/dashboards/`; the
top-level `version` need not change. Grafana reprovisions a dashboard whenever
its file content changes, within one 30-second poll, and overwrites the stored
copy. The deployment checks only that all five dashboards still exist, not that
the new content was applied. The workflow and `./scripts/ci-deploy.sh validate`
accept only the Classic dashboard model with the five UIDs declared in that
validation script.

### Convert a Grafana V2 layout export

Grafana 13.0.x can emit V2 dashboard JSON from **Save dashboard** even when
the drawer labels it Classic. Do not commit that V2 JSON. Instead, use its V2
grid layout to update only the existing Classic dashboard's panel positions:

```bash
./scripts/apply-grafana-v2-layout.sh \
  ~/Downloads/zibs-operator-v2.json \
  grafana/dashboards/operator.json
git diff -- grafana/dashboards/operator.json
```

The first argument may be either Grafana's bare V2 dashboard body or its V2
Resource envelope. The converter requires a `GridLayout`, verifies that every
`elements.panel-N` item maps exactly once to the target Classic dashboard's
panel `id: N`, and changes only `gridPos`. It refuses tabs, rows, missing
panels, duplicate IDs, and malformed coordinates without changing the target.
It reformats the target JSON, so review the diff for semantic changes before
pushing.

## One-time setup

### Shared ingress

Deploy [Hetzner-One](https://github.com/dvinubius/hetzner-one) first by
following its deployment runbook. It creates `zibs-edge`, obtains the
`zibs.app` certificate, and proxies that hostname to `zibs:8080`. DNS for
`zibs.app` must already point at the VPS.

### GitHub repository

The workflow expects a GitHub environment named `production` holding:

| Name | Kind | Value |
| --- | --- | --- |
| `DEPLOY_SSH_KEY` | secret | Private key of the dedicated deployment key pair |
| `DEPLOY_HOST` | variable | VPS address |
| `DEPLOY_USER` | variable | `zibs-deploy` |
| `DEPLOY_KNOWN_HOSTS` | variable | Verified `known_hosts` line(s) for `DEPLOY_HOST` |

The repository is public, so environment secrets and branch protection are
available on GitHub Free.

Create the environment first (`gh secret set --env` fails when it does not
exist) and allow deployments from `main` only:

```bash
gh api -X PUT repos/dvinubius/zibs/environments/production \
  -F 'deployment_branch_policy[protected_branches]=false' \
  -F 'deployment_branch_policy[custom_branch_policies]=true'
gh api -X POST repos/dvinubius/zibs/environments/production/deployment-branch-policies \
  -f name=main -f type=branch
```

Generate the dedicated key locally and never reuse the operator's key or
Hooklook's deployment key:

```bash
ssh-keygen -t ed25519 -N '' -C zibs-github-deploy -f ~/.ssh/zibs_github_deploy
```

Take the host key from a channel you already trust, such as an existing SSH
session, and never from an unverified `ssh-keyscan` in the workflow. On the
VPS, print the line with the address written exactly as `DEPLOY_HOST` will
hold it (an IP and a hostname do not match each other):

```bash
printf '%s %s\n' '<vps address>' "$(cut -d' ' -f1,2 /etc/ssh/ssh_host_ed25519_key.pub)"
ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub
```

Its fingerprint must match the one your workstation already trusts
(`ssh-keygen -F '<vps address>' -l`).

Then, from the repository:

```bash
gh secret set DEPLOY_SSH_KEY --env production <~/.ssh/zibs_github_deploy
gh variable set DEPLOY_HOST --env production --body "<vps address>"
gh variable set DEPLOY_USER --env production --body zibs-deploy
gh variable set DEPLOY_KNOWN_HOSTS --env production --body "<known_hosts line>"
```

Protect `main` against force pushes and deletion, and require the `test` job.
A rewritten `main` leaves the manifest's commit off the branch's history,
which the classifier treats as a full deployment. Direct pushes by the
repository admin bypass the required check (GitHub reports the bypass); the
workflow still deploys nothing unless `test` passes.

```bash
gh api -X PUT repos/dvinubius/zibs/branches/main/protection --input - <<'JSON'
{"required_status_checks":{"strict":false,"contexts":["test"]},"enforce_admins":false,"required_pull_request_reviews":null,"restrictions":null,"allow_force_pushes":false,"allow_deletions":false}
JSON
```

GHCR publication uses the workflow's `GITHUB_TOKEN`. The package is linked to
the repository through its `org.opencontainers.image.source` label. The VPS
pulls with the run's short-lived `GITHUB_TOKEN` and needs no PAT or Git
credential, whether the package is public or private.

### VPS deployment account

As root on the VPS, create an account used only for deployment, give it
Docker access, and hand it `/opt/zibs`. Docker group membership is
effectively host-level privilege, which is why this key must not be the
operator's general key.

```bash
useradd --create-home --shell /bin/bash zibs-deploy
usermod -aG docker zibs-deploy
install -d -m 700 -o zibs-deploy -g zibs-deploy ~zibs-deploy/.ssh
install -m 600 -o zibs-deploy -g zibs-deploy /dev/null ~zibs-deploy/.ssh/authorized_keys
# Append the contents of ~/.ssh/zibs_github_deploy.pub:
printf 'restrict %s\n' '<public key>' >>~zibs-deploy/.ssh/authorized_keys

chown -R zibs-deploy:zibs-deploy /opt/zibs
chmod 600 /opt/zibs/.env
```

`/opt/zibs/.env` holds `ADMIN_TOKEN` and `GRAFANA_ADMIN_PASSWORD`. It is owned
by the VPS; the workflow never writes it. To change a secret, edit the file on
the host and run `./scripts/compose.sh up -d --no-build` (see below).

Before handing the key to GitHub, confirm from the workstation that it logs
in and reaches Docker:

```bash
ssh -i ~/.ssh/zibs_github_deploy -o IdentitiesOnly=yes zibs-deploy@<vps address> 'id; docker ps --format "{{.Names}}"'
```

`flock` (util-linux), `curl`, Docker, and the Compose plugin must be
installed.

### SSH reachability and hardening

GitHub-hosted runners connect from changing addresses that cannot be
allowlisted, so the Hetzner Cloud firewall must allow inbound TCP 22 from any
IPv4 and IPv6 source, and `ci-deploy.sh` assumes port 22. `sshd` must accept
keys only, with fail2ban on the `sshd` jail. Hooklook shares this VPS and its
[deployment runbook](https://github.com/dvinubius/hooklook/blob/main/docs/deployment-runbook.md#ssh-reachability-and-hardening)
sets all of this up; confirm it is in place:

```bash
sshd -T | grep -Ei '^(passwordauthentication|kbdinteractiveauthentication|permitrootlogin|allowusers|allowgroups) '
fail2ban-client status sshd
```

Password and keyboard-interactive authentication must be `no`. If `sshd -T`
lists `allowusers` or `allowgroups`, add `zibs-deploy` there.

### First GHCR deployment

The existing host-built deployment has no manifest, so the first run is full.
Its snapshot tags the running host-built image as `zibs:rollback-<snapshot>`
so a failed or later rollback can return to it. The Compose project name stays
`zibs`, so the named volumes carry over unchanged.

1. Push `main`. The push itself triggers the workflow, which runs full mode
   because no manifest exists. Alternatively, run it manually:
   `gh workflow run deploy.yml --ref main`.
2. Confirm the run succeeded, then on the VPS check
   `cat /opt/zibs/.deploy/manifest`, `./scripts/compose.sh ps`, Grafana's
   `127.0.0.1:3000` bind, and that Caddy and Hooklook are healthy.
3. Remove the source tree left by the old rsync deployment. Everything the
   GHCR deployment needs lives in `compose.yaml`, `prometheus.yml`,
   `loki.yml`, `config.alloy`, `grafana/`, `scripts/`, `.env`, `.env.image`,
   and `.deploy/`:

   ```bash
   cd /opt/zibs
   ls -A   # review first
   rm -rf -- *.go go.mod go.sum Dockerfile .dockerignore web LICENSE README.md \
     AGENTS.md .devnotes.md .gitignore .ignore .claude .vscode .dev-backups .DS_Store zibs
   ```

   Confirm no Go source, `web/`, or compiled `zibs` binary remains.

## Operating the deployed stack

Always use the wrapper, which loads `.env`, the pinned `.env.image`, and the
production profile:

```bash
cd /opt/zibs
./scripts/compose.sh ps
./scripts/compose.sh logs --tail=100 zibs
./scripts/compose.sh up -d --no-build   # e.g. after a reboot or a secret change
./scripts/telemetry-smoke-test.sh       # full check; `dashboard` for dashboards only
cat .env.image                          # the running image digest
cat .deploy/manifest                    # the last verified deployment
ls .deploy/snapshots
```

After DNS and certificates have settled, confirm the public endpoint:

```bash
curl --fail https://zibs.app/health
```

The public check depends on the independently managed `/opt/caddy` service.
If it fails while the loopback health check passes, inspect Caddy from
`/opt/caddy` using the [Hetzner-One](https://github.com/dvinubius/hetzner-one)
runbook; do not rerun a zibs deployment as an ingress repair. For
per-component telemetry checks, see [Manual diagnostics](diagnostics.md).

## Roll back

A failed deployment restores its snapshot automatically. To reverse a
deployment that succeeded, restore an earlier snapshot on the VPS:

```bash
cd /opt/zibs
ls .deploy/snapshots
./scripts/remote-deploy.sh rollback <snapshot-name>
```

A snapshot named `<time>-<mode>` holds the state from before that deployment.
Rollback takes a fresh snapshot, restores the files, `.env.image`, and the
manifest from the chosen one, recreates zibs and telemetry without building,
and runs the same checks. The restored manifest describes what now runs, so
the next push deploys everything since then. Afterwards, fix forward with a
push to `main` (a revert commit is fine); a manual
`gh workflow run deploy.yml --ref main` redeploys the current head of `main`
in full.

Old images stay in the local Docker image store and in GHCR, where nothing
deletes them automatically. Keep them at least as long as the snapshots that
reference them. Neither rollback changes the `zibs-data` volume; never use
`docker compose down -v` or remove that volume during a code rollback.

If a database restore is needed, stop and assess the incident first; use the
isolated restore verification procedure in the
[database backup runbook](database-backup-runbook.md). Do not overwrite the
live volume with an unverified backup.
