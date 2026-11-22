# CLAUDE.md

Go Prometheus exporter for C411 (single file `main.go`, stdlib `net/http` + prometheus client).

## How it works
- Background loop calls `GET https://<C411_API_BASE_URL>/api/user/profile` with
  `Authorization: Bearer $C411_API_KEY` every `SCRAPE_INTERVAL` (default 5m, min 30s), parses the
  `Profile` struct and sets the gauges. `/metrics` only serves the registry.
- Errors: 401 bad key, 403 missing `profile:read` scope, 429 rate limit (30/min/key on this endpoint,
  shared with the 120/min Torznab budget). On error the last values stay and `c411_up` = 0.
- `c411_total_uploaded_bytes` / `c411_total_downloaded_bytes` are kept from v1 (= `transfer.*.total`).

## Commands
```bash
docker compose -f docker-compose.dev.yml up --build   # dev (reads .env)
curl localhost:9090/metrics
curl localhost:9090/health
```

## History
v1 scraped the website's internal `/api/auth/me` with a session obtained by password login, then
(v1.1.0) by a software-signed passkey because of Cloudflare Turnstile. v2.0.0 replaced all that with
the official API key.
