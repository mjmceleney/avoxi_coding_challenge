FROM golang:1.27.2 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app ./cmd/server

# The final image is based on geoipupdate so the container can refresh the database itself,
# mirroring the sidecar used in deploy/k8s/deployment.yaml. Credentials are supplied at runtime
# (GEOIPUPDATE_ACCOUNT_ID / GEOIPUPDATE_LICENSE_KEY), not baked into the image.
# Pinned to the same version as the Kubernetes sidecar, so builds are reproducible and both
# deployments run the same updater. Bump the two together.
FROM ghcr.io/maxmind/geoipupdate:v8.0.0
COPY --from=build /app /app
# The entrypoint fetches the database once so the app can start, then refreshes it every
# GEOIPUPDATE_FREQUENCY hours in the background. A frequency of 0 disables the refreshes,
# matching geoipupdate's own meaning of 0 ("run once").
RUN printf '%s\n' \
    '#!/bin/sh' \
    'set -e' \
    'case "$GEOIPUPDATE_FREQUENCY" in' \
    '  ""|*[!0-9]*) echo "GEOIPUPDATE_FREQUENCY must be a whole number of hours (0 disables refreshes), got \"$GEOIPUPDATE_FREQUENCY\"" >&2; exit 1 ;;' \
    'esac' \
    'geoipupdate -d "$GEOIPUPDATE_DB_DIR"' \
    'if [ "$GEOIPUPDATE_FREQUENCY" -gt 0 ]; then' \
    '  (' \
    '    while true; do' \
    '      sleep "$((GEOIPUPDATE_FREQUENCY * 3600))"' \
    '      geoipupdate -d "$GEOIPUPDATE_DB_DIR" || echo "geoipupdate failed; will retry next cycle" >&2' \
    '    done' \
    '  ) &' \
    'else' \
    '  echo "GEOIPUPDATE_FREQUENCY is 0: the database will not be refreshed until the container restarts" >&2' \
    'fi' \
    'exec /app' \
    > /entrypoint.sh && chmod +x /entrypoint.sh
ENV GEOIPUPDATE_EDITION_IDS=GeoLite2-Country \
    GEOIPUPDATE_FREQUENCY=24 \
    GEOIPUPDATE_DB_DIR=/usr/share/GeoIP \
    DB_PATH=/usr/share/GeoIP/GeoLite2-Country.mmdb
ENTRYPOINT ["/entrypoint.sh"]
