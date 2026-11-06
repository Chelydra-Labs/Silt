package db

import (
	"testing"

	"silt/backend/parser"
)

// captureDuplicateIDs installs the duplicate-id warning handler and returns
// the slice the warnings are appended to.
func captureDuplicateIDs(t *testing.T, dm *DatabaseManager) *[]DuplicateIDWarning {
	t.Helper()
	var got []DuplicateIDWarning
	dm.SetDuplicateIDWarningHandler(func(w DuplicateIDWarning) {
		got = append(got, w)
	})
	return &got
}

// TestIndexFileBlocks_WithinFileDuplicateWarnsAndSkips pins the
// within-document half of duplicate-id handling: a hand-edited (or
// plugin-raced) file whose parsed block list carries the same id twice must
// NOT abort the whole page index on the blocks.id primary key. The second
// occurrence is skipped (first wins), the rest of the file still indexes, and
// the collision is surfaced as a structured warning instead of a swallowed
// error.
func TestIndexFileBlocks_WithinFileDuplicateWarnsAndSkips(t *testing.T) {
	dm := newTestDB(t)
	got := captureDuplicateIDs(t, dm)

	const dup = "11111111-1111-1111-1111-111111111111"
	blocks := []parser.ParsedBlock{
		sampleNoteBlock(dup, 1),
		sampleNoteBlock("22222222-2222-2222-2222-222222222222", 2),
		sampleNoteBlock(dup, 3),
	}

	if err := dm.IndexFileBlocks("vault", "Work", "Sprint", "Board", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks with a within-file duplicate id: %v", err)
	}

	if len(*got) != 1 {
		t.Fatalf("duplicate-id warnings = %d, want 1: %+v", len(*got), *got)
	}
	w := (*got)[0]
	if w.BlockID != dup {
		t.Errorf("warning BlockID = %q, want %q", w.BlockID, dup)
	}
	if !w.WithinPage {
		t.Errorf("warning WithinPage = false, want true (both occurrences are in one page)")
	}
	if len(w.Pages) != 1 || w.Pages[0].Notebook != "Work" || w.Pages[0].Section != "Sprint" || w.Pages[0].Page != "Board" {
		t.Errorf("warning Pages = %+v, want one Work/Sprint/Board entry", w.Pages)
	}

	// The non-colliding block AND the id (once) must be indexed — the file
	// does not silently drop out of the index over one bad line.
	var pageBlocks int
	if err := dm.SQLDB().QueryRow(
		"SELECT COUNT(*) FROM blocks WHERE source = ? AND notebook = ? AND section = ? AND page = ?",
		"vault", "Work", "Sprint", "Board",
	).Scan(&pageBlocks); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if pageBlocks != 2 {
		t.Errorf("indexed blocks = %d, want 2 (the id once, plus the non-colliding block)", pageBlocks)
	}
}

// TestIndexFileBlocks_DuplicateSubtreeDoesNotInflateTaskRollups pins that a
// hand-pasted task subtree (same ids twice in one page) writes derived caches
// for the surviving occurrence only: the duplicate's descendants are skipped
// by the insert loop, so comments_count / subtask_total / subtask_done must
// not aggregate both occurrences' children into one row.
func TestIndexFileBlocks_DuplicateSubtreeDoesNotInflateTaskRollups(t *testing.T) {
	dm := newTestDB(t)

	const (
		taskID = "11111111-1111-1111-1111-111111111111"
		subID  = "22222222-2222-2222-2222-222222222222"
		noteID = "33333333-3333-3333-3333-333333333333"
	)
	task := sampleTaskBlock(taskID, 1)
	sub := sampleTaskBlock(subID, 2)
	sub.ParentID = taskID
	sub.Status = "DONE"
	comment := sampleNoteBlock(noteID, 3)
	comment.ParentID = taskID
	blocks := []parser.ParsedBlock{task, sub, comment, task, sub, comment}

	if err := dm.IndexFileBlocks("vault", "Work", "Sprint", "Board", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks with a duplicated task subtree: %v", err)
	}

	var total int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&total); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if total != 3 {
		t.Errorf("indexed blocks = %d, want 3 (each id once, first occurrence wins)", total)
	}

	var comments, subtaskTotal, subtaskDone int
	if err := dm.SQLDB().QueryRow(
		"SELECT comments_count, subtask_total, subtask_done FROM tasks WHERE block_id = ?", taskID,
	).Scan(&comments, &subtaskTotal, &subtaskDone); err != nil {
		t.Fatalf("query surviving task row: %v", err)
	}
	if comments != 1 {
		t.Errorf("comments_count = %d, want 1 (only the surviving note child counts)", comments)
	}
	if subtaskTotal != 1 {
		t.Errorf("subtask_total = %d, want 1 (only the surviving subtask child counts)", subtaskTotal)
	}
	if subtaskDone != 1 {
		t.Errorf("subtask_done = %d, want 1 (the one surviving subtask is DONE)", subtaskDone)
	}
}

