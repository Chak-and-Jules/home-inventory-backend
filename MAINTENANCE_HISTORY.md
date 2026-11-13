# Maintenance schedules, occurrences and history

Backend #154 implements persistence and read access for the model agreed in
[MAINTENANCE_POLICY.md](MAINTENANCE_POLICY.md). Completion, expiry selection and
successful-command recovery continue to use the journal from #152–#153 and
[MAINTENANCE_COMPLETION.md](MAINTENANCE_COMPLETION.md). Recurrence generation,
modern mutations, correction events and forecasts remain in #155–#158.

## Read contract

Authenticated home members, including viewers, can use these `/api/v1` routes.
Every route requires `X-Home-Id`. A resource in a different selected home returns
404 even when the caller belongs to both homes; a non-member receives 403.
Responses use `Cache-Control: private, no-store` and
`X-Maintenance-Contract: occurrences-v1`. Existing `/maintenance-tasks` routes
keep their response shape/casing and IDs.

| GET route | Result |
| --- | --- |
| `/maintenance-schedules` | Schedule defaults, review/restart context and requirement snapshots |
| `/maintenance-schedules/{id}` | One schedule and its current future defaults |
| `/maintenance-occurrences` | Pending/terminal jobs with their own due context and requirements |
| `/maintenance-occurrences/{id}` | One job; its ID never advances to the next job |
| `/maintenance-occurrences/{id}/history` | The job plus its existing executions and actual consumption snapshots |

Lists return `{ "contract": "occurrences-v1", "items": [], "next_offset": null }`.
Both lists accept `state`, `limit` (1–100, default 50) and nonnegative `offset`.
Schedules order by creation time/UUID. Occurrences order by due time/UUID, with
unknown due times last; they also accept `schedule_id`, `due_after` and
`due_before`. Due filters are inclusive RFC 3339 instants. Unknown/repeated/empty
query parameters, invalid states/ranges and malformed IDs return 400. Offset
pagination is stable for unchanged data; refetch when concurrent writes change
the list. These are bounded reads, not a guarantee of a frozen multi-page export.

History returns `occurrence`, `executions` and `execution_history_known`.
A migrated completed record without an execution has an empty execution list
and `execution_history_known: false`. Missing completion timestamps stay null.
No fictional actor, completion event or inventory link is created. Known events
retain their original IDs, actor/request IDs, version, `performed_at` (the
existing completion instant), `recorded_at` (the existing execution creation
instant) and the existing consumption/transaction references. Actual-work and
recording times are equal for today's server-timed completion; accepting a
separate client performed time belongs to the lifecycle work.

Requirements contain dependency/definition IDs, original stored base quantities,
and nullable name/base-unit facts. `snapshot_origin` distinguishes:

- `legacy_current_record`: facts available on the legacy row at migration or
  pending edit, rather than a claim about unrecorded historical work.
- `recorded_execution`: task facts from the original execution snapshot, with
  definition/unit labels from its retained consumption snapshots when available.
- `execution_only`: an execution whose task and usable task snapshot are gone;
  unknown due/requirement facts remain null/empty.

An execution lacking recorded label/unit facts never borrows a newly renamed
live definition to present those labels as historical. Raw legacy/execution
snapshot payloads are retained internally, without exposing arbitrary stored
JSON through these endpoints. Multi-query history reads use one repeatable-read
transaction so a concurrent completion cannot mix a pending job with newly
committed execution facts.

## Persistence and migration

Startup runs `AutoMigrate`, initializes the existing unit catalog, then calls
`maintenance.InitializeLegacy` before setting up routes/workers. It processes
bounded batches using per-task transactions and the existing home/task lock
order (home, legacy task, schedule, occurrence, then stock). Initialization is resumable, safe to repeat and safe under concurrent
starts/completions. It never consumes inventory or rewrites the legacy rows,
quantities, timestamps, old transactions, receipts or existing execution IDs.

New persistence consists of:

- `maintenance_schedules`: future defaults, state, interval, nullable confirmed
  mode/zone/local anchor, immutable original legacy payload and review/restart
  context. A deterministic schedule UUID and unique `legacy_task_id` provide
  stable mapping across retries.
- `maintenance_occurrences`: the original task UUID as the initial occurrence
  UUID, independent state/version, original/effective due instants, inventory
  assignment snapshot and separate requirement/task snapshots.
- Nullable `maintenance_executions.occurrence_id`: a link from the existing audit
  journal, with the same execution/request/consumption identities as before.
- Nullable `homes.maintenance_time_zone`: storage for the owner-controlled home
  zone. Legacy migration never infers a schedule zone/mode from that field.
  The configuration/review mutation API ships with #155; today's generic home
  PUT does not configure it.

