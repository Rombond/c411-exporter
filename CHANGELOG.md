# Changelog

## v2.0.0 - 2026-11-22

### Breaking
- Uses the official C411 API (`GET /api/user/profile`) with `C411_API_KEY` (Bearer, scope `profile:read`).
- Removed `C411_USERNAME`, `C411_PASSWORD`, `C411_PASSKEY`, the CSRF/cookie login and the passkey signer.
- `SCRAPE_INTERVAL` is now a Go duration (`90s`, `5m`, `1h30m`) with a 30 s minimum.

### Added
- Metrics: upload/download by kind (actual/credit/total), ratio, level/XP, zen, class quests, stars,
  grade, per-voie progress, download/freeleech/silverleech state, `c411_user_info`.
- Exporter health: `c411_up`, `c411_last_success_timestamp_seconds`, `c411_scrape_failures_total`.

### Changed
- `c411_total_uploaded_bytes` / `c411_total_downloaded_bytes` kept, now from `transfer.*.total`.
- Gin dropped for the standard library HTTP server. Dockerfile cleaned (port 9090, no config flag).
- `/health` reports last success time and last error instead of `authenticated`.

### Upgrade
1. On c411, create an API key with the `profile:read` scope (not ticked by default).
2. Set `C411_API_KEY` in `.env`, remove `C411_USERNAME` / `C411_PASSWORD` / `C411_PASSKEY`.
3. Pull the image and restart. You can delete the exporter's dedicated passkey on c411.

## v1.1.0 - 2026-10-06

### Added
- **Passkey login** via `C411_PASSKEY`. The exporter signs the site's WebAuthn challenge with a passkey
  exported from Bitwarden/Vaultwarden and calls `/api/auth/passkey-login`.
- `C411_PASSKEY` is passed through `docker-compose.yml` and `docker-compose.dev.yml`.

### Changed
- When `C411_PASSKEY` is set it is used instead of username/password, including for re-login after a session expires.
- Session cookies are no longer printed in the logs.

### Why
c411 now requires a Cloudflare Turnstile token on `POST /api/auth/login` (`400 TURNSTILE_REQUIRED`),
so the previous username/password login no longer works. The passkey login route does not require Turnstile.

### Upgrade
1. Export your passkey from Vaultwarden:
   ```bash
   bw get item <id> | jq -c '.login.fido2Credentials[0] | {credentialId, keyValue, rpId, userHandle, counter}' \
     | sed 's/^/C411_PASSKEY=/' >> .env
   ```
   Use a dedicated passkey for the exporter: it is a full login secret.
2. Add `C411_PASSKEY` to your compose environment (wrap the JSON in single quotes if needed).
3. Pull `ghcr.io/rombond/c411-exporter:v1.1.0` and restart.

`C411_USERNAME` / `C411_PASSWORD` still work only if Turnstile is disabled on the site.

## v1.0.0 - 2026-07-10

- Initial release: upload/download bytes via username/password login.
