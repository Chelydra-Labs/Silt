# ADR 0003: Tasks unification — one plugin, internal display modes

Date: 2026-07-06
Status: Accepted

## Context

Silt entered this milestone with **four overlapping task plugins**, each
shipped independently against its own surface:

- `silt-calendar` — month / week / agenda layouts of tasks by due date.
- `silt-kanban` — drag-and-drop board (TODO / DOING / DONE) with saved boards.
- `silt-tasks` (legacy) — a flat five-group list (Overdue / Today / Upcoming /
  No Date / Completed).
- `silt-agenda` — already merged into `silt-calendar` in #322.

Each plugin carried its **own sidebar, own shared-state module, own SQL query
path, own filter UX, and own save/persist semantics**. The duplication was
real: `KanbanSidebar.svelte` and `CalendarSidebar.svelte` re-implemented
parallel smart-list / filter / saved-configuration UIs;
`kanbanSharedState.svelte.ts` and `focusState.svelte.ts` modeled overlapping
state shapes that never unified because they shipped against separate
surfaces. The activity bar showed five entries (notes / tags / calendar /
tasks / kanban) for what is conceptually one kind of object.

Three forces made the status quo unsustainable:

1. **Per-plugin webview migration (#151 / #152).** Each first-party plugin is
   slated to gain its own isolated webview. Four task plugins means four
   webview lifecycles to manage, four SDK copies in memory, four reload paths.
2. **Maintenance cost.** A grouping or sorting improvement had to land in
   three places (List, Board, Calendar); a filter change twice (Kanban,
   Calendar); a saved-configuration feature once per plugin.
3. **User mental model.** Tasks are one concept. The board, the calendar, and
   the list are three visualizations of the same data, not three products. A
   user who saves "Today's Board" should be able to flip it to "Today's
   Calendar" without re-authoring the configuration.

## Decision

**Collapse the four task plugins into one `silt-tasks` plugin that hosts
three internal display modes — List, Board, Calendar — over a single
grouping-first engine.**

- One `TaskHubState` reactive store (`state.svelte.ts`) is the single source
  of truth for display mode, grouping dimension, sort, scope, filters,
  calendar sub-mode, status columns, and saved views.
- One SQL builder (`query.ts`) consumes the state and serves all three
  renderers (`views/{ListView,BoardView,CalendarView}.svelte`).
- One shell (`TasksHub.svelte`) hosts the mode switcher, shared FilterBar,
  scope breadcrumb, and routes to the active renderer.
- One unified sidebar (`Sidebar.svelte`) shows smart lists, saved views,
  mini-cal, and active filters — same sidebar for all three modes.
- Saved views span modes: a SavedView snapshots every hub dimension
  (displayMode + groupBy + sort + scope + filters + calendarSubMode +
  columns), so "Today's Board" and "This Week's Calendar" live in one list.

The activity bar collapses from five entries to three (notes / tags / tasks).
Old view-ids (`calendar`, `kanban`) resolve to `silt-tasks` for one release
via an alias shim so saved navigation state and external events keep working.

## Options considered

**A. One unified plugin with internal display modes (chosen).** The hub
owns the three renderers as internal modes; grouping and saved views are
shared. One webview, one state, one sidebar, one query path.

**B. Keep four plugins; extract a shared library.** Pull the duplicate
logic (state, query, sidebar facets) into a common module the four plugins
import. Lowest churn at the time, but the plugin boundaries stay artificial,
the activity bar stays cluttered, and per-plugin webview work multiplies
four-fold.

**C. Shell plugin with display modes as nested child plugins.** A
`silt-tasks-host` plugin hosts three child plugins (`silt-tasks-list`,
`silt-tasks-board`, `silt-tasks-calendar`) that register themselves as
modes. Maximally extensible (third parties could add modes) but introduces
an extension contract before there's a single consumer; the API surface
would be guesswork, and the inter-plugin IPC cost for sharing state across
modes would dwarf any benefit.

**D. Three plugins (drop Agenda, keep Calendar + Kanban + Tasks).** A
half-measure — the duplication remains across the three survivors, and the
activity bar still shows four entries. Solves none of the three forces
above.

## Why A won

- **Survives per-plugin webview (#151 / #152) without rework.** One plugin
  gets one webview; the migration is a fixed cost instead of a multiplier.
- **One source of truth.** Saved views, scope, filters, and grouping all
  live in `TaskHubState`; the sidebar and the header and the renderers all
  read and write the same store. No bidirectional-sync footguns like the
  old KanbanSidebar ↔ FilterBar bridge.
- **Mode-spanning saved views.** A user can save "By Owner" (list) and
  re-open it as "By Owner (board)" by changing one dimension. With separate
  plugins this required authoring the view twice.
- **Genuinely shared engine.** Board columns, List groups, and Calendar
  due-date buckets are all projections of the same grouping dimension. The
  grouping engine (`grouping.ts`) and SQL builder (`query.ts`) had to exist
  anyway; rendering three views off them is cheaper than three independent
  renderers.
- **New pattern: a plugin with internal display modes.** Documented in
  `PLUGIN_DEVELOPMENT.md` as a reference for future first-party surfaces
  that need multiple visualizations of the same data.
- **Matches user mental model.** "Tasks" is one concept; the three modes are
  lenses, not products.

## Consequences

- **One-time Go migrator (#431).** Existing vaults carry
  `plugins.plugin_settings.silt-calendar` and `silt-kanban` keys. A gated,
  idempotent migrator (`backend/vault/task_plugin_migrate.go`) runs on vault
  open, mapping the legacy fields into the unified `silt-tasks` schema. Old
  keys remain in YAML for one release so a downgrade still sees the user's
  config; they are dropped in N+1.
- **View-id alias shim (one release).** `calendar` and `kanban` view-ids
  (from saved nav state or external events) resolve to `silt-tasks` and
  hint the matching display mode. The shim lives in `App.svelte` +
  `getPluginSidebar.ts` and is documented for removal in N+1.
- **Linked-notebook fallback shim (one release).** `GetPluginSettingsForNotebook`
  aliases `silt-kanban` → `silt-tasks` when a linked notebook lacks the old
  id but has the new one, so linked notebooks that haven't been re-linked
  keep working.
- **System views are partial templates, not snapshots.** The three built-in
  views (Today's Board / By Owner / This Week's Calendar) intentionally omit
  dimensions irrelevant to their mode (a Board view doesn't snapshot
  calendar sub-mode). A lenient `viewMatchesState` helper compares only the
  dims a view defines, so partial-template views still highlight correctly
  in the sidebar and the bookmark icon. User views created via "Save current
  view" snapshot every dimension and reduce to the strict equality check.
- **~5,300 lines deleted** (the two retired plugin directories + their
  tests). Test count dropped accordingly; the lifted `silt-tasks/views/*`
  test suites cover equivalent ground.
- **`shared/` task components moved into `silt-tasks/components/`.** The
  transitional `shared/` directory is gone; `TaskEditDrawer`,
  `TaskSubEditorModal`, `BlockedDoneDialog`, `DependencyPicker`,
  `QuickAddTask`, and the Phase 8 `CommentThread` now live under the
  unified plugin where they're consumed.
- **Forward-only.** The unified hub is the basis for future task work
  (per-task notifications, recurring-task engine hardening, cross-vault
  task aggregation). Adding a new grouping dimension is one type extension
  + one binning case + one UI option; adding a new display mode is one
  view file + one route case in `TasksHub`.
