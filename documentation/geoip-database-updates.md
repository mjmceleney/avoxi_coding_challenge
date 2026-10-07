# GeoIP Database Updates

The service looks up the country of an IP address using MaxMind's **GeoLite2-Country** database (a `.mmdb` file). The application never downloads this file itself. Keeping it current is the job of [`geoipupdate`](https://github.com/maxmind/geoipupdate), MaxMind's official update tool, running alongside the application.

## How it works

1. **geoipupdate** downloads the latest database from MaxMind and writes it to a directory. It writes to a temporary file first and then renames it over the old one, so the database file is never seen half-written.
2. **The application** reads the file at `DB_PATH` fully into memory on startup. If the file is missing or invalid, it exits with an error.
3. Every minute, the application checks the file's modification time. When it changes, the new file is loaded into memory and swapped in atomically:
   - Lookups already in progress finish on the old copy.
   - New lookups use the new copy.
   - No lookup ever waits on a lock.
   - If a reload fails, the application logs the error and keeps serving the copy it already has.

Because the file is only open while it is being read, geoipupdate can replace it at any time without affecting the running application.

> **Note:** `main` currently performs a single demo lookup and exits. The reload loop only matters once the service runs as a long-lived API.

## Prerequisites (all environments)

- A MaxMind account with a license key. Sign up for GeoLite2 at <https://www.maxmind.com/en/geolite2/signup>, then generate a key under **Account → Manage License Keys**.
- Your **Account ID** and **License Key**. Treat the license key as a secret.

MaxMind limits how many databases an account can download per day. Every running copy of geoipupdate counts against that limit, so avoid running more updaters than you need.

### Configuration reference

| Setting | Used by | Description |
|---|---|---|
| `DB_PATH` | Application | Path to `GeoLite2-Country.mmdb` |
| `AccountID` / `GEOIPUPDATE_ACCOUNT_ID` | geoipupdate | MaxMind account ID |
| `LicenseKey` / `GEOIPUPDATE_LICENSE_KEY` | geoipupdate | MaxMind license key |
| `EditionIDs` / `GEOIPUPDATE_EDITION_IDS` | geoipupdate | Always `GeoLite2-Country` |
| `GEOIPUPDATE_FREQUENCY` | geoipupdate (Docker image) | Hours between updates. If unset, it runs once and exits |
| `GEOIPUPDATE_DB_DIR` | geoipupdate (Docker image) | Output directory (default `/usr/share/GeoIP`) |

---

## Running locally

### 1. Install geoipupdate

