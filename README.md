# c411_exporter

Prometheus exporter for C411: exposes uploaded/downloaded bytes of your account.

## Metrics

- `c411_total_uploaded_bytes`
- `c411_total_downloaded_bytes`

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `C411_PASSKEY` | Passkey JSON exported from Bitwarden/Vaultwarden (**recommended**) | |
| `C411_USERNAME` / `C411_PASSWORD` | Password login, only works if Cloudflare Turnstile is off | |
| `C411_API_BASE_URL` | Site host | `c411.org` |
| `PORT` | Listen port | `9090` |
| `SCRAPE_INTERVAL` | Refresh rate | `5m` |

C411 login now requires a Cloudflare Turnstile token, so the exporter signs in with a passkey.
Get it with the Bitwarden CLI (works with Vaultwarden):

```bash
bw get item <id> | jq -c '.login.fido2Credentials[0] | {credentialId, keyValue, rpId, userHandle, counter}' \
  | sed 's/^/C411_PASSKEY=/' >> .env
```

Use a dedicated passkey for the exporter: it is a full login secret.

## Run

```bash
# Prebuilt image (ghcr.io/rombond/c411-exporter:latest)
docker compose up -d

# Dev build
docker compose -f docker-compose.dev.yml up --build

# Local
go build -o c411_exporter . && ./c411_exporter
```

## Endpoints

- `GET /metrics` - Prometheus metrics
- `GET /health` - Health check

```text
# TYPE c411_total_uploaded_bytes gauge
c411_total_uploaded_bytes 5.58255848392e+11
# TYPE c411_total_downloaded_bytes gauge
c411_total_downloaded_bytes 1.073741824e+09
```

## Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'c411_exporter'
    static_configs:
      - targets: ['localhost:9090']
    metrics_path: /metrics
```

## Authentication Flow

1. With `C411_PASSKEY`: `GET /login` (CSRF), `POST /api/auth/passkey-login-options` for the challenge, sign it locally (ES256), `POST /api/auth/passkey-login`
2. Without it: `POST /api/auth/login` with username/password (fails with `400 TURNSTILE_REQUIRED` while Turnstile is on)
3. Keep the session cookies; on expiry the exporter logs in again with the same method

See [CHANGELOG.md](./CHANGELOG.md) for the passkey migration.

## License

MIT License
