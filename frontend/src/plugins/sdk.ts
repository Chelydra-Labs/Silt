// Silt Plugin SDK — the contract every plugin (first- or third-party) uses.
// Mirrors SPECS.md §8.2.

// Plugins must reach the backend through the PluginContext SDK (ctx.*) rather
// than the raw Wails bindings (wailsjs/go/main/App.js) — the raw surface will
// break when per-plugin isolated webviews land (#152). This is enforced by code
// review and the SDK contract, NOT by a runtime probe: a module-load check on
// window.go.main.App can't tell a plugin's raw import from the SDK bridge's own
// legitimate use, so it only ever produced a false-positive warning on boot.

export type TaskStatus = 'TODO' | 'DOING' | 'DONE'

/**
 * Today's date in the user's LOCAL timezone as YYYY-MM-DD.
 *
 * Plugins compare against this instead of SQLite's `date('now')`, which is
 * UTC and produces off-by-one results for the "today"/"overdue"/"this week"
 * quick-picks near local midnight (#118). The webview's local timezone is
 * the OS timezone (same machine as the Go backend's `time.Local`), so this
 * is computed in-process — no IPC round-trip, and it stays in sync with the
 * system clock on every read.
 */
export function localToday(): string {
  const d = new Date()
  const y = d.getFullYear()
  const m = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  return `${y}-${m}-${day}`
}

/**
 * Add `n` days to a YYYY-MM-DD string and return the resulting YYYY-MM-DD.
 * Used for date-range bounds like "this week" (today + 7). Operates in the
 * local timezone via Date arithmetic so month/year boundaries roll over
 * correctly. Pure + deterministic → trivially unit-testable.
 */
export function plusDaysISO(iso: string, n: number): string {
  // Parse as local Y/M/D (not UTC) to avoid off-by-one from Date's UTC
  // default parsing of date-only strings.
  const [y, m, d] = iso.split('-').map(Number)
  const date = new Date(y, (m ?? 1) - 1, d ?? 1)
  date.setDate(date.getDate() + n)
  const yy = date.getFullYear()
  const mm = String(date.getMonth() + 1).padStart(2, '0')
  const dd = String(date.getDate()).padStart(2, '0')
  return `${yy}-${mm}-${dd}`
}

/**
 * Result envelope returned by `PluginContext.sqliteQuery`. The shape
 * mirrors the Go-side `PluginRawQueryResult` struct: the row slice plus
 * a `truncated` flag the plugin can surface when the result hit the
 * Go-side `maxPluginQueryRows` cap (defense-in-depth memory safeguard).
 *
 * The split is intentional — silently truncating a vault-scope Kanban
 * query is exactly the kind of data-loss surprise a first-party plugin
 * shouldn't hide from the user. Plugins that don't care (Agenda,
 * Calendar) can simply destructure `rows` and ignore `truncated`.
 */
export interface SqliteQueryResult {
  rows: Record<string, unknown>[]
  truncated: boolean
}

/**
 * A task prerequisite returned by `getTaskBlockers` (#302). Carries the
 * breadcrumb + display fields the DONE-confirm dialog lists so the user can
 * see *which* tasks are blocking completion. Mirrors the relevant subset of
 * the backend TaskResult projection; display fields are optional to match
 * the Go struct's omitempty json tags.
 */
export interface BlockerTask {
  id: string
  clean_content?: string
  owner?: string
  due_date?: string
  notebook?: string
  section?: string
  page?: string
}

/**
 * A block in a task's child sub-tree (#305). Mirrors the editor's ParsedBlock
 * shape (the JSON that crosses the Wails IPC boundary in FetchPageBlocks /
 * SaveFileBlocks). The focused Task Sub-Editor Modal seeds its TipTap instance
 * from a fetched SubtreeBlock[] and serializes edits back via saveSubtreeBlocks.
 */
