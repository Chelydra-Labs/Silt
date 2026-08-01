package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"silt/backend/config"
	"silt/backend/parser"
)

func TestNavigationPreferences_RejectMultiSegmentNotebook(t *testing.T) {
	app := newTestApp(t)
	const evil = "Work/Evil"

	if err := app.SetNavigationSectionExpanded(evil, "Projects", true); err == nil {
		t.Fatalf("SetNavigationSectionExpanded accepted multi-segment notebook %q", evil)
	}
	if err := app.RecordRecentPage(evil, "Projects", "Site"); err == nil {
		t.Fatalf("RecordRecentPage accepted multi-segment notebook %q", evil)
	}
	if err := app.SetFavoritePage(evil, "Projects", "Site", true); err == nil {
		t.Fatalf("SetFavoritePage accepted multi-segment notebook %q", evil)
	}
}

func TestRecordRecentPage_RefreshesOpenedAtWhenAlreadyMostRecent(t *testing.T) {
	app := newTestApp(t)

	if err := app.RecordRecentPage("Work", "Projects", "Site"); err != nil {
		t.Fatalf("RecordRecentPage first: %v", err)
	}
	prefs, err := app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences: %v", err)
	}
	if len(prefs.RecentPages) != 1 {
		t.Fatalf("expected 1 recent page, got %+v", prefs.RecentPages)
	}
	firstOpenedAt := prefs.RecentPages[0].OpenedAt
	if firstOpenedAt <= 0 {
		t.Fatalf("expected positive OpenedAt, got %d", firstOpenedAt)
	}

	// Force a stale head timestamp so a same-second re-record still mutates.
	if err := app.mutateConfig(func(cfg *config.SystemConfig) error {
		items := append([]config.RecentPage(nil), cfg.UI.RecentPages...)
		items[0].OpenedAt = firstOpenedAt - 10
		cfg.UI.RecentPages = items
		return nil
	}); err != nil {
		t.Fatalf("seed stale OpenedAt: %v", err)
	}
	stale, err := app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences after seed: %v", err)
	}
	staleOpenedAt := stale.RecentPages[0].OpenedAt

	if err := app.RecordRecentPage("Work", "Projects", "Site"); err != nil {
		t.Fatalf("RecordRecentPage second: %v", err)
	}
	prefs, err = app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences after re-record: %v", err)
	}
	if len(prefs.RecentPages) != 1 {
		t.Fatalf("expected still 1 recent page, got %+v", prefs.RecentPages)
	}
	if prefs.RecentPages[0].OpenedAt <= staleOpenedAt {
		t.Fatalf("expected OpenedAt to refresh past %d, got %d", staleOpenedAt, prefs.RecentPages[0].OpenedAt)
	}
	if prefs.RecentPages[0].Notebook != "Work" || prefs.RecentPages[0].Section != "Projects" || prefs.RecentPages[0].Page != "Site" {
		t.Fatalf("unexpected recent page after re-record: %+v", prefs.RecentPages[0])
	}
}

func assertRenamePageIndexed(t *testing.T, app *App, notebook, section, page string) {
	t.Helper()
	blocks, err := app.db.FetchPageBlocks("vault", notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks(%s/%s/%s): %v", notebook, section, page, err)
	}
	if len(blocks) == 0 {
		t.Fatalf("expected indexed blocks for %s/%s/%s", notebook, section, page)
	}
}

func seedRenamePage(t *testing.T, app *App, notebook, section, page string) {
	t.Helper()
	path := filepath.Join(app.vaultPath, notebook, filepath.FromSlash(section), page+".md")
	content := fmt.Sprintf("---\nnotebook: %q\nsection: %q\npage: %q\ndate: 2026-01-01\ntags: []\n---\n# %s <!-- id: 11111111-1111-4111-8111-111111111111 -->\n", notebook, section, page, page)
	writeFile(t, path, content)
	blocks, meta, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
		t.Fatalf("IndexFileBlocks: %v", err)
	}
}

// TestListNavigation_DeepNesting verifies that #88's recursive walker
// surfaces sections at any depth and preserves the section's own pages
// alongside its nested children. The on-disk layout is:
//
//	<vault>/Work/Projects/Active/Site.md   (section="Projects/Active", page="Site")
//	<vault>/Work/Journal/Daily.md          (section="Journal", page="Daily")
//	<vault>/Work/Top.md                     (no section; page="Top")
//
// The expected tree is:
//
//	Work
//	  ├ "" (no section) -> [Top]
//	  ├ Projects
//	  │   └ Active -> [Site]
//	  └ Journal -> [Daily]
func TestListNavigation_DeepNesting(t *testing.T) {
	app := newTestApp(t)

	root := app.vaultPath
	// Layout
	for _, p := range []string{
		filepath.Join(root, "Work", "Projects", "Active"),
		filepath.Join(root, "Work", "Journal"),
	} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	writeFile(t, filepath.Join(root, "Work", "Projects", "Active", "Site.md"),
		"---\nnotebook: Work\nsection: Projects/Active\npage: Site\ndate: 2026-06-15\ntags: []\n---\n# Site\n")
	writeFile(t, filepath.Join(root, "Work", "Journal", "Daily.md"),
		"---\nnotebook: Work\nsection: Journal\npage: Daily\ndate: 2026-06-15\ntags: []\n---\n# Daily\n")
	writeFile(t, filepath.Join(root, "Work", "Top.md"),
		"---\nnotebook: Work\nsection: \"\"\npage: Top\ndate: 2026-06-15\ntags: []\n---\n# Top\n")

	tree, err := app.ListNavigation()
	if err != nil {
		t.Fatalf("ListNavigation: %v", err)
	}
	if len(tree.Notebooks) != 1 {
		t.Fatalf("expected 1 notebook, got %d", len(tree.Notebooks))
	}
	work := tree.Notebooks[0]
	if work.Name != "Work" {
		t.Fatalf("notebook name = %q", work.Name)
	}
	if len(work.Sections) < 3 {
		t.Fatalf("expected at least 3 top-level sections (no-section, Projects, Journal), got %d", len(work.Sections))
	}

	// Find the section-less group first (Name == "").
	var sectionless *parser.NavigationSection
	var projects *parser.NavigationSection
	var journal *parser.NavigationSection
	for i := range work.Sections {
		sec := &work.Sections[i]
		switch sec.Name {
		case "":
			sectionless = sec
		case "Projects":
			projects = sec
		case "Journal":
			journal = sec
		}
	}
	if sectionless == nil || projects == nil || journal == nil {
		t.Fatalf("missing top-level sections: sectionless=%v projects=%v journal=%v", sectionless, projects, journal)
	}

	// Section-less: one page (Top).
	if len(sectionless.Pages) != 1 || sectionless.Pages[0].Name != "Top" {
		t.Errorf("section-less pages = %+v", sectionless.Pages)
	}

	// Projects has one child ("Active") which has the Site page.
	if len(projects.Children) != 1 {
		t.Fatalf("Projects children = %d, want 1", len(projects.Children))
	}
	active := projects.Children[0]
	if active.Name != "Active" {
		t.Errorf("Active.Name = %q", active.Name)
	}
	if len(active.Pages) != 1 || active.Pages[0].Name != "Site" {
		t.Errorf("Active pages = %+v", active.Pages)
	}

	// Journal is a flat section with one page.
	if len(journal.Pages) != 1 || journal.Pages[0].Name != "Daily" {
		t.Errorf("Journal pages = %+v", journal.Pages)
	}
	if len(journal.Children) != 0 {
		t.Errorf("Journal should have no children, got %d", len(journal.Children))
	}
}

func TestNestedNavigationCRUD_ReconcilesPreferences(t *testing.T) {
	app := newTestApp(t)
	if err := app.CreateSection("TestNB", "Projects", "Active"); err != nil {
		t.Fatalf("CreateSection: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "Projects/Active", "Site", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if err := app.SetNavigationSectionExpanded("TestNB", "Projects", true); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if err := app.SetFavoritePage("TestNB", "Projects/Active", "Site", true); err != nil {
		t.Fatalf("favorite: %v", err)
	}
	if err := app.RecordRecentPage("TestNB", "Projects/Active", "Site"); err != nil {
		t.Fatalf("recent: %v", err)
	}
	if err := app.RenameSection("TestNB", "Projects", "Archive"); err != nil {
		t.Fatalf("RenameSection: %v", err)
	}
	if _, err := os.Stat(filepath.Join(app.vaultPath, "TestNB", "Archive", "Active", "Site.md")); err != nil {
		t.Fatalf("nested page after section rename: %v", err)
	}
	prefs, err := app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences: %v", err)
	}
	if len(prefs.ExpandedSections) != 1 || prefs.ExpandedSections[0].Path != "Archive" {
		t.Fatalf("expanded refs not reconciled: %+v", prefs.ExpandedSections)
	}
	if len(prefs.Favorites) != 1 || prefs.Favorites[0].Section != "Archive/Active" {
		t.Fatalf("favorites not reconciled: %+v", prefs.Favorites)
	}
	if len(prefs.RecentPages) != 1 || prefs.RecentPages[0].Section != "Archive/Active" {
		t.Fatalf("recents not reconciled: %+v", prefs.RecentPages)
	}
	if err := app.DeleteSection("TestNB", "Archive"); err != nil {
		t.Fatalf("DeleteSection: %v", err)
	}
	prefs, err = app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences after delete: %v", err)
	}
	if len(prefs.Favorites) != 0 || len(prefs.RecentPages) != 0 || len(prefs.ExpandedSections) != 0 {
		t.Fatalf("deleted subtree left stale prefs: %+v", prefs)
	}
}

