package db

import (
	"database/sql"
	"testing"
)

// TestPageFold_BackfillFillsNullAndEnablesLeafLookup mirrors a warm vault
// upgraded with NULL page_fold rows before the one-shot migration (#844).
func TestPageFold_BackfillFillsNullAndEnablesLeafLookup(t *testing.T) {
	dm := newTestDB(t)
	_, err := dm.SQLDB().Exec(
		`INSERT INTO blocks (id, parent_id, source, notebook, section, page, file_date, depth, type, raw_content, clean_content, line_number)
		 VALUES ('fold-bf-1', NULL, 'vault', 'NB', 'Sec', 'Café', '2026-07-20', 0, 'NOTE', 'body', 'body', 1)`,
	)
	if err != nil {
		t.Fatalf("seed block: %v", err)
	}
	if _, err := dm.SQLDB().Exec("UPDATE blocks SET page_fold = NULL"); err != nil {
		t.Fatalf("null page_fold: %v", err)
	}
	if _, err := dm.SQLDB().Exec("DELETE FROM schema_migrations WHERE name = ?", pageFoldBackfillMarker); err != nil {
		t.Fatalf("clear marker: %v", err)
	}
	if err := backfillPageFold(dm.SQLDB()); err != nil {
		t.Fatalf("backfillPageFold: %v", err)
	}
	if !migrationMarkerApplied(t, dm, pageFoldBackfillMarker) {
		t.Fatal("expected page_fold_backfill marker")
	}
	var fold sql.NullString
	if err := dm.SQLDB().QueryRow(`SELECT page_fold FROM blocks WHERE id = 'fold-bf-1'`).Scan(&fold); err != nil {
		t.Fatalf("read fold: %v", err)
	}
	want := pageFoldKey("Café")
	if !fold.Valid || fold.String != want {
		t.Fatalf("page_fold = %v, want %q", fold, want)
	}
	pages, err := dm.ListPagesByLeaf("CAFÉ")
	if err != nil {
		t.Fatalf("ListPagesByLeaf: %v", err)
	}
	if len(pages) != 1 || pages[0].Page != "Café" {
		t.Fatalf("leaf after backfill: %+v", pages)
	}
	if err := backfillPageFold(dm.SQLDB()); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
}
