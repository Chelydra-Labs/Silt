Technical Specification: Silt

A Local-First, High-Performance Hybrid Note & Task Management Lifecycle Architecture

**How to use this document.** This is the product *north star*: the
intended functionality and requirements Silt is built toward, expressed as
the best long-term solution — not a snapshot of any single sprint.

- **Authoritative for:** product behavior, file formats, the AST/grammar,
  the plugin SDK contract, the config schema, and non-functional targets.

**Principles**
- This is the *north star*: the intended functionality expressed as the
  best long-term solution, not a snapshot of any single sprint.
- Kept in sync with reality — when an implementation takes a better
  direction than an earlier plan, this spec is updated to reflect it. It is
  neither a frozen original spec nor a changelog.
- Describe **what the product must do and why** — the requirements — not how
  it is built. SPECS stands on its own as the north star: it may cover the
  same ground as ARCHITECTURE, but expressed at the requirements level.

**Rules**
- Describe the destination, not the progress.
- Rejected-alternative reasoning goes to ADRs; keep a one-line pointer here
  only when it is load-bearing.
- No implementation identifiers (Go package paths, IPC binding names,
  internal function names, Wails event names, frontend file paths) unless
  they *are* the contract — e.g. the plugin SDK interface.

**Best practices**
- Prefer a cross-reference (an ADR, an ARCHITECTURE section) to an inline
  `(#123)` tag.
- When the spec and the code diverge, decide which is the better long-term
  direction and update the lagging one deliberately.

**Not for**
- **Implementation detail** — how it's built (package paths, binding/function
  names, internal mechanisms, event names). That lives in ARCHITECTURE and
  the code; SPECS describes the requirement, not the mechanism.
- **Implementation status** — "done in sprint N," "not yet built,"
  "% complete."
- **Rejected-alternative essays** — "we considered X and rejected it."
- **Changelog / issue archaeology** — sprint numbers and `(#123)` tags.

ARCHITECTURE.md describes the system as built today; this spec describes
where it is going.

1. Executive Summary & Philosophy

1.1 Problem Statement

Modern personal knowledge management (PKM) and task-management tools are fundamentally split. Hierarchical tools excel at spatial partitioning and structured organization but fail at temporal journaling, lightweight processing, and open formats. On the other hand, outline graph-based systems offer friction-free, daily logging but struggle to natively integrate rich task metadata directly into the block-stream. Relying on complex, third-party plugin ecosystems to connect notes and tasks introduces structural instability, speed degradation, and unpredictable data-serialization standards.

1.2 The System Vision: Silt

Silt is an uncompromised, local-first desktop application designed to bridge structured notebooks with daily chronological capture streams. It treats simple, human-readable Markdown text files on your local drive as the immutable database of record. Simultaneously, it uses a lightweight, compiled Go-based backend and an in-memory SQLite indexing cache to serve real-time multi-dimensional productivity views:

The Document View: A seamless, virtualized infinite scrolling page of notes organized by days.

The silt-tasks Plugin: A single unified Tasks hub exposing the same task set through three internal display modes — **List** (a time-horizon roll-up), **Board** (drag-and-drop status columns), and **Calendar** (a spatial month/week grid) — over one grouping-first engine.

The Sovereign Principle: The local directory structure is the single source of truth. The application runtime acts strictly as a reactive viewport, transforming and writing text mutations safely back to disk without vendor lock-in.

2. Technical Stack & Decoupled Architecture

To hit our strict resource limits and keep the UI completely lag-free, Silt avoids bloated Electron wrappers in favor of a compile-time optimized, system-native desktop wrapper.

+-----------------------------------------------------------------------+
|                             SVELTE FRONTEND                           |
|  - Infinite Scroll Stream       - Dynamic Plugin Rendering Engine     |
|  - Rich Interactive AST Tokens  - Fast Keyboard Command Palette       |
+-----------------------------------------------------------------------+
                                  ▲  ▼
                        Wails IPC Bridge (JSON)
                                  ▲  ▼
+-----------------------------------------------------------------------+
|                           GO BACKEND CORE                             |
|                                                                       |
|   +-------------------+    Event    +----------------------------+    |
|   |   File Watcher    |  Triggered  |        AST Parser          |    |
|   |    (fsnotify)     | ---------- Pinpoint Block Extraction    |    |
|   +-------------------+             +----------------------------+    |
|             |                                     |                   |
|       Disk Changes                            Map Blocks              |
|             ▼                                     ▼                   |
|   +-------------------+             +----------------------------+    |
|   | Markdown Files on |             |     SQLite Cache Index     |    |
|   | Local Storage     | ◄---------- | - Tasks, Tags, Blocks      |    |
|   | (Atomic Writes)   |  Sync Write | - Hierarchical Links       |    |
|   +-------------------+             +----------------------------+    |
+-----------------------------------------------------------------------+


2.1 Stack Blueprint

Desktop Engine: Go (v1.26+). Handles system-level interactions, high-efficiency disk operations, directory indexing, config management, and AST parsing.

Application Shell Bridge: Wails Framework. Connects Go structures directly to platform-native WebKit engines (WebKit on macOS, WebKit2 on Linux, WebView2 on Windows) without bundled Node/V8 runtimes.

UI Presentation Layer: Svelte 5 + Tailwind CSS. Selected for its compile-time reactive paradigm. Svelte writes direct, targeted DOM updates, preserving rendering cycles during heavy UI interactions like dragging task cards or scrolling long documents.

Analytical Query Layer: SQLite 3 (In-Memory / Local Volatile Cache). Provides lightning-fast multi-dimensional indexing, relational join processing, and instant filtering across notebooks, sections, tags, and date limits.

2.2 Unidirectional State Synchronization

The architecture decouples UI actions from disk manipulations to prevent editing stutters:

Frontend State Shift: User interacts with a visual component (e.g., ticking a task checkbox).

IPC Event Despatch: Svelte transmits a structured JSON envelope across the Wails IPC bridge containing the targeted block's UUID and requested modification.

Backend Atomic Mutation: The Go backend locates the precise line in the target Markdown file, stages a temporary write, performs an atomic overwrite, and notifies the internal cache.

Index Optimization: The in-memory SQLite database processes the block shift.

Reactive Feedback Loop: The backend broadcasts a UI state event to ensure other views (e.g., Board columns or the Calendar) update in perfect lockstep.

3. File Directory Structure & Storage Engine

3.1 The Single-File Page Model

While the user experiences each Page as a single, endless, scrollable document, storing an entire page in one file is practical because a page is a focused topic (not an unbounded daily journal). Each block within the page carries its own `file_date` in the trailing `<!-- id: uuid @ YYYY-MM-DD -->` comment, preserving the temporal dimension the agenda and calendar views rely on.

The Go engine parses the single `.md` file on load and streams the blocks to the TipTap editor in Svelte. Writes are debounced and serialized atomically (temp file + fsync + rename) so a crash never corrupts the file. Blocks from different dates coexist in the same page file — the date is per-block, not per-file.

3.2 Physical Directory Layout

Silt uses a three-level hierarchy — **Notebook > Section > Page** — mapped directly onto folders on disk, where the **Section layer is optional**:

- A **Notebook** is a top-level folder directly under the vault root. Users open existing notebook folders or create new ones from the notebook selector. Multiple notebooks can be open at once.
- A **Section** is an optional grouping folder within a Notebook. Sections are shown even when empty, so a freshly created section appears immediately.
- A **Page** is a single `.md` file and is the **streaming unit**: the editor renders one TipTap instance per page. Each block within the page carries its own `file_date` in the trailing comment, so blocks from different dates coexist in one file. A page may live directly under a Notebook (no section) or nested within a Section.

```
VaultRoot/
├── .system/
│   ├── config.yaml
│   ├── plugins/
│   │   └── tasks/
│   ├── themes/                     ← first-class themes (embedded + scaffolded)
│   │   ├── cyber_forest.json       ← the default / primary ("Refined Cyber-Ink")
│   │   ├── silt-terra-noir.json    ← warm dark earth
│   │   ├── silt-linen.json         ← clean paper
│   │   ├── silt-stark.json         ← WCAG AAA high-contrast
│   │   ├── silt-graphite.json      ← calm monochrome dark
│   │   ├── silt-bubblegum.json     ← playful coral-pink / teal
│   │   ├── silt-frost.json         ← crisp blue-tinted light
│   │   ├── silt-synthwave.json     ← 80s retro neon
│   │   ├── silt-daybreak.json       ← dark chrome + light page
│   │   ├── silt-aggie.json         ← pine green + gold
│   │   └── silt-altgeld.json       ← navy + orange prairie-fire
│   └── templates/                  ← user-authored page templates (built-ins are embedded)
│       ├── my-meeting-template.md
│       └── sprint-review.md
├── Work/                          ← Notebook
│   ├── Inbox.md                   ← Page directly under the Notebook (no section)
│   └── Projects/                  ← Section
│       ├── WebsiteRedesign.md     ← Page (single file; blocks carry per-block dates)
│       └── MobileApp.md
└── Personal/                      ← another Notebook
    └── Journal/
        └── Daily.md
```

Path resolution: the **notebook** is the top folder under the vault; the **page** is the `.md` file; the **section** is the full relative directory path between them (`""` when the page sits directly under the notebook). The filesystem location is authoritative for navigation and lifecycle operations; frontmatter is rewritten to match that canonical location rather than overriding it. Files at shallower depths (e.g. a stray `.md` directly in a Notebook folder) are still represented as section-less pages when they are directly under a notebook.

Silt starts blank — no default notebook or section is created. The user creates or opens their first notebook from the sidebar's notebook selector.

**Moving pages across sections.** A page can be dragged from one section to another, or from a section into the notebook root (section-less), via sidebar drag-and-drop. The `MovePage` IPC renames the `.md` file on disk, rewrites its `section:` frontmatter, rebuilds the block index at the new path, and updates `nav_order` for both the source and target section keys. **Name collisions are rejected** (not auto-suffixed) — if a page with the same name already exists in the target section, the move fails with a user-visible error. This matches `RenamePage` semantics and prevents silent data loss.

**Navigation identity and discovery.** Section actions use the complete
slash-separated relative path; the root group is represented by the empty
section path. `ListNavigation` recursively includes nested and empty sections.
For an accessible, trusted linked root it walks the live filesystem; when the
root is disconnected it shows the last indexed tree and preserves expanded,
pinned (YAML `favorites`), and recent locators that the incomplete fallback
cannot verify. Available-root refreshes prune locators confirmed absent from
the filesystem. The sidebar Quick Access surface shows **Pinned** pages (user
action: Pin to Quick Access / Unpin; persisted as `ui.favorites`) and bounded
timestamped **Recent** pages (full vault recent set in a scrollable Quick Access
panel; no row clamp), while the active location is available as a Notebook ›
Section › Page breadcrumb.
The dedicated page switcher searches the flattened navigation tree, ranks valid
recents first, and opens through the normal page/tab funnel. Tab overflow is
represented by an accessible menu of hidden tabs rather than silently hiding
them. Creation shortcuts and shortcut help are remappable through the hotkey
map; the read-only help surface reflects current bindings and does not steal
ordinary typing from editable or composing controls.

Page actions include in-place duplication, OS reveal, new page in the current
section, and creation of a child section under an explicit parent path.
Duplication never transfers linked content into the vault, never auto-suffixes
a collision, and mints fresh block identities while preserving canonical
Markdown and location metadata.

