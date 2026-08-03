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
| **Per-vault UI preferences** | YAML | `<vault>/.system/config.yaml` | Per-vault, per-plugin settings: active/disabled plugin list, Tasks hub saved views + display mode/grouping/sort, hotkey bindings, editor font sizes, theme typography overrides, canonical navigation order, expanded section locators, bounded recent pages, favorites (Quick Access pins), and pinned tabs | `ui.expanded_sections: [{notebook: Work, path: Projects}]` |
| **Per-linked-notebook overrides** | YAML | `<linkedRoot>/.system/config.yaml` | Per-notebook plugin setting overrides for a linked (external) notebook. Read-only to Silt (user-authored); deep-merged over the vault defaults (linked wins per-key). See §3.1. | `plugins.plugin_settings.silt-tasks.default_group_by: status` |
| **Per-vault schema assets** | YAML | `<vault>/.system/types/*.yaml` | Type schemas: property definitions, hero field, target-type constraints for `page`/`pages` relations. Sibling to `.system/templates/` and `.system/themes/`. User-authored or shipped defaults; durable and portable. See ADR `docs/decisions/0008-typed-notes.md`. | `TypeDef` YAML with `properties: [{name, type, target, ...}]` |
| **User-global, pre-vault** | JSON | `<config>/silt/settings.json` | Settings that must be known before any vault is open: active theme id, dark/light/system mode, non-vault font preferences | `{"active_theme": "silt-graphite", "mode": "dark"}` |
| **Working memory** | SQLite (WAL) | `<DataDir>/silt/indexes/<vault-key>/index.sqlite*` (per-user local DataDir; relocated OUT of the synced vault) | Re-derivable caches: block↔location projection, FTS5 search index, denormalized per-task caches (comments/links counts, pin, progress — all re-derived from markdown on re-index), file mtime/size for incremental re-index, typed-notes projection (`page_types`/`page_properties` — page→type membership + property values, re-derived from frontmatter `type:` + the type schema) | The `blocks` table, `blocks_fts` virtual table, `files` mtime cache, `page_types`/`page_properties` projection |
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
   **core index** (relocated to a per-user local DataDir at
   `<DataDir>/silt/indexes/<vault-key>/index.sqlite*`, out of the synced vault)
   MUST be reproducible from the markdown + YAML above. The recovery path for
   any core-index corruption is *delete the index file and relaunch* — the
   documented, supported operation. The core index holds the block↔location projection, FTS5, file
   mtime/size caches, and re-derived per-task caches (comments/links counts,
   pin, progress, lifecycle timestamps/sort position — re-derived from
   markdown `[pin:: true]` / `[progress:: N]` / `[created::]` / `[completed::]`
   / `[order:: N]` tokens on every re-index; plus NOTE-block comment
   attribution `[author::]` / `[ts::]` into the sparse `block_meta`
   projection). It is **forbidden** to hold user intent *as the source of
   truth* there: pin state, progress, custom column names, filter
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

- **Standalone tasks** — tasks created from a quick-add surface (any
  silt-tasks quick-add surface — Calendar day cell, Board column footer,
  List footer, global `Mod+Shift+N`) that aren't attached to a note
  live as GFM checkboxes in `<vault>/.silt/tasks.md`, indexed under a
  synthetic hidden `.silt` notebook (no new SQL table, no nullable
  `block_id`). The silt-tasks hub is the only user-facing surface
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
|   │ Wails v3 Runtime (@wailsio/runtime)                       │               │   |
|   +───────────────────────────────────────────────────────────┼───────────────+   |
+────────────────┼──────────────────────────────────────────────┼────────────────---+
                 │                                              │
                 │ JSON RPC (WebView2 IPC)                      │ IPC Event Dispatch
                 ▼                                              │
+────────────────┼──────────────────────────────────────────────┼────────────────---+
|                │          BACKEND PROCESS BOUNDARY (Go Core)  │                   |
|   +────────────▼───────────────+                              │                   |
|   │ Wails v3 Service Dispatcher│                              │                   |
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

**Block model.** A GFM checkbox item (`- [ ]`, `- [/]`, `- [x]`) is a task; the remainder of any line is scanned for Dataview-style `[key:: value]` metadata tokens (due, start, owner, priority, pin, progress, recur, blocked_by, created, completed, order) — order-independent and extensible via the `scanTaskTokens` dispatch. NOTE blocks are scanned separately for two comment-attribution tokens (`author`, `ts`) via `scanNoteTokens`; the TASK and NOTE token spaces are disjoint by design, so comment attribution never leaks into task queries and task metadata never lands on a note. Each parsed line becomes a `ParsedBlock` typed as one of three prose types (`TASK`, `NOTE`, `HEADER`) or one of four multi-line region types (`CODE`, `TABLE`, `DETAILS`, `CALLOUT`).

**Multi-line region blocks.** The parser's `accumulateRegion` detects four region shapes — fenced code, GFM table runs (header + separator), `<details>` HTML (depth-counted), and Obsidian callouts (`> [!variant]` + consecutive `>` lines) — and collapses each into one managed `ParsedBlock` (one `blocks`-table row, one UUID, one FTS5 document). The block-identity comment lives on its own dedicated trailing line after the region so the on-disk format stays strictly GFM/HTML/Obsidian syntax (byte-exact interop with Obsidian/GitHub/VS Code). `ParseFileContent` and `RenderFileContent` share the region-boundary helpers (`detectRegionKind` / `findRegionCloser` / `skipManagedRegion`) so both paths agree. Legacy files with per-line id comments are detected (id comments stripped before matching), migrated to the trailing-id format on first parse, and `((uuid))` references to vanished per-line ids are remapped to the region block's id.

**Block identity.** If a block lacks an `<!-- id: UUIDv4 @ YYYY-MM-DD -->` trailing comment, the parser mints one, rewrites the line, and flags the file for atomic rewrite. The id is the only identifier stored in the file; everything else (status, position, metadata) is derived from the line.


3. SQLite Schema & Query Optimization Layer

The storage-of-truth contract — what each tier holds, and the rule that the
core index is reproducible working memory rather than a system of record —
lives in §0 above. This section covers the core index's on-disk mechanics
(WAL, incremental re-index) and the concrete schema.

The on-disk SQLite lives in WAL mode at a per-user local DataDir
(`<DataDir>/silt/indexes/<vault-key>/index.sqlite`, + `.sqlite-wal` +
`.sqlite-shm`), relocated out of the synced vault so a cloud-sync engine or
antivirus cannot lock or corrupt it. On restart only files whose
`mtime`+`size` differ from the last successful index are re-parsed and
re-indexed; a cold start (no index file yet, or the index file deleted by the
user) performs a full scan and rebuild. The recovery
path is documented and intentional: deleting the index file is safe
because every row in it is re-derivable from the markdown + YAML on the
next launch. This durable, incremental model is
what lets Silt scale to dozens of notebooks and thousands of pages
without rebuilding the whole index on every launch.

