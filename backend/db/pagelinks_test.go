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

// --- Source-qualified link resolution ---

func TestParseQualifiedTarget_Unqualified(t *testing.T) {
	src, path, q := ParseQualifiedTarget("Work/Projects/Site")
	if q || src != "" || path != "Work/Projects/Site" {
		t.Fatalf("unqualified: src=%q path=%q q=%v", src, path, q)
	}
}

func TestParseQualifiedTarget_LinkedQualified(t *testing.T) {
	src, path, q := ParseQualifiedTarget("linked:abc/Work/Projects/Site")
	if !q || src != "linked:abc" || path != "Work/Projects/Site" {
		t.Fatalf("linked qualified: src=%q path=%q q=%v", src, path, q)
	}
}

func TestParseQualifiedTarget_VaultPrefixIsUnqualified(t *testing.T) {
	// "vault/Work" is NOT source-qualified — "vault" is a valid legacy
	// notebook path prefix. Only "linked:" is a source qualifier.
	src, path, q := ParseQualifiedTarget("vault/Work/Site")
	if q || src != "" || path != "vault/Work/Site" {
		t.Fatalf("vault prefix should be unqualified: src=%q path=%q q=%v", src, path, q)
	}
}

func TestParseQualifiedTarget_Empty(t *testing.T) {
	_, _, q := ParseQualifiedTarget("")
	if q {
		t.Fatal("empty target should not be qualified")
	}
}

func TestQualifiedPath_Vault(t *testing.T) {
	p := QualifiedPath(PageLoc{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"})
	if p != "Work/Projects/Site" {
		t.Errorf("vault path should be unqualified: got %q", p)
	}
}

func TestQualifiedPath_Linked(t *testing.T) {
	p := QualifiedPath(PageLoc{Source: "linked:abc", Notebook: "Work", Section: "Projects", Page: "Site"})
	if p != "linked:abc/Work/Projects/Site" {
		t.Errorf("linked qualified path: %q", p)
	}
}

func TestResolvePageLinkAgainst_SourceQualifiedScoped(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
		{Source: "linked:ext", Notebook: "Work", Section: "Projects", Page: "Site"},
	}

	// Unqualified "Site" is ambiguous across sources.
	ref := ResolvePageLinkAgainst("Site", pages)
	if ref.Exists {
		t.Fatalf("unqualified Site should be ambiguous across sources: %+v", ref)
	}

	// Source-qualified resolves to the correct source.
	ref2 := ResolvePageLinkAgainst("linked:ext/Work/Projects/Site", pages)
	if !ref2.Exists || ref2.Source != "linked:ext" {
		t.Fatalf("qualified linked:ext: %+v", ref2)
	}

	// "vault/Work/Projects/Site" is NOT source-qualified — treated as a
	// normal unqualified path. Under the unique notebook-name invariant,
	// "vault" would be a notebook name, which doesn't exist here.
	ref3 := ResolvePageLinkAgainst("vault/Work/Projects/Site", pages)
	if ref3.Exists {
		t.Fatalf("vault/... should not resolve as a source qualifier: %+v", ref3)
	}

	// Unqualified full path is still ambiguous (both sources match).
	ref4 := ResolvePageLinkAgainst("Work/Projects/Site", pages)
	if !ref4.Ambiguous {
		t.Fatalf("same-path collision makes unqualified form ambiguous: %+v", ref4)
	}
}

func TestResolvePageLinkAgainst_SourceQualifiedBasename(t *testing.T) {
	// "linked:ext/Site" should resolve the linked Site page even though
	// basename "Site" alone is ambiguous.
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
		{Source: "linked:ext", Notebook: "Work", Section: "Log", Page: "Site"},
	}

	ref := ResolvePageLinkAgainst("linked:ext/Site", pages)
	if !ref.Exists || ref.Source != "linked:ext" || ref.Section != "Log" {
		t.Fatalf("qualified basename: %+v", ref)
	}
}

func TestResolvePageLinkAgainst_SourceQualifiedNoSuch(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
	}
	ref := ResolvePageLinkAgainst("linked:nope/Site", pages)
	if ref.Exists {
		t.Fatalf("nonexistent source should not match: %+v", ref)
	}
}

