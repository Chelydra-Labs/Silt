Engineering Architecture: Silt

**How to use this document.** This is the engineering blueprint of the
system *as it exists today*: process topology, storage tiers, the data
contract, the IPC surface, the concurrency model, and performance budgets.
It captures **what the system is and why it is shaped that way** — not how
every function is implemented.

- **Authoritative for:** contracts, boundaries, and invariants — topology,
  storage tiers, the schema, the IPC surface, the concurrency model,
  performance budgets.

**Principles**
- Describe what the system *is* and *why it is shaped that way*, not how
  every function is implemented.
- The code is the source of truth for implementation detail; this file
  points to the package rather than reproducing it.

**Rules**
- Never paste source code into this file (it drifts); point to the code.
- Update a contract here whenever the code's contract changes.

**Best practices**
- Prefer a cross-reference (`docs/decisions/`, a spec section) to an inline
  `(#123)` tag.
- When a contract is non-obvious, link the ADR that explains *why*.
- If a section reads like a code tour, the detail belongs in code.

**Not for**
- **Implementation detail** — struct fields, regexes, PRAGMA values,
  per-file behavior.
- **Status or changelog content** — sprint numbers, "replaced the former
  X," "was removed," "shipped in #N," roadmap/follow-up notes.
- **Issue archaeology** — `(#123)` tags and rejected-alternative essays.

Companion documents: **SPECS.md** is the forward-looking product north star;
**DESIGN.md** is the visual system; `docs/decisions/` holds ADRs.

## Storage-of-Truth Tiers (Read First)

Silt's persistent storage is layered into four tiers with **deliberate,
non-overlapping responsibilities**. Every new feature MUST be designed
against this map before writing code. Violating these tiers is a
correctness regression, not a style choice.

| Tier | Format | Location | Holds | Example |
|---|---|---|---|---|
| **Content** | Markdown (`.md`) | Vault root + per-page files | Block bodies, task markers, per-task metadata, block identity (`<!-- id: uuid @ YYYY-MM-DD -->`) | `[/] DOING TASK [Alice] (2026-06-15) #2 !pin [p:50] Implement search <!-- id: 7c2a… @ 2026-06-15 -->` |
| **Per-vault UI preferences** | YAML | `<vault>/.system/config.yaml` | Per-vault, per-plugin settings: active/disabled plugin list, Kanban columns, Kanban filter state, hotkey bindings, editor font sizes, theme typography overrides | `plugins.plugin_settings.silt-kanban.columns: [Backlog, In Progress, Review, Done]` |
| **Per-linked-notebook overrides** | YAML | `<linkedRoot>/.system/config.yaml` | Per-notebook plugin setting overrides for a linked (external) notebook. Read-only to Silt (user-authored); deep-merged over the vault defaults (linked wins per-key). See §3.1. | `plugins.plugin_settings.silt-kanban.columns: [Backlog, Done]` |
| **User-global, pre-vault** | JSON | `<config>/silt/settings.json` | Settings that must be known before any vault is open: active theme id, dark/light/system mode, non-vault font preferences | `{"active_theme": "silt-graphite", "mode": "dark"}` |
| **Working memory** | SQLite (WAL) | `<vault>/.system/index.sqlite*` | Re-derivable caches: block↔location projection, FTS5 search index, denormalized per-task caches (comments/links counts, pin, progress — all re-derived from markdown on re-index), file mtime/size for incremental re-index | The `blocks` table, `blocks_fts` virtual table, `files` mtime cache |
| **Plugin-owned storage** | SQLite (WAL) | `<vault>/.system/plugins/<id>/data/plugin.db` | Per-plugin private data the plugin owns the schema for: working memory OR durable storage at the plugin's discretion (embeddings, content-hash caches, agent memory). The plugin decides durability semantics; data that must survive uninstall or be portable MUST round-trip through markdown. | A plugin's `vec0` vector index, a content-hash cache table |

**The cardinal rules:**

1. **Markdown is the source of truth for content.** Every per-block
   metadata field (status, owner, priority, dates, pin, progress) MUST
   round-trip through the markdown inline task syntax. The block
   identity comment is the only identifier stored in the file; the file
   position and the inline syntax are the source for everything else.
   Deleting the entire `<vault>` should be recoverable by re-creating
   the YAML config — the markdown files are the *product*.

2. **YAML holds per-vault, per-user, per-plugin UI preferences** that
   don't belong in any individual block. If two plugins want different
   values, they live in YAML, not in markdown.

3. **JSON holds user-global, pre-vault settings.** A user can have a
   theme picked before they ever open a vault. The active theme id
   cannot wait for a vault to be loaded; it must live in user-global
   JSON.

4. **SQLite is working memory, not a system of record.** Every row in the
   **core index** (`<vault>/.system/index.sqlite*`) MUST be reproducible from
   the markdown + YAML above. The recovery path for any core-index corruption
   is *delete the index file and relaunch* — the documented, supported
   operation. The core index holds the block↔location projection, FTS5, file
   mtime/size caches, and re-derived per-task caches (comments/links counts,
   pin, progress — re-derived from markdown `[pin:: true]` / `[progress:: N]`
   tokens on every re-index). It is **forbidden** to hold user intent *as the
   source of truth* there: pin state, progress, custom column names, filter
   state, theme id, and hotkey bindings must round-trip through the markdown
   inline syntax (per-block) or YAML/JSON (per-vault/per-user). The cached
   pin/progress columns in the `tasks` table are query-speed projections, not
   authority — delete the index and they rebuild from markdown.

   **Plugin-owned storage carve-out.** Cardinal rule #4 governs the **core
   index only**. Each plugin MAY carry a separate SQLite file at
   `<vault>/.system/plugins/<id>/data/plugin.db`, opened on a **distinct**
   connection that is never `ATTACH`-able to the core index. The plugin owns
   its schema and chooses durability semantics (working memory *or* durable
   storage — vector indexes, content-hash caches, agent memory). The boundary:
   data that must survive uninstall or be portable across vaults MUST
   round-trip through markdown; plugin-private caches need not. The plugin DB
   is deleted on uninstall (see ADR
   `docs/decisions/0001-plugin-storage-tier.md`).

5. **Settings can be stored in JSON** (the pre-vault / user-global
   tier), but only when the data must be available before a vault is
   open. Everything else that is per-vault goes in YAML.

This is the local-first contract: the user's files on disk *are* the
product. The Svelte UI, the Go backend, and the SQLite index are all
projections of those files, not the other way around.

**Non-note data.** Two categories of block content live outside the normal
Notebook › Section › Page tree, both still plain markdown so the
source-of-truth invariant holds:

- **Standalone tasks** — tasks created from a quick-add surface (calendar
  cell, kanban footer, global `Mod+Shift+N`) that aren't attached to a note
  live as GFM checkboxes in `<vault>/.silt/tasks.md`, indexed under a
  synthetic hidden `.silt` notebook (no new SQL table, no nullable
  `block_id`). The Tasks first-party plugin is the only user-facing surface
  for them; every navigation funnel re-routes `.silt` jumps to it. See
  SPECS §4.1 for the feature design.
- **Recurring tasks** — a `[recur:: RULE]` token is cached in `tasks.recur`
  for query/filter speed; on DONE a pure resolver advances the `[due::]`
  anchor with skip-missed semantics and splices a fresh TODO block below the
  completed line in one atomic write. See SPECS §4.1 for the rule grammar.

---

1. System Topology & Process Boundaries

The Silt system runs as a single local process. The operating system boundary separates the low-level compiled disk-access layer from the lightweight front-end view frame using native platform Webview IPC handles:

+-----------------------------------------------------------------------------------+
|                           FRONTEND PROCESS BOUNDARY (Webkit)                      |
|                                                                                   |
|   [Svelte Rendering Framework] <───> [Svelte Store / Reactive State Matrix]       |
|                │                                              ▲                   |
|                ▼ (UI Event)                                   │ (Events/Data)     |
|   +───────────────────────────────────────────────────────────┼───────────────+   |
|   │ Wails JS Runtime (IPC Bridge)                             │               │   |
|   +───────────────────────────────────────────────────────────┼───────────────+   |
+────────────────┼──────────────────────────────────────────────┼────────────────---+
                 │                                              │
                 │ JSON RPC (WebKit MessagePorts)               │ IPC Event Dispatch
                 ▼                                              │
+────────────────┼──────────────────────────────────────────────┼────────────────---+
|                │          BACKEND PROCESS BOUNDARY (Go Core)  │                   |
|   +────────────▼───────────────+                              │                   |
|   │ Wails Binding Router       │                              │                   |
|   +────────────┬───────────────+                              │                   |
|                │ (Internal Calls)                             │                   |
|                ▼                                              │                   |
|   +────────────────────────────+                              │                   |
|   │ Mutex-Locked Disk Writer   │                              │                   |
|   +────────────┬───────────────+                              │                   |
|                │                                              │                   |
|                ├─► [Tmp Write] ──► [Atomic Rename] ──► [Disk] │                   |
|                │                                       │      │                   |
|                ▼                                       ▼      │                   |
|   +────────────────────────────+                +─────────────┴───────────────+   |
|   │ In-Memory SQLite Indexer   │ ◄───────────── | Directory File Monitor      │   |
|   +────────────────────────────+  (Parse Cache) | (fsnotify Engine)           │   |
|                                                 +─────────────────────────────+   |
+-----------------------------------------------------------------------------------+