**Linked / external notebooks.** A notebook root does not have to live
inside the vault. The user can LINK an external folder (e.g. a synced
SharePoint/OneDrive mount) as a notebook from the sidebar ("Link External
Folder…"); it is browsed/searched/edited in place and is NEVER copied into the
vault. The linked root IS one notebook: its sections/pages live directly under
it (there is no leading notebook-name component the way there is under the
vault). Each linked notebook carries a `source` of `'linked:<id>'` (vs.
`'vault'`) so two notebooks that happen to share a name across roots cannot
collide; display names are globally unique. The link registry lives in
vault-scoped `config.yaml` (`linked_notebooks:`). Unlinking a notebook stops
indexing it and leaves its files completely untouched (vs. deleting a vault
notebook, which trashes it). See ARCHITECTURE.md §3.1 for the full model
(identity, path resolution, multi-root watcher, failure modes).

**Relocating / duplicating a vault.** The vault path set during
onboarding is not permanent: Settings → General exposes a "Move vault…" /
"Copy vault…" action on the workspace row. Both copy the entire tree (notes +
`.system/` — config, themes, templates, plugins, trash) to a destination
folder, EXCEPT the reproducible SQLite index (now in the per-user local
DataDir, not the vault; a legacy `.system/index.sqlite*` is still excluded if
present), which
is rebuilt from markdown when the destination is first opened (the documented
recovery op, §0 rule 4 — this is what makes a move safe across volumes and
avoids stale absolute paths in the index). **Move** then switches the active
workspace: it tears down services, repoints `settings.json` `vault_path` at
the new location (theme/mode preserved), and reinitializes — with a verbatim
rollback to the original path if reinit fails. **Copy** leaves the active
vault live and produces a separate workspace the user can switch into later.
The destination must be an empty, local folder (a network mount is refused —
WAL requires shared memory). Linked notebooks are external folders and are
never moved or rewritten by a vault relocation.

**Portable archive / backup.** The same Settings → General workspace
kebab also exposes **"Export vault…"** and **"Import vault…"** for backup and
machine-to-machine migration. Export bundles the entire vault tree (notes +
`.system/` — config, themes, templates, plugins) into a single self-contained
`.silt-vault` archive (a ZIP with a custom extension) carrying a `manifest.json`
+ per-entry and whole-archive SHA-256 digests; the reproducible SQLite index is
excluded (rebuilt on import, same §0 rule 4 contract as Move/Copy). Import
validates the manifest + checksums and rejects zip-slip / absolute paths / a
missing manifest before extracting into a user-chosen empty local folder, then
opens it via the `SwitchVault` path. See §3.4 for the format.


3.3 File Boundary Specification & Frontmatter Standard

Every page file contains a strict YAML metadata block bounded by triple dashes (---). This block allows indexers to map orphaned or moved files without reading the entire file directory tree:

```
---
notebook: Work
section: Projects        # optional; omit (or leave empty) for a section-less page
page: WebsiteRedesign
date: 2026-06-13
tags: [systems/specs, wails/go]
---
# Saturday, June 13, 2026

## Daily Standup Logging
- [ ] Implement parser tests [owner:: Chris] [start:: 2026-06-13] [due:: 2026-06-20] [priority:: 1] <!-- id: f1b2c3d4-e5f6-7a8b-9c0d-1e2f3a4b5c6d -->
```


3.4 Portable Vault Archive (`.silt-vault`)

A `.silt-vault` archive is the portable, self-contained form of a vault,
produced by **Export** and consumed by **Import** (Settings → General →
workspace kebab). It is the local-first contract (§0) made portable: a
single file that carries the entire vault tree and is checksummed so
tampering/corruption is detectable before a single byte is extracted.

**Container.** A ZIP with a custom `.silt-vault` extension. The vault contents
live at the archive root (e.g. `Work/Inbox.md`, `.system/config.yaml`,
`.system/themes/...`) in their on-disk layout, using forward-slash paths for
cross-platform portability. Entries are stored uncompressed (`Method=Store`) so
the archive is trivially inspectable with any unzip tool and per-entry digests
are computed over a stable byte stream; compression is a documented future
enhancement.

**Exclusion.** The reproducible SQLite index (now in the per-user local
DataDir; a legacy `.system/index.sqlite*` is still excluded if present) is NEVER
archived — identical to Move/Copy and for the same reason (§0 rule 4):
it is reproducible working memory, rebuilt from markdown when the imported
vault is first opened. Linked notebooks are external folders and are never
included in the archive.

**`manifest.json` (written last).** Carries the archive's self-description and
integrity records:

| Field | Meaning |
|---|---|
| `archive_version` | Format version (this build produces + accepts `1.0.0`; a differing version is refused on import). |
| `silt_version` | Silt version that produced the archive (diagnostic). |
| `vault_name` | Optional display name; derived from the source folder name when empty on export. |
| `created_at` | Archive creation time, RFC3339 UTC. |
| `page_file_count` | Count of `.md` page files (under notebooks, NOT `.system/` — so templates/README are excluded). The honest, cheap proxy for the issue's "block count" (a true count would require parsing every file). |
| `file_count` / `total_bytes` | Total regular files archived + their uncompressed byte sum. |
| `archive_sha256` | Whole-archive integrity root: SHA-256 over the canonical serialization of every entry record (path + size + per-entry digest). A Merkle-root-style digest (the manifest cannot hash its own raw bytes). |
| `entries[]` | Per-file records: `{path, size, sha256}` (lowercase-hex SHA-256 over the entry's uncompressed bytes). |

**Integrity model (two layers, validated before extraction).** Import mirrors
the `.silt-plugin` installer posture (§8.4):

1. **Manifest self-consistency:** recompute the root digest over the declared
   `entries[]` and assert equality with `archive_sha256` — detects manifest /
   entry-list tampering BEFORE any file is written. The archive version must
   match `1.0.0`.
2. **Per-entry verification during extraction:** stream each entry into a
   sibling temp dir through a SHA-256 hasher; the recomputed digest MUST equal
   the manifest's declared `sha256` and the byte count MUST equal the declared
   `size`, or the entry is rejected as corrupt/tampered.

Only after every entry verifies is the temp dir atomically renamed into the
user-chosen empty destination folder; a corrupt or hostile archive leaves the
destination untouched. Import then opens the vault via `SwitchVault`,
which rebuilds the index from markdown and emits `vault:moved`.

**Safety guards (defense in depth, shared with §8.4).** Rejects zip-slip
(`..` segments) and absolute entry paths, bounds the total uncompressed size
and per-entry size (zip-bomb defense via `io.LimitReader` over the declared
size), and runs a final containment check on each joined extraction path.
Hostile archives never write outside the staging directory.

**Export & import.** Export writes the active vault (read-only) to a user-chosen `.silt-vault` path via a native save-file picker; Import opens a `.silt-vault` via a native open-file picker and an empty-destination picker. Both stream determinate progress (`phase: "export"|"extract"`, current, total) so the UI renders a progress bar for large vaults.


4. Custom AST Parser & Task Shorthand Grammar

The Go backend uses a custom tokenizer layered onto a Markdown syntax tree engine to parse, match, and modify inline task properties.

4.1 Task Syntax — Dataview Inline Metadata

Silt tasks are GFM checkbox items enriched with Dataview-style inline
metadata tokens (`[key:: value]`). The `TASK` keyword is dropped — any
GFM checkbox (`- [ ]`, `- [/]`, `- [x]`) is a task. Metadata is
order-independent and extensible.

```
- [/] Critical workstream [priority:: 1] [due:: 2026-08-03] [owner:: Bob] [pin:: true] [progress:: 50] #work/sprint-4
```

Checkbox State Marker (GFM convention):

[ ] = TODO

[/] = DOING (In-Progress)

[x] = DONE

Metadata Tokens (Dataview `[key:: value]` format):

| Key | Shorthand | Format | Example |
|---|---|---|---|
| `due` | — | `[due:: YYYY-MM-DD]` | `[due:: 2026-08-03]` |
| `start` | — | `[start:: YYYY-MM-DD]` | `[start:: 2026-06-13]` |
| `owner` | `[o:: name]` | `[owner:: name]` | `[owner:: Bob]` |
| `priority` | `[p:: N]` | `[priority:: N]` (1=critical, 2=normal, 3=low) | `[priority:: 1]` |
| `pin` | `[pinned:: true]` | `[pin:: true]` (boolean) | `[pin:: true]` |
| `progress` | `[prog:: N]` | `[progress:: N]` (0-100) | `[progress:: 50]` |
| `recur` | `[recurrence:: RULE]` | `[recur:: RULE]` (natural language) | `[recur:: every week]` |
| `blocked_by` | — | `[blocked_by:: ((uuid)) …]` (one or more block refs) | `[blocked_by:: ((a)) ((b))]` |
| `created` | — | `[created:: YYYY-MM-DDTHH:MM:SS]` (ISO 8601 local, no timezone) | `[created:: 2026-08-03T09:15:00]` |
| `completed` | — | `[completed:: YYYY-MM-DDTHH:MM:SS]` (ISO 8601 local) | `[completed:: 2026-08-04T17:30:00]` |
| `modified` | — | `[modified:: YYYY-MM-DDTHH:MM:SS]` (ISO 8601 local) | `[modified:: 2026-08-04T18:00:00]` |
| `order` | — | `[order:: N]` (1-based manual sort position) | `[order:: 3]` |
| `estimate` | — | `[estimate:: DURATION]` (`Nm` / `Nh` / `Nd`; 1d = 8h work-day) | `[estimate:: 2h]` |

`created` / `completed` / `order` are nullable lifecycle caches — they round-trip through the markdown file like every other task token and are re-derived from it on re-index. The parser mints them only on genuinely-new tasks (a fresh UUID gets `created` + `order`); existing tasks are never backfilled, so an older task simply lacks the tokens until the user (or a UI action) sets them. `completed` is stamped on the DONE transition and cleared on reopen. None of the three affects block identity (still the trailing `<!-- id: uuid @ YYYY-MM-DD -->` comment).

`modified` is stamped on every successful task-line rewrite (metadata setters, status change, title/body mutate) so the hub can sort by “Recently Modified” and filter stale open work (open + missing/older-than-30-days `modified`). Comment-only appends do not stamp it. `estimate` stores a raw duration string; the index projects whole minutes (`estimate_minutes`) for sort/rollup arithmetic — empty/absent is NULL (never shown as 0).

**Comment attribution (NOTE-block tokens).** A child NOTE block indented under a TASK is that task's **comment** — the same parent/child hierarchy that powers the `comments_count` cache. Two Dataview-style tokens attribute the comment, distinct from every TASK token above:

| Key | Format | Example |
|---|---|---|
| `author` | `[author:: NAME]` (free-form name) | `[author:: Dana]` |
| `ts` | `[ts:: YYYY-MM-DDTHH:MM:SS]` (ISO 8601 local) | `[ts:: 2026-08-03T14:22:00]` |

The `author` of a comment is **distinct from the task `Owner` (the assignee)** — a task can be owned by one person and commented on by several others; the two token spaces never overlap (an `[author::]` or `[ts::]` on a TASK line, or an `[owner::]` on a NOTE line, is not picked up by the other scanner). Likewise `ts` (a full timestamp) is **distinct from the date-only `file_date`** that lives in the block-identity comment — `ts` carries wall-clock time, `file_date` carries the day the block was authored. Both are nullable: a NOTE block without the tokens has no attribution row at all (no backfill).

Task dependencies: the `blocked_by` token lists this task's prerequisites as
space-separated `((uuid))` block references (#301). The silt-tasks List,
Board, and Calendar display modes render a lock badge while any prerequisite
is unfinished and prompt for confirmation before completing a still-blocked
task (#302). Cycles are prevented at write time — adding an edge that would
close a loop (A→B→A) is rejected. Completing a blocker broadcasts
`block:changed` to every dependent so its derived "blocked" state refreshes.
The `links_count` derived cache counts `((uuid))` references in `raw_content`,
which includes dependency refs (a task carrying `[blocked_by:: ((a))]`
reports `links_count` ≥ 1).

Recurrence rules: The `recur` token carries a natural-language
repeat rule. Supported grammar: `every day`, `every weekday` (Mon–Fri),
`every week`, `every N days`, `every N weeks`, `every N months`,
`every year`. When a recurring task is marked DONE, the next instance is
generated automatically: a new TODO block with a fresh UUID and an advanced
`[due::]` date is written directly below the completed line. The next date
is computed from the current due date + interval, using skip-missed
advancement (the first strictly-future occurrence, never backfilling).
Setting a recurrence rule requires a `[due::]` anchor; clearing the token
(empty string) stops recurrence.

Tags: Standard markdown hashtags (`#work/project/milestone-one`) —
unaffected by the metadata token system.

Persistent Identifier comment: A hidden HTML comment
`<!-- id: UUIDv4 @ YYYY-MM-DD -->` automatically generated and appended
to the block by the parser if one is missing.

**Standalone tasks.** Tasks created from a quick-add surface (any silt-tasks quick-add surface — a Calendar day cell, a Board column footer, a List footer, or the global `Mod+Shift+N` shortcut) that are not attached to a note persist as ordinary GFM checkboxes in a single dedicated file at `<vault>/.silt/tasks.md`. They are indexed under a synthetic, hidden `.silt` notebook and round-trip through the same `[key:: value]` token syntax as any other task — the markdown-source-of-truth invariant is preserved, with no new SQL table. The dot-prefixed notebook is excluded from the page browser; the only user-facing surface for these tasks is the **Tasks hub**.

**Tasks hub.** A first-party plugin (`silt-tasks`) that hosts **three display modes — List, Board, and Calendar** over a single grouping-first engine. Rather than three separate plugins each with their own query path, one hub renders the same task set three ways; a segmented switcher in the header flips the projection without re-querying. The engine groups tasks across **nine dimensions** — None, Status, Owner, Priority, Due date, Tag, Notebook, Section, and Page — and sorts within a group across **eight modes** — Manual (`[order::]`), Due date, Priority, Title, Created, Owner, Recently Modified (`[modified::]`), and Estimate (`estimate_minutes`). The **List** mode is the time-horizon roll-up (the default `groupBy: dueDate` bins into Overdue / Today / Upcoming / Later / No Date, with Completed folded in), so undated tasks — the natural output of quick-add, which intentionally produces them without a default due date — get a first-class surface that the date-scoped Calendar mode does not provide. **Board** mode lays tasks out in status columns (user-editable; defaults to TODO/DOING/DONE) with drag-and-drop reassignment and manual reordering; optional per-column **WIP limits** (YAML under `columns`, status grouping only) soft-warn when a column exceeds its limit. **Calendar** mode shows tasks by start/due date with a month/week sub-layout. Direct sub-task progress is cached as `subtask_total` / `subtask_done` (direct TASK children only) and shown as `[done/total]` on rows/cards. A hub-scoped **command palette** (`tasks_command_palette`, default Ctrl+K when the hub is focused and the editor is not) switches mode/group/sort, activates saved views, and jumps to find/add task.

Any combination of display mode, grouping, sort, scope, filters, and column set can be persisted as a **saved view** under `plugins.plugin_settings.silt-tasks.saved_views[]`; three code-defined system views (Today's Board, By Owner, This Week's Calendar) are read-only and never persisted. A **unified sidebar** exposes smart lists, saved views, a mini-calendar, and filter controls that stay bidirectionally in sync with the hub header.

**Comment threads.** A child NOTE block indented under a TASK is that task's **comment** (§"Comment attribution" above carries the `[author::]` / `[ts::]` tokens). Replies are grandchild NOTE blocks (one visual nesting level in the UI; deeper nesting is flattened). The Tasks hub renders these as a thread inside the task's edit drawer; adding a comment or reply splices a NOTE through the canonical write chain. The `comments_count` cache counts all NOTE descendants under the task. The `block_meta` projection hydrates the author/timestamp on `FetchSubtree`.

Because the `.silt` notebook is hidden, any navigation that would open a `.silt/tasks` tab (search jump, tag click, backlink) routes to the Tasks hub instead, scrolling the target task into view.

4.2 Editor Input Paths

Three input paths produce the same Dataview `[key:: value]` storage
format:

1. **`%` prefix autocomplete**: User types `%` → instant popup showing
   the task metadata keys (scoped to task metadata only, unlike the
   general `/` command palette). Typing filters; selecting inserts
   `[key:: ]` with the cursor positioned for value entry, and `pin`
   auto-fills `[pin:: true]`. The key catalog lives in
   `taskMetaSuggest.ts` (`META_KEYS`: due, start, owner, priority, pin,
   progress, recur, blocked_by) — see the source for the authoritative
   list rather than enumerating it here.
2. **Task edit drawer**: `TaskEditDrawer.svelte` is the primary
   structured editor for task metadata across every task view (Board,
   List, Calendar) — opened by a single click on a task card or chip. It
   exposes status, due date, priority, owner, estimate, tags,
   recurrence, pin, and dependencies through purpose-built controls
   (radiogroups, date picker, chip input, dependency picker), all
   committing through the canonical atomic write chain.
3. **Direct typing**: Power users type `[key:: value]` directly — what
   you type is what's stored (WYSIWYG).

4.3 Task Token State Matrix

| File Plaintext State | UI Checkbox | Board column | Calendar & List views |
|---|---|---|---|
| `- [ ] ...` | Unchecked `[ ]` | "To Do" | Assigned to Due Date |
| `- [/] ...` | Half-filled `[/]` | "In Progress" | Spans Start to Due |
| `- [x] ...` | Checked `[x]` | "Done" | Stays on original date |

4.4 Indentation and Nested Hierarchies

Indentation depths are defined by hard tabs ($T_{level}$).
If a block with nesting depth $T_{n}$ resides under a block at depth $T_{n-1}$, the parser evaluates the relationship and indexes a parent-child dependency map inside the SQLite database:

- [ ] Implement AST backend core [priority:: 1] [owner:: Chris] <!-- id: parent-uuid -->
    - [ ] Write lexer token rules [priority:: 2] [owner:: Jenny] <!-- id: child-uuid-1 -->
    - [ ] Write file synchronization loop [priority:: 2] [owner:: Jenny] <!-- id: child-uuid-2 -->


SQLite mapping schema:
INSERT INTO tasks (block_id, status, owner, priority) VALUES ('child-uuid-1', 'TODO', 'Jenny', 2);

4.5 Storage-of-Truth Tiers

See ARCHITECTURE.md §0 for the full storage-of-truth contract. Summary:
task metadata (`[key:: value]`) is **file-resident user intent** — the
markdown file is the source of truth. SQLite caches derived values
(comments count, links count) and the parsed projection for query speed,
but every SQLite row is re-derivable from the markdown.

5. Smart Graph Features: Namespaces & Block Links

5.1 Hierarchical Smart Tag Namespaces

Tags in Silt leverage a slash-delimited taxonomy (`#work/project/milestone-one`) to allow for structured, recursive querying without rigid metadata forms.

When a tag is processed, the parser splits it by depth levels and indexes it into a hierarchical table. The full slash-path is stored verbatim so any depth can be queried by exact match or prefix, while the first three levels are broken out into indexed columns for the common drill-down patterns:

```sql
CREATE TABLE tags (
    block_id TEXT NOT NULL,
    raw_path TEXT NOT NULL,   -- "work/project/milestone-one" (any depth)
    level_0 TEXT NOT NULL,    -- "work"
    level_1 TEXT,             -- "project"
    level_2 TEXT,             -- "milestone-one"
    PRIMARY KEY(block_id, raw_path),
    FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
);
```

`raw_path` carries the whole tag (arbitrary depth, prefix-queryable); `level_0`–`level_2` are the indexed denormalizations that power the typical three-deep browse. This lets you view an aggregated chronological timeline of everything under `#work` at a high level, or drill straight down to items tagged `#work/project/milestone-one` — and a tag deeper than three levels is still fully addressable via its `raw_path`.

5.2 Global Block-References & Embeds

Every line block is given a unique identifier appended as a comment suffix: <!-- id: UUID -->. This allows you to easily link and reuse blocks across different notebooks.

Block Reference ((uuid)): Inline placeholder text that renders as an interactive, clickable link. Hovering over the link reveals the original block content. Clicking it centers the view directly on the source file location.

Block Embed `{{embed:uuid}}`: Renders a live, interactive portal displaying the source block inline. Edits made in the embed write back to the source block, and edits to the source block update every embed of it in real time. In the editor both `((uuid))` and `{{embed:uuid}}` render as live, interactive elements; the on-disk tokens are preserved verbatim on save so the file stays round-trip identical with any plain-Markdown tool.

5.3 Page Links (Wiki Links)

Obsidian-compatible page links use double brackets:

- `[[target]]` — link to a page
- `[[target|alias]]` — display alias
- `[[target#heading]]` — open and scroll to a HEADER whose text matches `heading`
- `[[target#heading|alias]]` — both

`target` is a vault-relative path or basename (backslashes normalized to `/`; empty section is `Notebook/Page`, never `//`). Resolution uses **shortest unique path**: exact path → unique basename → unique path-suffix. Ambiguous basenames (same page name in two places) do not auto-resolve; the chip shows an ambiguous state. Linked notebooks are disambiguated by `blocks.source`.

In the editor, `[[…]]` is an inline atomic `pageLinkNode` (`PageLinkChip`). After two non-space query characters, the typeahead calls `SearchPages` for a server-ranked, case-insensitive top 50 (exact page name, page-prefix, path-prefix, then substring; deterministic ties). On save the exact syntax is reconstructed in `clean_text` (byte-for-byte round-trip). Click navigates via `ResolvePageLink` + `navigate-to-page`. Tab context menu **Copy Page Reference** emits `[[shortest-unique-path]]`; **Copy Page Path** remains the plain path string.

A derived `page_links` reverse index (rebuilt on re-index; FK cascade from `blocks`) powers rename/move rewrite: `RenamePage` / `MovePage` / `RenameSection` rewrite inbound `[[old…]]` → `[[new…]]` while preserving `|alias` and `#heading`. Block UUIDs are never rewritten. `![[embed-page]]` and `#^block` are out of scope for v1 (block identity stays `((uuid))`).

5.4 Tag Typeahead

Typing `#` at block start opens a typeahead listing tags from the index. The list is seeded with `recent_tags` (an MRU list maintained by `RecordTagUsage`, capped at 12) above the full tag set, so frequently-used tags are always one keystroke away. Selecting a tag inserts it as `#ns/sub/leaf` markdown syntax and records its usage. The typeahead filters case-insensitively by prefix as the user types after `#`.

5.5 Block Reference & Embed Typeahead

Typing `((` opens a block-reference picker listing indexed blocks (filterable by content). Selecting inserts `((uuid))` as an inline atomic `blockReferenceNode`. An embed picker for `{{embed:uuid}}` is not currently implemented; existing embed tokens are still parsed and rendered as block-level atomic `embedNode` values and resolve via `ResolveBlockReference`.

5.6 Backlinks Panel

Every page has a paged backlinks surface showing inbound references — `[[…]]`
page-links, `((uuid))` block references, and `{{embed:uuid}}` embeds — grouped
by source page with kind badges and clean-content snippets. The panel refreshes
automatically on content changes (debounced) and supports click-to-navigate
to the linking page or the specific block. The panel itself resolves lazily
at panel-open time (it queries the index, not the file system), but the
block-reference and embed edges are eagerly materialized during file indexing
into the `block_references` reverse lookup so panel-open cost is proportional
to inbound edge count rather than total block count. Source-aware so linked
notebooks contribute backlinks correctly. The panel loads additional result
pages explicitly, which bounds its initial payload and rendered projection.
See ADR `docs/decisions/0006-backlinks-query-strategy.md`.

6. User Interface Specification

6.1 Color Palette & Dark-Mode Aesthetics

To minimize eye strain and maintain professional focus, Silt utilizes a high-contrast dark aesthetic with deep visual depth:

Base Canvas: #121214 (Deep slate black)

Sidebar & Workspace Panels: #161619 (Solid dark charcoal)

Borders, Guidelines, & Rules: #27272a (Crisp zinc gray)

Primary Text: #e4e4e7 (Light warm gray)

Muted Text & Metadata: #8b8b94 (Medium cool gray)

Active Highlights & Guideline Markers: #2dd4bf (Refined teal, 400-shade)

6.2 Visual Guideline Path Highlights

For nested lists, Svelte tracks the active cursor focus and dynamically highlights the current hierarchy path. Vertical guide rules align to the indentation columns. Selecting a nested bullet changes the color of its ancestral parent guidelines from #27272a to #2dd4bf, providing instant visual context within deeply nested structures.

  - Root Node Focus
  |   - Sub-Level Node
  |   |   - Active Cursor Selection Bullet Point  <-- Guideline columns are colored teal
  |   - Unfocused Parallel Bullet Point           <-- Guideline column is colored dark gray


6.3 Contextual Keyboard Command Palette (Slash Menu)

Typing the / trigger key on an empty block opens a contextual command menu directly beneath your cursor. You can search, filter, and apply commands using only your keyboard:

Action Trigger

Action Result

/todo

Automatically appends `- [ ] ` (empty GFM checkbox) and triggers the `%` metadata autocomplete so the user can add owner, due date, priority, etc.

/today

Injects today's date formatted as YYYY-MM-DD.

/embed

Displays a search modal of indexed blocks to select and embed.

/h1

Transforms the active block into a first-level markdown header (# ).

/calendar

Opens the Date Glance month popover to pick a date to insert at the cursor (or copy to the clipboard when no editor target is available). Unlike `/today`, it does not insert immediately.

/shortcuts

Displays the keyboard-shortcut reference overlay, listing the current live bindings (disabled or unbound shortcuts are omitted).

6.4 Theme Customization Engine

To prevent styling stagnation, Silt provides a built-in user theme engine mapping to CSS Custom Properties. The shipping schema is **Theme System v2**; the forward architecture — OKLCH derivation, the 9-zone surface model, the unified background system, the contrast guarantee, and the custom-editor contract — is specified in [`docs/theme-system-v2-rfc.md`](docs/theme-system-v2-rfc.md), and the decision to make v2 the only supported schema with no v1→v2 migration is recorded in [`docs/decisions/0002-theme-schema-v2-no-migration.md`](docs/decisions/0002-theme-schema-v2-no-migration.md). The internal pipeline is documented in ARCHITECTURE.md §4.4; the design-system token vision lives in DESIGN.md §2.1 / §7.

**Theme files** live as canonical modes-based JSON in `<vault>/.system/themes/`. Each theme carries `schema_version: "2.0.0"` (hard-enforced — any other value is rejected), `id`, `name`, an optional theme-level `typography` section, and a `modes.dark` / `modes.light` token set. A mode defines:

- **`surfaces`** — 9 named zones (`app, sidebar, editor, panel, card, modal, popover, titlebar, activitybar`), each `{bg, border, text}`. Only `app` is required; the rest inherit from their parent zone (`popover→modal→panel→app`; `sidebar`/`editor`/`titlebar`/`activitybar`→`app`; `card→panel`) when omitted, so a minimal theme authors one canvas and the rest follow. Each zone MAY also carry a unified `background` block (`image` / `size` / `opacity` / `blend` / `position` / `scrim`) that subsumes the legacy `texture` overlay and powers per-zone background photos.
- **`accent`** — two hue-agnostic **semantic** accents (`primary` = the "go / done" hue, `secondary` = the "in progress" hue), each `start` / `end` / `glow` / `on` (label ink for solid fills using `start`). When `on` is omitted, Flatten derives black/white from `start` luminance. Semantic CSS alias `--color-text-on-accent` = primary `on`. Components reference only the semantic names; each theme maps its concrete hues onto them.
- **`status`** — `warn` / `danger` / `success` (all three required).
- **`error`** — a themeable family (`fg` / `bg` / `border`) replacing the static Material-3 error pink that used to render wrong in every dark theme; `status.danger` (destructive actions) and `error.fg` (validation / invalid input) are deliberately distinct.
- Zone-agnostic interaction tokens (`hover`, `active`, `border_active`, `border_focus`) and emphasis levels (`text_muted`, `text_disabled`) that apply on every surface.
- Optional **`radius`** / **`spacing`** / **`shadow`** geometry ramps, an **`editor`** interaction block (caret / selection / link / highlight), and a theme-level **`typography.scale`** (sizes / line-heights / weights). A theme that omits them renders with v1-equivalent geometry and type via emitted defaults. The `editor.caret` token carries a documented contrast contract: it must meet **≥4.5:1 against `--color-surface-editor`** in each mode (a thin 1–2px element needs the text-equivalent bar, not the 3:1 UI minimum). Verification uses the flatten-golden values as the resolved-color source; darkening the value (lower lightness, preserve hue) is the adjustment direction. This is a documented contract, not a committed CI gate.

Color slots accept `#hex`, `rgb()` / `rgba()`, and `oklch(L C H[/ A])`; OKLCH is what lets a theme derive perceptually-uniform hover/active/disabled variants and what lets the CI contrast gate reason exactly. The v1 flat `bg` model, the `chrome` block, and the `texture` block are removed — the `sidebar`, `titlebar`, and `activitybar` zones replace chrome, and the per-zone `background` replaces texture.

**Default & first-class themes.** The app embeds a guaranteed-correct default (`cyber_forest`) plus a first-class set (Cyber Forest, Terra Noir, Linen, Stark, Graphite, Bubblegum, Frost, Synthwave, Daybreak, Aggie, Altgeld) so it always has a fallback — before a vault exists, when the themes directory is empty/wiped, and when the active id is missing or invalid. The full first-class roster is always selectable (on-disk copies win on id collisions), and a non-default active theme is resolved from the embedded set even when it is not on disk, so it never flashes the default palette.

**Mode & switching.** Each theme carries both dark and light token sets; the user picks Dark, Light, or System (System follows the OS preference, resolved locally). Switching applies live with no restart and no flicker, and the pre-CSS paint already uses the active theme's background colour so there is no first-paint flash of the wrong palette. The active theme id + mode persist across restarts in user-global settings — they must be known before any vault is open.

**Import & export.** Users can import a theme from a JSON file (file picker or drag-and-drop) and export the active theme for round-trip editing. Imported themes are validated against the canonical v2 schema: only `#hex` / `rgb()` / `rgba()` / `oklch()` color values are accepted at every token slot (named colors, `hsl()`, `url()` at color slots, `<script>`, `expression()` are rejected before the file is written), and font-family / background-image values reject CSS-breaking characters. `schema_version` must be exactly `"2.0.0"` — a v1 file is rejected with a clear, field-level error (there is no migration path; re-author it as v2). Imported ids are sanitized and namespaced so they cannot collide with built-ins. Validation errors surface per-field so the UI can name the offending token and the expected format. A successfully imported theme is immediately selectable without a restart.

**Contrast guarantee.** A CI gate asserts WCAG AA (4.5:1 text, 3:1 UI) for every first-class theme in both modes; Stark is held to AAA (7:1). A theme that fails perceptual contrast will still import (validation checks structure/format, not contrast) but will be hard to read.

**Settings → Appearance** is the single surface for theme selection and customization — an accessible **card grid + details pane** with a two-stage preview (hover highlights only; click stages a workspace-wide preview; Apply/double-click commits; Revert/Esc restores), plus the mode toggle, import, export, and **Customize**. Mode is a `radiogroup` of Dark / Light / System with roving tabindex. Status and errors render in an `aria-live` region (escalating to `alert` on errors). The same theme engine drives the whole shell, including the custom titlebar.

**Custom theme editor.** Users open **Customize** on a theme to edit a working copy with progressive disclosure (Simple ~5 high-impact controls; Advanced groups by intent: Surfaces, Color & accent, Typography, Geometry, Editor, Background). Live preview re-flattens and injects CSS variables into the real app chrome (no reload). Built-in presets are never mutated in place — Save always forks to a new on-disk custom theme; disk customs can overwrite or Save as new. Background images stage without writing the theme until Save. Contrast feedback is informational (pass/warn/fail + optional OKLCH-lightness auto-fix) and **never blocks Save**. Interaction design is specified in [`docs/theme-v2-ux.md`](docs/theme-v2-ux.md); the token contract remains the RFC.

Schema Example (cyber_forest.json, dark mode shown — only the zones the theme authors are listed; `sidebar`, `editor`, and `popover` inherit from their parents):

```
{
  "schema_version": "2.0.0",
  "id": "cyber_forest",
  "name": "Cyber Forest",
  "author": "Chelydra Labs",
  "description": "...",
  "typography": {
    "font_family": "'Plus Jakarta Sans', sans-serif",
    "mono_font_family": "'JetBrains Mono', monospace",
    "headline_font": "'Hanken Grotesk', sans-serif"
  },
  "modes": {
    "dark": {
      "surfaces": {
        "app":   { "bg": "#0c0c0e", "border": "#1e1e23", "text": "#dee3e6" },
        "panel": { "bg": "#121215", "border": "#27272a", "text": "#dee3e6" },
        "modal": { "bg": "#121215", "border": "#3f3f46", "text": "#dee3e6" },
        "card":  { "bg": "#161619", "border": "#27272a", "text": "#dee3e6" }
      },
      "hover": "#1c1c21", "active": "#222226",
      "border_active": "#3f3f46", "border_focus": "#52525b",
      "text_muted": "#8b8b94", "text_disabled": "#4b5563",
      "accent": {
        "primary":   { "start": "#2dd4bf", "end": "#0d9488", "glow": "rgba(20, 184, 166, 0.15)" },
        "secondary": { "start": "#6366f1", "end": "#a855f7", "glow": "rgba(168, 85, 247, 0.12)" }
      },
      "status": { "warn": "#fbbf24", "danger": "#f43f5e", "success": "#22c55e" },
      "error":  { "fg": "#f43f5e", "bg": "#121215", "border": "#3f3f46" }
    },
    "light": { "...": "..." }
  }
}
```


6.5 Page Template Engine

Silt provides a full page template system: a built-in library of first-class templates (Notes, Meeting Notes, Standup, Daily Note, Project Brief, 1-on-1, Weekly Review, Decision Log/ADR, Reading Notes, Retrospective), user-extensible custom templates, and the UI/IPC surface to insert them as a new page or into the current page at the cursor. Templates are parameterized Markdown — a title, category, icon, optional placeholder list, and a Markdown body using `{{name}}` placeholder tokens.

Template Files: Parsed dynamically from Markdown files inside `<vault>/.system/templates/`. Each carries a `schema_version`, `id`, `title`, `category`, optional `icon`, optional `placeholders` list, and a Markdown body. The placeholder syntax is `{{name}}` (not Go template syntax) — a small substitution renderer resolves built-in defaults (`date`=YYYY-MM-DD, `time`=HH:MM, `iso_date`=ISO 8601, `weekday`=full weekday name) and user-declared/caller-supplied variables. Unknown placeholders warn (forward-compat), never error.

Smart Graph Compatibility: the placeholder grammar (`^[a-z][a-z0-9_]*$`) structurally excludes Smart Graph syntax — `{{embed:uuid}}` (colon) and `((uuid))` (parentheses) pass through the renderer byte-for-byte, so templates can contain embeds and references that resolve normally on load (§5.2).

**Default library.** The full first-class set is embedded so templates are always available — before a vault exists, when the templates directory is empty, and on existing vaults. Built-ins are read-only; user templates are writable (`<vault>/.system/templates/<id>.md`). On-disk templates win on id collisions with a built-in.

**Resolution & insertion.** Templates are resolved from on-disk + embedded (deduped, sorted by Category then Title) and presented in a picker. External edits to the templates directory hot-reload. Inserting a template produces real Silt blocks — ordinary GFM checkboxes (`- [ ]` and `- [x]`) become tasks and flow into the Tasks hub; Silt's in-progress `- [/]` marker is a task too. Embeds/references resolve, and blocks get fresh UUIDs. Templates are vault-scoped Markdown, read-mostly.

**Forward compatibility.** `schema_version` is informational (a forward-versioned template keeps loading); the `Source` field has three tiers — `builtin` (embedded, read-only), `disk` (user-authored, writable), and `plugin` (runtime-registered by a plugin); categories are additive (unknown categories warn, never reject); and new built-ins land as a single `.md` file with no engine change.

**Plugin templates.** Plugins may register templates at runtime. Plugin templates are grouped under a `Plugins / <plugin-id>` header and deduped last (on-disk > embedded > plugin), so a plugin cannot shadow a first-class or user template. A plugin may register up to 100 templates.

**Template management.** Settings provides a template-management surface backed
by the same template catalog: users can create blank or current-page-seeded
templates, edit and validate user templates, duplicate any readable template
as a new user-owned template, and delete user templates. Built-in and plugin
templates are immutable; saving a fork never changes the source template.
External edits refresh the catalog without a restart, and failed saves or
deletes preserve the current list and any in-progress draft.


7. Reliability, Protection, & Performance Targets

7.1 Atomic Staging & Overwrite Protocol

Because your notes are stored directly on disk, Silt must guarantee data safety during unexpected app exits, power failures, or system crashes. The Go file-writing engine never directly modifies an active file. Instead, it follows a strict atomic update sequence:

[InMemory Modified Block Buffer]
              │
              ▼
1. Create Scratch File: ".2026-06-13.md.tmp"
              │
              ▼
2. Flush Buffer to Disk: Call OS file.Sync()
              │
              ▼
3. Atomic Overwrite: Call OS os.Rename(".2026-06-13.md.tmp", "2026-06-13.md")


If a system crash occurs mid-write during steps 1 or 2, the current file on disk remains completely untouched and uncorrupted.

7.2 Non-Functional Performance Thresholds

Startup Ingestion: The parser must boot, scan, token-analyze, and index a directory containing 1,000 markdown files into the SQLite database in under 450ms.

UI Frame Budget: To keep typing smooth, Svelte must complete inline shorthand processing and DOM updates within a 16ms render window (maintaining a locked 60 FPS).

Memory Footprint: The application must maintain an idle memory footprint of less than 65MB RAM, ensuring Silt remains a lightweight utility running in your system tray.

7.3 Installation & Distribution Requirements

The Windows NSIS installer MUST satisfy the following:

- **Per-user, no-admin installation:** Silt installs per-user only — no administrator access is ever required and no UAC prompt appears. The install directory is `%LOCALAPPDATA%\Programs\Chelydra Labs\Silt`. Each Windows user who wants Silt installs their own copy.
- **Upgrade support:** Installing a newer version over an existing installation MUST upgrade in place. The installer detects a prior per-user install (via the HKCU registry uninstall key), silently runs the old uninstaller, then installs the new version to the same directory.
- **Registry correctness:** Uninstall registry entries (Add/Remove Programs) are written to HKCU, so the install appears correctly in Windows Settings for the current user.
- **User data preservation:** The vault (notebooks, config.yaml, plugins, themes, templates) lives in user-chosen directories, NOT in the install directory. Upgrading or uninstalling never touches user data.
- **Portable alternative:** A portable .zip (no installer, no registry entries) is also produced for users who prefer a zero-install experience.

8. Local-First Plugin Architecture

To support core system extension while retaining a lightweight base engine, Silt abstracts all dynamic dashboards—including the unified silt-tasks hub—into explicit plugins. The host application acts strictly as a raw block editor, tree compiler, and IPC router.

                  +--------------------------------+
                  |      Silt Core Editor       |
                  +--------------------------------+
                                  │
                                  ▼
                  +--------------------------------+
                  |     silt-tasks Plugin          |
                  |     (First-Party)             |
                  | List · Board · Calendar       |
                  +--------------------------------+


8.1 Runtime Sandboxing and Lifecycle

Frontend Modules: Svelte dynamically imports plugins at boot time. Plugins are written as independent ESM (ECMAScript Modules) and reside in Notebooks/.system/plugins/{plugin-name}/index.js.

Backend Hooking Structure: Plugins communicate with the Go backend via standard JSON-RPC bridges using Wails events. They obtain read/write query privileges targeting the local SQLite database.

8.2 Host-Plugin API Specification (Frontend)

Plugins run as native ES modules. First-party plugins with navigable views ship as compiled Svelte components bundled with the app; headless first-party providers may omit a view. Third-party plugins live in `.system/plugins/<id>/index.js` and are loaded at boot (native ESM via a blob URL so Vite does not resolve them at build time). Both kinds receive the same PluginContext:

```ts
export interface PluginContext {
  activeNotebook: string;
  activeSection: string;
  activePage: string;
  /**
   * UI location snapshot (identifiers only): active page triple, optional
   * focused/selected block id, and open tabs. Never includes full page bodies.
   */
  getUiLocation: () => {
    notebook: string;
    section: string;
    page: string;
    blockId?: string;
    openTabs: Array<{
      notebook: string;
      section: string;
      page: string;
      preview?: boolean;
      active: boolean;
    }>;
  };
  /** Today's date in the user's LOCAL timezone as YYYY-MM-DD. */
  today: string;
  // Read-only SQL against the in-memory index (SELECT / WITH only).
  sqliteQuery: (sql: string, params?: unknown[]) => Promise<Record<string, unknown>[]>;
  mutateBlock: (id: string, text: string) => Promise<boolean>;
  updateBlockState: (id: string, status: 'TODO' | 'DOING' | 'DONE') => Promise<boolean>;
  /** Update per-task metadata (pin, progress) — file-resident user intent. */
  updateTaskMeta: (id: string, meta: { pinned?: boolean | null; progress?: number }) => Promise<boolean>;
  /**
   * Resolve this plugin's settings for the ACTIVE notebook, applying the
    * co-located per-notebook override layer. Vault → vault settings;
   * linked → deep-merge of vault defaults with the linked notebook's
   * co-located config (linked wins per-key). Re-read on every call.
   */
  getPluginSettings: () => Promise<Record<string, any>>;
}

export interface SiltPlugin {
  manifest: { id: string; name: string; version: string; icon?: string };
  init?: (ctx: PluginContext) => void;
}
```

The active `notebook/section/page` from the navigator is bound into the context as LIVE reactive getters; reading them inside a Svelte reactive context (template, `$derived`, `$effect`) tracks navigation changes automatically. `sqliteQuery` is read-only (anything other than SELECT/WITH is rejected). `getPluginSettings` resolves per-active-notebook so a plugin rendering for a linked notebook sees the co-located overrides; writes still persist to the vault config via `updatePluginSetting`. See `docs/PLUGIN_DEVELOPMENT.md` for the full author guide.

**v2 SDK.** The PluginContext was expanded with: a capability/permission model (`capabilities` in the manifest, per-vault grants in `config.yaml`); lifecycle hooks (`onVaultOpen`/`onVaultClose`/`onShutdown`); a typed event bus (`ctx.on`); content CRUD (`createBlock`/`deleteBlock`/`moveBlock` + page/section/notebook CRUD); file I/O (`readFile`/`writeFile`/`deleteFile`/`listDir` + scratch space); OS integration (`openInNativeHandler`/`openUrl`/pickers/clipboard/notify); network/fetch (Go-side proxy, `network` capability-gated); editor extension points (slash-command registry + generic `embedBlock` node); rendered UI surfaces (sandboxed iframe + postMessage bridge); a declarative settings schema (`settings` in the manifest, generated UI); and the AI surface (`ctx.ai.complete` / `ctx.ai.embed`). The AI surface carries optional `tools` / `tool_choice` and returns `tool_calls` (`PluginAIToolDef` / `PluginAIToolChoice` / `PluginAIToolCall`); `messages` supports a `tool` role (with `tool_call_id` / `tool_calls`) for multi-turn tool use. Every privileged binding is gated server-side by `requireGrant`; `exec` is deferred. See `docs/PLUGIN_DEVELOPMENT.md` §8 for the full surface.

**AI tool-calling contract (#595).** The chat surface is provider-agnostic: the host maps the unified `tools` / `tool_choice` onto each provider's wire shape (OpenAI-compat functions, Anthropic `tool_use`, Google `functionDeclarations`) and decodes the model's tool invocations back into a single `tool_calls` result. The exact types a plugin consumes:

```ts
export interface PluginAIToolDef {
  name: string;
  description?: string;
  parameters: Record<string, unknown>; // JSON Schema (lowercase type strings)
}

export interface PluginAIToolChoice {
  mode: 'auto' | 'required' | 'none' | 'force';
  toolName?: string; // set when mode === 'force'
}

export interface PluginAIToolCall {
  id: string;            // correlates the tool-result message with this call
  name: string;
  arguments: Record<string, unknown>; // raw JSON object (unwrapped from
}                        // OpenAI's stringified form by the host)

// PluginAICompleteRequest gains optional tools + tool_choice
interface PluginAICompleteRequest {
  messages: PluginAIChatMessage[];
  tools?: PluginAIToolDef[];
  toolChoice?: PluginAIToolChoice;
  responseSchema?: Record<string, unknown>;
  // ...model/temperature/max_tokens/stream fields
}

// PluginAICompleteResult gains tool_calls (content may be empty when only
// tool calls were emitted)
interface PluginAICompleteResult {
  content: string;
  toolCalls?: PluginAIToolCall[];
  // ...model/usage/stream_id fields
}

// PluginAIChatMessage supports role 'tool' for multi-turn tool results:
// an assistant turn may carry tool_calls; a 'tool' turn carries tool_call_id
interface PluginAIChatMessage {
  role: 'system' | 'user' | 'assistant' | 'tool';
  content: string;
  toolCalls?: PluginAIToolCall[];   // on assistant turns
  toolCallId?: string;              // on 'tool' turns (correlates the call)
}
```

For streamed runs, in-progress tool-call fragments arrive via the
owner-scoped `ai:complete:tool-delta:<pluginID>` event (indexed by call,
arguments concatenated across chunks) alongside `ai:complete:delta:<pluginID>`
(#635). Destructive plugin tools (e.g. `silt-ai-agent`'s `rename_tag`) stage
behind a single-use confirmation token rather than executing directly — see
`docs/plugins/silt-ai-agent.md`.

**Unified AI interaction + enablement (#632).** Silt presents AI chat through
one right-side **Silt AI** drawer. Product enablement is `ai.features`
(Settings → AI): master enable, semantic search (RAG), note summaries — not
four independent Plugins toggles. The drawer accepts typed contributions for
conversation text, evidence and citations, tool activity, reviewable proposals,
staged confirmations, and structured status. AI-capable plugins are headless
capability providers loaded from feature flags; the agent loop provides default
orchestration when AI is enabled.

8.3 Core Feature Decoupling

**Content mutation is capability-gated.** Block-level content mutation (creating, deleting, or moving blocks) requires the `content-mutate` capability. First-party plugins are implicitly granted; third-party plugins must declare it in their manifest. Structural operations (page/section/notebook CRUD) are not gated — they are structural, not content changes. I/O-bound operations (files, network, OS, clipboard) are likewise capability-gated.

**Plugin network fetch is rate-limited.** A network-capable plugin's fetches are throttled per plugin (default 1 request/sec, burst 10). A plugin may request a higher limit via a manifest field:

```json
{
  "ratelimit": { "rps": 5, "burst": 20 }
}
```

`rps` must be > 0 and ≤ 10; `burst` must be > 0 and ≤ 100. Out-of-range values are rejected at install; hand-edited manifests are clamped at runtime as defense in depth.

**The first-party dashboards are plugins.** The unified silt-tasks hub uses the exact same SDK as any third-party plugin — the UI contains no privileged custom code for it. The hub hosts three display modes over one grouping-first engine, all scoped to the active navigation level (vault / notebook / section / page), selectable from the hub header:

- **List** — a time-horizon roll-up (Overdue / Today / Upcoming / Later / No Date), carrying unfinished tasks into the current day and giving undated tasks a first-class surface.
- **Board** — drag-and-drop status columns; a status change writes the new checkbox state to the source markdown and re-indexes the block, and cards support manual reordering.
- **Calendar** — tasks by start/due date with interactive timeline components and a month/week sub-layout.

**Page-scoped Tasks Hub intent.** Page chrome may open the existing hub for a
specific source-qualified page using a locator containing the source,
notebook, section, page, and a per-entry nonce. This is an ephemeral session
intent: it overlays page scope and consumer-provided display defaults while
leaving the ambient hub state and saved views unchanged. Intentional scope or
filter changes exit the page context.

All three modes remain projections of canonical Markdown task blocks through
the existing read-only index and shared task query path. Page routing does not
create a meeting entity, a separate board, another task store, or persisted
route state. Source is retained when opening a task's exact source page.

8.4 Plugin Packaging & Distribution (.silt-plugin)

Third-party plugins are distributed as `.silt-plugin` archives — a **ZIP with a custom extension** containing `plugin.json` + the entry module (`index.js`) + optional assets, all at the archive root:

```
plugin.json   { "id": "my-plugin", "name": "My Plugin", "version": "1.0.0", "main": "index.js" }
index.js      native ESM exporting { manifest, init(ctx) }
```

- **Validation:** on install, the manifest schema is checked (`id` must match `^[a-z0-9-]+$`, required name/version, entry module present); absolute paths, `..`, and zip-slip entries are rejected.
- **Install:** atomic extract into `.system/plugins/<id>/` (staged in a temp sibling dir, then renamed); refuses to overwrite an existing id. A newly installed plugin is immediately available without a restart.
- **Enable/Disable:** a `.disabled` sentinel file inside the plugin folder (the loader skips disabled plugins) — avoids fragile config.yaml edits. Discovery is folder-based, so install "just works" without editing config.
- **Uninstall:** removes the plugin folder (id sanitized + within-vault check).
- The in-app **Plugin Manager** (titlebar extension icon) drives validate → preview → install, plus per-plugin enable/disable and uninstall.
- First-party plugins (silt-tasks, silt-attachments) are always available (bundled) regardless of `.system/plugins/` contents.

8.5 Attachments Plugin Convention

The `silt-attachments` plugin lets users attach arbitrary files to notes.

- **File placement:** Files are copied into `<notebook>/attachments/` (visible placement, per the data-scoping principle). The `attachments/` directory is excluded from the scanner (`WalkMarkdown`), the sidebar navigator (`ListNavigation`), and the fsnotify watcher, so it never appears as an empty section and binary files are never indexed.
- **Markdown convention:** Images use standard `![alt](attachments/foo.png)` syntax. Non-image files are serialized as an HTML-comment marker `<!-- silt-embed: {"embedType":"attachment","src":"attachments/foo.pdf",...} -->` that round-trips byte for byte through the parser.
- **Open in native handler:** Activating an attachment embed block opens the file in the OS default handler (Preview / Adobe / `xdg-open` / etc.), not in-app. The path is resolved against the notebook's actual root (in-vault or linked).
- **Task-block travel:** An attachment embedBlock inserted as a CHILD of a task block (indented under it) automatically travels with its parent when the task is reordered on the Board. This is inherent to the block hierarchy — no explicit association model is needed.
- **Copy-in semantics:** The source file is copied (not linked/moved) into `attachments/`. Filename collisions are resolved with a counter suffix (`report-1.pdf`, `report-2.pdf`). A 100 MB size limit and an executable filetype blocklist (`.exe`, `.bat`, `.sh`, etc.) prevent the attachment folder from becoming an unbounded executable drop zone.

8.6 Per-plugin SQLite Store

Each plugin MAY carry its own SQLite file at
`<vault>/.system/plugins/<id>/data/plugin.db`, opened lazily on a **distinct**
connection from the core index (relocated to the per-user DataDir at
`<DataDir>/silt/indexes/<vault-key>/index.sqlite*`). This is the
**plugin-owned storage tier**: the plugin owns its schema and chooses
durability semantics — working memory *or* durable storage at its discretion.

- **Capability:** gated by the `plugin-db` capability (`ctx.pluginDb.exec` /
  `query` / `migrate`); first-use prompted like the other v2 capabilities.
- **sqlite-vec:** the connection has `sqlite-vec` registered, exposing `vec0`
  virtual tables and `vec_distance_cosine` / `vec_distance_L2`. Used by the AI
  Q&A plugin for vector indexes and by the summary plugin for
  content-hash caches.
- **Boundary with core:** the plugin DB is never `ATTACH`-able to the core
  index (and vice-versa). Cardinal rule #4 (SQLite is working-memory-only)
  governs the **core index only**; the plugin DB is a separate, plugin-owned
  tier.
- **Durability guidance:** data that must survive uninstall or be portable
  across vaults MUST round-trip through markdown; plugin-private caches
  (embeddings, hashes, agent memory) may live only in the plugin DB.
- **Lifecycle:** the connection is closed on `teardownPlugin(id)` and on vault
  close; the file is deleted on uninstall (the whole `.system/plugins/<id>/`
  folder is removed).
- See ARCHITECTURE.md §0 (rule 4 plugin carve-out) and ADR
  `docs/decisions/0001-plugin-storage-tier.md`.

8.7 Bespoke Plugin Settings Pages

A plugin with non-trivial configuration may declare a **bespoke Settings page**
instead of the generic `SettingSchema[]` form.

- **Manifest:** a plugin declares *either* a bespoke settings page *or* the
  generic `settings` schema — not both (single source of truth per plugin).
  Migrating generic → bespoke is supported (old `plugin_settings.<id>.*` keys
  remain valid).
- **Rendering:** first-party plugins render a compiled Svelte component;
  third-party plugins render via the sandboxed `settings-panel` iframe surface.
  The page appears as a dynamic tab in the Settings shell; disabled plugins'
  tabs are hidden.
- **Persistence:** bespoke pages call the existing `updatePluginSetting` /
  `getPluginSettings` plumbing — no new storage path.

8.8 `note-banner` Plugin Surface

A new `SurfaceKind = 'note-banner'` — a dismissible highlight region mounted at
the top of the note view (above the TipTap editor content).

- **Rendering:** banners render in registration order; first-party as a
  compiled Svelte component, third-party via the iframe bridge. Stacking is
  predictable (order, `max-height`, overflow) so several banners coexist.
- **Dismissal:** each banner exposes a close affordance; the host sends a
  host→iframe `dismiss` event so the plugin can persist dismissal state
  (recommended: `updatePluginSetting('<id>', 'dismissed_notes', [...])` —
  `updatePluginSetting` is proxied through the surface bridge). The host
  removes the surface after a short grace window regardless of plugin
  response. When more than two banners stack, they collapse into a single
  expandable summary.
- **Transient chrome:** banners do not capture editor focus on mount and are
  removed cleanly on `teardownPlugin`.

9. Editor Format & Grammar

## Markdown Dialect

**Silt's on-disk base dialect is GFM (CommonMark + GFM).** Markdown files are the
source of truth and must render correctly on GitHub, in Obsidian, in VS Code
preview, and through Pandoc — only GFM satisfies all four. Pandoc is a
**downstream converter**, not a dialect: any user can run `pandoc -f gfm` to
publish a note to LaTeX/PDF/Word with zero format change.

**Silt Markdown** = GFM base + a documented set of app-specific extensions that
already live on disk today:

- Obsidian callouts (`> [!variant]`)
- Dataview-style inline metadata (`[key:: value]`)
- Smart Graph block references (`((uuid))`) and embeds (`{{embed:uuid}}`)
- Block-identity comments (`<!-- id: uuid @ YYYY-MM-DD -->`)
- `@[mentions]`
- Sub/super: `<sub>`/`<sup>` HTML tags (NOT Pandoc's `~x~`/`^x^`, which render
  as literal text on GitHub). Silt already emits the HTML form.
- Math (`$x$` / `$$x$$`) — de-facto on GitHub/Obsidian
- Footnotes (`[^1]`) — de-facto on GitHub since 2021

Sub/super uses HTML `<sub>`/`<sup>` (GFM reserves `~` for strikethrough,
so Pandoc's `~sub~`/`^sup^` is not GFM-compatible). The `Ctrl+Shift+,` /
`Ctrl+.` hotkeys toggle these marks.

## Inline Formatting

Silt supports nine inline marks, block-level alignment, text/background color,
and a source/edit view toggle. All formatting is additive to `clean_text` —
the Go parser treats formatted text as opaque.

### Inline marks (on-disk syntax)

| Mark | Syntax | Example |
|---|---|---|
| Bold | `**text**` | `**bold**` |
| Italic | `*text*` | `*italic*` |
| Strikethrough | `~~text~~` | `~~struck~~` |
| Inline code | `` `text` `` | `` `code` `` |
| Highlight | `==text==` | `==highlighted==` |
| Underline | `<u>text</u>` | `<u>underlined</u>` |
| Subscript | `<sub>text</sub>` | `H<sub>2</sub>O` |
| Superscript | `<sup>text</sup>` | `E=mc<sup>2</sup>` |
| Link | `[text](url)` | `[docs](https://x.com)` |

Marks nest freely. Code shields its content from further parsing.

### Block-level alignment

NOTE and HEADER blocks support `left` (default), `center`, `right`, `justify`.
Alignment is persisted as a trailing HTML comment: `text <!-- silt-align: center -->`.
TASK blocks do not support alignment.

### Text color and highlight

Text color (foreground): `<span style="color: #hex">text</span>` — its own mark.

Highlight (background) is one mark with two forms:
- Default `==text==` — the themed highlight tint (no color attribute).
- Colored `<span style="background-color: #hex">text</span>` — the same mark with a color set.

Default and colored highlight are mutually exclusive on a given span (one mark;
the color attribute is either null or a hex value). Both round-trip verbatim
through `clean_text`; the Go parser preserves `==` and the HTML span unchanged.
A 12-color theme-aware palette is available via the format toolbar; the default
tint is also applied by the Highlight toolbar button and `Mod+Shift+H`.

### Heading levels

`# H1`, `## H2`, `### H3`. Convert blocks via Mod-Alt-1/2/3/0/4 or slash
commands `/h1` `/h2` `/h3` `/note` `/task`.

### View mode toggle

Per-page Edit (WYSIWYG) ↔ Source (raw markdown) toggle. The toggle is a
floating icon button in the editor's action bar (announced via `aria-pressed`
+ `aria-keyshortcuts`) plus the `toggle_view_mode` hotkey (default
`Ctrl+Shift+V`, remappable per-vault). Source view is **editable** raw
markdown: the buffer seeds from the on-disk page body (`FetchPageMarkdown`),
debounced writes go through `SavePageMarkdown` (frontmatter preserved, atomic
write + re-index), and dirty buffers prompt Keep mine / Reload on external
change. Read-only hosts may still use Shiki-highlighted projection.

The mode is **per-tab**: each tab keeps its own
mode, sticky within a session and **persisted across restarts** on
`TabRef.view_mode` in the vault `config.yaml` (only `source` is written;
absence means the Edit default). A freshly-opened tab starts in
`editor.default_view_mode`. Switching a tab to Source **unmounts its
TipTapEditor** (the editor is destroyed and rebuilt from saved blocks on
return to Edit), so a tab held in Source view pays no ProseMirror memory cost;
Edit scroll/caret restore across the round-trip.

**Markdown-as-you-type.** At the start of a note, typing `#`–`######`, `>`,
`` ``` ``, or `> [!variant]` + space converts to heading / quote / code fence /
callout (slash menu and toolbar remain available).

**Outline.** An optional outline panel lists H1–H6 headings, supports
click-to-jump, scroll-spy active heading, and per-heading collapse.

### Rich blocks & editor interactions

**Inline math (`$...$`) and block math (`$$...$$`).** LaTeX rendered
with **KaTeX**. Inline math is an atomic inline node; a block equation is a
NOTE whose entire body is `$$...$$` (rendered centered). Both round-trip the
raw LaTeX verbatim. A balanced inline `$…$` pair at a word boundary with no
internal spaces auto-triggers the inline node as soon as the closing `$` lands
(currency-safe: `5$ cash`, `$5`, and `cost $5 and $3` stay literal — the
InputRule finder rejects a `$` preceded by `$` and any pair spanning whitespace).
Block `$$…$$` is inserted via the `/math` slash command. Editing an existing
node, or `/math`, opens an in-app LaTeX popover with a live preview (commit
`Ctrl/Cmd+Enter`, cancel `Esc`, empty commit rejected), replacing the native
`window.prompt`. A per-vault `ui.formatting.math_enabled` toggle (default true)
controls the `/math` slash command; existing math in files always renders.

**Mermaid diagrams.** A fenced code block whose info string is
`mermaid` renders a live SVG diagram (```mermaid) instead of syntax-highlighted
text. The raw source is preserved verbatim; only the view differs. Invalid
source shows a readable inline error (never a blank box). Edit-source /
show-diagram toggle; copy button.

**@-mention (`@[name]`).** Typing `@` opens a typeahead of
known task owners (the distinct-owner set from the index). Selecting one
inserts an atomic mention chip. The `@[name]` token round-trips through
`clean_text`; the suggestion source is a read-only projection — no mention
state is stored. The owner list is filtered server-side as you type
(`DistinctOwners(prefix)` → `LIKE 'prefix%'`) and cached briefly with a
debounced refine, so the typeahead never ships a huge owner list over IPC. Confirming a mention inside a task line also writes `[owner:: name]`
in the same transaction (outside a task, the mention is just a reference and
no owner token is written).

**Block drag handle.** A drag grip reorders top-level blocks by
direct manipulation; dropping further to the right indents the block deeper,
and a drop-zone indicator previews the target depth.
`Alt+ArrowUp/Down` moves the active block by keyboard. `Delete` at the end of
a block and `Backspace` at the start merge the adjacent same-type same-parent
sibling into one block (survivor keeps its UUID); cross-type, cross-parent,
and code-block boundaries fall through to the per-type default.

**Enter vs Shift+Enter.** `Enter` always creates a new note block at the same
depth (outliner row). `Shift+Enter` inserts a soft line break inside the
current prose block (NOTE/HEADER, and task rows only inside the task
sub-editor). Soft breaks are stored as HTML `<br>` inside that block’s single
`clean_text` line — they do not create a new block id and do not use bare
newlines (which the prose renderer collapses). On a main-outline task row,
`Shift+Enter` opens the task sub-editor instead.

**Ordered lists.** Ordered note markers on disk stay GFM-simple (`1. ` / `1) `
with a trailing space) at every indent depth; nested items restart at `1` and
same-depth peers renumber on Enter, indent, and unindent. The editor may show
hierarchical outline labels in the UI (e.g. `1.1)`) computed from depth — those
labels are display-only and are never written to markdown.

### Block types

Silt round-trips the standard markdown block-level vocabulary. Each block type
is a first-class editor node, so the outliner's block operations (delete,
duplicate, indent, drag) treat the whole block as a unit. All on-disk forms
are standard syntax, interchangeable with Obsidian / Joplin / GitHub / VS Code.

| Block | On-disk syntax | Notes |
|---|---|---|
| Quote / blockquote | `> quoted text` | A `>` prefix is a note marker (parallel to `- `). Nested `>> ` quotes render deeper borders. `/quote` or Ctrl+Shift+9 toggles. |
| Callout | `> [!variant] message` + `>` body lines | Obsidian admonition syntax. Seven variants with material icon + accent. The body is `block+`: consecutive `>` lines form one managed `CALLOUT` block, and each body line may carry a block construct (task list, fenced code, GFM table, nested callout) — every body line is `>`-prefixed on disk, so multi-line children get `>` on each line and nested callouts become `>>`. Bare `>` is a paragraph break. `/callout` family. |
| Code block | ` ```lang … ``` ` (GFM fence) | Multi-line; internal newlines are preserved (a managed `CODE` block). Shiki syntax highlighting (theme-aware), language selector, copy button. `/code-block`. |
| Foldable details | `<details><summary>…</summary>…</details>` | Native HTML `<details>`; one managed `DETAILS` block. Collapse state is ephemeral. `/details` or Ctrl+Shift+. toggles. |
| GFM table | `| a | b |` pipe syntax | Editable grid with Tab/arrow nav, column resize, zebra + hover theming, and a 6-operation contextual toolbar (merge is omitted — GFM can't represent spans). One managed `TABLE` block — the block identity is on a trailing line after the last row. |

**Multi-line blocks.** The Go parser reads files line-by-line and `renderBlock`
collapses `\n`→space for prose blocks (TASK/NOTE/HEADER). Visual soft breaks
inside a single prose block are encoded as HTML `<br>` in `clean_text` (still
one managed line / one `ParsedBlock`). All multi-line block types use the
**unified region-block model**: each multi-line region — fenced code (`CODE`),
GFM table (`TABLE`), `<details>` HTML (`DETAILS`), and Obsidian callout
(`CALLOUT`) — is accumulated into ONE managed `ParsedBlock` whose `clean_text`
retains internal newlines. The block identity comment lives on its own
dedicated trailing line after the region content, so the on-disk format stays
strictly GFM/HTML/Obsidian syntax (interoperable with Obsidian, GitHub,
VS Code). The frontend converter is a clean 1:1 map — no regrouping.
Literal pipes in table cells are escaped as `\|`.

10. System Configuration Engine

Global settings are managed locally in a human-readable file located at Notebooks/.system/config.yaml. The schema defines global application defaults, plugin configurations, hotkeys, and parsing logic.

10.1 Configuration Schema (config.yaml)

# Silt Global System Settings Configuration

# Spatial Mapping
notebooks:
  path: "~/Notebooks"
  default_active: "Work"

# Editor Tuning
editor:
  font_family: "Plus Jakarta Sans"
  mono_font_family: "JetBrains Mono"
  font_size_px: 14
  line_height: 1.6
  tab_indent_spaces: 4
  auto_save_delay_ms: 500
  focus_highlight_ancestors: true
  show_word_count: false      # opt-in word count in editor status
  focus_mode: false           # dim non-active paragraphs
  default_view_mode: "edit"   # "edit" or "source"
  # Inline spellcheck (on by default). en-US is bundled; other languages
  # download on demand into the user-global dictionary cache.
  spellcheck_enabled: true
  spellcheck_language: "en-US"
  # Enabled domain/technical word-list packs (merged as Set layers).
  # Default includes the bundled software-terms list. Empty = none.
  spellcheck_domains: ["software-terms"]
  # Typewriter mode keeps the active line at a fixed viewport ratio
  # (default off; ratio clamped to [0.1, 0.9]).
  typewriter_mode: false
  typewriter_mode_ratio: 0.5
  # Per-vault custom spellcheck words (a per-vault UI pref, so it lives here
  # in YAML). A linked notebook may carry its own co-located override
  # (arrays replace; §3.1). Import/export as UTF-8 one-word-per-line .txt
  # (# comments allowed; Hunspell personal-dictionary format).
  custom_dictionary: []

# Task Parse Rules
# The task checkbox/metadata grammar is fixed and intentionally NOT
# user-editable: a user-supplied regex on a synced vault is a
# catastrophic-backtracking DoS vector against the indexer. Only the
# non-regex parse knobs below are configurable.
parsing:
  auto_inject_uuid: true
  default_task_priority: 3

# Key-Binding Map. Defaults are convention-anchored (see "Keyboard Shortcuts"
# in ARCHITECTURE.md): ties anchor to document-processor conventions, with
# code-editor conventions filling gaps where document processors have no
# opinion. Windows/Linux only (Ctrl everywhere).
# Spellcheck deliberately has NO hotkey (wavy underline + right-click + a
# toolbar button). Paste is not listed: Ctrl+V = rich, Ctrl+Shift+V = plain.
hotkeys:
  # open_search → Ctrl+Shift+F (the cross-file search convention;
  # single-document editors have no equivalent). Frees Ctrl+P for future Print.
  open_search: "Ctrl+Shift+F"
  # open_command_palette → Alt+Q (the "search the app" convention).
  open_command_palette: "Alt+Q"
  # open_settings → Ctrl+, (the universal settings convention; freed from
  # format_subscript, which moved to Ctrl+Shift, below). #511 made settings a
  # first-class sidebar-owned view.
  open_settings: "Ctrl+,"
  # Creation, navigation discovery, and shortcut help.
  new_page: "Ctrl+N"
  new_section: "Ctrl+Alt+N"
  new_notebook: "Ctrl+Alt+Shift+N"
  open_quick_switcher: "Ctrl+P"
  open_shortcuts_help: "Shift+?"
  # cycle_view_layout → Ctrl+Alt+V (Alt+Tab is the OS window-switcher).
  cycle_view_layout: "Ctrl+Alt+V"
  indent_block: "Tab"
  unindent_block: "Shift+Tab"
  open_template_picker: "Ctrl+Shift+T"
  next_tab: "Ctrl+Tab"
  prev_tab: "Ctrl+Shift+Tab"
  close_tab: "Ctrl+W"
  # Sidebar hotkeys. Ctrl+B toggles visibility; focus_sidebar moves keyboard
  # focus into the active sidebar's first control. When the editor is focused
  # Ctrl+B resolves to format_bold; toggle_sidebar / focus_sidebar fire only
  # outside the editor. If the sidebar is collapsed, focus_sidebar expands it
  # first, then focuses.
  toggle_sidebar: "Ctrl+B"
  focus_sidebar: "Ctrl+Shift+B"
  # Inline formatting hotkeys.
  format_bold: "Ctrl+B"
  format_italic: "Ctrl+I"
  format_underline: "Ctrl+U"
  format_strike: "Alt+Shift+5"
  format_code: "Ctrl+E"
  format_link: "Ctrl+K"
  format_highlight: "Ctrl+Shift+H"
  format_subscript: "Ctrl+Shift,"
  format_superscript: "Ctrl+."
  # Heading level hotkeys.
  set_h1: "Ctrl+Alt+1"
  set_h2: "Ctrl+Alt+2"
  set_h3: "Ctrl+Alt+3"
  set_note: "Ctrl+Alt+0"
  set_task: "Ctrl+Alt+4"
  # Text alignment hotkeys.
  align_left: "Ctrl+Shift+L"
  align_center: "Ctrl+Shift+E"
  align_right: "Ctrl+Shift+R"
  align_justify: "Ctrl+Shift+J"
  # Blockquote toggle.
  toggle_quote: "Ctrl+Shift+9"
  # Foldable details toggle. Ctrl+Shift+. (Ctrl+. is taken by superscript).
  toggle_details: "Ctrl+Shift+."
  # Table row/column insert hotkeys. Deletion + merge are toolbar-only.
  table_insert_row_above: "Ctrl+Shift+Up"
  table_insert_row_below: "Ctrl+Shift+Down"
  table_insert_col_left: "Ctrl+Shift+Left"
  table_insert_col_right: "Ctrl+Shift+Right"
  # View mode toggle.
  toggle_view_mode: "Ctrl+Shift+V"
  # toggle_format_toolbar → Ctrl+F1 (the "toggle ribbon" convention); frees
  # Ctrl+Shift+F for global search. focus_mode dims non-active paragraphs.
  toggle_format_toolbar: "Ctrl+F1"
  toggle_focus_mode: "Ctrl+Shift+D"
  # Search, find/replace & writing aids. find_in_page (Ctrl+F) and replace
  # (Ctrl+H) are the in-editor bindings; global_replace (Ctrl+Shift+G)
  # escalates replace across the vault; toggle_typewriter_mode (Ctrl+Shift+Y)
  # pairs with toggle_focus_mode (Ctrl+Shift+D).
  find_in_page: "Ctrl+F"
  replace: "Ctrl+H"
  global_replace: "Ctrl+Shift+G"
  toggle_typewriter_mode: "Ctrl+Shift+Y"
# Editor-scoped shortcuts (heading, alignment, quote, details, table, format
# marks) are remappable here; the remapped binding takes effect on the next
# page load.

# Global Search accepts a filter object alongside the query:
#   type: TASK / NOTE / HEADER / CODE / TABLE / DETAILS / CALLOUT
#   sort: "" | "relevance" (default) | "recency"
#   vaultOnly: exclude linked-notebook sources (in-vault only)
# Empty fields mean "no filter on that dimension". Tag matches the exact tag
# OR a hierarchical descendant ("work" → "work/*"). All user input is
# parameterized (a synced vault's tag/notebook name can't inject SQL).
#
# Global Replace previews matches grouped by page (before→after) with
# per-match and per-page accept, then applies approved replacements and keeps
# a session revert log. It applies to in-vault pages; linked notebooks are
# read-only.

# UI Preferences (per-vault)
ui:
  sidebar_width: 256
  enable_preview_tabs: true
  max_open_tabs: 8
  # Format toolbar visibility. Default true; hide for outliner density.
  show_format_toolbar: true
  # Per-tab dirty/save-failed glyph on the tab header. Default true; auto-save
  # means most dirty state is sub-second, so users who find the visual churn
  # noisy can disable it. The in-editor indicator is unaffected.
  show_tab_dirty_indicators: true
  # Open-tab persistence. Pinned tabs only; preview tabs are ephemeral.
  open_tabs: []        # list of {notebook, section, page, view_mode?}
  active_tab: null     # {notebook, section, page, view_mode?} or null
  # One-time tip dismissals.
  dismissed_tips: []
  # Note content zoom factor (independent of editor.font_size_px / app chrome).
  # Default 1.0; range 0.7–2.0; snapped to 0.1 steps. Atomic SetNoteZoom IPC.
  note_zoom: 1.0
  # Inline formatting toggles.
  formatting:
    typography_enabled: true   # smart quotes, em-dashes
    color_enabled: true        # text/background color pickers
    math_enabled: true         # LaTeX math ($...$ / $$...$$) + /math command

# Explicit navigation ordering for drag-to-reorder. Section/page keys use the
# format `${notebook}/${section}` (empty section for root pages). Items absent
# from the map fall back to alphabetical sort.
nav_order:
  notebooks: []                    # ordered notebook names
  sections: {}                     # notebook name → [section names]
  pages: {}                        # sectionKey → [page names]

# Plugin Registry
plugins:
  active:            # informational only; not a whitelist
    - "silt-tasks"
    - "silt-attachments"
  disabled: []
  plugin_settings:
    silt-tasks:
      default_display_mode: "list"   # list | board | calendar
      default_group_by: "dueDate"
      default_sort: "dueDate"
      columns: ["TODO", "DOING", "DONE"]   # status-board lane order
      saved_views:
        - id: "55c1f0...-..."        # client-generated UUID; system views (sys-*) never persist
          name: "Sprint Board"
          displayMode: "board"
          groupBy: "status"
          sort: "manual"
          scope: "vault"
          columns: ["TODO", "DOING", "DONE"]
          filters: { owners: [], priorities: [], dueDate: "", tags: [] }

# AI Providers + product features (#216, #218, #632)
# Two independent provider blocks (chat + embedding) that plugins call through
# ctx.ai.complete / ctx.ai.embed. Silt makes no cloud calls of its own; this
# points at a model server the user runs (local) or has a key for (cloud).
# Product enablement is ai.features (not plugins.disabled for first-party AI).
# See docs/BRING_YOUR_OWN_MODEL.md.
ai:
  # When true (default), API keys live in the OS keyring (Credential Manager /
  # Keychain / Secret Service) instead of plaintext below. Keys are vault-
  # scoped (SHA-8 of the vault path) so they don't travel on sync. When the
  # keyring is unreachable (headless Linux, WSL2), keys fall back to the
  # api_key field here and the AI settings tab surfaces a warning.
  use_keyring: true
  # Product switches (Settings → AI). All default false (opt-in).
  # rag_enabled / summaries_enabled require enabled; normalize clamps them.
  features:
    enabled: false            # master: agent drawer + writing assistant
    rag_enabled: false        # semantic search / Q&A index + agent retrieval tools
    summaries_enabled: false  # note summary banner
  chat:
    provider_type: "local"            # "local" | "openai-compatible"
    base_url: "http://localhost:11434" # local default = Ollama
    model: ""                          # e.g. "llama3.1", "gpt-4o"
    # api_key is omitted from version control when use_keyring is on; present
    # here only as the fallback when the keyring is off/unavailable.
    temperature: 0.7
    max_tokens: 2048
    reasoning_effort: "medium"         # chat only: none|minimal|low|medium|high|xhigh|max
    timeout_ms: 60000
  embedding:
    provider_type: "local"
    base_url: "http://localhost:11434"
    model: ""                          # e.g. "nomic-embed-text"
    dimensions: 0                      # 0 = read from the model's first response
    timeout_ms: 60000

10.2 Hot Reloading Logic

The Go file-system monitor sets a high-priority watch handler on .system/config.yaml. If settings are modified internally or externally, Go parses the file, updates the system memory state instantly, and triggers a style or action event over the Wails IPC bridge, bypassing the need for a full application reboot.

11. Typed Notes

Typed notes bring structured, schema-driven properties to Silt pages. A page declares a type in YAML frontmatter, inherits that type's property schema from a per-vault YAML file, renders and edits its properties through a dedicated UI, and appears in a per-type dashboard queryable by property. The feature follows the local-first contract: Markdown frontmatter (§3.3) is the source of truth for property values, the type schema is a per-vault YAML asset, and the SQLite projection is reproducible working memory. An ADR records the design rationale (`docs/decisions/0008-typed-notes.md`).

11.1 Type Schema Format

Type schemas live in `<vault>/.system/types/<type>.yaml`, siblings to `.system/templates/` and `.system/themes/`. Each file defines one type:

- `name` (required): the type's display name.
- `id` (optional): canonical identifier matching `^[a-z0-9_-]+$`; defaults to the filename stem. This id is what frontmatter `type:` stores.
- `description`, `icon`: optional metadata for the type picker and dashboard.
- `heroField` (optional): the property name whose value renders in the inline meta strip for glanceability.
- `properties` (required): a list of property definitions, each with:
  - `name` (required)
  - `type` (required): one of the 9-type taxonomy (§11.2)
  - `required` (optional, default false)
  - `options` (for select/multiselect): the allowed values
  - `target` (for page/pages): constrains the relation to pages of this target type
  - `default` (optional): the default value for new instances

The vault scaffolder seeds `.system/types/` with example types (a Book type and a Meeting type) on first vault init. Users create and edit type files directly or via the UI; a hot-reload watcher picks up external edits.

11.2 Property-Type Taxonomy & Encoding

The closed taxonomy: `text | number | date | datetime | checkbox | select | multiselect | page | pages`.

| Type | Frontmatter encoding | Validation |
|---|---|---|
| text | string | non-empty if required |
| number | JSON number | numeric; min/max if declared |
| date | `YYYY-MM-DD` | parseable calendar date |
| datetime | `YYYY-MM-DDTHH:mm:ss` | parseable timestamp |
| checkbox | `true` / `false` | boolean |
| select | string (one of `options`) | membership in declared options |
| multiselect | YAML list of strings | each member in declared options |
| page | string (target page path) | target exists + matches declared `target` type |
| pages | YAML list of strings | each target exists + matches declared `target` type |

Schema is the source of truth; frontmatter holds only set values. A property declared in the schema but absent from frontmatter renders as empty in the UI — adding a property to a schema propagates to all instances with zero file churn (virtual propagation). This means the SQLite projection stores rows only for properties that have values (sparse).

11.3 Properties UI

Two surfaces compose the properties experience:

1. Inline meta strip: in the breadcrumb row, shows the type chip and the `heroField` value. Untyped pages stay clean — there is no persistent "+ Type" affordance. The chip body opens the properties panel; a caret opens a menu with a "View all [Type]" action that routes to the per-type dashboard (§11.4).

2. Bottom docked panel (toggle: `Mod+;`): rises on demand, pushing the editor up so reading context is preserved. Renders one field per schema property, type-aware (checkbox as a toggle, select as a dropdown, page/pages as a combobox typeahead). The panel uses a non-blocking focus contract (`aria-modal="false"`) so the user can interact with the editor while the panel is open.

Page/pages relations render as combobox typeaheads with a dropdown of matching pages from the navigation tree, narrowed to the declared target type when set. Dangling targets (pages that have been deleted) render as strikethrough chips — silently inert, with no two-way inverse write (backlinks already derive the reverse relationship).

Type switching: when a typed page switches to a different type, a Turn-into dialog previews the property mapping (carries over / compatible / will be flagged / won't appear / new) and offers optional orphan clearing. Switching from untyped to typed assigns directly (nothing to map).

11.4 Per-Type Dashboard

A full-content-area view showing all pages of a given type in a sortable table with per-property filters and client-side group-by. Columns derive from the type's schema; cells render by value type (checkbox as a check/dash, select as a chip, dates formatted, text as-is). The page-name column always leads, with the hero field as a subtitle. Sortable column headers (click to toggle asc/desc); filter controls per property (dropdown for select/multiselect, text contains for text, tri-state for checkbox, date input for dates); group-by bins rows into collapsible sections. Rows link back to the editor on click.

11.5 MCP Property Read/Write

Typed properties are exposed to AI clients over the Local MCP host via three tools:

- **get_page_metadata** (read, ungated): returns the page's type, schema-merged properties, and raw frontmatter in one snapshot.
- **set_page_property** (write-gated): validates the value against the type's property schema and relation targets before any file I/O. Invalid values return a structured error and leave the file byte-identical — the schema gates every write. Single-key granularity.
- **set_page_type** (write-gated): validates existing values against the new schema before the `type:` rewrite.

The schema-gates-every-write guarantee is the core safety argument for exposing typed-property writes to AI clients. See `docs/LOCAL_MCP.md` for the tool interface.

11.6 Coexistence with Tags (§5.1)

Typed properties and the existing smart-tag namespace system (§5.1) are complementary, not competing. Tags are free-form, user-assigned at any granularity, and queried via the tag typeahead (§5.4). Typed properties are schema-constrained, type-scoped, and queried via the dashboard. A page can carry both: tags for ad-hoc categorization and typed properties for structured fields. The SQLite projection for typed properties (`page_properties`) is separate from the tag index (`tags` table), so the two query paths never interfere.
