
## 2024-06-16 - [Cache string allocations in hot middleware paths]
**Learning:** `jwtSecretBytes = []byte(jwtSecret)` in `FetchAndVerifyToken` caused an allocation for every single authenticated request on the backend. This is an anti-pattern as secrets initialized from environment variables rarely change at runtime.
**Action:** Always cache derived formats (like `[]byte`) of configuration/environment variables at app initialization time, instead of casting them per request in hot paths.
