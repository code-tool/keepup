# keepup

A lightweight Prometheus exporter that collects infrastructure inventory pushed by remote agents - OS releases, package versions (with end-of-life enrichment), and Kubernetes/Helm deployments - and exposes it as metrics.

`keepup` holds no state of its own: Redis is both the write buffer and the read source. Agents `PUT` JSON, `keepup` validates and stores it with a TTL, and Prometheus scrapes `/metrics` on demand.

## Contents

- [How it works](#how-it-works)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [API](#api)
  - [`PUT /os-release`](#put-os-release)
  - [`PUT /package-version`](#put-package-version)
  - [`PUT /helm-cluster`](#put-helm-cluster)
- [Metrics](#metrics)
- [Testing](#testing)
- [Deploying with Helm](#deploying-with-helm)
- [Releasing](#releasing)

## How it works

```
 agent(s)                    keepup                        Prometheus
┌─────────┐   PUT + token   ┌───────────────────┐  scrape  ┌────────────┐
│ os-info │ ───────────────>│ handler ──▶ Redis  │<────────│ /metrics   │
│ pkg-vers│                 │           (TTL)    │          │            │
│ helm    │                 └───────────────────┘          └────────────┘
└─────────┘
```

Every data domain follows the same shape:

| Domain | Endpoint | Redis key | Metric |
|---|---|---|---|
| OS release *(deprecated)* | `PUT /os-release` | SHA1 of `{data_center}-{host_ip}` | `os_release_info` |
| Package versions | `PUT /package-version` | SHA1 of `{data_center}-{host_ip}-PACKAGE_UUID` | `package_version_info` |
| Kubernetes / Helm | `PUT /helm-cluster` | SHA1 of `{cluster_name}` | `kubernetes_cluster_info` |

On each scrape, the collector `SCAN`s all Redis keys for the domain, deserializes every entry, and emits one Prometheus metric per entity - there is no in-memory cache, so every scrape hits Redis directly.

**Package EOL enrichment**: every `package-version` push is checked against `endoflife.date`, cached in Redis for 7 days under `eol_cache:all_packages`. Supported packages: `redis`, `memcached`, `mongodb`, `mysql`, `rabbitmq`, `envoy`, `debian`, `postgresql`, `elasticsearch`, `php`. Versions are compared as `major.minor` only (Debian epoch prefixes like `5:7.0.15-1~deb12u1` are stripped down to `7.0`).

## Quick start

Requires Go and a local Redis instance.

```bash
# run locally - must run from src/ so .env is found
cd src && go run main.go

# build a binary
go build -o keepup src/main.go

# build the Docker image
docker build -f docker/Dockerfile -t keepup .
```

The server listens on `LISTEN_PORT` (default `9101` in dev) and exposes:

- `PUT`/`GET /os-release`, `/package-version`, `/helm-cluster` - data ingestion & lookup (require `x-api-token`)
- `GET /metrics` - Prometheus scrape endpoint (no auth)
- `GET /healthcheck` - liveness probe

## Configuration

Config is loaded from environment variables. If `APP_ENV` is unset, `keepup` loads `src/.env` (development only). **All fields are required** - the app panics at startup if any are missing.

| Variable | Default (`.env`) | Purpose |
|---|---|---|
| `APP_ENV` | `dev` | when unset, triggers `.env` loading |
| `API_TOKEN` | `secret` | value required in the `x-api-token` header on every PUT/GET |
| `LISTEN_PORT` | `9101` | HTTP listen port |
| `REDIS_ADDR` | `127.0.0.1` | Redis host |
| `REDIS_PORT` | `6379` | Redis port |
| `REDIS_DBNO` | `7` | Redis logical DB number |
| `TTL_SECONDS` | `300` | expiry for every stored entry |

## API

All data endpoints require an `x-api-token` header matching `API_TOKEN`, and accept both `PUT` (insert) and `GET` (lookup by `id`).

### `PUT /os-release`

> **Deprecated** - kept for backwards compatibility, no longer receiving new fields (e.g. `team`). Do not build new integrations against it.

```jsonc
{
  "release": {
    "os_id": "debian",
    "version_codename": "bullseye",
    "version": "11 (bullseye)",
    "version_id": "11",
    "data_center": "aaa",
    "host_ip": "101.122.418.4"
  }
}
```

### `PUT /package-version`

```jsonc
{
  "packages": {
    "debian": "11",
    "mongodb": "7.3",
    "redis": "5:7.0.15-1~deb12u1",
    "mysql": "unknown",
    "host_ip": "101.122.418.4",
    "data_center": "aaa",
    "team": "platform"
  }
}
```

`host_ip`, `data_center`, and `team` are pulled out of the map and stored as entity metadata; every remaining key is treated as a package name -> installed version pair. Each package is enriched with `current_version_eof`, `newest_version`, and `expired` before being persisted.

### `PUT /helm-cluster`

```jsonc
{
  "cluster_name": "minikube",
  "kube_version": "1.29.0",
  "team": "platform",
  "helm_charts": [
    { "chart_name": "redis", "version": "18.1.5", "namespace": "database" },
    { "chart_name": "keepup", "version": "0.5.0", "namespace": "monitoring" }
  ]
}
```

Unlike the other two endpoints, the request body maps directly onto the stored struct (no wrapper key, no field filtering).

## Metrics

| Metric | Labels |
|---|---|
| `os_release_info` *(deprecated)* | `id`, `os_id`, `version_codename`, `version`, `version_id`, `data_center`, `host_ip` |
| `package_version_info` | `id`, `package_name`, `current_version`, `current_version_eof`, `newest_version`, `expired`, `data_center`, `host_ip`, `team` |
| `kubernetes_cluster_info` | `id`, `cluster_name`, `kube_version`, `chart_name`, `chart_version`, `chart_namespace`, `team` |

## Testing

There are no unit tests - only an end-to-end shell script that exercises all three endpoints against a running server:

```bash
# start the server first (see Quick start), then:
cd tests/end-to-end && ./run.sh
```

## Deploying with Helm

`charts/keepup/` deploys `keepup` with a Redis sidecar in the same pod (`redis.enabled: true` by default, so no external Redis is required). Key values:

- `apiToken` - auth token agents must send
- `ttlSeconds` - entry expiry
- `ingress.*` - expose the API externally
- `servicemonitor.enabled` - wire up Prometheus scraping automatically

```bash
helm install keepup charts/keepup --set apiToken=<your-token>
```

## Releasing

Pushing a git tag triggers `.github/workflows/build-docker-image.yml`, which builds and pushes the image to `ghcr.io/code-tool/keepup`. The build version is injected via `-ldflags "-X main.buildVersion=..."` and logged at startup.