export interface SubtreeBlock {
  id: string
  parent_id?: string
  type: string
  depth: number
  raw_text: string
  clean_text: string
  status?: string
  owner?: string
  start_date?: string
  due_date?: string
  recurrence?: string
  priority?: number
  line_number: number
  file_date?: string
  language?: string
}

/**
 * A block-search hit returned by `searchBlocks` (#303). Mirrors the relevant
 * subset of the backend TaskResult projection — enough for the dependency
 * picker (and embed insertion) to render a breadcrumb + label per result.
 */
export interface SearchHit {
  id: string
  clean_content?: string
  notebook?: string
  section?: string
  page?: string
}

export interface PluginContext {
  /**
   * The active notebook. This is a LIVE reactive getter (#69): reading it
   * inside a Svelte reactive context (template, $derived, $effect) tracks
   * navigation changes automatically. Do NOT destructure in init() — that
   * captures a stale snapshot. Read it at query/render time instead.
   */
  activeNotebook: string
  /** Active section — same reactive semantics as activeNotebook. */
  activeSection: string
  /** Active page — same reactivity as activeNotebook. */
  activePage: string
  /**
   * Today's date in the user's LOCAL timezone as YYYY-MM-DD. Read this
   * instead of SQLite's `date('now')` (UTC) so date comparisons match the
   * local day (#118). A plain getter returning a fresh value on each read.
   */
  today: string
  /**
   * Read-only SQL against the in-memory index (SELECT/WITH only). Returns
   * the row slice plus a `truncated` flag; see `SqliteQueryResult`.
   */
  sqliteQuery: (sql: string, params?: unknown[]) => Promise<SqliteQueryResult>
  /** Rewrite a block's body text by UUID (preserves task syntax + UUID). */
  mutateBlock: (id: string, text: string) => Promise<boolean>
  /** Transition a task block's status. */
  updateBlockState: (id: string, status: TaskStatus) => Promise<boolean>
  /**
   * Update per-task metadata (pin, progress). Both fields are optional;
   * pass undefined to skip a field. Pin and progress are file-resident
   * user intent — the call round-trips through the markdown file.
   *
   * Pin is tri-state (#123): `true`→`[pin:: true]`, `false`→`[pin:: false]`
   * (explicit unpinned, preserved across round-trips), `null`→clears the
   * token entirely. `undefined` leaves the pin unchanged.
   */
  updateTaskMeta: (
    id: string,
    meta: { pinned?: boolean | null; progress?: number }
  ) => Promise<boolean>
  /**
   * Rewrite a task's `[due:: YYYY-MM-DD]` inline token on disk atomically
   * (#293). Pass an empty string to clear the due date. This is the mutation
   * surface behind calendar drag-and-drop rescheduling: drop a task card on a
   * day cell → set the due date to that day. Round-trips through the markdown
   * file (source of truth), re-indexes, and emits `block:changed`.
   * Gated by content-mutate.
   */
  setTaskDueDate: (id: string, dueDate: string) => Promise<boolean>
  /**
   * Rewrite a task's `[recur:: RULE]` inline token on disk atomically (#296).
   * Pass an empty string to clear the recurrence (the "stop recurring" path).
   * A non-empty rule must be valid recurrence grammar (`every day|weekday|
   * week|month|year` or `every N days|weeks|months|years`) AND the task must
   * already carry a `[due::]` date — the resolver anchors on the due date, so
   * recurrence without an anchor is rejected. Round-trips through the markdown
   * file, re-indexes, and emits `block:changed`. Gated by content-mutate.
   */
  setTaskRecurrence: (id: string, recurrence: string) => Promise<boolean>
  /**
   * Rewrite a task's `[blocked_by:: ((uuid))...]` inline token on disk
   * atomically (#301/#303). Pass an empty array to clear all dependencies.
   * Cycle prevention runs server-side: adding an edge that would close a
   * loop is rejected (the promise rejects). Round-trips through the markdown
   * file, re-indexes, and emits block:changed. Gated by content-mutate.
   */
  setTaskBlockedBy: (id: string, depIDs: string[]) => Promise<boolean>
  /**
   * Return the open (non-DONE) prerequisites of a task (#302), each with full
   * metadata (owner, due date, breadcrumb) for the DONE-transition confirm
   * dialog. Empty array = the task is actionable.
   */
  getTaskBlockers: (id: string) => Promise<BlockerTask[]>
  /**
   * Fetch a task block's child sub-tree — the indented blocks beneath it
   * (#305). Used to seed the focused Task Sub-Editor Modal. Returns an empty
   * array when the task has no children. Read-only; no IPC write. The block
   * shape mirrors the editor's ParsedBlock; see SubtreeBlock.
   */
  fetchSubtree: (blockId: string) => Promise<SubtreeBlock[]>
  /**
   * Splice an edited child sub-tree back into the parent task's block,
   * atomically re-rendering the whole page through the canonical write chain
   * (#305). The parent task block and all surrounding content are preserved
   * verbatim; only the contiguous child range (depth > parent depth) is
   * replaced. Emits block:changed for the parent so views refresh.
   */
  saveSubtreeBlocks: (
    blockId: string,
    children: SubtreeBlock[]
  ) => Promise<boolean>
  /**
   * Create a standalone task (a GFM checkbox) in the dedicated non-note
   * markdown file `<vault>/.silt/tasks.md` (#368). The task is queryable via
   * sqliteQuery / QueryTasks immediately and survives a full re-index because
   * it round-trips through the markdown-source-of-truth. title is required;
   * dueDate (YYYY-MM-DD) and status default to no due date / TODO. Returns the
   * new block's UUID. Gated by content-mutate.
   */
  createTask: (opts: {
    title: string
    dueDate?: string
    status?: TaskStatus
  }) => Promise<string>
  /**
   * Resolve this plugin's settings map for the ACTIVE notebook, applying the
   * co-located per-notebook override layer (#133). For a vault notebook (or
   * no active notebook), returns the vault-scoped config.yaml entry for this
   * plugin. For a linked notebook, returns the deep-merge of the vault entry
   * with the linked notebook's co-located `<root>/.system/config.yaml` entry
   * (linked wins per-key). The co-located file is READ-ONLY / user-authored;
   * Silt persists plugin settings to the vault config via updatePluginSetting.
   *
   * Re-read on every call so an external edit (vault or co-located) is
   * reflected immediately; the `linked-config:changed` event drives reactive
   * refreshes for active UIs.
   */
  getPluginSettings: () => Promise<Record<string, any>>
  /**
   * Resolve a SINGLE setting key with schema-default fallback (#103). Reads
   * the merged per-active-notebook settings and falls back to the schema's
   * default when the key is absent. Returns undefined if neither a stored
   * value nor a default exists.
   */
  getSetting: (key: string) => Promise<any | undefined>
  /**
   * Persist a SINGLE setting key to the vault-scoped config.yaml via the
   * atomic UpdatePluginSetting binding (#120). The value is stored under
   * `plugins.plugin_settings.<pluginID>.<key>`. No session token required —
   * this is the same atomic path the generic SettingsForm uses.
   */
  updatePluginSetting: (key: string, value: any) => Promise<boolean>
  /**
   * Subscribe to a typed host event (#106). Returns an unsubscribe function;
   * the host also auto-cleans every subscription on plugin disable/uninstall/
   * vault close, so a plugin cannot leak listeners across reloads. The
   * recommended debounce pattern for high-frequency events (esp. block:changed)
   * is the plugin's responsibility.
   *
   * Initial event set:
   *   - 'block:changed'            → BlockChangedEvent
   *   - 'config:changed'           → SystemConfig (full config snapshot)
   *   - 'active-notebook:changed'  → ActiveNotebookChangedEvent
   *   - 'selection:changed'        → SelectionChangedEvent
   */
  on: <E extends PluginEventName>(
    event: E,
    cb: (payload: PluginEventPayload<E>) => void
  ) => () => void

