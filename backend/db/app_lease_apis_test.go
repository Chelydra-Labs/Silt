package db

import (
	"errors"
	"testing"

	"silt/backend/parser"
)

func TestCountBlocksGroupedByPage_HappyPath(t *testing.T) {
	dm := newTestDB(t)
	id1 := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	id2 := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	if err := dm.IndexFileBlocks("vault", "NB", "S", "P1", []parser.ParsedBlock{
		sampleTaskBlock(id1, 1),
		sampleNoteBlock(id2, 2),
	}, nil); err != nil {
		t.Fatal(err)
	}
	id3 := "dddddddd-dddd-dddd-dddd-dddddddddddd"
	if err := dm.IndexFileBlocks("linked:x", "Ext", "", "Root", []parser.ParsedBlock{
		sampleTaskBlock(id3, 1),
	}, nil); err != nil {
		t.Fatal(err)
	}

	rows, err := dm.CountBlocksGroupedByPage()
	if err != nil {
		t.Fatalf("CountBlocksGroupedByPage: %v", err)
	}
	got := map[string]int{}
	for _, r := range rows {
		key := r.Source + "|" + r.Notebook + "|" + r.Section + "|" + r.Page
		got[key] = r.Count
	}
	if got["vault|NB|S|P1"] != 2 {
		t.Fatalf("vault page count: got %v want 2 (rows=%v)", got["vault|NB|S|P1"], rows)
	}
	if got["linked:x|Ext||Root"] != 1 {
		t.Fatalf("linked page count: got %v want 1 (rows=%v)", got["linked:x|Ext||Root"], rows)
	}
}

func TestCountBlocksGroupedByPage_PostClose(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := dm.CountBlocksGroupedByPage(); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("want ErrDBClosed, got %v", err)
	}
}

func TestGetBlockReference_HappyAndMissing(t *testing.T) {
	dm := newTestDB(t)
	id := "eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee"
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", []parser.ParsedBlock{
		sampleTaskBlock(id, 3),
	}, nil); err != nil {
		t.Fatal(err)
	}

	ref, err := dm.GetBlockReference(id)
	if err != nil {
		t.Fatalf("GetBlockReference: %v", err)
	}
	if !ref.Exists {
		t.Fatal("expected Exists=true")
	}
	if ref.Type != string(parser.BlockTask) && ref.Type != "TASK" {
		// BlockType stored as TASK string
		if ref.Type != "TASK" {
			t.Fatalf("type: got %q", ref.Type)
		}
	}
	if ref.Notebook != "Work" || ref.Section != "Journal" || ref.Page != "Daily" {
		t.Fatalf("location: %+v", ref)
	}
	if ref.CleanText != "sample task" {
		t.Fatalf("clean: %q", ref.CleanText)
	}
	if ref.LineNumber != 3 {
		t.Fatalf("line: %d", ref.LineNumber)
	}

	missing, err := dm.GetBlockReference("ffffffff-ffff-ffff-ffff-ffffffffffff")
	if err != nil {
		t.Fatalf("missing should not error: %v", err)
	}
	if missing.Exists {
		t.Fatal("missing should have Exists=false")
	}
	if missing.ID != "ffffffff-ffff-ffff-ffff-ffffffffffff" {
		t.Fatalf("id preserved: %q", missing.ID)
	}
}

func TestGetBlockReference_PostClose(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := dm.GetBlockReference("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("want ErrDBClosed, got %v", err)
	}
}

func TestMarkFilesIndexed_BatchAndEmpty(t *testing.T) {
	dm := newTestDB(t)

	if err := dm.MarkFilesIndexed(nil); err != nil {
		t.Fatalf("empty nil: %v", err)
	}
	if err := dm.MarkFilesIndexed([]FileIndexStat{}); err != nil {
		t.Fatalf("empty slice: %v", err)
	}

	files := []FileIndexStat{
		{Path: "/vault/a.md", MTime: 100, Size: 10},
		{Path: "/vault/b.md", MTime: 200, Size: 20},
		{Path: "", MTime: 1, Size: 1}, // skipped
	}
	if err := dm.MarkFilesIndexed(files); err != nil {
		t.Fatalf("MarkFilesIndexed: %v", err)
	}

	unchanged, err := dm.IsFileUnchanged("/vault/a.md", 100, 10)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("a.md should be unchanged")
	}
	unchanged, err = dm.IsFileUnchanged("/vault/b.md", 200, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !unchanged {
		t.Fatal("b.md should be unchanged")
	}
	// Different mtime → changed
	unchanged, err = dm.IsFileUnchanged("/vault/a.md", 999, 10)
	if err != nil {
		t.Fatal(err)
	}
	if unchanged {
		t.Fatal("a.md should be changed with new mtime")
	}
}

func TestMarkFilesIndexed_PostClose(t *testing.T) {
	dm, err := NewDatabaseManager("")
	if err != nil {
		t.Fatal(err)
	}
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}
	if err := dm.MarkFilesIndexed([]FileIndexStat{{Path: "/x.md", MTime: 1, Size: 1}}); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("want ErrDBClosed, got %v", err)
	}
}
