# Testing & Verification

**How to use this document.** The testing strategy, coverage matrix, and
verification gates for Silt.

- **Authoritative for:** test strategy, the coverage-by-package matrix,
  performance benchmarks and budgets, the manual-verification checklist.

**Principles**
- Read the coverage matrix and budgets as the verification contract; the
  per-change detail lives in the tests themselves and in git history.
- Budgets are hard regression gates enforced in CI, not aspirational targets.

**Rules**
- When a budget or matrix entry changes, update it here in the same change.
- Record a test's intent in the matrix, not a per-sprint changelog of what
  was added.

**Best practices**
- Link to the test file rather than restating its assertions here.

**Not for**
- Per-sprint coverage logs (that is git history) or implementation detail.

> See [`CONTRIBUTING.md`](./CONTRIBUTING.md) for the contribution workflow,
> pre-push hook setup (`git config core.hooksPath .githooks`), and the
> auto-regenerating `npm install` (via the `prepare` script —
> `npm run generate` is now an explicit-refresh alias).

## Testing constraints

- **No browser-driven e2e.** The Wails webview cannot run headless in CI, so
  there are no Playwright/Selenium-style end-to-end tests against the rendered
  app (see AGENTS.md). Cover interactions at the **IPC boundary** instead:
  mock the Wails bindings (`vi.mock` + `vi.hoisted` on
  `../../wailsjs/go/main/App.js`) and assert the contract — never hit live IPC.
- **Frontend:** Vitest (jsdom).
- **Backend:** Go's `testing` package, run with `-race`.

## Automated tests

```sh
go test -race -count=1 ./...          # Go suite (race detector)
cd frontend && npm run check && npm test   # frontend type-check + Vitest
```

### Coverage by package

The live source of truth for "what is tested" is the test files themselves;
this matrix records the intent of each area's coverage.

| Area | Covers |
|---|---|
| App bindings (root `*_test.go`) | the Wails-bound IPC surface — block mutation/state, navigation CRUD, vault lifecycle (move/copy/switch), archive export/import, plugin install/capabilities, recurrence, standalone tasks, templates, themes, spellcheck, updates, lock-order invariants |
| `backend/config` | config schema, load-over-defaults, atomic save, hot-reload |
| `backend/core` | `ExecutionCoordinator` — DB write serialization, read concurrency, per-file lock isolation |
| `backend/db` | `DatabaseManager` — block insertion/cascade, re-index, FTS5 (ranking/snippets/grouping/pagination), files-table incremental diff, WAL recovery, tag hydration |
| `backend/monitor` | `DirectoryWatcher` / `WriteTracker` — self-write suppression, focus leases + sweeper, symlink-loop handling |
| `backend/parser` | the AST parser — ID injection, date normalization, line/region parsing, single-serializer round-trip identity |
| `backend/plugins` | `.silt-plugin` validate/install/uninstall/enable, zip-slip + traversal guards, rate limiting, network safety (SSRF), capability grants |
| `backend/recurrence` | recurrence rule resolution, skip-missed advancement, end-of-month clamping |
| `backend/templates` | template validate/render/load/store/watcher, placeholder substitution, smart-graph passthrough |
| `backend/themes` | canonical schema, embed fallback, loader, validator (color/font sandbox) |
| `backend/updates` | update check, semver compare, download + SHA-256 verify |
| `backend/vault` | settings durability & theme persistence, vault move/copy/verify, archive manifest |
| Frontend (Vitest) | editor smoke + converter/schema round-trip identity, sidebar/tabs/titlebar, theme store, plugin surfaces, standalone-tasks router, a11y |

## Benchmarks & budgets

These are **hard regression gates** (run in the normal `-race` CI gate, skipped
under `-short`), not aspirations.

- **Cold scan** — index 1,000 page files in under **450 ms**
  (`TestScanWorkspace_BudgetRegression`; baseline ~280 ms).
- **Warm restart** — the on-disk WAL `files`-table diff for a 5,000-page vault
  stays well under budget (`BenchmarkWarmStart_5000Files`, ~48 ms/op).
