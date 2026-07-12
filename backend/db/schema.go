package db

import (
	"database/sql"
	"fmt"
	"strings"
)

func (dm *DatabaseManager) initSchema() error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	// Foreign-key enforcement is per-connection.
	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// journal_mode is persistent in the DB file header; on an in-memory DB
	// SQLite silently keeps "memory" (the call still succeeds). Setting WAL
	// here means the first on-disk open creates a WAL-mode file and every
	// later connection — including the plugin read-only handle — inherits it
	// without re-running the pragma.
	if _, err := db.Exec("PRAGMA journal_mode = WAL;"); err != nil {
		return fmt.Errorf("failed to set journal mode: %w", err)
	}

	// Belt-and-suspenders (#79): assert the journal mode actually stuck. Some
	// mounts silently downgrade away from WAL (returning "memory" or "delete"
	// instead of erroring). On an in-memory DB the mode is "memory" which is
	// expected — only assert for on-disk databases.
	if dm.path != "" {
		var mode string
		if err := db.QueryRow("PRAGMA journal_mode;").Scan(&mode); err != nil {
			return fmt.Errorf("failed to read journal mode: %w", err)
		}
		if !strings.EqualFold(mode, "wal") {
			return fmt.Errorf("%w: PRAGMA journal_mode returned %q instead of \"wal\" — the filesystem may not support shared memory", ErrWALRejected, mode)
		}
	}
	// Per-connection pragmas. synchronous=NORMAL is safe under WAL (the WAL
	// itself preserves durability across app crashes; only an OS crash can
	// lose the last few transactions, an acceptable trade for local-first
	// speed). mmap_size memory-maps the file for faster reads on large
	// indexes; cache_size is the per-connection page cache (negative = KB,
	// so -64000 ≈ 64 MB). busy_timeout makes a contended write wait rather
	// than fail instantly.
	pragmas := []string{
		"PRAGMA synchronous = NORMAL;",
		"PRAGMA temp_store = MEMORY;",
		"PRAGMA mmap_size = 268435456;", // 256 MiB mmap threshold
		"PRAGMA cache_size = -64000;",   // 64 MiB page cache
		"PRAGMA busy_timeout = 5000;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("failed to apply pragma %q: %w", p, err)
		}
	}

	// Blocks Table
	//
	// `source` discriminates the notebook root a block belongs to: 'vault' for
	// the classic in-vault notebook, or 'linked:<id>' for an external/linked
	// notebook (#100). It disambiguates same-named notebooks across roots (two
	// "Work" notebooks — one in the vault, one on a synced mount — must not
	// collide on (notebook, section, page)). The index idx_blocks_src_file
	// carries source as its leading column. Markdown is still the source of
	// truth; this column is reproducible from the file tree + the link registry.
	createBlocksTable := `
	CREATE TABLE IF NOT EXISTS blocks (
		id TEXT PRIMARY KEY,
		parent_id TEXT,
		source TEXT NOT NULL DEFAULT 'vault',
		notebook TEXT NOT NULL,
		section TEXT NOT NULL,
		page TEXT NOT NULL,
		file_date TEXT NOT NULL, -- YYYY-MM-DD
		depth INTEGER DEFAULT 0,
		type TEXT NOT NULL,      -- 'TASK', 'NOTE', 'HEADER'
		raw_content TEXT NOT NULL,
		clean_content TEXT NOT NULL,
		line_number INTEGER NOT NULL,
		FOREIGN KEY(parent_id) REFERENCES blocks(id) ON DELETE SET NULL
	);`
	if _, err := db.Exec(createBlocksTable); err != nil {
		return fmt.Errorf("failed to create blocks table: %w", err)
	}

	// Migration: add the `source` discriminator to pre-existing blocks tables
	// (a vault created before #100). Idempotent via the try-ignore pattern used
	// for the tasks columns above; existing rows inherit the 'vault' default.
	for _, col := range []struct{ name, defn string }{
		{"source", "TEXT NOT NULL DEFAULT 'vault'"},
	} {
		alter := fmt.Sprintf("ALTER TABLE blocks ADD COLUMN %s %s", col.name, col.defn)
		if _, err := db.Exec(alter); err != nil {
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("failed to migrate blocks table (add %s): %w", col.name, err)
			}
		}
	}

	// Tasks Metadata Table
	createTasksTable := `
	CREATE TABLE IF NOT EXISTS tasks (
		block_id TEXT PRIMARY KEY,
		status TEXT NOT NULL,    -- 'TODO', 'DOING', 'DONE'
		owner TEXT,
		start_date TEXT,         -- YYYY-MM-DD or NULL
		due_date TEXT,           -- YYYY-MM-DD or NULL
		priority INTEGER,        -- 1, 2, 3
		pinned INTEGER DEFAULT 0,           -- NULL/0/1 tri-state cache: NULL=absent, 0=[pin:: false], 1=[pin:: true]; reproducible from markdown on re-index (#135)
		progress INTEGER DEFAULT 0,         -- 0-100; file-resident user intent (cached for query speed)
		recur TEXT,                         -- recurrence rule (e.g. 'every week'); NULL for one-off tasks (#296)
		comments_count INTEGER DEFAULT 0,   -- count of NOTE descendants under the task (derived cache)
		links_count INTEGER DEFAULT 0,      -- count of ((uuid)) refs in raw_content (derived cache)
		created_at TEXT,                    -- ISO 8601 local [created::] timestamp; NULL when absent (no backfill) (#417)
		completed_at TEXT,                  -- ISO 8601 local [completed::] timestamp; NULL when not DONE (no backfill) (#417)
		manual_order INTEGER,               -- 1-based [order:: N] sort position; NULL when absent (no backfill) (#417)
		modified_at TEXT,                   -- ISO 8601 local [modified::] last task-line touch; NULL when absent (#440)
		estimate_minutes INTEGER,           -- minutes from [estimate::]; NULL when absent (#439)
		subtask_total INTEGER DEFAULT 0,    -- direct TASK children count (derived cache, #434)
		subtask_done INTEGER DEFAULT 0,     -- direct TASK children in DONE (derived cache, #434)
		FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
	);`
	if _, err := db.Exec(createTasksTable); err != nil {
		return fmt.Errorf("failed to create tasks table: %w", err)
	}

	// Migration: add new columns to existing tasks tables (a vault that
	// was created before the pinned/progress/comments_count/links_count
	// columns shipped). SQLite's ALTER TABLE ADD COLUMN is idempotent-
	// safe only via the try-ignore pattern below (it errors if the column
	// already exists). Each column is nullable/defaulted so existing rows
	// stay valid without a data backfill — a re-index populates them.
	for _, col := range []struct{ name, defn string }{
		{"pinned", "INTEGER DEFAULT 0"},
		{"progress", "INTEGER DEFAULT 0"},
		{"recur", "TEXT"}, // nullable, no default — NULL for one-off tasks
		{"comments_count", "INTEGER DEFAULT 0"},
		{"links_count", "INTEGER DEFAULT 0"},
		// #417 task lifecycle metadata: nullable caches re-derivable from
		// the [created::], [completed::], [order::] markdown tokens. NULL
		// means "token absent" (no backfill of pre-existing tasks). The
		// cache is disposable — dropping the index and re-indexing rebuilds
		// these from the markdown source of truth (rule 4).
		{"created_at", "TEXT"},
		{"completed_at", "TEXT"},
		{"manual_order", "INTEGER"},
		// #440 / #439 / #434: re-derivable caches from markdown tokens /
		// block hierarchy. NULL/0 means absent; no backfill of old tasks.
		{"modified_at", "TEXT"},
		{"estimate_minutes", "INTEGER"},
		{"subtask_total", "INTEGER DEFAULT 0"},
		{"subtask_done", "INTEGER DEFAULT 0"},
	} {
		alter := fmt.Sprintf("ALTER TABLE tasks ADD COLUMN %s %s", col.name, col.defn)
		if _, err := db.Exec(alter); err != nil {
			// "duplicate column name" → already migrated; ignore.
			// Any other error is real.
			if !strings.Contains(err.Error(), "duplicate column name") {
				return fmt.Errorf("failed to migrate tasks table (add %s): %w", col.name, err)
			}
		}
	}

	// Tags Table
	createTagsTable := `
	CREATE TABLE IF NOT EXISTS tags (
		block_id TEXT NOT NULL,
		raw_path TEXT NOT NULL,  -- 'work/project/milestone-one'
		level_0 TEXT NOT NULL,   -- 'work'
		level_1 TEXT,            -- 'project'
		level_2 TEXT,            -- 'milestone-one'
		PRIMARY KEY(block_id, raw_path),
		FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
	);`
	if _, err := db.Exec(createTagsTable); err != nil {
		return fmt.Errorf("failed to create tags table: %w", err)
	}

	// Task Dependencies Table — caches the [blocked_by:: ((uuid))] edges parsed
	// from task lines (#301). Each row means "block_id is blocked by
	// blocked_by_id". Both columns FK to blocks(id) ON DELETE CASCADE so a
	// deleted block cleans up its edges both as a dependent and as a blocker.
	// The table is a re-derivable projection of the markdown tokens (rule 4 —
	// SQLite is working memory); the markdown is the source of truth. The
	// reverse-lookup index on blocked_by_id serves the DONE-branch fan-out
	// ("who is blocked by the block I just completed?") and the Kanban/Agenda
	// "blocking me" badge without a full scan.
	createTaskDepsTable := `
	CREATE TABLE IF NOT EXISTS task_dependencies (
		block_id     TEXT NOT NULL,
		blocked_by_id TEXT NOT NULL,
		PRIMARY KEY(block_id, blocked_by_id),
		FOREIGN KEY(block_id)      REFERENCES blocks(id) ON DELETE CASCADE,
		FOREIGN KEY(blocked_by_id) REFERENCES blocks(id) ON DELETE CASCADE
	);`
	if _, err := db.Exec(createTaskDepsTable); err != nil {
		return fmt.Errorf("failed to create task_dependencies table: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_task_deps_blocked_by ON task_dependencies(blocked_by_id)`); err != nil {
		return fmt.Errorf("failed to create task_dependencies reverse-lookup index: %w", err)
	}

	// Block Meta Table — caches the NOTE-block comment-attribution tokens
	// (`[author::]` / `[ts::]`, #418) parsed off NOTE blocks. Sparse
	// projection: a row exists ONLY for NOTE blocks that carry at least one
	// of the tokens, so the majority of NOTE blocks (and every TASK /
	// HEADER / etc.) have no row here. Kept in a SEPARATE table from
	// `blocks` so `blocks` remains the pure block↔location projection and
	// the cache stays disposable — re-indexing from the markdown source of
	// truth reproduces it exactly (rule #4). FK ON DELETE CASCADE mirrors
	// task_dependencies: a deleted block cleans up its meta row.
	// `[author::]`/`[ts::]` apply to NOTE blocks only; `scanTaskTokens`
	// (TASK) has no `author`/`ts` cases, so this table never reflects task
	// data (disjoint token spaces — see scanNoteTokens).
	createBlockMetaTable := `
	CREATE TABLE IF NOT EXISTS block_meta (
		block_id  TEXT PRIMARY KEY,
		author    TEXT,
		timestamp TEXT,
		FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
	);`
	if _, err := db.Exec(createBlockMetaTable); err != nil {
		return fmt.Errorf("failed to create block_meta table: %w", err)
	}

	// Files Table — records the last-seen mtime + size of every indexed file
	// so a warm restart can skip re-parsing/re-indexing unchanged files (#29).
	// Lives in the same (on-disk, WAL) database as the blocks index so it
	// persists across restarts naturally. Keyed by absolute path; a renamed
	// file is treated as a new path, with the stale old path pruned by
	// PruneStaleFiles on the next startup scan.
	createFilesTable := `
	CREATE TABLE IF NOT EXISTS files (
		path       TEXT PRIMARY KEY,
		mtime      INTEGER NOT NULL,
		size       INTEGER NOT NULL,
		indexed_at INTEGER NOT NULL
	);`
	if _, err := db.Exec(createFilesTable); err != nil {
		return fmt.Errorf("failed to create files table: %w", err)
	}

	// Create covered indexes
	indexes := []string{
		// #100: replace the pre-source idx_blocks_file (keyed on notebook..)
		// with a source-aware index. DROP IF EXISTS is a one-time cleanup of a
		// pre-migration vault; CREATE IF NOT EXISTS is a no-op afterwards, so
		// this does not rebuild on every launch.
		"DROP INDEX IF EXISTS idx_blocks_file;",
		"CREATE INDEX IF NOT EXISTS idx_blocks_src_file ON blocks(source, notebook, section, page, file_date);",
		"CREATE INDEX IF NOT EXISTS idx_tasks_dates ON tasks(start_date, due_date) WHERE start_date IS NOT NULL OR due_date IS NOT NULL;",
		"CREATE INDEX IF NOT EXISTS idx_tags_lookup ON tags(level_0, level_1, level_2);",
		// QueryBlocksByTag and the tag filter in QueryTasksWithFilters both
		// filter on raw_path (equality + prefix LIKE 'path/%'); the level_*
		// index above can't serve those. Prefix-LIKE is sargable under the
		// default BINARY collation.
		"CREATE INDEX IF NOT EXISTS idx_tags_raw_path ON tags(raw_path);",
		// Functional indexes for case-insensitive search (SearchBlocks).
		"CREATE INDEX IF NOT EXISTS idx_blocks_clean_lower ON blocks(LOWER(clean_content));",
		"CREATE INDEX IF NOT EXISTS idx_blocks_notebook_lower ON blocks(LOWER(notebook));",
		"CREATE INDEX IF NOT EXISTS idx_blocks_section_lower ON blocks(LOWER(section));",
	}

	for _, idxQuery := range indexes {
		if _, err := db.Exec(idxQuery); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	// FTS5 full-text index for SearchBlocks (#39). External-content table
	// linked to blocks by rowid, kept in sync by AFTER INSERT/UPDATE/DELETE
	// triggers so every code path that mutates blocks (IndexFileBlocks,
	// IndexScanResults, ClearFileBlocks) keeps the FTS index consistent
	// without each caller knowing about FTS. Created once; on first creation
	// we rebuild from any pre-existing blocks rows so the migration is
	// additive and lossless.
	// Use ensureFTSOn (unlocked) — initSchema already holds the handle() lease.
	if err := ensureFTSOn(db); err != nil {
		return fmt.Errorf("failed to initialize FTS index: %w", err)
	}

	return nil
}

// ensureFTS creates the blocks_fts virtual table and its sync triggers if they
// do not yet exist, and (on first creation) repopulates FTS from the current
// blocks table. Idempotent: a no-op on every subsequent open where the FTS
// table already exists and the triggers are in place.
func (dm *DatabaseManager) ensureFTS() error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	return ensureFTSOn(db)
}