func TestShortestUniquePath_SourceCollision(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
		{Source: "linked:ext", Notebook: "Work", Section: "Projects", Page: "Site"},
	}

	// Vault page: even though basename collides with linked, vault never gets
	// a "vault/" prefix. Under the unique notebook-name invariant this
	// collision is unreachable, but the fallback is the unqualified full path.
	s := ShortestUniquePath(pages[0], pages)
	if s != "Work/Projects/Site" {
		t.Errorf("vault Site should be unqualified full path: got %q", s)
	}
	// Linked page: gets source-qualified form since all unqualified collide.
	s2 := ShortestUniquePath(pages[1], pages)
	if s2 != "linked:ext/Work/Projects/Site" {
		t.Errorf("linked Site should qualify: got %q", s2)
	}
}

func TestShortestUniquePath_NoSourceCollision(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
		{Source: "vault", Notebook: "Archive", Section: "Old", Page: "Site"},
		// Different notebooks — unqualified section/page disambiguates.
	}
	s := ShortestUniquePath(pages[0], pages)
	if s != "Projects/Site" {
		t.Errorf("same source, different notebook: expected Projects/Site, got %q", s)
	}
}

func TestShortestUniquePath_UniqueAcrossSources(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Daily"},
		{Source: "linked:ext", Notebook: "Work", Section: "Log", Page: "Site"},
	}
	// "Daily" only exists in vault — basename is unique across all sources.
	s := ShortestUniquePath(pages[0], pages)
	if s != "Daily" {
		t.Errorf("unique basename across sources: expected Daily, got %q", s)
	}
}

// --- DB integration: source in page_links ---

func TestPageLinks_SourceColumnIndexed(t *testing.T) {
	dm := newTestDB(t)
	// Vault source page with a link.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})
	// Linked source page with a link.
	idx(t, dm, "linked:ext", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[Target]]"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})

	// Verify both page_links rows exist with correct source.
	rows, err := dm.ListPageLinksByTargetRaws([]string{"Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 page_links rows, got %d", len(rows))
	}

	sources := map[string]bool{}
	for _, r := range rows {
		sources[r.Source] = true
	}
	if !sources["vault"] || !sources["linked:ext"] {
		t.Errorf("expected both sources, got %v", sources)
	}
}

func TestPageLinks_SourceBackfillFromMigration(t *testing.T) {
	dm, dbPath := newOnDiskDB(t)

	// Index a block so blocks.source has a known value.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target content"),
	})

	// Close and reopen to simulate a vault restart.
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dm2.Close()

	// page_links should have been migrated with source backfilled.
	rows, err := dm2.ListPageLinksByTargetRaws([]string{"Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after reopen, got %d", len(rows))
	}
	if rows[0].Source != "vault" {
		t.Errorf("expected source='vault', got %q", rows[0].Source)
	}
}

func TestPageLinks_PKDistinctAcrossSources(t *testing.T) {
	dm := newTestDB(t)
	// Two pages in different sources with the same notebook/section/page/block
	// linking to the same target. These must be distinct rows.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})
	idx(t, dm, "linked:ext", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[Target]]"),
	})

	rows, err := dm.ListAllPageLinks()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 distinct rows across sources, got %d", len(rows))
	}
}

