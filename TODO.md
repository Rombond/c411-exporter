# TODO

## Bugs

- [ ] **2. Remove CLI flag from Dockerfile** — `CMD ["./c411_exporter", "-config=config.json"]` passes a flag that doesn't exist
- [ ] **4. Fix ticker leak** — `startMetricsRefresher` creates a `time.NewTicker` that is never stopped; add `defer ticker.Stop()` or use a `context.Context`
- [ ] **8. Fix race condition** — `scrapeAndRefresh` goroutine and `FetchMetrics` share `c.cookies` via the HTTP client's cookie jar and manual cookie attachment without proper synchronization

## Code Quality

- [ ] **3. Remove unused fields** — `server.lastMetrics` and `server.mu` are only written to, never read; remove them
- [ ] **5. Guard `/metrics` endpoint** — Return `503` if not authenticated, or use a separate health-only gauge that's always available so Prometheus doesn't alert on startup
- [ ] **6. Remove custom `parseDuration`** — Replace with `time.ParseDuration("5m")` which handles the same formats natively
- [ ] **10. Dynamic HTTP timeout** — Use `ScrapeInterval - 1s` instead of hardcoded 10s so the client never times out mid-scrape
- [ ] **11. Fix indentation** — `main()` has a stray tab before `// Start background metrics refresher`

## Config / Consistency

- [ ] **7. Use 9090 everywhere** — Dockerfile `EXPOSE 8080` is wrong; should be `9090` to match default config

## Nice-to-have

- [ ] **9. Validate CSRF token before login** — Check that `c.csrfToken` is non-empty after `FirstRequest()` before sending the login request
- [ ] **12. Extra Prometheus metrics** — Add `build_info` (const gauge), `last_scrape_success`, `last_scrape_timestamp_seconds`