func TestMovePage_RejectsSymlinkedDestination(t *testing.T) {
	app := newTestApp(t)
	external := t.TempDir()
	link := filepath.Join(app.vaultPath, "Work", "External")
	if err := os.Symlink(external, link); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	source := filepath.Join(app.vaultPath, "Work", "Source", "Page.md")
	writeFile(t, source, "---\nnotebook: Work\nsection: Source\npage: Page\n---\n# Page\n")
	if err := app.MovePage("Work", "Source", "External", "Page"); err == nil {
		t.Fatal("MovePage accepted a destination through a parent symlink")
	}
	if _, err := os.Stat(filepath.Join(external, "Page.md")); !os.IsNotExist(err) {
		t.Fatalf("MovePage wrote outside the notebook root: %v", err)
	}
}

func TestSidebarViewPreference_RoundTripsThroughNavigationIPC(t *testing.T) {
	app := newTestApp(t)

	prefs, err := app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences: %v", err)
	}
	if prefs.SidebarView != "tree" {
		t.Fatalf("sidebar view should default to \"tree\", got %q", prefs.SidebarView)
	}

	if err := app.SetSidebarView("quick"); err != nil {
		t.Fatalf("SetSidebarView(\"quick\"): %v", err)
	}
	prefs, err = app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences after set quick: %v", err)
	}
	if prefs.SidebarView != "quick" {
		t.Fatalf("sidebar view should report \"quick\" after setter, got %q", prefs.SidebarView)
	}

	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if loaded.UI.SidebarView == nil || *loaded.UI.SidebarView != "quick" {
		t.Fatalf("\"quick\" was not persisted in config.yaml: %v", loaded.UI.SidebarView)
	}

	if err := app.SetSidebarView("tree"); err != nil {
		t.Fatalf("SetSidebarView(\"tree\"): %v", err)
	}
	prefs, err = app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences after set tree: %v", err)
	}
	if prefs.SidebarView != "tree" {
		t.Fatalf("sidebar view should report \"tree\" after setter, got %q", prefs.SidebarView)
	}

	// Invalid views are rejected and do not persist.
	if err := app.SetSidebarView("bogus"); err == nil {
		t.Fatalf("SetSidebarView(\"bogus\") should be rejected")
	}
	prefs, err = app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences after bogus: %v", err)
	}
	if prefs.SidebarView != "tree" {
		t.Fatalf("invalid view must not change the persisted value, got %q", prefs.SidebarView)
	}
}

func TestNavigationPreferenceMutationsAreSerialized(t *testing.T) {
	app := newTestApp(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			section := "Projects/" + string(rune('A'+i))
			if err := app.SetNavigationSectionExpanded("TestNB", section, true); err != nil {
				t.Errorf("expand %d: %v", i, err)
			}
			if err := app.SetFavoritePage("TestNB", section, "Page", true); err != nil {
				t.Errorf("favorite %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	prefs, err := app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences: %v", err)
	}
	if len(prefs.ExpandedSections) != 12 || len(prefs.Favorites) != 12 {
		t.Fatalf("concurrent mutations lost state: expanded=%d favorites=%d", len(prefs.ExpandedSections), len(prefs.Favorites))
	}
}

func TestDeleteSection_NavOrderUsesPathBoundary(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.CreatePage("TestNB", "A", "Removed", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage A: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "Archive", "Kept", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage Archive: %v", err)
	}
	if err := app.SetNavPageOrder("TestNB", "A", []string{"Removed"}); err != nil {
		t.Fatalf("SetNavPageOrder A: %v", err)
	}
	if err := app.SetNavPageOrder("TestNB", "Archive", []string{"Kept"}); err != nil {
		t.Fatalf("SetNavPageOrder Archive: %v", err)
	}
	if err := app.DeleteSection("TestNB", "A"); err != nil {
		t.Fatalf("DeleteSection: %v", err)
	}
	order, err := app.GetNavOrder()
	if err != nil {
		t.Fatalf("GetNavOrder: %v", err)
	}
	if got := order.Pages["TestNB/Archive"]; len(got) != 1 || got[0] != "Kept" {
		t.Fatalf("deleting A removed Archive ordering: %+v", order.Pages)
	}
}

func TestMigrateNavOrderKeys_NestedPathsAndCollisionAreDeterministic(t *testing.T) {
	order := map[string][]string{
		"TestNB/Projects":        {"Projects"},
		"TestNB/Projects/Active": {"Active"},
		// A pre-existing destination is overwritten by the migrated source.
		"TestNB/Archive": {"old"},
	}
	migrateNavOrderKeys(order, "TestNB/Projects", "TestNB/Archive")

	want := map[string][]string{
		"TestNB/Archive":        {"Projects"},
		"TestNB/Archive/Active": {"Active"},
	}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("migrated nav order = %#v, want %#v", order, want)
	}
}

// --- Config UI block tests (#63, #68) ---

func TestGetSetSidebarWidth_RoundTrip(t *testing.T) {
	app := newTestApp(t)

	if w := app.GetSidebarWidth(); w != 256 {
		t.Fatalf("default sidebar width: got %d, want 256", w)
	}

	if err := app.SetSidebarWidth(320); err != nil {
		t.Fatalf("SetSidebarWidth(320): %v", err)
	}
	if w := app.GetSidebarWidth(); w != 320 {
		t.Fatalf("after set: got %d, want 320", w)
	}

	// Reload from disk to verify persistence.
	cfg, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.UI.SidebarWidth != 320 {
		t.Fatalf("persisted: got %d, want 320", cfg.UI.SidebarWidth)
	}
}

func TestSetSidebarWidth_Clamps(t *testing.T) {
	app := newTestApp(t)

	if err := app.SetSidebarWidth(50); err != nil {
		t.Fatalf("SetSidebarWidth(50): %v", err)
	}
	if w := app.GetSidebarWidth(); w != 200 {
		t.Fatalf("below min: got %d, want 200 (clamped)", w)
	}

	if err := app.SetSidebarWidth(999); err != nil {
		t.Fatalf("SetSidebarWidth(999): %v", err)
	}
	if w := app.GetSidebarWidth(); w != 480 {
		t.Fatalf("above max: got %d, want 480 (clamped)", w)
	}
}

func TestNarrowNavOrder_RoundTrip(t *testing.T) {
	app := newTestApp(t)

	if err := app.SetNavNotebookOrder([]string{"Personal", "Work"}); err != nil {
		t.Fatalf("SetNavNotebookOrder: %v", err)
	}
	if err := app.SetNavSectionOrder("Work", "", []string{"Projects", "Inbox"}); err != nil {
		t.Fatalf("SetNavSectionOrder: %v", err)
	}
	if err := app.SetNavPageOrder("Work", "Projects", []string{"Site"}); err != nil {
		t.Fatalf("SetNavPageOrder: %v", err)
	}

	got, err := app.GetNavOrder()
	if err != nil {
		t.Fatalf("GetNavOrder: %v", err)
	}
	if len(got.Notebooks) != 2 || got.Notebooks[0] != "Personal" {
		t.Fatalf("nav order notebooks: got %v", got.Notebooks)
	}
	if len(got.Sections["Work"]) != 2 || got.Sections["Work"][0] != "Projects" {
		t.Fatalf("nav order sections: got %v", got.Sections["Work"])
	}
}

func TestNavOrderNarrowMutationsPreserveStaleClientKeys(t *testing.T) {
	app := newTestApp(t)
	if err := app.SetNavPageOrder("TestNB", "First", []string{"One"}); err != nil {
		t.Fatalf("first page order: %v", err)
	}
	if err := app.SetNavPageOrder("TestNB", "Second", []string{"Two"}); err != nil {
		t.Fatalf("second page order: %v", err)
	}
	if err := app.SetNavSectionOrder("TestNB", "", []string{"First", "Second"}); err != nil {
		t.Fatalf("section order: %v", err)
	}
	// This models two independent clients whose snapshots were both stale when
	// they issued their writes: each narrow mutation must retain the other key.
	if err := app.SetNavPageOrder("TestNB", "First", []string{"One", "Updated"}); err != nil {
		t.Fatalf("updated first page order: %v", err)
	}
	order, err := app.GetNavOrder()
	if err != nil {
		t.Fatalf("GetNavOrder: %v", err)
	}
	if got := order.Pages["TestNB/Second"]; len(got) != 1 || got[0] != "Two" {
		t.Fatalf("stale client lost unrelated page key: %+v", order.Pages)
	}
	if got := order.Sections["TestNB"]; len(got) != 2 || got[1] != "Second" {
		t.Fatalf("stale client lost section order: %+v", order.Sections)
	}

	if _, err := app.CreatePage("TestNB", "Second", "Two", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if err := app.RenameSection("TestNB", "Second", "Renamed"); err != nil {
		t.Fatalf("RenameSection: %v", err)
	}
	order, err = app.GetNavOrder()
	if err != nil {
		t.Fatalf("GetNavOrder after reconciliation: %v", err)
	}
	if got := order.Pages["TestNB/First"]; len(got) != 2 || got[1] != "Updated" {
		t.Fatalf("reconciliation lost unrelated page key: %+v", order.Pages)
	}
}

// --- Rename tests (#62, #83) ---

func TestRenamePage_UpdatesFrontmatterAndFile(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "", "OldPage", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := app.RenamePage("TestNB", "", "OldPage", "NewPage"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	oldPath := filepath.Join(app.vaultPath, "TestNB", "OldPage.md")
	newPath := filepath.Join(app.vaultPath, "TestNB", "NewPage.md")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should not exist after rename")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new file should exist: %v", err)
	}
	if !strings.Contains(string(content), `"NewPage"`) {
		t.Fatalf("frontmatter should contain NewPage: %s", content)
	}
}

