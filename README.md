# avoxi_coding_challenge

An HTTP and gRPC service that takes an IP address and a list of allowed countries, looks up the IP's country in MaxMind's GeoLite2-Country database, and reports whether it is allowed. It is designed to sit behind the API gateway, which supplies the customer's allowed countries for each login.

```bash
curl -X POST http://localhost:8080/v1/check \
  -H "Content-Type: application/json" \
  -d '{"ip": "8.8.8.8", "allowed_countries": ["US", "GB"]}'
# {"country":"US","allowed":true}
```

- [Quick start](#quick-start)
- [Running in Docker](#running-in-docker)
- [Running in Kubernetes](#running-in-kubernetes)
- [HTTP API](#http-api)
- [gRPC API](#grpc-api)
- [Behaviour](#behaviour)
- [Extensions](#extensions)
- [Keeping the data up to date](#keeping-the-data-up-to-date)
- [Configuration](#configuration)
- [Development](#development)

## Quick start

Requires [Go 1.27.1](https://go.dev/dl/) or later.

### 1. Get a MaxMind license key

The GeoLite2 database can't be redistributed, so it isn't included in this repository; each user downloads their own copy. Sign up for a free [MaxMind GeoLite account](https://www.maxmind.com/en/geolite2/signup), then generate a license key under **Account → Manage License Keys**. Note your account ID and license key.

### 2. Download the database into `data/`

The easiest way on any OS is a one-off run of MaxMind's [geoipupdate](https://github.com/maxmind/geoipupdate) image. Without `GEOIPUPDATE_FREQUENCY` set, it downloads once and exits:

```bash
docker run --rm -v "$(pwd)/data:/usr/share/GeoIP" \
  -e GEOIPUPDATE_ACCOUNT_ID=<your-account-id> \
  -e GEOIPUPDATE_LICENSE_KEY=<your-license-key> \
  -e GEOIPUPDATE_EDITION_IDS=GeoLite2-Country \
  ghcr.io/maxmind/geoipupdate:v8.0.0
```

In PowerShell, use `${PWD}` instead of `$(pwd)` and replace each trailing `\` with a backtick.

Without Docker, you can either:
- install the geoipupdate CLI (see [docs/geoip-database-updates.md](docs/geoip-database-updates.md#running-locally)), or
- download **GeoLite2 Country** (GZIP) from **Account → Download Databases** and extract `GeoLite2-Country.mmdb` into `data/`.

`data/*.mmdb` is git-ignored. MaxMind's licence requires deleting old copies within 30 days of a new release, so re-run the download regularly.

### 3. Run the service

```bash
go run ./cmd/server
```

```
{"time":"…","level":"INFO","msg":"loaded database","path":"data/GeoLite2-Country.mmdb","built":"2026-10-06T21:21:33Z"}
{"time":"…","level":"INFO","msg":"gRPC server listening","addr":":9090"}
{"time":"…","level":"INFO","msg":"HTTP server listening","addr":":8080"}
```

In another terminal:

```bash
curl http://localhost:8080/health
curl -X POST http://localhost:8080/v1/check \
  -H "Content-Type: application/json" \
  -d '{"ip": "8.8.8.8", "allowed_countries": ["JP"]}'
# {"country":"US","allowed":false,"reason":"country_not_allowed"}
```

On Windows PowerShell, `curl` is an alias for `Invoke-WebRequest`. Use `Invoke-RestMethod` instead:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod -Method Post -Uri http://localhost:8080/v1/check -ContentType application/json `
  -Body '{"ip": "8.8.8.8", "allowed_countries": ["JP"]}'
```

Stop the server with Ctrl+C. It finishes in-flight requests (up to 10 seconds) before exiting.

## Running in Docker

Build the image from the repository root:

```bash
docker build -f deploy/Dockerfile -t avoxi-coding-challenge .
```

The image includes MaxMind's [geoipupdate](https://github.com/maxmind/geoipupdate). On startup it downloads the latest database, then refreshes it every 24 hours in the background. This needs a free [MaxMind account](https://www.maxmind.com/en/geolite2/signup): sign up, then generate a license key under **Account → Manage License Keys**.

```bash
docker run -d --rm --name avoxi -p 8080:8080 -p 9090:9090 \
  -e GEOIPUPDATE_ACCOUNT_ID=<your-account-id> \
  -e GEOIPUPDATE_LICENSE_KEY=<your-license-key> \
  avoxi-coding-challenge

docker logs avoxi    # check it started
docker stop avoxi
```

Without credentials, the container exits immediately with `the AccountID option is required`.

**Using a database you've already downloaded:** if you followed the [quick start](#quick-start), you can mount `data/` and skip the built-in updater by running the binary directly. The data then won't refresh itself.

```bash
docker run -d --rm --name avoxi -p 8080:8080 -p 9090:9090 \
  -v "$(pwd)/data:/data:ro" -e DB_PATH=/data/GeoLite2-Country.mmdb \
  --entrypoint /app avoxi-coding-challenge
```

In PowerShell, use `${PWD}` instead of `$(pwd)` and replace the trailing `\` with a backtick.

## Running in Kubernetes

[deploy/k8s/deployment.yaml](deploy/k8s/deployment.yaml) creates:
- a `geoip-api` namespace
- a `maxmind-credentials` Secret
- a two-replica Deployment
- a ClusterIP Service: HTTP on port 80, gRPC on port 9090

Each pod runs geoipupdate as a [native sidecar](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/). It downloads the database before the app starts, then keeps it fresh. This requires Kubernetes 1.29 or later.

1. **Build and push the image** to a registry your cluster can pull from:

   ```bash
   docker build -f deploy/Dockerfile -t <your-registry>/geoip-api:<tag> .
   docker push <your-registry>/geoip-api:<tag>
   ```

   On a local cluster that shares Docker's image store (such as Docker Desktop), building is enough.

2. **Edit the manifest:**
   - set the app container's `image` to the tag you pushed
   - replace `YOUR_ACCOUNT_ID` and `YOUR_LICENSE_KEY` in the Secret

   Don't commit your real license key. Alternatively, delete the Secret from the manifest and create it separately with `kubectl -n geoip-api create secret generic maxmind-credentials --from-literal=account-id=... --from-literal=license-key=...`.

3. **Apply it and wait for the rollout:**

   ```bash
   kubectl apply -f deploy/k8s/deployment.yaml
   kubectl -n geoip-api rollout status deployment/geoip-api
   ```

4. **Test it** through a port-forward:

   ```bash
   kubectl -n geoip-api port-forward svc/geoip-api 8080:80 9090:9090
   curl http://localhost:8080/health
   ```

   Other workloads in the cluster reach it at `http://geoip-api.geoip-api.svc` (HTTP) and `geoip-api.geoip-api.svc:9090` (gRPC).

If pods are stuck in `Init`, check the sidecar's logs with `kubectl -n geoip-api logs deploy/geoip-api -c geoipupdate`. Usually the credentials are wrong.

## HTTP API

### `POST /v1/check`

Request:

```json
{"ip": "8.8.8.8", "allowed_countries": ["US", "GB"]}
```

| Field | Description |
|---|---|
| `ip` | IPv4 or IPv6 address |
| `allowed_countries` | Non-empty list of [ISO 3166-1 alpha-2](https://en.wikipedia.org/wiki/ISO_3166-1_alpha-2) codes, compared case-insensitively. Entries that aren't valid codes (e.g. `"USA"`, or `"UK"` instead of `"GB"`) are ignored and logged as errors, rather than failing the request |

Responses (`200 OK`):

```json
{"country": "US", "allowed": true}
{"country": "US", "allowed": false, "reason": "country_not_allowed"}
{"country": "", "allowed": false, "reason": "country_unknown"}
```

| Field | Description |
|---|---|
| `country` | ISO code the IP resolved to. Empty if the database has no country for it |
| `allowed` | Whether `country` is in `allowed_countries` |
| `reason` | Only present when `allowed` is false: `country_not_allowed`, or `country_unknown` when the IP's country can't be determined (e.g. private ranges such as `10.0.0.1`) |

Errors return `{"error": "<message>"}` with:

| Status | When |
|---|---|
| `400 Bad Request` | Malformed JSON, unknown fields, an invalid IP, or empty `allowed_countries` |
| `500 Internal Server Error` | The lookup itself failed (e.g. a corrupt database). Details are logged, not returned |

A disallowed IP is a successful `200` with `"allowed": false`, not an error. An error means no decision was made. The gateway should decide whether to fail open or closed in that case, rather than treat the error as a denial.

### `GET /health`

```json
{"status": "ok", "database_build": "2026-10-06T21:21:33Z"}
```

Always `200` while the server is running: it won't start without a database, and keeps serving the last good one if a refresh fails. `database_build` is when MaxMind built the loaded data, so monitoring can alert if it stops advancing.

## gRPC API

Defined in [proto/iplookup/v1/iplookup.proto](proto/iplookup/v1/iplookup.proto) as `iplookup.v1.IPLookupService/Check`. It has the same inputs and outputs as `POST /v1/check`, with `reason` as an enum (`REASON_COUNTRY_NOT_ALLOWED`, `REASON_COUNTRY_UNKNOWN`, or `REASON_UNSPECIFIED` when allowed).

| Status code | When |
|---|---|
| `INVALID_ARGUMENT` | An invalid IP, or empty `allowed_countries` |
| `INTERNAL` | The lookup failed |

Server reflection is enabled, so [grpcurl](https://github.com/fullstorydev/grpcurl) needs no `.proto` file:

```bash
grpcurl -plaintext localhost:9090 list
grpcurl -plaintext -d '{"ip": "8.8.8.8", "allowed_countries": ["US", "GB"]}' \
  localhost:9090 iplookup.v1.IPLookupService/Check
# {"country": "US", "allowed": true}
```

grpcurl omits fields at their default value, so a denied unknown-country response shows only `{"reason": "REASON_COUNTRY_UNKNOWN"}`.

If grpcurl isn't installed, you can run it with Docker Desktop:

```bash
docker run --rm fullstorydev/grpcurl:v1.9.3 -plaintext host.docker.internal:9090 list
```

The standard [gRPC health service](https://github.com/grpc/grpc/blob/master/doc/health-checking.md) (`grpc.health.v1.Health`) reports `SERVING` while the server is running.

## Behaviour

- **IPv4 and IPv6** are both supported, including IPv4-mapped IPv6 (`::ffff:8.8.8.8`).
- **Unknown countries are denied.** Private and reserved ranges, and the roughly 2,000 networks MaxMind can't place in a country, return `country_unknown`.
- **Only the IP's actual location is used.** MaxMind also records a `registered_country`, which is where the network's owner registered it. That is often not where the user is (for example, Cloudflare's `1.1.1.0/24` is registered in Australia), so it is deliberately not used as a fallback.
- **Invalid country codes are ignored, not rejected.** One bad entry in a customer's list (such as `"UK"`) shouldn't lock out users from that customer's valid countries. Each request with invalid entries logs an error naming them, so the configuration can be fixed.
- **Logs are structured JSON** on stderr: one line per request (method, path or RPC, status, duration), plus database loads, reload failures and errors. End users' IP addresses are never logged.
- **VPNs and proxies are not detected.** This is out of scope for phase one; the service checks the presented IP only.

## Extensions

Beyond the core requirement and the listed bonus items (keeping the data updated, Docker, Kubernetes and gRPC), I added the following. Each entry is the case I'd make for it in a design meeting.

### Reasons for denials

**What:** a denied result includes a `reason`: `country_not_allowed`, or `country_unknown` when the IP's location can't be determined (`REASON_*` enum values in gRPC).

**Why:** a bare `allowed: false` covers two very different situations. "You're logging in from a country your company doesn't allow" is a policy working as intended. "We couldn't tell where you are" is usually a private network or an unusual address, and the user's IT team can likely fix it. When a customer's employee is locked out and calls support, the reason in the gateway's logs turns a support investigation into a lookup. It costs one optional field, and an allowed response is unchanged.

### Tolerating mistakes in customer country lists

**What:** entries in `allowed_countries` are checked against the real ISO 3166-1 list. Invalid ones, such as `"USA"` or `"UK"` (instead of `"GB"`), are ignored and logged as errors naming the bad values. The rest of the list still applies.

**Why:** If a correctly passed ID is valid, we do not want to cause a failure, when we could properly treat it as a success. The error will still be logged if a user is seeing unexpected denials.

### Zero-downtime data reloads

**What:** when geoipupdate replaces the database file, the service loads the new copy and swaps it in atomically, without a restart. Lookups never wait on a lock, in-flight lookups finish on the old copy, and a failed reload keeps the last good data in service.

**Why:** MaxMind publishes updates twice a week. Picking them up by restarting would mean a rolling restart of every replica twice a week, with each pod downloading again. Reloading in place costs about 40 lines, makes updates invisible to callers, and turns a bad download into a logged warning instead of an outage.

### Data freshness reporting

**What:** `GET /health` reports `database_build`, the date MaxMind built the data being served, and every load is logged with the same date.

**Why:** the most likely long-term failure is quiet: the license key gets revoked or a download limit is hit, updates stop, and the service keeps answering from data that gets older every week. Nothing errors. Exposing the build date lets monitoring alert when it stops advancing (around 7 days without an update). The service deliberately doesn't fail its health check on old data: every replica would go stale at once, and a data-freshness problem would become a full outage.

### Structured logging

**What:** JSON logs via `log/slog`:
- one line per HTTP request and gRPC call, with the method, status and duration
- separate entries for database loads, reload failures, lookup errors and invalid country codes
- level set by `LOG_LEVEL`, with health checks at debug level so Kubernetes probes don't flood the logs

**Why:** this service sits on every login path, so when logins fail the first question is whether it was the cause. Structured fields can be queried and alerted on directly (`status`, `duration_ms`, `invalid`), with no text parsing. End users' IP addresses are deliberately not logged, which keeps personal data out of the logs.

### Graceful shutdown

**What:** in Kubernetes, a terminating pod first pauses for 5 seconds (a `preStop` hook) while it is taken out of the Service's rotation, so requests still being routed to it are served. Then, on SIGTERM, the HTTP and gRPC servers stop accepting connections and finish in-flight requests, with a shared 10-second deadline, before the process exits.

**Why:** Kubernetes sends SIGTERM on every deployment, scale-down and node drain. Without draining, each rollout fails whatever login checks are in flight at that moment, plus any that arrive before the rest of the cluster stops routing to the pod: a small but certain error rate on every release. It's about 30 lines in `main` and one hook in the manifest.

### Health checks and gRPC reflection

**What:**
- `GET /health` for the Kubernetes probes
- the standard `grpc.health.v1` service for gRPC-aware load balancers
- server reflection, so tools like `grpcurl` and Postman can call the API without the `.proto` file

**Why:** health checks let Kubernetes hold traffic until a pod is serving and replace a stuck one, which is what makes rolling deployments safe. Reflection makes the gRPC API as easy to debug as `curl` makes the HTTP one. Both are a few lines each and use standard protocols, so existing tooling works unchanged.

### Considered and deferred

- **`registered_country` fallback.** For the roughly 2,000 networks with no location, MaxMind also records where the network's owner registered it. That isn't where the user is (Cloudflare's `1.1.1.0/24` is registered in Australia), so for a security control I chose to deny rather than guess.
- **Metrics (Prometheus).** These would be the natural next step after logging: request rates by outcome and reason, latency, and data age as a gauge. Deferred because the logs already carry the same signals for a service at this stage.
- **VPN and proxy detection.** Out of scope for phase one, per the requirements. It needs a separate data source, such as MaxMind's anonymous IP database, and the `reason` field leaves room for a `vpn_detected` value.

## Keeping the data up to date

MaxMind updates GeoLite2 twice a week. The service never downloads data itself. [geoipupdate](https://github.com/maxmind/geoipupdate), MaxMind's official updater, keeps the file at `DB_PATH` current. In Docker it runs inside the container; in Kubernetes it runs as a sidecar.

Once a minute, the service checks the file's modification time. When geoipupdate replaces the file, the new data is loaded and swapped in atomically, so there's no restart and no lookup is blocked or fails. If a reload fails, the service logs it and keeps serving the previous data. Each successful load is logged with the database build date, which `/health` also reports.

[docs/geoip-database-updates.md](docs/geoip-database-updates.md) covers the details: running geoipupdate locally, Docker Compose, and scaling to many replicas without exceeding MaxMind's download limits.

## Configuration

Environment variables:

| Variable | Default | Description |
|---|---|---|
| `ADDR` | `:8080` | HTTP listen address |
| `GRPC_ADDR` | `:9090` | gRPC listen address |
| `LOG_LEVEL` | `info` | Minimum log level: `debug`, `info`, `warn` or `error`. Health-check requests are logged at `debug` |
| `DB_PATH` | `data/GeoLite2-Country.mmdb` (`/usr/share/GeoIP/GeoLite2-Country.mmdb` in the image) | Path to the database |
| `GEOIPUPDATE_ACCOUNT_ID` | | MaxMind account ID (Docker image only) |
| `GEOIPUPDATE_LICENSE_KEY` | | MaxMind license key (Docker image only) |
| `GEOIPUPDATE_FREQUENCY` | `24` | Hours between database refreshes (Docker image only). `0` downloads once at startup and never refreshes; see the [update guide](docs/geoip-database-updates.md#disabling-refreshes) |

If you change `ADDR` or `GRPC_ADDR` in Docker, change the container side of the matching `-p` too.

## Development

For how the service works internally (data structures, concurrency, lifecycle, and the reasoning behind its design decisions), see the [low-level design](docs/low-level-design.md).

```
cmd/server/          entrypoint: configuration, servers, graceful shutdown
internal/iplookup/   the service (api.go) and its HTTP (http.go) and gRPC (grpc.go) adapters
  testdata/          MaxMind's test database, used by the tests
proto/               gRPC API definition
gen/                 generated gRPC code (committed)
deploy/              Dockerfile and Kubernetes manifest
docs/                design doc, database update guide, AI usage notes
data/                your local GeoLite2-Country database (git-ignored; see Quick start)
```

Run the tests. They use MaxMind's [test database](https://github.com/maxmind/MaxMind-DB) (Apache-2.0/MIT licensed, with fixed records) rather than real GeoLite data, so they need no MaxMind account and don't change when the data does:

```bash
go test ./...
```

After editing the `.proto`, regenerate `gen/` with [buf](https://buf.build/docs/installation). It uses buf's remote plugins, so it needs network access (the `.proto` is sent to buf.build):

```bash
buf lint && buf generate
# or, without installing buf:
docker run --rm -v "$(pwd):/workspace" -w /workspace bufbuild/buf:1.73.0 generate
```

## AI usage

This project was built with help from Claude Code. [docs/ai-usage.md](docs/ai-usage.md) covers how it was used and verified, which suggestions were accepted or rejected and why, and the full prompt log.

## Attribution

This product includes GeoLite2 data created by MaxMind, available from [https://www.maxmind.com](https://www.maxmind.com).
