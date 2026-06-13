# Project Strategy: notes# - Sprint 1 (Foundation)

## 1. High-Level Strategy
Our goal is to build the compiled Go backend core and in-memory SQLite indexing engine, which serves as the foundation for the local-first note-taking and task-management lifecycle. 

Key technical strategies:
1. **Scaffold with Wails**: Create a clean Wails + Svelte + TypeScript project shell, relocate assets, and set up a decoupled folder layout.
2. **Custom AST Regex Parser**: Implement a fast line-by-line parser that identifies task checkboxes, keywords, owners, priority, and date limits, and handles parent-child nesting structures.
3. **Volatile In-Memory SQLite Index**: Maintain an ephemeral, highly optimized SQLite instance that is populated on startup. We will write all index insertions inside a single transaction to hit the startup parsing budget (< 450ms for 1,000 files).
4. **Resilient Writing & Self-Correction**: Implement an atomic file-replacement pattern (write to `.tmp` file, flush, rename) and automatically inject block UUIDs (`<!-- id: UUID -->`) during file loading or mutation.
5. **Decoupled Folder Onboarding**: Implement a premium first-run view in Svelte 5. If no workspace is selected, prompt the user with Wails' native folder selector, then scaffold `.system/config.yaml`, default theme, and an initial welcome note.

---