2. Go Backend Core

The Go runtime orchestrates system access, monitors local storage directories, parses block arrays into structural tokens, and updates the SQLite analytics cache.

2.1 File System Monitor (fsnotify Pipeline)

To allow interoperability with external plain-text editors (e.g. an external editor), the Go backend implements an active directory watcher using github.com/fsnotify/fsnotify.

**Feedback-loop prevention.** When Silt writes to disk, fsnotify intercepts
the write event, which could re-trigger parsing and loop. The writer
therefore registers every self-write (`WriteTracker.RegisterWrite`); the
watcher's `IsSelfGenerated` check suppresses any fsnotify event for a path
within a 300 ms cooldown of our own atomic write. See
`backend/monitor/watcher.go`.


2.2 Custom AST Parser Engine

Every file ingested is scanned line-by-line using a customized Markdown AST engine built on top of yuin/goldmark (`backend/parser`).

**Block model.** A GFM checkbox item (`- [ ]`, `- [/]`, `- [x]`) is a task; the remainder of any line is scanned for Dataview-style `[key:: value]` metadata tokens (due, start, owner, priority, pin, progress, recur, blocked_by) — order-independent and extensible via the `scanTaskTokens` dispatch. Each parsed line becomes a `ParsedBlock` typed as one of three prose types (`TASK`, `NOTE`, `HEADER`) or one of four multi-line region types (`CODE`, `TABLE`, `DETAILS`, `CALLOUT`).

**Multi-line region blocks.** The parser's `accumulateRegion` detects four region shapes — fenced code, GFM table runs (header + separator), `<details>` HTML (depth-counted), and Obsidian callouts (`> [!variant]` + consecutive `>` lines) — and collapses each into one managed `ParsedBlock` (one `blocks`-table row, one UUID, one FTS5 document). The block-identity comment lives on its own dedicated trailing line after the region so the on-disk format stays strictly GFM/HTML/Obsidian syntax (byte-exact interop with Obsidian/GitHub/VS Code). `ParseFileContent` and `RenderFileContent` share the region-boundary helpers (`detectRegionKind` / `findRegionCloser` / `skipManagedRegion`) so both paths agree. Legacy files with per-line id comments are detected (id comments stripped before matching), migrated to the trailing-id format on first parse, and `((uuid))` references to vanished per-line ids are remapped to the region block's id.

**Block identity.** If a block lacks an `<!-- id: UUIDv4 @ YYYY-MM-DD -->` trailing comment, the parser mints one, rewrites the line, and flags the file for atomic rewrite. The id is the only identifier stored in the file; everything else (status, position, metadata) is derived from the line.


3. SQLite Schema & Query Optimization Layer

The storage-of-truth contract — what each tier holds, and the rule that the
core index is reproducible working memory rather than a system of record —
lives in §0 above. This section covers the core index's on-disk mechanics
(WAL, incremental re-index) and the concrete schema.

The on-disk SQLite lives in WAL mode at `<vault>/.system/index.sqlite`
(+ `.sqlite-wal` + `.sqlite-shm`). On restart only files whose
`mtime`+`size` differ from the last successful index are re-parsed and
re-indexed; a cold start (no index file yet, or the 3 index files
deleted by the user) performs a full scan and rebuild. The recovery
path is documented and intentional: deleting the 3 `.system/index.sqlite*`
files is safe because every row in them is re-derivable from the
markdown + YAML on the next launch. This durable, incremental model is
what lets Silt scale to dozens of notebooks and thousands of pages
without rebuilding the whole index on every launch.