// TestRenamePage_RewritesInboundWikiLinks verifies that RenamePage rewrites
// [[OldTarget]] → [[NewTarget]] in other pages via the page_links reverse
// index, while preserving block UUIDs on the renamed page (#545).
func TestRenamePage_RewritesInboundWikiLinks(t *testing.T) {
	app := newTestApp(t)

	targetID := "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	sourceID := "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	targetPath := filepath.Join(app.vaultPath, "Work", "Target.md")
	sourcePath := filepath.Join(app.vaultPath, "Work", "Source.md")
	writeFile(t, targetPath, "---\nnotebook: \"Work\"\nsection: \"\"\npage: \"Target\"\n---\n\n"+
		"target body <!-- id: "+targetID+" -->\n")
	writeFile(t, sourcePath, "---\nnotebook: \"Work\"\nsection: \"\"\npage: \"Source\"\n---\n\n"+
		"See [[Target#Goals|the target]] please <!-- id: "+sourceID+" -->\n")

	for _, p := range []struct {
		path, nb, sec, page string
	}{
		{targetPath, "Work", "", "Target"},
		{sourcePath, "Work", "", "Source"},
	} {
		b, _ := os.ReadFile(p.path)
		blocks, meta, _, _, err := parser.ParseFileContent(string(b), p.nb, p.sec, p.page, "2026-01-01", app.spacesPerTab)
		if err != nil {
			t.Fatalf("parse %s: %v", p.page, err)
		}
		if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
			t.Fatalf("index %s: %v", p.page, err)
		}
	}

	if err := app.RenamePage("Work", "", "Target", "Renamed"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	src, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "[[Renamed#Goals|the target]]") {
		t.Errorf("expected rewritten wiki-link, got:\n%s", s)
	}
	if strings.Contains(s, "[[Target") {
		t.Errorf("old target should be gone, got:\n%s", s)
	}
	// Block UUID on the renamed page is preserved.
	renamed, err := os.ReadFile(filepath.Join(app.vaultPath, "Work", "Renamed.md"))
	if err != nil {
		t.Fatalf("read renamed: %v", err)
	}
	if !strings.Contains(string(renamed), targetID) {
		t.Errorf("block UUID must survive rename, got:\n%s", renamed)
	}
	// ResolvePageLink finds the new name.
	ref, err := app.ResolvePageLink("Renamed")
	if err != nil {
		t.Fatalf("ResolvePageLink: %v", err)
	}
	if !ref.Exists || ref.Page != "Renamed" {
		t.Errorf("ResolvePageLink Renamed: %+v", ref)
	}
}

// TestRenamePage_DoesNotRewriteAmbiguousBasenameLinks verifies that renaming
// one of two same-basename pages does NOT rewrite links pointing at the other
// page (review fix for #545).
func TestRenamePage_DoesNotRewriteAmbiguousBasenameLinks(t *testing.T) {
	app := newTestApp(t)

	// Two pages named "Daily" in different sections — basename is ambiguous.
	dailyA := filepath.Join(app.vaultPath, "Work", "Journal", "Daily.md")
	dailyB := filepath.Join(app.vaultPath, "Archive", "Old", "Daily.md")
	// A third page links to [[Daily]] (ambiguous target).
	source := filepath.Join(app.vaultPath, "Work", "Hub.md")
	writeFile(t, dailyA, "---\nnotebook: \"Work\"\nsection: \"Journal\"\npage: \"Daily\"\n---\n\n"+
		"body A <!-- id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa -->\n")
	writeFile(t, dailyB, "---\nnotebook: \"Archive\"\nsection: \"Old\"\npage: \"Daily\"\n---\n\n"+
		"body B <!-- id: bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb -->\n")
	writeFile(t, source, "---\nnotebook: \"Work\"\nsection: \"\"\npage: \"Hub\"\n---\n\n"+
		"Link [[Daily]] <!-- id: cccccccc-cccc-4ccc-8ccc-cccccccccccc -->\n")

	for _, p := range []struct {
		path, nb, sec, page string
	}{
		{dailyA, "Work", "Journal", "Daily"},
		{dailyB, "Archive", "Old", "Daily"},
		{source, "Work", "", "Hub"},
	} {
		b, _ := os.ReadFile(p.path)
		blocks, meta, _, _, err := parser.ParseFileContent(string(b), p.nb, p.sec, p.page, "2026-01-01", app.spacesPerTab)
		if err != nil {
			t.Fatalf("parse %s: %v", p.page, err)
		}
		if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
			t.Fatalf("index %s: %v", p.page, err)
		}
	}

	// Rename Work/Journal/Daily → Work/Journal/Renamed.
	if err := app.RenamePage("Work", "Journal", "Daily", "Renamed"); err != nil {
		t.Fatalf("RenamePage: %v", err)
	}

	// The ambiguous [[Daily]] link must be UNCHANGED — it could refer to
	// either page and must not be silently rewritten.
	src, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if !strings.Contains(string(src), "[[Daily]]") {
		t.Errorf("ambiguous [[Daily]] must NOT be rewritten:\n%s", src)
	}
}

// TestMovePage_RewritesInboundWikiLinks verifies MovePage rewrites section-
// qualified [[…]] targets via the reverse index (#545).
func TestMovePage_RewritesInboundWikiLinks(t *testing.T) {
	app := newTestApp(t)

	targetPath := filepath.Join(app.vaultPath, "Work", "FromSec", "Moved.md")
	sourcePath := filepath.Join(app.vaultPath, "Work", "Other.md")
	writeFile(t, targetPath, "---\nnotebook: \"Work\"\nsection: \"FromSec\"\npage: \"Moved\"\n---\n\n"+
		"body <!-- id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa -->\n")
	writeFile(t, sourcePath, "---\nnotebook: \"Work\"\nsection: \"\"\npage: \"Other\"\n---\n\n"+
		"See [[FromSec/Moved]] please <!-- id: bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb -->\n")

	for _, p := range []struct {
		path, nb, sec, page string
	}{
		{targetPath, "Work", "FromSec", "Moved"},
		{sourcePath, "Work", "", "Other"},
	} {
		b, _ := os.ReadFile(p.path)
		blocks, meta, _, _, err := parser.ParseFileContent(string(b), p.nb, p.sec, p.page, "2026-01-01", app.spacesPerTab)
		if err != nil {
			t.Fatalf("parse %s: %v", p.page, err)
		}
		if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
			t.Fatalf("index %s: %v", p.page, err)
		}
	}

	if err := app.MovePage("Work", "FromSec", "ToSec", "Moved"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	src, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "[[ToSec/Moved]]") {
		t.Errorf("expected section-qualified rewrite, got:\n%s", s)
	}
	if strings.Contains(s, "[[FromSec/Moved]]") {
		t.Errorf("old section path should be gone, got:\n%s", s)
	}
}

// TestRenameSection_RewritesInboundWikiLinks verifies RenameSection rewrites
// inbound links for every page under the old section (#545).
func TestRenameSection_RewritesInboundWikiLinks(t *testing.T) {
	app := newTestApp(t)

	targetPath := filepath.Join(app.vaultPath, "Work", "OldSec", "Page1.md")
	sourcePath := filepath.Join(app.vaultPath, "Work", "Hub.md")
	writeFile(t, targetPath, "---\nnotebook: \"Work\"\nsection: \"OldSec\"\npage: \"Page1\"\n---\n\n"+
		"body <!-- id: aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa -->\n")
	writeFile(t, sourcePath, "---\nnotebook: \"Work\"\nsection: \"\"\npage: \"Hub\"\n---\n\n"+
		"Link [[OldSec/Page1|p1]] <!-- id: bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb -->\n")

	for _, p := range []struct {
		path, nb, sec, page string
	}{
		{targetPath, "Work", "OldSec", "Page1"},
		{sourcePath, "Work", "", "Hub"},
	} {
		b, _ := os.ReadFile(p.path)
		blocks, meta, _, _, err := parser.ParseFileContent(string(b), p.nb, p.sec, p.page, "2026-01-01", app.spacesPerTab)
		if err != nil {
			t.Fatalf("parse %s: %v", p.page, err)
		}
		if err := app.db.IndexFileBlocks("vault", meta.Notebook, meta.Section, meta.Page, blocks, meta.Tags); err != nil {
			t.Fatalf("index %s: %v", p.page, err)
		}
	}

	if err := app.RenameSection("Work", "OldSec", "NewSec"); err != nil {
		t.Fatalf("RenameSection: %v", err)
	}

	src, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	s := string(src)
	if !strings.Contains(s, "[[NewSec/Page1|p1]]") {
		t.Errorf("expected section rename rewrite, got:\n%s", s)
	}
	if strings.Contains(s, "[[OldSec/") {
		t.Errorf("old section path should be gone, got:\n%s", s)
	}
}

