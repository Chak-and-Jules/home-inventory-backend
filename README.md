# home-inventory-backend

## Running locally

- Create a new file named `.env` and add credentials into it using the template in `.env.example`
- Run `go run main.go`

## Running tests

Run `go test ./...`

## Guide catalog and user progress

See [GUIDES.md](GUIDES.md) for the authenticated catalog/action endpoints,
cross-platform completion and skip behavior, platform-only restoration,
concurrency/retry contract, publication policy and disposable PostgreSQL tests.

See [DEMO_HOMES.md](DEMO_HOMES.md) for private sample-house provisioning,
reset/removal, notification boundaries and the separate house-switching guide.

## Firebase profile identity mapping

Profiles retain their UUID primary key and can store a nullable, unique
`firebase_uid` (up to 128 characters). Startup's existing `AutoMigrate` adds the
column and unique index; existing profiles remain unlinked with `NULL` values.
The mapping is internal and excluded from profile JSON.

The default profile tests use in-memory SQLite. To exercise the same migration,
lookup, and uniqueness checks against a disposable PostgreSQL database, set
`PROFILE_TEST_POSTGRES_DSN` and run `go test ./internal/models -run '^TestProfile' -count=1`.
The test role must be allowed to create schemas; each test uses an isolated schema
and removes it afterward.

## Firebase authentication

All API routes require `Authorization: Bearer <Firebase ID token>` from the client
SDK (for example, `await user.getIdToken()`). Supabase tokens and Firebase custom
tokens are not accepted. The Admin SDK validates signature, issuer, audience,
expiry, and revocation/disabled/deleted user status. Revocation checking performs
a Firebase user lookup on each request; certificate keys are cached by the SDK.

Set `FIREBASE_PROJECT_ID=home-inventory-11`. Startup fails if it is missing.
On Cloud Run the SDK uses Application Default Credentials from the runtime
service account; do not upload a service account key or the web `firebaseConfig`.
Grant that service account `firebaseauth.users.get` (revocation checking) and
`firebaseauth.users.delete` (account deletion) in the Firebase project, preferably
through a custom role limited to those permissions. Ensure the Identity Toolkit
API (`identitytoolkit.googleapis.com`) is enabled. Keep the existing Cloud SQL
and Secret Manager permissions. Supabase auth environment variables are obsolete.

Locally, use `gcloud auth application-default login` with an identity that has
those permissions. The example database socket is specific to Cloud Run; for
local access use the Cloud SQL Auth Proxy connection settings instead.
`FIREBASE_AUTH_EMULATOR_HOST` is allowed only when `APP_ENV=local` outside Cloud Run; omit it on
Cloud Run because emulator tokens are unsigned.

On the first authenticated request, the backend creates a profile with a new
internal UUID and the verified Firebase UID. Later requests resolve by UID and
synchronize the verified token's email. Existing profiles with a NULL or different
Firebase UID are never linked by email, even if the addresses match. An email
collision fails without changing the existing account; fresh installations
should use a fresh profile rather than silently claiming old data.

Call `POST /api/v1/profiles/sync` after sign-in:

```json
{"profile":{"id":"<Firebase user.uid>","email":"<Firebase user.email>"}}
```

Both values must match the verified token. The response `ID` is the internal
profile UUID, which clients must store separately from Firebase `user.uid` and
use for database-facing identifiers such as account deletion `user_id`. Sync
also creates the initial default home when the profile has no home memberships.
Do not send `firebase_uid` in profile updates; it is server-managed.

Both deletion routes (`/api/v1/profiles/delete-account` and
`/api/v1/account/delete-request`) require a Firebase token and the internal UUID
in `user_id`. The backend deletes the Firebase identity before committing the SQL
cleanup. Firebase failures roll SQL back and return an error. These two systems
do not share an atomic transaction: a SQL commit failure after Firebase deletion
requires operator reconciliation of the remaining SQL data.

