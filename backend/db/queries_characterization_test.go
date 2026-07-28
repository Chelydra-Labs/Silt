package db

import (
	"errors"
	"testing"

	"silt/backend/parser"
)

// Characterization tests for the residual queries.go APIs before the #767
// domain-file split. Locks current behavior so a cut-paste regression fails
// immediately. Functions already covered strongly elsewhere are omitted.

func TestGetBlockLocation_HappyAndMissing(t *testing.T) {
	dm := newTestDB(t)
	id := "11111111-1111-1111-1111-111111111111"
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily",
		[]parser.ParsedBlock{sampleTaskBlock(id, 1)}, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	loc, err := dm.GetBlockLocation(id)
	if err != nil {
		t.Fatalf("GetBlockLocation: %v", err)
	}
	if loc.Source != "vault" || loc.Notebook != "Work" || loc.Section != "Journal" ||
		loc.Page != "Daily" || loc.BlockType != string(parser.BlockTask) {
		t.Errorf("unexpected location: %+v", loc)
	}

	_, err = dm.GetBlockLocation("00000000-0000-0000-0000-000000000000")
	if err == nil {
		t.Fatal("expected error for missing block")
	}
}

func TestFetchPageBlocks_OrderedAndScoped(t *testing.T) {
	dm := newTestDB(t)
	a := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	b := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	other := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	blocks := []parser.ParsedBlock{
		sampleNoteBlock(b, 2),
		sampleTaskBlock(a, 1),
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Other", "Page",
		[]parser.ParsedBlock{sampleNoteBlock(other, 1)}, nil); err != nil {
		t.Fatalf("IndexFileBlocks other: %v", err)
	}

	got, err := dm.FetchPageBlocks("vault", "Work", "Journal", "Daily")
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 blocks, got %d", len(got))
	}
	if got[0].ID != a || got[1].ID != b {
		t.Errorf("order by line_number: got %s then %s, want %s then %s",
			got[0].ID, got[1].ID, a, b)
	}
	if got[0].Status != "TODO" || got[0].Owner != "Alice" {
		t.Errorf("task fields not hydrated: status=%q owner=%q", got[0].Status, got[0].Owner)
	}

	// Empty source defaults to vault.
	got2, err := dm.FetchPageBlocks("", "Work", "Journal", "Daily")
	if err != nil {
		t.Fatalf("FetchPageBlocks empty source: %v", err)
	}
	if len(got2) != 2 {
		t.Errorf("empty source should default to vault: got %d blocks", len(got2))
	}
}