func TestRenamePage_NameCollision(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "", "Page1", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage Page1: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "", "Page2", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage Page2: %v", err)
	}

	err := app.RenamePage("TestNB", "", "Page1", "Page2")
	if err == nil {
		t.Fatalf("rename to existing name should fail")
	}
}

func TestRenamePage_PathTraversal(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "", "Safe", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// sanitizePathSegment strips ".." and "/", so traversal is neutralized.
	// The rename succeeds with a sanitized name, but the file stays in the vault.
	if err := app.RenamePage("TestNB", "", "Safe", "../../../etc/passwd"); err != nil {
		t.Fatalf("rename with traversal chars should succeed (sanitized): %v", err)
	}

	// The file should be inside the vault with a sanitized name, not at /etc/passwd.
	origPath := filepath.Join(app.vaultPath, "TestNB", "Safe.md")
	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Fatalf("old file should not exist after rename")
	}
	// /etc/passwd.md should NOT exist as a result (path stayed in vault).
	if _, err := os.Stat("/etc/passwd.md"); err == nil {
		t.Fatalf("path traversal escaped the vault!")
	}
	// The sanitized file (etcpasswd.md) should exist inside the vault.
	sanitizedPath := filepath.Join(app.vaultPath, "TestNB", "etcpasswd.md")
	if _, err := os.Stat(sanitizedPath); err != nil {
		t.Fatalf("sanitized file should exist inside vault: %v", err)
	}
}

func TestRenameSection_UpdatesAllFiles(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "OldSec", "Page1", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage Page1: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "OldSec", "Page2", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage Page2: %v", err)
	}

	if err := app.RenameSection("TestNB", "OldSec", "NewSec"); err != nil {
		t.Fatalf("RenameSection: %v", err)
	}

	// Both files should be in the new section folder with updated frontmatter.
	for _, pg := range []string{"Page1", "Page2"} {
		path := filepath.Join(app.vaultPath, "TestNB", "NewSec", pg+".md")
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("file %s should exist in new section: %v", pg, err)
		}
		if !strings.Contains(string(content), `"NewSec"`) {
			t.Fatalf("frontmatter should contain NewSec for %s: %s", pg, content)
		}
	}
}

// --- MovePage tests (#177) ---

func TestMovePage_ToSection_UpdatesFileAndFrontmatter(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "", "RootPage", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "Target", "Existing", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage Target/Existing: %v", err)
	}

	if err := app.MovePage("TestNB", "", "Target", "RootPage"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	oldPath := filepath.Join(app.vaultPath, "TestNB", "RootPage.md")
	newPath := filepath.Join(app.vaultPath, "TestNB", "Target", "RootPage.md")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should not exist after move")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("new file should exist: %v", err)
	}
	if !strings.Contains(string(content), `section: "Target"`) {
		t.Fatalf("frontmatter section should be Target: %s", content)
	}
	if !strings.Contains(string(content), `page: "RootPage"`) {
		t.Fatalf("frontmatter page should be preserved: %s", content)
	}
}

func TestMovePage_ToRoot_SectionBecomesEmpty(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "FromSec", "Movable", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := app.MovePage("TestNB", "FromSec", "", "Movable"); err != nil {
		t.Fatalf("MovePage to root: %v", err)
	}

	oldPath := filepath.Join(app.vaultPath, "TestNB", "FromSec", "Movable.md")
	newPath := filepath.Join(app.vaultPath, "TestNB", "Movable.md")

	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatalf("old file should not exist after move to root")
	}
	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("root file should exist: %v", err)
	}
	if !strings.Contains(string(content), `section: ""`) {
		t.Fatalf("frontmatter section should be empty for root: %s", content)
	}
}

func TestMovePage_NameCollision_Rejects(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "SecA", "Shared", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage SecA/Shared: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "SecB", "Shared", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage SecB/Shared: %v", err)
	}

	err := app.MovePage("TestNB", "SecA", "SecB", "Shared")
	if err == nil {
		t.Fatalf("move to section with same-named page should fail")
	}

	// Source file should still exist (move rejected, no data loss).
	srcPath := filepath.Join(app.vaultPath, "TestNB", "SecA", "Shared.md")
	if _, err := os.Stat(srcPath); err != nil {
		t.Fatalf("source file should still exist after rejected move: %v", err)
	}
}

func TestMovePage_PathTraversal_Rejected(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "", "Safe", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// sanitizeSectionPath strips ".." segments, neutralizing traversal.
	// The move either no-ops (if sanitized target == source) or succeeds
	// with a sanitized name staying inside the vault.
	_ = app.MovePage("TestNB", "", "../../../etc", "Safe")

	// The file must still be inside the vault.
	origPath := filepath.Join(app.vaultPath, "TestNB", "Safe.md")
	newPath := filepath.Join(app.vaultPath, "TestNB", "etc", "Safe.md")
	if _, err := os.Stat(origPath); err != nil && !os.IsNotExist(err) {
		t.Fatalf("stat orig: %v", err)
	}
	// Either the file stayed at orig (no-op) or moved to sanitized etc/
	if _, origErr := os.Stat(origPath); origErr == nil {
		return // file stayed — acceptable (no-op or sanitized to same section)
	}
	if _, err := os.Stat(newPath); err != nil {
		t.Fatalf("file should be inside vault (orig or sanitized etc/): %v", err)
	}
}

func TestMovePage_SameSection_NoOp(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "Sec", "Page1", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	err := app.MovePage("TestNB", "Sec", "Sec", "Page1")
	if err != nil {
		t.Fatalf("same-section move should be a no-op, got: %v", err)
	}

	// File unchanged.
	path := filepath.Join(app.vaultPath, "TestNB", "Sec", "Page1.md")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file should still exist after no-op move: %v", err)
	}
}

func TestMovePage_UpdatesNavOrder_BothSectionKeys(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "SecA", "Alpha", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage SecA/Alpha: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "SecA", "Beta", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage SecA/Beta: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "SecB", "Gamma", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage SecB/Gamma: %v", err)
	}

	// Seed nav_order so we can verify the page is removed from the old list.
	if err := app.SetNavPageOrder("TestNB", "SecA", []string{"Alpha", "Beta"}); err != nil {
		t.Fatalf("SetNavPageOrder SecA: %v", err)
	}
	if err := app.SetNavPageOrder("TestNB", "SecB", []string{"Gamma"}); err != nil {
		t.Fatalf("SetNavPageOrder SecB: %v", err)
	}

	if err := app.MovePage("TestNB", "SecA", "SecB", "Alpha"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	order, err := app.GetNavOrder()
	if err != nil {
		t.Fatalf("GetNavOrder: %v", err)
	}
	// Alpha removed from SecA.
	secA := order.Pages["TestNB/SecA"]
	for _, p := range secA {
		if p == "Alpha" {
			t.Errorf("Alpha should be removed from SecA nav_order, got %v", secA)
		}
	}
	if len(secA) != 1 || secA[0] != "Beta" {
		t.Errorf("SecA should contain only Beta, got %v", secA)
	}
	// Alpha appended to SecB.
	secB := order.Pages["TestNB/SecB"]
	found := false
	for _, p := range secB {
		if p == "Alpha" {
			found = true
		}
	}
	if !found {
		t.Errorf("Alpha should be appended to SecB nav_order, got %v", secB)
	}
}

func TestMovePage_LastPageInSection_SectionRemainsVisible(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "Lonely", "Only", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	if err := app.MovePage("TestNB", "Lonely", "", "Only"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	// The now-empty section dir may or may not be pruned by the OS, but
	// ListNavigation should still surface it (sections are shown even when
	// empty per SPECS §3.2).
	tree, err := app.ListNavigation()
	if err != nil {
		t.Fatalf("ListNavigation: %v", err)
	}
	found := false
	for _, sec := range tree.Notebooks[0].Sections {
		if sec.Name == "Lonely" {
			found = true
		}
	}
	if !found {
		t.Errorf("empty section 'Lonely' should remain visible after its last page moves out; sections: %+v", tree.Notebooks[0].Sections)
	}
}

func TestMovePage_SelfWriteSuppressed(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "SecA", "Movable", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "SecB", "Anchor", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage SecB/Anchor: %v", err)
	}

	// Set up a real config watcher so RegisterSelfWrite is meaningful.
	changed := make(chan config.SystemConfig, 4)
	cw, err := config.NewConfigWatcher(app.vaultPath, func(c config.SystemConfig) {
		changed <- c
	}, nil)
	if err != nil {
		t.Fatalf("NewConfigWatcher: %v", err)
	}
	defer cw.Close()
	cw.Start()
	app.configWatcher = cw
	defer func() { app.configWatcher = nil }()

	// Give the watcher time to settle.
	time.Sleep(150 * time.Millisecond)

	if err := app.MovePage("TestNB", "SecA", "SecB", "Movable"); err != nil {
		t.Fatalf("MovePage: %v", err)
	}

	// The watcher should NOT fire within the self-write cooldown window —
	// MovePage calls RegisterSelfWrite before config.Save.
	select {
	case <-changed:
		t.Fatalf("self-write should be suppressed, but config:changed fired")
	case <-time.After(700 * time.Millisecond):
		// expected: no reload within the cooldown window
	}
}

// --- Delete tests (#62) ---

