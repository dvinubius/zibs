<p align="center">
  <img src="web/static/zibs-thumbnail.png" alt="zibs wordmark on a dark background" width="400">
</p>

<p align="center">
  <em>A friendly link shortener. No account, no costs — just be human and say hi.</em>
</p>

<p align="center">
  <a href="docs/README.md"><strong>Docs</strong></a> ·
  <a href="#architecture-at-a-glance"><strong>Architecture</strong></a> ·
  <a href="docs/observability.md"><strong>Observability</strong></a>
</p>

<p align="center">
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/dvinubius/zibs" alt="Go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT%20(code%20only)-blue" alt="License"></a>
  <a href="https://zibs.app/health"><img src="https://img.shields.io/website?url=https%3A%2F%2Fzibs.app%2Fhealth&label=zibs.app&up_message=live&down_message=down" alt="zibs.app status"></a>
</p>

<h3 align="center"><a href="https://zibs.app">https://zibs.app</a></h3>

# zibs

**Zibs** is a friendly link shortener — small in scope, production-grade in shape.

Zibs is **free to use**.

The app requires a "zib" token, which you can request by email — personally. Each browser remembers the token after its first use, so it is entered once per device.

Behind the one-page frontend sits a complete, self-hosted production service: a Go application with SQLite persistence, deployed as a hardened container behind a TLS-terminating reverse proxy on a single VM, with scripted deployments, verified database backups, and a **full observability stack** — Prometheus metrics, structured logs shipped through Alloy to Loki, and Grafana dashboards provisioned straight from this repository.

- 📈 **Watch it run** — the [public metrics dashboard](https://zibs.app/public-dashboards/6b58a8bd322f4bbfb06f4b285055028c) shows live request rates, latency percentiles, and redirect totals. It is the deliberately limited public view of the private operator dashboard.
- ⭐ **Like what you see?** Star the repo — it helps others find it.
- 📕 There is a [**Substack article**](https://dvinubius.substack.com/p/zibs-a-link-shortener-designed-to-connect-us) describing this app, and it refers to the v1 release. Check out that [specific tree](https://github.com/dvinubius/zibs/tree/28cff4a1bb402486c5fdfde32b6e9a63e95826ca) if you want to explore the project as presented in the article.

## Architecture at a glance

```mermaid
flowchart LR
    browser([Browser]) ==>|HTTPS| caddy

    subgraph vm[one VM]
        subgraph edge[zibs-edge · owned by Hetzner-One]
            caddyAppEdge[Caddy<br/>attachment]
            zibs[zibs<br/>Go service]
        end
        zibs --> db[(SQLite)]
        zibs -.->|metrics · logs| obs[Prometheus · Alloy<br/>Loki · Grafana<br/>- - - includes - - -<br/>services, network attachments, volumes]
        obs -.->|shared public dashboard| caddy
        caddy[Caddy · Hetzner-One project<br/>shared ingress]
    end

    caddy -.-|network attachment| caddyAppEdge
    caddyAppEdge --> zibs

    classDef simplified stroke-dasharray: 5 5;
    class caddyAppEdge,obs simplified;
```

> [!IMPORTANT]
> **zibs is not a standalone deployment.** This repository contains no reverse
> proxy, obtains no TLS certificates, and does not create the Docker network
> its services join. Public ingress comes from the separate
> [Hetzner-One](https://github.com/dvinubius/hetzner-one) repository, which
> owns the shared Caddy service (public ports 80/443, TLS certificates,
> hostname routing for `zibs.app`) and the `zibs-edge` network. Hetzner-One
> must be deployed first; zibs then joins `zibs-edge` as an external network.
> Without it, zibs still runs on VM loopback, but nothing serves `zibs.app`
> or the public dashboard.

Caddy is the only public entry point. Hetzner-One runs it as the `/opt/caddy`
Compose project on the VM and owns both `zibs-edge` and `hooklook-edge`.
zibs does not deploy, configure, or reload Caddy. The telemetry stack is
private except for the one externally shared dashboard, which Caddy proxies
on a narrow route.

> **Article note:** [“zibs: A Link Shortener Designed to Connect Us”](https://dvinubius.substack.com/p/zibs-a-link-shortener-designed-to-connect-us?r=dqiys)
> predates the Caddy extraction and therefore depicts a different deployment
> topology. In the current deployment, Caddy is decoupled from zibs and managed
> by [Hetzner-One](https://github.com/dvinubius/hetzner-one) at `/opt/caddy`.

This diagram is strongly simplified. The full picture — observability services, networks, volumes, port
boundaries — is in [deployment architecture](docs/deployment-architecture.md)
and [observability](docs/observability.md).

## Working locally

Install Go 1.25.0 or newer. Set a development-only admin token and start the
service directly:

```bash
ADMIN_TOKEN=development-only-token go run .
```

Open `http://localhost:8080`. The service creates `zibs.db` in the working
directory. Its private metrics endpoint is `http://127.0.0.1:9091/metrics`;
do not expose it through a public reverse proxy.

Docker Compose is also available when you want the containerized shape.
It requires the external `zibs-edge` network. On production, deploy
[Hetzner-One](https://github.com/dvinubius/hetzner-one) first; for an
isolated local setup, create it once with `docker network create zibs-edge`.

```bash
ADMIN_TOKEN=development-only-token docker compose up --build
```

The application is then available at `http://127.0.0.1:8080`. The Compose
configuration keeps the metrics port private.

Run the complete test suite with:

```bash
go test -count=1 ./...
```

## Documentation

The [documentation guide](docs/README.md) covers the HTTP API, creation
frontend, service design, deployment, database backups, and observability. For
the current monitoring boundary and intentionally postponed work, start with
[Observability](docs/observability.md) and [deferred observability work](docs/v2-deferred-observability.md).

## License

The code is MIT-licensed. The visual design and brand assets are excluded — see [LICENSE](LICENSE).
