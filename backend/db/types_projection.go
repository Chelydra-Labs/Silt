package db

import (
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ProjectedProperty is one set property value of a page, as projected into the
// working-memory index. ValueText is the human value; ValueSort is a
// type-correct coercion for uniform ordering (numbers zero-padded, dates as
// ISO, text as-is); ValueType is the schema property type. The App layer
// computes all three from the type schema so this package stays schema-free.
type ProjectedProperty struct {
	Property  string `json:"property"`
	ValueText string `json:"value_text"`
	ValueSort string `json:"value_sort"`
	ValueType string `json:"value_type"`
}

// PageProjectionRow is a page's typed projection: its coordinates, its type,
// and its set property values. Returned by dashboard queries.
type PageProjectionRow struct {
	Source     string              `json:"source"`
	Notebook   string              `json:"notebook"`
	Section    string              `json:"section"`
	Page       string              `json:"page"`
	TypeName   string              `json:"type_name"`
	Properties []ProjectedProperty `json:"properties"`
}

// clearPageProjectionTx deletes a page's page_types and page_properties rows
// on the caller's open transaction. Shared by every atomic projection clear
// path (IndexPageProjection's replace, ClearPageProjection, and the unified
// IndexFileWithProjection / IndexScanResultsWithProjection). The two DELETEs
// run on the same tx so a mid-failure cannot leave one table cleared and
// the other still carrying stale rows.
func clearPageProjectionTx(tx *sql.Tx, source, notebook, section, page string) error {
	if _, err := tx.Exec(
		"DELETE FROM page_types WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	); err != nil {
		return fmt.Errorf("failed to clear page_types: %w", err)
	}
	if _, err := tx.Exec(
		"DELETE FROM page_properties WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	); err != nil {
		return fmt.Errorf("failed to clear page_properties: %w", err)
	}
	return nil
}

// applyPageProjectionTx replaces a page's projection on the caller's open
// transaction: clear prior rows, then (when typeID != "") insert one
// page_types row plus one page_properties row per set property. typeID == ""
// means untyped — clear only, no insert — so the unified block+projection
// index path can atomically drop a page's projection when frontmatter loses
// its `type:` line. The caller has already validated source/notebook/page.
func applyPageProjectionTx(tx *sql.Tx, source, notebook, section, page, typeID string, props []ProjectedProperty) error {
	if err := clearPageProjectionTx(tx, source, notebook, section, page); err != nil {
		return err
	}
	if typeID == "" {
		return nil
	}
	if _, err := tx.Exec(
		"INSERT INTO page_types (source, notebook, section, page, type_name) VALUES (?, ?, ?, ?, ?)",
		source, notebook, section, page, typeID,
	); err != nil {
		return fmt.Errorf("failed to insert page_types: %w", err)
	}
	for _, p := range props {
		if _, err := tx.Exec(
			"INSERT INTO page_properties (source, notebook, section, page, type_name, property, value_text, value_sort, value_type) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			source, notebook, section, page, typeID, p.Property, p.ValueText, p.ValueSort, p.ValueType,
		); err != nil {
			return fmt.Errorf("failed to insert page_properties: %w", err)
		}
	}
	return nil
}

// IndexPageProjection replaces a page's type projection in one transaction:
// delete any existing page_types/page_properties rows for the page, then insert
// a page_types row (the page is of typeID) and one page_properties row per set
// property. Call ClearPageProjection instead when a page becomes untyped.
// Reproducible from frontmatter + the type schema (cardinal rule 4).
//
// Projection-only path: external-edit re-projection (onExternalPageChanged)
// and schema-triggered re-projection (reprojectAllTypedPages) still use this
// because they have no blocks to update. Every frontmatter-affecting block
// write path now routes through IndexFileWithProjection so blocks and
// projection share one transaction.
func (dm *DatabaseManager) IndexPageProjection(source, notebook, section, page, typeID string, props []ProjectedProperty) error {
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
	if err := applyPageProjectionTx(tx, source, notebook, section, page, typeID, props); err != nil {
		return err
	}
	// Test seam: same shape as IndexFileBlocks for the projection-only path.
	if err := dm.runIndexerTestingHook(indexerHookContext{
		Phase:    indexerHookIndexPageProjectionPreCommit,
		Source:   source,
		Notebook: notebook,
		Section:  section,
		Page:     page,
	}); err != nil {
		return fmt.Errorf("indexer testing hook aborted IndexPageProjection: %w", err)
	}
	return tx.Commit()
}

// ClearPageProjection removes a page's type projection (used when a page loses
// its type, is deleted, or its on-disk file vanishes during re-projection).
// Idempotent. Both deletes share one transaction (clearPageProjectionTx) so a
// mid-failure cannot leave page_properties rows orphaned after page_types is
// gone (mirrors ClearFileBlocks / ClearSourceBlocks).
func (dm *DatabaseManager) ClearPageProjection(source, notebook, section, page string) error {
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
		return err
	}
	defer tx.Rollback()
	if err := clearPageProjectionTx(tx, source, notebook, section, page); err != nil {
		return err
	}
	return tx.Commit()
}