- **Atomic-write safety** — a kill-mid-write recovers every committed block via
  WAL replay, with zero stray `*.tmp` files
  (`TestAtomicWrite_KillMidWriteRecoversViaWAL`).
- **UI frame budget** — a dev-only probe (`?perf=1`, `measureFrameBudget`)
  instruments the three hottest paths — Kanban drag-settle, editor transaction,
  theme-token injection — against the 16 ms / 60 FPS budget.

```sh
go test -bench=. -count=3 ./backend/parser/   # cold scan
go test -bench=. -count=3 ./backend/db/        # warm-restart diff
```

## Manual verification checklist

Interactions the jsdom layer cannot drive — HTML5 drag-drop, real
`DataTransfer`, layout-driven coordinates, native pickers — are verified
manually against `wails dev`. Grouped by surface; each item is pass/fail.

**Onboarding & vault lifecycle**
- [ ] First run shows the empty state; "Initialize Workspace" opens the native
      folder picker and scaffolds `.system/` (config, themes, templates).
- [ ] Close/reopen auto-loads the vault without re-showing the picker.
- [ ] Settings → General: Move vault and Copy vault work across volumes;
      Switch vault opens an existing vault; Export/Import round-trip a
      `.silt-vault`.

**Navigation & sidebar**
- [ ] Notebook › Section › Page CRUD from the sidebar; section-less pages
      group correctly; drag-to-reorder persists.
- [ ] Collapse/restore the sidebar (Ctrl+B); focus-sidebar (Ctrl+Shift+B)
      moves focus in.
- [ ] Linked (external) notebook: link, browse/search/edit in place; unlink
      leaves its files untouched.

**Editor**
- [ ] Block types round-trip: task/note/header, callout, code block, table,
      foldable details, blockquote.
- [ ] Inline formatting marks, alignment, color, math (`$…$`), Mermaid render.
- [ ] Edit ↔ Source toggle is per-tab; the scroll offset is preserved across
      the round-trip.
- [ ] Block drag-handle reorders/indents; Alt+ArrowUp/Down moves by keyboard.
- [ ] Find/replace in-page (Ctrl+F / Ctrl+H); global search (Ctrl+Shift+F)
      filters + sort; global replace (Ctrl+Shift+G) with per-match accept + undo.

**Smart Graph**
- [ ] `#ns/sub/leaf` tags render as pills and aggregate hierarchically.
- [ ] `((uuid))` reference: hover preview, click scrolls to source.
- [ ] `{{embed:uuid}}`: live portal; editing the embed updates the source and
      vice-versa.

**Tasks, Kanban, Calendar, Agenda**
- [ ] Task checkbox cycle (`[ ]`/`[/]`/`[x]`) writes to disk and re-indexes.
- [ ] Inline metadata tokens (due/start/owner/priority/pin/progress/recur)
      parse and project.
- [ ] Recurring task: completing it spawns the next instance with an advanced
      due date.
- [ ] Quick-add (`Mod+Shift+N`) creates a standalone task; the Tasks view
      lists it (including the No Date group).
- [ ] Kanban scope switch (vault/notebook/section/page); drag changes status.

**Themes & templates**
- [ ] Theme picker: switch, mode toggle (Dark/Light/System), import a JSON
      (rejected if unsafe), export.
- [ ] No first-paint flash of the wrong palette for a non-default theme.
- [ ] New page from template; `/template` insert at cursor; placeholders
      resolve.

**Plugins**
- [ ] Install/enable/disable/uninstall a `.silt-plugin`; capability-grant
      prompts on first use.
- [ ] First-party Agenda/Calendar/Kanban/Tasks render and respond to
      navigation.

**Self-update** (when enabled)
- [ ] Check for updates; a download is verified against the published SHA-256
      before it is offered to install.

## Known gaps

- No Wails integration test (requires the `wails dev` runtime) — covered by the
  manual matrix above.
- No watcher e2e against real fsnotify events.
- HTML5 drag-drop end-to-end (Kanban, block reorder) has no jsdom equivalent —
  manual-only.
- Cursor-position restore across the Edit↔Source round-trip is not yet
  implemented.
