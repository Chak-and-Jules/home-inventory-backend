# Unit test review

Reviewed on 2026-09-20, starting from main at `efb3bd4`.

## Current condition

- The backend uses Go 1.26.3, Gin, and GORM. Existing tests exercise handlers, middleware, routing, translations, logging, utility functions, and application setup. Database tests commonly use sqlmock; product lookup tests use local HTTP servers.
- The root module passes `go vet ./...`, `go build ./...`, and `go test ./...` after these additions. `gofmt -s -w .` was also run successfully.
- The initial coverage-instrumented run failed with package-resolution errors before producing usable coverage. It was not retried, following the request to skip blocked work. No measured before/after coverage percentage is claimed.
- `.functions/predictions` contains additional tests outside the directories traversed by `go test ./...`. An explicit test attempt failed because access to a Go build-cache file was denied; it was skipped without retrying. The existing coverage workflow also uses `./...`.
- Existing receipt-handler tests use fixed 50 ms sleeps for asynchronous processing. The rate-limit test replaces a package-global limiter without restoring it. These are test-isolation/reliability risks, not failures observed in the passing root suite.

## Added coverage

Fifteen new top-level tests, with table-driven cases, cover:

- Receipt parsing: decimal separators, quantity formats, unit prices, tax markers, non-item lines, zero and overflowing prices, empty names/input, reader errors, and oversized scanner tokens. Error assertions check that partial receipts are not returned.
- Notifications: ordinary/urgent low-stock and expiry events, with nil databases, resolved home names, missing homes, and database errors. Tests assert complete structured log fields and fallback messages, without contacting external services.
- Fuzzy matching: empty and Unicode strings, punctuation normalization, substring scores in both directions, token overlap, the inclusive match threshold, stable tie-breaking, and rejected-match confidence.
- Translations: language aliases, invalid context IDs, template precedence/fallbacks, and cached English defaults after failed or incomplete database lookups.
- Maintenance scheduling: missing, zero, negative, fractional, and invalid custom intervals, plus whitespace/case normalization and fractional truncation.

The changes add test fixtures and assertions only. They introduce no application-facing strings or API contract changes, so translation catalogs and OpenAPI do not require changes.

## Remaining limits

These additions improve exercised paths but do not establish maximum attainable coverage. A functioning coverage run is needed to prioritize the remaining handler/database branches quantitatively. No production refactoring was made merely to expose unreachable parser branches or startup/external-service paths to tests.
