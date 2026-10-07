# avoxi_coding_challenge

An HTTP service that looks up the country of an IP address (using the MaxMind GeoLite2-Country database) and reports whether it is in a list of allowed countries.

## Build the Docker image

The image downloads the GeoLite2-Country database at build time, so you need a free [MaxMind account](https://www.maxmind.com/en/geolite2/signup) with an account ID and license key.

PowerShell:

```powershell
docker build `
  -f deploy/Dockerfile `
  --build-arg MAXMIND_ACCOUNT_ID=<your-account-id> `
  --build-arg MAXMIND_LICENSE_KEY=<your-license-key> `
  -t avoxi-coding-challenge .
```

bash:

```bash
docker build \
  -f deploy/Dockerfile \
  --build-arg MAXMIND_ACCOUNT_ID=<your-account-id> \
  --build-arg MAXMIND_LICENSE_KEY=<your-license-key> \
  -t avoxi-coding-challenge .
```

## Run the container

The service listens on port 8080 inside the container. Publish it with `-p` so it is reachable from your host:

```bash
docker run -d --rm -p 8080:8080 --name avoxi avoxi-coding-challenge
```

Check that it started, and stop it when done:

```bash
docker logs avoxi
docker stop avoxi
```

Configuration (optional, via `-e`):

| Variable  | Default                                      | Description                    |
|-----------|----------------------------------------------|--------------------------------|
| `ADDR`    | `:8080`                                      | Address the server listens on  |
| `DB_PATH` | `data/GeoLite2-Country.mmdb`    | Path to the GeoLite2 database  |

If you change `ADDR`, change the container side of `-p` to match.

## API

Health check:

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

Check an IP against a list of allowed countries (ISO country codes):

```bash
curl -X POST http://localhost:8080/v1/check \
  -H "Content-Type: application/json" \
  -d '{"ip": "8.8.8.8", "allowed_countries": ["US", "GB"]}'
# {"country":"US","allowed":true}
```

On Windows PowerShell, use `curl.exe` (plain `curl` is an alias for `Invoke-WebRequest`) and escape the JSON quotes, or use `Invoke-RestMethod`.
