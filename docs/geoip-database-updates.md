# GeoIP Database Updates

The service looks up the country of an IP address using MaxMind's **GeoLite2-Country** database (a `.mmdb` file). The service never downloads the file itself. Keeping it current is the job of [geoipupdate](https://github.com/maxmind/geoipupdate), MaxMind's official update tool, running alongside it.

This guide covers how updates work, how to set them up in each environment, and how to check they are working.

- [How it works](#how-it-works)
- [Prerequisites](#prerequisites)
- [Running locally](#running-locally)
- [Running in Docker](#running-in-docker)
- [Running in Kubernetes](#running-in-kubernetes)
- [Monitoring freshness](#monitoring-freshness)
- [Licence obligations](#licence-obligations)
- [Troubleshooting](#troubleshooting)

## How it works

1. **geoipupdate downloads the latest database** from MaxMind and writes it to a directory. It writes to a temporary file first and then renames it over the old one, so the service never sees a half-written file. MaxMind publishes GeoLite2 updates twice a week.
2. **The service reads the file at `DB_PATH` fully into memory on startup**, and logs the database's build date. If the file is missing or invalid, it exits with an error.
3. **Every minute, the service checks the file's modification time.** When it changes, the new file is loaded into memory and swapped in atomically:
   - Lookups already in progress finish on the old copy, and new lookups use the new copy.
   - No lookup waits on a lock, and no restart is needed.
   - If a reload fails, the service logs the error and keeps serving the copy it already has, then tries again a minute later.

Because the file is only open while it is being read, geoipupdate can replace it at any time without affecting the running service, even on network storage.

## Prerequisites

You need a MaxMind account and license key:

1. Sign up for a free GeoLite account at <https://www.maxmind.com/en/geolite2/signup>.
2. Generate a license key under **Account → Manage License Keys**.
3. Note your **account ID** and **license key**. Treat the license key as a secret.

MaxMind limits how many databases an account can download per day. Every running copy of geoipupdate counts against that limit, so don't run more updaters than you need (see [Scaling considerations](#scaling-considerations)).

### Configuration reference

| Setting | Used by | Description |
|---|---|---|
| `DB_PATH` | Service | Path to `GeoLite2-Country.mmdb` (default `data/GeoLite2-Country.mmdb`) |
| `AccountID` / `GEOIPUPDATE_ACCOUNT_ID` | geoipupdate | MaxMind account ID |
| `LicenseKey` / `GEOIPUPDATE_LICENSE_KEY` | geoipupdate | MaxMind license key |
| `EditionIDs` / `GEOIPUPDATE_EDITION_IDS` | geoipupdate | Always `GeoLite2-Country` |
| `GEOIPUPDATE_FREQUENCY` | geoipupdate (Docker images) | Hours between updates. `0` (or unset, in MaxMind's image) means download once and stop; see [Disabling refreshes](#disabling-refreshes) |
| `GEOIPUPDATE_DB_DIR` | geoipupdate (Docker images) | Output directory (default `/usr/share/GeoIP`) |

The left-hand names are for the geoipupdate CLI's config file; the `GEOIPUPDATE_*` names are environment variables.

---

## Running locally

### 1. Download the database

**With Docker** (any OS), a one-off run of the geoipupdate image writes the database into `data/` and exits:

```bash
docker run --rm -v "$(pwd)/data:/usr/share/GeoIP" \
  -e GEOIPUPDATE_ACCOUNT_ID=<your-account-id> \
  -e GEOIPUPDATE_LICENSE_KEY=<your-license-key> \
  -e GEOIPUPDATE_EDITION_IDS=GeoLite2-Country \
  ghcr.io/maxmind/geoipupdate:v8.0.0
```

**With the geoipupdate CLI**, install it:

| OS | Install |
|---|---|
| Windows | Download the Windows zip from the [releases page](https://github.com/maxmind/geoipupdate/releases) and put `geoipupdate.exe` on your `PATH` |
| macOS | `brew install geoipupdate` |
| Ubuntu/Debian | `sudo add-apt-repository ppa:maxmind/ppa && sudo apt install geoipupdate`, or install the `.deb` from the releases page |
| Other Linux | Install the `.rpm` or `.tar.gz` from the releases page |

Then create a `GeoIP.conf` file **outside the repository**, because it contains your license key:

```
AccountID YOUR_ACCOUNT_ID
LicenseKey YOUR_LICENSE_KEY
EditionIDs GeoLite2-Country
```

From the repository root, run:

```sh
geoipupdate -f path/to/GeoIP.conf -d data
```

Either way, the result is `data/GeoLite2-Country.mmdb`, which is git-ignored.

### 2. Run the service

```sh
go run ./cmd/server
```

`data/GeoLite2-Country.mmdb` is the default `DB_PATH`, so no configuration is needed.

### 3. Keep it updated

Re-run the download command whenever you want fresh data. To automate it, schedule it with Task Scheduler on Windows or cron on macOS/Linux, for example daily. A running service picks up each new file within a minute.

---

## Running in Docker

### Built-in updater (default)

The image built from [deploy/Dockerfile](../deploy/Dockerfile) is based on MaxMind's geoipupdate image, and its entrypoint keeps the database current by itself:

1. On startup, it runs geoipupdate once. If that fails (for example, because of bad credentials), the container exits rather than starting without data.
2. It starts a background loop that runs geoipupdate every `GEOIPUPDATE_FREQUENCY` hours (default 24). Failures are logged and retried on the next cycle, and the service keeps serving the data it has. If `GEOIPUPDATE_FREQUENCY` is `0`, no loop is started (see [Disabling refreshes](#disabling-refreshes)). If it isn't a whole number, the container exits with an error rather than guessing.
3. It starts the service, which reads from `/usr/share/GeoIP/GeoLite2-Country.mmdb`.

Credentials are supplied at runtime, never baked into the image:

```bash
docker build -f deploy/Dockerfile -t avoxi-coding-challenge .
docker run -d --rm --name avoxi -p 8080:8080 -p 9090:9090 \
  -e GEOIPUPDATE_ACCOUNT_ID=<your-account-id> \
  -e GEOIPUPDATE_LICENSE_KEY=<your-license-key> \
  avoxi-coding-challenge
```

The data lives inside the container, so each new container downloads it again on startup. That's fine for a single long-running container; for many containers, see [Scaling considerations](#scaling-considerations).

### Disabling refreshes

Set `GEOIPUPDATE_FREQUENCY=0` to download the database once at startup and never refresh it while the container runs. The log says so at startup:

```
GEOIPUPDATE_FREQUENCY is 0: the database will not be refreshed until the container restarts
```

This suits short-lived containers, or environments that refresh by redeploying. Two things to keep in mind:

- **Licence:** MaxMind requires old copies to be deleted within 30 days of a new release, so a container with refreshes disabled must be restarted (and so re-download) at least that often.
- **Kubernetes:** this applies to the image's built-in updater only. The Kubernetes sidecar must keep a non-zero frequency. At `0` geoipupdate exits after one download, and Kubernetes restarts an exited native sidecar, so it would download repeatedly and exhaust the daily download limit.

### Separate updater with Docker Compose

To keep the data in a volume that survives container restarts, run geoipupdate as its own container and start the service binary directly (`entrypoint: ["/app"]`), bypassing the built-in updater. Put your credentials in a `.env` file next to `compose.yaml`, and don't commit it:

```
MAXMIND_ACCOUNT_ID=YOUR_ACCOUNT_ID
MAXMIND_LICENSE_KEY=YOUR_LICENSE_KEY
```

`compose.yaml` in the repository root:

```yaml
services:
  geoipupdate:
    image: ghcr.io/maxmind/geoipupdate:v8.0.0
    restart: unless-stopped
    environment:
      GEOIPUPDATE_ACCOUNT_ID: ${MAXMIND_ACCOUNT_ID}
      GEOIPUPDATE_LICENSE_KEY: ${MAXMIND_LICENSE_KEY}
      GEOIPUPDATE_EDITION_IDS: GeoLite2-Country
      GEOIPUPDATE_FREQUENCY: "24"
    volumes:
      - geoip:/usr/share/GeoIP
    healthcheck:
      # Healthy once the first download has finished
      test: ["CMD", "test", "-f", "/usr/share/GeoIP/GeoLite2-Country.mmdb"]
      interval: 5s
      retries: 60

  app:
    build:
      context: .
      dockerfile: deploy/Dockerfile
    entrypoint: ["/app"]
    environment:
      DB_PATH: /usr/share/GeoIP/GeoLite2-Country.mmdb
    ports:
      - "8080:8080"
      - "9090:9090"
    volumes:
      - geoip:/usr/share/GeoIP:ro
    depends_on:
      geoipupdate:
        condition: service_healthy

volumes:
  geoip:
```

```sh
docker compose up --build
```

The `depends_on` condition holds the service back until the first download has finished, so it doesn't exit at startup because the file is missing.

---

## Running in Kubernetes

[deploy/k8s/deployment.yaml](../deploy/k8s/deployment.yaml) runs geoipupdate as a **sidecar** in each pod. The [README](../README.md#running-in-kubernetes) has the deployment steps; this section explains how the updates work.

- **geoipupdate is a [native sidecar](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/):** an init container with `restartPolicy: Always`. Kubernetes starts it first and only starts the app container once its `startupProbe` passes, meaning the first download has finished. It keeps running for the life of the pod and refreshes the data every 24 hours. Native sidecars require Kubernetes 1.29 or later.
- **The two containers share an `emptyDir` volume** mounted at `/usr/share/GeoIP`, so no shared storage is needed. The app mounts it read-only.
- **The app container runs `/app` directly** (`command: ["/app"]`), bypassing the image's built-in updater, because the sidecar already does that job.
- **Credentials come from the `maxmind-credentials` Secret** in the `geoip-api` namespace. To create it outside the manifest:

  ```sh
  kubectl -n geoip-api create secret generic maxmind-credentials \
    --from-literal=account-id=YOUR_ACCOUNT_ID \
    --from-literal=license-key=YOUR_LICENSE_KEY
  ```

**On clusters older than 1.29**, replace the native sidecar with two containers:
- **Init container:** a plain geoipupdate container without `GEOIPUPDATE_FREQUENCY`. It downloads once and exits before the app starts.
- **Updater:** a second geoipupdate container under `containers`, with `GEOIPUPDATE_FREQUENCY` set, to keep the file updated.

This downloads the database twice each time a pod starts.

### Scaling considerations

Each pod runs its own geoipupdate, so every replica counts against MaxMind's daily download limit, and pods download again whenever they restart. That is fine for a handful of replicas. With many replicas, switch to a single updater:

- A Kubernetes **CronJob** runs geoipupdate on a schedule and writes to a **ReadWriteMany** PersistentVolumeClaim, backed by network storage such as NFS, EFS or Azure Files.
- App pods mount the same claim read-only, point `DB_PATH` at it, and drop the sidecar.

This results in one download per schedule, regardless of replica count. The service is safe on network storage because it reads the file into memory instead of memory-mapping it.

---

## Monitoring freshness

Two signals show whether updates are reaching the service:

- **Logs.** Every successful load is logged with the database's build date, so a new line should appear after each MaxMind release:

  ```
  {"time":"…","level":"INFO","msg":"loaded database","path":"/usr/share/GeoIP/GeoLite2-Country.mmdb","built":"2026-10-06T21:21:33Z"}
  ```

  Reload failures are logged at `WARN` with the message `reloading database failed, still serving the previous copy`.

- **`GET /health`** reports the build date of the data currently being served:

  ```json
  {"status": "ok", "database_build": "2026-10-06T21:21:33Z"}
  ```

Since MaxMind releases twice a week, alert if `database_build` is more than about 7 days old. That means updates have stopped, usually because of revoked credentials or the download limit.

Stale data deliberately does **not** fail the health check. Every replica would go stale at the same moment, so failing the check would take the whole service down over a data-freshness problem.

## Licence obligations

The [GeoLite EULA](https://www.maxmind.com/en/geolite2/eula) shapes how the data is handled:

- **No redistribution.** The database can't be shared with third parties, which is why it isn't committed to this repository. Each environment downloads its own copy with its own license key. The tests use MaxMind's freely licensed [test database](https://github.com/maxmind/MaxMind-DB) instead.
- **Delete old copies within 30 days of a new release.** geoipupdate replaces the file in place, and the service drops old copies from memory after a reload, so the setups above don't keep old versions. If you download manually, or run with refreshes disabled, re-download at least every 30 days and don't keep old copies.
- **Attribution.** Products using the data must credit MaxMind; see the README's Attribution section.

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| Service exits on startup with `loading database "..."` and a file-not-found error | The database hasn't been downloaded yet, or `DB_PATH` doesn't match where geoipupdate writes it |
| Container exits immediately with `the AccountID option is required` | The image's built-in updater was started without `GEOIPUPDATE_ACCOUNT_ID` / `GEOIPUPDATE_LICENSE_KEY` |
| geoipupdate reports `401` / `AUTHORIZATION_INVALID` | Wrong account ID or license key, or the key was revoked |
| geoipupdate reports a download limit error | Too many updaters or restarts on one account; see [Scaling considerations](#scaling-considerations) |
| Kubernetes pods stuck in `Init` | The sidecar can't download; check `kubectl -n geoip-api logs deploy/geoip-api -c geoipupdate` |
| `reloading database failed` warnings in the logs | A transient read failure while the file was being replaced. The service keeps serving the previous copy and retries a minute later |
| `database_build` in `/health` stops advancing | Updates have stopped; check the updater's logs for the errors above |