## 2. Requirements Coverage
- **Requirement**: Wails project initialization, base config, and folders setup (Issue #1)
  - *Planned coverage*: Phase 1 / checklist items "Initialize Wails project" and "Scaffold system directories"
- **Requirement**: Move `logo.svg` to appropriate frontend location (Issue #1)
  - *Planned coverage*: Phase 1 / checklist item "Move logo asset"
- **Requirement**: Read notebooks directory recursively & parse YAML frontmatter boundaries (Issue #2)
  - *Planned coverage*: Phase 3 / checklist items "Read folders recursively" and "Parse YAML frontmatter"
- **Requirement**: Regex-based line tokenizer for task checkboxes, owner, temporal windows, priority, and block UUID (Issue #3)
  - *Planned coverage*: Phase 2 / checklist items "Write line tokenizer regex" and "Map tasks to ParsedBlock"
- **Requirement**: Atomic file write system (`.tmp` write, `Sync()`, `Rename()`) and dynamic UUID comment injection (Issue #4)
  - *Planned coverage*: Phase 2 / checklist items "Implement atomic write" and "EnsureBlockID comment injection"
- **Requirement**: In-memory SQLite schema (`blocks`, `tasks`, `tags`), covered indexes, and sub-450ms startup loading (Issue #5)
  - *Planned coverage*: Phase 3 / checklist items "SQLite Schema Creation", "Covered Indexes", and "Optimized Transaction Loader"
- **Requirement**: Recursive fsnotify file watcher with a 300ms cooldown `WriteTracker` to avoid self-triggering loops (Issue #6)
  - *Planned coverage*: Phase 4 / checklist items "WriteTracker Class Implementation" and "fsnotify Watcher Loop"
- **Requirement**: Onboarding vault initializer and directory selection dialog (Issue #24)
  - *Planned coverage*: Phase 5 / checklist items "Vault Check & Scaffolder" and "Folder Selection Dialogue UI"

---

## 3. Implementation Plan

### Phase 1: Wails Project & Layout Initialization
We will set up the workspace shell using Wails `svelte-ts` template, move `logo.svg` to `frontend/src/assets/logo.svg`, configure `wails.json`, initialize Go modules, and establish folder scaffolding.

### Phase 2: Markdown AST Parser, ID Injection & Atomic Writer
We will build the custom markdown line tokenizer and block parser. We will implement `EnsureBlockID` to inject missing UUIDs and the atomic file saver (`.tmp` write, `Sync`, `os.Rename`) to prevent data corruption.

### Phase 3: SQLite Cache & Ingestion Engine
We will set up the in-memory SQLite schema, database manager, and recursive directory scanner. We will run the scanner using a worker pool and write blocks/tasks/tags into the database within a single transaction to guarantee sub-450ms execution.

### Phase 4: Directory Watcher & Loop Cooldown
We will configure `fsnotify` to track external directory changes recursively and update the cache. We will implement the `WriteTracker` cooldown tracker to avoid infinite write feedback loops.

### Phase 5: Onboarding Bridge & First-Run Frontend
We will write the Wails Go bindings for vault initialization. In Svelte 5, we will build a beautiful "Refined Cyber-Ink" onboarding splash screen allowing users to pick their vault folder, then auto-scaffold files (config, theme, welcome daily note) and transition into the app.

### Phase 6: Testing & Verification
We will run automated unit tests for AST parsing and file watching, and manually check compilation and directory scanning.

---

## 4. Execution Checklist

### Phase 1: Project & Layout Setup
- [ ] Initialize temporary Wails project using `wails init -n notes-sharp -t svelte-ts -d _wails_temp`
- [ ] Move files to workspace root and delete `_wails_temp`
- [ ] Relocate `logo.svg` to `frontend/src/assets/logo.svg`
- [ ] Edit `wails.json` with configuration and title "notes#"
- [ ] Initialize Go modules and add dependencies: `fsnotify`, `sqlite` (`modernc.org/sqlite`), `yaml.v3`, `uuid`
- [ ] Set up project folder structure: `backend/core`, `backend/monitor`, `backend/parser`, `backend/db`, `backend/vault`

### Phase 2: AST Parser & File Writer
- [ ] Implement `backend/parser/models.go` with structs `ParsedBlock`, `FileMetadata`, `TaskQueryFilter`, `DayGroup`, `TaskResult`
- [ ] Implement `backend/parser/parser.go` with regex-based line tokenization (state, keyword, owner, dates, priority, block UUID)
- [ ] Implement `EnsureBlockID` function in `backend/parser/parser.go`
- [ ] Implement hierarchical depth and parent-child block parsing
- [ ] Implement `backend/parser/writer.go` with atomic write loop (temp file write, `Sync()`, `os.Rename()`, cleanup on fail)

### Phase 3: SQLite Cache & Scanner
- [ ] Implement `backend/db/db.go` to initialize SQLite in-memory database
- [ ] Implement SQLite schema tables (`blocks`, `tasks`, `tags`) and covered indexes (`idx_blocks_file`, `idx_tasks_dates`, `idx_tags_lookup`)
- [ ] Implement recursive directory scanning in `backend/parser/scanner.go`, ignoring `.system` directories
- [ ] Implement YAML frontmatter boundaries extraction (`notebook`, `section`, `date`, `tags`) in scanner
- [ ] Implement optimized transaction inserts in `backend/db/db.go` for blocks, tasks, and tags

### Phase 4: Watcher & Concurrency Locks
- [ ] Implement `backend/core/coordinator.go` with `ExecutionCoordinator` containing file-level write mutexes and DB RWMutex
- [ ] Implement `backend/monitor/tracker.go` with `WriteTracker` cooldown tracking (300ms window)
- [ ] Implement recursive directory monitoring in `backend/monitor/watcher.go` using `fsnotify`
- [ ] Set up fsnotify event handler to re-index changed files and ignore self-generated updates

### Phase 5: Onboarding & UI Bindings
- [ ] Implement `backend/vault/vault.go` to read/write AppData `settings.json` and scaffold default directories and configuration
- [ ] Expose Go functions in `app.go`: `IsVaultInitialized()`, `InitializeVault()`, `FetchSectionTimeline()`, `UpdateBlockState()`, `QueryTasks()`
- [ ] Create Refined Cyber-Ink style system in `frontend/src/index.css`
- [ ] Build Onboarding Folder Picker overlay screen in `frontend/src/App.svelte` using Wails' `OpenDirectoryDialog`

### Phase 6: Testing & Quality Assurance
- [ ] Write unit tests in `backend/parser/parser_test.go` for line tokenizer and parent-child depth maps
- [ ] Write unit tests in `backend/monitor/tracker_test.go` for WriteTracker cooldown behavior
- [ ] Run automated tests and verify clean exits
- [ ] Execute `wails dev` to run the app, verify onboarding flow, scaffold files, and check log parsing speed