  // --- Expanded content API (#104) --------------------------------------

  /** Query helpers: typed wrappers over sqliteQuery (read-only, no grant). */
  queryByTag: (path: string) => Promise<SqliteQueryResult>
  queryByDateRange: (start: string, end: string) => Promise<SqliteQueryResult>
  fullTextSearch: (query: string) => Promise<SqliteQueryResult>
  getBacklinks: (uuid: string) => Promise<SqliteQueryResult>
  getEmbeds: (uuid: string) => Promise<SqliteQueryResult>
  /**
   * FTS5 block search (#303 dependency picker, embed insertion). Returns
   * matching blocks with breadcrumb + clean-content metadata. Wraps the
   * SearchBlocks binding so plugin code never imports wailsjs/go/main/App.js
   * directly (AGENTS.md — deprecated, breaks on per-plugin webviews #151/#152).
   */
  searchBlocks: (query: string) => Promise<SearchHit[]>
  /**
   * FTS5 search constrained to TASK blocks (#303). The dependency picker uses
   * this so a non-task (note/header/code) can never be added as a
   * `[blocked_by::]` prerequisite — OpenBlockers JOINs tasks, so a non-task
   * blocker would silently never appear in the DONE-confirm dialog and could
   * never be cleared, leaving the dependent permanently "blocked".
   */
  searchTasks: (query: string) => Promise<SearchHit[]>