// TypedPageLocator is one (source, notebook, section, page, type_name) tuple
// from page_types — enough to re-locate a page's file and clear its projection
// before re-projecting it. Returned by GetAllTypedPageLocators so the App layer
// can re-project every typed page when the type schema changes.
type TypedPageLocator struct {
	Source   string `json:"source"`
	Notebook string `json:"notebook"`
	Section  string `json:"section"`
	Page     string `json:"page"`
	TypeName string `json:"type_name"`
}

// GetAllTypedPageLocators returns every distinct (source, notebook, section,
// page, type_name) tuple currently in page_types. Used by the types watcher's
// re-projection pass so a schema edit reaches pages that have not been touched
// since (the projection would otherwise drift until each page is independently
// re-indexed). Read-only; does not surface property values.
func (dm *DatabaseManager) GetAllTypedPageLocators() ([]TypedPageLocator, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	rows, err := db.Query(
		"SELECT source, notebook, section, page, type_name FROM page_types ORDER BY source, notebook, section, page",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query page_types: %w", err)
	}
	return scanTypedPageLocators(rows)
}

// GetTypedPageLocatorsByIDs returns every distinct (source, notebook, section,
// page, type_name) tuple whose type_name is in typeIDs. Used by the App's
// scoped reprojection worker so a schema edit (save / delete) reaches only
// pages of the affected type(s) — not every typed page in the vault.
//
// Served by idx_page_types_type (CREATE INDEX … ON page_types(type_name)).
// Deduplicates typeIDs before query construction so a caller passing the
// same id twice (e.g. old+new on rename when they happen to coincide) does
// not double-count rows. An empty / deduped-to-empty input returns (nil, nil)
// — the worker treats that as "nothing to do" rather than "reproject all".
//
// Deterministic row order (source, notebook, section, page) so the worker's
// disk-read sequence is reproducible — a test can assert exact visit order.
func (dm *DatabaseManager) GetTypedPageLocatorsByIDs(typeIDs []string) ([]TypedPageLocator, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	deduped := dedupeTypeIDs(typeIDs)
	if len(deduped) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(deduped))
	args := make([]interface{}, len(deduped))
	for i, id := range deduped {
		placeholders[i] = "?"
		args[i] = id
	}
	query := "SELECT source, notebook, section, page, type_name FROM page_types " +
		"WHERE type_name IN (" + strings.Join(placeholders, ",") + ") " +
		"ORDER BY source, notebook, section, page"
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query page_types by type ids: %w", err)
	}
	return scanTypedPageLocators(rows)
}