// TestScanDuplicateIDs_ReportsCrossFileDuplicates pins the cross-file half:
// when a scan parses two pages carrying the same block id, the grouping pass
// reports one structured warning naming BOTH page paths. This is the exact
// detection point — the scan has both parses in hand, so there is no
// move-vs-duplicate ambiguity.
func TestScanDuplicateIDs_ReportsCrossFileDuplicates(t *testing.T) {
	const shared = "11111111-1111-1111-1111-111111111111"
	results := []parser.ScanResult{
		{
			Notebook: "Work", Section: "Sprint", Page: "Board",
			Blocks: []parser.ParsedBlock{
				sampleNoteBlock(shared, 1),
				sampleNoteBlock("22222222-2222-2222-2222-222222222222", 2),
			},
		},
		{
			Notebook: "Work", Section: "Projects", Page: "Roadmap",
			Blocks: []parser.ParsedBlock{
				sampleNoteBlock(shared, 1),
				sampleNoteBlock("33333333-3333-3333-3333-333333333333", 2),
			},
		},
	}

	warnings := ScanDuplicateIDs(results)
	if len(warnings) != 1 {
		t.Fatalf("warnings = %d, want 1: %+v", len(warnings), warnings)
	}
	w := warnings[0]
	if w.BlockID != shared {
		t.Errorf("BlockID = %q, want %q", w.BlockID, shared)
	}
	if w.WithinPage {
		t.Errorf("WithinPage = true, want false (two distinct pages)")
	}
	if len(w.Pages) != 2 {
		t.Fatalf("Pages = %+v, want both carrier pages", w.Pages)
	}
	if w.Pages[0].Notebook != "Work" || w.Pages[0].Section != "Sprint" || w.Pages[0].Page != "Board" {
		t.Errorf("Pages[0] = %+v, want Work/Sprint/Board", w.Pages[0])
	}
	if w.Pages[1].Section != "Projects" || w.Pages[1].Page != "Roadmap" {
		t.Errorf("Pages[1] = %+v, want Work/Projects/Roadmap", w.Pages[1])
	}
}

// TestScanDuplicateIDs_CleanVaultNoWarnings pins the noise floor: distinct
// ids across pages produce no warnings, so a clean vault sees nothing.
func TestScanDuplicateIDs_CleanVaultNoWarnings(t *testing.T) {
	results := []parser.ScanResult{
		{
			Notebook: "Work", Section: "Sprint", Page: "Board",
			Blocks: []parser.ParsedBlock{
				sampleNoteBlock("11111111-1111-1111-1111-111111111111", 1),
			},
		},
		{
			Notebook: "Work", Section: "Sprint", Page: "Retro",
			Blocks: []parser.ParsedBlock{
				sampleNoteBlock("22222222-2222-2222-2222-222222222222", 1),
			},
		},
	}
	if warnings := ScanDuplicateIDs(results); len(warnings) != 0 {
		t.Errorf("clean vault warnings = %+v, want none", warnings)
	}
}

