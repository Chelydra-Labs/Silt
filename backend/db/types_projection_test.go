package db

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"silt/backend/parser"
)

// propsEqual is a shallow, order-insensitive comparison for projected property
// slices — the dashboard's contract is "the right set of values", the row order
// is just a presentation convenience the query already sorts by name.
func propsEqual(a, b []ProjectedProperty) bool {
	if len(a) != len(b) {
		return false
	}
	sorted := func(in []ProjectedProperty) []ProjectedProperty {
		out := append([]ProjectedProperty(nil), in...)
		sort.Slice(out, func(i, j int) bool { return out[i].Property < out[j].Property })
		return out
	}
	a, b = sorted(a), sorted(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestIndexPageProjection_Basic(t *testing.T) {
	dm := newTestDB(t)

	props := []ProjectedProperty{
		{Property: "status", ValueText: "in progress", ValueSort: "in progress", ValueType: "select"},
		{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"},
	}
	if err := dm.IndexPageProjection("vault", "Work", "Sprint", "Board", "task", props); err != nil {
		t.Fatalf("IndexPageProjection: %v", err)
	}

	row, err := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil {
		t.Fatalf("expected projection row, got nil")
	}
	if row.TypeName != "task" {
		t.Errorf("type_name = %q, want %q", row.TypeName, "task")
	}
	if !propsEqual(row.Properties, props) {
		t.Errorf("properties = %+v, want %+v", row.Properties, props)
	}

	pages, err := dm.QueryPagesByType("task")
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(pages) != 1 {
		t.Fatalf("expected 1 page of type task, got %d", len(pages))
	}
	if pages[0].Page != "Board" {
		t.Errorf("page = %q, want %q", pages[0].Page, "Board")
	}
}

func TestIndexPageProjection_ReplacesOnReindex(t *testing.T) {
	dm := newTestDB(t)

	first := []ProjectedProperty{
		{Property: "status", ValueText: "todo", ValueSort: "todo", ValueType: "select"},
		{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"},
	}
	if err := dm.IndexPageProjection("vault", "Work", "Sprint", "Board", "task", first); err != nil {
		t.Fatalf("first index: %v", err)
	}

	// Re-index with the same type but a different property set: owner dropped,
	// priority added. The delete-then-insert must not union old and new.
	second := []ProjectedProperty{
		{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"},
		{Property: "priority", ValueText: "high", ValueSort: "high", ValueType: "text"},
	}
	if err := dm.IndexPageProjection("vault", "Work", "Sprint", "Board", "task", second); err != nil {
		t.Fatalf("reindex: %v", err)
	}

	row, err := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil {
		t.Fatalf("expected projection row, got nil")
	}
	if !propsEqual(row.Properties, second) {
		t.Errorf("properties = %+v, want %+v", row.Properties, second)
	}

	// A row count confirms the sparse insert is replacing rather than appending.
	var n int
	if err := dm.SQLDB().QueryRow(
		"SELECT COUNT(*) FROM page_properties WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		"vault", "Work", "Sprint", "Board",
	).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(second) {
		t.Errorf("property row count = %d, want %d (reindex should replace not union)", n, len(second))
	}
}

func TestIndexPageProjection_TypeChange(t *testing.T) {
	dm := newTestDB(t)

	if err := dm.IndexPageProjection("vault", "Work", "Notes", "Daily", "journal",
		[]ProjectedProperty{{Property: "mood", ValueText: "good", ValueSort: "good", ValueType: "select"}},
	); err != nil {
		t.Fatalf("index type A: %v", err)
	}

	// Flip the page's type. The page_types row must be replaced, not duplicated.
	if err := dm.IndexPageProjection("vault", "Work", "Notes", "Daily", "meeting",
		[]ProjectedProperty{{Property: "attendees", ValueText: "3", ValueSort: "00000000000003.000000", ValueType: "number"}},
	); err != nil {
		t.Fatalf("index type B: %v", err)
	}

	row, err := dm.GetPageProjection("vault", "Work", "Notes", "Daily")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil {
		t.Fatalf("expected projection row, got nil")
	}
	if row.TypeName != "meeting" {
		t.Errorf("type_name = %q, want %q", row.TypeName, "meeting")
	}
	if len(row.Properties) != 1 || row.Properties[0].Property != "attendees" {
		t.Errorf("properties = %+v, want only [attendees]", row.Properties)
	}

	var typeCount int
	if err := dm.SQLDB().QueryRow(
		"SELECT COUNT(*) FROM page_types WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		"vault", "Work", "Notes", "Daily",
	).Scan(&typeCount); err != nil {
		t.Fatalf("count types: %v", err)
	}
	if typeCount != 1 {
		t.Errorf("expected 1 page_types row after type change, got %d", typeCount)
	}
}

func TestClearPageProjection(t *testing.T) {
	dm := newTestDB(t)

	if err := dm.IndexPageProjection("vault", "Work", "Notes", "Daily", "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}},
	); err != nil {
		t.Fatalf("index: %v", err)
	}

	if err := dm.ClearPageProjection("vault", "Work", "Notes", "Daily"); err != nil {
		t.Fatalf("ClearPageProjection: %v", err)
	}

	row, err := dm.GetPageProjection("vault", "Work", "Notes", "Daily")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row != nil {
		t.Errorf("expected nil row after clear, got %+v", row)
	}

	// Both tables must be empty — the two deletes share one transaction so a
	// partial clear cannot leave ghost page_properties rows.
	for _, table := range []string{"page_types", "page_properties"} {
		var n int
		if err := dm.SQLDB().QueryRow(
			"SELECT COUNT(*) FROM "+table+" WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			"vault", "Work", "Notes", "Daily",
		).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if n != 0 {
			t.Errorf("expected 0 %s rows after ClearPageProjection, got %d", table, n)
		}
	}

	// Idempotent: a second clear on the now-empty page is not an error.
	if err := dm.ClearPageProjection("vault", "Work", "Notes", "Daily"); err != nil {
		t.Errorf("second ClearPageProjection should be a no-op: %v", err)
	}
}

func TestQueryPagesByType_MultiplePages(t *testing.T) {
	dm := newTestDB(t)

	// Two pages of the same type with different property values, placed so the
	// (notebook, section, page) sort order is deterministic.
	if err := dm.IndexPageProjection("vault", "Alpha", "Sec", "Page-One", "task",
		[]ProjectedProperty{
			{Property: "status", ValueText: "todo", ValueSort: "todo", ValueType: "select"},
			{Property: "owner", ValueText: "Zed", ValueSort: "Zed", ValueType: "text"},
		},
	); err != nil {
		t.Fatalf("index page one: %v", err)
	}
	if err := dm.IndexPageProjection("vault", "Alpha", "Sec", "Page-Two", "task",
		[]ProjectedProperty{
			{Property: "status", ValueText: "done", ValueSort: "done", ValueType: "select"},
			{Property: "owner", ValueText: "Amy", ValueSort: "Amy", ValueType: "text"},
		},
	); err != nil {
		t.Fatalf("index page two: %v", err)
	}

	pages, err := dm.QueryPagesByType("task")
	if err != nil {
		t.Fatalf("QueryPagesByType: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 pages, got %d", len(pages))
	}
	// Sorted by (notebook, section, page): Page-One before Page-Two.
	if pages[0].Page != "Page-One" || pages[1].Page != "Page-Two" {
		t.Errorf("order = [%s, %s], want [Page-One, Page-Two]", pages[0].Page, pages[1].Page)
	}
	// Properties within each page sorted by name: owner before status.
	for i, p := range pages {
		if !sort.SliceIsSorted(p.Properties, func(a, b int) bool {
			return p.Properties[a].Property < p.Properties[b].Property
		}) {
			t.Errorf("page %d (%s): properties not sorted by name: %+v", i, p.Page, p.Properties)
		}
	}
	if pages[0].Properties[0].Property != "owner" || pages[0].Properties[0].ValueText != "Zed" {
		t.Errorf("page one owner = %+v, want Zed", pages[0].Properties[0])
	}
	if pages[1].Properties[1].Property != "status" || pages[1].Properties[1].ValueText != "done" {
		t.Errorf("page two status = %+v, want done", pages[1].Properties[1])
	}
}

// TestIndexFileBlocks_PreservesProjection is the MB1 regression: block-only
// reindex (task meta / deps / recurrence) must not erase typed projection rows.
// ClearFileBlocks(tx!=nil) clears blocks only; projection is App-owned.
func TestIndexFileBlocks_PreservesProjection(t *testing.T) {
	dm := newTestDB(t)
	blocks := []parser.ParsedBlock{sampleTaskBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)}
	if err := dm.IndexFileBlocks("vault", "Work", "Sprint", "Board", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks seed: %v", err)
	}
	if err := dm.IndexPageProjection("vault", "Work", "Sprint", "Board", "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}},
	); err != nil {
		t.Fatalf("IndexPageProjection: %v", err)
	}
	// Reindex with a different block set (simulates a task-meta edit).
	blocks2 := []parser.ParsedBlock{sampleTaskBlock("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", 1)}
	if err := dm.IndexFileBlocks("vault", "Work", "Sprint", "Board", blocks2, nil); err != nil {
		t.Fatalf("IndexFileBlocks reindex: %v", err)
	}
	row, err := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil || row.TypeName != "task" {
		t.Fatalf("projection must survive block-only reindex, got %+v", row)
	}
	if len(row.Properties) != 1 || row.Properties[0].ValueText != "Alice" {
		t.Errorf("projection properties = %+v", row.Properties)
	}
}

// TestClearFileBlocks_ClearsProjection proves the delete/remove safety net:
// ClearFileBlocks(tx==nil) drops blocks AND projection so DeletePage /
// rename-old / watcher-remove cannot leave a dangling projection behind.
func TestClearFileBlocks_ClearsProjection(t *testing.T) {
	dm := newTestDB(t)

	blocks := []parser.ParsedBlock{sampleTaskBlock("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", 1)}
	if err := dm.IndexFileBlocks("vault", "Work", "Sprint", "Board", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	// Now add the projection row that projectPageType would write.
	if err := dm.IndexPageProjection("vault", "Work", "Sprint", "Board", "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}},
	); err != nil {
		t.Fatalf("IndexPageProjection: %v", err)
	}
	row, _ := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if row == nil {
		t.Fatalf("seed projection missing before clear")
	}

	if err := dm.ClearFileBlocks(nil, "vault", "Work", "Sprint", "Board"); err != nil {
		t.Fatalf("ClearFileBlocks: %v", err)
	}

	row, err := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if err != nil {
		t.Fatalf("GetPageProjection after clear: %v", err)
	}
	if row != nil {
		t.Errorf("projection should be cleared with the blocks, got %+v", row)
	}

	// Blocks are gone too — the delete covered both tables.
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&n); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 blocks after clear, got %d", n)
	}
}

// TestClearFileBlocks_TxNilClearsAllThreeTables pins the atomicity contract
// for the watcher/delete path (tx==nil): the three deletes (blocks,
// page_types, page_properties) are wrapped in a single transaction so a
// mid-failure cannot leave a page with its blocks gone but its projection
// rows lingering as a dashboard ghost. A successful clear leaves zero rows
// of every kind for the page.
func TestClearFileBlocks_TxNilClearsAllThreeTables(t *testing.T) {
	dm := newTestDB(t)

	// Seed one row in each of the three tables for the same page coords.
	blocks := []parser.ParsedBlock{sampleTaskBlock("33333333-3333-3333-3333-333333333333", 1)}
	if err := dm.IndexFileBlocks("vault", "Notes", "Inbox", "PageA", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if err := dm.IndexPageProjection("vault", "Notes", "Inbox", "PageA", "task",
		[]ProjectedProperty{{Property: "owner", ValueText: "Bob", ValueSort: "Bob", ValueType: "text"}},
	); err != nil {
		t.Fatalf("IndexPageProjection: %v", err)
	}
	for _, table := range []string{"blocks", "page_types", "page_properties"} {
		var c int
		if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			"vault", "Notes", "Inbox", "PageA").Scan(&c); err != nil {
			t.Fatalf("seed count %s: %v", table, err)
		}
		if c == 0 {
			t.Fatalf("seed failed: %s has 0 rows", table)
		}
	}

	if err := dm.ClearFileBlocks(nil, "vault", "Notes", "Inbox", "PageA"); err != nil {
		t.Fatalf("ClearFileBlocks: %v", err)
	}

	for _, table := range []string{"blocks", "page_types", "page_properties"} {
		var c int
		if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM "+table+" WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
			"vault", "Notes", "Inbox", "PageA").Scan(&c); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		if c != 0 {
			t.Errorf("expected 0 rows in %s after tx==nil clear, got %d", table, c)
		}
	}
}

// TestGetPageProjection_RealDBErrorPropagates pins the F4 fix: a real DB error
// (anything that is not sql.ErrNoRows) must propagate as a non-nil error, not
// be swallowed as (nil, nil). validateOneRelationTarget relies on this — a
// swallowed transient error reads as proj==nil and rejects a valid relation
// write as "wrong type". We bypass dm.handle()'s ErrDBClosed guard by closing
// the underlying *sql.DB directly, so QueryRow().Scan() itself fails.
func TestGetPageProjection_RealDBErrorPropagates(t *testing.T) {
	dm := newTestDB(t)
	if err := dm.SQLDB().Close(); err != nil {
		t.Fatalf("close underlying sql.DB: %v", err)
	}

	row, err := dm.GetPageProjection("vault", "Work", "Sprint", "Board")
	if err == nil {
		t.Fatal("GetPageProjection: expected non-nil error after DB failure, got nil")
	}
	if row != nil {
		t.Errorf("expected nil row on error, got %+v", row)
	}
}

// projectionSnapshot captures the raw working-memory rows for a type so a
// reopen-and-rebuild can be compared against it deterministically. This is the
// cardinal-rule-4 evidence: the index is reproducible from frontmatter + the
// type schema, so deleting the file and re-deriving yields the same projection.
type projectionSnapshot struct {
	typeRow  string
	propRows [][4]string // (property, value_text, value_sort, value_type)
}

func snapshotProjection(t *testing.T, dm *DatabaseManager, typeID string) projectionSnapshot {
	t.Helper()
	var snap projectionSnapshot
	if err := dm.SQLDB().QueryRow(
		"SELECT type_name FROM page_types WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		"vault", "Work", "Journal", "Daily",
	).Scan(&snap.typeRow); err != nil {
		t.Fatalf("snapshot type row: %v", err)
	}
	rows, err := dm.SQLDB().Query(
		"SELECT property, value_text, value_sort, value_type FROM page_properties WHERE type_name = ? ORDER BY property",
		typeID,
	)
	if err != nil {
		t.Fatalf("snapshot property rows: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r [4]string
		if err := rows.Scan(&r[0], &r[1], &r[2], &r[3]); err != nil {
			t.Fatalf("scan: %v", err)
		}
		snap.propRows = append(snap.propRows, r)
	}
	return snap
}

// TestProjection_ReproducibleAcrossReopen is the cardinal-rule-4 evidence for
// the typed-notes projection: delete the index file and re-derive the
// projection from the (simulated) frontmatter, and the page_types +
// page_properties rows are byte-identical. This is what makes the projection
// disposable — relaunch rebuilds it exactly.
func TestProjection_ReproducibleAcrossReopen(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "index.sqlite")

	indexProjection := func(dm *DatabaseManager) {
		if err := dm.IndexPageProjection("vault", "Work", "Journal", "Daily", "task",
			[]ProjectedProperty{
				{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"},
				{Property: "priority", ValueText: "3", ValueSort: "00000000000003.000000", ValueType: "number"},
				{Property: "done", ValueText: "false", ValueSort: "0", ValueType: "checkbox"},
			},
		); err != nil {
			t.Fatalf("IndexPageProjection: %v", err)
		}
	}

	dm1, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	indexProjection(dm1)
	snap := snapshotProjection(t, dm1, "task")
	if err := dm1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Delete every sidecar the WAL connection may have created so the reopen
	// is a true cold rebuild, not a warm restart.
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		if err := os.Remove(dbPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove %s: %v", dbPath+suffix, err)
		}
	}

	// Reopen at the SAME path — initSchema recreates the (empty) tables — and
	// re-derive the projection exactly as a launch-time frontmatter scan would.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer dm2.Close()
	indexProjection(dm2)
	rebuilt := snapshotProjection(t, dm2, "task")

	if rebuilt.typeRow != snap.typeRow {
		t.Errorf("type_name drift: got %q, want %q", rebuilt.typeRow, snap.typeRow)
	}
	if len(rebuilt.propRows) != len(snap.propRows) {
		t.Fatalf("property row count drift: got %d, want %d", len(rebuilt.propRows), len(snap.propRows))
	}
	for i, want := range snap.propRows {
		if rebuilt.propRows[i] != want {
			t.Errorf("property row %d drift: got %+v, want %+v", i, rebuilt.propRows[i], want)
		}
	}
}
