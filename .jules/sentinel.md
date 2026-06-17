## 2024-06-17 - Error Message Leakage Fix
**Vulnerability:** Raw `err.Error()` output was being returned in HTTP JSON responses, which could potentially expose sensitive information like stack traces or unhandled error context to users.
**Learning:** Returning `err.Error()` directly in user-facing JSON can lead to information disclosure vulnerabilities.
**Prevention:** Instead of exposing underlying errors directly, catch them, log them appropriately on the backend, and present a sanitized, generic error message to users using standardized application errors or translations.
