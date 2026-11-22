# c411_exporter

Prometheus exporter for C411: exposes your account stats (transfer, ratio, level, zen, class quests...)
from the official API `GET /api/user/profile`.

## Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `C411_API_KEY` | API key with the **`profile:read`** scope (required) | |
| `C411_API_BASE_URL` | Site host | `c411.org` |
| `PORT` | Listen port | `9090` |
| `METRICS_PATH` | Metrics path | `/metrics` |
| `SCRAPE_INTERVAL` | Refresh rate (Go duration, min `30s`) | `5m` |

Create the key on c411 and tick the `profile:read` scope: it is not granted by default and the
scope of an existing key cannot be changed. Use a dedicated key for the exporter: the endpoint
allows 30 req/min per key, shared with the 120 req/min Torznab budget of the same key.
Data is refreshed at most every 30 s, so polling faster is useless.

## Metrics

| Metric | Description |
|--------|-------------|
| `c411_uploaded_bytes{kind="actual\|credit\|total"}` | Uploaded bytes |
| `c411_downloaded_bytes{kind="actual\|credit\|total"}` | Downloaded bytes |
| `c411_total_uploaded_bytes`, `c411_total_downloaded_bytes` | v1 names, = `kind="total"` |
| `c411_ratio` | total uploaded / total downloaded |
| `c411_level`, `c411_xp`, `c411_xp_into_level`, `c411_xp_to_next_level` | Level and XP |
| `c411_zen_balance`, `c411_zen_earned_total`, `c411_zen_spent_total` | Zen |
| `c411_class_quests{state="completed\|total"}`, `c411_class_stars`, `c411_class_grade_level`, `c411_class_tree_bonus_basis_points` | Class progress |
| `c411_voie_quests{voie,state}`, `c411_voie_star{voie}` | Per-voie progress |
| `c411_download_allowed`, `c411_freeleech_active`, `c411_silverleech_active`, `c411_leech_bonuses` | Download state |
| `c411_user_info{username,class,grade_tier,team}` | Always 1 |
| `c411_up`, `c411_last_success_timestamp_seconds`, `c411_scrape_failures_total` | Exporter health |

On an API error the last values are kept and `c411_up` drops to 0.

## Run

```bash
# Prebuilt image (ghcr.io/rombond/c411-exporter:latest)
docker compose up -d

# Dev build
docker compose -f docker-compose.dev.yml up --build

# Local
go build -o c411_exporter . && C411_API_KEY=... ./c411_exporter
```

## Endpoints

- `GET /metrics` - Prometheus metrics
- `GET /health` - JSON with last success time and last error

## Prometheus Configuration

```yaml
scrape_configs:
  - job_name: 'c411_exporter'
    static_configs:
      - targets: ['localhost:9090']
```

See [CHANGELOG.md](./CHANGELOG.md) for the v1 to v2 migration.

## License

MIT License
