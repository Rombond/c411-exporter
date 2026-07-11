---
name: fix_auth_401
description: Fix 401 Unauthorized by using browser-like headers with CSRF token from site visit
type: feedback
---

Rule: Fix "401 Unauthorized Forbidden" error by using browser-like headers (User-Agent, Referer) and extracting CSRF token from the /login page.

**Why:** C411 API requires browser-like behavior - must fetch login page first to extract CSRF token, then include proper headers in auth requests. Without this, requests get blocked with 401 Forbidden.

**How to apply:** 
1. Fetch https://<base>/login with browser headers
2. Parse HTML for `<meta name="csrf-token" content="...">`
3. POST to /api/auth/login with CSRF token header + User-Agent/Referer
4. Store cookies from response