Auth tests run without Google credentials or external services and exercise the
real SDK with local signing keys and HTTPS fixtures. To test profile provisioning
and concurrent first requests against disposable PostgreSQL, set
`AUTH_TEST_POSTGRES_DSN` and run `go test ./internal/middleware -run '^TestFirebase' -count=1`.
Each fixture creates and removes an isolated schema; do not use a production DB.

## Private image storage

Set `GCS_BUCKET_NAME=home-inventory-11-images` on the backend. Startup uses ADC
for Cloud Storage and the IAM Service Account Credentials API. The Cloud Run
runtime service account is discovered from metadata and must have Storage Object
User on this bucket and `iam.serviceAccounts.signBlob` on itself. Enable
`iamcredentials.googleapis.com` and keep uniform bucket-level access and public
access prevention enabled. No bucket ACL changes or service account keys are used.

For local ADC development with storage enabled, set `GCS_SIGNER_EMAIL` to the
runtime service account's email and give your local identity `signBlob` access
on that account as well as object access on the bucket. If `GCS_BUCKET_NAME` is
omitted, the app starts with image endpoints disabled (503). A configured storage
client that cannot initialize fails startup. Existing auth/database setup is
still required.

All image requests require a Firebase bearer token and `X-Home-Id`. The API
checks current SQL home membership before touching storage. Owners/editors may
upload and delete; viewers may obtain read URLs.

| Request | Result |
|---|---|
| `POST /api/v1/images` with multipart field `file` | 201 `{"path":"<home UUID>/<image UUID>.jpg"}` |
| `POST /api/v1/images/signed-urls` with `{"paths":["<object key>"]}` | 200 `{"images":[{"path":"<object key>","url":"<signed GET URL>","expires_at":"<UTC timestamp>"}]}` |
| `DELETE /api/v1/images?path=<URL-encoded object key>` | 204, including when already missing |

Uploads must be valid JPEG or PNG files, up to 5 MiB and 20 megapixels. The
backend derives the MIME type and extension from decoded content, generates a
fresh UUID key, and prevents overwrites. At most two uploads run per process;
capacity errors return 503 with `Retry-After: 1`. Uploads go through Cloud Run,
so the browser does not need bucket CORS permissions. Images have private browser
caching for five minutes. The API never makes the bucket or objects public.

Save the returned object key in the existing item-definition `image_url` field.
Signing accepts 1–100 keys from the selected home, deduplicates them, and creates
V4 GET URLs valid for one hour. The batch fails with 404 if any object is missing;
Cloud Storage/IAM failures return 502 without exposing provider error details.
The response uses `Cache-Control: no-store`. Anyone holding a signed URL can read
its object until expiry/deletion, even if their membership changes in the meantime.
Clients must use `expires_at` for caching, scope caches to the signed-in user and
home, clear them on logout/home changes, and never save signed URLs in SQL.

Deletion removes the object only. Clear/replace SQL references before removing
an old image; deleting an item definition or home does not automatically remove
its stored objects. Soft-deleted objects follow the bucket's retention policy.
Existing Supabase objects are not copied or deleted by this change. The web
client must replace its Supabase upload/delete/signed-URL helpers before using
these endpoints, including its previous one-year signed-URL cache.

Tests exercise authorization, malformed/oversized/corrupt images, cross-home
batches, provider failures, concurrent-upload limits, and real Storage/IAM SDK
requests using local HTTP fixtures. They need no cloud credentials or live bucket.
The handler tests also support disposable PostgreSQL through
`UNIT_TEST_POSTGRES_DSN` (`go test ./internal/handlers -run '^TestImage' -count=1`).

## Quantity and unit contract

This schema is intended for a fresh PostgreSQL 13+ database. Startup creates all
application tables, seeds the immutable unit catalog idempotently, and validates
it before starting the API or background jobs. UUID defaults use PostgreSQL's
built-in `gen_random_uuid()`. Existing stock data is not converted.

