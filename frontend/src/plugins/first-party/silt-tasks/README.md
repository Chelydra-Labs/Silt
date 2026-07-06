# silt-tasks — Tasks view

The Tasks view is the first-party vault-scoped surface for every active
task (dated **and** undated), grouped by time horizon. It is a sibling
to Calendar's date-scoped agenda, not a replacement — its purpose is
specifically to surface undated tasks that the existing agenda surfaces
filter out by SQL design.

## Interaction model (#410 unified task-edit surface)

Single-click a row opens the shared **non-blocking inspector drawer**
(`first-party/shared/TaskEditDrawer.svelte`) — the same drawer Kanban
uses — for inline edits (pin, progress, recurrence, due date, status).
The pencil affordance on each row (or `Shift+Enter`) opens the shared
**sub-editor modal** (`TaskSubEditorModal`) for the task's child
sub-tree. The former single-click behavior (navigate-to-block / open the
source page) is now a button inside the drawer, hidden for standalone
(`.silt`) tasks. The row mark-done checkbox routes through the shared
`BlockedDoneDialog` when the task has open prerequisites, matching Kanban
and Agenda.

## SDK surface

The component is built exclusively on the PluginContext SDK — no
capabilities, no Go bindings, no schema changes:

- `ctx.sqliteQuery(sql)` — two queries: one for the open groups
  (`status != 'DONE'`, ordered so undated rows go to the tail), one
  for the Completed group (`status = 'DONE'`, ordered by `file_date
  DESC`).
- `ctx.updateBlockState(blockId, 'DONE')` — mark-done.
- `ctx.on('block:changed', reload)` — reflow on any task mutation.

## Unified hub foundation (`state.svelte.ts` + `query.ts`)

This package is also the home of the **forward-looking unified hub
foundation** (#419, milestone #37 phase 4). Two modules form the carrier
for a single Tasks hub that supersedes the parallel state/query patterns
invented independently by sibling surfaces:

- **`state.svelte.ts`** exports `TaskHubState` — a unified store carrying
  scope + filters (Kanban lineage) AND `focusDate` + `activeFilter`
  (Calendar lineage) together, plus a `DisplayMode` (`list` | `board`)
  and `groupBy` dimension. A future hub renders either a list (the
  Calendar/Agenda/Tasks lineage) or a board (the Kanban lineage) from
  this one state shape.
- **`query.ts`** exports `buildQuery` — a pure SQL builder lifted from
  the Kanban builder (the most capable of the three task surfaces) and
  extended with two optional levers the hub needs:
  - `groupBy` — re-orders rows so each group is contiguous (sort
    concern, not a filter concern).
  - `window` — adds a due-date `WHERE` window (`{ start, end }`,
    inclusive) for Calendar-style "just this month" queries without
    re-deriving the SQL inline.

These two modules are **purely additive** this phase: no existing
consumer imports them yet. The two legacy modules — `silt-calendar/
focusState.svelte.ts` and `silt-kanban/kanbanSharedState.svelte.ts` +
`silt-kanban/query.ts` — stay live until a later milestone migrates
their consumers, then they are deleted. The shapes were kept identical
so that migration is a one-for-one import swap.

The `scopeUserOverride` invariant (a user-narrowed scope survives an
automatic scope change) is preserved verbatim in `TaskHubState`
(`setScope` / `narrowScopeTo` / `clearScopeOverride`).

## Groups

| Group | Source | Style |
|---|---|---|
| **Overdue** | `due_date < today` | error tone |
| **Today** | `due_date == today` | primary tone |
| **Upcoming** | `tomorrow <= due_date <= today + 7 days` | muted tone |
| **No Date** | `due_date` is null or empty | muted tone, expanded by default |
| **Completed** | `status = 'DONE'` | collapsed by default; click chevron to expand |

The "No Date" label matches the cross-app convention (Things 3 Anytime,
Todoist no-date filter, Obsidian Tasks `no due date` group, TickTick
Inbox). The 7-day Upcoming window matches the Things 3 default; revisit
if users request more.

## Completion sort

The Completed group is ordered by `file_date DESC` — the file's
modification time as recorded in the `blocks` table when a task line
was last rewritten. This is a coarse completion-recency proxy.

The `tasks.completed_at` column shipped (#417 — ISO 8601 local timestamp
stamped on the DONE transition, cleared on reopen), so a dedicated
completion-recency sort is now possible. It is **nullable with no
backfill**: only tasks created after the column landed are guaranteed to
carry `[completed::]`, and only if they transitioned to DONE after it.
Older completed tasks (predating the column, or completed before it
shipped) have `NULL`, so the Tasks view still sorts on the `file_date`
proxy rather than excluding or pushing those rows to the bottom. Caveats
of the proxy:

- A bulk-edit that touches a file (e.g. a Markdown reformat of the
  whole `.md`) bumps every completed task in that file up together.
- Two tasks completed minutes apart on the same day may sort
  arbitrarily.

A follow-up can switch the Completed query to `ORDER BY
completed_at DESC NULLS LAST` (or `COALESCE(completed_at, file_date)
DESC`) once enough of the backlog carries the token, or once the
migration explicitly backfills it. Until then, the proxy keeps the
ordering stable for legacy rows.

## Focus target prop

The component accepts a `focusBlockId: string` prop. When set, the
list scrolls to the matching row (`data-block-id={id}`) and applies
the `.tasks-focused` class for a transient 3-second visual
highlight. The `focusKey` prop is a monotonic counter (typically a
timestamp) that re-fires the focus effect on every navigation even
when `focusBlockId` is unchanged.

This is the receiving end of the standalone-task navigation router
(`#374` — `frontend/src/lib/standaloneTasksNav.ts`). Search jumps,
tag-explorer clicks, and block-reference follows that target a
`.silt` block get re-routed here automatically; the
`STANDALONE_TASKS_NOTEBOOK = '.silt'` constant lives in that same
file and is the single source of truth on the frontend.

## Follow-ups

- **Drag-to-reschedule.** Moving a row between date groups should
  rewrite `[due:: YYYY-MM-DD]` on disk (currently exposed via
  Calendar's drag-and-drop through `ctx.setTaskDueDate`).
- **Switch the Completed sort to `completed_at`.** The column shipped
  (#417); see "Completion sort" above for why the query still uses the
  `file_date` proxy and what the cutover looks like.
- **Typed Go binding (`GetTasksView`).** The current implementation
  uses raw `ctx.sqliteQuery(...)` per the issue's "tradeoff noted"
  — promote to a Go method if the `tasks` schema gains columns we
  want to filter on without bumping every caller.

## Tests

`Tasks.test.ts` covers:
- undated → No Date (AC1)
- dated → Today, no duplication (AC2)
- overdue tone distinct from Today/Upcoming (AC3)
- Completed collapsed by default + expands on click (AC4)
- header count respects open vs. done (AC5)
- mark-done calls `updateBlockState` + removes the row (AC6); a blocked
  task surfaces the DONE-on-blocked guard first
- single-click opens the inspector drawer; pencil / Shift+Enter open the
  sub-editor; standalone tasks hide "Open source page"
- empty state when no tasks exist
- `focusBlockId` scrolls + highlights (cross-cuts #374 AC4)
- SQL pushes undated to the tail of the open list
- Upcoming group is capped at today + 7 days
