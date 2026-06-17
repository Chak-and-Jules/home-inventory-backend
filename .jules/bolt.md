
## 2026-06-16 - [Cache string allocations in hot middleware paths]
**Learning:** `jwtSecretBytes = []byte(jwtSecret)` in `FetchAndVerifyToken` caused an allocation for every single authenticated request on the backend. This is an anti-pattern as secrets initialized from environment variables rarely change at runtime.
**Action:** Always cache derived formats (like `[]byte`) of configuration/environment variables at app initialization time, instead of casting them per request in hot paths.
## 2026-06-17 - Avoid strings.EqualFold for Enum-like Strings in Hot Paths
**Learning:** In Go, `strings.EqualFold` is surprisingly slow compared to exact string matching because it must handle complex Unicode casing rules. For enum-like values that generally come from specific sources (like database values for language preferences "Türkçe", "Turkish", "tr"), using a fast path with explicit string equality checks (`==`) is orders of magnitude faster.
**Action:** When performing case-insensitive checks against a known small set of strings on a hot path, explicitly list the common casing variations using `==` rather than relying on `strings.EqualFold` to improve CPU performance.
