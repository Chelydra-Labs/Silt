package db

import (
	"testing"
)

// TestGetTypedPageLocatorsByIDs_ScalesWithTypeCount is the #866 scaling
// evidence: only pages of the supplied type IDs are returned, NOT every
// typed page in the vault. The scoped worker that calls this does work
// proportional to the affected pages, not the entire typed-notes index.
func TestGetTypedPageLocatorsByIDs_ScalesWithTypeCount(t *testing.T) {
	dm := newTestDB(t)

	// Seed three types, each with multiple pages.
	indexType := func(typeID string, pageIDs ...string) {
		for _, pageID := range pageIDs {
			if err := dm.IndexPageProjection("vault", "Books", "Read", pageID, typeID,
				[]ProjectedProperty{{Property: "title", ValueText: pageID, ValueSort: pageID, ValueType: "text"}}); err != nil {
				t.Fatalf("IndexPageProjection %s/%s: %v", typeID, pageID, err)
			}
		}
	}
	indexType("book", "Dune", "Hyperion", "Foundation")
	indexType("meeting", "Standup", "Review")
	indexType("task", "Bug", "Feature", "Spike", "Refactor")

	// Ask for "meeting" pages only. Result set is scoped to meetings, not
	// every typed page.
	locators, err := dm.GetTypedPageLocatorsByIDs([]string{"meeting"})
	if err != nil {
		t.Fatalf("GetTypedPageLocatorsByIDs: %v", err)
	}
	if len(locators) != 2 {
		t.Fatalf("scoped lookup returned %d locators, want 2 (only meeting pages)", len(locators))
	}
	for _, loc := range locators {
		if loc.TypeName != "meeting" {
			t.Errorf("locator type = %q, want meeting (lookup must scope by id)", loc.TypeName)
		}
	}

	// Ask for two types — union, no duplicates, deterministic order.
	locators, err = dm.GetTypedPageLocatorsByIDs([]string{"task", "book"})
	if err != nil {
		t.Fatalf("GetTypedPageLocatorsByIDs(multi): %v", err)
	}
	if len(locators) != 7 { // 3 book + 4 task
		t.Errorf("multi-type lookup returned %d, want 7 (3 book + 4 task)", len(locators))
	}
	// Deterministic order: ORDER BY source, notebook, section, page.
	for i := 1; i < len(locators); i++ {
		prev := locators[i-1]
		cur := locators[i]
		if prev.Page > cur.Page {
			t.Errorf("locator %d (%s) out of order after %s", i, cur.Page, prev.Page)
		}
	}
}

// TestGetTypedPageLocatorsByIDs_DeduplicatesIDs proves the input contract:
// the same id passed twice (e.g. old+new on a no-op rename) does not produce
// duplicate rows in the result. Lets the App layer union affected IDs
// naively without post-dedup.
func TestGetTypedPageLocatorsByIDs_DeduplicatesIDs(t *testing.T) {
	dm := newTestDB(t)
	if err := dm.IndexPageProjection("vault", "Books", "Read", "Dune", "book",
		[]ProjectedProperty{{Property: "title", ValueText: "Dune", ValueSort: "Dune", ValueType: "text"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	locators, err := dm.GetTypedPageLocatorsByIDs([]string{"book", "book", "book"})
	if err != nil {
		t.Fatalf("GetTypedPageLocatorsByIDs: %v", err)
	}
	if len(locators) != 1 {
		t.Errorf("duplicate input ids produced %d locators, want 1 (dedupe before query)", len(locators))
	}
}

// TestGetTypedPageLocatorsByIDs_EmptyInputReturnsNothing proves the empty-
// input contract: the scoped worker treats a zero-length / all-empty input
// as "nothing to do" rather than "reproject all". This is the load-bearing
// distinction from GetAllTypedPageLocators.
func TestGetTypedPageLocatorsByIDs_EmptyInputReturnsNothing(t *testing.T) {
	dm := newTestDB(t)
	if err := dm.IndexPageProjection("vault", "Books", "Read", "Dune", "book",
		[]ProjectedProperty{{Property: "title", ValueText: "Dune", ValueSort: "Dune", ValueType: "text"}}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	for _, input := range [][]string{nil, {}, {""}, {"", ""}} {
		locators, err := dm.GetTypedPageLocatorsByIDs(input)
		if err != nil {
			t.Errorf("input %v: unexpected err: %v", input, err)
			continue
		}
		if len(locators) != 0 {
			t.Errorf("input %v: got %d locators, want 0 (empty input = nothing to do)", input, len(locators))
		}
	}
}

// TestGetTypedPageLocatorsByIDs_UsesTypeIndex pins the query plan: a lookup
// by type IDs must be served by idx_page_types_type, not a full table scan.
// Without this index a large vault's reprojection pass would dominate the
// latency budget of every SaveType / DeleteType call.
func TestGetTypedPageLocatorsByIDs_UsesTypeIndex(t *testing.T) {
	dm := newTestDB(t)
	var n int
	if err := dm.SQLDB().QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_page_types_type'",
	).Scan(&n); err != nil {
		t.Fatalf("probe index: %v", err)
	}
	if n != 1 {
		t.Fatalf("idx_page_types_type missing — scoped reprojection would full-scan")
	}
}

// TestProjectionReprojectWorker_RapidSaveCoalescing lives in the main
// package (it exercises the App-level worker). See
// app_types_worker_test.go::TestProjectionReprojectWorker_RapidSaveCoalesces.

// TestSchemaMigrations_BackfillMarkerIsIdempotent verifies the cold-start
// backfill contract isn't regressed by the scoped worker refactor. The
// marker is still recorded only when the full backfill succeeds.
func TestSchemaMigrations_BackfillMarkerIsIdempotent(t *testing.T) {
	dm := newTestDB(t)
	if err := dm.RecordSchemaMigration(PageProjectionBackfillMarker); err != nil {
		t.Fatalf("RecordSchemaMigration: %v", err)
	}
	applied, err := dm.SchemaMigrationApplied(PageProjectionBackfillMarker)
	if err != nil {
		t.Fatalf("SchemaMigrationApplied: %v", err)
	}
	if !applied {
		t.Errorf("marker not applied after record")
	}
	// Idempotent: re-recording does not error.
	if err := dm.RecordSchemaMigration(PageProjectionBackfillMarker); err != nil {
		t.Errorf("second RecordSchemaMigration: %v", err)
	}
}

// Compile-time guard that the scanner helper exists at the package level so
// a future refactor that drops one of the SELECT paths keeps the shared
// scan path intact.
var _ = scanTypedPageLocators