func TestPageLinks_LegacyMigrationPreservesData(t *testing.T) {
	dm, _ := newOnDiskDB(t)

	// Index data normally.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target#Intro|link text]]"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})

	// Verify heading/alias are preserved.
	rows, err := dm.ListPageLinksByTargetRaws([]string{"Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatal("expected 1 row")
	}
	if rows[0].Heading != "Intro" {
		t.Errorf("heading: %q", rows[0].Heading)
	}
	if rows[0].Alias != "link text" {
		t.Errorf("alias: %q", rows[0].Alias)
	}
}

// --- Linked collisions in backlinks ---

func TestGetBacklinks_SourceQualifiedTarget(t *testing.T) {
	dm := newTestDB(t)
	// Vault target: VaultNB/Sec/VaultPage
	idx(t, dm, "vault", "VaultNB", "Sec", "VaultPage", []parser.ParsedBlock{
		noteBlock(uuidA, "vault target"),
	})
	// Linked target: LinkedNB/Sec/LinkedPage
	idx(t, dm, "linked:ext", "LinkedNB", "Sec", "LinkedPage", []parser.ParsedBlock{
		noteBlock(uuidF, "linked target"),
	})
	// Source page in vault links to [[VaultPage]] — unique basename.
	idx(t, dm, "vault", "VaultNB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[VaultPage]]"),
	})

	// Backlinks for vault target: should find the vault source page-link.
	bl, err := dm.GetBacklinks("vault", "VaultNB", "Sec", "VaultPage")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 {
		t.Fatalf("vault target: expected 1 backlink, got %d: %+v", len(bl), bl)
	}

	// Backlinks for linked target: should find nothing (no one links to it).
	bl2, err := dm.GetBacklinks("linked:ext", "LinkedNB", "Sec", "LinkedPage")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 0 {
		t.Fatalf("linked target: expected 0 backlinks, got %d", len(bl2))
	}
}

func TestGetBacklinks_AmbiguousBasenameAcrossSources(t *testing.T) {
	dm := newTestDB(t)
	// Vault page NB/Sec/Daily
	idx(t, dm, "vault", "NB", "Sec", "Daily", []parser.ParsedBlock{
		noteBlock(uuidA, "vault daily"),
	})
	// Linked page NB/Sec/Daily (same notebook/section/page, different source)
	idx(t, dm, "linked:ext", "NB", "Sec", "Daily", []parser.ParsedBlock{
		noteBlock(uuidF, "linked daily"),
	})
	// Source page links to [[Daily]] — ambiguous across sources.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Daily]] here"),
	})

	// Both targets should have 0 backlinks (ambiguous link excluded).
	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "Daily")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 0 {
		t.Fatalf("vault Daily: ambiguous link should be excluded, got %d", len(bl))
	}

	bl2, err := dm.GetBacklinks("linked:ext", "NB", "Sec", "Daily")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 0 {
		t.Fatalf("linked Daily: ambiguous link should be excluded, got %d", len(bl2))
	}
}

// --- Source-aware LinkTargetRawCandidates ---

func TestLinkTargetRawCandidates_QualifiedLinkedForm(t *testing.T) {
	// When a linked notebook page collides on basename, ShortestUniquePath emits
	// "linked:abc/Work/Site". LinkTargetRawCandidates must include this form so
	// the rename/backlinks paths discover and rewrite those rows.
	got := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: "linked:ext", Notebook: "Work", Section: "Projects", Page: "Site"},
	})
	want := []string{
		"Site",
		"Projects/Site",
		"Work/Projects/Site",
		"linked:ext/Work/Projects/Site",
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
			t.Errorf("missing qualified candidate %q in %v", w, got)
		}
	}
}

func TestLinkTargetRawCandidates_QualifiedLinkedBasename(t *testing.T) {
	// "linked:ext/Site" is a valid ShortestUniquePath form.
	got := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: "linked:ext", Notebook: "Work", Section: "Log", Page: "Site"},
	})
	want := []string{
		"Site",
		"Log/Site",
		"Work/Log/Site",
		"linked:ext/Site",
		"linked:ext/Log/Site",
		"linked:ext/Work/Log/Site",
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

func TestLinkTargetRawCandidates_VaultSourceNeverEmitsVaultQualified(t *testing.T) {
	// Vault source must NEVER emit "vault/..." qualified forms — "vault/" is a
	// valid legacy notebook path prefix, not a source qualifier.
	got := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: "vault", Notebook: "Work", Section: "Projects", Page: "Site"},
	})
	for _, g := range got {
		if strings.HasPrefix(g, "vault/") {
			t.Errorf("vault source must not emit vault-qualified candidate %q", g)
		}
	}
	// Must still emit unqualified forms.
	want := []string{
		"Site",
		"Projects/Site",
		"Work/Projects/Site",
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

func TestLinkTargetRawCandidates_LinkedSourceOmitsVaultQualified(t *testing.T) {
	// A linked source must NEVER emit vault-qualified candidates — a linked-page
	// rename must not match vault-qualified rows.
	got := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: "linked:ext", Notebook: "Work", Section: "Projects", Page: "Site"},
	})
	for _, g := range got {
		if strings.HasPrefix(g, "vault/") {
			t.Errorf("linked source must not emit vault-qualified candidate %q", g)
		}
	}
	// Must still emit the linked-qualified forms.
	wantLinked := []string{
		"linked:ext/Site",
		"linked:ext/Projects/Site",
		"linked:ext/Work/Projects/Site",
	}
	for _, w := range wantLinked {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("missing linked-qualified candidate %q in %v", w, got)
		}
	}
}

