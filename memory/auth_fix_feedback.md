---
name: auth_fix_feedback
description: Auth fix requires browser-like headers with CSRF token from site visit
type: feedback
---

Rule: Fix "401 Unauthorized Forbidden" by using browser-like headers with CSRF token extracted from the login page visit.

Why: The C411 API requires a browser-like session - must first fetch /login to extract CSRF token, then use it in auth requests with proper User-Agent and Referer headers.

How to apply: When fixing authentication issues, implement a two-step process: 1) GET /login with browser headers to extract CSRF token from meta tag, 2) POST /api/auth/login with CSRF token + credentials. Never make API calls without first establishing a session this way.