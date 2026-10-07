FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# The final image is based on geoipupdate so the container can refresh the database itself,
# mirroring the sidecar used in deploy/k8s/deployment.yaml. Credentials are supplied at runtime
# (GEOIPUPDATE_ACCOUNT_ID / GEOIPUPDATE_LICENSE_KEY), not baked into the image.
FROM ghcr.io/maxmind/geoipupdate:latest
COPY --from=build /app /app
RUN printf '%s\n' \
    '#!/bin/sh' \
    'set -e' \
    '# Fetch the database once so the app can start, then keep refreshing it in the background.' \
    'geoipupdate -d "$GEOIPUPDATE_DB_DIR"' \
    '(' \
    '  while true; do' \
    '    sleep "$((GEOIPUPDATE_FREQUENCY * 3600))"' \
    '    geoipupdate -d "$GEOIPUPDATE_DB_DIR" || echo "geoipupdate failed; will retry next cycle" >&2' \
    '  done' \
    ') &' \
    'exec /app' \
    > /entrypoint.sh && chmod +x /entrypoint.sh
ENV GEOIPUPDATE_EDITION_IDS=GeoLite2-Country \
    GEOIPUPDATE_FREQUENCY=24 \
    GEOIPUPDATE_DB_DIR=/usr/share/GeoIP \
    DB_PATH=/usr/share/GeoIP/GeoLite2-Country.mmdb
ENTRYPOINT ["/entrypoint.sh"]
