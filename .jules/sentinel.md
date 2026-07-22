## 2026-06-17 - Error Message Leakage Fix
**Vulnerability:** Raw `err.Error()` output was being returned in HTTP JSON responses, which could potentially expose sensitive information like stack traces or unhandled error context to users.
**Learning:** Returning `err.Error()` directly in user-facing JSON can lead to information disclosure vulnerabilities.
**Prevention:** Instead of exposing underlying errors directly, catch them, log them appropriately on the backend, and present a sanitized, generic error message to users using standardized application errors or translations.

## 2026-06-18 - Basic In-Memory Rate Limiting
**Vulnerability:** API endpoints were completely unprotected from high-volume requests, risking brute-force attacks and abuse.
**Learning:** Adding `golang.org/x/time/rate` alongside a thread-safe map effectively provides IP-based rate limiting. However, storing limiters per IP in an unbounded map creates a minor memory leak/OOM risk over long periods if not periodically cleaned up.
**Prevention:** Implement an IP-based rate limiter to protect all endpoints, but consider adding an eviction strategy (e.g., a background cleanup goroutine or utilizing an LRU cache) to handle long-running memory usage securely.

## 2026-06-19 - Missing Timeout on External HTTP Request
**Vulnerability:** External HTTP GET request (`http.Get`) to fetch Supabase JWKS inside the token parsing loop didn't specify a timeout.
**Learning:** This is a DoS risk because the default HTTP client has no timeout. If the external endpoint hangs or responds extremely slowly, it ties up a goroutine. Under load, this could exhaust available goroutines and crash the service or degrade performance.
**Prevention:** Always use a custom `http.Client` with a strict `Timeout` when making outbound HTTP requests, especially those on hot paths like authentication middleware.

## 2026-07-18 - [Security Headers]
**Vulnerability:** Missing standard HTTP security headers (HSTS, clickjacking protection, MIME-sniffing protection).
**Learning:** Gin doesn't add security headers by default; they must be explicitly added via middleware to ensure defense-in-depth on all routes.
**Prevention:** Always implement a dedicated security headers middleware early in the global router chain (e.g., using `r.Use`) for any new Gin web application.
## 2026-07-22 - [Fix SSRF via unsanitized barcode in product lookup]
 **Vulnerability:** Unsanitized path parameter injection in HTTP requests causing Server-Side Request Forgery (SSRF) and Path Traversal.
 **Learning:** When taking user input to form an outgoing HTTP request URL path segment, failing to URL-encode the input allows attackers to break out of the intended path segment (e.g., using `../`) to access unintended resources.
 **Prevention:** Always use `net/url.PathEscape` (or equivalent URL encoding functions depending on the URL component) to sanitize user-provided variables before inserting them into a URL.
