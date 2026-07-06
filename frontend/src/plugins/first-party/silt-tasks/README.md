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
was last rewritten. This is the best-available completion-recency
proxy without introducing a new schema column. Caveats:

- A bulk-edit that touches a file (e.g. a Markdown reformat of the
  whole `.md`) bumps every completed task in that file up together.
- The proxy is coarse — two tasks completed minutes apart on the
  same day may sort arbitrarily.

A dedicated `tasks.completed_at` column would cleanly resolve both
caveats. Tracked as a follow-up; the v1 implementation accepts the
proxy in exchange for shipping without a schema migration. If a
follow-up adds the column, the Completed query is the only one to
update.

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
- **Dedicated `completed_at` column.** As noted above.
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
