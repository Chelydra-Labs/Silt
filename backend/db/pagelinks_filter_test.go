package db

import (
	"fmt"
	"testing"

	"silt/backend/parser"
)

func TestLinkTargetRawCandidates_IncludesSuffixes(t *testing.T) {
	got := LinkTargetRawCandidates([]struct{ Notebook, Section, Page string }{
		{"Work", "Projects/Active", "Site"},
	})
	want := []string{
		"Site",
		"Active/Site",
		"Projects/Active/Site",
		"Work/Projects/Active/Site",
	}
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing candidate %q in %v", w, got)
		}
	}
}

func TestListPageLinksByTargetRaws_CaseInsensitive(t *testing.T) {
	dm := newTestDB(t)
	blocks := []parser.ParsedBlock{
		{
			ID:         "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb",
			Type:       parser.BlockNote,
			RawText:    "see [[TargetPage]] and noise [[Other]]",
			CleanText:  "see [[TargetPage]] and noise [[Other]]",
			LineNumber: 1,
		},
	}
	if err := dm.IndexFileBlocks("vault", "NB", "Sec", "Src", blocks, nil); err != nil {
		t.Fatalf("index: %v", err)
	}
	if err := dm.IndexFileBlocks("vault", "NB", "Sec", "TargetPage", []parser.ParsedBlock{{
		ID: "cccccccc-cccc-4ccc-8ccc-cccccccccccc", Type: parser.BlockNote,
		RawText: "x", CleanText: "x", LineNumber: 1,
	}}, nil); err != nil {
		t.Fatalf("index target: %v", err)
	}

	rows, err := dm.ListPageLinksByTargetRaws([]string{"targetpage"})
	if err != nil {
		t.Fatalf("ListPageLinksByTargetRaws: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row for case-insensitive TargetPage, got %d %+v", len(rows), rows)
	}
	if rows[0].TargetRaw != "TargetPage" {
		t.Errorf("TargetRaw=%q", rows[0].TargetRaw)
	}

	rows, err = dm.ListPageLinksByTargetRaws([]string{"NoSuchPage"})
	if err != nil {
		t.Fatalf("empty: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("want 0 rows, got %d", len(rows))
	}
}

// BenchmarkListPageLinksByTargetRaws_10k compares filtered vs full-table load.
// Baseline Windows amd64 2026-07-19 (~10k noise + 50 hits):
//
//	filtered   ~64 µs / 30 KiB
//	full_table ~10.8 ms / 8.5 MiB  (~170× slower)
func BenchmarkListPageLinksByTargetRaws_10k(b *testing.B) {
	dm, err := newBenchDB(b)
	if err != nil {
		b.Fatal(err)
	}
	const noise = 10000
	const hits = 50
	blocks := make([]parser.ParsedBlock, 0, noise+hits)
	for i := 0; i < noise; i++ {
		id := fmt.Sprintf("aaaaaaaa-aaaa-4aaa-8aaa-%012d", i)
		name := fmt.Sprintf("Noise%d", i)
		blocks = append(blocks, parser.ParsedBlock{
			ID: id, Type: parser.BlockNote,
			RawText: "[[" + name + "]]", CleanText: "[[" + name + "]]",
			LineNumber: i + 1,
		})
	}
	for i := 0; i < hits; i++ {
		id := fmt.Sprintf("bbbbbbbb-bbbb-4bbb-8bbb-%012d", i)
		blocks = append(blocks, parser.ParsedBlock{
			ID: id, Type: parser.BlockNote,
			RawText: "[[TargetPage]]", CleanText: "[[TargetPage]]",
			LineNumber: noise + i + 1,
		})
	}
	if err := dm.IndexFileBlocks("vault", "NB", "Sec", "Src", blocks, nil); err != nil {
		b.Fatalf("index: %v", err)
	}

	cands := LinkTargetRawCandidates([]struct{ Notebook, Section, Page string }{
		{"NB", "Sec", "TargetPage"},
	})
	b.Run("filtered", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rows, err := dm.ListPageLinksByTargetRaws(cands)
			if err != nil {
				b.Fatal(err)
			}
			if len(rows) < hits {
				b.Fatalf("want >= %d hits, got %d", hits, len(rows))
			}
		}
	})
	b.Run("full_table", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			rows, err := dm.ListAllPageLinks()
			if err != nil {
				b.Fatal(err)
			}
			if len(rows) < noise {
				b.Fatalf("want large table, got %d", len(rows))
			}
		}
	})
}

func newBenchDB(b *testing.B) (*DatabaseManager, error) {
	b.Helper()
	// Mirror newTestDB: empty path → in-memory SQLite.
	dm, err := NewDatabaseManager("")
	if err != nil {
		return nil, err
	}
	b.Cleanup(func() { _ = dm.Close() })
	return dm, nil
}