`SizeUnit` contains `Family`, `IsBase`, and `FactorToBase`. Every family has exactly
one base unit: `g` for mass and `mL` for volume. Metric conversions initially cover
`kg`/`g` and `L`/`mL`. The standalone `piece` unit belongs to the count family.
Other standalone units must have independent families and a base unit; a bottle
or bag is never implicitly converted to mass or volume.

Inventory quantities, transaction deltas, definition thresholds/targets, linked
shopping quantities, recipe/task requirements, and prediction amounts use base
units. Prices, servings, recurrence intervals, unconfirmed OCR quantities, and
unlinked shopping counts retain their own meanings.

Quantity writes require an explicit `quantity_unit_id` when linked to a definition.
This identifies the submitted amount independently from the saved display choice.
For definition thresholds and targets, one `quantity_unit_id` applies to both
submitted amounts. Each ingredient, dependency, or receipt line has its own input
unit. Receipt `size_unit_id` specifies a newly created definition's default unit.

For example, `POST /api/v1/inventory` with `quantity: 3` and the kg unit identifier
saves `Quantity: 3000` grams. Setting remaining stock to `800` with the g identifier
saves `800` and records `QuantityChange: -2200`. Barcode `change` is a signed
adjustment, whereas inventory quantity updates are absolute remaining amounts.
Prediction `applied_amount` is an amount to deduct. Clamped deductions record the
actual applied change.

Quantities are calculated using exact decimal rationals. JSON decimal text is
validated before conversion to the existing float64 JSON/ORM boundary. Normalized
stored amounts support six fractional base-unit digits and absolute values up to
1,000,000,000. Thus `0.000000001 kg` is valid (`0.000001 g`), while excess precision
is rejected rather than silently rounded. Presentation rounding does not change
stock. Stock and planning arithmetic uses decimal addition/subtraction.

`ItemDefinition.SizeUnitID` is the default display unit and may change only within
the same family. Such a change does not rescale any stored quantities. An inventory
entry's nullable `DisplaySizeUnitID` overrides that default. Explicit overrides
remain explicit even if equal to the current definition default.

Use `PATCH /api/v1/inventory/{id}` for partial updates:

```json
{"display_size_unit_id": "<unit-id-from-GET-size-units>"}
```

An omitted override preserves it; `null` restores inheritance. Omitted quantity
and expiry fields are preserved, and `expiry_date: null` clears expiry. A submitted
quantity requires `quantity_unit_id`; `quantity: null` is invalid. Display-only
edits do not generate transactions or stock-driven effects. The existing PUT
endpoint retains replacement behavior for expiry: omission clears it. The quick
quantity endpoint changes only quantity.

Read responses identify base quantities with `base_size_unit_id`, directly or on
their associated definition. Inventory also returns `DisplaySizeUnitID` and the
derived `effective_display_size_unit_id`. Unit metadata supplies `FactorToBase`,
so clients calculate `display quantity = base quantity / FactorToBase` without
persisting another quantity. Do not label a raw base quantity with the definition's
default unit. These API changes require coordinated web/mobile updates; the full
request and response schemas are in `openapi.json`.

The default test suite runs the unit lifecycle and other stock-writing workflows
against in-memory SQLite. To verify the same cases against a disposable PostgreSQL
database, set `UNIT_TEST_POSTGRES_DSN` and run:

```sh
UNIT_TEST_POSTGRES_DSN='host=127.0.0.1 port=55432 user=unit_test dbname=unit_test sslmode=disable' go test ./internal/handlers -run '^TestUnit' -count=1
```

The database must be disposable and the test role must be allowed to create schemas
and functions. Each fixture creates and removes its own isolated schema.

## Status

### Code Coverage

![Codecov](https://codecov.io/github/Chak-and-Jules/home-inventory-backend/graphs/icicle.svg?token=UTALTUNJ65)