// TestScanDuplicateIDs_SkipsUnresolvableResults: results that failed to read
// or did not resolve to a page (no notebook) have no trustworthy coordinates
// and must not participate in the grouping.
func TestScanDuplicateIDs_SkipsUnresolvableResults(t *testing.T) {
	const shared = "11111111-1111-1111-1111-111111111111"
	results := []parser.ScanResult{
		{
			Notebook: "Work", Section: "Sprint", Page: "Board",
			Blocks: []parser.ParsedBlock{sampleNoteBlock(shared, 1)},
		},
		{Notebook: "", Blocks: []parser.ParsedBlock{sampleNoteBlock(shared, 1)}},
	}
	if warnings := ScanDuplicateIDs(results); len(warnings) != 0 {
		t.Errorf("warnings = %+v, want none (second result has no resolvable page)", warnings)
	}
}

// TestIndexPaths_CleanVaultNoWarnings pins the noise floor for the
// transaction-internal detector: indexing clean files through BOTH paths
// (single-file and batched scan) dispatches nothing. The within-file check
// is error-path-only (the constraint firing) and the cross-file check is a
// scan-side pure pass — no clean-vault query or per-block probe exists on
// the hot path.
func TestIndexPaths_CleanVaultNoWarnings(t *testing.T) {
	dm := newTestDB(t)
	got := captureDuplicateIDs(t, dm)

	pageA := []parser.ScanResult{{
		Notebook: "Work", Section: "Sprint", Page: "Board",
		Blocks: []parser.ParsedBlock{
			sampleTaskBlock("11111111-1111-1111-1111-111111111111", 1),
			sampleNoteBlock("22222222-2222-2222-2222-222222222222", 2),
		},
	}}
	pageB := []parser.ScanResult{{
		Notebook: "Work", Section: "Projects", Page: "Roadmap",
		Blocks: []parser.ParsedBlock{
			sampleNoteBlock("33333333-3333-3333-3333-333333333333", 1),
		},
	}}

	if _, _, err := dm.IndexScanResults(append(pageA, pageB...)); err != nil {
		t.Fatalf("IndexScanResults: %v", err)
	}
	if err := dm.IndexFileWithProjection("vault", "Work", "Sprint", "Board",
		pageA[0].Blocks, nil, "", nil, PageCoreFields{}); err != nil {
		t.Fatalf("IndexFileWithProjection: %v", err)
	}
	if warnings := ScanDuplicateIDs(append(pageA, pageB...)); len(warnings) != 0 {
		t.Errorf("scan grouping warnings = %+v, want none", warnings)
	}
	if len(*got) != 0 {
		t.Errorf("dispatched warnings = %+v, want none on a clean vault", *got)
	}
}

// TestIndexScanResults_DuplicateIDStillIndexesBothFiles pins that a
// cross-file duplicate in the batched scan path does not abort the batch:
// both files' non-colliding blocks index (the id itself lands on whichever
// page the batch processes last) and the collision is warned via the handler.
func TestIndexScanResults_DuplicateIDStillIndexesBothFiles(t *testing.T) {
	dm := newTestDB(t)
	got := captureDuplicateIDs(t, dm)

	const shared = "11111111-1111-1111-1111-111111111111"
	results := []parser.ScanResult{
		{
			Notebook: "Work", Section: "Sprint", Page: "Board",
			Blocks: []parser.ParsedBlock{
				sampleNoteBlock(shared, 1),
				sampleNoteBlock("22222222-2222-2222-2222-222222222222", 2),
			},
		},
		{
			Notebook: "Work", Section: "Projects", Page: "Roadmap",
			Blocks: []parser.ParsedBlock{
				sampleNoteBlock(shared, 1),
				sampleNoteBlock("33333333-3333-3333-3333-333333333333", 2),
			},
		},
	}

	indexed, _, err := dm.IndexScanResults(results)
	if err != nil {
		t.Fatalf("IndexScanResults with a cross-file duplicate id: %v", err)
	}
	if indexed != 2 {
		t.Errorf("indexed files = %d, want 2", indexed)
	}
	// The tx-internal detector only sees within-file duplicates; the
	// cross-file report comes from the app-layer grouping over the scan's
	// parses, so the handler must stay quiet here.
	if len(*got) != 0 {
		t.Errorf("handler warnings = %+v, want none (cross-file detection is scan-side)", *got)
	}
	var total int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&total); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	// 3 distinct ids across the two pages (the shared one indexes once).
	if total != 3 {
		t.Errorf("indexed blocks = %d, want 3 (shared id indexes once, all distinct ids present)", total)
	}
}
