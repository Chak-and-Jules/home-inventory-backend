1. Add security headers to the CORS middleware or create a new Security headers middleware.
   - Headers to add: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Strict-Transport-Security: max-age=31536000; includeSubDomains`.
2. Add the middleware to `routes.go`.
3. Update or write a test for the security headers.
4. Complete pre-commit steps.
5. Submit the PR.
