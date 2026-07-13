package db

import (
	"strings"
	"testing"

	"silt/backend/parser"
)

func TestResolvePageLinkAgainst_ExactBasenameAndAmbiguous(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
		{Source: "vault", Notebook: "Personal", Section: "", Page: "Inbox"},
		{Source: "vault", Notebook: "Archive", Section: "Old", Page: "Site"},
	}

	// Unique basename
	got := ResolvePageLinkAgainst("Inbox", pages)
	if !got.Exists || got.Page != "Inbox" || got.Notebook != "Personal" {
		t.Fatalf("Inbox: %+v", got)
	}
	if got.Shortest != "Inbox" {
		t.Errorf("expected shortest Inbox, got %q", got.Shortest)
	}

	// Ambiguous basename
	amb := ResolvePageLinkAgainst("Site", pages)
	if amb.Exists || !amb.Ambiguous || len(amb.Candidates) != 2 {
		t.Fatalf("Site ambiguous: %+v", amb)
	}

	// Section/page disambiguates
	sec := ResolvePageLinkAgainst("Projects/Site", pages)
	if !sec.Exists || sec.Notebook != "Work" || sec.Section != "Projects" {
		t.Fatalf("Projects/Site: %+v", sec)
	}

	// Full path
	full := ResolvePageLinkAgainst("Work/Projects/Site", pages)
	if !full.Exists || full.Page != "Site" {
		t.Fatalf("full path: %+v", full)
	}

	// Unresolved
	miss := ResolvePageLinkAgainst("NoSuchPage", pages)
	if miss.Exists || miss.Ambiguous {
		t.Fatalf("missing should soft-fail: %+v", miss)
	}
}

func TestShortestUniquePath(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
		{Source: "vault", Notebook: "Archive", Section: "Old", Page: "Site"},
		{Source: "vault", Notebook: "Personal", Section: "", Page: "Inbox"},
	}
	// Site is ambiguous as basename → needs section path
	s := ShortestUniquePath(pages[0], pages)
	if s != "Projects/Site" {
		t.Errorf("expected Projects/Site, got %q", s)
	}
	// Inbox is unique
	if ShortestUniquePath(pages[2], pages) != "Inbox" {
		t.Errorf("expected Inbox, got %q", ShortestUniquePath(pages[2], pages))
	}
}

func TestMapTargetRawAndRewrite(t *testing.T) {
	if got := MapTargetRaw("Site", "Work", "Projects", "Site", "Work", "Projects", "Website"); got != "Website" {
		t.Errorf("basename map: %q", got)
	}
	if got := MapTargetRaw("Projects/Site", "Work", "Projects", "Site", "Work", "Projects", "Website"); got != "Projects/Website" {
		t.Errorf("section map: %q", got)
	}
	if got := MapTargetRaw("Work/Projects/Site", "Work", "Projects", "Site", "Work", "Projects", "Website"); got != "Work/Projects/Website" {
		t.Errorf("full map: %q", got)
	}

	in := "See [[Site]] and [[Site#Goals|Goals]] and ((uuid))."
	out, n := RewritePageLinksInContent(in, "Site", "Website")
	if n != 2 {
		t.Fatalf("expected 2 rewrites, got %d: %s", n, out)
	}
	if out != "See [[Website]] and [[Website#Goals|Goals]] and ((uuid))." {
		t.Errorf("rewrite content: %s", out)
	}
	// Unrelated link unchanged
	out2, n2 := RewritePageLinksInContent("[[Other]]", "Site", "Website")
	if n2 != 0 || out2 != "[[Other]]" {
		t.Errorf("unrelated: n=%d out=%s", n2, out2)
	}
	_ = parser.PageLinkRegex // ensure package link
}

func TestRewritePageLinksInContent_SkipsCodeFences(t *testing.T) {
	in := "See [[Daily]]\n```go\n// [[Daily]] in code\n```\nMore [[Daily]]\n"
	out, n := RewritePageLinksInContent(in, "Daily", "Journal")
	if n != 2 {
		t.Fatalf("expected 2 rewrites (outside fence), got %d: %s", n, out)
	}
	if strings.Contains(out, "```go\n// [[Journal]]") {
		t.Errorf("code fence link must NOT be rewritten:\n%s", out)
	}
	if !strings.Contains(out, "See [[Journal]]") || !strings.Contains(out, "More [[Journal]]") {
		t.Errorf("prose links should be rewritten:\n%s", out)
	}
}

func TestRewritePageLinksInContent_CaseInsensitive(t *testing.T) {
	in := "See [[daily]] and [[DAILY]]"
	out, n := RewritePageLinksInContent(in, "Daily", "Journal")
	if n != 2 {
		t.Fatalf("expected 2 case-insensitive rewrites, got %d: %s", n, out)
	}
	if !strings.Contains(out, "[[Journal]]") {
		t.Errorf("expected rewritten:\n%s", out)
	}
}

func TestResolvePageLinkAgainst_AmbiguousBasenameNotResolvedToExists(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Journal", Page: "Daily"},
		{Source: "vault", Notebook: "Archive", Section: "Old", Page: "Daily"},
	}
	// Ambiguous → Exists=false, Ambiguous=true
	ref := ResolvePageLinkAgainst("Daily", pages)
	if ref.Exists || !ref.Ambiguous {
		t.Fatalf("expected ambiguous, got %+v", ref)
	}
}

func TestMapTargetRaw_CaseInsensitive(t *testing.T) {
	// Case-insensitive match: [[daily]] matches page "Daily", mapped to the
	// canonical new page name (resolution is case-insensitive regardless).
	if got := MapTargetRaw("daily", "Work", "Journal", "Daily", "Work", "Journal", "Diary"); got != "Diary" {
		t.Errorf("case-insensitive basename: %q", got)
	}
}

func TestResolvePageLink_DB(t *testing.T) {
	dm := newTestDB(t)
	blocksA := []parser.ParsedBlock{
		sampleNoteBlock("aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa", 1),
	}
	blocksA[0].CleanText = "note on site"
	if err := dm.IndexFileBlocks("vault", "Work", "Projects", "Site", blocksA, nil); err != nil {
		t.Fatal(err)
	}
	blocksB := []parser.ParsedBlock{
		sampleNoteBlock("bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 1),
	}
	if err := dm.IndexFileBlocks("vault", "Personal", "", "Inbox", blocksB, nil); err != nil {
		t.Fatal(err)
	}

	ref, err := dm.ResolvePageLink("Inbox")
	if err != nil {
		t.Fatal(err)
	}
	if !ref.Exists || ref.Page != "Inbox" {
		t.Fatalf("%+v", ref)
	}

	ref2, err := dm.ResolvePageLink("Projects/Site")
	if err != nil {
		t.Fatal(err)
	}
	if !ref2.Exists || ref2.Notebook != "Work" {
		t.Fatalf("%+v", ref2)
	}
}
