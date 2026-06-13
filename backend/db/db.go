package db

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"

	"notes-sharp/backend/parser"

	_ "modernc.org/sqlite"
)

type DatabaseManager struct {
	DB *sql.DB
}

func NewDatabaseManager() (*DatabaseManager, error) {
	// Open a shared in-memory SQLite database.
	// We use cache=shared so multiple connections can access it if needed,
	// and it persists as long as the main connection remains open.
	db, err := sql.Open("sqlite", "file::memory:?cache=shared")
	if err != nil {
		return nil, fmt.Errorf("failed to open SQLite: %w", err)
	}

	dm := &DatabaseManager{DB: db}
	if err := dm.initSchema(); err != nil {
		db.Close()
		return nil, err
	}

	return dm, nil
}

func (dm *DatabaseManager) Close() error {
	if dm.DB != nil {
		return dm.DB.Close()
	}
	return nil
}

func (dm *DatabaseManager) initSchema() error {
	// Enable foreign key constraints
	_, err := dm.DB.Exec("PRAGMA foreign_keys = ON;")
	if err != nil {
		return fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Optimize pragmas for in-memory speed
	_, err = dm.DB.Exec("PRAGMA journal_mode = MEMORY;")
	if err != nil {
		return fmt.Errorf("failed to set journal mode: %w", err)
	}
	_, err = dm.DB.Exec("PRAGMA synchronous = OFF;")
	if err != nil {
		return fmt.Errorf("failed to set synchronous: %w", err)
	}

	// Blocks Table
	createBlocksTable := `
	CREATE TABLE IF NOT EXISTS blocks (
		id TEXT PRIMARY KEY,
		parent_id TEXT,
		notebook TEXT NOT NULL,
		section TEXT NOT NULL,
		file_date TEXT NOT NULL, -- YYYY-MM-DD
		depth INTEGER DEFAULT 0,
		type TEXT NOT NULL,      -- 'TASK', 'NOTE', 'HEADER'
		raw_content TEXT NOT NULL,
		clean_content TEXT NOT NULL,
		line_number INTEGER NOT NULL,
		FOREIGN KEY(parent_id) REFERENCES blocks(id) ON DELETE SET NULL
	);`
	if _, err := dm.DB.Exec(createBlocksTable); err != nil {
		return fmt.Errorf("failed to create blocks table: %w", err)
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
		FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
	);`
	if _, err := dm.DB.Exec(createTasksTable); err != nil {
		return fmt.Errorf("failed to create tasks table: %w", err)
	}

	// Tags Table
	createTagsTable := `
	CREATE TABLE IF NOT EXISTS tags (
		block_id TEXT NOT NULL,
		raw_path TEXT NOT NULL,  -- 'work/sogav/milestone-one'
		level_0 TEXT NOT NULL,   -- 'work'
		level_1 TEXT,            -- 'sogav'
		level_2 TEXT,            -- 'milestone-one'
		PRIMARY KEY(block_id, raw_path),
		FOREIGN KEY(block_id) REFERENCES blocks(id) ON DELETE CASCADE
	);`
	if _, err := dm.DB.Exec(createTagsTable); err != nil {
		return fmt.Errorf("failed to create tags table: %w", err)
	}

	// Create covered indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_blocks_file ON blocks(notebook, section, file_date);",
		"CREATE INDEX IF NOT EXISTS idx_tasks_dates ON tasks(start_date, due_date) WHERE start_date IS NOT NULL OR due_date IS NOT NULL;",
		"CREATE INDEX IF NOT EXISTS idx_tags_lookup ON tags(level_0, level_1, level_2);",
	}

	for _, idxQuery := range indexes {
		if _, err := dm.DB.Exec(idxQuery); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

// ExtractTags finds inline tags starting with # followed by a letter, ignoring numeric priorities.
func ExtractTags(text string) []string {
	tagRegex := regexp.MustCompile(`\B#([a-zA-Z][a-zA-Z0-9_/]*)`)
	matches := tagRegex.FindAllStringSubmatch(text, -1)
	var tags []string
	seen := make(map[string]bool)
	for _, match := range matches {
		if len(match) > 1 {
			t := match[1]
			if !seen[t] {
				seen[t] = true
				tags = append(tags, t)
			}
		}
	}
	return tags
}

// ClearFileBlocks deletes all blocks, tasks, and tags associated with a specific notebook section day.
func (dm *DatabaseManager) ClearFileBlocks(tx *sql.Tx, notebook, section, fileDate string) error {
	query := "DELETE FROM blocks WHERE notebook = ? AND section = ? AND file_date = ?"
	var err error
	if tx != nil {
		_, err = tx.Exec(query, notebook, section, fileDate)
	} else {
		_, err = dm.DB.Exec(query, notebook, section, fileDate)
	}
	return err
}

// IndexFileBlocks updates the index with a set of blocks in a single transaction.
func (dm *DatabaseManager) IndexFileBlocks(notebook, section, fileDate string, blocks []parser.ParsedBlock, fileTags []string) error {
	tx, err := dm.DB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Clear old blocks first
	if err := dm.ClearFileBlocks(tx, notebook, section, fileDate); err != nil {
		return fmt.Errorf("failed to clear old blocks: %w", err)
	}

	if len(blocks) == 0 {
		return tx.Commit()
	}

	stmtBlock, err := tx.Prepare("INSERT INTO blocks (id, parent_id, notebook, section, file_date, depth, type, raw_content, clean_content, line_number) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmtBlock.Close()

	stmtTask, err := tx.Prepare("INSERT INTO tasks (block_id, status, owner, start_date, due_date, priority) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmtTask.Close()

	stmtTag, err := tx.Prepare("INSERT INTO tags (block_id, raw_path, level_0, level_1, level_2) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmtTag.Close()

	for _, block := range blocks {
		// 1. Insert into blocks
		var parentID interface{}
		if block.ParentID != "" {
			parentID = block.ParentID
		}
		_, err = stmtBlock.Exec(block.ID, parentID, notebook, section, fileDate, block.Depth, string(block.Type), block.RawText, block.CleanText, block.LineNumber)
		if err != nil {
			return fmt.Errorf("failed to insert block %s: %w", block.ID, err)
		}

		// 2. Insert task metadata if it's a task
		if block.Type == parser.BlockTask {
			var owner, startDate, dueDate interface{}
			if block.Owner != "" {
				owner = block.Owner
			}
			if block.StartDate != "" {
				startDate = block.StartDate
			}
			if block.DueDate != "" {
				dueDate = block.DueDate
			}
			_, err = stmtTask.Exec(block.ID, block.Status, owner, startDate, dueDate, block.Priority)
			if err != nil {
				return fmt.Errorf("failed to insert task for block %s: %w", block.ID, err)
			}
		}

		// 3. Extract and insert tags for this block
		tags := ExtractTags(block.RawText)
		// Also index file-level frontmatter tags on the first block of the file
		if block.LineNumber == 1 || len(blocks) == 1 {
			for _, ft := range fileTags {
				trimmedFT := strings.TrimPrefix(ft, "#")
				found := false
				for _, t := range tags {
					if t == trimmedFT {
						found = true
						break
					}
				}
				if !found && trimmedFT != "" {
					tags = append(tags, trimmedFT)
				}
			}
		}

		for _, tagPath := range tags {
			parts := strings.Split(tagPath, "/")
			var level0, level1, level2 interface{}
			if len(parts) > 0 {
				level0 = parts[0]
			}
			if len(parts) > 1 {
				level1 = parts[1]
			}
			if len(parts) > 2 {
				level2 = parts[2]
			}
			_, err = stmtTag.Exec(block.ID, tagPath, level0, level1, level2)
			if err != nil {
				// We can ignore duplicate tags for the same block if it arises,
				// or handle it. PRIMARY KEY is (block_id, raw_path) so it prevents duplicate entries automatically.
				continue
			}
		}
	}

	return tx.Commit()
}
