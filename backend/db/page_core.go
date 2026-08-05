package db

// Type-independent core-metadata projection (#867). page_core is a SEPARATE
// working-memory table from page_types/page_properties (type-scoped) and tags
// (block-scoped index). One row per indexed page (typed OR untyped) carries
// the core fields every page exposes in the PropertiesPanel: type id, date,
// aliases, created. `modified` is read from the files-table mtime cache at
// read time, NOT stored here (a block-only write bumps mtime without touching
// frontmatter, so caching modified in page_core would stale between writes).
//
// All methods stay on *DatabaseManager. The atomic clear/insert helpers mirror
// types_projection.go's clearPageProjectionTx / applyPageProjectionTx so the
// unified IndexFileWithProjection / IndexScanResultsWithProjection paths can
// publish page_core in the SAME transaction as blocks + page_types/
// page_properties — a reader can never observe a half-published core row.

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

// PageCoreFields is the type-independent core payload the App layer computes
// from parsed frontmatter and the indexer projects into page_core. The App
// layer is the source of truth for these values (it parses the file); the
// indexer receives them already-shaped so db stays parser-free.
type PageCoreFields struct {
	Type    string   // note-type id (empty for untyped)
	Date    string   // YYYY-MM-DD
	Aliases []string // frontmatter aliases array (nil = absent)
	Created string   // ISO datetime or YYYY-MM-DD
}

// aliasesJSON encodes aliases as a JSON string array for storage in page_core.
// nil/empty yields "[]" so the column never carries NULL/empty (mirrors
// formatMultiValues for page_properties multi-value cells).
func aliasesJSON(aliases []string) string {
	if aliases == nil {
		aliases = []string{}
	}
	b, err := json.Marshal(aliases)
	if err != nil {
		// Non-stringifiable aliases are impossible from yaml.v3 decode; fall
		// back to an empty array so the row still writes.
		return "[]"
	}
	return string(b)
}

// clearPageCoreTx deletes a page's page_core row on the caller's open
// transaction. Shared by every atomic core clear path (the unified
// IndexFileWithProjection / IndexScanResultsWithProjection replace path, and
// the delete/clear path via ClearFileBlocks / ClearSourceBlocks).
func clearPageCoreTx(tx *sql.Tx, source, notebook, section, page string) error {
	if _, err := tx.Exec(
		"DELETE FROM page_core WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	); err != nil {
		return fmt.Errorf("failed to clear page_core: %w", err)
	}
	return nil
}

// applyPageCoreTx replaces a page's core row on the caller's open transaction:
// clear the prior row, then upsert the new one. Called for EVERY indexed page
// (typed OR untyped) so the panel always has a core row to render — the
// untyped case is the whole point of #867. An empty core struct is valid and
// produces a row with empty type/date/created and "[]" aliases.
func applyPageCoreTx(tx *sql.Tx, source, notebook, section, page string, core PageCoreFields) error {
	if err := clearPageCoreTx(tx, source, notebook, section, page); err != nil {
		return err
	}
	if _, err := tx.Exec(
		"INSERT INTO page_core (source, notebook, section, page, type, date, aliases, created) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		source, notebook, section, page, core.Type, core.Date, aliasesJSON(core.Aliases), core.Created,
	); err != nil {
		return fmt.Errorf("failed to insert page_core: %w", err)
	}
	return nil
}

// PageCoreRow is the read shape of one page's core projection. Aliases is the
// decoded string slice (never nil). Used by GetPageCoreProjection for tests +
// future dashboards; the per-page PropertiesPanel read path composes from
// frontmatter directly (consistent with GetPageType/GetPageProperties).
type PageCoreRow struct {
	Source   string
	Notebook string
	Section  string
	Page     string
	Type     string
	Date     string
	Aliases  []string
	Created  string
}

// GetPageCoreProjection returns a page's page_core row, or (nil, nil) when the
// page has no core row (never indexed, or the projection was cleared). Used by
// tests to assert the projection rebuilds correctly and by future cross-page
// dashboards. The PropertiesPanel read path (App.GetPageCoreMetadata) composes
// from frontmatter + files-table mtime instead.
func (dm *DatabaseManager) GetPageCoreProjection(source, notebook, section, page string) (*PageCoreRow, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	var (
		row        PageCoreRow
		typeVal    string
		dateVal    string
		aliasesVal string
		createdVal string
	)
	err = db.QueryRow(
		"SELECT source, notebook, section, page, type, date, aliases, created FROM page_core WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	).Scan(&row.Source, &row.Notebook, &row.Section, &row.Page, &typeVal, &dateVal, &aliasesVal, &createdVal)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query page_core: %w", err)
	}
	row.Type = typeVal
	row.Date = dateVal
	row.Created = createdVal
	row.Aliases = decodeAliasesJSON(aliasesVal)
	return &row, nil
}

// decodeAliasesJSON parses a page_core.aliases JSON string-array cell back into
// a string slice. A malformed cell yields an empty slice (never nil) so callers
// can iterate without a nil guard.
func decodeAliasesJSON(s string) []string {
	if s == "" {
		return []string{}
	}
	var out []string
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return []string{}
	}
	if out == nil {
		return []string{}
	}
	return out
}