// ensureFTSOn is the unlocked body of ensureFTS for callers that already hold
// a handle() lease (initSchema) — re-entering handle() would deadlock (#517).
func ensureFTSOn(db *sql.DB) error {
	var ftsExists int
	if err := db.QueryRow(
		"SELECT count(*) FROM sqlite_master WHERE type='table' AND name='blocks_fts'").Scan(&ftsExists); err != nil {
		return fmt.Errorf("failed to check blocks_fts existence: %w", err)
	}

	// External-content FTS5: the virtual table mirrors blocks.clean_content,
	// notebook, and section, linked by the implicit rowid. Queries join back
	// to blocks on rowid.
	createFTS := []string{
		`CREATE VIRTUAL TABLE IF NOT EXISTS blocks_fts USING fts5(
			clean_content, notebook, section,
			content='blocks', content_rowid='rowid',
			tokenize='unicode61'
		);`,
		`CREATE TRIGGER IF NOT EXISTS blocks_fts_ai AFTER INSERT ON blocks BEGIN
			INSERT INTO blocks_fts(rowid, clean_content, notebook, section)
			VALUES (new.rowid, new.clean_content, new.notebook, new.section);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS blocks_fts_ad AFTER DELETE ON blocks BEGIN
			INSERT INTO blocks_fts(blocks_fts, rowid, clean_content, notebook, section)
			VALUES ('delete', old.rowid, old.clean_content, old.notebook, old.section);
		END;`,
		`CREATE TRIGGER IF NOT EXISTS blocks_fts_au AFTER UPDATE ON blocks BEGIN
			INSERT INTO blocks_fts(blocks_fts, rowid, clean_content, notebook, section)
			VALUES ('delete', old.rowid, old.clean_content, old.notebook, old.section);
			INSERT INTO blocks_fts(rowid, clean_content, notebook, section)
			VALUES (new.rowid, new.clean_content, new.notebook, new.section);
		END;`,
	}
	for _, q := range createFTS {
		if _, err := db.Exec(q); err != nil {
			return fmt.Errorf("failed to create FTS object: %w", err)
		}
	}

	// First creation: populate FTS from whatever blocks rows already exist
	// (the migration case — an upgraded vault with blocks but no FTS yet).
	if ftsExists == 0 {
		if _, err := db.Exec("INSERT INTO blocks_fts(blocks_fts) VALUES ('rebuild');"); err != nil {
			return fmt.Errorf("failed to rebuild FTS index: %w", err)
		}
	}
	return nil
}

// RebuildFTSIndex forces a full repopulation of blocks_fts from the current
// blocks table. Call this after a bulk reindex or any path that bypassed the
// sync triggers (none in normal operation, but available for recovery). On an
// empty blocks table this is a no-op.
func (dm *DatabaseManager) RebuildFTSIndex() error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	_, err = db.Exec("INSERT INTO blocks_fts(blocks_fts) VALUES ('rebuild');")
	return err
}