Connections are opened by `db.NewDatabaseManager(dbPath)` (pass `""` for an ephemeral in-memory shared-cache DB, used in tests and before a vault is open). The DB runs in **WAL mode** — persistent in the file header, so every later connection (including the plugin SDK's read-only handle) inherits it without re-running the pragma. Per-connection pragmas are configured for WAL safety and performance; see
`backend/db/schema.go` for the values.

Concurrency: WAL allows unlimited readers alongside a single writer; readers never block writers and the writer never blocks readers. The Go-level `core.ExecutionCoordinator` serializes all access (`SetMaxOpenConns(1)`) so the locking story stays simple. Clean shutdown runs `PRAGMA wal_checkpoint(TRUNCATE)` (in `DatabaseManager.Close` and after each startup re-index pass) so the WAL does not grow unbounded across sessions; on a crash, SQLite auto-recovery replays the WAL on the next open.

Caveat: WAL relies on shared memory and therefore does **not** work on network filesystems (NFS/SMB). Local-first single-user desktop is the supported deployment; a vault on a network mount will fail to open the index with a clear error rather than silently corrupt.

-- Blocks Table
-- file_date is per-block (stored inline in the trailing comment
-- <!-- id: uuid @ YYYY-MM-DD -->), not file-level. A page is a single .md
-- file; blocks from different dates coexist in the same page file.
CREATE TABLE blocks (
    id TEXT PRIMARY KEY,
    parent_id TEXT,
    source TEXT NOT NULL DEFAULT 'vault',  -- 'vault' | 'linked:<id>'
    notebook TEXT NOT NULL,
    section TEXT NOT NULL,
    page TEXT NOT NULL,       -- Page (streaming unit) inside the Section
    file_date TEXT NOT NULL,  -- YYYY-MM-DD
    depth INTEGER DEFAULT 0,
    type TEXT NOT NULL,      -- 'TASK', 'NOTE', 'HEADER'
    raw_content TEXT NOT NULL,
    clean_content TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    FOREIGN KEY(parent_id) REFERENCES blocks(id) ON DELETE SET NULL
);

-- Tasks Metadata Projection Table (Mapped from blocks)
CREATE TABLE tasks (
    block_id TEXT PRIMARY KEY,
    status TEXT NOT NULL,    -- 'TODO', 'DOING', 'DONE'
    owner TEXT,
    start_date TEXT,         -- YYYY-MM-DD or NULL
    due_date TEXT,           -- YYYY-MM-DD or NULL
    priority INTEGER,        -- 1, 2, 3
    pinned INTEGER DEFAULT 0,         -- NULL/0/1 tri-state cache: NULL=no [pin::] token, 0=[pin:: false], 1=[pin:: true]; reproducible from markdown
    progress INTEGER DEFAULT 0,       -- 0-100; cached from [progress:: N] markdown token
    recur TEXT,                       -- recurrence rule (e.g. 'every week'); NULL for one-off tasks; cached from [recur:: RULE] token
    comments_count INTEGER DEFAULT 0, -- derived: child NOTE blocks
    links_count INTEGER DEFAULT 0,    -- derived: ((uuid)) references in body
    FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
);

-- Namespace Hierarchical Tags
CREATE TABLE tags (
    block_id TEXT NOT NULL,
    raw_path TEXT NOT NULL,  -- 'work/project/milestone-one'
    level_0 TEXT NOT NULL,   -- 'work'
    level_1 TEXT,            -- 'project'
    level_2 TEXT,            -- 'milestone-one'
    PRIMARY KEY(block_id, raw_path),
    FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
);

-- Task Dependencies (#301): the [blocked_by:: ((uuid))] edge graph. Each row
-- means "block_id is blocked by blocked_by_id". Both FKs cascade so a deleted
-- block cleans up its edges as a dependent and as a blocker. Re-derivable
-- from markdown (rule 4); the reverse-lookup index on blocked_by_id serves
-- the DONE-branch fan-out and the Kanban/Agenda "blocked" badge.
CREATE TABLE task_dependencies (
    block_id      TEXT NOT NULL,
    blocked_by_id TEXT NOT NULL,
    PRIMARY KEY(block_id, blocked_by_id),
    FOREIGN KEY(block_id)      REFERENCES blocks(id) ON DELETE CASCADE,
    FOREIGN KEY(blocked_by_id) REFERENCES blocks(id) ON DELETE CASCADE
);
CREATE INDEX idx_task_deps_blocked_by ON task_dependencies(blocked_by_id);

-- File-stats cache for incremental re-indexing. Keyed by absolute path;
-- a renamed file is a new path, with the stale old row pruned by the next
-- startup scan. A warm restart skips re-parsing any file whose mtime+size
-- match the last successful index.
CREATE TABLE files (
    path       TEXT PRIMARY KEY,
    mtime      INTEGER NOT NULL, -- Unix nanoseconds
    size       INTEGER NOT NULL,
    indexed_at INTEGER NOT NULL  -- Unix nanoseconds
);

-- Covered indexes for query performance.
-- idx_blocks_src_file is source-aware (source leads) so same-named notebooks
-- across roots don't collide.
CREATE INDEX idx_blocks_src_file ON blocks(source, notebook, section, page, file_date);
CREATE INDEX idx_tasks_dates ON tasks(start_date, due_date) WHERE start_date IS NOT NULL OR due_date IS NOT NULL;
CREATE INDEX idx_tags_lookup ON tags(level_0, level_1, level_2);


3.1 External / Linked Notebooks

A vault is the default home for notebooks, but it is not the only one. A user
can LINK an external folder (e.g. a synced SharePoint/OneDrive/Dropbox mount)
as a notebook and edit it IN PLACE — it is never copied into the vault, so its
existing source of truth and sync/conflict semantics are preserved. The
local-first contract is unchanged: markdown is the product; the SQLite index is
reproducible working memory.

Identity model. `blocks.source` discriminates the root a block belongs to:
`'vault'` for an in-vault notebook, or `'linked:<id>'` for a linked notebook.
This disambiguates same-named notebooks across roots (a vault "Work" and a
linked "Work" never collide on `(notebook, section, page)`). Notebook DISPLAY
NAMES are globally unique — `LinkNotebook` rejects a name that collides with a
vault notebook or an existing link — so the frontend resolves a notebook's
source from its name alone via a name→source map kept in sync on each nav load.
The index stays LOCAL (`<vault>/.system/index.sqlite*`); only the markdown
content (and any co-located `<root>/.system/`) lives on the remote mount.

Link registry. The vault-scoped `config.yaml` carries a `linked_notebooks:`
list (`{id, root_path, display_name}`), persisted atomically by the existing
`config.Save` (self-write suppressed). The registry is vault state (same bucket
as the active plugin list), NOT user-global.

Path resolution. `App.resolveNotebookDir(notebook, source)` returns a
notebook's content directory: `<vault>/<notebook>` for `'vault'`, or the linked
root itself for `'linked:<id>'` (sections/pages live directly under it). Source
is resolved **server-side** by `resolveSourceByName(name)` — notebook display
names are globally unique (link collision rejection), so the name alone maps to
`'vault'` or `'linked:<id>'`. Every notebook-scoped operation (the blockID write
paths via `GetBlockLocation().Source`; CreatePage / CreatePageFromTemplate /
DeletePage / RenamePage / CreateSection / DeleteSection / RenameSection; the
editor focus-lease) routes through it, so linked notebooks get full page CRUD +
focus protection with no parallel frontend source-flow. The traversal guard
generalizes to `isPathWithinRoot(target, root)`.

Multi-root watcher. `DirectoryWatcher` observes the vault root PLUS any number
of linked roots on one process-wide fsnotify watcher, sharing the coordinator,
WriteTracker, and focus-lease maps (all path-keyed, root-agnostic). `AddWatchRoot`
/ `RemoveWatchRoot` register/deregister; `resolveFileMetadata` does a longest-
prefix root lookup and attributes each event: for the vault root the notebook
is the first path component (a vault holds many notebooks); for a linked root
the notebook is the registered display name (the root IS one notebook).

Lifecycle bindings. `LinkNotebook(folderPath)` validates, assigns a stable id,
rejects collisions, folders already inside the vault, **and ancestors of the
vault** (which would double-index the vault), persists the registry, watches +
indexes the tree in a SINGLE batched transaction (forcing `notebook =
DisplayName` so an external file's frontmatter can't drift it out of the nav).
The batched path threads `source` through `IndexScanResults` (the same
function the vault startup scan uses) and does the `files`-table
(`MarkFileIndexed`) pass after the index commit, so a large synced mount
indexes without per-file WAL-checkpoint thrash. `UnlinkNotebook(id)` stops watching,
drops the source's index rows (`ClearSourceBlocks`), and leaves the external
files COMPLETELY UNTOUCHED (safe default). `PickLinkedNotebook()` drives the
native folder picker. Deleting a linked notebook from the sidebar UNLINKS it
(vs. trashing a vault notebook). Page/section delete inside a linked notebook
removes the file IN PLACE (the external folder is the source of truth — Silt
never copies linked content into the vault trash). `RenameNotebook` refuses a
linked notebook (rename = unlink + re-link); page/section rename works in place.

Failure modes. An offline mount degrades gracefully: `ListNavigation` marks the
notebook `Disconnected` (the badge flips to cloud_off) but its last-synced index
rows remain queryable; writes to a disconnected root return a clear error (no
crash). Reconnect re-indexes on the next fsnotify event (the watch survives a
temporary mount drop). Linking a folder that is offline at link time registers
the link and indexes best-effort (logged); the user can re-link once it's back.

Sync-conflict caveat. Silt's atomic write is temp-file + `os.Rename`. On a
network filesystem (SMB/WebDAV) `os.Rename` may not be atomic the way it is on a
local FS, but the `WriteTracker` self-write suppression and the editor focus
lock still hold, so external (non-Silt) edits to the same file are reconciled
by the existing diff/lease machinery once both sides land. A vault index must
stay on a local disk regardless (WAL does not work on NFS/SMB — §3), so only
the markdown crosses the mount.

Co-located per-notebook config. Per the storage-of-truth model, data
attached to a notebook travels with the notebook. For a linked (external)
notebook, per-notebook plugin overrides live at
`<linkedRoot>/.system/config.yaml`, so an external notebook on SharePoint
carries its own config with it — not in the vault. The co-located file is
READ-ONLY to Silt (user-authored); plugin settings continue to persist to the
vault-scoped `config.yaml` via the atomic `UpdatePluginSetting` path. The
co-located file is purely an override layer.

Merge precedence: vault-scoped config.yaml is the baseline; a linked
notebook's co-located file overlays it per-key (linked wins). Nested maps
merge recursively; scalars and arrays from the co-located file replace the
vault's. The merge is computed on every call from the live, mtime-cached
co-located config (see `App.linkedConfigs`), so an external edit is reflected
on the next call. The multi-root watcher observes `<linkedRoot>/.system/
config.yaml` and emits `linked-config:changed` on external edit, driving
reactive refreshes (e.g. Kanban columns/filters re-resolve on the switch).

Resolution surface: `App.GetPluginSettingsForNotebook(pluginID, notebookName)`
is the IPC binding that resolves a plugin's settings for the active notebook
(vault → vault settings verbatim; linked → deep-merge). The SDK
`PluginContext.getPluginSettings()` wraps it with the live `activeNotebook`
reactive getter, so a plugin that calls it at render time always sees the
merged settings for the current notebook.


4. Wails Bridge & IPC API Contract

Communication between Svelte and Go occurs over a typed JSON bridge. The following API commands are registered with the Wails framework.

4.1 Block Mutation Envelope

type MutateBlockPayload struct {
	ID        string `json:"id"`
	FilePath  string `json:"file_path"`
	NewText   string `json:"new_text"`
}


4.2 Query Filter Envelope (Agenda / Calendar)

type TaskQueryFilter struct {
	Owner     string   `json:"owner"`
	Priority  int      `json:"priority"`
	Tags      []string `json:"tags"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
}


4.3 IPC Service Surface

All bindings hang off the single Wails-bound `App` (`Bind: { app }`) and are
auto-exposed to the frontend as JSON RPC. Grouped by domain:

- **Block I/O** — `FetchPageBlocks`, `SaveFileBlocks`, `UpdateBlockState`
  (task-checkbox transition + atomic file rewrite + re-index),
  `MutateBlock`, `QueryTasks` (dashboard filter query). **Task dependencies**
  (#301): `SetTaskBlockedBy` / `PluginSetTaskBlockedBy` (cycle-checked
  `[blocked_by::]` token rewrite) and `GetTaskBlockers` (open-prerequisite
  read for the DONE-confirm guard). **Sub-editor** (#305): `FetchSubtree`
  (read-only child sub-tree extraction) and `SaveSubtreeBlocks` (atomic
  sub-tree splice through the canonical write chain).
- **Navigation CRUD** — `CreateNotebook` / `OpenNotebook` /
  `PickNotebookFolder`, `CreateSection`, `CreatePage` / `MovePage`
  (cross-section; `section` may be `""`). Silt starts blank — the user opens
  or creates the first notebook from the sidebar.
- **Vault lifecycle** — `CopyVault` / `MoveVault` / `SwitchVault`. The
  SQLite index is never copied: it is reproducible working memory (§0 rule
  4) and rebuilds from markdown at the destination, which sidesteps stale
  absolute-path concerns. `MoveVault`/`SwitchVault` emit `vault:moved`.
- **Portable archive** — `ExportVault` / `ImportVault` (`.silt-vault`:
  validate-before-extract, SHA-256 manifest, zip-slip guarded; format in
  SPECS §3.4). Streams `vault:archive:progress`.
- **Navigation tree** — `ListNavigation` (Notebook › Section › Page tree
  from on-disk folders, block counts merged from the index).
- **Configuration** — `GetSystemConfig` / `SaveSystemConfig` (§8);
  `GetAppVersion`.
- **Per-plugin settings** — `UpdatePluginSetting` (atomic read-modify-write
  in vault `config.yaml`); `GetPluginSettingsForNotebook` resolves a
  plugin's settings for the active notebook, applying the co-located
  per-notebook override layer (linked wins per-key; emits
  `linked-config:changed`).
- **Self-update** — `CheckForUpdates` / `DownloadUpdate` / `InstallUpdate`
  / `GetUpdateSettings` / `SetUpdateSettings`. GitHub release fetch,
  SHA-256-verified before use; the auto-check toggle persists to
  user-global `settings.json`. `InstallUpdate` returns `WillQuit` so the
  frontend quits via the graceful shutdown path (vault + WAL flush).
- **UI persistence** — `GetOpenTabs` / `SetOpenTabs` (pinned tabs only,
  pruned against `ListNavigation`); `GetNavOrder` / `SetNavOrder`;
  `GetSidebarWidth` / `SetSidebarWidth`.

Signatures and per-binding doc-comments live in `app.go` and the `app_*.go`
files; this list is the contract surface, not the source.


4.4 Theme Engine IPC & Pipeline

The theme engine is a four-stage pipeline (DESIGN.md §7 / SPECS.md §6.4): canonical schema -> settings persistence -> loader -> runtime injection. It lives in backend/themes and frontend/src/theme and reuses the existing App-binding -> JSON RPC -> Svelte store IPC topology; it does NOT touch SQLite or the file write lock (the only disk write is AppSettings, via the atomic settings.json writer).

Pipeline (single source of truth shared with DESIGN.md §7 / SPECS.md §6.4):

```
  <vault>/.system/themes/*.json          (on-disk user themes)
          │  +  embed.FS cyber_forest.json (guaranteed fallback)
          ▼
  +----------------------------------------------------------+
  | Go: backend/themes                                       |
  |   validate.go  ParseAndValidate (schema sandbox)         |
  |   loader.go    ListThemes / ResolveActive / LoadByID      |
  |   importer.go  ImportThemeFromPath / ExportThemeToPath    |
  |   cache.go     CachedThemeByID (mtime-aware, launch path) |
  |   default.go   embedded canonical default                |
  +----------------------------------------------------------+
          │  Wails JSON RPC (single Bind: { app })
          │   ListThemes / GetActiveTheme / ApplyTheme
          │   ImportTheme / ExportActiveTheme / PickThemeFile
          │   events: theme:changed | themes:changed
          ▼
  +----------------------------------------------------------+
  | Svelte store (frontend/src/theme/store.svelte.ts)        |
  |   themeState   active id/name/mode + dark/light maps     |
  |   themesState  listing + flat tokens (picker previews)   |
  |   resolves "system" locally via prefers-color-scheme     |
  +----------------------------------------------------------+
          │  injectTokens(tokens)
          ▼
  ONE <style id="silt-theme">:root{ ... }</style>   (one DOM write
                                                    -> one recalc
                                                    -> same-tick repaint;
                                                       index.css :root is
                                                       startup fallback only)

  AppSettings (user-global settings.json): { active_theme, theme_mode }
          ▲  atomic write via vault.SaveSettings
          │  ApplyTheme persists here (the only disk write in the engine)
```

**Storage layout.** Theme files live in `<vault>/.system/themes/*.json` (SPECS §3.2). The **first-class set** (`cyber_forest` plus Terra Noir, Linen, Stark, Graphite) is embedded via `//go:embed themes/*.json` and is what `ScaffoldVault` writes, so each first-class theme has one source of truth. `ListThemes` appends every embedded first-class theme (deduped — on-disk wins), so the full roster is always selectable even on an empty or wiped vault; `ResolveActive` / `CachedThemeByID` resolve a first-class id from the embed when it is not on disk, so a non-default active theme always resolves its palette from the embed, so the default palette never appears. The active id + mode persist to user-global `settings.json` — the only disk write in the engine.

**backend/themes** validates the canonical schema (colors narrowed to `#hex`/`rgb()`/`rgba()` — the import sandbox), loads on-disk + embedded themes (deduped by id), imports/exports atomically, and serves a process-local mtime-aware cache. See the package for per-file responsibilities.

**IPC.** `ListThemes`, `GetActiveTheme`, `ApplyTheme`, `ImportTheme`, `ExportActiveTheme`, `PickThemeFile`. `ApplyTheme` persists to `settings.json` and emits `theme:changed`; `ImportTheme` emits `themes:changed` (the listing event — distinct from the active-theme event). `GetActiveTheme` returns both dark + light maps so the frontend resolves "system" locally without a second round-trip.

**Frontend** (`frontend/src/theme`): `store.svelte.ts` holds `themeState` (active id/name/mode + token maps) and `themesState` (listing + flat tokens for previews); `inject.ts` rewrites a single `<style id="silt-theme">:root{…}</style>` (one DOM write → one recalc → same-tick repaint); `AppearanceTab.svelte` is the accessible picker.

**Launch background.** `main.go` resolves the webview `BackgroundColour` from the in-process theme cache so a non-default active theme's `bg.void` is used for the pre-CSS paint; it falls back to the embedded default when no settings exist or the active id is invalid.


4.5 Template Engine IPC & Pipeline

The template engine mirrors the theme engine's two-tier design (§4.4) but is strictly simpler: there is no "active" template (you insert one, you don't wear one), so there is no settings.json persistence and no SQLite/file-write-lock involvement. Templates are vault-scoped Markdown, read-mostly. The only disk writes are user-template save/delete (atomic, self-write-tracked) and the new-page-from-template write (reuses the CreatePage atomic-write path).

Pipeline (single source of truth shared with SPECS.md §6.5 / docs/TEMPLATES.md):

```
  <vault>/.system/templates/*.md      (on-disk user templates)
          │  +  embed.FS builtin/*.md  (10 first-class defaults, read-only)
          ▼
  +----------------------------------------------------------+
  | Go: backend/templates                                    |
  |   template.go   Template/Placeholder/TemplateSummary     |
  |   render.go     Render (substitution; smart-graph        |
  |                  passthrough; unknown→warn)              |
  |   validate.go   Validate (structured ValidationErrors)   |
  |   default.go    //go:embed builtin/*.md                  |
  |   loader.go     ListTemplates / GetTemplate              |
  |   store.go      SaveTemplate / DeleteTemplate            |
  |   cache.go      mtime-aware CachedGetTemplate            |
  |   watcher.go    fsnotify on .system/templates/           |
  +----------------------------------------------------------+
          │  Wails JSON RPC (single Bind: { app })
          │   ListTemplates / GetTemplate / RenderTemplate
          │   RenderTemplateBlocks / SaveUserTemplate
          │   DeleteUserTemplate / ReloadTemplates
          │   RegisterPluginTemplates / UnregisterPluginTemplates
          │   CreatePageFromTemplate
          │   events: templates:changed
          ▼
  +----------------------------------------------------------+
  | Svelte store (frontend/src/templates/store.svelte.ts)    |
  |   templatesState  listing (TemplateSummary[])            |
  |   initTemplates   load + templates:changed subscription  |
  +----------------------------------------------------------+
          │
          ▼
  TemplatePicker.svelte (modal: search, category groups,
                         live preview, placeholder form,
                         new-page | insert-at-cursor)
```

**backend/templates** validates (id grammar, placeholder grammar, semver schema; categories are additive), renders via a small `{{name}}` substitution — *not* Go `text/template`; the grammar structurally excludes Smart Graph `{{embed:uuid}}` and `((uuid))`, which pass through byte-for-byte — loads on-disk + embedded built-ins (deduped, on-disk wins), saves/deletes user templates atomically, caches (mtime-aware), and watches `.system/templates/` for external edits. See the package for per-file responsibilities.

**IPC.** `ListTemplates`, `GetTemplate`, `RenderTemplate`, `RenderTemplateBlocks`, `SaveUserTemplate`, `DeleteUserTemplate`, `ReloadTemplates`, `RegisterPluginTemplates`/`UnregisterPluginTemplates` (plugin-provided templates, deduped last), `CreatePageFromTemplate`. Emits `templates:changed`. `CreatePageFromTemplate` renders + prepends standard frontmatter + writes atomically + indexes, composing with the `CreatePage` path.

**Frontend** (`frontend/src/templates`): `store.svelte.ts` (`templatesState` listing + `templates:changed` subscription); `TemplatePicker.svelte` (modal: search, category groups, live preview, placeholder form; new-page or insert-at-cursor). Entry points: New Page → From Template (`Ctrl+Shift+T`) and the `/template` slash command.


5. Svelte 5 Frontend Architecture

The frontend uses Svelte 5's fine-grained compiler. The editor surface is built on TipTap v3 (ProseMirror engine) via the `svelte-tiptap` adapter. TipTap provides native cross-block selection and delegates IME/selection edge cases to the framework.

5.1 TipTap Editor Surface (one editor per open tab)

Each **open tab** renders a single TipTap editor instance
(`TipTapEditor.svelte`) containing all of that page's blocks. The tab strip
(`TabStrip.svelte`, directly above the editor in the content area) manages the
standard preview-vs-pinned model: a single-click opens a transient
**preview tab** (reusable slot); a double-click, middle-click, or first edit
promotes it to a dedicated **pinned tab**. Multiple editors coexist (one per
open tab, hidden via `display:none` to preserve per-tab scroll, cursor, and
selection); only the active tab is visible and holds the focus lease. The tab
set + active tab persist across restarts via `ui.open_tabs` / `ui.active_tab`
in `config.yaml` (pinned-only; preview tabs are ephemeral).

**Per-notebook tab scoping.** Tabs are scoped per-notebook: the tab strip and
editor surface display only tabs whose `notebook` matches `activeNotebook`
(the `displayedTabs` derived in `App.svelte`). The full `openTabs` array
(tabs from ALL notebooks) persists to config.yaml, so switching notebooks
preserves each notebook's tab set — the sidebar notebook selector activates
the MRU tab for the newly-selected notebook (or shows the blank state if no
tabs exist for it). Cross-notebook navigation (block references, search jumps)
switches `activeNotebook` via `syncActiveFromTab()`, which in turn updates the
displayed tab set.

The editor's transaction lifecycle is wired to the Go backend:
- **Load:** `FetchPageBlocks(notebook, section, page)` returns a flat `[]ParsedBlock`; `blocksToDoc(blocks)` converts to ProseMirror doc JSON; `editor.commands.setContent(doc)` populates the editor.
- **Save:** `editor.on('update')` (debounced via `editor.auto_save_delay_ms`) → `docToBlocks(editor.getJSON())` → `SaveFileBlocks(notebook, section, page, blocks)`. Go's `RenderFileContent` remains the single on-disk serializer.
- **Focus lock:** the editor's `onFocus`/`onBlur` events drive `Acquire/ReleaseFocusLock`; a 20s heartbeat (`RefreshFocusLock`) keeps the lease alive while focused.
- **Per-tab save-state:** `TipTapEditor` exposes `onSaveStateChange({ dirty, error })` on dirty/error/clean transitions. The callback threads through `VirtualScrollContainer` → `App.svelte`, which writes `TabEntry.dirty` / `TabEntry.saveError`. The tab strip renders a dirty glyph (`circle` icon in `--color-text-muted`) or error glyph (`error` icon in `--color-status-danger`) before the page name, visible from any tab — not just the active one. Controlled by `ui.show_tab_dirty_indicators` (default true). The in-editor footer indicator remains the authoritative surface; the tab glyph is a secondary always-visible hint.

The ProseMirror schema defines block node types that map to `parser.ParsedBlock`:
the three prose types (`taskBlock`, `noteBlock`, `headerBlock`) map 1:1, plus
the additional block primitives — `calloutBlock` (Obsidian `> [!variant]`),
`codeBlock` (managed multi-line fenced code), the TipTap `details`/`detailsSummary`/
`detailsContent` family (foldable sections), and the TipTap `table`/`tableRow`/
`tableCell`/`tableHeader` family (GFM tables). `noteBlock` additionally carries a
`quote` attr (a `> ` blockquote marker, parallel to `bullet`). Each carries a
UUID `id` attr and a per-block `file_date`. A `UniqueBlockIds` extension
(`appendTransaction`) mints fresh UUIDs for pasted/duplicated blocks to prevent
`blocks`-table PK collisions. `calloutBlock` and `detailsContent` use
`content: 'block+'` so a callout or foldable section can nest task lists, code
blocks, tables, and other callouts; this is safe (no silent drop) because the
converter serializer has an explicit branch for every allowed block type — a
plain `>` body line parses back to a paragraph so legacy multi-paragraph
callouts round-trip byte-for-byte.

**Multi-line block model.** The Go parser reads files
line-by-line and `renderBlock` collapses `\n`→space for the prose types
(TASK/NOTE/HEADER). All multi-line block types use ONE unified strategy: the
parser's `accumulateRegion` detects region openers — fenced code (```),
GFM table runs (header + separator), `<details>` HTML, and Obsidian callouts
(`> [!variant]` + consecutive `>` lines) — and accumulates each into ONE
`ParsedBlock` (type CODE/TABLE/DETAILS/CALLOUT) whose `clean_text` retains
internal newlines. `renderBlock` emits them verbatim (no `\n`→space collapse)
with the block identity comment on its own dedicated trailing line, so the
on-disk format stays strictly GFM/HTML/Obsidian syntax (byte-exact interop
with Obsidian / GitHub / VS Code). The frontend converter (`blocksToDoc`) is
a clean 1:1 map (`blocks.map(blockToNode)`). Each multi-line block is one `blocks`-table row, one UUID, one
searchable FTS5 document, and one SDK mutation target.

NodeView components (`TaskBlockView`, `NoteBlockView`, `HeaderBlockView`) render the Svelte UI for each block type — checkbox cycle for tasks, drag handles, meta badges. The slash menu (`/` at block start) surfaces commands to change block types.

**Smart Graph NodeViews.** Two additional schema nodes render Smart Graph syntax as live, interactive elements inside the editor. The converter layer (`frontend/src/lib/editor/converters.ts`) tokenizes `clean_text` and emits the corresponding node types inline within the parent `noteBlock`; on save, the textual tokens are reconstructed byte-for-byte so the on-disk file is round-trip identical.

- `embedNode` (block-level, atomic) — `{{embed:uuid}}` becomes a live `EmbedPortal` NodeView. The portal fetches the referenced block via `ResolveBlockReference` and renders it as a nested live view.
- `blockReferenceNode` (inline, atomic) — `((uuid))` becomes a clickable `BlockReferenceChip` NodeView that navigates to the referenced block via the `navigate-to-block` DOM event.

The NodeView wrappers (`frontend/src/components/editor/EmbedNodeView.svelte`, `BlockReferenceNodeView.svelte`) re-use the existing read-mode `EmbedPortal.svelte` and `BlockReferenceChip.svelte` components — the same rendering pipeline serves both the read-mode (search snippets, standalone embeds) and the NodeView contexts.

5.2 View Mode — Edit ↔ Source toggle

Each tab carries a `viewMode: 'edit' | 'source'` on its `TabEntry` (`frontend/src/lib/tabs.ts`) — the single source of truth for which projection a tab shows. `App.svelte` owns the value; the toggle is the floating icon button in `VirtualScrollContainer`'s action bar (`aria-pressed` + `aria-keyshortcuts`) and the `toggle_view_mode` hotkey (default `Ctrl+Shift+V`, per-vault), both routed through `handleToggleViewMode(tabId)` → the pure `setTabViewMode` state-machine action. The hotkey fires regardless of editor focus (there is no editor-internal keymap for it).

**Persistence.** `viewMode` seeds from the per-vault `editor.default_view_mode` when a tab is created, survives navigation within a session, and persists across restarts on `TabRef.view_mode` in the vault `config.yaml` (the per-vault UI tier — never SQLite; §0 rule 4). Only `"source"` is written (absence = Edit); `normalize()` collapses any other value to `""`. `GetOpenTabs`/`SetOpenTabs` round-trip it as part of the existing `TabRef`.

**Source view.** `MarkdownSourceViewer.svelte` renders the reconstructed raw markdown as a read-only `role="document"` `<pre>` with a line-number gutter and "Copy as Markdown". Syntax is highlighted by **Shiki** via `useMarkdownHighlighter.ts`: a lazy singleton over the markdown grammar, fed by `tokensToShikiTheme` — the single place Shiki meets the Silt theme, mapping the effective `--color-*` token map to a Shiki custom theme. The viewer re-highlights on source / theme-token / mode change (race-guarded async `$effect`) and falls back to plain text until the highlighter resolves and on any error.

**Editor teardown in Source view.** The Edit/Source switch lives in `VirtualScrollContainer`: Source mode renders only `MarkdownSourceViewer` and does **not** mount `TipTapEditor`, so a tab held in Source view pays no editor memory cost (Svelte destroys the ProseMirror editor + NodeViews + listeners on the switch; it rebuilds from `blocks` on return to Edit, since content is on disk via auto-save). Lifecycle safety: `TipTapEditor.onDestroy` flushes the pending save and releases the focus lease, and `hasFirstEdit` is container-scoped so edit-to-pin can't double-fire across a remount. See `docs/editor-memory-profiling.md` for the cost model and the data-gated recommendation.

**Scroll preservation across the round-trip.** `VirtualScrollContainer` captures `containerEl.scrollTop` in a `$effect.pre` the instant a tab leaves Edit (before the editor unmounts and the container height collapses) and restores it after the remounted editor signals readiness — `TipTapEditor` surfaces its internal `editorReady` state to the parent via an `onReady` callback fired in `onCreate`. Restore waits one tick + animation frame (so remounted NodeViews have measured) and clamps to the current scroll height (a doc may have shortened via autosave/fsnotify while the tab was in Source).

**Rich inline & block content.** Three more atomic node types render inside the editor and round-trip their source verbatim through `clean_text`, exactly like the Smart Graph tokens above. **Math** is KaTeX: inline `$...$` is an inline atomic `InlineMathNode`, and a NOTE whose entire body is `$$...$$` becomes a top-level `BlockMathNode` (the sole-content-NOTE path mirrors `embedNode` — block math is never emitted inside inline content, which would violate the schema). A function-based InputRule auto-triggers the inline node on a balanced `$…$` pair (currency-safe: the finder rejects a `$` preceded by `$` and any pair containing internal whitespace, so `5$ cash` / `$5` stay literal). `MathNodeView.svelte` renders KaTeX (`output: 'htmlAndMathml'` for screen readers, `throwOnError: false` so a bad equation shows inline in error color); the `/math` slash command and click-to-edit on an existing node open an in-app LaTeX popover (`MathLatexPopover.svelte`) with a live preview, replacing the native `window.prompt`. The popover is raised by a `silt:edit-math` window event so the editor and the NodeView stay decoupled (the NodeView is non-editable; it carries the latex as an attr). `Ctrl/Cmd+Enter` commits, `Esc` cancels, and an empty equation is rejected; math is implemented as a custom node rather than `@tiptap/extension-mathematics`, so it composes cleanly with Silt's converter/NodeView pipeline. **Mermaid** is a render branch on the existing `codeBlock`: a block whose `language` is `mermaid` renders an SVG via a lazy-loaded `mermaid.js` singleton (`useMermaid.ts`, dynamic import, ~200KB gzipped kept out of the main bundle, `securityLevel: 'strict'`, parse-guarded so invalid source shows a readable error) instead of the Shiki dual-layer; the ```mermaid fence round-trips via the existing `codeBlock.language` attr (Mermaid is a pure view). **@-mention** is an inline atomic `MentionNode` (`@[name]` token, like `((uuid))`); its suggestion list is a **read-only** `SELECT DISTINCT owner FROM tasks` projection surfaced via the `DistinctOwners(prefix)` IPC — SQLite stays working memory, no mention state is stored (§0 rule 4). `DistinctOwners` narrows server-side (`LIKE 'prefix%'`) so a vault with thousands of owners never ships the full list, and the editor caches the unfiltered set on mount with a short TTL plus a 120ms debounce on the prefix-refine path instead of re-fetching on every focus. Confirming a mention inside a `taskBlock` also stamps `[owner:: name]` in the same transaction (single source of truth for the token format via `buildMetaToken`); in a regular paragraph the chip is inserted with no owner write-back. The mention typeahead is a self-contained `Extension.create` mirroring `taskMetaSuggest` (no `@tiptap/suggestion` dependency — the in-repo convention that keeps the suggest logic jsdom-pure).

**Block drag handle.** A Notion-style drag grip is rendered inline inside every block-level NodeView (`NoteBlockView`, `TaskBlockView`, `HeaderBlockView`, `EmbedBlockNodeView`) as a `<span data-drag-handle draggable="true">` — a fixed-column affordance with no layout jitter. The `SiltInlineDragHandle` extension (`frontend/src/lib/editor/siltInlineDragHandle.ts`) listens for `dragstart` on these spans, resolves the top-level block via the wrapper's `data-id`, and populates `view.dragging = { slice, move: true, node: NodeSelection }` so native ProseMirror drop reorders whole blocks (direct manipulation) and `BlockIndentOnDrop` can read `.node.from` for depth-on-drop and the depth-guide overlay. `Alt+ArrowUp/Down` is the keyboard complement (`moveActiveBlock` in `keymaps.ts`, no-`Mod` prefix so it never collides with the `Mod-Shift-Arrow` table bindings). `Delete` at the end of a block and `Backspace` at the start merge the adjacent same-type same-parent sibling's inline content into one block in a single ProseMirror transaction (`mergeSiblingBlock`), preserving the survivor's UUID; cross-type, cross-parent, and `codeBlock` boundaries fall through to the per-type default. Dropping sets the block's indent from the horizontal drop position (drop further right → deeper nesting), reusing the flat `depth` attr the renderer already pads via `[data-depth='N']` — no schema change, no new on-disk field. The depth math (`resolveDropDepth` in `dragIndentDrop.ts`) snaps to a 24px grid matching `--indent-unit` and is extracted as a pure helper so it is jsdom-testable.

The `handleDrop` ProseMirror plugin is deliberately conservative: it returns `true` (and dispatches the indent-aware transaction) only when it can prove the dragged identity, the drop target, and the resolved depth are all unambiguous; on any uncertainty it returns `false` and hands control back to ProseMirror's native reorder-only drop. The identity check is `$old.nodeAfter.eq(draggedNode)` against the drag source's `NodeSelection`, so a stale drag position (e.g. an editor re-render mid-drag) can never delete or indent the wrong block — a false `true` here is document-mutating. The interactive HTML5 drag pipeline has no jsdom equivalent (no real `DataTransfer` / layout-driven `posAtCoords`), so the end-to-end path is gated on the TESTING.md manual matrix.

5.3 Drag-and-Drop Kanban Board

The Kanban board is a first-party plugin (`silt-kanban`, `frontend/src/plugins/first-party/silt-kanban/Kanban.svelte`) that uses the identical `PluginContext` SDK as Agenda and Calendar — no direct `window.go.*` access. It queries tasks via `ctx.sqliteQuery` and shifts status via `ctx.updateBlockState`, preserving the "core feature decoupling" contract (SPECS §8.3).

Cards are rendered as `role="button"` elements with `aria-grabbed`/`aria-label` and animated with Svelte's native `svelte/animate/flip` (200ms cubic-out, per DESIGN.md §6). HTML5 drag-and-drop drives the data; the FLIP animation repositions remaining cards in the same paint frame. Keyboard users change status with ArrowLeft/ArrowRight directly; Enter/click navigates to the source block. The board supports multi-level scope (vault / notebook / section / page) via a segmented control, with the SQL `WHERE` clause built per scope level.

5.4 Search & Writing Aids

Four editor/search features sharing a common substrate (ProseMirror decorations
+ transactions for in-editor work; the existing FTS5 index + atomic
`SaveFileBlocks` write path for cross-vault work). All are config-driven,
per-vault, hot-reloadable, and add zero SQLite schema, zero new write
primitive, and zero new file tier (the per-vault custom dictionary lives in
`editor.custom_dictionary` in `config.yaml` — the YAML tier, §0 rule 2).

**In-page find (Ctrl+F)** — `frontend/src/lib/editor/search/searchExtension.ts`
wraps the official `prosemirror-search` plugin (MIT, by the ProseMirror author).
A `SearchQuery` carries the term + case/whole-word/regex + a scope `filter`
that rejects matches inside fenced `codeBlock`s, the inline `code` mark, and
the `link` mark (URLs); block-identity comments aren't in the editor doc (the
id is a node attr) so they need no filter. The decoration set rebuilds only on
doc/selection/query change (the official invalidation strategy — not every
transaction). `FindBar.svelte` is a `role="toolbar"` overlay with a `1 of N`
counter (aria-live), prev/next, toggles, Esc-to-close. `getMatchCount` /
`getActiveMatchIndex` read the decoration set for the counter.

**In-page find & replace (Ctrl+H)** — extends FindBar with a replace row.
`replaceNextInPage` / `replaceAllInPage` proxy to `prosemirror-search`'s
commands; Replace All iterates matches in reverse in ONE transaction (one undo
step). Regex capture-group substitution (`$1`/`$&`) works in-page.

**Global search enhancements (Ctrl+Shift+F)** —
`SearchBlocksPaged(query, offset, limit, SearchFilters)` adds parameterized
WHERE (notebook/section/tag/type/scope) + sort (relevance=bm25 |
recency=file_date DESC) + a 20-token snippet window. Tag matches the exact tag
OR a hierarchical descendant. `SearchFilters.VaultOnly` scopes to in-vault
blocks. The SearchModal adds a scope segmented control (Vault | +Linked),
category filter chips (single-select by block type), a sort toggle, and a live
count — chips over tabbed categories (the Teams anti-pattern: tabs force a
type-guess + hide cross-type results). Markdown dialect is GFM (§"Markdown
Dialect" in SPECS.md); sub/super are `<sub>`/`<sup>` HTML.

**Global replace (Ctrl+Shift+G)** — `GlobalReplaceModal`
previews FTS5 matches grouped by page (before→after), with per-match + per-page
accept. Apply iterates accepted pages: `FetchPageBlocks` → replace in
`clean_text`/`raw_text` → `SaveFileBlocks` (atomic, self-write-tracked,
re-indexes). A session revert log records the original blocks per page;
"Undo last" restores. Applies to in-vault pages; linked notebooks are
Applies to in-vault pages; linked notebooks are read-only by design.

**Inline spellcheck** — `frontend/src/lib/editor/spellcheck/`:
`dictionary.ts` wraps `typo-js` (pure-JS Hunspell, BSD) loading the bundled
`en-US` dictionary from `frontend/public/dictionaries/en-US/` via `fetch`
(fully local — no network). A per-vault custom-word Set (from
`editor.custom_dictionary`) is layered over Hunspell; a session-ignore Set
backs the "Ignore" menu action. `SpellcheckExtension.ts` is a ProseMirror
decoration plugin that walks text nodes, tokenizes (letters + contractions),
skips camelCase + ALLCAPS acronyms (false-positive reduction), and skips
fenced code blocks (ancestor check), inline code + link (the text node's own
`.marks`), and Dataview `[key:: value]` token RANGES (token-level, not
whole-node). 300 ms debounced rebuild on doc change; `requestSpellcheckRecheck`
forces an immediate rebuild (after dict load / custom-word change). The
`.silt-spell-error` decoration uses `text-decoration: underline wavy
var(--color-status-danger)` + skip-ink + under (WCAG: color+shape, theme-aware).
`SpellcheckMenu.svelte` is the corrections popover (top-N suggestions + Add to
dictionary via the atomic `AddCustomDictionaryWord` IPC + Ignore); right-click
over a misspelled word opens it, and a FormatToolbar spellcheck button opens it
for the cursor's word. No hotkey by design (wavy underline + right-click +
toolbar button).

**Typewriter mode** — `frontend/src/lib/editor/typewriter/TypewriterModeExtension.ts`
is a ProseMirror PluginView.update that, on a keyboard-driven selection/doc
change, sets the scroll container's scrollTop so the cursor lands at
`editor.typewriter_mode_ratio` (default 0.5). Reads config live (toggle applies
without reload). Mouse-driven changes filtered out (mousedown flag).
`handleScrollToSelection` returns true while enabled to suppress ProseMirror's
native make-visible scroll. Always instant (iA Writer; `prefers-reduced-motion`
moot). The `SetTypewriterMode` IPC mirrors `SetFocusMode`'s atomic single-field
toggle.


6. Race Conditions, Locking, & Cooldowns

Running a local-first system that allows concurrent UI actions and external filesystem editing requires robust concurrency protections.

6.1 Multi-Thread Access Locking (Go Mutex Pools)

Because SQLite runs in memory, concurrent reads/writes from the Svelte UI and the fsnotify file monitor must be strictly controlled to prevent database-locked exceptions. The engine routes all file writing and database tasks through an app-wide `core.ExecutionCoordinator`: a per-file `sync.Mutex` map (`LockFileWrite(path, fn)` serializes all writes to a given path, so writes to *different* files don't block each other) plus the DB connection. DB access is serialized via `SetMaxOpenConns(1)`; WAL still allows unlimited concurrent readers (§3). See `backend/core`.


6.2 Viewport Sync Conflict Mitigation

If you edit a markdown file in an external editor while the Silt dashboard is open, the file-watcher triggers a rebuild of the SQLite cache. If Svelte is actively editing the same line, the changes could conflict.

Mitigation Plan:

Focus Locking (TTL leases): While the TipTap editor has focus on a page, the backend monitor holds a time-limited lease on that page's file and pauses external sync operations for it. The editor acquires the lease on focus (`AcquireFocusLock`), refreshes it on a 20s heartbeat while focused (`RefreshFocusLock`), and releases on blur (`ReleaseFocusLock`). One editor per page = one lease per file. The Go side runs a background sweeper (`monitor.DirectoryWatcher.startLeaseSweeper`) that drops expired leases every `TTL/2` (default TTL 60s), so if a component unmounts without releasing — route change, crash, hot-reload — fsnotify suppression self-heals within a minute instead of leaking forever. `RefreshFocusLock` is a no-op on an already-expired lease (the editor must re-acquire), so a stale heartbeat can't resurrect suppression. On shutdown / `CloseVault`, `ReleaseAllFocus` clears every outstanding lease so a clean exit can't strand a file. The `WriteTracker` self-write cooldown is unaffected.

Deterministic Diff Verification: Instead of overwriting entire files when external changes occur, Go computes a diff patch based on block IDs to preserve uncommitted cursor inputs.


7. Plugin Subsystem & Smart Graph Events

7.1 Plugin Loader Pipeline (Frontend)

The Svelte shell discovers and renders plugins at boot:

config.yaml (optional active whitelist)
        │
        ▼
ListPlugins() → .system/plugins/<id>/ folders (skip .disabled sentinel)
        │
        ▼
resolve each id:
   first-party registry (bundled Svelte component)  ──► always available
   on-disk → ReadPluginSource(id) → Blob URL → import(/* @vite-ignore */)
        │
        ▼
plugin.init(ctx: PluginContext)   ←   sqliteQuery (SELECT/WITH-only),
                                      mutateBlock, updateBlockState,
                                      updateTaskMeta, ctx.on (typed event bus)
plugin.onVaultOpen(ctx)             ←   v2 lifecycle hook
         │
         ▼
App view router renders plugin:<id> via PluginView (or Agenda/Calendar slots)

Per-plugin load failures are collected and surfaced (PluginView shows a load-error notice) without aborting boot. The `plugins:changed` Wails event (emitted after install/uninstall/enable/disable) re-runs discovery.

**Vault-switch lifecycle.** The Go `vault:closing` event fires before teardown so the loader can run every plugin's `onVaultClose`/`onShutdown` hook and clear the session registry; it also resets the first-party shared-state module globals (`resetKanbanState` / `resetFocusState`) so a switched vault doesn't inherit the previous scope, board owners/tags, filters, or `focusDate`. A `loadedPlugins.loadersReady` flag gates `PluginContext` construction in `Sidebar.svelte` and `PluginView.svelte`: the flag flips to `false` at the start of teardown and back to `true` once the next `loadPlugins` completes, so a sidebar that remounts during the clear→re-register window never captures a stale/empty session token (and `makePluginContext` is simply not called against a half-torn-down registry). The derived context re-runs on the flag, so the moment the new vault's plugins resolve the sidebar re-binds cleanly.

7.2 v2 SDK Capability & Permission Model

Every privileged v2 SDK binding (file I/O, network, OS integration, editor
schema, rendered UI, content mutation) is gated server-side by
`App.requireGrant(pluginID, capability)`. Grants are per-vault, stored in
`config.yaml` under `plugins.grants` (pluginID → capability → qualifier).
First-use prompts the user (contextual, low-fatigue); Settings → Plugins shows
requested vs. granted with revoke. First-party plugins are implicitly granted.
`exec` is not exposed — it is gated behind the trust/signing model.

Capabilities: `read-files`, `write-files`, `network`, `os-open`,
`os-clipboard`, `os-notify`, `ui-surface`, `editor-schema`,
`content-mutate` (gates block CRUD), `plugin-db` (gates the per-plugin
SQLite store: `ctx.pluginDb.exec` / `query` / `migrate`).

**Binding identity.** High-risk bindings (fetch, file write/delete,
block CRUD) also validate a session token: the loader calls
`RegisterPluginSession(pluginID)` at load time and the SDK closures capture the
token. The Go side verifies `token ↔ pluginID` before `requireGrant` so a
plugin cannot impersonate another by calling a raw binding with a different
pluginID.

**Registry-internal gates.** The three frontend registries
(slash-registry, surfaces, decorations) check `isGranted(pluginID, cap)` from a
Go-provided grant cache — NOT from the plugin's self-declared manifest. A
plugin importing `registerSlashCommand` directly still hits the gate.

**Iframe CSP.** Plugin UI surfaces (iframe srcdoc) carry a restrictive
CSP: `connect-src 'none'` blocks direct fetch/XHR/WebSocket from inside the
iframe. All network traffic routes through the postMessage bridge → `ctx.fetch`
(SSRF-defended + audit-logged).

**Rate limiting.** `PluginFetch` is throttled by a per-plugin token-
bucket rate limiter (default 1 rps, burst 10; manifest `ratelimit` override).
Buckets are evicted on uninstall.

**Network audit log.** `auditNetwork` appends to the in-memory log
(capped 500 entries) under `networkAuditMu`, then enqueues a disk-write op
onto a buffered channel. A single background goroutine (`startNetworkAuditWriter`,
started in `initializeVaultServices`, stopped first in `teardownVaultServices`)
drains the channel and writes to the per-plugin `network.log` WITHOUT holding
the lock, so concurrent `PluginFetch` calls don't serialize on file I/O.
On vault open, `seedNetworkAuditFromDisk` reads the on-disk logs to seed the
in-memory log (before the writer starts). No SQLite table (audit data is not
reproducible from markdown; §0 rule 4).

**Runtime integrity.** `Install` computes `sha256(index.js)` and writes
it into `plugin.json` as `contentSha256`. The frontend loader verifies the hash
via `crypto.subtle.digest` before Blob import. A tampered `index.js` is refused.

7.3 v2 SDK Extended APIs

- Content API: query helpers (queryByTag/queryByDateRange/
  fullTextSearch/getBacklinks/getEmbeds) + block CRUD (createBlock/
  deleteBlock/moveBlock) + page/section/notebook CRUD + bulk ops.
- File I/O: readFile/writeFile/deleteFile/listDir (notebook-scoped,
  traversal-guarded, atomic-write path); scratch space at
  <notebook>/.system/plugins/<id>/data/.
- OS integration: openInNativeHandler, openUrl (scheme-restricted),
  pickers, clipboard, notify — all capability-gated.
- Network/fetch: ctx.fetch via Go net/http proxy (timeout/size/
  redirect caps); SSRF defense at URL validation, redirect re-validation, AND
  dial-time — the custom `DialContext` re-runs `isInternalIP` on every resolved
  IP (DNS-rebinding guard) and fails closed on lookup error so the OS resolver
  cannot bypass the check; audit-logged.
- Editor extension points: slash-command registry; generic embedBlock
  node (round-trips through <!-- silt-embed: {json} --> markers).
- Rendered UI surfaces: sandboxed <iframe srcdoc> + postMessage bridge;
  sidebar panel / modal / status-bar / `note-banner` surfaces; theme tokens
  injected. The `note-banner` kind mounts a dismissible banner host at
  the top of the note view (above the TipTap editor); first-party banners
  render a compiled Svelte component, third-party via the iframe bridge. The
  bridge is bidirectional: iframe→host requests (PluginContext proxy) AND
  host→iframe events. The close affordance sends a `dismiss` event so the
  plugin can persist dismissal state (`updatePluginSetting('<id>',
  'dismissed_notes', [...])`) — `updatePluginSetting` is in the bridge's
  `allowedMethods` so the documented pattern is reachable from a sandboxed
  banner. When more than two banners stack, the host collapses them into a
  single expandable summary. Bespoke plugin settings pages
  mount inside a `<svelte:boundary>` so a component that throws on render
  cannot crash the focus-trapped Settings dialog.
- Settings schema: declarative `SettingSchema[]` on the manifest;
  generic form renderer is the default. A plugin may instead declare a
  **bespoke settings page** — first-party as a compiled Svelte
  component, third-party via the `settings-panel` iframe surface — rendered
  as a dynamic tab in the Settings shell. A plugin declares *either* the
  bespoke page *or* the generic schema form, not both; bespoke pages persist
  through the same `updatePluginSetting` / `getPluginSettings` plumbing (no
  new storage path).
- Per-plugin SQLite store: `plugin-db` capability gates
  `ctx.pluginDb.exec` / `query` / `migrate` against
  `<vault>/.system/plugins/<id>/data/plugin.db` — a distinct connection from
  the core index (never `ATTACH`-able). `sqlite-vec` is registered on every
  plugin connection (`vec0` virtual tables + `vec_distance_cosine`) via the
  pure-Go `modernc.org/sqlite/vec` blank import. The plugin owns its schema
  and chooses durability semantics (working memory or durable). See §0 rule
  4 plugin carve-out and ADR `docs/decisions/0001-plugin-storage-tier.md`.

See `frontend/src/plugins/sdk.ts` for the full typed contract and
`docs/PLUGIN_DEVELOPMENT.md` §8 for the author guide.

7.4 PluginContext → Go Bindings

PluginContext is a thin frontend wrapper over four Wails bindings on App:

- PluginRawQuery(sql, params) — read-only; rejects anything not starting with SELECT/WITH; routed through ExecutionCoordinator.WithDBRead; returns row maps.
- PluginMutateBlock(id, text) / PluginUpdateBlockState(id, status) — wrap MutateBlock / UpdateBlockState (same atomic-write + re-index + lock path as the core editor).
- GetPluginRegistry() / ListPlugins() / ReadPluginSource(id) — discovery.
- ValidatePluginArchive / PickPluginArchive / InstallPlugin / UninstallPlugin / EnablePlugin / DisablePlugin — `.silt-plugin` distribution (see backend/plugins package; zip-slip + traversal guarded, atomic extract).

7.5 Smart Graph Events

Block mutations broadcast a `block:changed` Wails event (BlockChangedEvent {ID, Notebook, Section, Page, FileDate}) so live embeds (`{{embed:uuid}}`) and references (`((uuid))`) refresh in real time. Emitted from MutateBlock, UpdateBlockState, and the post-write path of SaveFileBlocks; emission no-ops when ctx is nil (tests). The frontend EmbedPortal subscribes via EventsOn and re-fetches its source block when the event matches its uuid (a module-scoped render-stack guard stops recursive embed loops). When a block transitions to DONE, `UpdateBlockState` also fans the event out to every dependent task (those `[blocked_by::]` the just-completed block) so the Kanban/Agenda "blocked" badge and the DONE-confirm guard re-evaluate (#301).

---

8. System Configuration Engine (config.yaml)

Global settings — editor defaults, parsing rules, hotkeys, and the plugin registry — live in <vault>/.system/config.yaml, the single source of truth for everything except the vault path (which stays in OS-config settings.json because it must be known before any vault can be opened).

8.1 Parser (backend/config)

config.SystemConfig mirrors the SPECS §10.1 schema (notebooks / editor / parsing / hotkeys / plugins / ui). The `ui.*` block holds per-vault UI preferences: `sidebar_width`, `nav_order` (explicit section/page ordering for drag-to-reorder), `open_tabs` / `active_tab` (pinned-tab persistence — preview tabs are ephemeral), `enable_preview_tabs`, `max_open_tabs`, `show_format_toolbar`, `show_tab_dirty_indicators` (default true), `dismissed_tips`, and `formatting.*` toggles. Load(vaultPath) decodes over config.Defaults() so omitted sections keep their default values rather than being zero-valued; a missing file returns defaults (non-fatal), but a file that exists and fails to parse returns an error (fail-loud — never silently fall through). Save(vaultPath, cfg) is atomic (temp file + fsync + rename), matching the durability guarantee of note writes. The App holds the parsed config under configMu and replaces it wholesale on reload (never mutated in place), so a struct read under RLock is a safe snapshot.

8.2 Hot-Reload (backend/config.ConfigWatcher)

A dedicated fsnotify watcher observes the .system parent directory (not the file alone) so a delete+recreate of config.yaml is still observed. Self-loop prevention is a local time-window in ConfigWatcher: SaveSystemConfig calls RegisterSelfWrite() before the atomic write, and the watcher ignores every config.yaml event until a 500ms window elapses — a single logical save can emit several fsnotify events (atomic temp+rename, or truncate+write), so the window suppresses all of them, not just the first. External edits re-parse and invoke onChange → App.applyConfig (updates live knobs + emits config:changed); a parse failure invokes onError → config:error (last-good config retained). This implements SPECS §10.2 without an application restart.

8.3 Settings Menu (frontend)

The settings store (settings/store.svelte.ts) is a $state object exposing loadConfig/saveConfig, dirty tracking, and a config:changed / config:error subscription. The SettingsShell is a full-screen frosted overlay with a left tab rail (General / Appearance / Plugins / About), roving keyboard navigation (Arrow/Home/End, Esc to close), and ARIA tablist semantics. GeneralTab edits a local draft (Save/Revert) so an external hot-reload cannot fight a half-edited form; if an external change lands while the draft is dirty, the draft is preserved and a non-blocking "reload" notice is shown (never a silent clobber). The Plugins tab is the single plugin UI: rich cards (first-party bundled vs. third-party installed), enable/disable (all plugins — first-party via config.yaml `plugins.disabled` list, third-party via `.disabled` sentinel), uninstall (third-party only), inline load errors, an expandable detail panel with per-plugin settings, and the .silt-plugin install flow. The titlebar extension icon opens Settings → Plugins.

8.4 Editor Config Consumer (frontend)

The editor-token pipeline (settings/editor-tokens.svelte.ts) mirrors the theme injector pattern (§4.4): editor.* config values (font_family, mono_font_family, font_size_px, line_height) are injected as CSS custom properties (--editor-font-family, --editor-mono-font-family, --editor-font-size, --editor-line-height) on :root via a dedicated <style id="silt-editor"> element, separate from the theme injector's <style id="silt-theme">. initEditorTokens() uses $effect.root to watch the reactive settings store, so config changes apply live (one DOM write → one recalculation → same-tick repaint) without a reload or remount. The index.css :root values are startup fallbacks only.

TipTapEditor (the live block editor, frontend/src/components/TipTapEditor.svelte)
consumes the full editor.* config surface: typography flows through the CSS
variables (font-family, font-size, line-height on the contenteditable);
auto_save_delay_ms drives the triggerAutoSave debounce; focus_highlight_ancestors
gates the guide-rail active highlight; show_word_count toggles a subtle
CharacterCount display; focus_mode dims non-active paragraphs; and
indent_block / unindent_block hotkeys are matched via matchHotkey
(settings/hotkeys.ts). The cycle_view_layout hotkey is wired in App.svelte's
global keydown handler alongside open_search, toggle_sidebar, and
toggle_view_mode. Inline formatting marks, block alignment,
text/background color, and the source/edit view toggle are all
additive to clean_text — the Go parser sees formatted text as opaque and
requires zero parser changes.

**Markdown dialect.** Silt's on-disk base dialect is **GFM
(CommonMark + GFM)** with a documented set of Silt-specific extensions layered
on top (Obsidian callouts, Dataview `[key:: value]` metadata, Smart Graph
`((uuid))`/`{{embed:uuid}}` refs, block-identity comments, `<sub>`/`<sup>`
HTML for sub/super, `$math$`, `[^footnotes]`). Pandoc is a downstream
converter (`pandoc -f gfm`), not a dialect; Pandoc-native authoring is a
future plugin. See SPECS.md "Markdown Dialect" for the full rationale
and the reversibility analysis. Sub/super use `<sub>`/`<sup>` HTML (not
Pandoc's `~x~`/`^x^`, which render as literal text on GitHub).

**Hotkey scheme.** Hotkey defaults live in one place —
`config.go` `Defaults()` — and every display surface (slash-registry hints,
FormatToolbar/TableContextToolbar tooltips, `aria-keyshortcuts`) derives from
`settings.config.hotkeys` via `resolveHotkeyDisplay` (settings/hotkeys.ts), so
a user remap is reflected everywhere with no drift. The defaults are
convention-anchored (see SPECS.md sample): Google Docs wins ties over MS
Office; Office/Docs win over code editors for shared actions; VS Code/Sublime/
Notepad++ fill gaps where Office/Docs have no opinion. Windows/Linux only
(`Ctrl` everywhere). Spellcheck deliberately has no hotkey (wavy underline +
right-click + a FormatToolbar button). `Load()` decodes over
`Defaults()`, which is the single source of truth for hotkeys. Paste is not in the hotkey map:
`Ctrl+V` is ProseMirror's native rich paste, `Ctrl+Shift+V` inserts the
clipboard as plain text (PlainPaste extension, lib/editor/plainPaste.ts).


9. Performance Budgets

9.1 Boot-Scanner Budget (Hard Regression Gate)

TestScanWorkspace_BudgetRegression (backend/parser/parser_test.go) seeds 1,000 small page files and asserts ScanWorkspace completes in under 450ms (baseline ~280ms on Ryzen AI MAX+ / Go 1.25 / Windows). The test runs in the normal `go test -race ./...` CI gate (skipped under `-short`) so a regression is caught immediately, not only when someone runs `-bench`.

9.2 Atomic-Write Safety (Kill-Mid-Write WAL Recovery)

TestAtomicWrite_KillMidWriteRecoversViaWAL (backend/db/db_test.go) simulates a destructive exit (SIGKILL / power loss) by closing the raw `*sql.DB` handle WITHOUT the `PRAGMA wal_checkpoint(TRUNCATE)` that `DatabaseManager.Close` performs. A subsequent `NewDatabaseManager` (the "next launch") auto-replays the WAL, recovering every committed block. The test also asserts zero stray `*.tmp` files in the vault directory. TestWriteFileAtomic_NoTruncatedFilesOnKill verifies 100 concurrent atomic writes to different files leave no truncated content.

9.3 UI Frame-Budget Probe

frontend/src/lib/perf/frame-budget.ts provides `measureFrameBudget(label, fn)` — a dev-only probe (gated on `?perf=1` in the URL; zero-cost pass-through otherwise) that wraps a callback in `performance.mark`/`measure` + `requestAnimationFrame` and logs the elapsed time against the 16ms frame budget. Instrumented on the three highest-stress paths: Kanban drag-drop settle, TipTap editor transaction (docToBlocks), and theme-token injection.