// --- Qualified linked backlinks ---

func TestGetBacklinks_QualifiedLinkedTarget(t *testing.T) {
	dm := newTestDB(t)
	// Vault page Work/Sec/Site.
	idx(t, dm, "vault", "Work", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidA, "vault site"),
	})
	// Linked page Work/Sec/Site (same path, different source).
	idx(t, dm, "linked:ext", "Work", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidF, "linked site"),
	})
	// Source page uses the qualified form to disambiguate.
	idx(t, dm, "vault", "Other", "", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[linked:ext/Work/Sec/Site]]"),
	})

	// Backlinks for the linked target should find the qualified link.
	bl, err := dm.GetBacklinks("linked:ext", "Work", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 1 {
		t.Fatalf("expected 1 qualified backlink, got %d: %+v", len(bl), bl)
	}
	if bl[0].Kind != BacklinkPageLink {
		t.Errorf("expected page-link kind, got %q", bl[0].Kind)
	}

	// Backlinks for the vault target should NOT include the qualified link.
	bl2, err := dm.GetBacklinks("vault", "Work", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 0 {
		t.Fatalf("vault target should have no qualified-linked backlinks, got %d", len(bl2))
	}
}

// --- MapTargetRaw qualified form preservation ---

func TestMapTargetRaw_QualifiedLinkedPreservesQualifier(t *testing.T) {
	// Renaming linked:ext/Work/Sec/Site → linked:ext/Work/Sec/Web should
	// preserve the "linked:ext/" qualifier.
	got := MapTargetRaw("linked:ext/Work/Sec/Site", "Work", "Sec", "Site", "Work", "Sec", "Web")
	if got != "linked:ext/Work/Sec/Web" {
		t.Errorf("qualified map: got %q", got)
	}
	// Basename form.
	got2 := MapTargetRaw("linked:ext/Site", "Work", "Sec", "Site", "Work", "Sec", "Web")
	if got2 != "linked:ext/Web" {
		t.Errorf("qualified basename map: got %q", got2)
	}
}

func TestMapTargetRaw_QualifiedSectionPreservesDepth(t *testing.T) {
	got := MapTargetRaw("linked:abc/Sec/Site", "NB", "Sec", "Site", "NB", "Sec", "Web")
	if got != "linked:abc/Sec/Web" {
		t.Errorf("qualified section map: got %q", got)
	}
}

// --- No vault-qualified source isolation ---

func TestGetBacklinks_VaultPrefixNotSourceQualified(t *testing.T) {
	dm := newTestDB(t)
	// Vault page and linked page with same path.
	idx(t, dm, "vault", "Work", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidA, "vault site"),
	})
	idx(t, dm, "linked:ext", "Work", "Sec", "Site", []parser.ParsedBlock{
		noteBlock(uuidF, "linked site"),
	})
	// Source page links with vault/ prefix — NOT source-qualified, treated as
	// a link to a page at notebook="vault" which doesn't exist.
	idx(t, dm, "vault", "Other", "", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[vault/Work/Sec/Site]]"),
	})

	// Backlinks for vault target: vault/... link won't resolve to vault/Work
	// since "vault" is not a notebook. No backlinks expected.
	bl, err := dm.GetBacklinks("vault", "Work", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 0 {
		t.Fatalf("vault target should have 0 backlinks (vault/ not qualified), got %d: %+v", len(bl), bl)
	}

	// Backlinks for linked target: same — vault/... won't resolve here.
	bl2, err := dm.GetBacklinks("linked:ext", "Work", "Sec", "Site")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 0 {
		t.Fatalf("linked target should not have vault-prefixed backlinks, got %d", len(bl2))
	}
}