| Legacy record | Canonical representation |
| --- | --- |
| Valid Once/empty frequency, pending | Active one-off schedule, same pending occurrence ID and absolute due time |
| Valid Once, completed | Archived one-off schedule and completed history |
| Valid repeating, pending | `needs_review`, one actionable legacy pending occurrence; normalized interval is context, not permission to generate |
| Valid repeating, completed | Paused history, `restart_required: true`; no pending successor |
| Fractional/invalid/oversized interval or unknown due time | Preserve original payload and values, mark `needs_review`; never truncate or activate |
| Execution retained after task deletion | Recover the same occurrence ID from its recorded snapshot, or retain only known execution facts; require review/restart |

Review reasons are stable machine values: `legacy_mode_and_zone_unconfirmed`,
`legacy_restart_required`, `legacy_interval_invalid`, `legacy_due_time_unknown`
and `legacy_task_missing`. All migrated recurrence modes, zones and local anchors
remain unconfirmed/null; an original absolute due instant is not reinterpreted
in a guessed zone. Missing legacy due instants are null in the new view while
original raw values remain available internally.

Constraints enforce valid lifecycle states/nonnegative versions, one pending job
per schedule, and one occupied logical slot per schedule/slot key. Explicit
supersession can release slot occupancy while retaining old IDs/history.
Home/due/state and schedule indexes support scoped reads. Composite home/resource
foreign keys prevent cross-home occurrence/schedule/execution links. Ordinary
schedule deletion cannot cascade-delete occurrences, and an occurrence linked
to execution history cannot be physically deleted. Authorized home/account
removal cascades all home-owned records. Demo reset clears only its own execution,
occurrence and schedule data in dependency order.

Requirements and inventory assignments use snapshot UUIDs rather than live
cascading FKs. Deleting a batch, changing a definition name or editing future
schedule defaults cannot erase/relabel a completed job. Existing completed
execution snapshots take precedence over later live labels. Current-record
snapshots without recorded execution facts remain explicitly identified as such.

## Compatibility writes and delivery order

The existing legacy create/edit/complete transaction also persists the mapped
schedule and initial occurrence. Only an advancing pending legacy revision can
refresh its pending requirements; terminal snapshots are not rebuilt by startup.
Completion freezes the actual execution facts and links the existing journal in
the same stock/shopping/request transaction. A failure writing the new snapshot
rolls back the legacy mutation too. A newer canonical revision or a confirmed
recurrence mode/zone causes legacy edits/completion/deletion to return 409
without replacing that decision. Successful old completion receipts still
acknowledge their recorded outcome.

Legacy pending DELETE removes the old task row and preserves a cancelled
occurrence with actor/time/reason `legacy_delete`, plus an archived schedule.
The old URL subsequently returns 404; the new history read retains the job.
Completed generic edit/delete remains rejected, as shipped in #152. Legacy
`is_completed` input retains its existing behavior until #156's explicit adapter.
There are no new schedule/occurrence mutation routes in this PR, no automatic
successor, and no hidden recurrence restart. The existing initial-execution
uniqueness/reopen guard remains until the lifecycle implementation evolves it;
that work must extend this journal rather than creating another one.

Rehearse the upgrade against a restored PostgreSQL database, back up production,
and drain old writer replicas before deploying the new schema/backfill. Enable
new client reads only after initialization succeeds. Do not mix older binaries
that omit canonical writes with these reads. An application rollback retains all
new tables and the original journal; disable the new reads and reconcile legacy
writes/deletions before re-enabling them on upgrade. Do not drop service history
as a rollback strategy. The final cross-feature deployment gate remains #162.

## Verification

The normal suite tests fresh schemas, logical data backfill, unknown legacy
facts, snapshot retention, constraints, compatibility writes, rollback,
permissions, validation, localization and paging. PostgreSQL additionally
rehearses the populated pre-#154 execution schema, repeated DDL/backfill, FK/home
cleanup, concurrent startup/completion and consistent history reads during a
completion. SQLite's table-rebuild migrator is not the production upgrade path.

```sh
UNIT_TEST_POSTGRES_DSN='host=127.0.0.1 port=5432 user=postgres password=... dbname=maintenance_test sslmode=disable' \
  go test -race ./internal/maintenance ./internal/handlers \
  -run 'Test(Legacy|Canonical|Postgres|Complet|Maintenance)' -count=1 -timeout=180s
```

Use only a disposable database that permits test schema creation/deletion.
Client integration stays within existing maintenance milestones; no issues
outside the backend repository are created by this change.
