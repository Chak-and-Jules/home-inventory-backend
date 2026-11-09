# Private demo houses

New users receive a real default home and a separate editable demo during profile
sync. The pair and all sample content commit in one transaction, serialized on
the existing profile row. Repeated/concurrent sync creates one pair. Existing
users with real memberships keep their homes/defaults and can explicitly opt in
through the demo API. No blanket backfill or deployment is included.

Identify demos using `Home.IsDemo`, returned by ordinary home listing. Their
names are editable. `user_demo_homes` records one private owner/home and a version;
its nullable home reference preserves deliberate removal. It has profile-deletion
cascade and home-deletion SET NULL constraints. The existing home-role model
still permits ordinary inventory, quantity, expiry, shopping and image access.
Home-route policy blocks sharing, membership changes and default selection for
demos. A demo never counts as another real home when deleting the last real home,
and deleting a real default selects another real home rather than the demo.

## API and retries

Both routes require the normal Firebase bearer token, use the authenticated
profile, and return private/no-store responses. No target user/home ID is accepted.

- `GET /api/v1/demo-home` returns `status`, `version`, nullable `home`, and
  `can_create`. Status is `not_created` (version 0), `available`, or `removed`.
  Discovery never provisions or changes state.
- `POST /api/v1/demo-home/actions` accepts exactly `request_id` (nonzero UUID),
  `expected_version` (from discovery), and `action`: `create`, `reset`, or `remove`.
  The body is limited to 4096 bytes. Successful responses have the same view shape.

`create` explicitly provisions an absent or deliberately removed demo. It returns
409 if a demo already exists. Only this explicit command can recreate a removed
demo; ordinary login, profile sync, guide replay and tutorial Restore cannot.

`reset` preserves the home ID/name, default preferences and guide state while
replacing its SQL content with fresh samples. Dependencies, receipts/OCR jobs,
recipes, maintenance, predictions, shopping and inventory records are removed only
within that house. `remove` deletes its content/home and retains the registry
tombstone/version. The legacy `DELETE /homes/{id}` route performs the same private
removal bookkeeping. Use the action endpoint for request-receipt retry semantics.
Physical object storage follows the application's existing image lifecycle; this
change does not introduce a bucket-object cleanup worker.

Explicit commands use the existing `guide_requests` per-user receipt namespace
with a separate `demo-home` payload fingerprint. State changes and receipts commit
together. Retry a network-uncertain command with its original ID and exact body.
The backend acknowledges an existing receipt and returns **current** status,
including after subsequent reset/removal/recreation, without replaying the action.
Reusing an ID with a different body or for a guide/setup command is a 409 conflict.
On stale `expected_version`, reload and reconcile before issuing a new command;
never automatically replay a destructive reset/remove against a refreshed version.
The version tracks demo lifecycle operations, not ordinary stock edits.

All destructive work verifies the registry, demo marker and owner membership.
It locks the owning profile and demo home; root deletion or FK failures roll the
transaction back. Demo-guide commands take the same profile lock first so guide
checkpoints and removal cannot deadlock while clearing retained home references.
The API cannot select another user's demo or a real house.

## Samples and automation

Each seed/reset creates two categories, five definitions, six stock batches and
their initial quantity transactions, using IDs/conversions from the unit catalog:

| Item | Example |
| --- | --- |
| Rice | Separate 1 kg and 0.5 kg batches, expiring in 90/120 days |
| Milk | 1 L expiring in two days |
| Eggs | Six pieces expiring in ten days |
| Dish soap | 100 mL; threshold 250 mL, target 1000 mL |
| Sponges | Three pieces |

Shopping generation produces a 900 mL soap shortage through normal stock logic.
Dates are relative to provisioning/reset. Initial names use the user's language
through TranslateDB; subsequent user-entered names remain data.

Demo shopping automation remains functional inside that home. Both expiry and
low-stock notification entry points suppress demos, including calls from the
daily refresh and ordinary stock mutations. Notification lookup failures are
also suppressed rather than emitting a signal for unknown home context.

## Separate house-switching guide

The catalog adds logical `demo_house`, revision 1, renderer capability version 3.
Renderer-v1/v2 clients continue receiving only their supported catalog. Future
web/mobile clients implement these variants and the `guides.demoHouse.title` key:

- Web: `switch_home`, `explore_demo`, `return_home`.
- Mobile: `select_demo`, `explore_demo`, `return_home`.

`entry_point` is `after_introduction`. The introduction must be completed or
skipped; this guide opts into `allow_skipped_prerequisites`. Its availability also
requires an owned existing demo. `demo_home_id` supplies the guide's home context,
independently of the currently selected real home. Accepting the invitation starts
the guide and teaches the existing house selector; completion happens only after
the final step. An omitted lifecycle `home_id` resolves to this user's demo;
an explicitly different home is rejected. Returning to the real home does not
change the guide's retained demo context or the saved default-home preference.

Use existing lifecycle commands: Maybe later defers only that platform; complete/
skip applies to both counterparts; Restore reopens only the requesting platform.
These actions never provision, reset or recreate a house. Missing/removed demos
report `demo_home_required`, so Restore cannot cause a broken automatic walkthrough.
The introduction reports `personal_home_required` in demo context; sample stock
cannot count toward personal onboarding. Existing introduction state is preserved.

## Migration and verification

AutoMigrate adds `homes.is_demo` with false for existing homes and creates the
private registry. The guide catalog, existing progress, request receipts and
renderer-v2 setup tables are reused. Existing real homes/stock are not rewritten.
Roll out the backend before clients that use renderer capability 3.

Run ordinary repository checks plus disposable PostgreSQL tests:

```sh
DEMO_TEST_POSTGRES_DSN='host=127.0.0.1 port=55444 user=postgres password=test dbname=test sslmode=disable' go test -race ./internal/demos
UNIT_TEST_POSTGRES_DSN='host=127.0.0.1 port=55444 user=postgres password=test dbname=test sslmode=disable' go test -race ./internal/handlers -run '^(TestDemo|TestSyncProfile|TestFirebaseProfileLifecycle)' -count=1
```

Fixtures create/drop isolated schemas and require schema creation permission.
Never use production. Default tests use SQLite; PostgreSQL checks exercise actual
concurrent first sync, lifecycle/version conflicts, duplicate commands, rollback,
dependent-content cleanup, migration preservation and cross-user isolation.
