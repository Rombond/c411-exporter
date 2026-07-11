---
name: c411_headless_auth
description: Headless C411 auth using CSRF token from login page HTML
type: user
---

## Goal: Auto-login without human/browser interaction

## Problem: C411.org requires CSRF protection

- **Error**: `403 Forbidden - use browser-like headers with csrf-token from site visit`
- **Cause**: c411.org embeds a CSRF token in the `/login` page HTML (typically in `<meta name="csrf-token" content="...">`)
- The API login endpoint requires this token to be included

## Solution: Fetch login page first, extract CSRF, then auth

### Steps:
1. **GET /login** with browser-like headers (User-Agent, Accept-Language)
2. Parse HTML response for CSRF meta tag (usually `<meta name="csrf-token" content="...">` or similar in head)
3. Extract token value
4. **POST /api/auth/login** including:
   - CSRF token in header: `X-CSRF-Token: <token>` (or similar)
   - User-Agent: Mozilla/5.0 Chrome
   - Origin: https://c411.org
   - Referer: https://c411.org/login

### Code pattern needed:
```go
// Step 1: Fetch login page to get CSRF token
resp, _ := client.Get(cfg.BaseURL + "/login")
body, _ := io.ReadAll(resp.Body)
html := string(body)

// Step 2: Extract CSRF from meta tag
re := regexp.MustCompile(`(?i)csrf-token["\s]*=["\s]*([^\s"'/>]+)`_)
tokens := re.FindStringSubmatch(html)
if len(tokens) >= 2 {
    csrfToken := tokens[1]
}

// Step 3: Include in login request
req.Header.Set("X-CSRF-Token", csrfToken)
```

## Current State (Mar 9, 2025)

- Go code compiles but auto-login still fails with 403
- Need to implement HTML parsing for CSRF token extraction
- Once implemented, dockerized exporter can run headlessly with just credentials

## Files Modified

- `main.go`: c411 client with cookie jar
- `Dockerfile`: Alpine-based with Prometheus exporter
- `docker-compose.dev.yml`: Dev compose config
