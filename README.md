# keepup

A lightweight Prometheus exporter that collects infrastructure inventory pushed by remote agents - package versions (with end-of-life enrichment) and Kubernetes/Helm deployments - and exposes it as metrics.

`keepup` holds no state of its own: Redis is both the write buffer and the read source. Agents `PUT` JSON, `keepup` validates and stores it with a TTL, and Prometheus scrapes `/metrics` on demand.

## Contents

- [How it works](#how-it-works)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [API](#api)
  - [`PUT /package-version`](#put-package-version)
  - [`PUT /helm-cluster`](#put-helm-cluster)
- [Metrics](#metrics)
- [Maintenance portfolio](#maintenance-portfolio)
- [Testing](#testing)
- [Deploying with Helm](#deploying-with-helm)
- [Releasing](#releasing)

## How it works

```
 agent(s)                    keepup                        Prometheus
-----------   PUT + token   ---------------------  scrape  --------------
│ pkg-vers│ --------------->│ handler --> Redis │<---------│ /metrics   |
│ helm    │                 │           (TTL)   │          │            │
-----------                 ---------------------          --------------
```

Every data domain follows the same shape:

| Domain | Endpoint | Redis key | Metric |
|---|---|---|---|
| Package versions | `PUT /package-version` | SHA1 of `{data_center}-{host_ip}-PACKAGE_UUID` | `package_version_info` |
| Kubernetes / Helm | `PUT /helm-cluster` | SHA1 of `{cluster_name}` | `kubernetes_cluster_info` |

On each scrape, the collector `SCAN`s all Redis keys for the domain, deserializes every entry, and emits one Prometheus metric per entity - there is no in-memory cache, so every scrape hits Redis directly.

**EOL enrichment**: every `package-version` and `helm-cluster` push is checked against `endoflife.date`, cached in Redis for 7 days under `eol_cache:all_packages`. Supported packages: `redis`, `memcached`, `mongodb`, `mysql`, `rabbitmq`, `envoy`, `debian`, `postgresql`, `elasticsearch`, `php`, `gitlab-runner`, `linux`, `openbao`, `metallb`, `authentik`, `argo-cd`, `nginx`, `kibana`, `grafana`. The installed version is matched to its release cycle by `major.minor`, falling back to `major` (Debian epoch prefixes like `5:7.0.15-1~deb12u1` are stripped down to `7.0`):

- `current_version_eof` - EOL of the installed cycle: a date, `true`/`false`, or `unknown` if the cycle isn't listed.
- `newest_version` - `major.minor` of the newest release.
- `expired` - `true` once the installed cycle has reached its EOL. An older release that is still supported is not expired.

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

- `PUT`/`GET /package-version`, `/helm-cluster` - data ingestion & lookup (require `x-api-token`)
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
| `PORTFOLIO_DIR` | none | optional, directory with team maintenance catalogs, enables the [maintenance portfolio](#maintenance-portfolio) metrics when set |

## API

All data endpoints require an `x-api-token` header matching `API_TOKEN`, and accept both `PUT` (insert) and `GET` (lookup by `id`).

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
    "team": "platform",
    "environment": "prod"
  }
}
```

`host_ip`, `data_center`, `team`, and `environment` are pulled out of the map and stored as entity metadata; every remaining key is treated as a package name -> installed version pair. Each package is enriched with `current_version_eof`, `newest_version`, and `expired` before being persisted.

### `PUT /helm-cluster`

```jsonc
{
  "cluster_name": "minikube",
  "kube_version": "1.29.0",
  "team": "platform",
  "environment": "non-prod",
  "helm_charts": [
    { "chart_name": "redis", "version": "18.1.5", "namespace": "database" },
    { "chart_name": "keepup", "version": "0.5.0", "namespace": "monitoring" }
  ]
}
```

`environment` (both endpoints) must be `prod` or `non-prod` (case-insensitive); it defaults to `prod` when omitted, and any other value is rejected with `400`.

Unlike the other two endpoints, the request body maps directly onto the stored struct (no wrapper key, no field filtering).

## Metrics

| Metric | Labels                                                                                                                                    |
|---|-------------------------------------------------------------------------------------------------------------------------------------------|
| `package_version_info` | `id`, `package_name`, `current_version`, `current_version_eof`, `newest_version`, `expired`, `data_center`, `host_ip`, `team`, `environment` |
| `kubernetes_cluster_info` | `id`, `cluster_name`, `kube_version`, `chart_name`, `chart_version`, `chart_namespace`, `current_version_eof`, `newest_version`, `expired`, `team`, `environment` |

Maintenance metrics are listed in [Maintenance portfolio](#maintenance-portfolio).

## Maintenance portfolio

Optional. Each team describes, per kind of object it maintains, how often and how long maintenance takes. `keepup` multiplies these costs by the real object counts from its inventory and reports the team's planned yearly maintenance workload next to its capacity.

One YAML file per team, loaded from `PORTFOLIO_DIR` once at startup (an invalid catalog stops the process):

```yaml
team: team-a              # must match the `team` sent by agents
capacity_days: 60.0       # optional, yearly maintenance capacity
hours_per_day: 8          # optional, default 8
objects:
  - name: mongo replicaset
    environment: prod     # prod | non-prod
    source: package       # package | helm | static
    key: mongodb          # package_name (package) or chart_name (helm)
    maintenances_per_year: 2
    hours_per_object: 0.5
    automation_factor: 1
  - name: sentry
    environment: prod
    source: static        # not visible to keepup - count comes from the catalog
    count: 1
    maintenances_per_year: 4
    hours_per_object: 12
    automation_factor: 1
```

Object counts per `team` x `environment`:

- `package` - hosts reporting the package with a known version (not `unknown`/empty);
- `helm` - distinct `cluster` x `namespace` pairs where the chart is installed;
- `static` - objects keepup cannot see and their number is set by hand in the catalog's `count` field.

Unknown fields, unsupported `environment`/`source` values, non-positive costs and duplicate `(source, key, environment)` entries are rejected.

| Metric | Labels | Value |
|---|---|---|
| `maintenance_objects` | `team`, `environment`, `object`, `source`, `key` | object count |
| `maintenance_hours_annual` | same | `objects x maintenances_per_year x hours_per_object x automation_factor` |
| `maintenance_capacity_hours` | `team` | `capacity_days x hours_per_day` |
| `maintenance_uncatalogued_objects` | `team`, `environment`, `source`, `key` | inventory objects of a catalogued team that match no entry |

```promql
sum by (team, environment) (maintenance_hours_annual) / 8                        # days per year
sum by (team) (maintenance_hours_annual) / on (team) maintenance_capacity_hours  # load, < 1 fits capacity
```

## Testing

Unit tests cover the handler, metrics and portfolio packages against an in-process fake Redis ([`miniredis`](https://github.com/alicebob/miniredis)) - no external services required:

```bash
go test ./...
```

An end-to-end shell script also exercises both endpoints against a running server:

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
- `portfolio.*` - team maintenance catalogs, see below

```bash
helm install keepup charts/keepup --set apiToken=<your-token>
```

Maintenance catalogs are mounted into `/etc/keepup/portfolio` and enable the portfolio metrics:

```yaml
portfolio:
  enabled: true
  teams:                    # rendered into the keepup-portfolio ConfigMap, one <team>.yaml per key
    team-a:
      capacity_days: 60.0
      objects: [...]
  existingConfigMaps: []    # or ConfigMaps managed elsewhere, each with <team>.yaml keys
```

## Releasing

Pushing a git tag triggers `.github/workflows/build-docker-image.yml`, which builds and pushes the image to `ghcr.io/code-tool/keepup`. The build version is injected via `-ldflags "-X main.buildVersion=..."` and logged at startup.
