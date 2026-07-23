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
  `../../bindings/silt/app.js`) and assert the contract — never hit live IPC.
- **Frontend:** Vitest (jsdom).
- **Backend:** Go's `testing` package, run with `-race`.

## Automated tests

```sh
go test -race -count=1 ./...          # Go suite (race detector)
cd frontend && npm run format:check && npm run lint && npm run check && npm test
# frontend: Prettier check + ESLint + svelte-check + Vitest (jsdom; no Playwright)
```

Frontend quality gates (local and CI) are distinct:

- **`format:check`** — Prettier on authored `src` (ts/svelte/css). Write mode remains `npm run format` and the pre-commit hook.
- **`lint`** — ESLint type-aware recommended rules on authored JS/TS/Svelte (including colocated tests; `projectService` + `.svelte`). Does not lint generated `bindings/` or `dist/`. Does not replace `svelte-check`.
- **`check`** — `svelte-check` for Svelte/TypeScript diagnostics and a11y warnings.
- **`test`** — Vitest/jsdom only; no browser-driven e2e.

### Coverage by package

The live source of truth for "what is tested" is the test files themselves;
this matrix records the intent of each area's coverage.

| Area | Covers |
|---|---|
| App bindings (root `*_test.go`) | the Wails-bound IPC surface — block mutation/state, navigation CRUD, vault lifecycle (move/copy/switch), archive export/import, plugin install/capabilities, recurrence, task dependencies (cycle prevention, DONE fan-out), subtree fetch/splice, standalone tasks, templates, themes, spellcheck, updates, lock-order invariants |
| `backend/config` | config schema, load-over-defaults, atomic save, hot-reload |
| `backend/core` | `ExecutionCoordinator` — DB write serialization, read concurrency, per-file lock isolation |
| `backend/db` | `DatabaseManager` — block insertion/cascade, re-index, FTS5 (ranking/snippets/grouping/pagination), files-table incremental diff, WAL recovery, tag hydration, task_dependencies projection + cascade |
| `backend/dependencies` | task-dependency ref extraction/formatting, DFS cycle detection (WouldCreateCycle, DetectsCycle) |
| `backend/monitor` | `DirectoryWatcher` / `WriteTracker` — self-write suppression, focus leases + sweeper, symlink-loop handling |
| `backend/parser` | the AST parser — ID injection, date normalization, line/region parsing, single-serializer round-trip identity, `blocked_by` token round-trip |
| `backend/plugins` | `.silt-plugin` validate/install/uninstall/enable, zip-slip + traversal guards, rate limiting, network safety (SSRF), capability grants |
| `backend/recurrence` | recurrence rule resolution, skip-missed advancement, end-of-month clamping |
| `backend/templates` | template validate/render/load/store/watcher, placeholder substitution, smart-graph passthrough |
| `backend/themes` | canonical schema, embed fallback, loader, validator (color/font sandbox) |
| `backend/updates` | update check, semver compare, download + SHA-256 verify |
| `backend/vault` | settings durability & theme persistence, vault move/copy/verify, archive manifest |
| Frontend (Vitest) | editor smoke + converter/schema round-trip identity, sidebar/tabs/titlebar, theme store, plugin surfaces, standalone-tasks router, silt-tasks Board blocked-badge + DONE guard, silt-tasks List blocked-badge + guard, DependencyPicker, Task Sub-Editor Modal, a11y |

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
  instruments the three hottest paths — Board drag-settle, editor transaction,
  theme-token injection — against the 16 ms / 60 FPS budget.

```sh
go test -bench=. -count=3 ./backend/parser/   # cold scan
go test -bench=. -count=3 ./backend/db/        # warm-restart diff
```

## Manual verification checklist

Interactions the jsdom layer cannot drive — HTML5 drag-drop, real
`DataTransfer`, layout-driven coordinates, native pickers — are verified
manually against `wails3 dev`. Grouped by surface; each item is pass/fail.

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
- [ ] Tab / Shift-Tab indent (or remapped `indent_block` / `unindent_block`):
      second bullet/task/plain line nests visually under the previous sibling;
      outer block root keeps `data-depth` in sync; save/reload preserves nesting.
- [ ] Find/replace in-page (Ctrl+F / Ctrl+H); global search (Ctrl+Shift+F)
      filters + sort; global replace (Ctrl+Shift+G) with per-match accept + undo.
- [ ] New page opens as blank prose (no bullet marker); Backspace on the blank
      line is a clean no-op (no duplicate line created) — reload stays blank.