func TestDeletePage_MovesToTrash(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "", "Doomed", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	origPath := filepath.Join(app.vaultPath, "TestNB", "Doomed.md")
	if _, err := os.Stat(origPath); err != nil {
		t.Fatalf("page should exist before delete: %v", err)
	}

	if err := app.DeletePage("TestNB", "", "Doomed"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}

	if _, err := os.Stat(origPath); !os.IsNotExist(err) {
		t.Fatalf("page should not exist after delete")
	}

	// Verify file exists in trash.
	trashDir := filepath.Join(app.vaultPath, ".system", "trash")
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("trash dir should exist: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("trash should contain at least one timestamped folder")
	}
}

// TestDeletePage_EmitsBlockChanged ensures plugin indexes (AI vectors) learn
// about UI deletes via block:changed (#850).
func TestDeletePage_EmitsBlockChanged(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.CreatePage("TestNB", "", "Doomed", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}
	var got []parser.BlockChangedEvent
	app.eventEmit = func(name string, data ...any) {
		if name != string(EventBlockChanged) || len(data) == 0 {
			return
		}
		if ev, ok := data[0].(parser.BlockChangedEvent); ok {
			got = append(got, ev)
		}
	}
	if err := app.DeletePage("TestNB", "", "Doomed"); err != nil {
		t.Fatalf("DeletePage: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 block:changed, got %d", len(got))
	}
	if got[0].Notebook != "TestNB" || got[0].Page != "Doomed" {
		t.Fatalf("unexpected event: %+v", got[0])
	}
}

// TestDeleteSection_EmitsBlockChangedPerPage covers multi-page delete emit (#850).
func TestDeleteSection_EmitsBlockChangedPerPage(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.CreatePage("TestNB", "DoomSec", "P1", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage P1: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "DoomSec", "P2", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage P2: %v", err)
	}
	var pages []string
	app.eventEmit = func(name string, data ...any) {
		if name != string(EventBlockChanged) || len(data) == 0 {
			return
		}
		if ev, ok := data[0].(parser.BlockChangedEvent); ok {
			pages = append(pages, ev.Page)
		}
	}
	if err := app.DeleteSection("TestNB", "DoomSec"); err != nil {
		t.Fatalf("DeleteSection: %v", err)
	}
	if len(pages) != 2 {
		t.Fatalf("expected 2 page emits, got %v", pages)
	}
	sort.Strings(pages)
	if pages[0] != "P1" || pages[1] != "P2" {
		t.Fatalf("unexpected pages: %v", pages)
	}
}

func TestDeleteSection_DeletesAllPages(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("TestNB", "DoomSec", "P1", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage P1: %v", err)
	}
	if _, err := app.CreatePage("TestNB", "DoomSec", "P2", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage P2: %v", err)
	}

	secPath := filepath.Join(app.vaultPath, "TestNB", "DoomSec")
	if _, err := os.Stat(secPath); err != nil {
		t.Fatalf("section should exist: %v", err)
	}

	if err := app.DeleteSection("TestNB", "DoomSec"); err != nil {
		t.Fatalf("DeleteSection: %v", err)
	}

	if _, err := os.Stat(secPath); !os.IsNotExist(err) {
		t.Fatalf("section should not exist after delete")
	}
}

// --- Notebook-level tests (#62) ---

func TestRenameNotebook_UpdatesAllFiles(t *testing.T) {
	app := newTestApp(t)

	// Seed a section-less page and a sectioned page.
	if _, err := app.CreatePage("OldNB", "", "TopPage", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage TopPage: %v", err)
	}
	if _, err := app.CreatePage("OldNB", "Sec1", "NestedPage", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage NestedPage: %v", err)
	}

	if err := app.RenameNotebook("OldNB", "NewNB"); err != nil {
		t.Fatalf("RenameNotebook: %v", err)
	}

	// Old notebook folder should not exist.
	oldDir := filepath.Join(app.vaultPath, "OldNB")
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatalf("old notebook dir should not exist after rename")
	}

	// Both files should be under NewNB with updated notebook: frontmatter.
	checks := []struct {
		relPath string
	}{
		{filepath.Join("NewNB", "TopPage.md")},
		{filepath.Join("NewNB", "Sec1", "NestedPage.md")},
	}
	for _, c := range checks {
		full := filepath.Join(app.vaultPath, c.relPath)
		content, err := os.ReadFile(full)
		if err != nil {
			t.Fatalf("file %s should exist under NewNB: %v", c.relPath, err)
		}
		if !strings.Contains(string(content), `"NewNB"`) {
			t.Fatalf("frontmatter in %s should contain notebook:\"NewNB\": %s", c.relPath, content)
		}
	}
}

func TestRenameSection_RollsBackPostRenameWriteFailureAndRetries(t *testing.T) {
	app := newTestApp(t)
	seedRenamePage(t, app, "TestNB", "Old/Child", "Page1")
	if err := app.SetFavoritePage("TestNB", "Old/Child", "Page1", true); err != nil {
		t.Fatalf("SetFavoritePage: %v", err)
	}

	failed := false
	app.renameHooks = &renameHooks{writeFileAtomic: func(path string, content []byte) error {
		if !failed && strings.Contains(path, filepath.Join("TestNB", "New")) {
			failed = true
			return errors.New("injected post-rename section write failure")
		}
		return parser.WriteFileAtomic(path, content)
	}}
	err := app.RenameSection("TestNB", "Old", "New")
	if err == nil || !failed {
		t.Fatalf("RenameSection should fail after injected post-rename write failure: err=%v failed=%v", err, failed)
	}
	oldPath := filepath.Join(app.vaultPath, "TestNB", "Old", "Child", "Page1.md")
	newPath := filepath.Join(app.vaultPath, "TestNB", "New", "Child", "Page1.md")
	if _, statErr := os.Stat(oldPath); statErr != nil {
		t.Fatalf("rollback should restore old section path: %v", statErr)
	}
	if _, statErr := os.Stat(newPath); !os.IsNotExist(statErr) {
		t.Fatalf("rollback should remove new section path, got %v", statErr)
	}
	assertRenamePageIndexed(t, app, "TestNB", "Old/Child", "Page1")
	prefs, prefErr := app.GetNavigationPreferences()
	if prefErr != nil || len(prefs.Favorites) != 1 || prefs.Favorites[0].Section != "Old/Child" {
		t.Fatalf("failed rename must preserve navigation config: prefs=%+v err=%v", prefs, prefErr)
	}

	app.renameHooks = nil
	if err := app.RenameSection("TestNB", "Old", "New"); err != nil {
		t.Fatalf("retry RenameSection: %v", err)
	}
	assertRenamePageIndexed(t, app, "TestNB", "New/Child", "Page1")
}

func TestRenameSection_RollsBackPostRenameIndexFailureAndRetries(t *testing.T) {
	app := newTestApp(t)
	seedRenamePage(t, app, "TestNB", "Old", "Page1")
	failed := false
	app.renameHooks = &renameHooks{indexFile: func(a *App, source, notebook, section, page string, blocks []parser.ParsedBlock, tags []string, warnings ...string) error {
		if !failed {
			failed = true
			return errors.New("injected post-rename section index failure")
		}
		return a.indexFile(source, notebook, section, page, blocks, tags, warnings...)
	}}
	err := app.RenameSection("TestNB", "Old", "New")
	if err == nil || !failed {
		t.Fatalf("RenameSection should fail after injected index failure: err=%v failed=%v", err, failed)
	}
	if _, statErr := os.Stat(filepath.Join(app.vaultPath, "TestNB", "Old", "Page1.md")); statErr != nil {
		t.Fatalf("rollback should restore old section after index failure: %v", statErr)
	}
	assertRenamePageIndexed(t, app, "TestNB", "Old", "Page1")

	app.renameHooks = nil
	if err := app.RenameSection("TestNB", "Old", "New"); err != nil {
		t.Fatalf("retry RenameSection: %v", err)
	}
	assertRenamePageIndexed(t, app, "TestNB", "New", "Page1")
}

func TestRenameSection_RollsBackPostRenameConfigFailure(t *testing.T) {
	app := newTestApp(t)
	seedRenamePage(t, app, "TestNB", "Old", "Page1")
	if err := app.SetFavoritePage("TestNB", "Old", "Page1", true); err != nil {
		t.Fatalf("SetFavoritePage: %v", err)
	}
	app.renameHooks = &renameHooks{reconcileSection: func(a *App, notebook, oldPath, newPath string, remove bool) error {
		if err := a.reconcileNavigationSection(notebook, oldPath, newPath, remove); err != nil {
			return err
		}
		return errors.New("injected post-rename section config failure")
	}}
	if err := app.RenameSection("TestNB", "Old", "New"); err == nil {
		t.Fatal("RenameSection should fail after injected config reconciliation failure")
	}
	if _, statErr := os.Stat(filepath.Join(app.vaultPath, "TestNB", "Old", "Page1.md")); statErr != nil {
		t.Fatalf("rollback should restore old section after config failure: %v", statErr)
	}
	assertRenamePageIndexed(t, app, "TestNB", "Old", "Page1")
	prefs, err := app.GetNavigationPreferences()
	if err != nil || len(prefs.Favorites) != 1 || prefs.Favorites[0].Section != "Old" {
		t.Fatalf("rollback should restore old section config: prefs=%+v err=%v", prefs, err)
	}
}