// dedupeTypeIDs returns typeIDs with duplicates and empty strings removed,
// preserving first-seen order so a test's expected visit sequence is stable.
func dedupeTypeIDs(typeIDs []string) []string {
	if len(typeIDs) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(typeIDs))
	out := make([]string, 0, len(typeIDs))
	for _, id := range typeIDs {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// scanTypedPageLocators drains rows into a TypedPageLocator slice with one
// shared scan loop. Used by both locator lookups so the row-decode contract
// has a single source of truth.
func scanTypedPageLocators(rows *sql.Rows) ([]TypedPageLocator, error) {
	defer rows.Close()
	var out []TypedPageLocator
	for rows.Next() {
		var loc TypedPageLocator
		if err := rows.Scan(&loc.Source, &loc.Notebook, &loc.Section, &loc.Page, &loc.TypeName); err != nil {
			return nil, err
		}
		out = append(out, loc)
	}
	return out, rows.Err()
}

// GetPageProjection returns the projection row for a single page (its type plus
// set property values), or (nil, nil) when the page has no projection (it is
// untyped or not indexed). Used by the properties UI and dashboard lookups.
func (dm *DatabaseManager) GetPageProjection(source, notebook, section, page string) (*PageProjectionRow, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()
	if source == "" {
		source = "vault"
	}
	var row PageProjectionRow
	err = db.QueryRow(
		"SELECT source, notebook, section, page, type_name FROM page_types WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		source, notebook, section, page,
	).Scan(&row.Source, &row.Notebook, &row.Section, &row.Page, &row.TypeName)
	if err != nil {
		// Distinguish "no projection row" (untyped/unindexed) from a real DB
		// error: callers like validateOneRelationTarget would otherwise read a
		// transient error as proj==nil and reject a valid relation as "wrong
		// type", hiding the underlying failure.
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil // no row = untyped/unindexed; not an error
		}
		return nil, fmt.Errorf("failed to query page_types: %w", err)
	}
	rows, err := db.Query(
		"SELECT property, value_text, value_sort, value_type FROM page_properties WHERE source = ? AND notebook = ? AND section = ? AND page = ? ORDER BY property",
		source, notebook, section, page,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query page_properties: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var p ProjectedProperty
		if err := rows.Scan(&p.Property, &p.ValueText, &p.ValueSort, &p.ValueType); err != nil {
			return nil, err
		}
		row.Properties = append(row.Properties, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &row, nil
}

// QueryPagesByType returns every page of the given type with its set property
// values, for the per-type dashboard (typed-notes feature). Source-scoped results
// include both vault and linked-notebook pages. Pages are sorted by
// (notebook, section, page); properties within a page are sorted by name.
func (dm *DatabaseManager) QueryPagesByType(typeName string) ([]PageProjectionRow, error) {
	db, release, err := dm.handle()
	if err != nil {
		return nil, ErrDBClosed
	}
	defer release()

	typeRows, err := db.Query(
		"SELECT source, notebook, section, page FROM page_types WHERE type_name = ? ORDER BY notebook, section, page",
		typeName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query page_types: %w", err)
	}
	var rows []PageProjectionRow
	coords := make([]PageProjectionRow, 0)
	for typeRows.Next() {
		var r PageProjectionRow
		r.TypeName = typeName
		if err := typeRows.Scan(&r.Source, &r.Notebook, &r.Section, &r.Page); err != nil {
			typeRows.Close()
			return nil, err
		}
		coords = append(coords, r)
	}
	if err := typeRows.Err(); err != nil {
		typeRows.Close()
		return nil, err
	}
	typeRows.Close()

	// Fetch all property rows for this type in one pass and bucket them by
	// page coordinates (cheaper than N per-page queries for a large type).
	// No ORDER BY: the only useful index is (type_name, property, value_sort),
	// so an SQL sort would be a temp-sort; the per-page Go sort below fully
	// determines row order anyway.
	propRows, err := db.Query(
		"SELECT source, notebook, section, page, property, value_text, value_sort, value_type FROM page_properties WHERE type_name = ?",
		typeName,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query page_properties: %w", err)
	}
	defer propRows.Close()
	bucket := map[string]int{}
	for i, c := range coords {
		bucket[c.Source+"\x00"+c.Notebook+"\x00"+c.Section+"\x00"+c.Page] = i
	}
	for _, c := range coords {
		rows = append(rows, PageProjectionRow{
			Source: c.Source, Notebook: c.Notebook, Section: c.Section,
			Page: c.Page, TypeName: typeName,
		})
	}
	for propRows.Next() {
		var source, notebook, section, page string
		var p ProjectedProperty
		if err := propRows.Scan(&source, &notebook, &section, &page, &p.Property, &p.ValueText, &p.ValueSort, &p.ValueType); err != nil {
			return nil, err
		}
		idx, ok := bucket[source+"\x00"+notebook+"\x00"+section+"\x00"+page]
		if !ok {
			continue
		}
		rows[idx].Properties = append(rows[idx].Properties, p)
	}
	if err := propRows.Err(); err != nil {
		return nil, err
	}
	// Properties are sorted per-page by name. This is the sole ordering
	// authority for the properties slice — the SQL query above is unordered.
	for i := range rows {
		sort.Slice(rows[i].Properties, func(a, b int) bool {
			return rows[i].Properties[a].Property < rows[i].Properties[b].Property
		})
	}
	return rows, nil
}
