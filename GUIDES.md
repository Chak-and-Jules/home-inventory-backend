# Guide catalog and progress API

The introduction is the first guide in a code-owned catalog (`internal/guides/catalog.go`).
It needs neither demo provisioning nor feature flags. Clients own their native or web
walkthrough controls, translated copy and overlays. `title_key` is a client translation
key, not display text. Backend errors are localized through `TranslateDB`.

## Discovery

Authenticated clients call `GET /api/v1/guides?platform=web&client_version=1`
(or `mobile`). `client_version` is the positive integer **guide renderer capability**
version, not the app's semantic version. Increment a variant's `min_client_version`
when its client implementation requires a newer renderer. Supply optional
`X-Home-Id` to evaluate against the selected home; otherwise the saved platform
home is used. Responses contain only that platform's metadata and effective state.
Do not cache these private responses.

Active, published, client-compatible variants appear even if the user predates
their publication. Inactive, future and unsupported variants are omitted. Settings
uses this same list, including completed/skipped entries; web shows only web
tutorials and mobile shows only mobile tutorials.

`available` describes current home/role/prerequisite eligibility. Reasons are
stable machine codes for client translation. `should_offer` additionally requires
not_started/in_progress status and no unexpired deferral. A missing progress row
means not_started, version 0; discovery does not create rows or business records.
Revoked home access removes saved home context from the response and prevents
further progress actions there. Skip and restore remain possible without a home.

Evaluate on sign-in/app return and feature entry. Present one invitation at a time.
Catalog order supplies initial priority; later feature guides can declare their
entry point and prerequisites. Prerequisites are logical IDs whose current
revision must be completed on the requesting platform; skipped is not completed.
Configure entry-point guides without an unnecessary prerequisite if they should
also be available to users who skipped the introduction.

## Actions and concurrency

Send `POST /api/v1/guides/introduction/actions`, using the revision/version from
discovery. The authenticated Firebase identity selects the user; user IDs cannot
be supplied in this payload. Example first action:

```json
{
  "request_id": "a888de6a-b90d-4b3c-b1da-391284ab4e94",
  "platform": "web",
  "client_version": 1,
  "revision": 1,
  "expected_version": 0,
  "action": "start",
  "home_id": "13349edc-cc68-40e4-bfd6-869b5d5da945"
}
```

`home_id` here is a guide checkpoint context, not a stock transaction. Only start,
checkpoint and complete accept it; omitted/null uses the saved platform context.
Home access and required roles are rechecked for all three actions. Business API
requests still use their existing `X-Home-Id` contract and normal permissions.
Starting in a different home resets the checkpoint to the first step. Changing
home directly in checkpoint/complete is rejected so old steps are not attributed
to another home's inventory.

| Action | Effect |
| --- | --- |
| start | Starts at this platform's first step, or resumes its checkpoint; clears its deferral. Restore a completed/skipped guide first. |
| checkpoint | Requires in_progress and a `step_id` from this platform's variant. Can move to an existing-data-appropriate step; does not create inventory. |
| maybe_later | Requires a future `defer_until` chosen by the client. Keeps status/checkpoint and defers only this platform. Does not mark either app completed/skipped. |
| complete | Requires in_progress at the platform's last step; sets both apps completed and clears both deferrals. |
| skip | Sets both apps skipped and clears both deferrals, including an app not installed yet. |
| restore | Resets only the requesting app to not_started with no step, home or deferral. Preserves the other app's state and all business data. |

Send `step_id` only for checkpoint and `defer_until` only for maybe_later.
Unexpected fields, oversized bodies (over 4096 bytes), and missing concurrency
fields are rejected. Complete is tutorial bookkeeping: clients must advance only
after the relevant business save succeeds, adapt to existing data, and recover
saved definitions/inventory through the business APIs rather than create duplicates.

One aggregate version covers both apps, so even a local restore invalidates older
writes from either app. On `409 state_conflict`, reload the list, reconcile with
the latest state and deliberately retry with a **new** request ID/version. Never
automatically turn a stale checkpoint into a new-version command: the latest
state may represent a skip or restore from the other app.

Retry a network-uncertain command with exactly its original request ID and
payload. Successful commands have a per-user durable receipt committed atomically
with progress. Identical retries return current state without replaying the old
action, even after subsequent restoration. A reused ID with a different payload
returns `409 request_id_reused`. Failed commands have no receipt. Clients should
serialize their own writes and accept the returned version before the next one.

A platform-only restore is persisted state, not a device preference. Reading or
logging in never reapplies an older shared completed/skipped outcome over it.
A **new explicit** completion/skip subsequently sets the outcome for both apps.
No action changes another logical guide, creates a home or resets demo content.

## Publication and revision policy

Keep logical IDs and step IDs stable. Change a variant's `content_version` for
wording, translation or layout fixes; this never resets status. Increment the
logical `revision` only for deliberate workflow reintroduction. Each revision has
its own progress; an older revision command conflicts and must not be silently
reinterpreted. Shared outcomes are stored for both applications, even if the
counterpart variant is published later within that logical revision.

The code catalog is validated at startup for duplicate identities, invalid steps,
missing prerequisites and cycles. Add the demo as a distinct logical guide so users
who completed the introduction earlier can discover it without repeating onboarding.

## Storage, migration and verification

Startup's AutoMigrate adds `guide_progresses` (user/guide/revision aggregate) and
`guide_requests` (per-user idempotency receipts). No existing profile or inventory
is rewritten or backfilled. Profile deletion cascades both tables; home deletion
clears nullable checkpoint home references. Receipts are retained with the user
to prevent replay of older successful commands. Rollback can leave these additive
tables in place; older application versions ignore them.

Default tests use SQLite. For the same lifecycle/migration tests plus real
concurrent transaction cases, use an explicitly disposable PostgreSQL database:

```sh
GUIDE_TEST_POSTGRES_DSN='host=127.0.0.1 port=55443 user=postgres password=test dbname=test sslmode=disable' go test ./internal/guides -count=1
```

The role needs schema creation permission. Each fixture creates an isolated schema
and drops it at cleanup. Do not point this test at production. HTTP tests and the
router suite additionally verify platform filtering, validation, localization and
authentication protection; normal repository vet/build/test checks still apply.