  /**
   * Block CRUD (#104). These reuse the same atomic-write + re-index path as
   * the core editor. Gated by the content-mutate capability (#156).
   * createBlock returns the new block's UUID.
   */
  createBlock: (opts: {
    type: 'TASK' | 'NOTE' | 'HEADER'
    text: string
    after?: string
    notebook?: string
    section?: string
    page?: string
  }) => Promise<string>
  deleteBlock: (uuid: string) => Promise<boolean>
  moveBlock: (
    uuid: string,
    opts: { after?: string; notebook?: string; section?: string; page?: string }
  ) => Promise<boolean>
  /**
   * Apply a batch of create/delete/move ops in a single coalesced write pass.
   * Gated by content-mutate (#156). Each op mirrors createBlock/deleteBlock/
   * moveBlock. Returns true on success.
   */
  applyBlocks: (
    ops: Array<{
      kind: 'create' | 'delete' | 'move'
      type?: 'TASK' | 'NOTE' | 'HEADER'
      text?: string
      blockId?: string
      after?: string
      notebook?: string
      section?: string
      page?: string
    }>
  ) => Promise<boolean>

  /** Page / section / notebook CRUD (sandboxed wrappers over App methods). */
  createPage: (
    notebook: string,
    section: string,
    page: string,
    date?: string
  ) => Promise<string>
  createSection: (notebook: string, section: string) => Promise<boolean>
  createNotebook: (name: string) => Promise<boolean>
  deletePage: (
    notebook: string,
    section: string,
    page: string
  ) => Promise<boolean>
  renamePage: (
    notebook: string,
    section: string,
    oldName: string,
    newName: string
  ) => Promise<boolean>

  // --- Plugin file I/O (#108) — capability-gated (read-files / write-files) ---

  /**
   * Read a file within a notebook (relative path, traversal-guarded). Returns
   * the file bytes as a Uint8Array. Gated by read-files.
   */
  readFile: (notebook: string, relPath: string) => Promise<Uint8Array>
  /**
   * Write a file within a notebook atomically (temp+fsync+rename, same lock
   * path as note writes). Restricted to attachments/ + plugin scratch dirs.
   * Gated by write-files.
   */
  writeFile: (
    notebook: string,
    relPath: string,
    data: Uint8Array
  ) => Promise<boolean>
  /** Delete a file within a notebook. Gated by write-files. */
  deleteFile: (notebook: string, relPath: string) => Promise<boolean>
  /** List the immediate children of a directory within a notebook. Gated by read-files. */
  listDir: (notebook: string, relPath: string) => Promise<string[]>
  /** Resolve a notebook's absolute root dir (in-vault or linked per #100). Gated by read-files. */
  notebookRoot: (notebook: string) => Promise<string>
  /** Get (and lazily create) this plugin's per-notebook scratch dir. Gated by write-files. */
  scratchDir: (notebook: string) => Promise<string>
  /** Get (and lazily create) this plugin's vault-scoped scratch dir (caches). Gated by write-files. */
  vaultScratchDir: () => Promise<string>
  /** Resolve a relative asset path against a notebook root. Gated by read-files. */
  resolveAsset: (notebook: string, relPath: string) => Promise<string>
  /** Read a file from the plugin's own install directory (bundled assets). */
  readPluginAsset: (relPath: string) => Promise<string>
  /** Get the navigation tree (notebook > section > page). Read-only. */
  getNavigationTree: () => Promise<{
    notebooks: Array<{
      name: string
      sections: Array<{ name: string; pages: Array<{ name: string }> }>
    }>
  }>

