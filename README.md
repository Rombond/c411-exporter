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
docker compose -f docker-compose.dev.yml up --build
curl localhost:9090/metrics
```

## License

MIT License