| OS | Install |
|---|---|
| Windows | Download the Windows zip from the [releases page](https://github.com/maxmind/geoipupdate/releases) and put `geoipupdate.exe` on your `PATH` |
| macOS | `brew install geoipupdate` |
| Ubuntu/Debian | `sudo add-apt-repository ppa:maxmind/ppa && sudo apt install geoipupdate`, or install the `.deb` from the releases page |
| Other Linux | Install the `.rpm` or `.tar.gz` from the releases page |

### 2. Create a config file

Create `GeoIP.conf` outside the repository, or add it to `.gitignore`, because it contains your license key:

```
AccountID YOUR_ACCOUNT_ID
LicenseKey YOUR_LICENSE_KEY
EditionIDs GeoLite2-Country
```

### 3. Download the database

From the repository root:

```sh
geoipupdate -f path/to/GeoIP.conf -d geolite_database
```

This writes `geolite_database/GeoLite2-Country.mmdb`. Run the same command again whenever you want a fresh copy. To keep it updated automatically, schedule it with Task Scheduler on Windows or cron on macOS/Linux, for example once a day.

### 4. Run the application

```sh
# macOS / Linux / Git Bash
DB_PATH=geolite_database/GeoLite2-Country.mmdb go run .
```

```powershell
# PowerShell
$env:DB_PATH = "geolite_database/GeoLite2-Country.mmdb"; go run .
```

---

## Running in Docker

In Docker, MaxMind's official image `ghcr.io/maxmind/geoipupdate` runs as its own container. It writes into a volume that the application container also mounts.

### 1. Build an application image

The repository does not include a Dockerfile yet. A minimal one:

```dockerfile
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app .

FROM gcr.io/distroless/static
COPY --from=build /app /app
ENTRYPOINT ["/app"]
```

### 2. Run with Docker Compose

Put your credentials in a `.env` file next to `compose.yaml`, and don't commit it:

```
MAXMIND_ACCOUNT_ID=YOUR_ACCOUNT_ID
MAXMIND_LICENSE_KEY=YOUR_LICENSE_KEY
```

`compose.yaml`:

```yaml
services:
  geoipupdate:
    image: ghcr.io/maxmind/geoipupdate:latest
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
    build: .
    environment:
      DB_PATH: /usr/share/GeoIP/GeoLite2-Country.mmdb
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

The `depends_on` condition holds the application back until the first download has finished, so it doesn't exit at startup because the file is missing. After that, geoipupdate refreshes the file every 24 hours and the application picks up each new version within a minute.

---

## Running in Kubernetes

The recommended setup runs geoipupdate as a **sidecar** in each application pod. The two containers share an `emptyDir` volume, so no shared storage is needed.

### 1. Create a Secret for the credentials

```sh
kubectl create secret generic maxmind-credentials \
  --from-literal=account-id=YOUR_ACCOUNT_ID \
  --from-literal=license-key=YOUR_LICENSE_KEY
```

### 2. Deploy the application with the geoipupdate sidecar

This uses a native sidecar (an init container with `restartPolicy: Always`), which requires Kubernetes 1.29 or later. Kubernetes starts the sidecar first and waits for its `startupProbe` to pass, meaning the first download has finished. Only then does it start the application container. The sidecar keeps running for the life of the pod and refreshes the database every 24 hours.

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: geoip-api
spec:
  replicas: 2
  selector:
    matchLabels:
      app: geoip-api
  template:
    metadata:
      labels:
        app: geoip-api
    spec:
      initContainers:
        - name: geoipupdate
          image: ghcr.io/maxmind/geoipupdate:latest
          restartPolicy: Always          # native sidecar
          env:
            - name: GEOIPUPDATE_ACCOUNT_ID
              valueFrom:
                secretKeyRef: { name: maxmind-credentials, key: account-id }
            - name: GEOIPUPDATE_LICENSE_KEY
              valueFrom:
                secretKeyRef: { name: maxmind-credentials, key: license-key }
            - name: GEOIPUPDATE_EDITION_IDS
              value: GeoLite2-Country
            - name: GEOIPUPDATE_FREQUENCY
              value: "24"
          startupProbe:
            exec:
              command: ["test", "-f", "/usr/share/GeoIP/GeoLite2-Country.mmdb"]
            periodSeconds: 5
            failureThreshold: 60
          volumeMounts:
            - name: geoip
              mountPath: /usr/share/GeoIP
      containers:
        - name: app
          image: YOUR_REGISTRY/geoip-api:TAG
          env:
            - name: DB_PATH
              value: /usr/share/GeoIP/GeoLite2-Country.mmdb
          volumeMounts:
            - name: geoip
              mountPath: /usr/share/GeoIP
              readOnly: true
      volumes:
        - name: geoip
          emptyDir: {}
```

**On clusters older than 1.29**, use two containers instead:

- **Init container:** a plain geoipupdate container without `GEOIPUPDATE_FREQUENCY`. It downloads once and exits before the application starts.
- **Sidecar:** a second geoipupdate container under `containers` with `GEOIPUPDATE_FREQUENCY` set, to keep the file updated.

This downloads the database twice each time a pod starts.

### Scaling considerations

Each pod runs its own geoipupdate, so every replica counts against MaxMind's daily download limit. Pods also download again whenever they restart. If you run many replicas, switch to a single updater:

- A Kubernetes **CronJob** runs geoipupdate on a schedule and writes to a **ReadWriteMany** PersistentVolumeClaim, backed by network storage such as NFS, EFS or Azure Files.
- Application pods mount the same claim read-only and point `DB_PATH` at it.

This results in one download per schedule, regardless of replica count. The application is safe on network storage because it reads the file into memory instead of memory-mapping it.

---

## Troubleshooting

| Symptom | Likely cause |
|---|---|
| `Error loading database: ... no such file or directory` on startup | geoipupdate hasn't run yet, or `DB_PATH` doesn't match its output directory |
| geoipupdate reports `401` / invalid license key | Wrong account ID or license key, or the key was revoked |
| geoipupdate reports a download limit error | Too many updaters or restarts on one account; see [Scaling considerations](#scaling-considerations) |
| `Error reloading database: ...` in the logs | A transient read failure during replacement. The application keeps serving the previous copy and retries on the next check |