// --- Similar targets across sources (no false cross-source matches) ---

func TestGetBacklinks_SimilarTargetsAcrossSources(t *testing.T) {
	dm := newTestDB(t)
	// Vault: NB/Sec/SiteA and NB/Sec/SiteB
	idx(t, dm, "vault", "NB", "Sec", "SiteA", []parser.ParsedBlock{
		noteBlock(uuidA, "site A"),
	})
	idx(t, dm, "vault", "NB", "Sec", "SiteB", []parser.ParsedBlock{
		noteBlock(uuidE, "site B"),
	})
	// Linked: NB/Sec/SiteA (same notebook/section/page as vault SiteA)
	idx(t, dm, "linked:ext", "NB", "Sec", "SiteA", []parser.ParsedBlock{
		noteBlock(uuidF, "linked site A"),
	})
	// Source page links to [[SiteA]] — ambiguous across sources.
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[SiteA]]"),
	})
	// Source page links to [[SiteB]] — unique.
	idx(t, dm, "vault", "NB", "Sec", "Src2", []parser.ParsedBlock{
		noteBlock(uuidC, "see [[SiteB]]"),
	})

	// Vault SiteA backlinks: [[SiteA]] is ambiguous → excluded.
	bl, err := dm.GetBacklinks("vault", "NB", "Sec", "SiteA")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl) != 0 {
		t.Fatalf("ambiguous [[SiteA]] should be excluded for vault SiteA, got %d", len(bl))
	}

	// Linked SiteA backlinks: [[SiteA]] is ambiguous → excluded.
	bl2, err := dm.GetBacklinks("linked:ext", "NB", "Sec", "SiteA")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl2) != 0 {
		t.Fatalf("ambiguous [[SiteA]] should be excluded for linked SiteA, got %d", len(bl2))
	}

	// Vault SiteB backlinks: [[SiteB]] is unique → included.
	bl3, err := dm.GetBacklinks("vault", "NB", "Sec", "SiteB")
	if err != nil {
		t.Fatal(err)
	}
	if len(bl3) != 1 {
		t.Fatalf("unique [[SiteB]] should be included, got %d", len(bl3))
	}
}

// --- Migration restart safety ---

func TestPageLinks_MigrationRestartSafe(t *testing.T) {
	// Simulate a vault that was created before the source column.
	dm, dbPath := newOnDiskDB(t)
	// Index a block so blocks table has data for FK backfill.
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})

	// Drop the new-style page_links and recreate with old-style schema
	// (5-column PK, no source column). Include all columns for the backfill.
	dm.SQLDB().Exec("DROP TABLE IF EXISTS page_links")
	dm.SQLDB().Exec(`
		CREATE TABLE page_links (
			source_notebook TEXT NOT NULL,
			source_section  TEXT NOT NULL,
			source_page     TEXT NOT NULL,
			source_block_id TEXT NOT NULL,
			target_raw      TEXT NOT NULL,
			target_notebook TEXT,
			target_section  TEXT,
			target_page     TEXT,
			heading         TEXT,
			alias           TEXT,
			PRIMARY KEY (source_notebook, source_section, source_page, source_block_id, target_raw)
		)`)

	// Insert old-style data (no source column). FK points to existing block.
	dm.SQLDB().Exec(`INSERT INTO page_links (source_notebook, source_section, source_page, source_block_id, target_raw)
		VALUES ('NB', 'Sec', 'Src', '` + uuidB + `', 'Target')`)

	// Close (simulates crash after ADD COLUMN but before rebuild).
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen — migration must detect old PK and rebuild.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dm2.Close()

	// Verify the table was rebuilt: source column exists and has 'vault' backfill.
	rows, err := dm2.ListPageLinksByTargetRaws([]string{"Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after migration restart, got %d", len(rows))
	}
	if rows[0].Source != "vault" {
		t.Errorf("expected source='vault' after restart migration, got %q", rows[0].Source)
	}

	// Verify the PK includes source (no duplicate-key error on insert).
	// First create the FK target block.
	idx(t, dm2, "linked:ext", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidC, "linked src"),
	})
	_, err = dm2.SQLDB().Exec(`INSERT OR IGNORE INTO page_links (source, source_notebook, source_section, source_page, source_block_id, target_raw)
		VALUES ('linked:ext', 'NB', 'Sec', 'Src', '` + uuidC + `', 'Target')`)
	if err != nil {
		t.Fatalf("insert with source should succeed on new PK: %v", err)
	}
}

