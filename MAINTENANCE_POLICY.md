# Maintenance scheduling and lifecycle policy

Policy version 1, for [backend #151](https://github.com/Chak-and-Jules/home-inventory-backend/issues/151)
and [maintenance milestone #2](https://github.com/Chak-and-Jules/home-inventory-backend/milestone/2).

This document defines the intended behavior for the remaining maintenance work.
It does not make that behavior available in the current API. The deployed wire
contract remains [openapi.json](openapi.json) until the implementation issues
update it. The source baseline reviewed for this decision is
[`41cf1e9`](https://github.com/Chak-and-Jules/home-inventory-backend/tree/41cf1e9be9ba714740e0c498a57d71be54e01ecc).

## Confirmed product decisions

The product owner confirmed these choices on 2026-11-10. The remaining sections
spell out their detailed implementation contract for review through #151.

| Decision | Selected behavior |
| --- | --- |
| Scheduling modes | Support fixed calendar schedules and intervals after completion. Default new repeating schedules to after-completion mode. |
| Missed work | Keep one overdue occurrence; do not create a pending job for every missed interval. |
| Reopening/correction | Retain recorded supply usage. Stock changes require a separate explicit, audited correction. |
| Time zone | Use the home's time zone and save it with each schedule. Device/user display zones do not change recurrence. |
| Completed legacy repeating tasks | Preserve completed history and require an explicit restart. |
| Custom intervals | Accept positive whole numbers of days, weeks, months or years. Reject fractions. |

Warranty tracking (#133) and configurable modules (#145) remain separate work.
Notification channel, provider and recipient policy remain decisions for #159;
they do not block this scheduling/lifecycle policy.

## Schedules, occurrences and history

A schedule defines recurrence, its local-time anchor, its saved IANA time-zone
identifier and future requirements. An occurrence is one identifiable job with
its own due time, requirement snapshot, version and lifecycle state. An execution
records actual work, who recorded it, actual completion time, recording time and
the associated consumption/correction records.

Schedule states are active, paused, needs-review or archived. An active schedule
has at most one unresolved occurrence. Occurrence states are pending, completed,
skipped and cancelled. Overdue and snoozed are derived conditions on a pending
occurrence, not replacement completion states. A paused/needs-review schedule can
retain an existing pending job but cannot generate another job automatically.

Never reuse an occurrence ID for the next job. Retain immutable execution and
correction events; changing a schedule must not rewrite completed work or its
requirement/quantity snapshots. Historical actors or usage links that were never
recorded remain unknown. Live stock rows may disappear without erasing the
historical definition, unit, batch, expiry and quantity snapshots needed to
explain maintenance consumption.

Forecasts may project later occurrences through the canonical recurrence engine,
but projections are estimates, not extra pending jobs or recorded work.
Completion-based projections assume work happens on its current due date and
must be identified as provisional. Forecasts and reminders use the same due-time
and lifecycle rules as execution.

## Time and interval rules

Store absolute event/due instants in UTC and retain the schedule's local anchor
and IANA zone for recurrence calculations. Store actual `performed_at` separately
from server `recorded_at`. A client may display either in its own zone; that must
not change the saved schedule.

The home owns the maintenance time-zone setting. Home owners configure it;
owners/editors can manage schedules using that setting. New schedules require a
configured, valid IANA zone; missing configuration produces an actionable error,
not a silent server/device-zone default. Changing the home's zone affects new
schedules. Existing schedules retain their saved zone until an explicit,
versioned schedule edit adopts the changed home setting and previews its effect.
Terminal history and its original instants are unchanged.

Supported intervals are mathematically integral positive JSON numbers in day,
week, month or year units. Thus `2` and `2.0` represent the same interval; `0`,
negative values, `0.5`, `1.9`, unknown units and values that overflow supported
date arithmetic are rejected. Never truncate, clamp or quietly convert an
invalid repeating interval into Once. Dates must remain representable by the
API's RFC 3339 timestamps. Once has no repeat interval.

The existing frequency labels map to these rules: Daily = 1 day, Weekly = 1 week,
Monthly = 1 month, Every 3 Months = 3 months, Every 6 Months = 6 months and Yearly =
1 year. Existing empty frequency means Once. Label/unit case and surrounding
spaces may be normalized without changing their meaning.

Day/week additions preserve local wall time rather than assuming 24-hour UTC
days. Month/year additions clamp an unavailable day to the last day of that
target month. Calendar mode retains its original nominal anchor day, so a
January-31 monthly schedule uses February's last day and then March 31; it does
not drift permanently to the 28th. Completion mode uses each actual completion
as a new anchor, so a job completed on February 28 next falls on March 28.

For a generated local time in a daylight-saving gap, move it forward by the gap's
size. For a repeated local time in a fall-back fold, choose the earlier UTC
instant and generate one occurrence. These adjustments do not change a calendar
schedule's nominal wall-time anchor for later dates. Explicit event timestamps
must identify an instant with an offset; never guess an ambiguous local input.

## Due-date advancement and overdue work

In calendar mode, keep the original interval grid. After completing the current
occurrence, choose the first grid date strictly after both that occurrence's
effective due date and its actual completion time. Early completion cannot
recreate the same calendar slot; late completion cannot move the calendar anchor.

In after-completion mode, the next due time is one interval after actual
`performed_at`, interpreted in the saved schedule zone. Recording a completion
late does not substitute recording time for actual work time. A backdated
completion is allowed when it does not contradict a later recorded execution;
if its next due time is already past, retain one overdue job.

Reject future actual-completion timestamps and conflicting chronology with a
recoverable validation/conflict response. Correcting old work does not silently
move or invalidate later completed work. Such conflicts require the explicit
history-correction/reconciliation flow, not a second hidden completion.

Passing several interval boundaries leaves the existing job overdue. Do not
fabricate completed/skipped jobs for elapsed slots or deduct their supplies.
Calendar completion after a long delay advances to the next appropriate future
grid slot; a backdated completion may legitimately leave its successor overdue.
Record the gap as scheduling context, not as work someone supposedly performed.

A calendar skip advances past the skipped due date and the time of the skip to
the next grid date. An after-completion skip schedules one interval after the
later of its effective due date and the skip action time. This is an explicit
administrative deferral, not actual work; it must not change the last actual
completion or consume supplies. An early skip therefore cannot recreate the
same upcoming job.

## Occurrence transition matrix

All successful commands create an audit event and return current canonical
state. Actions not allowed below return a clear conflict without partial writes.

| Command | Allowed state | Result | Supplies | Future work/reminders |
| --- | --- | --- | --- | --- |
| Complete | Pending | Completed; append an execution | Consume eligible quantities once, accounting for retained usage after reopening | Resolve current reminders and generate one successor if the schedule is active |
| Reschedule this occurrence | Pending | Pending with a new effective due time | Unchanged | Recalculate readiness/reminders; retain the calendar anchor |
| Snooze | Pending | Pending with a future reminder-suppression time | Unchanged | Suppress reminders until then; due time, overdue status and supply planning stay unchanged |
| Skip | Pending | Skipped with actor/time/reason | Unchanged | Advance under the skip rules; cancel current reminders |
| Reopen | Completed | Pending; append a reopening event | Retain recorded net usage | Apply the successor safeguards below; refresh readiness/reminders |
| Correct execution facts | Completed, or a reopened execution's history | Append a correction; retain original facts | Unchanged unless a separate explicit usage correction is requested | Never rewrite later executions; reconcile any changed pending successor explicitly |
| Correct recorded usage | Existing execution/history | Append a linked debit/credit correction | Only the explicitly confirmed correction | Refresh readiness/shopping after the transaction commits |
| Cancel occurrence | Pending | Cancelled | Unchanged | Cancel its reminders; no automatic replacement job |

Once a job is skipped/cancelled, it is terminal. Recording work afterward requires
an explicit new/ad-hoc occurrence or a history correction, not toggling its state
through an old completion checkbox. Schedule archive cancels pending work and
stops generation while retaining terminal history. Pause stops generation;
retaining versus cancelling an existing pending job must be an explicit choice.
Cancelling a recurring schedule's active occurrence also pauses that schedule;
skip is the operation that deliberately advances it. A background generator must
not undo cancellation by immediately creating a replacement.
Resume retains any existing pending ID or creates one next occurrence using the
saved anchor/last actual completion; it never creates a missed-job backlog or
consumes stock. Resume after cancellation requires an explicit first due time,
so the backend does not guess whether to revisit a cancelled slot. Ordinary
maintenance archive/delete preserves history; the
application's authorized home/account deletion remains a separate data lifecycle.

Every date, recurrence, zone or requirement edit declares its scope: this
occurrence, future occurrences, or this pending occurrence and future work.
The backend previews/reconciles due-time effects and snapshots; it must not
silently apply an edit to history or retarget an already visible job ID. Commands
cannot use snooze as a disguised due-date change.

## Reopening and stock corrections

Reopening retains the original completion event and all net recorded usage.
Keep actual-used quantities separate from the occurrence's current requirements.
Completing the reopened job reuses its retained usage; only a confirmed missing
quantity may be deducted. Lowering requirements never silently returns stock,
and recording a genuinely second performance requires a new occurrence.

An ordinary reopen can supersede an automatically generated, untouched pending
successor in the same transaction: cancel that successor with a linked audit
reason, reopen the original ID and invalidate successor reminders. Regeneration
after recompletion uses a new successor ID. An edited/snoozed or terminal
successor, or any later recorded execution, causes a successor conflict requiring
explicit reconciliation. Do not create two pending jobs or erase another user's
changes. Check versions on every affected resource. Uniqueness must account for
explicit supersession: only the live generation occupies an active slot, while
old IDs remain terminal and linked to their replacement.

A usage correction names the original execution/consumption, the amount and a
reason. Never mutate/delete the original inventory transaction. Append a linked
compensating transaction, respect exact base units and caps, and prevent repeated
credits or returning more than the remaining recorded consumption. Extra usage
requires an explicit debit with the usual stock-availability checks.

Returning stock to an existing expired batch retains its expiry and remains
ineligible for maintenance/planning. Do not turn expired supplies into fresh
supplies. A missing batch or incompatible home/definition/unit produces a stock
reconciliation conflict; do not resurrect a row or automatically choose a
substitute batch. The authorized user may explicitly reconcile inventory and
link that correction, while a history-only reopening/correction can still retain
the original usage. Failure of a stock correction leaves that correction and
all its stock effects uncommitted.

## API and concurrency contract

The transition matrix defines semantic operations for #154–#156. Those issues
will publish the actual occurrence/schedule routes and schemas in OpenAPI before
clients use them. Resource IDs, command identity, versions, action names, explicit
scope and errors must carry the rules here; route spelling is not a reason to
create a second lifecycle implementation.

Modern mutations require an authenticated user, selected home, command UUID and
expected resource version. Include all affected versions when a command replaces
a successor. Use the existing home roles: members can read, owners/editors can
write; time-zone home administration remains owner-controlled. Authorize before
disclosing history/state or acknowledging an old receipt. Demo work stays in its
home and cannot emit real-world maintenance reminders.

Commit state, execution/correction history, usage, shopping effects, successor
changes, reminder invalidation and the successful command receipt atomically,
or record durable events for downstream processing in that transaction. External
notification delivery cannot be part of the SQL transaction. Exact replay of a
successful command acknowledges current state without repeating its effects.
Different payload/target reuse of a UUID and stale versions return conflicts.
Validation/insufficient-stock failures leave no successful receipt or partial
state. Refresh/reconcile before issuing a changed command with a new UUID; never
silently rebase destructive corrections onto a newer version.

Planned status semantics are 200 for acknowledged commands, 400 for invalid
input/unsupported intervals, 401 for missing authentication, 403 for denied roles,
404 for a missing or foreign-home resource in the selected-home API, and 409 for
stale versions, invalid transitions, command reuse or reconciliation conflicts.
Expose stable machine reasons alongside localized English/Turkish explanations;
no error may masquerade as a successful ignored command. Do not retrofit these
planned schemas into the current OpenAPI until their handlers are implemented.

## Legacy compatibility and migration

Keep each legacy task ID bound to its original occurrence or an explicit stable
mapping. An old URL must never start referring to the next pending occurrence.
Keep current read shapes/casing and identify the newer occurrence/lifecycle
contract through a versioned capability when the implementation is available.
Older clients must be able to read compatible records without losing history.

| Legacy request | Compatibility behavior to implement |
| --- | --- |
| Create without `is_completed`, or with false | Create pending work under representable rules; do not consume stock |
| Create with `is_completed: true` | Reject with completion-action-required; do not ignore it or consume supplies implicitly |
| PUT omits `is_completed` | Treat it as absent, not false; apply only a valid, unambiguous edit |
| PUT supplies the unchanged completion value | It does not request a lifecycle transition; allow compatible pending metadata edits or an actual no-op |
| PUT changes completion value | Reject the whole mutation with completion/correction-action-required; no partial metadata save |
| Old complete URL, initial pending execution | Delegate to the canonical atomic completion; same ID stays the same job |
| Old complete URL, already completed | Acknowledge current completed state without another deduction |
| Old unversioned complete after reopen/advanced changes | Require a capable client/modern command; an unidentified retry cannot consume again |
| Old edit cannot represent mode, scope or history correction | Return a capability/scope conflict, not a silent reset of recurrence or requirements |
| Old DELETE of recurring/terminal work | Require explicit schedule/occurrence scope or history handling; do not cascade-delete service history |

Track field presence when adapting the legacy boolean. Completed snapshots are
corrected through history operations, not rewritten by a generic PUT. Introduce
the adapter/capability and client recovery together; document deployment order
and do not retain accepted-but-ignored fields as the migration strategy.
A versionless legacy request cannot express a client's expected revision: the
adapter guarantees one execution and rejects unsafe advanced histories, while
capable clients must use versioned commands for stale-intent protection.

Migration changes neither stock nor previously recorded consumption. Preserve
legacy IDs, inventory links, quantities, timestamps and completed status, with
an auditable mapping to schedules/occurrences. Classify records as follows:

| Existing record | Treatment |
| --- | --- |
| Valid Once/empty frequency, pending | Preserve one pending occurrence and its absolute due instant |
| Valid Once, completed | Preserve completed history; infer no unknown actor or missing consumption |
| Repeating, incomplete, missing explicit mode/zone | Retain one actionable legacy pending job; mark the schedule needs-review before activating new generation |
| Repeating, already completed | Preserve inactive history; restart only on an explicit command choosing mode, zone and first due time |
| Fractional/invalid interval or ambiguous timestamp | Preserve original values for review; never truncate, reinterpret or auto-activate |

A review/restart must use the configured home zone and explicitly confirm the
recurrence interpretation. Completing retained legacy pending work is still
possible through the safe adapter; it does not activate an unconfirmed schedule.
Do not create a missed-occurrence backlog, mark gaps completed/skipped, or
retroactively consume inventory during migration/restart. Repeated migration or
restart requests must not duplicate records.

Old untyped inventory transactions have unknown origin. Do not infer maintenance
attribution from coincident times, definitions or amounts. For migrated work,
readiness can project confirmed upcoming requirements, but the maintenance-aware
forecast must mark those definitions' general-use estimate as unknown and omit
legacy unknown usage from that combined estimate. Use explicitly classified new
general consumption for the combined daily rate, and count new maintenance
usage only through its canonical occurrences. An explicit reviewed attribution
can later make old usage eligible; no fabricated backfill is required.

## Worked verification examples

Dates below are local to the saved schedule zone unless an offset is shown.
These are expected future behavior, not tests that the current handlers pass.

| Case | Expected result |
| --- | --- |
| Monthly after-completion, due April 1 at 09:00; done April 1 at 09:00 | Next due May 1 at 09:00; retain April history |
| Same job done April 20 at 15:00 | Next due May 20 at 15:00 |
| Calendar monthly on day 1 at 09:00, due April 1; done April 20 | Next due May 1 at 09:00 |
| Calendar job overdue since February 1; done May 20 | Keep one overdue job until completion; next due June 1; do not fabricate March–May executions |
| April 1 job actually done April 20, recorded May 25 | After-completion successor is May 20 and overdue; calendar successor is May 1 and overdue; neither is silently anchored to May 25 |
| Calendar monthly January 31, 2027 at 09:00 | February 28 then March 31 at 09:00 |
| After-completion monthly, done January 31 then done again February 28 | February 28 then March 28 due dates |
| Calendar yearly February 29, 2024 | February 28 in 2025–2027, February 29 in 2028 |
| Europe/Berlin calendar 02:30 on March 29, 2026 | Generate 03:30 CEST (01:30Z); retain nominal 02:30 for later dates |
| Europe/Berlin calendar 02:30 on October 25, 2026 | Generate once at the earlier 02:30 CEST (00:30Z), not again at 01:30Z |
| Custom 40 days or 3 weeks | Advance exactly that many local calendar days/weeks; 0.5 or 1.9 is rejected |
| Calendar April 1 job rescheduled to April 15 and completed then | Next remains May 1; the one-off move does not change the grid |
| April 1 job skipped on April 20 | Calendar next is May 1; after-completion next is May 20; stock and last actual completion are unchanged |
| Reminder snoozed to April 5 for April 1 work | Still overdue with the same supply demand; suppress reminders until April 5 |
| A 100 g completion is reopened and completed again | Retain the first execution and its 100 g usage; do not silently deduct another 100 g |
| An explicit 100 g return after its original batch expires | Credit that expired batch only through an audited correction; it is still unusable |
| Original batch was deleted before an attempted return | Controlled reconciliation conflict; no replacement row or fresh stock is invented |
| Legacy completed Monthly job encountered during migration | Preserve history; no automatic pending job, restart or deduction |

## Delivery and remaining gates

| Issue | Responsibility / gate |
| --- | --- |
| [#152](https://github.com/Chak-and-Jules/home-inventory-backend/issues/152) | Atomic completion/retry/audit foundation; can start against the current model |
| [#153](https://github.com/Chak-and-Jules/home-inventory-backend/issues/153) | Eligible stock; follows #152 |
| [#154](https://github.com/Chak-and-Jules/home-inventory-backend/issues/154) | Persistence, migration and history reads; follows this policy and #152 |
| [#155](https://github.com/Chak-and-Jules/home-inventory-backend/issues/155) | Canonical recurrence engine and generation after persistence/stock safety |
| [#156](https://github.com/Chak-and-Jules/home-inventory-backend/issues/156) | Lifecycle/compatibility commands and correction behavior |
| [#157](https://github.com/Chak-and-Jules/home-inventory-backend/issues/157) | Forecast classification and shared projection |
| [#158](https://github.com/Chak-and-Jules/home-inventory-backend/issues/158) | Supply allocation, readiness and shopping quantities |
| [#159](https://github.com/Chak-and-Jules/home-inventory-backend/issues/159) | Reminder channel, recipient and preference decisions |
| [#160](https://github.com/Chak-and-Jules/home-inventory-backend/issues/160) | Durable preference/scheduling/queue behavior after lifecycle and #159 |
| [#161](https://github.com/Chak-and-Jules/home-inventory-backend/issues/161) | Delivery adapter, retries and suppression after #160 |
| [#162](https://github.com/Chak-and-Jules/home-inventory-backend/issues/162) | Cross-feature verification, migration rehearsal and deployment compatibility |

The six product choices above are settled for #151. Provider/channel, recipient
and preference decisions remain a blocker only for the reminder branch, tracked
in [#159](https://github.com/Chak-and-Jules/home-inventory-backend/issues/159).
Implementation issues must turn the examples and transition/conflict cases into
database-backed PostgreSQL tests, including multiple completions, concurrent
workers, stock corrections, membership changes and old-client retries.

This policy PR adds documentation only. Runtime schemas/messages/OpenAPI changes
belong to the implementing issues. Coordinate with existing client milestones;
do not create issues outside the backend repository without a separate user
instruction.
