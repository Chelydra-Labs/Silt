package db

import (
	"testing"
)

func seedPageCore(t *testing.T, dm *DatabaseManager, notebook, section, page, typeID string) {
	t.Helper()
	if err := dm.IndexPageCore("vault", notebook, section, page, PageCoreFields{Type: typeID}); err != nil {
		t.Fatalf("IndexPageCore %s/%s/%s type=%q: %v", notebook, section, page, typeID, err)
	}
}

func locatorPages(locs []TypedPageLocator) []string {
	out := make([]string, len(locs))
	for i, loc := range locs {
		out[i] = loc.Page
	}
	return out
}

// TestListPageCoreTypeMatches_EmptyInput returns nothing for nil/empty/blank
// type refs even when page_core has rows (cold-path migrate "nothing to do").
func TestListPageCoreTypeMatches_EmptyInput(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Notes", "", "Dune", "book")

	for _, tc := range []struct {
		name      string
		typeIDs   []string
		typeNames []string
	}{
		{"nil both", nil, nil},
		{"empty both", []string{}, []string{}},
		{"blank ids", []string{"", "  "}, nil},
		{"blank names", nil, []string{"", "\t"}},
		{"blank mixed", []string{""}, []string{"  "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locs, err := dm.ListPageCoreTypeMatches(tc.typeIDs, tc.typeNames)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if len(locs) != 0 {
				t.Errorf("got %d locators %v, want 0", len(locs), locatorPages(locs))
			}
		})
	}
}

// TestListPageCoreTypeMatches_Dedup ensures duplicate ids/names across both
// slices do not double-count rows; each matching page appears once.
func TestListPageCoreTypeMatches_Dedup(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Notes", "", "Dune", "book")
	seedPageCore(t, dm, "Notes", "", "Hyperion", "book")

	locs, err := dm.ListPageCoreTypeMatches(
		[]string{"book", "book", "Book"},
		[]string{"book", "BOOK", "book"},
	)
	if err != nil {
		t.Fatalf("ListPageCoreTypeMatches: %v", err)
	}
	if len(locs) != 2 {
		t.Fatalf("got %d locators %v, want 2 (deduped input, unique pages)", len(locs), locatorPages(locs))
	}
	seen := map[string]int{}
	for _, loc := range locs {
		seen[loc.Page]++
	}
	for page, n := range seen {
		if n != 1 {
			t.Errorf("page %q appeared %d times, want 1", page, n)
		}
	}
}

// TestListPageCoreTypeMatches_CaseInsensitive matches page_core.type against
// typeIDs and typeNames case-insensitively.
func TestListPageCoreTypeMatches_CaseInsensitive(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Notes", "", "Dune", "Book")

	for _, tc := range []struct {
		name      string
		typeIDs   []string
		typeNames []string
	}{
		{"id lower", []string{"book"}, nil},
		{"id mixed", []string{"BoOk"}, nil},
		{"name upper", nil, []string{"BOOK"}},
		{"name lower", nil, []string{"book"}},
		{"both", []string{"book"}, []string{"BOOK"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			locs, err := dm.ListPageCoreTypeMatches(tc.typeIDs, tc.typeNames)
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			if len(locs) != 1 {
				t.Fatalf("got %d locators, want 1", len(locs))
			}
			if locs[0].Page != "Dune" || locs[0].TypeName != "Book" {
				t.Errorf("locator = %+v, want Dune/Book", locs[0])
			}
		})
	}
}

// TestListPageCoreTypeMatches_DisplayNameAndID covers id vs display-name
// refs finding pages whose stored type string is either form.
func TestListPageCoreTypeMatches_DisplayNameAndID(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Notes", "", "Dune", "Book")
	seedPageCore(t, dm, "Notes", "", "Foundation", "book")

	byName, err := dm.ListPageCoreTypeMatches(nil, []string{"Book"})
	if err != nil {
		t.Fatalf("by name: %v", err)
	}
	// Case-insensitive: both Book and book match "Book".
	if len(byName) != 2 {
		t.Fatalf("typeNames [Book]: got %d %v, want 2", len(byName), locatorPages(byName))
	}

	byID, err := dm.ListPageCoreTypeMatches([]string{"book"}, nil)
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if len(byID) != 2 {
		t.Fatalf("typeIDs [book]: got %d %v, want 2", len(byID), locatorPages(byID))
	}
}

// TestListPageCoreTypeMatches_FiltersEmptyType never returns page_core rows
// with type "".
func TestListPageCoreTypeMatches_FiltersEmptyType(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Notes", "", "Untyped", "")
	seedPageCore(t, dm, "Notes", "", "Dune", "book")

	// Even if a caller passes empty string among refs, empty-type rows stay out.
	locs, err := dm.ListPageCoreTypeMatches([]string{"book", ""}, []string{""})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(locs) != 1 || locs[0].Page != "Dune" {
		t.Errorf("got %v, want only Dune", locatorPages(locs))
	}
}

// TestListPageCoreTypeMatches_UnrelatedTypes excludes pages of other types.
func TestListPageCoreTypeMatches_UnrelatedTypes(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Notes", "", "Dune", "book")
	seedPageCore(t, dm, "Notes", "", "Standup", "meeting")
	seedPageCore(t, dm, "Notes", "Work", "Review", "meeting")

	locs, err := dm.ListPageCoreTypeMatches([]string{"book"}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(locs) != 1 {
		t.Fatalf("got %d %v, want 1", len(locs), locatorPages(locs))
	}
	if locs[0].Page != "Dune" || locs[0].TypeName != "book" {
		t.Errorf("locator = %+v, want Dune/book", locs[0])
	}
}

// TestListPageCoreTypeMatches_Ordered pins ORDER BY source, notebook, section, page.
func TestListPageCoreTypeMatches_Ordered(t *testing.T) {
	dm := newTestDB(t)
	seedPageCore(t, dm, "Zeta", "", "A", "book")
	seedPageCore(t, dm, "Alpha", "b", "P2", "book")
	seedPageCore(t, dm, "Alpha", "a", "P1", "book")
	seedPageCore(t, dm, "Alpha", "a", "P0", "book")

	locs, err := dm.ListPageCoreTypeMatches([]string{"book"}, nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(locs) != 4 {
		t.Fatalf("got %d, want 4", len(locs))
	}
	want := []TypedPageLocator{
		{Source: "vault", Notebook: "Alpha", Section: "a", Page: "P0", TypeName: "book"},
		{Source: "vault", Notebook: "Alpha", Section: "a", Page: "P1", TypeName: "book"},
		{Source: "vault", Notebook: "Alpha", Section: "b", Page: "P2", TypeName: "book"},
		{Source: "vault", Notebook: "Zeta", Section: "", Page: "A", TypeName: "book"},
	}
	for i := range want {
		got := locs[i]
		if got.Source != want[i].Source || got.Notebook != want[i].Notebook ||
			got.Section != want[i].Section || got.Page != want[i].Page {
			t.Errorf("loc[%d] = %s/%s/%s/%s, want %s/%s/%s/%s",
				i, got.Source, got.Notebook, got.Section, got.Page,
				want[i].Source, want[i].Notebook, want[i].Section, want[i].Page)
		}
	}
}