  // --- OS integration (#114) — capability-gated ---------------------------

  /** Open a notebook file in the OS native handler. Gated by os-open. */
  openInNativeHandler: (notebook: string, relPath: string) => Promise<boolean>
  /** Open a URL (http/https/mailto only) in the system browser. Gated by os-open. */
  openUrl: (url: string) => Promise<boolean>
  /** Native open-file picker (user-driven; returns the chosen path or ""). */
  pickOpenFile: (filterPattern?: string) => Promise<string>
  /** Native save-file picker (user-driven; returns the chosen path or ""). */
  pickSaveFile: (defaultFilename?: string) => Promise<string>
  /** Read the system clipboard (text). Gated by os-clipboard. */
  clipboardRead: () => Promise<string>
  /** Write text to the system clipboard. Gated by os-clipboard. */
  clipboardWrite: (text: string) => Promise<boolean>
  /** Show a desktop notification. Gated by os-notify. */
  notify: (opts: { title: string; body: string }) => Promise<boolean>

  // --- Network / fetch (#115) — capability-gated ---------------------------

  /**
   * HTTP fetch through the Go-side proxy (CORS-free, with timeout/size/
   * redirect caps). Host + status are audit-logged (never the body). Gated by
   * the network capability.
   */
  fetch: (
    url: string,
    opts?: {
      method?: string
      headers?: Record<string, string>
      body?: string
      timeoutMs?: number
    }
  ) => Promise<{
    status: number
    headers: Record<string, string>
    body: string
    ok: boolean
    truncated: boolean
  }>

  // --- Editor extension points (#110) ------------------------------------

  /**
   * Register a slash-menu command (#110). The command appears in the `/` menu
   * alongside built-ins; when selected, `onSelect` is called with the live
   * TipTap editor instance + cursor position. The id is namespaced as
   * `<this plugin's id>:<id>` to avoid collisions. Returns an unregister fn.
   * Registration is user-driven (a menu item) so it is not capability-gated;
   * the handler's own privileged calls route through the normal gates.
   */
  registerSlashCommand: (cmd: {
    id: string
    label: string
    description?: string
    icon?: string
    onSelect: (editor: unknown, pos: number) => void
  }) => () => void

  /**
   * Register a read-only decoration provider (#110). The provider is called
   * on each editor render with the current doc and returns an array of
   * decoration specs (from/to/class). Decorations are transient — never
   * persisted. Returns an unregister function.
   */
  provideDecorations: (
    id: string,
    provider: (
      doc: unknown
    ) => Array<{ from: number; to: number; class?: string }>
  ) => () => void

  // --- Rendered UI surfaces (#117) — capability-gated ---------------------

  /**
   * Register a rendered UI surface (#117). The surface HTML runs in a sandboxed
   * iframe (srcdoc, allow-scripts but not allow-same-origin); a postMessage
   * bridge proxies this PluginContext into the iframe. Theme tokens are
   * injected so the surface matches the active theme. Gated by ui-surface.
   * Returns an unregister function.
   */
  registerSurface: (surface: {
    id: string
    kind:
      | 'sidebar-panel'
      | 'modal'
      | 'status-bar-item'
      | 'command-palette-entry'
      | 'settings-panel'
      | 'note-banner'
    label: string
    icon?: string
    html: string
  }) => () => void

