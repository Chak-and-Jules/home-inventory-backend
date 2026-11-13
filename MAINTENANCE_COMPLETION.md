# Atomic maintenance completion

The current legacy-task API implements the completion safety foundation from
[#152](https://github.com/Chak-and-Jules/home-inventory-backend/issues/152).
[MAINTENANCE_POLICY.md](MAINTENANCE_POLICY.md) remains the target policy for the
larger maintenance milestone; recurrence and lifecycle commands are separate work.

## Client contract

`POST /maintenance-tasks/{id}/complete` requires an authenticated home owner or
editor. An empty body or `{}` continues to work. Completion now returns the
localized `message` plus:

```json
{
  "task_id": "5b92ad30-abcc-48db-9864-85e30cdd65b0",
  "execution_id": "d44a84ce-955b-44bb-9009-dc64de242507",
  "completed_at": "2026-11-10T09:00:00Z",
  "version": 1,
  "already_completed": false,
  "replayed": false
}
```

Capable clients should save an identified command before sending it:

```json
{
  "request_id": "bf115f65-1fe9-4e45-bce9-a3a2872fca53",
  "expected_version": 0
}
```

Reuse the exact ID and payload after a timeout or uncertain response. Successful
replays return the recorded outcome with `replayed: true`; they never change
stock, logs, shopping quantities or completion time. IDs belong to one per-user
maintenance namespace, across tasks and homes. Reusing an ID for a different
task or expected version returns `409 request_id_reused`. Failed commands do not
reserve IDs. Receipts have no TTL; they are removed with their home or demo reset.
Current home write authorization is checked again on every acknowledgment.

`expected_version` is optional, but requires `request_id` when completing. Task
reads expose the capitalized `Version` alongside the existing model fields.
New and migrated tasks start at zero. Successful edits and initial completion
increment it. A stale expected version returns `409 state_conflict`. An exact
successful-command retry acknowledges its receipt even if that version is old.
The completion body accepts only these two fields, rejects explicit nulls and
zero UUIDs, and is limited to 4096 bytes.

Legacy repeated completion of an already completed task now returns **200**
with `already_completed: true`, rather than the previous 400. It does not create
a second execution or deduct supplies again. A historical completed task with
no execution stays historical: `execution_id` and an unknown `completed_at`
remain null; no actor or consumption is invented. An identified no-op can still
have a receipt. Distinct versioned commands with a stale revision return 409.

`PUT /maintenance-tasks/{id}` accepts optional `expected_version` in its JSON
body. `DELETE /maintenance-tasks/{id}` accepts it as a query parameter. Both
coordinate with completion. Completed tasks return
`409 completed_task_immutable`: a stale editor/deleter cannot erase a successful
completion or its evidence. Omitted dependencies still preserve the current
requirements; supplied dependencies replace them atomically. For legacy pending
work, omission of expected_version retains the current-state behavior. The
existing accepted-but-ignored `is_completed` input remains pending the explicit
compatibility/lifecycle work in #156; it cannot reopen a completed task here.

Errors from the completion service contain stable `code` and localized `error`
fields (see [openapi.json](openapi.json)). Malformed input/insufficient stock is
400; missing authentication is 401; no write access is 403; missing task/home is
404; stale state, request reuse or unsafe raw reopening is 409; storage failure
is 500. Older preliminary validation paths on edit/delete retain their existing
error-only responses. No client work items are created by this backend change;
coordinate through the existing client maintenance milestones.

## Transaction and audit boundaries

Completion takes a home `FOR KEY SHARE` lock before a task `FOR UPDATE` lock.
The home lock coordinates with reset/deletion while allowing different tasks to
proceed. Edit and delete use the same lock order. Only the task read under that
lock and the dependency set read afterward are authoritative. The earlier ID /
home lookup merely routes to the home lock. Owner/editor membership is checked
inside the transaction before task state or a recorded outcome is disclosed.

Requirements for the same definition are summed with exact base-unit arithmetic.
Definitions are visited in UUID order and stock rows locked in expiration/UUID
order. All deductions, stock transaction records, execution/consumption
snapshots, task state/version, shopping updates and successful-command receipts
commit together. A failure, including a late receipt failure or shortage of a
second supply, rolls everything back. Concurrent commands sharing a request ID
on different tasks resolve through the receipt uniqueness constraint; a losing
transaction rolls back all of its tentative effects before returning conflict.

Startup `AutoMigrate` adds:

- `maintenance_tasks.version`, non-null with default 0.
- `maintenance_executions`: one initial execution per legacy task, with actor,
  request correlation, version, UTC time and the task/dependency JSON snapshot.
- `maintenance_consumptions`: positive base quantity, definition and unit labels,
  batch and inventory transaction IDs, and expiry at execution time.
- `maintenance_requests`: successful command receipts keyed by user/request ID,
  with canonical payload fingerprint and recorded outcome.
- Nullable `inventory_transactions.source` and `maintenance_execution_id`.
  New maintenance deductions use source `maintenance`; old/non-maintenance logs
  remain null/unknown. Other stock-writing flows retain their existing behavior.

Audit task, actor, batch and definition IDs are snapshots, without destructive
foreign-key cascades from those live records. The home owns the audit lifetime.
Deleting a live inventory batch can still remove its ordinary transactions via
existing cascades, while the consumption snapshot retains their IDs and facts.
Home deletion cascades through the journal; demo reset explicitly clears only
that demo home's journal. There is no public audit mutation/history API yet.

## Remaining scope

Stock expiry eligibility is deliberately unchanged here: completion still
selects positive stock by expiration, including expired batches. #153 fixes that
selection rule and its boundary tests. This change does not generate future
occurrences, schedule recurring tasks, infer legacy usage, restore consumed
stock, send durable reminders or implement history corrections. #154–#158 build
on these execution IDs/receipts; the one-execution-per-legacy-task constraint
must evolve with their occurrence identity rather than creating a second audit
mechanism. Current low-stock notification helpers only log; durable delivery
belongs to the reminder work and must use a transactional outbox.

## Verification

`go test ./...` includes database-backed rollback, exact quantity, audit,
permission, HTTP/localization, edit/delete and demo-reset cases using SQLite.
The same service/handler cases and additional locking races run against an
isolated schema per test in a disposable PostgreSQL database:

```sh
UNIT_TEST_POSTGRES_DSN='host=127.0.0.1 port=5432 user=postgres password=... dbname=maintenance_test sslmode=disable' \
  go test -race ./internal/maintenance ./internal/handlers \
  -run 'Test(Postgres|Complet|Maintenance)' -count=1 -timeout=180s
```

The database must allow schema creation/deletion; never supply a production DSN.
PostgreSQL tests cover concurrent legacy and identified completions, distinct
commands with stale versions, cross-task request reuse, edit/delete races,
authoritative dependency reads after lock waits, shared-stock exhaustion, and
late failures. The default suite skips only the PostgreSQL-specific races when
this environment variable is absent.