func TestRenameNotebook_RollsBackPostRenameWriteFailureAndRetries(t *testing.T) {
	app := newTestApp(t)
	seedRenamePage(t, app, "OldNB", "Nested/Child", "Page1")
	if err := app.SetFavoritePage("OldNB", "Nested/Child", "Page1", true); err != nil {
		t.Fatalf("SetFavoritePage: %v", err)
	}

	failed := false
	app.renameHooks = &renameHooks{writeFileAtomic: func(path string, content []byte) error {
		if !failed && strings.Contains(path, filepath.Join("NewNB", "Nested")) {
			failed = true
			return errors.New("injected post-rename notebook write failure")
		}
		return parser.WriteFileAtomic(path, content)
	}}
	err := app.RenameNotebook("OldNB", "NewNB")
	if err == nil || !failed {
		t.Fatalf("RenameNotebook should fail after injected post-rename write failure: err=%v failed=%v", err, failed)
	}
	oldPath := filepath.Join(app.vaultPath, "OldNB", "Nested", "Child", "Page1.md")
	newPath := filepath.Join(app.vaultPath, "NewNB", "Nested", "Child", "Page1.md")
	if _, statErr := os.Stat(oldPath); statErr != nil {
		t.Fatalf("rollback should restore old notebook path: %v", statErr)
	}
	if _, statErr := os.Stat(newPath); !os.IsNotExist(statErr) {
		t.Fatalf("rollback should remove new notebook path, got %v", statErr)
	}
	assertRenamePageIndexed(t, app, "OldNB", "Nested/Child", "Page1")
	prefs, prefErr := app.GetNavigationPreferences()
	if prefErr != nil || len(prefs.Favorites) != 1 || prefs.Favorites[0].Notebook != "OldNB" {
		t.Fatalf("failed rename must preserve notebook config: prefs=%+v err=%v", prefs, prefErr)
	}

	app.renameHooks = nil
	if err := app.RenameNotebook("OldNB", "NewNB"); err != nil {
		t.Fatalf("retry RenameNotebook: %v", err)
	}
	assertRenamePageIndexed(t, app, "NewNB", "Nested/Child", "Page1")
}

func TestRenameNotebook_RollsBackPostRenameIndexFailureAndRetries(t *testing.T) {
	app := newTestApp(t)
	seedRenamePage(t, app, "OldNB", "Nested", "Page1")
	failed := false
	app.renameHooks = &renameHooks{indexFile: func(a *App, source, notebook, section, page string, blocks []parser.ParsedBlock, tags []string, warnings ...string) error {
		if !failed {
			failed = true
			return errors.New("injected post-rename notebook index failure")
		}
		return a.indexFile(source, notebook, section, page, blocks, tags, warnings...)
	}}
	err := app.RenameNotebook("OldNB", "NewNB")
	if err == nil || !failed {
		t.Fatalf("RenameNotebook should fail after injected index failure: err=%v failed=%v", err, failed)
	}
	if _, statErr := os.Stat(filepath.Join(app.vaultPath, "OldNB", "Nested", "Page1.md")); statErr != nil {
		t.Fatalf("rollback should restore old notebook after index failure: %v", statErr)
	}
	assertRenamePageIndexed(t, app, "OldNB", "Nested", "Page1")

	app.renameHooks = nil
	if err := app.RenameNotebook("OldNB", "NewNB"); err != nil {
		t.Fatalf("retry RenameNotebook: %v", err)
	}
	assertRenamePageIndexed(t, app, "NewNB", "Nested", "Page1")
}

func TestRenameNotebook_RollsBackPostRenameConfigFailure(t *testing.T) {
	app := newTestApp(t)
	seedRenamePage(t, app, "OldNB", "Nested", "Page1")
	if err := app.SetFavoritePage("OldNB", "Nested", "Page1", true); err != nil {
		t.Fatalf("SetFavoritePage: %v", err)
	}
	app.renameHooks = &renameHooks{reconcileNotebook: func(a *App, oldName, newName string, remove bool) error {
		if err := a.reconcileNavigationNotebook(oldName, newName, remove); err != nil {
			return err
		}
		return errors.New("injected post-rename notebook config failure")
	}}
	if err := app.RenameNotebook("OldNB", "NewNB"); err == nil {
		t.Fatal("RenameNotebook should fail after injected config reconciliation failure")
	}
	if _, statErr := os.Stat(filepath.Join(app.vaultPath, "OldNB", "Nested", "Page1.md")); statErr != nil {
		t.Fatalf("rollback should restore old notebook after config failure: %v", statErr)
	}
	assertRenamePageIndexed(t, app, "OldNB", "Nested", "Page1")
	prefs, err := app.GetNavigationPreferences()
	if err != nil || len(prefs.Favorites) != 1 || prefs.Favorites[0].Notebook != "OldNB" {
		t.Fatalf("rollback should restore old notebook config: prefs=%+v err=%v", prefs, err)
	}
}

func TestDeleteNotebook_TrashesAll(t *testing.T) {
	app := newTestApp(t)

	if _, err := app.CreatePage("DoomNB", "", "P1", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage P1: %v", err)
	}
	if _, err := app.CreatePage("DoomNB", "Sub", "P2", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage P2: %v", err)
	}

	nbPath := filepath.Join(app.vaultPath, "DoomNB")
	if _, err := os.Stat(nbPath); err != nil {
		t.Fatalf("notebook should exist: %v", err)
	}

	if err := app.DeleteNotebook("DoomNB"); err != nil {
		t.Fatalf("DeleteNotebook: %v", err)
	}

	// Notebook folder should be gone from vault root.
	if _, err := os.Stat(nbPath); !os.IsNotExist(err) {
		t.Fatalf("notebook should not exist after delete")
	}

	// Notebook content should be in trash.
	trashDir := filepath.Join(app.vaultPath, ".system", "trash")
	entries, err := os.ReadDir(trashDir)
	if err != nil {
		t.Fatalf("trash dir should exist: %v", err)
	}
	found := false
	for _, e := range entries {
		trashNB := filepath.Join(trashDir, e.Name(), "DoomNB")
		if _, err := os.Stat(trashNB); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("notebook subtree should exist under .system/trash/<ts>/DoomNB/")
	}
}

// --- Per-block write-intent lock test (#64) ---

func TestLockBlocksWrite_NoDeadlock(t *testing.T) {
	app := newTestApp(t)

	// Create a page with multiple blocks.
	if _, err := app.CreatePage("TestNB", "", "LockTest", "2026-01-01"); err != nil {
		t.Fatalf("CreatePage: %v", err)
	}

	// Concurrent SaveFileBlocks calls should not deadlock.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// LockBlocksWrite with overlapping sets should not deadlock
			// because acquisition is sorted.
			app.coordinator.LockBlocksWrite([]string{"a-1", "b-2", "c-3"}, func() {})
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		app.coordinator.LockBlockWrite("b-2", func() {})
	}()
	wg.Wait()
}

// --- Open tabs IPC tests (#142) ---

// writePageOnDisk creates a minimal page file so ListNavigation surfaces it.
// The page carries standard frontmatter matching the parser's expectations.
func writePageOnDisk(t *testing.T, vaultPath, notebook, section, page string) {
	t.Helper()
	var dir string
	if section == "" {
		dir = filepath.Join(vaultPath, notebook)
	} else {
		dir = filepath.Join(vaultPath, notebook, section)
	}
	content := "---\nnotebook: " + notebook + "\nsection: \"" + section + "\"\npage: " + page + "\ndate: 2026-06-15\ntags: []\n---\n# " + page + "\n"
	writeFile(t, filepath.Join(dir, page+".md"), content)
}

func TestGetSetOpenTabs_RoundTrip(t *testing.T) {
	app := newTestApp(t)

	// Create pages on disk so the nav tree has valid targets.
	writePageOnDisk(t, app.vaultPath, "Work", "Projects", "Site")
	writePageOnDisk(t, app.vaultPath, "Work", "", "Top")
	writePageOnDisk(t, app.vaultPath, "Personal", "Journal", "Daily")

	tabs := []config.TabRef{
		{Notebook: "Work", Section: "Projects", Page: "Site"},
		{Notebook: "Work", Section: "", Page: "Top"},
	}
	active := &config.TabRef{Notebook: "Work", Section: "Projects", Page: "Site"}

	if err := app.SetOpenTabs(tabs, active); err != nil {
		t.Fatalf("SetOpenTabs: %v", err)
	}

	result, err := app.GetOpenTabs()
	if err != nil {
		t.Fatalf("GetOpenTabs: %v", err)
	}
	if len(result.OpenTabs) != 2 {
		t.Fatalf("expected 2 tabs, got %d: %+v", len(result.OpenTabs), result.OpenTabs)
	}
	// Both tabs should survive (both pages exist on disk).
	pages := map[string]bool{}
	for _, tab := range result.OpenTabs {
		pages[tab.Page] = true
	}
	if !pages["Site"] || !pages["Top"] {
		t.Errorf("expected Site + Top tabs, got %v", pages)
	}
	if result.ActiveTab == nil || result.ActiveTab.Page != "Site" {
		t.Errorf("active tab: got %+v, want Site", result.ActiveTab)
	}
}