  // --- Attachments (#101) -------------------------------------------------

  /**
   * Copy a source file (absolute path) into the notebook's attachments/
   * directory and return the relative link path. Collision-safe (counter
   * suffix on duplicate names). Resolves against the notebook's actual root
   * (#100, in-vault or linked). #101.
   */
  addAttachment: (srcPath: string, notebook?: string) => Promise<string>
  /** Open an attachment in the OS native handler. #101. */
  openAttachment: (notebook: string, relPath: string) => Promise<boolean>
  /** Delete an attachment file (unlink-only; orphan GC is separate). #101. */
  deleteAttachment: (notebook: string, relPath: string) => Promise<boolean>

  // --- Per-plugin SQLite store (#213) — capability-gated ------------------

  /**
   * The per-plugin SQLite store (gated by the 'plugin-db' capability). A
   * distinct connection from the core index, at
   * <vault>/.system/plugins/<id>/data/plugin.db; sqlite-vec is registered
   * (vec0 virtual tables + vec_distance_cosine). The plugin owns its schema
   * and chooses durability semantics. #213.
   */
  pluginDb: PluginDbApi

  // --- Core AI service (#216) — capability-gated --------------------------

  /**
   * The core AI service. A plugin calls `ctx.ai.complete` / `ctx.ai.embed` to
   * run chat completions and embedding batches against the user-configured
   * provider (Ollama / llama.cpp / OpenRouter / LM Studio / OpenAI). The host
   * reads provider credentials server-side and proxies the call, so a plugin
   * NEVER sees API keys or endpoint URLs — it only passes messages/texts and
   * receives results. Gated by the `ai` capability; rate-limited and
   * audit-logged exactly like `ctx.fetch`.
   *
   * The chat LLM and the embedding model are EACH independently configured by
   * the user (Settings → AI Provider): a plugin that needs both talks to two
   * different endpoints through the same two methods.
   */
  ai: PluginAIApi
}

/**
 * The core AI service API (#216). `complete` targets the configured chat model;
 * `embed` targets the configured embedding model. Both are Go-side proxies: the
 * plugin never handles credentials, and every call is rate-limited + audit-
 * logged. Errors surface as a typed `code` (matching the backend's AIErrorKind)
 * so a plugin can branch on "unauthorized" vs "rate-limited" vs "model-missing".
 */
export interface PluginAIApi {
  /**
   * Run a chat completion against the configured chat provider. Returns the
   * generated content, the model that served it, and (when the provider
   * reports it) a token-usage summary. `stream` is accepted now so the
   * signature is additive when streaming lands (Sprint 22); the non-streaming
   * buffer is returned regardless.
   *
   * Rejections carry `code` set to a normalized kind: 'unauthorized',
   * 'rate-limited', 'model-missing', 'timeout', 'unreachable', 'bad-request',
   * 'forbidden', 'server', or 'unknown'.
   */
  complete: (req: {
    messages: PluginAIChatMessage[]
    model?: string
    temperature?: number
    maxTokens?: number
    stream?: boolean
  }) => Promise<PluginAICompleteResult>
  /**
   * Compute embeddings for a batch of texts against the configured embedding
   * provider. The whole batch is sent in one request; `embeddings[i]`
   * corresponds to `texts[i]`. `dimensions` overrides the provider's native
   * vector length (truncation) for this call when supported.
   */
  embed: (req: {
    texts: string[]
    model?: string
    dimensions?: number
  }) => Promise<PluginAIEmbedResult>
}

/** One message in a chat-completion conversation. */
export interface PluginAIChatMessage {
  role: 'system' | 'user' | 'assistant'
  content: string
}

/** Result of `ctx.ai.complete`. usage is present only when the provider reports it. */
export interface PluginAICompleteResult {
  content: string
  model: string
  usage?: {
    promptTokens?: number
    completionTokens?: number
    totalTokens?: number
  }
}