Connections are opened by `db.NewDatabaseManager(dbPath)` (pass `""` for an ephemeral in-memory shared-cache DB, used in tests and before a vault is open). The DB runs in **WAL mode** — persistent in the file header, so every later connection (including the plugin SDK's read-only handle) inherits it without re-running the pragma. Per-connection pragmas are configured for WAL safety and performance; see
`backend/db/schema.go` for the values.

Concurrency: WAL allows unlimited readers alongside a single writer; readers never block writers and the writer never blocks readers. The Go-level `core.ExecutionCoordinator` serializes all access (`SetMaxOpenConns(1)`) so the locking story stays simple. `DatabaseManager` additionally owns its handle lifecycle: package methods take a read lease via `handle()` / `withDB` (nested helpers must not re-enter `handle` — use `*sql.Tx` or `ensureFTSOn(db)`); `Close` takes the write lock, swaps the live `*sql.DB` to nil, checkpoints, and closes so leases drain first and post-close calls return `ErrDBClosed` instead of nil-derefing (vault-switch races). App IPC query/write paths use typed package methods (`CountBlocksGroupedByPage`, `GetBlockReference`, `MarkFilesIndexed`, …); `SQLDB()` remains only for coordinator bootstrap at vault open (handle is live and single-threaded there) and test fixtures. The coordinator cannot stop a third party from closing the handle; the manager is self-protecting at the package API. Clean shutdown runs `PRAGMA wal_checkpoint(TRUNCATE)` (in `DatabaseManager.Close` and after each startup re-index pass) so the WAL does not grow unbounded across sessions; on a crash, SQLite auto-recovery replays the WAL on the next open.

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
    comments_count INTEGER DEFAULT 0, -- derived: NOTE descendants under the task
    links_count INTEGER DEFAULT 0,    -- derived: ((uuid)) references in body
    created_at TEXT,                  -- ISO 8601 local [created::] timestamp; NULL when absent (no backfill); reproducible from markdown
    completed_at TEXT,                -- ISO 8601 local [completed::] timestamp; NULL when not DONE (no backfill); reproducible from markdown
    manual_order INTEGER,             -- 1-based [order:: N] sort position; NULL when absent (no backfill); reproducible from markdown
    modified_at TEXT,                 -- ISO 8601 local [modified::]; NULL when absent; reproducible from markdown
    estimate_minutes INTEGER,         -- minutes from [estimate::]; NULL when absent; reproducible from markdown
    subtask_total INTEGER DEFAULT 0,  -- derived: direct TASK children
    subtask_done INTEGER DEFAULT 0,   -- derived: direct TASK children with status DONE
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
-- the DONE-branch fan-out and the silt-tasks "blocked" badge.
CREATE TABLE task_dependencies (
    block_id      TEXT NOT NULL,
    blocked_by_id TEXT NOT NULL,
    PRIMARY KEY(block_id, blocked_by_id),
    FOREIGN KEY(block_id)      REFERENCES blocks(id) ON DELETE CASCADE,
    FOREIGN KEY(blocked_by_id) REFERENCES blocks(id) ON DELETE CASCADE
);
CREATE INDEX idx_task_deps_blocked_by ON task_dependencies(blocked_by_id);

-- Block-meta projection (NOTE-block comment attribution): caches the
-- [author:: NAME] / [ts:: YYYY-MM-DDTHH:MM:SS] tokens parsed off NOTE
-- blocks. Sparse — a row exists ONLY for NOTE blocks carrying at least
-- one of the tokens, so the majority of NOTE blocks and every TASK /
-- HEADER have no row. Kept in a SEPARATE table from `blocks` so `blocks`
-- stays the pure block↔location projection; the cache is disposable and
-- re-derived from markdown on re-index (rule 4). FK ON DELETE CASCADE
-- mirrors task_dependencies: a deleted block cleans up its meta row.
-- `author` (comment authorship) is distinct from the task `Owner`
-- (assignee); `timestamp` is distinct from the date-only block-identity
-- `file_date`. The TASK and NOTE token spaces are disjoint by design.
CREATE TABLE block_meta (
    block_id  TEXT PRIMARY KEY,
    author    TEXT,
    timestamp TEXT,
    FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
);

-- Block References (#704): the ((uuid)) block-ref and {{embed:uuid}} embed
-- edge graph parsed off block bodies. Each row is one distinct
-- (source_block_id, target_block_id, kind) edge; the PK collapses same-kind
-- duplicate tokens in one source block. Re-derivable from markdown (rule 4);
-- the markdown is the source of truth. Source-only FK by design: the source
-- edge survives a missing target (deleted target, not-yet-indexed target,
-- hand-edited markdown, a file indexed later), and the backlink re-resolves
-- when the target subsequently appears without a source re-index. Target
-- existence is resolved at query time by joining against the live blocks
-- rows for the target page, so a dangling edge is silently inert. The
-- reverse-lookup index on (target_block_id, kind) serves the backlinks
-- panel's `target_block_id IN (...)` lookup, replacing the prior
-- leading-wildcard raw_content LIKE scan (ADR 0006).
CREATE TABLE block_references (
    source_block_id TEXT NOT NULL,
    target_block_id TEXT NOT NULL,
    kind            TEXT NOT NULL,  -- 'block-ref' | 'embed'
    PRIMARY KEY (source_block_id, target_block_id, kind),
    FOREIGN KEY(source_block_id) REFERENCES blocks(id) ON DELETE CASCADE
);
CREATE INDEX idx_block_references_target ON block_references(target_block_id, kind);

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

-- Typed-notes projection (re-derived from frontmatter type: + schema).
-- page_types: page→type membership. page_properties: one row per SET
-- property value (sparse, like block_meta). value_sort is a coercion for
-- cross-type ordering (number→numeric text, date→ISO). Both source-aware.
-- See ADR docs/decisions/0008-typed-notes.md.
CREATE TABLE page_types (
    source     TEXT NOT NULL,
    notebook   TEXT NOT NULL,
    section    TEXT NOT NULL,
    page       TEXT NOT NULL,
    type_name  TEXT NOT NULL,
    PRIMARY KEY (source, notebook, section, page)
);
CREATE TABLE page_properties (
    source      TEXT NOT NULL,
    notebook    TEXT NOT NULL,
    section     TEXT NOT NULL,
    page        TEXT NOT NULL,
    type_name   TEXT NOT NULL,
    property    TEXT NOT NULL,
    value_text  TEXT,
    value_sort  TEXT,
    value_type  TEXT NOT NULL,
    PRIMARY KEY (source, notebook, section, page, property)
);
CREATE INDEX idx_page_types_type ON page_types(type_name);
CREATE INDEX idx_page_properties_type_prop ON page_properties(type_name, property, value_sort);


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
The index stays LOCAL (relocated to a per-user DataDir, outside any synced
mount); only the markdown content (and any co-located `<root>/.system/`) lives
on the remote mount.

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

Navigation authority. `ListNavigation` walks the filesystem recursively for
vault notebooks and for linked roots whose fingerprint is trusted and whose
root is accessible. It preserves empty sections and returns full relative
section paths. A disconnected linked notebook uses its last indexed rows only
as an offline fallback, including its disconnected status; preference
reconciliation does not prune locators for that incomplete fallback. The
filesystem tree remains authoritative whenever the root is available.

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
reactive refreshes (e.g. Tasks hub saved views/filters re-resolve on the switch).

Resolution surface: `App.GetPluginSettingsForNotebook(pluginID, notebookName)`
is the IPC binding that resolves a plugin's settings for the active notebook
(vault → vault settings verbatim; linked → deep-merge). The SDK
`PluginContext.getPluginSettings()` wraps it with the live `activeNotebook`
reactive getter, so a plugin that calls it at render time always sees the
merged settings for the current notebook.


4. Wails Bridge & IPC API Contract

Communication between Svelte and Go occurs over a typed JSON bridge. The following API commands are registered with the Wails v3 framework as a single service.

4.1 Block Mutation Envelope

type MutateBlockPayload struct {
	ID        string `json:"id"`
	FilePath  string `json:"file_path"`
	NewText   string `json:"new_text"`
}


4.2 Query Filter Envelope (silt-tasks)

type TaskQueryFilter struct {
	Owner     string   `json:"owner"`
	Priority  int      `json:"priority"`
	Tags      []string `json:"tags"`
	StartDate string   `json:"start_date"`
	EndDate   string   `json:"end_date"`
}


4.3 IPC Service Surface

All bindings hang off the single Wails v3 service (`*App` registered via `application.NewServiceWithOptions`) and are
auto-exposed to the frontend as JSON RPC. Grouped by domain:

- **Block I/O** — `FetchPageBlocks`, `SaveFileBlocks`, `FetchPageMarkdown` / `SavePageMarkdown` (raw source body), `UpdateBlockState`
  (task-checkbox transition + atomic file rewrite + re-index; returns the spawned
  recurrence instance's UUID on a recurring TODO/DOING→DONE transition, `""`
  otherwise — the `Plugin*` wrapper surfaces it as `{ok, spawned_id}`),
  `MutateBlock`, `QueryTasks` (dashboard filter query). **Task dependencies**:
  `SetTaskBlockedBy` / `PluginSetTaskBlockedBy` (cycle-checked
  `[blocked_by::]` token rewrite) and `GetTaskBlockers` (open-prerequisite
  read for the DONE-confirm guard). **Task metadata setters**:
  `SetTaskOwner` / `SetTaskPriority` / `SetTaskTags` / `SetTaskTitle`
  (and their `Plugin*` SDK wrappers) follow the same atomic-rewrite +
  `block:changed` shape as `SetTaskDueDate` / `SetTaskRecurrence`.
  `SetTaskOrder` / `PluginSetTaskOrder` rewrite the 1-based `[order:: N]`
  manual-sort token (clears it on `0`; negative values are rejected up
  front so a UI glitch can't stamp an off-by-one into the file).
  `SetTaskOrders` / `PluginSetTaskOrders` batch the same `[order:: N]`
  rewrite across multiple task blocks in one atomic per-file write (a single
  drag-reorder shifts several rows; batching avoids N round-trips and N file
  re-parses, and the per-file write lock keeps each batch atomic).
  `SetTaskTags` takes the full new tag set (the backend does the surgical
  `#hashtag` add/remove on the prose); `SetTaskTitle` rewrites only the
  prose, preserving `#tags`, `((uuid))` refs, and inline `[key:: value]`
  tokens. **Sub-editor**: `FetchSubtree`
  (read-only child sub-tree extraction; also hydrates `block_meta`
  comment attribution for NOTE children) and `SaveSubtreeBlocks` (atomic
  sub-tree splice through the canonical write chain). **Local author**:
  `GetLocalAuthor` returns the host OS username (the default seed for the
  per-vault `local_author` pref that attributes Task comment threads).
- **Navigation CRUD** — `CreateNotebook` / `OpenNotebook` /
  `PickNotebookFolder`, `CreateSection(notebook, parentPath, name)`,
  `CreatePage` / `MovePage` (cross-section; `section` may be `""`),
  `DuplicatePage`, and `RevealPageInOS`. Section/page locators are canonical
  relative paths; duplicate stays in the resolved source root and mints fresh
  block identities; reveal resolves and guards the page server-side. Silt starts blank — the user opens
  or creates the first notebook from the sidebar.
- **Vault lifecycle** — `CopyVault` / `MoveVault` / `SwitchVault`. The
  SQLite index is never copied: it is reproducible working memory (§0 rule
  4) and rebuilds from markdown at the destination, which sidesteps stale
  absolute-path concerns. `MoveVault`/`SwitchVault` emit `vault:moved`.
- **Portable archive** — `ExportVault` / `ImportVault` (`.silt-vault`:
  validate-before-extract, SHA-256 manifest, zip-slip guarded; format in
  SPECS §3.4). Streams `vault:archive:progress`.
- **Navigation tree** — `ListNavigation` (Notebook › Section › Page tree
  from recursive on-disk folders, block counts merged from the index, linked
  source/disconnected metadata preserved).
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
  pruned against `ListNavigation`); `GetNavOrder` plus narrow serialized
  mutations `SetNavNotebookOrder`, `SetNavSectionOrder`,
  `SetNavPageOrder`, and the corresponding clear methods; `GetSidebarWidth` /
  `SetSidebarWidth`; and narrow navigation-preference bindings for expanded
  sections, recents, and favorites. Navigation-order and preference writes
  merge against current config under the vault→config lock order; no whole
  navigation snapshot setter is a competing source of truth.

- **Navigation discovery** — the canonical tree feeds source-aware recents and
  favorites, a breadcrumb for the active notebook/section/page, a dedicated
  page switcher, and tab-overflow discovery. These surfaces continue to open
  pages through the application page-opening funnel rather than bypassing tab
  and notebook scoping.
- **Typeaheads & backlinks** — `SearchPages` powers the `[[` page-link
  autocomplete picker after two non-space query characters. It returns the
  rank-correct, case-insensitive top 50 over distinct page paths (exact name,
  page-prefix, path-prefix, then substring; deterministic ties). `RecordTagUsage`
  validates a bounded tag path before maintaining an MRU
  `recent_tags` list (capped at 12) that seeds the `#` tag-typeahead above
  full index tags. `GetBacklinksPaged` returns bounded, cursor-paged inbound
  references to a page
  across three legs — `[[…]]` page-links (indexed `page_links` reverse lookup,
  resolved against the canonical page set), `((uuid))` block-refs, and
  `{{embed:uuid}}` embeds (both via the indexed `block_references` reverse
  lookup, parameterized by the target page's block IDs) — source-aware,
  deduped, stably sorted. Pagination bounds IPC and DOM work; collection cost
  is proportional to inbound edge count, not total block count. See ADR
  `docs/decisions/0006-backlinks-query-strategy.md`.   `GetUnlinkedMentionsPaged`
  surfaces residual plain-text mentions of the current page title — an FTS5
  phrase query (`clean_content : "title"`) rides the existing `blocks_fts`
  index. Each **FTS window** is a **rowid-keyset** probe: nested
  `SELECT rowid FROM blocks_fts … ORDER BY rowid LIMIT unlinkedScanCap+1`
  bounds MATCH work before joining `blocks` (path-ordered join plans
  TEMP-sort the full match set; the FTS subquery does not). CODE and self-page
  rows are dropped in Go after each probe; the scanner **loop-fills** up to a
  small round cap (`unlinkedScanFillRounds`) until it has `unlinkedScanCap`
  keepers or FTS is exhausted — still O(rounds×cap), never O(all vault matches).
  When the window fills with more FTS hits beyond it, the envelope sets
  `truncated: true` and `scan_cursor` (opaque keyset: exclusive lower-bound
  **rowid observed at scan time**, plus diagnostic block id — never re-resolved
  to a live UUID rowid, which would skip unread matches if the anchor is
  re-indexed to a higher rowid; legacy UUID cursors soft-reset). Implicit
  `blocks` rowids are monotonic in practice for this workload; rare SQLite
  rowid reuse is bounded by the truncated surface and client block-id
  merge/dedup — do not reintroduce live rowid lookup. Page-level
  `has_more` / `cursor` only page residual source pages **within the current
  batch** — orthogonal to `truncated` / `scan_cursor`. Residual presentation
  is path-sorted in Go; scan order only defines the batch window. Further
  batches are user-gated (**Scan more**); the UI blocks Scan more while residual
  Load more remains for the current batch so unread residual pages are not dropped.
  **Residual Load more** reuses a short-lived process-local **candidate-window
  cache** on `DatabaseManager` keyed by `(source, notebook, section, title,
  scan_cursor input)` plus a generation counter: the FTS loop-fill runs once
  per window; residual plain-match + path-sort + residual keyset paging run on
  the cached keepers. Cache entries miss when generation bumps (any successful
  `IndexFileBlocks` / `IndexScanResults` / block delete / `ClearSourceBlocks` /
  `Close`) or after a short TTL — so re-index never serves a stale batch.
  Distinct `scan_cursor` values are distinct keys (**Scan more** always scans
  fresh). A Go-side word-boundary regex confirms each hit, then
  `FirstPlainTitleOccurrence` keeps only title spans that do not overlap a
  `[[…]]` wiki-link (same rule as promote). Blocks that already link the page
  once still surface when residual plain text remains; fully-linked-only
  blocks stay out. Each matched block carries a contextual `source_snippets`
  excerpt (120-rune window centered on the residual plain span).
  **Leaf ambiguity** for the active title uses an indexed
  `page_fold = ?` lookup (`idx_blocks_page_fold`). `page_fold` is a persisted
  Unicode simple-fold key (each rune mapped to the minimum code point in its
  `unicode.SimpleFold` cycle) written on index and backfilled on open, matching
  `strings.EqualFold` equivalence classes — including non-ASCII titles (Café/
  CAFÉ) and Unicode folds of ASCII letters (K vs U+212A Kelvin) — without a
  full page-inventory scan. When multiple
  locations share the leaf, `ambiguous` is true and `candidates` are
  stable-sorted (active notebook/section first) and **capped**
  (`unlinkedAmbiguousCandidateCap`) on each residual row, with
  `candidates_truncated` / `candidates_total` when the full collision set
  exceeds the cap. Unique leaves stay
  O(leaf matches). `PromoteUnlinkedMention` takes an explicit
  `(notebook, section, page)` target: exact path existence is authoritative
  (UI chip); otherwise leaf resolution against same-leaf pages rejects true
  ambiguity with `ambiguous_target`. `ShortestUniquePath` still uses the full
  inventory on promote (rare write path). The first residual plain occurrence
  in the source block is rewritten to a `[[shortest]]` page link — migrating
  it into the backlinks leg when no further plain hits remain.
- **AI providers** (#216, #218, #479, #632) — `GetAIProviderConfig` (key-scrubbed
  read; emits `has_key` flags + `features`, never the raw secret),
  `UpdateAIProviderConfig` (provider type / base URL / model / tuning — never
  the key), `UpdateAIFeatures` (product enablement: master AI, RAG, summaries),
  `SetAIAPIKey` / `ClearAIAPIKey` (dedicated key surface: routes to the OS
  keyring when `use_keyring` + reachable, else plaintext config), `SetUseKeyring`
  (toggles keyring storage + opportunistically migrates plaintext keys),
  `TestAIConnection` (1-token chat / single-embed probe), `ListModels` (polls
  the provider's model-list endpoint; cached per-provider in-memory, invalidated
  on type/base-URL/key change; force=false on cold start returns empty without
  a network call). `GetAIAudit` / `ClearAIAudit` expose the plugin-AI-call log
  (in-memory, mirrored to per-plugin `ai.log`; `ClearAIAudit` truncates both).
  `PluginAIAuditEvent` appends redacted structured agent events (tool_call,
  tool_result, staging_decision) to the same log; the closed allowlist of
  persisted keys holds only bounded identifiers (including `block_id`, a UUID,
  so agent write-tool mutations are traceable without leaking prose). Provider types: `local` |
  `openai-compatible` (universal default, OpenAI-shaped) | `google` |
  `anthropic` (native first-party APIs). NOT capability-gated (core settings),
  but `PluginAIComplete` / `PluginAIEmbed` / `PluginAIAuditEvent` are.
  `PluginAIEmbed` accepts optional `task_type` (`RETRIEVAL_DOCUMENT` /
  `RETRIEVAL_QUERY`). `PluginAIComplete` (#595) carries optional `tools` +
  `tool_choice` and returns `tool_calls`; messages accept a `tool` role for
  multi-turn agent loops (`silt-ai-agent`). Streamed runs emit
  **owner-scoped** events `ai:complete:delta:<pluginID>`,
  `ai:complete:tool-delta:<pluginID>`, `ai:complete:done:<pluginID>`,
  `ai:complete:error:<pluginID>` (#635) so argument fragments are not on a
  global bus. The `ctx.ai.complete` SDK wrapper strips reasoning tags — see
  `frontend/src/plugins/stripReasoning.ts`. The agent is **user-invoked only**,
  tool calls are **transparent**, and **destructive ops are staged** behind a
   single-use confirmation token. See `docs/plugins/silt-ai-agent.md`.

- **Local MCP host** (#687) — Go package `backend/mcp` runs an in-process
  **generic MCP server** (official `github.com/modelcontextprotocol/go-sdk`
  ≥ v1.6.1) when `ai.local_mcp.enabled` is true and a vault is open. The
  contract is **client-agnostic**: any MCP-capable desktop agent connects the
  same way (stdio `silt mcp` and/or loopback Streamable HTTP). There is no
  vendor-specific host, packaging format, or per-client protocol fork.
  **Bridge model:** tools call App content APIs (`SearchBlocksPaged`,
  `FetchPageBlocks`, `SaveFileBlocks`, `CreatePage`, `ListNavigation`, …) so
  Silt remains the single vault writer/indexer. **Transports:** loopback
   Streamable HTTP on `127.0.0.1` only (default port 17887) with bearer auth
   from the OS keyring (`Silt` / `mcp-local-auth-token`); stdio via `silt mcp`
   which dials the running instance (logs to stderr only). The SDK's
   cross-origin protection (default-ON in v1.4–v1.5, default-OFF since v1.6.0)
   is intentionally left OFF — CSRF is already prevented by the bearer scheme
   (tokens are not cookies, so a cross-origin page cannot attach one) plus the
   origin allowlist in `authMiddleware` (`isAllowedOrigin`). The loopback bind
   separately blocks remote network access.
   Discovery prefers a
   keyring-pinned endpoint (`mcp-local-endpoint`) over `mcp-endpoint.json` so a
   rewritten discovery file alone cannot redirect the bearer. The endpoint file
   records `{endpoint,pid}`; write/clear take a cross-process lock and refuse to
   clobber a live peer’s record (PID alive **and** `/health` serves `silt-mcp`)
   so crash+PID-reuse can reclaim discovery. Host `Start`/`Stop` are serialized
   on an internal `startMu` (outside the status `mu`) so bind/Shutdown cannot
   interleave. **Tools (v1):**
  read — `search_blocks`/`search_notes`, `read_page`/`read_blocks`,
  `list_notebooks`; write (grant) — `create_page`, `update_blocks`. No
  delete/move/bulk. **Lifecycle:** start on vault open when enabled; stop on
  vault close/switch and `ServiceShutdown`; close-to-tray keeps MCP.
  **Audit:** `<vault>/.system/logs/mcp-audit.jsonl` with redacted args.
  Settings UI: Settings → AI → Local MCP. User docs: `docs/LOCAL_MCP.md`.
  Optional portable Skill (workflow guidance only, not a second protocol):
  `integrations/silt-agent/SKILL.md`.


**Unified AI surface + enablement (#632).** AI chat has one right-side drawer,
opened by the titlebar **Silt AI** control when `ai.features.enabled` is on.
Product enablement lives under **Settings → AI** (not four Plugins toggles):
master **Enable AI** (agent + writing assistant), **Semantic search** (RAG /
`silt-ai-qa`, requires master + embedding), **Note summaries** (sub-toggle for
`silt-ai-summary`). First-party AI modules load from these flags via
`AIPluginLoadEnabled` / frontend `shouldLoadAIPlugin`; a one-shot migration
maps legacy `plugins.disabled` AI ids into features. The drawer renders a typed
transcript (text, evidence, tool calls/results, proposals, confirmations,
structured status). The agent loop is the default orchestrator; retrieval and
writing attach as capabilities when their flags are on.

- **Typed notes** — schema-driven note types live in `<vault>/.system/types/*.yaml`
  and a page declares its type via YAML frontmatter `type:`. The SQLite
  projection (`page_types` / `page_properties`) is reproducible working memory
  rebuilt from frontmatter + the type schema; the type watcher hot-reloads the
  schema and re-projects every typed page so the dashboards do not drift on a
  schema edit. **Type CRUD**: `ListTypes` / `GetType` / `SaveType` / `DeleteType`
  (atomic write to `.system/types/<id>.yaml`, watcher self-write suppressed),
  plus `ResolveTypeID` (frontmatter ref → canonical id) and `ReloadTypes`
  (manual cache flush; both emit `types:changed`). **Per-page type ops**:
  `GetPageType` (resolved schema + raw chip on unknown refs), `GetPageProperties`
  (full schema form with `IsSet` flags), `SetPageType` (keep-and-flag on schema
  mismatch), `SetPageProperty` / `ClearPageProperty` (surgical single-field
  rewrite, validated twice — once at entry and again inside the file lock to
  close the schema-hot-reload race). **Dashboard query**: `QueryPagesByType`
  (all pages of a type + their set properties, source-scoped). **Plugin SDK**:
  `PluginListTypes` / `PluginGetType` (read-only, no grant), `PluginSaveType` /
  `PluginDeleteType` (gated under `CapContentMutate`).

Signatures and per-binding doc-comments live in `app.go` and the `app_*.go`
files; this list is the contract surface, not the source.

**Error envelope (#478).** Every bound method returns `(T, error)`. Wails v3
delivers the error to TypeScript as an `Error` whose `.message` is the JSON
string from `MarshalError` (`formatIPCError` serializes an `*IPCError` or
`*plugins.CapabilityDeniedError` to `'{"code":"...","message":"..."}'`). The
stable, machine-readable error-code contract is therefore carried as a
**JSON string** on `.message`: the App's `MarshalError` callback (`main.go`,
`formatIPCError`) serializes an `*IPCError` (`ipc_errors.go`) or
`*plugins.CapabilityDeniedError` to `'{"code":"...","message":"..."}'`, which
survives `new Error()` intact. The
frontend `coerceIPCError` (`frontend/src/lib/ipcError.ts`) JSON-parses
`.message` to recover `{code, message}`; non-JSON prose (an unmigrated
sentinel) falls through to the raw message. Stable codes live in
`ipc_errors.go` (`CodeBlockBeingEdited`, `CodeVaultClosing`,
`CodeCapabilityDenied`); a migrated sentinel keeps `errors.Is` compatibility
with its pre-migration var via `IPCError.Is`, so Go-side tests asserting
`errors.Is(err, errBlockBeingEdited)` pass unchanged. Frontend callers map on
the code, not the prose, so a backend wording change cannot regress the
friendly mapping.


4.4 Theme Engine IPC & Pipeline

The theme engine is a pipeline (DESIGN.md §7 / SPECS.md §6.4): canonical schema → validate/flatten → settings + vault theme files → runtime injection. It lives in `backend/themes` and `frontend/src/theme` and reuses the App-binding → JSON RPC → Svelte store IPC topology; it does **not** touch SQLite. Disk writes are: user-global `settings.json` (active id + mode), vault theme JSON under `.system/themes/`, and optional per-theme asset directories / editor staging.

Pipeline (single source of truth shared with DESIGN.md §7 / SPECS.md §6.4):

```
  <vault>/.system/themes/*.json          (on-disk custom + imported themes)
          │  +  embed.FS themes/*.json   (first-class roster)
          ▼
  +----------------------------------------------------------+
  |   Go: backend/themes - Theme System v2                   |
  |   validate.go    ParseAndValidate (schema sandbox)       |
  |   loader.go      ListThemes / ResolveActive / FlatTokens |
  |   importer.go    ImportThemeFromPath / ExportThemeToPath |
  |   save.go        GetThemeJSON / Save / Rename / Delete   |
  |   cache.go       CachedThemeByID (mtime-aware cache)     |
  |   default.go     //go:embed themes/*.json (11 themes)    |
  |   derivation.go  OKLCH hover/active/disabled derivation  |
  |   background.go  per-zone background asset pipeline      |
  |   theme.go       Theme schema + Flatten (token emission) |
  +----------------------------------------------------------+
          │  Wails v3 JSON RPC (single App service)
          │   ListThemes / GetActiveTheme / ApplyTheme
          │   ImportTheme / ExportActiveTheme / PickThemeFile
          │   PickBackgroundImage   (persist bg into active theme)
          │   GetThemeJSON / SaveCustomTheme / RenameCustomTheme
          │   DeleteCustomTheme / PickImageFile / PrepareBackgroundAsset
          │   events: theme:changed | themes:changed
          ▼
  +----------------------------------------------------------+
  | Svelte store (frontend/src/theme/store.svelte.ts)        |
  |   themeState   active id/name/mode + dark/light maps     |
  |   themesState  listing + flat tokens (picker previews)   |
  |   resolves "system" locally via prefers-color-scheme     |
  |   editor working copy → FE flatten → injectTokens        |
  +----------------------------------------------------------+
          │  injectTokens(tokens)
          ▼
  ONE <style id="silt-theme">:root{ ... }</style>   (one DOM write
                                                    -> one recalc
                                                    -> same-tick repaint;
                                                       index.css :root is
                                                       startup fallback only)

  AppSettings (user-global settings.json): { active_theme, theme_mode }
          ▲  atomic write via vault.UpdateSettings / SaveSettings
          │  ApplyTheme / SaveCustomTheme(apply) persist selection here
```

**Storage layout.** Theme files live in `<vault>/.system/themes/*.json` (SPECS §3.2); large background assets in `<id>.assets/`; editor staging under `.editor-staging/`. The **first-class set** is embedded via `//go:embed themes/*.json` (embed-authoritative for first-class ids). `ListThemes` always surfaces the full roster; `ResolveActive` / `CachedThemeByID` resolve first-class ids from the embed when not on disk. The active id + mode persist to user-global `settings.json`.

**Schema & validation.** `backend/themes` validates the canonical v2 schema (RFC `docs/theme-system-v2-rfc.md`). `schema_version` is hard-enforced at `"2.0.0"` — any other value (including v1) is rejected with a descriptive error, and `DisallowUnknownFields` makes a typo fail loudly instead of being silently dropped. There is no v1→v2 migration path (single-user project; first-party themes were re-authored natively — see ADR `docs/decisions/0002-theme-schema-v2-no-migration.md`). Color slots accept `#hex` (`#rgb`/`#rrggbb`/`#rrggbbaa`), `rgb()`/`rgba()`, and `oklch(L C H[/ A])`; everything else (named colors, `hsl()`, `url()` at color slots, `expression()`, `<script>`) is rejected before the file is written — the import sandbox. The loader dedupes on-disk + embedded themes by id, imports/exports atomically, and serves a process-local mtime-aware cache.

**Flatten.** `Theme.Flatten(mode)` emits the CSS custom properties the runtime injector writes to `:root` — the *same* custom properties Tailwind v4's `@theme` block declares (and generates utilities from), so one namespace overrides both, with no alias layer. The emission is the **surface-zone model**: 9 named zones (`app, sidebar, editor, panel, card, modal, popover, titlebar, activitybar`), each producing `--color-surface-<zone>` / `-border` / `-text`. Inheritance is a strict tree realized as `var()` fallback chains (`popover→modal→panel→app`; `sidebar`/`editor`/`titlebar`/`activitybar`→`app`; `card→panel`): an authored zone is emitted verbatim, an omitted one falls back to its parent, so a theme switch repaints every surface in one cycle and every property always resolves. Zone-agnostic interaction tokens (`--color-hover`, `--color-active`, `--color-border-active`, `--color-border-focus`) and text-emphasis levels (`--color-text-primary`, `--color-text-muted`, `--color-text-disabled` — first-class semantic tokens; `text-primary` resolves to the app zone's foreground by definition, parallel to muted/disabled) apply on every surface. The accents, status, and the themeable error family (`--color-error`, `--color-error-bg`, `--color-error-border` — replacing the static Material-3 pink) round out the color namespace; the v1 parallel Material-3 palette is gone entirely (no aliases — a grep confirmed zero consumers). Optional `radius` / `spacing` / `shadow` ramps, an `editor` interaction block (caret/selection/link/highlight), theme-level `typography` (families plus a type scale), and a per-zone unified `background` block (absorbing the legacy `texture`; emits `--silt-bg-<zone>-*` overlay tokens) are emitted when authored, with sensible defaults otherwise. The v1 flat `bg` model, the `border`/`text` sub-blocks, the `chrome` block, and the `texture` block are removed; raised surfaces are now the `modal`/`popover` zones.

**Token–utility generation contract.** Every key `Theme.Flatten` emits that needs Tailwind utility generation MUST also be declared in `frontend/src/index.css`'s `@theme` block (with Cyber Forest dark as the startup-fallback value), or Tailwind v4 will not generate the matching utility class (`bg-…`, `text-…`, `border-…`, `rounded-…`, `shadow-…`). The intentional exception is `--spacing-*`: emitted by the geometry flatten pass but declared in `:root`, not `@theme`, because no utilities are generated from it (see the rationale comment in `index.css`). The @theme block is guarded by `hardcoded-colors.test.ts`, which asserts (a) no dead v1 token names (e.g. `--color-void`, bare `--color-surface:` / `--color-panel:`) are re-declared in @theme, (b) no resurrected dead v1 utility classes (e.g. `bg-void`, bare `bg-surface` / `bg-panel`) appear anywhere under `src/`, and (c) all 9 surface zones (`--color-surface-{app,sidebar,editor,panel,card,modal,popover,titlebar,activitybar}`) are declared in @theme as a sanity check that the block was extracted correctly. It does NOT cross-compare the `Flatten` key set against @theme — drift is caught by the dead-token and missing-zone guards, not a name-by-name diff.

**Chrome surfaces consume their respective zones directly.** The app skeleton (sidebar, titlebar, activity bar) renders their respective zones (`bg-surface-sidebar`, `bg-surface-titlebar`, `bg-surface-activitybar`). There is no scoping/remap class: each chrome element says its zone explicitly. For themes that omit these zones, the engine's inheritance resolves them to the app zone, so chrome matches the page; Daybreak, Synthwave, and Bubblegum author dark titlebar/activitybar/sidebar zones against a light `editor`/`app` to produce the unified dark chrome shell framing a bright page. (The v1 `.silt-chrome` CSS-variable remap was a shim and is gone; the class survives only as a non-theming layout/drag hook if at all.)

**IPC.** Listing/apply: `ListThemes`, `GetActiveTheme`, `ApplyTheme`. Import/export: `ImportTheme`, `ExportActiveTheme`, `PickThemeFile`. Background (persist into active theme, may fork embeds): `PickBackgroundImage`. Custom editor: `GetThemeJSON` (seed working copy), `SaveCustomTheme` (validate + write disk custom; optional apply), `RenameCustomTheme`, `DeleteCustomTheme` (refuse active), `PickImageFile` + `PrepareBackgroundAsset` (stage image for working copy without mutating a theme). Theme-file mutations take `themeWriteMu`; when Save also updates the active id, lock order is `themeWriteMu` → settings write. `ApplyTheme` / successful apply-on-save emit `theme:changed`; listing mutations emit `themes:changed`. `RenameCustomTheme` also emits `theme:changed` (with `name`) when the renamed id is the active theme so the active label updates without a full re-apply. `GetActiveTheme` returns both dark + light maps so the frontend resolves "system" locally without a second round-trip. FE `flatten.ts` is pinned to Go `Theme.Flatten` for every embedded theme × mode via shared golden fixtures under `frontend/src/theme/__fixtures__/flatten-goldens/`.

**Frontend** (`frontend/src/theme`): `store.svelte.ts` holds `themeState` / `themesState` and editor IPC wrappers; `inject.ts` rewrites a single `<style id="silt-theme">:root{…}</style>`; `flatten.ts` mirrors Go Flatten for live editor preview; `contrast.ts` classifies pairs and offers OKLCH-lightness auto-fix; `editor/ThemeEditor.svelte` is the progressive-disclosure editor (working copy → inject → Save). `AppearanceTab.svelte` is the accessible picker — card grid + details, two-stage preview, Customize entry, rename/delete for disk themes. UX contract: `docs/theme-v2-ux.md`.

**Launch background.** `main.go` resolves the webview `BackgroundColour` from the in-process theme cache so a non-default active theme's app-zone background (`surfaces.app.bg`) is used for the pre-CSS paint; it falls back to the embedded default when no settings exist or the active id is invalid.

**Contrast guarantee.** A CI gate (`backend/themes/contrast_test.go`) enumerates the critical semantic pairs for every embedded theme in both modes and fails the build below WCAG AA (4.5:1 text, 3:1 UI); Stark is asserted at AAA (7:1) for primary text as a regression guard. The editor surfaces the same ratios at runtime as non-blocking pass/warn/fail indicators (never blocks Save).


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
          │  Wails v3 JSON RPC (single App service)
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

**IPC.** `ListTemplates`, `GetTemplate`, `RenderTemplate`, `RenderTemplateBlocks`, `SaveUserTemplate`, `DeleteUserTemplate`, `ReloadTemplates`, `RegisterPluginTemplates`/`UnregisterPluginTemplates` (plugin-provided templates, deduped last), `CreatePageFromTemplate`. Emits `templates:changed`. `CreatePageFromTemplate` renders + prepends standard frontmatter + writes atomically + indexes, composing with the `CreatePage` path. If the target page already exists it returns IPC error code `page_exists` and does not clobber.

Template management is also exposed from Settings: user templates may be
created, edited, duplicated as user-owned copies, and deleted; built-in and
plugin templates remain read-only. The existing atomic save/delete bindings
remain the persistence boundary, and external edits continue to invalidate the
cache through `templates:changed`.

**Frontend** (`frontend/src/templates`): `store.svelte.ts` (`templatesState` listing + `templates:changed` subscription); `TemplatePicker.svelte` (modal: search, category groups, **rendered** live preview via marked + DOMPurify with `{{placeholder}}` chips, placeholder form; new-page or insert-at-cursor with mid-page insert confirmation). Entry points: New Page → From Template (`Ctrl+Shift+T`) and the `/template` slash command.


4.6 System Tray & Native Menus (#501, #503)

The v3 migration adds a system tray and native application menu.

**System tray (#501).** `setupTray` (tray.go) creates a `SystemTray` with the
app icon, a context menu (Show/Hide/Quit), and attaches the main window so a
single click toggles visibility. Close-to-tray is a user-global, **default-off**
setting at Settings → General → Window (`settings.json` `close_to_tray`). A
native `WindowClosing` hook (`main.go`, `setupMainWindowEvents`) always cancels
the OS-level close (titlebar button, Alt+F4, taskbar close) and routes it
through `RequestClose`, which checks the setting: enabled → the window hides
and the process + tray remain; disabled (or the tray Quit item) → `Quit()`
runs the canonical `ServiceShutdown` drain (WAL checkpoint, in-flight call
drain, plugin `onVaultClose` hooks) so there is one quit path. The tray keeps
the process alive — `wailsApp.Run()` does not exit until `Quit()` is explicitly
called. The OS-close interception routes every close gesture (titlebar
button, Alt+F4, taskbar close) through `RequestClose`, so close-to-tray and a
full quit share one decision path; the routing logic is unit-tested
(`tray_test.go`). When Local MCP is enabled (#687), close-to-tray keeps the
MCP host answering; Quit stops MCP via `ServiceShutdown` / `stopMCPHost`.

**Native menus (#503).** `setupMenus` (menus.go) creates a platform-aware
application menu: File (New Page, Open Vault, Save, Quit), Edit (Undo, Redo,
Cut, Copy, Paste, Select All via standard v3 roles), View (Toggle Sidebar,
Toggle Format Toolbar, Find, Focus Mode, Settings), Help (About). Custom
items emit Wails events (`menu:new-page`, `menu:save`, etc.) that App.svelte
listens for. Most route to the same handlers the keyboard shortcuts use; the
exception is Save, which flushes the active editor directly (`editor.flush()`)
rather than synthesizing a Ctrl+S keystroke, so the save runs against the
focused page without depending on editor focus state. Standard editing roles
use v3's built-in platform handling so keyboard shortcuts work natively
without custom JS dispatch.


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
- **Per-tab save-state:** `TipTapEditor` exposes `onSaveStateChange({ phase, dirty, error })` on save-pipeline transitions (`phase`: `idle` | `pending` | `saving` | `saved` | `error`). Debounce is `pending` (silent); only `saving` shows "Saving…"; success holds transient "Saved" (~2s) in the editor footer. The callback threads through `VirtualScrollContainer` → `App.svelte`, which writes `TabEntry.dirty` / `TabEntry.saveError` / optional `TabEntry.savePhase`. The tab strip renders a dirty glyph, a subtle saving indicator on the active tab when phase is `saving`, or an error glyph — visible from any tab. Controlled by `ui.show_tab_dirty_indicators` (default true). Fail-loud errors stay assertive; `pending` is never labeled "Saving…".

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

**Smart Graph NodeViews.** Three additional schema nodes render Smart Graph syntax as live, interactive elements inside the editor. The converter layer (`frontend/src/lib/editor/converters.ts`) tokenizes `clean_text` and emits the corresponding node types inline within the parent `noteBlock`; on save, the textual tokens are reconstructed byte-for-byte so the on-disk file is round-trip identical.

- `embedNode` (block-level, atomic) — `{{embed:uuid}}` becomes a live `EmbedPortal` NodeView. The portal fetches the referenced block via `ResolveBlockReference` and renders it as a nested live view. Display uses `RichText`; on focus, a schema-subset TipTap editor mounts for inline marks + chips, serializing back through `serializeInlineContent` → debounced `MutateBlock` (single-line `CleanText`; newlines still collapse).
- `blockReferenceNode` (inline, atomic) — `((uuid))` becomes a clickable `BlockReferenceChip` NodeView that navigates to the referenced block via the `navigate-to-block` DOM event.
- `pageLinkNode` (inline, atomic) — `[[target]]` / `[[target#heading|alias]]` becomes a clickable `PageLinkChip` NodeView. Resolution is `ResolvePageLink` (shortest unique path); click dispatches `navigate-to-page` (optional heading scroll). A derived `page_links` table (FK cascade from `blocks`) indexes outbound targets for rename rewrite.

The NodeView wrappers (`frontend/src/components/editor/EmbedNodeView.svelte`, `BlockReferenceNodeView.svelte`, `PageLinkNodeView.svelte`) re-use the existing read-mode chip/portal components — the same rendering pipeline serves both the read-mode and the NodeView contexts.

5.2 View Mode — Edit ↔ Source toggle

Each tab carries a `viewMode: 'edit' | 'source'` on its `TabEntry` (`frontend/src/lib/tabs.ts`) — the single source of truth for which projection a tab shows. `App.svelte` owns the value; the toggle is the floating icon button in `VirtualScrollContainer`'s action bar (`aria-pressed` + `aria-keyshortcuts`) and the `toggle_view_mode` hotkey (default `Ctrl+Shift+V`, per-vault), both routed through `handleToggleViewMode(tabId)` → the pure `setTabViewMode` state-machine action. The hotkey fires regardless of editor focus (there is no editor-internal keymap for it).

**Persistence.** `viewMode` seeds from the per-vault `editor.default_view_mode` when a tab is created, survives navigation within a session, and persists across restarts on `TabRef.view_mode` in the vault `config.yaml` (the per-vault UI tier — never SQLite; §0 rule 4). Only `"source"` is written (absence = Edit); `normalize()` collapses any other value to `""`. `GetOpenTabs`/`SetOpenTabs` round-trip it as part of the existing `TabRef`.

**Source view.** `MarkdownSourceViewer.svelte` is an **editable** raw-markdown surface (textarea + line gutter + "Copy as Markdown"). The buffer seeds from on-disk body via `FetchPageMarkdown` (reconstructed block `raw_text` is fallback only); debounced writes go through `SavePageMarkdown` (preserves YAML frontmatter, atomic write + re-index, returns the re-parsed block list). Dirty buffers block auto-save on external block refreshes until the user chooses Keep mine / Reload; clean buffers re-fetch. Focus lease is acquired while Source is mounted (same TTL path as Edit). When `editable={false}`, the viewer falls back to a read-only Shiki-highlighted `<pre>` (tests / future read-only hosts).

**Editor teardown in Source view.** The Edit/Source switch lives in `VirtualScrollContainer`: Source mode renders only `MarkdownSourceViewer` and does **not** mount `TipTapEditor`, so a tab held in Source view pays no ProseMirror memory cost (Svelte destroys the editor + NodeViews + listeners on the switch; it rebuilds from `blocks` on return to Edit after Source saves). Lifecycle safety: `TipTapEditor.onDestroy` flushes the pending save and releases the focus lease, and `hasFirstEdit` is container-scoped so edit-to-pin can't double-fire across a remount. See `docs/editor-memory-profiling.md` for the cost model.

**Scroll preservation across the round-trip.** `VirtualScrollContainer` captures `containerEl.scrollTop` in a `$effect.pre` the instant a tab leaves Edit (before the editor unmounts and the container height collapses) and restores it after the remounted editor signals readiness — `TipTapEditor` surfaces its internal `editorReady` state to the parent via an `onReady` callback fired in `onCreate`. Restore waits one tick + animation frame (so remounted NodeViews have measured) and clamps to the current scroll height (a doc may have shortened via autosave/fsnotify while the tab was in Source).

**Rich inline & block content.** Three more atomic node types render inside the editor and round-trip their source verbatim through `clean_text`, exactly like the Smart Graph tokens above. **Math** is KaTeX: inline `$...$` is an inline atomic `InlineMathNode`, and a NOTE whose entire body is `$$...$$` becomes a top-level `BlockMathNode` (the sole-content-NOTE path mirrors `embedNode` — block math is never emitted inside inline content, which would violate the schema). A function-based InputRule auto-triggers the inline node on a balanced `$…$` pair (currency-safe: the finder rejects a `$` preceded by `$` and any pair containing internal whitespace, so `5$ cash` / `$5` stay literal). `MathNodeView.svelte` renders KaTeX (`output: 'htmlAndMathml'` for screen readers, `throwOnError: false` so a bad equation shows inline in error color); the `/math` slash command and click-to-edit on an existing node open an in-app LaTeX popover (`MathLatexPopover.svelte`) with a live preview, replacing the native `window.prompt`. The popover is raised by a `silt:edit-math` window event so the editor and the NodeView stay decoupled (the NodeView is non-editable; it carries the latex as an attr). `Ctrl/Cmd+Enter` commits, `Esc` cancels, and an empty equation is rejected; math is implemented as a custom node rather than `@tiptap/extension-mathematics`, so it composes cleanly with Silt's converter/NodeView pipeline. **Mermaid** is a render branch on the existing `codeBlock`: a block whose `language` is `mermaid` renders an SVG via a lazy-loaded `mermaid.js` singleton (`useMermaid.ts`, dynamic import, ~200KB gzipped kept out of the main bundle, `securityLevel: 'strict'`, parse-guarded so invalid source shows a readable error) instead of the Shiki dual-layer; the ```mermaid fence round-trips via the existing `codeBlock.language` attr (Mermaid is a pure view). **@-mention** is an inline atomic `MentionNode` (`@[name]` token, like `((uuid))`); its suggestion list is a **read-only** `SELECT DISTINCT owner FROM tasks` projection surfaced via the `DistinctOwners(prefix)` IPC — SQLite stays working memory, no mention state is stored (§0 rule 4). `DistinctOwners` narrows server-side (`LIKE 'prefix%'`) so a vault with thousands of owners never ships the full list, and the editor caches the unfiltered set on mount with a short TTL plus a 120ms debounce on the prefix-refine path instead of re-fetching on every focus. Confirming a mention inside a `taskBlock` also stamps `[owner:: name]` in the same transaction (single source of truth for the token format via `buildMetaToken`); in a regular paragraph the chip is inserted with no owner write-back. The mention typeahead is a self-contained `Extension.create` mirroring `taskMetaSuggest` (no `@tiptap/suggestion` dependency — the in-repo convention that keeps the suggest logic jsdom-pure).

**Block drag handle.** A drag grip is rendered inline inside every block-level NodeView (`NoteBlockView`, `TaskBlockView`, `HeaderBlockView`, `EmbedBlockNodeView`) as a `<span data-drag-handle draggable="true">` — a fixed-column affordance with no layout jitter. The `SiltInlineDragHandle` extension (`frontend/src/lib/editor/siltInlineDragHandle.ts`) listens for `dragstart` on these spans, resolves the top-level block via the wrapper's `data-id`, and populates `view.dragging = { slice, move: true, node: NodeSelection }` so native ProseMirror drop reorders whole blocks (direct manipulation) and `BlockIndentOnDrop` can read `.node.from` for depth-on-drop and the depth-guide overlay. `Alt+ArrowUp/Down` is the keyboard complement (`moveActiveBlock` in `keymaps.ts`, no-`Mod` prefix so it never collides with the `Mod-Shift-Arrow` table bindings). `Delete` at the end of a block and `Backspace` at the start merge the adjacent same-type same-parent sibling's inline content into one block in a single ProseMirror transaction (`mergeSiblingBlock`), preserving the survivor's UUID; cross-type, cross-parent, and `codeBlock` boundaries fall through to the per-type default. Dropping sets the block's indent from the horizontal drop position (drop further right → deeper nesting), reusing the flat `depth` attr the renderer already pads via `[data-depth='N']` — no schema change, no new on-disk field. The depth math (`resolveDropDepth` in `dragIndentDrop.ts`) snaps to a 24px grid matching `--indent-unit` and is extracted as a pure helper so it is jsdom-testable.

The `handleDrop` ProseMirror plugin is deliberately conservative: it returns `true` (and dispatches the indent-aware transaction) only when it can prove the dragged identity, the drop target, and the resolved depth are all unambiguous; on any uncertainty it returns `false` and hands control back to ProseMirror's native reorder-only drop. The identity check is `$old.nodeAfter.eq(draggedNode)` against the drag source's `NodeSelection`, so a stale drag position (e.g. an editor re-render mid-drag) can never delete or indent the wrong block — a false `true` here is document-mutating. The interactive HTML5 drag pipeline has no jsdom equivalent (no real `DataTransfer` / layout-driven `posAtCoords`), so the end-to-end path is gated on the TESTING.md manual matrix.

5.3 Board Display Mode (silt-tasks)

The Board is one of three display modes hosted by the unified `silt-tasks` plugin (`frontend/src/plugins/first-party/silt-tasks/views/BoardView.svelte`, mounted inside `TasksHub.svelte`). It uses the identical `PluginContext` SDK as any third-party plugin — no direct `window.go.*` access. It reads the task set via the shared SQL builder (`silt-tasks/query.ts`) against the unified hub state (`state.svelte.ts`) and shifts status via `ctx.updateBlockState`, preserving the "core feature decoupling" contract (SPECS §8.3).

Cards are rendered as `role="button"` elements with `aria-grabbed`/`aria-label` and animated with Svelte's native `svelte/animate/flip` (200ms cubic-out, per DESIGN.md §6). HTML5 drag-and-drop drives the data; the FLIP animation repositions remaining cards in the same paint frame. Keyboard users change status with ArrowLeft/ArrowRight directly; Enter/click opens the shared non-blocking inspector drawer (`silt-tasks/components/TaskEditDrawer.svelte`) and `Shift+Enter` opens the shared scoped sub-editor (`TaskSubEditorModal`). The board supports multi-level scope (vault / notebook / section / page) via a segmented control, with the SQL `WHERE` clause built per scope level. Cross-card and within-column drops persist a new `[order:: N]` manual-sort position via `ctx.setTaskOrder`.

**Unified hub state.** The Board is not a standalone plugin — it shares one `TaskHubState` reactive store and one `buildQuery` SQL factory with the List and Calendar modes. The hub state (scope + filters + `focusDate` + `activeFilter` + `displayMode` + `groupBy` + `sort` + `columns` + saved views) is the single reactive source of truth the shell (`TasksHub.svelte`), the unified sidebar (`Sidebar.svelte`), and all three renderers read from and write to. The `scopeUserOverride` invariant (a user-narrowed scope survives an automatic scope change) lives in `setScope` / `narrowScopeTo` / `clearScopeOverride`.

**Page-scoped Tasks Hub routing.** A page-level action may enter a transient
session intent identified by a source-qualified locator: source, notebook,
section, page, and a nonce. The intent overlays the hub's effective page scope
and may provide display defaults without changing the ambient hub state. List,
Board, and Calendar continue to use the same task contract and query builder;
their queries qualify both the source and page coordinates, preventing
same-named pages in different roots from mixing. Task rows retain the source
through the shared inspector so source-page navigation resolves the exact
origin.

This route is session-only. It is not a saved view, is never written to
configuration or SQLite, and is cleared when the user intentionally changes
scope or filters. The projection reads canonical Markdown task blocks through
the existing derived index; it introduces no meeting model, separate board,
or duplicate task store.

5.4 Backlinks Panel

The backlinks panel (`BacklinksSidebarPanel.svelte`) is a sidebar surface
mounted when `activeView === 'backlinks'`, showing cursor-paged inbound
references to the currently open page. It calls `GetBacklinksPaged` (§4.3) on
mount and reacts
to `block:changed` events (debounced 200 ms) so edits that add/remove links
refresh the list without a manual reload. Results are grouped by source page,
each group listing its references with a kind badge (`[[` page link, `((`
block reference, `{{` embed), a clean-content snippet (contextual 120-rune
window centered on the reference token, with ellipsis markers), and
click-to-navigate. The
panel is empty-state-aware (no page open → prompt; no backlinks → hint with
link syntax) and surfaces load/error states with `aria-live` regions. An
explicit Load more control appends later pages without expanding the initial
IPC payload or DOM projection.

Beneath the backlinks groups, a collapsible **Unlinked mentions** section
lists other pages whose body text still has a residual plain (non-`[[…]]`)
whole-word mention of the current page title — including blocks that already
link the page once when plain text remains. It queries
`GetUnlinkedMentionsPaged` (§4.3) on mount and on the same debounced
`block:changed` cycle. The section is collapsed by default; each row shows
the source page, a match count badge, and per-block prose snippets (residual
plain title emphasized) with a Link action that calls
`PromoteUnlinkedMention` to convert the first residual plain occurrence into
a real page link. On success, when no residual plain hits remain on that
source page, the row migrates out of the unlinked leg and the new link
reappears among the backlinks groups. Mentions whose title resolves to more
than one page (ambiguous basename) are flagged and render candidate paths as
clickable chips — each chip promotes to that explicit target in one click,
  never auto-promoted without a choice. Server-side leaf lookup caps the chip
  list (`candidates` + `candidates_truncated` / `candidates_total`); the UI
  shows a “+N more” affordance when collisions exceed the cap so truncation is
  never silent. Residual **Load more** may reuse a short-lived server FTS
  candidate-window cache (§4.3); **Scan more** still advances the next window.
  When the API reports `truncated`, the
  section surfaces an accessible incompleteness notice (header subtitle and
  expanded status strip) so common-title caps are never silent. **Load more**
  advances the residual page cursor within the current FTS batch; **Scan more
  mentions** (when `truncated` and `scan_cursor` are set, and residual
  `has_more` is false) requests the next capped FTS batch and appends unique
  residual pages — blocked while unread residual pages remain in the current
  batch so continuation does not abandon them. The two controls stay distinct.

5.5 Search & Writing Aids

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
count. Each result keeps the backend `source` discriminator, shows a Vault or
Linked qualifier beside its breadcrumb and snippet, and sends the complete
source-qualified locator plus block target through App's existing page-open
funnel for both pointer and Enter activation. Source-less wiki-link events keep
their existing coordinate-based behavior, and `.silt` locators still route to
the Tasks view rather than opening an editor tab. Category chips are preferred
over tabbed categories because tabs force a type guess and hide cross-type
results. Markdown dialect is GFM (§"Markdown Dialect" in SPECS.md); sub/super
are `<sub>`/`<sup>` HTML.

**Global replace (Ctrl+Shift+G)** — `GlobalReplaceModal`
previews FTS5 matches grouped by page (before→after), with per-match + per-page
accept. Apply iterates accepted pages: `FetchPageBlocks` → replace in
`clean_text`/`raw_text` → `SaveFileBlocks` (atomic, self-write-tracked,
re-indexes). A session revert log records the original blocks per page;
"Undo last" restores. Applies to in-vault pages; linked notebooks are
Applies to in-vault pages; linked notebooks are read-only by design.

**Inline spellcheck** — `frontend/src/lib/editor/spellcheck/`:
`dictionary.ts` wraps `typo-js` (pure-JS Hunspell, BSD). The default `en-US`
dictionary loads from `frontend/public/dictionaries/en-US/` via `fetch`.
Additional languages (`editor.spellcheck_language`) download on demand from
version-pinned jsDelivr npm packages (wooorm/dictionaries) into a **user-global
cache** (`UserConfigDir/silt/dictionaries/`) via `EnsureLanguagePack` /
`GetLanguagePackContent` — offline after first fetch; errors surface loudly
(no silent fallback to en-US). Domain/technical word lists
(`editor.spellcheck_domains`, default `["software-terms"]`) and the per-vault
custom dictionary (`editor.custom_dictionary`) are **Set layers** over Hunspell
(typo-js has no public `addWord`); a session-ignore Set backs "Ignore".
Bundled curated `software-terms` ships embedded in the Go binary
(`backend/spellcheck/data/software-terms.txt`) and is served via
`GetDomainPackWords`; other MIT domain packs download the same way as
languages. Custom dictionary **import/export** uses native
file dialogs and a plain UTF-8 one-word-per-line format (`#` comments allowed).
**Note text never leaves the machine** — only optional dictionary *assets* are
fetched when the user opts in. `SpellcheckExtension.ts` is a ProseMirror
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
toolbar button). Catalog + cache live in `backend/spellcheck`.

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

Because SQLite runs in memory, concurrent reads/writes from the Svelte UI and the fsnotify file monitor must be strictly controlled to prevent database-locked exceptions. The engine routes all file writing and database tasks through an app-wide `core.ExecutionCoordinator`: a per-file `sync.Mutex` map (`LockFileWrite(path, fn)` serializes all writes to a given path, so writes to *different* files don't block each other) plus the DB connection. Paths are normalized to absolute form before use as lock keys. **Exact-path identity only:** locking a directory string does *not* exclude writers on descendant file paths. Structural ops that `os.Rename` a directory (section/notebook rename, tree delete) must `LockPathsWrite` every affected `.md` path (and inbound wiki-link source paths they will rewrite) before the rename — sorted multi-acquire avoids deadlock. Inbound sources are re-collected under the multi-path lock (retry if the set grows) so a concurrent save that inserts a new `[[target]]` cannot slip past the lock set; a post-unlock residual sweep rewrites any remaining broken `[[old]]` rows that landed in the narrow window after the in-lock re-collect. Inbound collect/rewrite query `page_links` by `lower(target_raw)` candidates (path variants + intermediate suffixes), batched in SQL `IN` clauses to stay under SQLite parameter limits without dropping targets on large notebook renames. Page saves keep single-path locks and, after acquiring the lock, fail loudly if the file was moved away (`os.Stat` fail-closed) rather than recreating a ghost path — Stat cost is negligible vs write+reindex. Large notebook renames still acquire one mutex per page; acquire cost at thousands of paths is ~1 ms and remains dominated by disk I/O, so hierarchical directory locks are intentionally not used. Lock order: block locks outside file locks; multi-file sets as one sorted unit; DB inside. DB access is serialized via `SetMaxOpenConns(1)`; WAL still allows unlimited concurrent readers (§3). See `backend/core`.

The App-level locking model is layered on top of the coordinator and guards
distinct concerns with distinct mutexes (see `app.go` for the full contract):
`vaultMu` (RWMutex) protects the LIFECYCLE of the vault-scoped service
pointers (db, coordinator, watcher, tracker, vaultPath) — reader IPC handlers
take `RLock` for the call's duration so a lifecycle cutover can't nil a
pointer mid-use; lifecycle transitions take the exclusive `Lock`.
`themeWriteMu` (Mutex) serializes on-disk theme-file mutations (import, fork,
set-background) so the importer's collision-check-then-write and the fork's
stat-then-write can't race. `settingsWriteMu` (Mutex, in `vault.go`)
serializes the settings.json read-modify-write. A handler that writes both a
theme file and settings.json acquires `themeWriteMu` first, then
`settingsWriteMu` (never reversed). Blocking native dialogs (file pickers)
are never called under `vaultMu` — the handler snapshots the needed paths
under `RLock`, releases, runs the dialog, then acquires `themeWriteMu` for the
write.

**Vault-close drain (#452, #471).** IPC handlers that release `vaultMu`
mid-call (today: `PluginAIComplete` / `PluginAIEmbed`, which release the lock
after preflight so a 60s LLM call can't hold it) are tracked by a separate
`vaultClosingWG`. `CloseVault` / `SwitchVault` set a `closing` flag under
`vaultMu.Lock`, release the lock, cancel the vault-scoped `vaultCtx` (a child
of the app-lifecycle `aiCtx`), then `vaultClosingWG.Wait()` outside the lock
before teardown. The cancel aborts in-flight HTTP calls in milliseconds (the
HTTP client observes `context.Canceled`) instead of blocking the close for the
provider timeout. The `closing` flag + `vaultClosingWG.Add(1)` share ONE
`RLock` hold in the preflight (`withAIPreflight`, which returns a `done` func
the caller defers), making the gate atomic w.r.t. the close path's set+Wait —
no TOCTOU window where a call slips through after the drain returns. `aiCtx`
itself is cancelled only in `shutdown()` so in-flight calls don't outlive the
process. See `app.go` and `app_ai.go`.


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
   first-party registry (bundled entry; optional Svelte component) ─► always available
   on-disk → ReadPluginSource(id) → Blob URL → import(/* @vite-ignore */)
        │
        ▼
plugin.init(ctx: PluginContext)   ←   sqliteQuery (SELECT/WITH-only),
                                      mutateBlock, updateBlockState,
                                      updateTaskMeta, ctx.on (typed event bus)
plugin.onVaultOpen(ctx)             ←   plugin lifecycle hook
         │
         ▼
App view router renders plugin:<id> via PluginView (incl. the silt-tasks hub)

Per-plugin load failures are collected and surfaced (PluginView shows a load-error notice) without aborting boot. The `plugins:changed` Wails event (emitted after install/uninstall/enable/disable) re-runs discovery.

`RegisteredPlugin.component` is optional. `PluginView` skips a registered
plugin that has no component, allowing headless capability providers to keep
their lifecycle hooks, settings, event subscriptions, and SDK-backed services
without adding a navigable view. The unified AI drawer is the host-owned
surface for the first-party AI capability providers.

**Vault-switch lifecycle.** The Go `vault:closing` event fires before teardown so the loader can run every plugin's `onVaultClose`/`onShutdown` hook and clear the session registry; it also resets the unified Tasks hub state (`resetTaskHubState` in `first-party/silt-tasks/state.svelte.ts`) so a switched vault doesn't inherit the previous display mode, scope, grouping, filters, saved views, or `focusDate`. A `loadedPlugins.loadersReady` flag gates `PluginContext` construction in `Sidebar.svelte` and `PluginView.svelte`: the flag flips to `false` at the start of teardown and back to `true` once the next `loadPlugins` completes, so a sidebar that remounts during the clear→re-register window never captures a stale/empty session token (and `makePluginContext` is simply not called against a half-torn-down registry). The derived context re-runs on the flag, so the moment the new vault's plugins resolve the sidebar re-binds cleanly.

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
SQLite store: `ctx.pluginDb.exec` / `query` / `migrate`), `ai` (gates
`ctx.ai.complete` / `ctx.ai.embed` — routes to the user-configured model
server; the plugin never receives credentials).

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

**Plugin webview isolation (#502).** Per-plugin isolated webviews will
**not** be implemented. Wails v3's service bindings are
**application-global**: every registered service is reachable from every
window, and there is no per-window binding scope or capability model
that could restrict which `Plugin*` methods a given window may call (the
`WebviewWindowOptions.Permissions` map covers only webview-level OS
permissions — camera/mic/geo/clipboard — not Go-binding access). A
spike against `v3.0.0-alpha2.117` confirmed multi-window creation works
but binding isolation does not exist, so #502's acceptance criterion
(enforcing capabilities per-plugin-webview via the Wails 3 capability
model) is unachievable. The existing boundary remains authoritative: the
iframe sandbox (CSP `connect-src 'none'` + postMessage bridge +
Go-proxied `PluginFetch` with SSRF defense) for rendered plugin UI, plus
the Go-layer session verification (`validatePluginSession`) and
capability grants (`requireGrant`) for every privileged binding —
together they enforce the property #502 was after (a plugin cannot
escalate beyond its grants or impersonate another plugin). See ADR
`docs/decisions/0005-plugin-webview-isolation-wontfix.md`; #151/#152
stay blocked on a Wails v3 capability that does not exist today.

**Rate limiting.** `PluginFetch` is throttled by a per-plugin token-
bucket rate limiter (default 1 rps, burst 10; manifest `ratelimit` override).
Buckets are evicted on uninstall. Capability denials (`requireGrant`) and
rate-limit rejects (fetch + AI) also increment a session-scoped in-memory
per-plugin counter (`GetPluginSecurityStats`) and emit a structured
`security:event` Wails event so Settings → Plugins can show a warning badge
(#518). Counters clear on vault close and per-plugin uninstall — not
persisted (not markdown-reproducible).

**Network audit log.** `auditNetwork` appends to the in-memory log
(capped 500 entries) under `networkAuditMu`, then enqueues a disk-write op
onto a buffered channel. A single background goroutine (`startNetworkAuditWriter`,
started in `initializeVaultServices`, stopped first in `teardownVaultServices`)
drains the channel and writes to the per-plugin `network.log` WITHOUT holding
the lock, so concurrent `PluginFetch` calls don't serialize on file I/O.
On vault open, `seedNetworkAuditFromDisk` reads the on-disk logs to seed the
in-memory log (before the writer starts). No SQLite table (audit data is not
reproducible from markdown; §0 rule 4).

**AI audit log.** `auditAI` mirrors the network audit's design exactly: appends
to the in-memory log (capped 500 entries) under `aiAuditMu`, then enqueues a
disk-write op onto its own buffered channel drained by a parallel background
goroutine (`startAIAuditWriter`, started in `initializeVaultServices`, stopped
first in `teardownVaultServices`). It writes the per-plugin `ai.log` (one JSON
object per line, same format as `network.log`) WITHOUT holding the lock, so
concurrent `PluginAIComplete` / `PluginAIEmbed` calls don't serialize on file
I/O. On vault open, `seedAIAuditFromDisk` seeds the in-memory log from disk
(before the writer starts). Logs plugin / kind (chat | embed) / host / model /
status / token counts — NEVER message content or embedding vectors.

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
- AI (#216, #632): `ctx.ai.complete` / `ctx.ai.embed` / `ctx.ai.auditEvent`
  route to the user-configured model server (Settings → AI) through
  `backend/ai`. Provider config + resolved API key are snapshotted under
  vaultMu+configMu, locks RELEASED, then HTTP runs so a long completion cannot
  hold the vault lock. Keys resolve keyring-first (#218); plugins never receive
   credentials. Transport uses per-attempt timeouts plus an overall retry
   envelope, HTTP Retry-After and provider body retry delays (e.g. Google
   RetryInfo.retryDelay), and jitter; stream connect retries pre-byte only.
   Rate-limit UI copy may include provider detail when present. Calls and
   structured agent events are audit-logged (`auditAI` / `PluginAIAuditEvent`,
   in-memory + `ai.log`) and surfaced in Settings → AI → Recent AI activity.
- Editor extension points: slash-command registry; generic embedBlock
  node (round-trips through <!-- silt-embed: {json} --> markers).
- Rendered UI surfaces: sandboxed <iframe srcdoc> + postMessage bridge;
  sidebar panel / modal / status-bar / `note-banner` surfaces; theme tokens
  injected. The `note-banner` kind mounts a dismissible banner host at
  the top of the note view (above the TipTap editor); first-party banners
  render a compiled Svelte component (passed via the surface's `component`
  field, mounted directly with `{ ctx, onDismiss }` props — `silt-ai-summary`
  is the reference consumer), third-party via the iframe bridge. The
  bridge is bidirectional: iframe→host **data-only** RPC requests
  (serializable args/results; callback methods like `on` /
  `registerSlashCommand` / `registerSurface` are not in `allowedMethods`
  because functions do not survive structured clone — those run from
  main-webview `init()` only) AND host→iframe events (`silt:surface:event`).
  The close affordance sends a `dismiss` event (iframe path) or invokes the
  component's `onDismiss` prop (first-party path) so the plugin can persist
  dismissal state (`updatePluginSetting('<id>', 'dismissed_notes', [...])`) —
  `updatePluginSetting` is in the bridge's `allowedMethods` so the
  documented pattern is reachable from a sandboxed banner. When more than two banners stack, the host collapses them into a
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
- PluginMutateBlock(id, text) / PluginUpdateBlockState(id, status) — wrap MutateBlock / UpdateBlockState (same atomic-write + re-index + lock path as the core editor). PluginUpdateBlockState returns `{ok, spawned_id}`, carrying the recurrence instance UUID minted by a recurring TODO/DOING→DONE transition (empty otherwise).
- GetPluginRegistry() / ListPlugins() / ReadPluginSource(id) — discovery.
- ValidatePluginArchive / PickPluginArchive / InstallPlugin / UninstallPlugin / EnablePlugin / DisablePlugin — `.silt-plugin` distribution (see backend/plugins package; zip-slip + traversal guarded, atomic extract).

7.5 Smart Graph Events

Block mutations broadcast a `block:changed` Wails event (BlockChangedEvent {ID, Notebook, Section, Page, FileDate}) so live embeds (`{{embed:uuid}}`) and references (`((uuid))`) refresh in real time. Emitted from MutateBlock, UpdateBlockState, the post-write path of SaveFileBlocks, page/section/notebook deletes, and the fsnotify watcher after external reindex or clear (so plugin-owned indexes such as silt-ai-qa vectors stay consistent). Emission no-ops when ctx is nil (tests). The frontend EmbedPortal subscribes via `Events.On` (v3 runtime) and re-fetches its source block when the event matches its uuid (a module-scoped render-stack guard stops recursive embed loops). When a block transitions to DONE, `UpdateBlockState` also fans the event out to every dependent task (those `[blocked_by::]` the just-completed block) so the silt-tasks "blocked" badge and the DONE-confirm guard re-evaluate (#301).

---

8. System Configuration Engine (config.yaml)

Global settings — editor defaults, parsing rules, hotkeys, and the plugin registry — live in <vault>/.system/config.yaml, the single source of truth for everything except the vault path (which stays in OS-config settings.json because it must be known before any vault can be opened).

8.1 Parser (backend/config)

Navigation preferences extend the `ui.*` tier with `expanded_sections`
(notebook plus full relative section path), bounded timestamped `recent_pages`,
bounded MRU `recent_tags` (capped at 12, case-insensitive dedupe, maintained by
`RecordTagUsage`), and canonical `favorites` (Quick Access pins; UI copy is Pin
to Quick Access / Unpin — distinct from pinned tabs and task `[pin::]`). These
values and navigation order are normalized,
deduplicated, reconciled after filesystem changes, and persisted through the
serialized narrow config mutation path rather than a competing whole-navigation
snapshot setter. The canonical navigation defaults are `Ctrl+N` for a new page,
`Ctrl+Alt+N` for a new section, `Ctrl+Alt+Shift+N` for a new notebook, `Ctrl+P`
for the page switcher, and `Shift+?` for shortcut help; an explicit empty
binding disables an action.

config.SystemConfig mirrors the SPECS §10.1 schema (notebooks / editor / parsing / hotkeys / plugins / ui / ai). The `ai.*` block (#216, #218, #632) carries two provider configs (chat + embedding: provider_type, base_url, model, tuning), `use_keyring`, and `features` (`enabled` / `rag_enabled` / `summaries_enabled` — product enablement for first-party AI modules; dependents clamp when master is off). API keys are `json:"-"` (never serialized to the frontend) and, when `use_keyring` is on + the OS keyring is reachable, are stored in the OS credential store (`backend/keyring`) instead of plaintext config — so a synced vault doesn't carry cloud keys. `SaveSystemConfig` preserves live keys server-side so a frontend round-trip doesn't blank them. The `ui.*` block holds per-vault UI preferences: `sidebar_width`, `nav_order` (explicit section/page ordering for drag-to-reorder), `open_tabs` / `active_tab` (pinned-tab persistence — preview tabs are ephemeral), `enable_preview_tabs`, `max_open_tabs`, `show_format_toolbar`, `show_tab_dirty_indicators` (default true), `dismissed_tips`, `note_zoom` (note content zoom 0.7–2.0, independent of `editor.font_size_px`; atomic `SetNoteZoom`), and `formatting.*` toggles. Load(vaultPath) decodes over config.Defaults() so omitted sections keep their default values rather than being zero-valued; a missing file returns defaults (non-fatal), but a file that exists and fails to parse returns an error (fail-loud — never silently fall through). Save(vaultPath, cfg) is atomic (temp file + fsync + rename), matching the durability guarantee of note writes. The App holds the parsed config under configMu and replaces it wholesale on reload (never mutated in place), so a struct read under RLock is a safe snapshot.

8.2 Hot-Reload (backend/config.ConfigWatcher)

A dedicated fsnotify watcher observes the .system parent directory (not the file alone) so a delete+recreate of config.yaml is still observed. Self-loop prevention is a local time-window in ConfigWatcher: SaveSystemConfig calls RegisterSelfWrite() before the atomic write, and the watcher ignores every config.yaml event until a 500ms window elapses — a single logical save can emit several fsnotify events (atomic temp+rename, or truncate+write), so the window suppresses all of them, not just the first. External edits re-parse and invoke onChange → App.applyConfig (updates live knobs + emits config:changed); a parse failure invokes onError → config:error (last-good config retained). This implements SPECS §10.2 without an application restart.

8.3 Settings Menu (frontend)

The settings store (settings/store.svelte.ts) is a $state object exposing loadConfig/saveConfig, dirty tracking, and a config:changed / config:error subscription. Settings is a first-class **view** (`activeView === 'settings'`), matching how the Tasks/Tags plugin views own the sidebar rather than a modal or a workspace tab: the **Sidebar** renders `SettingsNav` (the section list — General / Editor / Appearance / AI / Hotkeys / Plugins / `plugin:<id>` bespoke-settings tabs / Dev (dev mode only) / About — as a `role="tablist"` with roving keyboard navigation Arrow/Home/End), and the **content area** renders `SettingsPanel` (`role="tabpanel"`) for the active section. The section list is a single shared module (`settingsSections.svelte.ts`) consumed by both; the active section (`settingsSection`, default 'general') is the single source of truth, bind-chained App → Sidebar → SettingsNav. Settings opens via the activity-bar gear button (highlighted when active), the `open_settings` hotkey (`Ctrl+,`), the native File→Settings / Help→About menus, and the plugin `'open-settings'` / `open-plugin-manager` events; it is session-only by construction (`activeView` is not persisted). The former `SettingsShell` modal overlay was decomposed into `SettingsNav` + `SettingsPanel`, dropping the modal focus-trap — dismissal is by switching views. The former Workspace tab was folded into **General**, which now carries a **Window** section (user-global, pre-vault: close-to-tray) above a **Workspace** section (vault-scoped: move/copy/switch/export-import) — the split keeps user-global controls rendering before any vault is open. **AI** (section id `ai`) is a separate core tab: product feature toggles (Enable AI / Semantic search / Note summaries via `ai.features`), shared provider infrastructure and credentials (chat + embedding, API key/keyring, connection test), and the audit log. First-party AI plugins are not independently toggled on the Plugins tab. Plugin-owned surfaces (e.g. the `silt-ai-summary` note banner) still render through the plugin surface system when their feature flag is on. GeneralTab edits a local draft (Save/Revert) so an external hot-reload cannot fight a half-edited form; if an external change lands while the draft is dirty, the draft is preserved and a non-blocking "reload" notice is shown (never a silent clobber). The Plugins tab is the single plugin UI: rich cards (first-party bundled vs. third-party installed), enable/disable (all plugins — first-party via config.yaml `plugins.disabled` list, third-party via `.disabled` sentinel), uninstall (third-party only), inline load errors, an expandable detail panel with per-plugin settings, and the .silt-plugin install flow. The titlebar extension icon opens Settings → Plugins.

8.4 Editor Config Consumer (frontend)

The editor-token pipeline (settings/editor-tokens.svelte.ts) mirrors the theme injector pattern (§4.4): editor.* config values (font_family, mono_font_family, font_size_px, line_height) are injected as CSS custom properties (--editor-font-family, --editor-mono-font-family, --editor-font-size, --editor-line-height) on :root via a dedicated <style id="silt-editor"> element, separate from the theme injector's <style id="silt-theme">. initEditorTokens() uses $effect.root to watch the reactive settings store, so config changes apply live (one DOM write → one recalculation → same-tick repaint) without a reload or remount. The index.css :root values are startup fallbacks only.

TipTapEditor (the live block editor, frontend/src/components/TipTapEditor.svelte)
consumes the full editor.* config surface: typography flows through the CSS
variables (font-family, font-size, line-height on the contenteditable);
auto_save_delay_ms drives the triggerAutoSave debounce; focus_highlight_ancestors
gates the guide-rail active highlight; show_word_count toggles a subtle
CharacterCount display; focus_mode dims non-active paragraphs; and
indent_block / unindent_block (and other editor-scoped chords) are resolved
live via resolveShortcut on each keydown in SiltBlockKeymaps, so HotkeysTab
saves apply without remounting the editor. Visual nesting uses data-depth on
the outer NodeView root (outerNodeViewAttrs) so `.ProseMirror > div[data-depth]`
CSS applies. The cycle_view_layout hotkey is wired in App.svelte's
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
convention-anchored (see SPECS.md sample): ties anchor to document-processor
conventions, with code-editor conventions filling gaps where document
processors have no opinion. Windows/Linux only
(`Ctrl` everywhere). Spellcheck deliberately has no hotkey (wavy underline +
right-click + a FormatToolbar button). Settings opens on `open_settings`
(`Ctrl+,`, the universal settings convention); `Ctrl+,` was freed by moving
`format_subscript` to `Ctrl+Shift,`. `Load()` decodes over
`Defaults()`, which is the single source of truth for hotkeys. Paste is not in the hotkey map:
`Ctrl+V` is ProseMirror's native rich paste, `Ctrl+Shift+V` inserts the
clipboard as plain text (PlainPaste extension, lib/editor/plainPaste.ts).


9. Performance Budgets

9.1 Boot-Scanner Budget (Hard Regression Gate)

TestScanWorkspace_BudgetRegression (backend/parser/parser_test.go) seeds 1,000 small page files and asserts ScanWorkspace completes in under 450ms (baseline ~280ms on Ryzen AI MAX+ / Go 1.25 / Windows). The test runs in the normal `go test -race ./...` CI gate (skipped under `-short`) so a regression is caught immediately, not only when someone runs `-bench`.

9.2 Atomic-Write Safety (Kill-Mid-Write WAL Recovery)

TestAtomicWrite_KillMidWriteRecoversViaWAL (backend/db/db_test.go) simulates a destructive exit (SIGKILL / power loss) by closing the raw `*sql.DB` handle WITHOUT the `PRAGMA wal_checkpoint(TRUNCATE)` that `DatabaseManager.Close` performs. A subsequent `NewDatabaseManager` (the "next launch") auto-replays the WAL, recovering every committed block. The test also asserts zero stray `*.tmp` files in the vault directory. TestWriteFileAtomic_NoTruncatedFilesOnKill verifies 100 concurrent atomic writes to different files leave no truncated content.

9.3 UI Frame-Budget Probe

frontend/src/lib/perf/frame-budget.ts provides `measureFrameBudget(label, fn)` — a dev-only probe (gated on `?perf=1` in the URL; zero-cost pass-through otherwise) that wraps a callback in `performance.mark`/`measure` + `requestAnimationFrame` and logs the elapsed time against the 16ms frame budget. Instrumented on the three highest-stress paths: Board drag-drop settle, TipTap editor transaction (docToBlocks), and theme-token injection.