func TestQueryBlocksByTag_PrefixSemantics(t *testing.T) {
	dm := newTestDB(t)
	blocks := []parser.ParsedBlock{
		{
			ID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", Type: parser.BlockTask,
			RawText:   "- [ ] leaf #work/project/milestone-one <!-- id: aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa -->",
			CleanText: "leaf", Status: "TODO", LineNumber: 1,
		},
		{
			ID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", Type: parser.BlockTask,
			RawText:   "- [ ] mid #work/project <!-- id: bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb -->",
			CleanText: "mid", Status: "TODO", LineNumber: 2,
		},
		{
			ID: "cccccccc-cccc-cccc-cccc-cccccccccccc", Type: parser.BlockTask,
			RawText:   "- [ ] root #work <!-- id: cccccccc-cccc-cccc-cccc-cccccccccccc -->",
			CleanText: "root", Status: "TODO", LineNumber: 3,
		},
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	res, err := dm.QueryBlocksByTag("work")
	if err != nil {
		t.Fatalf("QueryBlocksByTag work: %v", err)
	}
	if len(res) != 3 {
		t.Errorf("expected #work to match all 3 (prefix), got %d", len(res))
	}

	res2, err := dm.QueryBlocksByTag("work/project/milestone-one")
	if err != nil {
		t.Fatalf("QueryBlocksByTag leaf: %v", err)
	}
	if len(res2) != 1 {
		t.Errorf("expected leaf to match 1, got %d", len(res2))
	}

	empty, err := dm.QueryBlocksByTag("")
	if err != nil {
		t.Fatalf("QueryBlocksByTag empty: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("empty tag path should return no rows, got %d", len(empty))
	}
}

func TestOpenBlockers_OpenOnly(t *testing.T) {
	dm := newTestDB(t)
	openDep := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	doneDep := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	subject := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	doneBlock := sampleTaskBlock(doneDep, 2)
	doneBlock.Status = "DONE"
	blocks := []parser.ParsedBlock{
		sampleTaskBlock(openDep, 1),
		doneBlock,
		func() parser.ParsedBlock {
			b := sampleTaskBlock(subject, 3)
			b.BlockedBy = []string{openDep, doneDep}
			return b
		}(),
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	got, err := dm.OpenBlockers(subject)
	if err != nil {
		t.Fatalf("OpenBlockers: %v", err)
	}
	if len(got) != 1 || got[0] != openDep {
		t.Errorf("OpenBlockers = %v, want [%s]", got, openDep)
	}

	none, err := dm.OpenBlockers(openDep)
	if err != nil {
		t.Fatalf("OpenBlockers no deps: %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Errorf("expected non-nil empty slice, got %#v", none)
	}
}

func TestDependentsOf_ReverseEdges(t *testing.T) {
	dm := newTestDB(t)
	dep := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	d1 := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	d2 := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	blocks := []parser.ParsedBlock{
		sampleTaskBlock(dep, 1),
		func() parser.ParsedBlock {
			b := sampleTaskBlock(d1, 2)
			b.BlockedBy = []string{dep}
			return b
		}(),
		func() parser.ParsedBlock {
			b := sampleTaskBlock(d2, 3)
			b.BlockedBy = []string{dep}
			return b
		}(),
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	got, err := dm.DependentsOf(dep)
	if err != nil {
		t.Fatalf("DependentsOf: %v", err)
	}
	if len(got) != 2 || got[0] != d1 || got[1] != d2 {
		t.Errorf("DependentsOf = %v, want [%s %s]", got, d1, d2)
	}
}

func TestDependencyEdges_ScopedAndEmpty(t *testing.T) {
	dm := newTestDB(t)
	a := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	b := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	c := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	blocks := []parser.ParsedBlock{
		sampleTaskBlock(a, 1),
		func() parser.ParsedBlock {
			blk := sampleTaskBlock(b, 2)
			blk.BlockedBy = []string{a}
			return blk
		}(),
		func() parser.ParsedBlock {
			blk := sampleTaskBlock(c, 3)
			blk.BlockedBy = []string{b}
			return blk
		}(),
	}
	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", blocks, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	empty, err := dm.DependencyEdges(nil)
	if err != nil {
		t.Fatalf("DependencyEdges nil: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("empty input: got %#v", empty)
	}

	edges, err := dm.DependencyEdges([]string{b, c})
	if err != nil {
		t.Fatalf("DependencyEdges: %v", err)
	}
	// Edges where either endpoint is in {b,c}: b→a and c→b.
	if len(edges[b]) != 1 || edges[b][0] != a {
		t.Errorf("edges[%s] = %v, want [%s]", b, edges[b], a)
	}
	if len(edges[c]) != 1 || edges[c][0] != b {
		t.Errorf("edges[%s] = %v, want [%s]", c, edges[c], b)
	}
}

func TestValidTaskBlockIDs_FiltersNonTasks(t *testing.T) {
	dm := newTestDB(t)
	taskID := "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"
	noteID := "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"
	missing := "cccccccc-cccc-cccc-cccc-cccccccccccc"

	if err := dm.IndexFileBlocks("vault", "Work", "Journal", "Daily", []parser.ParsedBlock{
		sampleTaskBlock(taskID, 1),
		sampleNoteBlock(noteID, 2),
	}, nil); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}

	got, err := dm.ValidTaskBlockIDs([]string{taskID, noteID, missing})
	if err != nil {
		t.Fatalf("ValidTaskBlockIDs: %v", err)
	}
	if !got[taskID] {
		t.Errorf("expected task %s valid", taskID)
	}
	if got[noteID] {
		t.Errorf("note should not be a valid task id")
	}
	if got[missing] {
		t.Errorf("missing id should not be valid")
	}

	empty, err := dm.ValidTaskBlockIDs(nil)
	if err != nil {
		t.Fatalf("ValidTaskBlockIDs nil: %v", err)
	}
	if empty == nil || len(empty) != 0 {
		t.Errorf("nil ids: got %#v", empty)
	}
}

func TestFetchPageBlocks_PostClose(t *testing.T) {
	dm := newTestDB(t)
	if err := dm.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := dm.FetchPageBlocks("vault", "W", "S", "P"); !errors.Is(err, ErrDBClosed) {
		t.Fatalf("post-close FetchPageBlocks: %v", err)
	}
}
