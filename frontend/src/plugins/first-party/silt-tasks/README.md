# silt-tasks — the unified Tasks hub

The first-party vault-scoped surface for **every** task in the vault. One
plugin hosts **three display modes — List, Board, and Calendar** — over a
single grouping-first engine. The activity-bar entry is "Tasks"; a segmented
switcher in the header flips the projection without re-querying.

## Architecture

The package is organized around one reactive state hub that every renderer
and the sidebar read from and write to. There is no parallel
state/query module per mode.

| File | Role |
|---|---|
| `state.svelte.ts` | The unified reactive hub state (`TaskHubState`) — display mode, grouping, sort, scope, filters, focus date, smart-list filter, calendar sub-mode, status columns, saved views, and the active-view + dirty flags. Svelte 5 `$state`. |
| `query.ts` | Pure SQL builder (`buildQuery`) consuming hub state → `{ sql, params }`. Scope + filter WHERE clauses, groupBy/sort ORDER BY, optional due-date window. All values flow through `?` placeholders. |
| `grouping.ts` | Pure client-side binning (`binByDimension`) — assigns rows to ordered `GroupSection`s for the 9 grouping dimensions. `distinctValues` derives the universe of values for sidebar pickers. |
| `savedViews.ts` | The three code-defined `SYSTEM_VIEWS` + the `fingerprintOf` / `fingerprintOfState` helpers that power the "is the live state the same as this saved view?" check. |
| `settings.ts` | YAML pref I/O for everything under `plugins.plugin_settings.silt-tasks`. Reads come from the synchronous settings snapshot; writes go through the atomic `updatePluginSetting`. |
| `TasksHub.svelte` | The shell — title + open/done count header, mode switcher, FilterBar, scope breadcrumb, saved-view bookmark affordance, and the route to the active renderer. |
| `views/ListView.svelte` | List renderer — time-horizon (or any `groupBy`) sections, inline quick-add, row drag-reorder under `sort: manual`. |
| `views/BoardView.svelte` | Board renderer — status columns, HTML5 drag-and-drop with FLIP animation, manual `[order::]` reordering, ArrowLeft/Right keyboard parity. |
| `views/CalendarView.svelte` | Calendar renderer — month/week grids over a windowed due-date query. |
| `Sidebar.svelte` | The unified sidebar — smart lists, saved views, mini-calendar, and filter controls. Reads + writes the same hub state as the shell. |
| `components/FilterBar.svelte` | Owner / priority / due-date / tag filter chips (writes `TaskFilters`). |
| `components/TaskEditDrawer.svelte` | Shared non-blocking inspector drawer (pin, progress, recurrence, due date, status, owner, priority, tags). |
| `components/CommentThread.svelte` | Comment thread over a task's NOTE-block children (`FetchSubtree` + `block_meta` attribution). |
| `components/QuickAddTask.svelte` | Shared quick-add input (used by every mode's quick-add surface). |
| `components/BlockedDoneDialog.svelte` | The DONE-on-blocked confirm guard (matches the lock badge). |
| `components/DependencyPicker.svelte` | The `[blocked_by::]` typeahead picker. |
| `components/TaskSubEditorModal.svelte` | Shared scoped sub-tree editor (Shift+Enter from a card/row). |
| `types.ts` | The `TaskDetail` cross-surface task contract + priority/lane label helpers. |

## Data flow

```
  TaskHubState (state.svelte.ts)
        │  scope · filters · groupBy · sort · displayMode · columns
        ▼
  buildQuery (query.ts)  ──►  SQLite (ctx.sqliteQuery)  ──►  TaskDetail[] (types.ts)
        │                                                              │
        │                                              binByDimension  │ (grouping.ts)
        │                                                              ▼
        └────────────── TasksHub.svelte routes to ──────────► ListView / BoardView / CalendarView
                                                                   ▲
  Sidebar.svelte reads + writes the same hub state ────────────────┘
```

Every dimension setter in `state.svelte.ts` marks the active saved view
dirty when one is active, so the header bookmark can offer Update /
Save-as-new instead of just "saved". The sidebar and the header are
bidirectionally in sync because they read the same `$state` singleton.

## Configuration

All prefs live under `plugins.plugin_settings.silt-tasks` in the vault
`config.yaml` (ARCHITECTURE §0 rule 2). The full key set:

| Key | Meaning |
|---|---|
| `default_display_mode` | `list` / `board` / `calendar` (hydrated once on mount; every later switch persists). |
| `default_group_by` | One of the 9 `GroupBy` values (survives a List → Board hop). |
| `default_sort` | One of the 6 `SortMode` values. |
| `calendar_sub_mode` | `month` / `week`. |
| `columns` | Status-board lane order (defaults to `["TODO","DOING","DONE"]`). |
| `saved_views[]` | User-defined saved views (system views are re-derived from code, never persisted). |
| `local_author` | Seeded from the OS username (`ctx.getLocalAuthor`) on first comment; the user's override always wins. |

The `filters` shape (`{ owners, priorities, dueDate, tags }`) is held in hub
state and snapshotted into saved views; it is not separately persisted as a
default.

## Extending

**Add a new grouping dimension.** 1) Extend the `GroupBy` union in
`state.svelte.ts` and add the option to `GROUP_OPTIONS` in `TasksHub.svelte`
(+ `GROUP_BY_VALUES` in `settings.ts` for validation). 2) Add a `binByX`
case to `binByDimension` in `grouping.ts` (and `distinctValues` if the
sidebar should offer it as a picker). 3) If the dimension can sort
server-side, add an `orderByFor` / `sortClauseFor` branch in `query.ts`;
high-cardinality dimensions (tag/notebook/section/page) are binned
client-side and share the default ORDER BY.

**Add a new display mode.** 1) Extend the `DisplayMode` union and add the
option to `MODES` in `TasksHub.svelte`. 2) Add a `views/<Mode>View.svelte`
renderer that reads hub state + `ctx.sqliteQuery(buildQuery(...))`. 3) Add
the route case in the shell's content area. The hub state, query builder,
grouping, and sidebar need no changes — a new mode is just another renderer
over the same engine.

## Tests

Each module has a co-located `*.test.ts`:

- `state.test.ts` — every hub-state setter (ported from the legacy
  `kanbanSharedState` + `focusState` suites), scope-override invariant,
  saved-view upsert/apply/delete + the 50-user-view cap, `resetTaskHubState`.
- `query.test.ts` — scope branches, filter branches, combined scope+filters,
  groupBy/sort ORDER BY composition (ported from the legacy Kanban builder).
- `grouping.test.ts` — every `binByDimension` dimension, bucket ordering,
  the trailing Unassigned bucket, tag multi-membership.
- `savedViews.test.ts` — the three system views, fingerprint equality,
  state-vs-view matching.
- `settings.test.ts` — load/persist round-trips, system-view strip on write,
  legacy `silt-kanban.boards[]` forward-mapping.
- `TasksHub.test.ts` — mode switcher, saved-view apply/dirty tracking,
  hydrate of system + legacy views.
- `views/{ListView,BoardView,CalendarView}.test.ts` — per-renderer
  rendering, drag-reorder (`setTaskOrder`), quick-add, blocked-badge +
  DONE guard.
- `Sidebar.test.ts` — smart-list pick, saved-view list, mini-cal, filter
  sync back into hub state.
- `components/*.test.ts` — drawer, comment thread (incl.
  `ctx.getLocalAuthor` seeding + persist), dependency picker, sub-editor.
