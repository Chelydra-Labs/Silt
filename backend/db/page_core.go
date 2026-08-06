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
	"strings"
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

// IndexPageCore replaces a page's core row in one transaction (clear + insert),
// for the projection-only backfill path (warm-upgrade vault_init): pages whose
// blocks were warm-skipped on restart never entered the unified
// IndexFileWithProjection path, so they lack a page_core row until either a
// file touch or this backfill derives it from frontmatter. Mirrors
// IndexPageProjection's standalone-tx shape. Reproducible from frontmatter
// (cardinal rule 4); an empty core is valid (untyped page).
func (dm *DatabaseManager) IndexPageCore(source, notebook, section, page string, core PageCoreFields) error {
	db, release, err := dm.handle()
	if err != nil {
		return ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()
	if err := applyPageCoreTx(tx, source, notebook, section, page, core); err != nil {
		return err
	}
	return tx.Commit()
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

// ListPageCoreTypeMatches returns page locators whose page_core.type matches
// any of the given type ids or display names (case-insensitive). Used by the
// #900 reserved-property migration to find pages that may not yet appear in
// page_types (e.g. warm-skipped files before projection backfill).
func (dm *DatabaseManager) ListPageCoreTypeMatches(typeIDs, typeNames []string) ([]TypedPageLocator, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()

	refs := make([]string, 0, len(typeIDs)+len(typeNames))
	seen := map[string]bool{}
	for _, id := range typeIDs {
		id = strings.TrimSpace(id)
		if id == "" || seen[strings.ToLower(id)] {
			continue
		}
		seen[strings.ToLower(id)] = true
		refs = append(refs, id)
	}
	for _, name := range typeNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		refs = append(refs, name)
	}
	if len(refs) == 0 {
		return nil, nil
	}

	// SQLite LOWER(type) IN (...) so display-name refs match case-insensitively.
	placeholders := make([]string, len(refs))
	args := make([]any, len(refs))
	for i, r := range refs {
		placeholders[i] = "LOWER(?)"
		args[i] = strings.ToLower(r)
	}
	query := "SELECT source, notebook, section, page, type FROM page_core " +
		"WHERE LOWER(type) IN (" + strings.Join(placeholders, ",") + ") " +
		"AND type != '' " +
		"ORDER BY source, notebook, section, page"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query page_core by type: %w", err)
	}
	defer rows.Close()

	var out []TypedPageLocator
	for rows.Next() {
		var loc TypedPageLocator
		if err := rows.Scan(&loc.Source, &loc.Notebook, &loc.Section, &loc.Page, &loc.TypeName); err != nil {
			return nil, fmt.Errorf("failed to scan page_core type match: %w", err)
		}
		out = append(out, loc)
	}
	return out, rows.Err()
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