/** Result of `ctx.ai.embed`. embeddings[i] is the vector for texts[i]. */
export interface PluginAIEmbedResult {
  embeddings: number[][]
  model: string
  dimensions: number
  usage?: {
    promptTokens?: number
    totalTokens?: number
  }
}

/**
 * The per-plugin SQLite store API (#213). exec permits DDL/DML (ATTACH/DETACH
 * and non-user_version PRAGMAs are blocked); query is SELECT/WITH-only with a
 * row cap; migrate applies a forward-only schema migration stamped via
 * PRAGMA user_version.
 */
export interface PluginDbApi {
  /** Execute a write (DDL or DML). ATTACH/DETACH and escaping PRAGMAs blocked. */
  exec: (sql: string, params?: unknown[]) => Promise<void>
  /** Read-only query (SELECT/WITH only). Row-capped; { rows, truncated }. */
  query: (
    sql: string,
    params?: unknown[]
  ) => Promise<{ rows: Record<string, unknown>[]; truncated: boolean }>
  /** Forward-only schema migration; stamps PRAGMA user_version = version. */
  migrate: (version: number, sql: string) => Promise<void>
}

// --- v2 SDK typed event bus (#106) ---------------------------------------

/** Names of the host events a plugin may subscribe to via ctx.on. */
export type PluginEventName =
  | 'block:changed'
  | 'config:changed'
  | 'active-notebook:changed'
  | 'selection:changed'
  | 'editor:save'

/** Payload of the 'block:changed' event — mirrors Go parser.BlockChangedEvent. */
export interface BlockChangedEvent {
  id: string
  notebook: string
  section: string
  page: string
  file_date: string
}

/** Payload of the 'active-notebook:changed' event (#106). Emitted when the
 *  navigator focus moves between notebook/section/page. */
export interface ActiveNotebookChangedEvent {
  notebook: string
  section: string
  page: string
}

/** Payload of the 'selection:changed' event from the TipTap editor (#106/#110). */
export interface SelectionChangedEvent {
  notebook: string
  section: string
  page: string
  /** Block id at the selection anchor, when inside a known block. */
  blockId?: string
}

/** Maps an event name to its typed payload (single source of truth). */
export type PluginEventPayload<E extends PluginEventName> = {
  'block:changed': BlockChangedEvent
  'config:changed': Record<string, unknown>
  'active-notebook:changed': ActiveNotebookChangedEvent
  'selection:changed': SelectionChangedEvent
  'editor:save': ActiveNotebookChangedEvent
}[E]

/** A capability id from the v2 SDK capability taxonomy (#113). */
export type Capability =
  | 'read-files'
  | 'write-files'
  | 'network'
  | 'os-open'
  | 'os-clipboard'
  | 'os-notify'
  | 'ui-surface'
  | 'editor-schema'
  | 'content-mutate'
  | 'plugin-db'
  | 'ai'

/** A capability scope qualifier (#113). 'granted' is the default whole-scope. */
export type CapabilityQualifier = 'granted' | 'notebook' | 'vault'

export interface PluginManifest {
  id: string
  name: string
  version: string
  author?: string
  description?: string
  icon?: string
  minSiltVersion?: string
  /**
   * The v2 SDK capability declaration (#113): capability id → true | scope
   * qualifier. Surfaced to the user at install; granted on first use.
   * Absent for plugins that use only the read-only SDK.
   */
  capabilities?: Record<string, true | CapabilityQualifier>
  /**
   * Declarative settings schema (#103). Settings → Plugins renders the form
   * generically from this; no plugin hand-rolls its settings panel. Each field
   * declares a type, a default, and optional validation. Resolution precedence
   * is user-global → vault → notebook (notebook-attached overrides via #100's
   * co-located config). Plugins read the merged value via ctx.getSetting(key).
   */
  settings?: SettingSchema[]
}

