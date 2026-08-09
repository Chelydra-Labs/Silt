package db

// Block location and page-block read queries. One file per read domain in
// package db (ARCHITECTURE.md points at the package, not individual files).
// All methods stay on *DatabaseManager.

import (
	"database/sql"
	"fmt"

	"silt/backend/parser"
)

// BlockLocation holds the file-level coordinates of a block, used by write
// paths (UpdateBlockState, MutateBlock, PluginUpdateTaskMeta) to resolve the
// on-disk file path from a block UUID. Source ('vault' | 'linked:<id>') tells
// the path-resolution layer which root the file lives under (#100).
type BlockLocation struct {
	Source    string
	Notebook  string
	Section   string
	Page      string
	BlockType string
}

// GetBlockLocation looks up the (source, notebook, section, page, type) for a
// block UUID. This is the typed API replacement for the raw SQLDB().QueryRow
// calls that were scattered across app.go write paths.
func (dm *DatabaseManager) GetBlockLocation(blockID string) (BlockLocation, error) {
	db, release, err := dm.handle()
	if err != nil {
		return BlockLocation{}, ErrDBClosed
	}
	defer release()
	var loc BlockLocation
	err = db.QueryRow(
		"SELECT COALESCE(source, 'vault'), notebook, section, page, type FROM blocks WHERE id = ?",
		blockID,
	).Scan(&loc.Source, &loc.Notebook, &loc.Section, &loc.Page, &loc.BlockType)
	if loc.Source == "" {
		loc.Source = "vault"
	}
	return loc, err
}

// PageBlockCount is one GROUP BY row from CountBlocksGroupedByPage — block
// totals per (source, notebook, section, page) for the navigation tree.
type PageBlockCount struct {
	Source   string
	Notebook string
	Section  string
	Page     string
	Count    int
}

// CountBlocksGroupedByPage returns block counts keyed by page location.
// Used by ListNavigation so App IPC does not hold a raw SQLDB() pointer
// across the query (lease-aware; post-close returns ErrDBClosed).
func (dm *DatabaseManager) CountBlocksGroupedByPage() ([]PageBlockCount, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(
		`SELECT COALESCE(source, 'vault'), notebook, section, page, COUNT(*)
		 FROM blocks
		 GROUP BY COALESCE(source, 'vault'), notebook, section, page`,
	)
	if err != nil {
		return nil, fmt.Errorf("count blocks by page: %w", err)
	}
	defer rows.Close()
	var out []PageBlockCount
	for rows.Next() {
		var row PageBlockCount
		if err := rows.Scan(&row.Source, &row.Notebook, &row.Section, &row.Page, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// GetBlockReference loads hover/nav fields for a ((uuid)) reference.
// Missing IDs return Exists=false with a nil error (broken-link chip).
// Post-close returns ErrDBClosed.
func (dm *DatabaseManager) GetBlockReference(blockID string) (parser.BlockReference, error) {
	ref := parser.BlockReference{ID: blockID}
	db, release, err := dm.handle()
	if err != nil {
		return ref, ErrDBClosed
	}
	defer release()
	var bType, raw, clean, notebook, section, page, fileDate string
	var ln int
	err = db.QueryRow(
		`SELECT type, raw_content, clean_content, notebook, section, page, file_date, line_number
		 FROM blocks WHERE id = ?`,
		blockID,
	).Scan(&bType, &raw, &clean, &notebook, &section, &page, &fileDate, &ln)
	if err == sql.ErrNoRows {
		return ref, nil
	}
	if err != nil {
		return ref, err
	}
	ref.Exists = true
	ref.Type = bType
	ref.RawText = raw
	ref.CleanText = clean
	ref.Notebook = notebook
	ref.Section = section
	ref.Page = page
	ref.FileDate = fileDate
	ref.LineNumber = ln
	return ref, nil
}

// HasInboundEmbed reports whether any block_references row targets blockID
// with kind=embed ({{embed:uuid}} inbound). Used by MCP get_block.
func (dm *DatabaseManager) HasInboundEmbed(blockID string) (bool, error) {
	db, release, err := dm.handle()
	if err != nil {
		return false, ErrDBClosed
	}
	defer release()
	var one int
	err = db.QueryRow(
		`SELECT 1 FROM block_references WHERE target_block_id = ? AND kind = 'embed' LIMIT 1`,
		blockID,
	).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// FetchPageBlocks returns a flat ordered list of all blocks for a page.
// A page is a single file; all blocks share the same (notebook, section,
// page) and are ordered by line_number. Each block carries its own file_date.
func (dm *DatabaseManager) FetchPageBlocks(source, notebook, section, page string) ([]parser.ParsedBlock, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	rows, err := db.Query(`
		SELECT b.id, b.parent_id, b.depth, b.type, b.raw_content, b.clean_content, b.line_number,
		       b.file_date,
		       COALESCE(t.status, ''), COALESCE(t.owner, ''), COALESCE(t.start_date, ''), COALESCE(t.due_date, ''), COALESCE(t.priority, 0),
		       t.created_at, t.completed_at, t.manual_order, t.modified_at, t.estimate_minutes
		FROM blocks b
		LEFT JOIN tasks t ON b.id = t.block_id
		WHERE b.source = ? AND b.notebook = ? AND b.section = ? AND b.page = ?
		ORDER BY b.line_number ASC
	`, source, notebook, section, page)
	if err != nil {
		return nil, fmt.Errorf("failed to query page blocks: %w", err)
	}
	defer rows.Close()

	var blocks []parser.ParsedBlock
	for rows.Next() {
		var b parser.ParsedBlock
		var bType, fileDate string
		var parentID sql.NullString
		var status, owner, start, due string
		var priority int
		var createdAt, completedAt, modifiedAt sql.NullString
		var manualOrder, estimateMinutes sql.NullInt64

		if err := rows.Scan(&b.ID, &parentID, &b.Depth, &bType, &b.RawText, &b.CleanText, &b.LineNumber, &fileDate, &status, &owner, &start, &due, &priority, &createdAt, &completedAt, &manualOrder, &modifiedAt, &estimateMinutes); err != nil {
			return nil, err
		}
		if parentID.Valid {
			b.ParentID = parentID.String
		}
		b.Type = parser.BlockType(bType)
		b.Status = status
		b.Owner = owner
		b.StartDate = start
		b.DueDate = due
		b.Priority = priority
		b.FileDate = fileDate
		// Lifecycle metadata (#417): hydrate the nullable caches so a block
		// loaded from the DB and re-rendered preserves its [created::],
		// [completed::], [order::] tokens (otherwise the next save would
		// silently strip them — a round-trip violation, rule 1).
		if createdAt.Valid {
			b.CreatedAt = createdAt.String
		}
		if completedAt.Valid {
			b.CompletedAt = completedAt.String
		}
		if manualOrder.Valid {
			b.ManualOrder = int(manualOrder.Int64)
		}
		// [modified::] / [estimate::] (#439/#440): minutes are the cache;
		// FormatEstimateMinutes is best-effort for re-render from DB.
		if modifiedAt.Valid {
			b.ModifiedAt = modifiedAt.String
		}
		if estimateMinutes.Valid {
			b.Estimate = parser.FormatEstimateMinutes(int(estimateMinutes.Int64))
		}
		blocks = append(blocks, b)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating page blocks: %w", err)
	}
	return blocks, nil
}