- [ ] Typing `- `, `* `, `+ `, or `1. ` at the start of a blank line still
      creates the bullet/numbered marker (explicit input rules unaffected).

**Smart Graph**
- [ ] `#ns/sub/leaf` tags render as pills and aggregate hierarchically.
- [ ] `#` typeahead opens on typing `#`; recent tags (MRU from `RecordTagUsage`)
      appear first, then the full index; selecting records usage.
- [ ] `((uuid))` reference: hover preview, click scrolls to source.
- [ ] `((` typeahead opens a block-reference picker; selecting inserts
      `((uuid))` as an inline atomic node.
- [ ] `{{embed:uuid}}`: live portal; editing the embed updates the source and
      vice-versa.
- [ ] `[[Page]]` / `[[Section/Page#Heading|alias]]`: chip resolves, click opens
      the page (heading scrolls); unresolved/ambiguous chips are non-links.
- [ ] `[[` typeahead opens a page-link picker; calls `SearchPages` for a
      server-side substring filter; selecting inserts the shortest-unique-path
      as a `pageLinkNode`.
- [ ] Unresolved chip: hover → "Create page" → page created in active
      notebook/section, chip becomes a link, navigates to the new page.
- [ ] Ambiguous chip: hover → candidate pick list + "Create page" button;
      pick navigates, create uses the typed target name.
- [ ] `[[ExistingNotebook/Page]]` (first segment matches a real notebook name)
      creates the page in that notebook (section empty); `[[RandomSection/Page]]`
      (first segment does not match any notebook) creates in the active notebook
      under that section. Hover shows the resolved path subtitle.
- [ ] Rename/move target page rewrites inbound `[[…]]` (alias/heading preserved);
      block UUIDs unchanged.
- [ ] Tab context: **Copy Page Path** (plain path) and **Copy Page Reference**
      (`[[shortest]]`).

**Backlinks panel**
- [ ] Breadcrumb "Backlinks" crumb opens the panel; it shows all inbound refs
      (`[[…]]`, `((uuid))`, `{{embed:uuid}}`) for the active page, grouped by
      source page with kind badges and snippets. (Manual verification: the panel
      requires a live webview — no jsdom coverage.)
- [ ] Adding/removing a `[[link]]`, `((ref))`, or `{{embed:}}` to the active
      page refreshes the backlinks panel within ~200 ms (debounced
      `block:changed` listener). (Manual only.)
- [ ] Clicking a backlink item navigates to the source page via
      `navigate-to-page`. A separate "Jump to exact block" button (shown when
      `sourceBlockId` is present) scrolls to the specific block via
      `navigate-to-block`.
- [ ] Panel shows correct empty states: no page open → prompt; no backlinks →
      hint with link/bracket syntax.

- [ ] A page with more than 50 backlinks shows **Load more**; loading another
      page appends distinct rows, while a content refresh resets to the first
      page. (Manual only.)

**Autosave status**
- [ ] Typing shows dirty tab only (no "Saving…" during debounce).
- [ ] In-flight save shows muted "Saving…"; success shows transient "Saved".
- [ ] Save failure is assertive "Save failed" (fail-loud).

**Writing Assistant proposed edit**
- [ ] Selection replace shows in-editor strike + ghost preview before Accept.
- [ ] Accept = one history step + autosave; Reject/Esc clears preview with no disk write.
- [ ] Multi-paragraph proposal on a block-spanning selection: Accept creates one
      note block per paragraph (structure preserved, not flattened).
- [ ] Schema-incompatible multi-paragraph proposal (e.g. inside a table cell):
      in-editor preview not shown; panel Accept uses SDK apply path (no silent drop).

**Drawers**
- [ ] Opening Writing Assistant closes AI Q&A and vice versa; Escape only affects the open drawer.

**Tasks (silt-tasks hub)**
- [ ] Task checkbox cycle (`[ ]`/`[/]`/`[x]`) writes to disk and re-indexes.
- [ ] Inline metadata tokens (due/start/owner/priority/pin/progress/recur)
      parse and project.
- [ ] Recurring task: completing it spawns the next instance with an advanced
      due date.
- [ ] Quick-add (`Mod+Shift+N`) creates a standalone task; the Tasks hub
      lists it (including the No Date group).
- [ ] Tasks hub inline quick-add: type at the bottom input, Enter creates a
      task; the input clears and stays focused for rapid entry; the new row
      appears on the next tick via `block:changed`. Confirm on both an empty
      list (input pinned to viewport bottom) and a long scrolling list.