func TestGetOpenTabs_PruneStaleTabs(t *testing.T) {
	app := newTestApp(t)

	// Create two pages.
	writePageOnDisk(t, app.vaultPath, "Work", "Projects", "KeepMe")
	writePageOnDisk(t, app.vaultPath, "Work", "Projects", "DeleteMe")

	// Persist tabs for both.
	tabs := []config.TabRef{
		{Notebook: "Work", Section: "Projects", Page: "KeepMe"},
		{Notebook: "Work", Section: "Projects", Page: "DeleteMe"},
	}
	active := &config.TabRef{Notebook: "Work", Section: "Projects", Page: "DeleteMe"}
	if err := app.SetOpenTabs(tabs, active); err != nil {
		t.Fatalf("SetOpenTabs: %v", err)
	}

	// Delete the "DeleteMe" page from disk.
	os.Remove(filepath.Join(app.vaultPath, "Work", "Projects", "DeleteMe.md"))

	// GetOpenTabs should prune the stale tab AND clear the stale active.
	result, err := app.GetOpenTabs()
	if err != nil {
		t.Fatalf("GetOpenTabs: %v", err)
	}
	if len(result.OpenTabs) != 1 || result.OpenTabs[0].Page != "KeepMe" {
		t.Errorf("expected only KeepMe tab after prune, got %+v", result.OpenTabs)
	}
	if result.ActiveTab != nil {
		t.Errorf("expected nil active tab (stale active pruned), got %+v", *result.ActiveTab)
	}
}

func TestGetOpenTabs_PruneMalformedEntries(t *testing.T) {
	app := newTestApp(t)

	writePageOnDisk(t, app.vaultPath, "Work", "", "Valid")

	// A malformed entry with an empty Page should be dropped silently.
	tabs := []config.TabRef{
		{Notebook: "Work", Section: "", Page: "Valid"},
		{Notebook: "Work", Section: "", Page: ""}, // malformed
	}
	if err := app.SetOpenTabs(tabs, nil); err != nil {
		t.Fatalf("SetOpenTabs: %v", err)
	}

	result, err := app.GetOpenTabs()
	if err != nil {
		t.Fatalf("GetOpenTabs: %v", err)
	}
	if len(result.OpenTabs) != 1 || result.OpenTabs[0].Page != "Valid" {
		t.Errorf("expected only Valid tab (malformed pruned), got %+v", result.OpenTabs)
	}
}

