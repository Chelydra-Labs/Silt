# first-party/shared — shared task-edit surface

Cross-surface components consumed by every task-editing view so a task
edited from one surface behaves identically to one edited from another
(one unified system, #410). Sibling first-party plugins import these via
`../shared/<Component>.svelte`.

## Components

- **`TaskEditDrawer.svelte`** — the non-blocking inspector drawer. Single
  click on a task (Tasks row or Kanban card) opens it. Edits pin, progress,
  recurrence, **due date** (`ctx.setTaskDueDate`), and **status**
  (`ctx.updateBlockState`) inline, all optimistic + revert-on-error. The
  DONE transition routes through `BlockedDoneDialog` when the task has open
  prerequisites. Non-modal (`aria-modal="false"`, no scrim, focus-not-
  trapped) so the host list stays interactive — click another task to
  switch. Source-aware: standalone (`.silt`) tasks show "Standalone task"
  and hide "Open source page". Optional `onOpenSubEditor` renders an
  "Open sub-editor" button.
- **`TaskSubEditorModal.svelte`** — the focused scoped TipTap sub-editor
  (#304). Opens over the drawer (or directly via the Tasks pencil /
  Kanban `Shift+Enter`) and splices the edited child sub-tree back via
  `ctx.saveSubtreeBlocks` (#305). Source-aware breadcrumb in its header.
- **`DependencyPicker.svelte`** — the `[blocked_by::]` prerequisite editor
  (a drawer child), backed by `ctx.searchTasks` / `ctx.sqliteQuery` /
  `ctx.setTaskBlockedBy`.
- **`BlockedDoneDialog.svelte`** — the DONE-on-blocked confirmation (#302).
  Shared by the drawer's status control, Kanban's drag-drop, and Agenda's
  mark-done so the guard is identical everywhere.
- **`QuickAddTask.svelte`** — the title-only quick-add input (Tasks footer,
  Kanban per-column, Calendar day-cells, global `Ctrl+Shift+N`).

## Contract

- **`types.ts`** exports `TaskDetail` — the single task shape every surface
  consumes (generalized from the former silt-kanban `KanbanCard`), plus the
  `PRIORITY_LABELS` / `laneLabel` / `priorityClass` helpers. SQL projections
  mirror `silt-kanban/query.ts` (correlated subqueries for `tags` /
  `blocked_by` / `is_blocked`; **no LEFT JOINs** that would inflate rows).

## Scope notes / forward dependencies

- **`#412`** — owner / priority / tags / title editing from the drawer
  (needs new Go backend bindings + parser write-side; purely additive to
  the drawer once it lands).
- **`#413`** — functional Kanban custom columns (the drawer's status
  control ships as the three canonical TODO/DOING/DONE statuses today and
  evolves when custom lanes are real).
- **`#414`** — Calendar + Agenda adoption of this shared surface (the
  components are ready; wiring is the follow-up).
