# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

# Architecture Overview

## High-Level Summary
This is a **Go-based Prometheus exporter** that scrapes user metrics from the C411 API and exposes them via the Prometheus HTTP interface. The application uses:
- Gin for HTTP routing
- Prometheus client library for metrics exposition
- OAuth2-like token-based authentication with C411 API

## Key Components

### 1. Authentication Layer (`main.go`)
- `C411Client` struct manages login cookies and HTTP sessions
- Uses browser-like headers (User-Agent, Referer) to bypass CSRF protection
- **Critical**: CSRF token is embedded in `/login` page meta tag `<meta name="csrf-token" content="...">`
- Must first fetch login page → extract CSRF token → use in auth request
- Stores cookies after successful login at `api/auth/login`
- Uses mutex for thread-safe cookie access
- Auto-authenticates on startup if credentials are provided via environment variables

### 2. Metrics Collection (`main.go`)
- `FetchMetrics()` calls `/api/auth/me` with the login cookies
- Returns `UserMetrics` struct (uploaded/downloaded bytes)
- Prometheus gauges registered to track these metrics over time

### 3. HTTP Server (`main.go`)
- Serves `/metrics` endpoint that scrapes and updates Prometheus gauges, then exposes them
- Health check at `/health` for monitoring tools
- Runs as Gin server on configurable port (default: 9090)

### 4. Configuration (via `.env`)
```
C411_API_BASE_URL     # C411 API host (e.g., c411.org)
C411_USERNAME         # API credentials (required for auth)
C411_PASSWORD         # API credentials (required for auth)
PORT                  # Server port (default: 9090)
METRICS_PATH          # Metrics path (default: /metrics)
SCRAPE_INTERVAL       # How often to refresh metrics (configured in cron, not used here)
```

## Authentication Flow (HEADLESS AUTO-LOGIN)

### Required Steps:
1. **Fetch login page**: `GET https://<base>/login` with browser headers
2. **Extract CSRF token**: Parse HTML response for `<meta name="csrf-token" content="TOKEN">`
3. **Auth request**: POST to `/api/auth/login` with:
   - CSRF token in header (or cookie if site uses different mechanism)
   - User-Agent, Referer matching browser behavior
   - JSON body: `{"username": "...", "password": "..."}`
4. **Store session cookies** from successful response for subsequent API calls

### Implementation Details:
- Use `http.Header` to set browser-like headers (User-Agent, Accept-Language, etc.)
- Parse login page HTML to extract CSRF meta tag
- The site requires this because it protects against automated login attempts
- Cookie storage uses mutex for thread safety across scrape intervals

## Data Flow
1. Startup: Load config → attempt auto-login (with CSRF extraction) → start Gin server
2. `/metrics` requests: Check auth → call C411 API → update Prometheus gauges → expose via promhttp.Handler()
3. External Prometheus server scrapes `/metrics` to collect data

# Commands

## Building and Running

```bash
# In development, rebuild after code changes
docker compose -f docker-compose.dev.yml up --build
```

## Common Tasks

### View metrics locally
```bash
curl http://localhost:9090/metrics
```

### Health check
```bash
curl http://localhost:9090/health
```

# Access logs
docker compose -f docker-compose.dev.yml logs -f

# Stop
docker compose -f docker-compose.dev.yml down
```