func TestPageLinks_MigrationCrashBetweenAddColumnAndRebuild(t *testing.T) {
	// Simulate the exact crash scenario: ALTER TABLE ADD COLUMN succeeds,
	// then crash before rebuild. On reopen, the migration must still run.
	dm, dbPath := newOnDiskDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})

	// Recreate with old-style schema (same as real pre-source vaults).
	dm.SQLDB().Exec("DROP TABLE IF EXISTS page_links")
	dm.SQLDB().Exec(`
		CREATE TABLE page_links (
			source_notebook TEXT NOT NULL,
			source_section  TEXT NOT NULL,
			source_page     TEXT NOT NULL,
			source_block_id TEXT NOT NULL,
			target_raw      TEXT NOT NULL,
			target_notebook TEXT,
			target_section  TEXT,
			target_page     TEXT,
			heading         TEXT,
			alias           TEXT,
			PRIMARY KEY (source_notebook, source_section, source_page, source_block_id, target_raw)
		)`)
	dm.SQLDB().Exec(`INSERT INTO page_links (source_notebook, source_section, source_page, source_block_id, target_raw)
		VALUES ('NB', 'Sec', 'Src', '` + uuidB + `', 'Target')`)

	// Manually add the source column (simulates the ALTER succeeding).
	dm.SQLDB().Exec("ALTER TABLE page_links ADD COLUMN source TEXT NOT NULL DEFAULT 'vault'")
	// DO NOT run the rebuild — this simulates the crash.

	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}

	// Reopen: migration must detect old PK (source not in PK SQL) and rebuild.
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dm2.Close()

	// Verify data survived and has correct source.
	rows, err := dm2.ListPageLinksByTargetRaws([]string{"Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after crash-restart migration, got %d", len(rows))
	}
	if rows[0].Source != "vault" {
		t.Errorf("expected source='vault', got %q", rows[0].Source)
	}

	// Verify PK is now 6-column (source-first) by inserting a same-key row
	// with a different source — should succeed.
	idx(t, dm2, "linked:ext", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidC, "linked src"),
	})
	_, err = dm2.SQLDB().Exec(`INSERT OR IGNORE INTO page_links (source, source_notebook, source_section, source_page, source_block_id, target_raw)
		VALUES ('linked:ext', 'NB', 'Sec', 'Src', '` + uuidC + `', 'Target')`)
	if err != nil {
		t.Fatalf("new PK should allow same path different source: %v", err)
	}
}