func TestSetOpenTabs_AtomicWrite(t *testing.T) {
	app := newTestApp(t)
	tabs := []config.TabRef{{Notebook: "Work", Section: "", Page: "Page1"}}
	if err := app.SetOpenTabs(tabs, nil); err != nil {
		t.Fatalf("SetOpenTabs: %v", err)
	}
	// The .system directory should contain exactly config.yaml (no leftover
	// temp files from the atomic write).
	entries, err := os.ReadDir(filepath.Join(app.vaultPath, ".system"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		// Allow config.yaml, themes/, plugins/, templates/ dirs; reject any
		// .tmp file (leftover from a failed atomic write).
		if strings.HasSuffix(name, ".tmp") {
			t.Errorf("leftover temp file in .system: %s", name)
		}
	}
}

func TestSetOpenTabs_NilBecomesEmptySlice(t *testing.T) {
	app := newTestApp(t)
	// Passing nil for openTabs should persist as an empty slice, not null
	// (so the frontend JSON layer never sees null).
	if err := app.SetOpenTabs(nil, nil); err != nil {
		t.Fatalf("SetOpenTabs(nil): %v", err)
	}
	cfg, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.UI.OpenTabs == nil || len(cfg.UI.OpenTabs) != 0 {
		t.Errorf("expected non-nil empty slice, got %v", cfg.UI.OpenTabs)
	}
}

func TestGetOpenTabs_EmptyVault(t *testing.T) {
	app := newTestApp(t)
	// No tabs persisted → empty slice, nil active, no error.
	result, err := app.GetOpenTabs()
	if err != nil {
		t.Fatalf("GetOpenTabs on empty vault: %v", err)
	}
	if len(result.OpenTabs) != 0 {
		t.Errorf("expected empty tab slice, got %d", len(result.OpenTabs))
	}
	if result.ActiveTab != nil {
		t.Errorf("expected nil active, got %+v", *result.ActiveTab)
	}
}

// TestSetOpenTabs_SelfWriteSuppressed verifies that SetOpenTabs calls
// RegisterSelfWrite so the config watcher does NOT fire a config:changed
// reload for Silt's own write (#142). This is the PLAN's promised
// self-write suppression test, exercised end-to-end via a real ConfigWatcher.
func TestSetOpenTabs_SelfWriteSuppressed(t *testing.T) {
	app := newTestApp(t)

	// Set up a real config watcher so RegisterSelfWrite is meaningful.
	changed := make(chan config.SystemConfig, 4)
	cw, err := config.NewConfigWatcher(app.vaultPath, func(c config.SystemConfig) {
		changed <- c
	}, nil)
	if err != nil {
		t.Fatalf("NewConfigWatcher: %v", err)
	}
	defer cw.Close()
	cw.Start()
	app.configWatcher = cw
	defer func() { app.configWatcher = nil }()

	// Give the watcher time to settle.
	time.Sleep(150 * time.Millisecond)

	tabs := []config.TabRef{{Notebook: "Work", Section: "", Page: "Page1"}}
	if err := app.SetOpenTabs(tabs, nil); err != nil {
		t.Fatalf("SetOpenTabs: %v", err)
	}

	// The watcher should NOT fire within the self-write cooldown window.
	select {
	case <-changed:
		t.Fatalf("self-write should be suppressed, but config:changed fired")
	case <-time.After(config.SelfWriteSuppressionTimeout):
		// expected: no reload within the cooldown window
	}
}

// TestAppendDismissedTip_AtomicWrite confirms the atomic tip-dismiss writer
// (#197) leaves no leftover temp files in .system after a successful write.
func TestAppendDismissedTip_AtomicWrite(t *testing.T) {
	app := newTestApp(t)
	if err := app.AppendDismissedTip("formatting_tip_v1"); err != nil {
		t.Fatalf("AppendDismissedTip: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(app.vaultPath, ".system"))
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("leftover temp file in .system: %s", e.Name())
		}
	}
}

// TestAppendDismissedTip_SelfWriteSuppressed verifies that AppendDismissedTip
// calls RegisterSelfWrite so the config watcher does NOT fire a
// config:changed reload for Silt's own write (#197), mirroring the
// SetOpenTabs contract.
func TestAppendDismissedTip_SelfWriteSuppressed(t *testing.T) {
	app := newTestApp(t)

	changed := make(chan config.SystemConfig, 4)
	cw, err := config.NewConfigWatcher(app.vaultPath, func(c config.SystemConfig) {
		changed <- c
	}, nil)
	if err != nil {
		t.Fatalf("NewConfigWatcher: %v", err)
	}
	defer cw.Close()
	cw.Start()
	app.configWatcher = cw
	defer func() { app.configWatcher = nil }()

	time.Sleep(150 * time.Millisecond)

	if err := app.AppendDismissedTip("formatting_tip_v1"); err != nil {
		t.Fatalf("AppendDismissedTip: %v", err)
	}

	select {
	case <-changed:
		t.Fatalf("self-write should be suppressed, but config:changed fired")
	case <-time.After(config.SelfWriteSuppressionTimeout):
		// expected: no reload within the cooldown window
	}
}

// TestSaveConfigTracked_FailedSaveClearsSuppression verifies the fix for #487:
// when a config save fails, saveConfigTracked must clear the self-write
// suppression window (UnregisterSelfWrite) so a legitimate external edit
// landing right after the failed save is NOT silently dropped. A failed save
// that left the window armed would suppress the next real external reload.
func TestSaveConfigTracked_FailedSaveClearsSuppression(t *testing.T) {
	app := newTestApp(t)

	changed := make(chan config.SystemConfig, 4)
	cw, err := config.NewConfigWatcher(app.vaultPath, func(c config.SystemConfig) {
		changed <- c
	}, nil)
	if err != nil {
		t.Fatalf("NewConfigWatcher: %v", err)
	}
	defer cw.Close()
	cw.Start()
	app.configWatcher = cw
	defer func() { app.configWatcher = nil }()

	time.Sleep(150 * time.Millisecond)

	// Force config.Save to fail: replace config.yaml with a directory so the
	// atomic writer's final os.Rename (file over a directory) fails.
	cfgPath := config.ConfigPath(app.vaultPath)
	if err := os.Remove(cfgPath); err != nil {
		t.Fatalf("remove config.yaml: %v", err)
	}
	if err := os.Mkdir(cfgPath, 0o700); err != nil {
		t.Fatalf("mkdir config.yaml: %v", err)
	}
	// Let the watcher absorb the directory-creation event before the external
	// edit so it cannot mask the assertion below.
	time.Sleep(config.SelfWriteSuppressionTimeout)

	// The setter routes through saveConfigTracked; Save must fail (config.yaml
	// is a directory), and the failed path must clear the suppression window.
	if err := app.SetShowFormatToolbar(true); err == nil {
		t.Fatalf("SetShowFormatToolbar should have failed (config.yaml is a directory)")
	}

	// Restore a writable config.yaml and simulate a legitimate external edit.
	if err := os.RemoveAll(cfgPath); err != nil {
		t.Fatalf("remove config.yaml dir: %v", err)
	}
	ext := config.Defaults()
	ext.Editor.FontSizePx = 42
	if err := config.Save(app.vaultPath, ext); err != nil {
		t.Fatalf("external write: %v", err)
	}

	// The external edit must be detected — the failed save cleared the window.
	// If the bug were present (window left armed), this would time out.
	select {
	case <-changed:
		// expected: external edit detected because the failed save cleared the
		// self-write suppression window.
	case <-time.After(config.SelfWriteSuppressionTimeout):
		t.Fatalf("external edit was suppressed — failed save left the self-write window armed (#487)")
	}
}

// --- RecordTagUsage ---

func TestRecordTagUsage_PromotesToFrontAndDedupes(t *testing.T) {
	app := newTestApp(t)

	// Seed three tags.
	for _, tag := range []string{"work/project", "personal/journal", "ideas"} {
		if err := app.RecordTagUsage(tag); err != nil {
			t.Fatalf("RecordTagUsage(%q): %v", tag, err)
		}
	}
	prefs, err := app.GetNavigationPreferences()
	if err != nil {
		t.Fatalf("GetNavigationPreferences: %v", err)
	}
	if !reflect.DeepEqual(prefs.RecentPages, []config.RecentPage(nil)) {
		t.Fatalf("recent_pages should be empty (no page mutations), got %+v", prefs.RecentPages)
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	want := []string{"ideas", "personal/journal", "work/project"}
	if !reflect.DeepEqual(loaded.UI.RecentTags, want) {
		t.Fatalf("recent_tags after 3 inserts:\n got  %v\n want %v", loaded.UI.RecentTags, want)
	}

	// Re-record an existing tag (case-insensitive) → moves to front.
	if err := app.RecordTagUsage("Work/Project"); err != nil {
		t.Fatalf("RecordTagUsage(Work/Project): %v", err)
	}
	loaded, err = config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	want = []string{"Work/Project", "ideas", "personal/journal"}
	if !reflect.DeepEqual(loaded.UI.RecentTags, want) {
		t.Fatalf("recent_tags after re-record:\n got  %v\n want %v", loaded.UI.RecentTags, want)
	}
}

func TestRecordTagUsage_CapsAtMaxRecentTags(t *testing.T) {
	app := newTestApp(t)

	for i := 0; i < config.MaxRecentTags+5; i++ {
		tag := fmt.Sprintf("tag-%02d", i)
		if err := app.RecordTagUsage(tag); err != nil {
			t.Fatalf("RecordTagUsage(%q): %v", tag, err)
		}
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.UI.RecentTags) != config.MaxRecentTags {
		t.Fatalf("recent_tags length: got %d, want %d", len(loaded.UI.RecentTags), config.MaxRecentTags)
	}
	// Most recently inserted should be first (highest tag number).
	if loaded.UI.RecentTags[0] != fmt.Sprintf("tag-%02d", config.MaxRecentTags+4) {
		t.Errorf("most recent tag should be first, got %q", loaded.UI.RecentTags[0])
	}
}

func TestRecordTagUsage_IgnoresEmptyAndWhitespace(t *testing.T) {
	app := newTestApp(t)

	if err := app.RecordTagUsage(""); err != nil {
		t.Fatalf("empty tag should be no-op: %v", err)
	}
	if err := app.RecordTagUsage("   "); err != nil {
		t.Fatalf("whitespace tag should be no-op: %v", err)
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.RecentTags, []string{}) {
		t.Fatalf("recent_tags should stay empty after empty/whitespace inserts, got %v", loaded.UI.RecentTags)
	}
}

func TestRecordTagUsage_IdempotentWhenNoChange(t *testing.T) {
	app := newTestApp(t)

	// Insert the same tag twice without any change to other tags.
	if err := app.RecordTagUsage("alpha"); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := app.RecordTagUsage("alpha"); err != nil {
		t.Fatalf("second (same): %v", err)
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	// Re-inserting the same tag at the front produces an identical slice, so
	// mutateConfig's DeepEqual check skips the save.
	if !reflect.DeepEqual(loaded.UI.RecentTags, []string{"alpha"}) {
		t.Fatalf("recent_tags after idempotent insert:\n got  %v\n want [alpha]", loaded.UI.RecentTags)
	}
}

func TestRecordTagUsage_EmitsConfigChangedOnlyForPersistedChange(t *testing.T) {
	app := newTestApp(t)
	type tagEvent struct {
		name string
		tags []string
	}
	events := make(chan tagEvent, 4)
	app.eventEmit = func(name string, data ...any) {
		if len(data) != 1 {
			return
		}
		cfg, ok := data[0].(config.SystemConfig)
		if !ok {
			return
		}
		events <- tagEvent{name: name, tags: append([]string(nil), cfg.UI.RecentTags...)}
	}

	if err := app.RecordTagUsage("alpha"); err != nil {
		t.Fatalf("RecordTagUsage(alpha): %v", err)
	}
	select {
	case event := <-events:
		if event.name != "config:changed" {
			t.Fatalf("event name = %q, want config:changed", event.name)
		}
		if !reflect.DeepEqual(event.tags, []string{"alpha"}) {
			t.Fatalf("event recent_tags = %v, want [alpha]", event.tags)
		}
	case <-time.After(time.Second):
		t.Fatal("successful tag usage did not emit config:changed")
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load after event: %v", err)
	}
	if !reflect.DeepEqual(loaded.UI.RecentTags, []string{"alpha"}) {
		t.Fatalf("persisted recent_tags = %v, want [alpha]", loaded.UI.RecentTags)
	}

	if err := app.RecordTagUsage("alpha"); err != nil {
		t.Fatalf("RecordTagUsage(alpha) no-op: %v", err)
	}
	if err := app.RecordTagUsage("   "); err != nil {
		t.Fatalf("RecordTagUsage(whitespace) no-op: %v", err)
	}
	select {
	case event := <-events:
		t.Fatalf("no-op tag usage emitted %q", event.name)
	default:
	}

	// A case-change re-record produces an actual persisted change and must emit.
	if err := app.RecordTagUsage("ALPHA"); err != nil {
		t.Fatalf("RecordTagUsage(ALPHA): %v", err)
	}
	select {
	case event := <-events:
		if event.name != "config:changed" {
			t.Fatalf("event name = %q, want config:changed", event.name)
		}
		if !reflect.DeepEqual(event.tags, []string{"ALPHA"}) {
			t.Fatalf("event recent_tags = %v, want [ALPHA]", event.tags)
		}
	case <-time.After(time.Second):
		t.Fatal("case-change re-record should emit config:changed")
	}

	// A second distinct tag should also emit.
	if err := app.RecordTagUsage("beta"); err != nil {
		t.Fatalf("RecordTagUsage(beta): %v", err)
	}
	select {
	case event := <-events:
		if event.name != "config:changed" {
			t.Fatalf("event name = %q, want config:changed", event.name)
		}
		if !reflect.DeepEqual(event.tags, []string{"beta", "ALPHA"}) {
			t.Fatalf("event recent_tags = %v, want [beta, ALPHA]", event.tags)
		}
	case <-time.After(time.Second):
		t.Fatal("second distinct tag should emit config:changed")
	}
}

func TestRecordTagUsage_RejectsInvalidCharacters(t *testing.T) {
	app := newTestApp(t)

	invalid := []string{
		"has space",
		"has\ttab",
		"has\nnewline",
		"has\rnewline",
		"123startdigit",
		"/starts-slash",
		"-starts-hyphen",
		"_starts-underscore",
		"has!bang",
		"has@at",
		"has(paren",
		"has.dot",
		"has:colon",
		"a\x00null",
		"tag\x1Besc",
	}
	for _, tag := range invalid {
		err := app.RecordTagUsage(tag)
		if err == nil {
			t.Errorf("RecordTagUsage(%q): expected error, got nil", tag)
		}
	}
	// Config should be untouched — load and verify empty.
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.UI.RecentTags) != 0 {
		t.Errorf("recent_tags should be empty after all invalid inputs, got %v", loaded.UI.RecentTags)
	}
}

func TestRecordTagUsage_RejectsOversizedTag(t *testing.T) {
	app := newTestApp(t)

	oversized := strings.Repeat("a", config.MaxTagPathBytes+1)
	err := app.RecordTagUsage(oversized)
	if err == nil {
		t.Errorf("RecordTagUsage(%d-byte tag): expected error, got nil", len(oversized))
	}

	// Exactly at the limit should succeed.
	exact := strings.Repeat("a", config.MaxTagPathBytes)
	if err := app.RecordTagUsage(exact); err != nil {
		t.Fatalf("RecordTagUsage(%d-byte tag): unexpected error: %v", config.MaxTagPathBytes, err)
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.UI.RecentTags) != 1 || loaded.UI.RecentTags[0] != exact {
		t.Errorf("recent_tags after valid max-length tag: got %v", loaded.UI.RecentTags)
	}
}

func TestRecordTagUsage_AcceptsValidBoundaryInputs(t *testing.T) {
	app := newTestApp(t)

	valid := []string{
		"a",                               // single letter
		"Z",                               // single uppercase
		"work/project",                    // multi-segment with slash
		"work/project/milestone-one",      // multi-segment with hyphen
		"a_b_c",                           // underscores
		"X1",                              // letter then digit
		"work/project/milestone_one/v2-0", // complex path
	}
	for _, tag := range valid {
		if err := app.RecordTagUsage(tag); err != nil {
			t.Errorf("RecordTagUsage(%q): unexpected error: %v", tag, err)
		}
	}
	loaded, err := config.Load(app.vaultPath)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if len(loaded.UI.RecentTags) != len(valid) {
		t.Errorf("recent_tags length: got %d, want %d", len(loaded.UI.RecentTags), len(valid))
	}
	// Most recently inserted should be first.
	if loaded.UI.RecentTags[0] != valid[len(valid)-1] {
		t.Errorf("most recent tag = %q, want %q", loaded.UI.RecentTags[0], valid[len(valid)-1])
	}
}
