# Changelog

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