func TestPageLinks_FreshVaultNoRebuild(t *testing.T) {
	// Fresh vault already has source in PK — ensurePageLinksSourceMigrated
	// should be a no-op (no unnecessary rebuild).
	dm, dbPath := newOnDiskDB(t)
	idx(t, dm, "vault", "NB", "Sec", "Target", []parser.ParsedBlock{
		noteBlock(uuidA, "target"),
	})
	idx(t, dm, "vault", "NB", "Sec", "Src", []parser.ParsedBlock{
		noteBlock(uuidB, "see [[Target]]"),
	})

	// Close and reopen — should be a clean reopen with no rebuild needed.
	if err := dm.Close(); err != nil {
		t.Fatal(err)
	}
	dm2, err := NewDatabaseManager(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer dm2.Close()

	rows, err := dm2.ListPageLinksByTargetRaws([]string{"Target"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row after clean reopen, got %d", len(rows))
	}
	if rows[0].Source != "vault" {
		t.Errorf("expected source='vault', got %q", rows[0].Source)
	}
}

// --- Legacy vault notebook regression tests ---

func TestParseQualifiedTarget_LegacyVaultNotebook(t *testing.T) {
	// A notebook literally named "vault" has paths like "vault/Section/Page".
	// This must NOT be parsed as source-qualified.
	src, path, q := ParseQualifiedTarget("vault/MySection/MyPage")
	if q {
		t.Fatalf("vault/ notebook prefix should be unqualified: src=%q path=%q", src, path)
	}
	if path != "vault/MySection/MyPage" {
		t.Errorf("path should preserve vault/ prefix: got %q", path)
	}
}

func TestResolvePageLinkAgainst_LegacyVaultNotebook(t *testing.T) {
	// A notebook named "vault" with section "Sec" and page "Page" should
	// be resolvable via its full path "vault/Sec/Page".
	pages := []PageLoc{
		{Source: "vault", Notebook: "vault", Section: "Sec", Page: "Page"},
		{Source: "vault", Notebook: "Other", Section: "Sec", Page: "Page"},
	}

	// "vault/Sec/Page" resolves to the vault notebook's page.
	ref := ResolvePageLinkAgainst("vault/Sec/Page", pages)
	if !ref.Exists || ref.Notebook != "vault" || ref.Page != "Page" {
		t.Fatalf("vault notebook path should resolve: %+v", ref)
	}

	// Bare "Page" is ambiguous (both notebooks have it).
	ref2 := ResolvePageLinkAgainst("Page", pages)
	if ref2.Exists || !ref2.Ambiguous {
		t.Fatalf("bare Page should be ambiguous: %+v", ref2)
	}

	// "Sec/Page" is still ambiguous (both have section Sec).
	ref3 := ResolvePageLinkAgainst("Sec/Page", pages)
	if ref3.Exists || !ref3.Ambiguous {
		t.Fatalf("Sec/Page should be ambiguous: %+v", ref3)
	}
}

func TestShortestUniquePath_LegacyVaultNotebook(t *testing.T) {
	pages := []PageLoc{
		{Source: "vault", Notebook: "vault", Section: "Sec", Page: "Page"},
		{Source: "vault", Notebook: "Other", Section: "Sec", Page: "Page"},
	}
	// The vault notebook's page needs its notebook name to disambiguate.
	s := ShortestUniquePath(pages[0], pages)
	if s != "vault/Sec/Page" {
		t.Errorf("vault notebook needs full path disambiguation: got %q", s)
	}
	// The other notebook's page also needs its name.
	s2 := ShortestUniquePath(pages[1], pages)
	if s2 != "Other/Sec/Page" {
		t.Errorf("Other notebook needs full path disambiguation: got %q", s2)
	}
}

func TestMapTargetRaw_LegacyVaultNotebookPreserved(t *testing.T) {
	// Renaming a page in a notebook named "vault" should preserve the
	// "vault/" prefix in the link target.
	got := MapTargetRaw("vault/Sec/Page", "vault", "Sec", "Page", "vault", "Sec", "NewPage")
	if got != "vault/Sec/NewPage" {
		t.Errorf("vault notebook rename should preserve prefix: got %q", got)
	}
}

func TestLinkTargetRawCandidates_LegacyVaultNotebookInCandidates(t *testing.T) {
	// A notebook named "vault" should emit "vault/Sec/Page" as a candidate
	// via PathVariants, NOT via any vault-qualified mechanism.
	got := LinkTargetRawCandidates([]LinkTargetSpec{
		{Source: "vault", Notebook: "vault", Section: "Sec", Page: "Page"},
	})
	want := []string{
		"Page",
		"Sec/Page",
		"vault/Sec/Page",
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
	// No vault/ prefix forms should appear beyond the notebook name.
	for _, g := range got {
		if g == "vault/Page" || g == "vault/vault/Page" {
			t.Errorf("unexpected vault-qualified candidate: %q", g)
		}
	}
}