- [ ] Tasks hub task creation works on a fresh vault AND an existing vault
      (grants re-seed on launch — no `content-mutate` capability error).
- [ ] **Mode switching**: List / Board / Calendar switch from the header;
      the chosen mode + grouping + sort persists across restarts.
- [ ] **Grouping**: all 9 dimensions (None/Status/Owner/Priority/Due date/
      Tag/Notebook/Section/Page) bin correctly; the trailing Unassigned
      bucket renders for empty values.
- [ ] **Board scope switch** (vault/notebook/section/page); drag changes
      status; `sort: manual` cross/same-column drops persist via
      `setTaskOrder`.
- [ ] **Saved views**: apply, save-as-new, update an active view, delete;
      the three system views are present, read-only, and survive a restart.
- [ ] Task dependencies: add/remove via the CardDetailPanel picker; the lock
      badge renders on blocked cards in Board + List; completing a blocker
      clears the dependent's badge; completing a blocked task prompts for
      confirmation; a circular dependency is rejected inline.
- [ ] Comment threads: add a NOTE-child comment; the `[author::]`/`[ts::]`
      tokens persist; the composer seeds the author from the OS username on
      first post and caches it.
- [ ] Task Sub-Editor: double-click a Board card opens the modal; nested
      notes/sub-tasks edit and save back to the parent file; surrounding
      content is untouched; Esc restores focus to the card.
- [ ] CardDetailPanel recurrence dropdown and the dependency typeahead results
      stay fully visible (not clipped) when the section sits low in the scroll
      area; arrow/Enter/Esc and click-away still behave.

**Themes & templates**
- [ ] Theme picker: switch, mode toggle (Dark/Light/System), import a JSON
      (rejected if unsafe), export.
- [ ] Caret is clearly visible on click in both a light theme (e.g. Cyber Forest
      light) and a dark theme — the thin 1–2px caret line reads at a glance
      against the editor surface in every first-class theme.
- [ ] Theme-picker swatches show surface identity: warm (Linen), neutral
      (Graphite), and cool (Frost) themes are distinguishable at a glance by
      the base chip fill across all 11 themes in both dark and light modes.
- [ ] Inline color-picker default palette derives from the active theme — no
      Tailwind clashing on Graphite (monochrome), Terra Noir (earth), or
      Synthwave (neon). User-stored mark colors survive a theme switch.
- [ ] No first-paint flash of the wrong palette for a non-default theme.
- [ ] New page from template; `/template` insert at cursor; placeholders
      resolve.

**Plugins**
- [ ] Install/enable/disable/uninstall a `.silt-plugin`; capability-grant
      prompts on first use.
- [ ] First-party silt-tasks hub renders and responds to navigation across
      all three modes (List/Board/Calendar); silt-attachments renders.

**Self-update** (when enabled)
- [ ] Check for updates; a download is verified against the published SHA-256
      before it is offered to install.

**Resilience & hardening** (vault-close AI drain, id-strip detection, IPC errors)
- [ ] With an AI provider configured and a plugin making a slow completion
      (e.g. a local model), trigger a vault switch mid-call — the switch
      returns in well under the provider timeout (no ~60s hang); the aborted
      call's audit entry lands in the SOURCE vault's `ai.log`, not the
      target's.
- [ ] In an external editor, strip every `<!-- id: ... -->` comment from a
      multi-task file and save — Silt shows a sticky warning naming the page,
      with a "Show file" action that opens it; the copy explains links may
      have broken and how to recover. Creating a brand-new file with many
      tasks does NOT trigger the warning.
- [ ] Open a task's file in the editor (focus lock), then trigger a task
      setter from elsewhere (e.g. the Tasks hub) — the error banner shows
      the friendly "save or close it first" copy. (The mapping is code-based,
      so a backend wording change must not regress it.)

## Known gaps

- No Wails integration test (requires the `wails3 dev` runtime) — covered by the
  manual matrix above.
- No watcher e2e against real fsnotify events.
- HTML5 drag-drop end-to-end (Board, block reorder) has no jsdom equivalent —
  manual-only.
- Cursor-position restore across the Edit↔Source round-trip is not yet
  implemented.
- Backlinks panel, `[[`/`((`/`#` typeaheads, and the `SearchPages` IPC are
  tested at the Go-unit and Vitest-mock level; the end-to-end typeahead
  interaction (user typing `[[` → server result → picker render → insert)
  requires a live webview and is manual-only.