/** A single declarative settings field (#103). */
export interface SettingSchema {
  /** The settings key (stored under plugin_settings.<pluginID>.<key>). */
  key: string
  /** Human-readable label shown in the generated form. */
  label: string
  /** Field type — drives the generated input control. */
  type: 'string' | 'number' | 'bool' | 'select' | 'color' | 'keymap' | 'list'
  /** Default value when no setting is stored. */
  default?: unknown
  /** For 'select': the selectable options. */
  options?: string[]
  /** Optional help text under the field. */
  help?: string
  /** For 'string': min/max length validation. */
  minLength?: number
  maxLength?: number
  /** For 'number': min/max range validation. */
  min?: number
  max?: number
}

export interface SiltPlugin {
  manifest: PluginManifest
  /** Called once when the plugin is loaded; receives the host context. */
  init?: (ctx: PluginContext) => void
  /** Called after init once a vault is open and the context is fully usable (#106). */
  onVaultOpen?: (ctx: PluginContext) => void
  /** Called before the active vault tears down (workspace switch / app close) so
   *  the plugin can release watchers/timers. #106. */
  onVaultClose?: () => void
  /** Called during app shutdown, after onVaultClose. Best-effort: IPC may be
   *  tearing down. #106. */
  onShutdown?: () => void
}

// A renderable, registered plugin. First-party plugins supply a compiled
// Svelte component; on-disk (third-party) plugins supply one via the loader
// host when possible.
export interface RegisteredPlugin {
  manifest: PluginManifest
  /** Svelte component rendered for the plugin's view. */
  component: any
  /**
   * Optional primary sidebar component (#321). When the plugin's view is
   * the active sidebar context, `Sidebar.svelte` resolves this component
   * via the same `loadedPlugins.plugins.get(...)` lookup the main view
   * uses, and renders it in place of the Notebook › Section › Page tree.
   *
   * The component receives `{ ctx, manifest }` as props — the same
   * PluginContext the main view receives, with the session token attached
   * (#151/#236). A plugin that omits this field falls back to the page
   * tree (the previous default behavior).
   *
   * This slot is for first-party compiled Svelte components. Third-party
   * plugins render sidebar content via the existing iframe surface system
   * (`registerSurface({ kind: 'sidebar-panel', ... })`, #117); they do
   * not use this field. The split is intentional — third-party plugins
   * ship JS, not compiled Svelte, and the iframe bridge is the only safe
   * way to render untrusted code in the host webview today.
   */
  sidebarComponent?: any
  /**
   * Optional bespoke Settings page component (#214). When present, the plugin
   * contributes a dedicated tab to the Settings shell rendered from this
   * compiled Svelte component (instead of the generic SettingSchema[] form).
   * The component receives `{ ctx, manifest }` as props, the same as
   * sidebarComponent.
   *
   * A plugin declares EITHER settingsPageComponent OR manifest.settings
   * (the generic schema), NOT both — registering both is a configuration
   * error rejected by the registry. Third-party plugins render a bespoke
   * page via the existing `settings-panel` iframe surface
   * (registerSurface({ kind: 'settings-panel', ... })); they do not use
   * this field.
   */
  settingsPageComponent?: any
  /** Optional init hook invoked with the live PluginContext. */
  init?: (ctx: PluginContext) => void
  /** v2 lifecycle hooks (#106) — invoked by the host loader. */
  onVaultOpen?: (ctx: PluginContext) => void
  onVaultClose?: () => void
  onShutdown?: () => void
  /** Origin: bundled with the app vs loaded from .system/plugins/. */
  source: 'first-party' | 'disk'
}

export interface LoadedPlugins {
  plugins: Map<string, RegisteredPlugin>
  errors: { id: string; message: string }[]
  /**
   * False during the vault-switch window where sessionTokens have been
   * cleared but the next loadPlugins has not yet re-registered them.
   * Sidebar/PluginView gate context construction on this so they never
   * capture a context with an empty session token (#326 item 5).
   */
  loadersReady: boolean
}
