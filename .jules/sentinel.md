## 2026-06-17 - Error Message Leakage Fix
**Vulnerability:** Raw `err.Error()` output was being returned in HTTP JSON responses, which could potentially expose sensitive information like stack traces or unhandled error context to users.
**Learning:** Returning `err.Error()` directly in user-facing JSON can lead to information disclosure vulnerabilities.
**Prevention:** Instead of exposing underlying errors directly, catch them, log them appropriately on the backend, and present a sanitized, generic error message to users using standardized application errors or translations.

## 2026-06-18 - Basic In-Memory Rate Limiting
**Vulnerability:** API endpoints were completely unprotected from high-volume requests, risking brute-force attacks and abuse.
**Learning:** Adding `golang.org/x/time/rate` alongside a thread-safe map effectively provides IP-based rate limiting. However, storing limiters per IP in an unbounded map creates a minor memory leak/OOM risk over long periods if not periodically cleaned up.
**Prevention:** Implement an IP-based rate limiter to protect all endpoints, but consider adding an eviction strategy (e.g., a background cleanup goroutine or utilizing an LRU cache) to handle long-running memory usage securely.
