## 2025-03-05 - Basic In-Memory Rate Limiting
**Vulnerability:** API endpoints were completely unprotected from high-volume requests, risking brute-force attacks and abuse.
**Learning:** Adding `golang.org/x/time/rate` alongside a thread-safe map effectively provides IP-based rate limiting. However, storing limiters per IP in an unbounded map creates a minor memory leak/OOM risk over long periods if not periodically cleaned up.
**Prevention:** Implement an IP-based rate limiter to protect all endpoints, but consider adding an eviction strategy (e.g., a background cleanup goroutine or utilizing an LRU cache) to handle long-running memory usage securely.
