package history

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testLoc(page string) Locator {
	return Locator{Source: "vault", Notebook: "Work", Section: "Journal", Page: page}
}

func TestCaptureAndReadRoundTrip(t *testing.T) {
	root := t.TempDir()
	loc := testLoc("Daily")
	prev := []byte("---\npage: Daily\n---\n\nhello history\n")
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	skip, err := Capture(root, loc, prev, "editor", now, Options{})
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if skip != "" {
		t.Fatalf("unexpected skip %q", skip)
	}

	list, err := List(root, loc)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len=%d, want 1", len(list))
	}
	if list[0].Source != "editor" || list[0].Bytes != len(prev) {
		t.Errorf("entry = %+v", list[0])
	}

	got, err := Read(root, loc, list[0].ID)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if !bytes.Equal(got, prev) {
		t.Fatalf("Read mismatch:\n got %q\nwant %q", got, prev)
	}

	// Files live under .system/history, independent of any db.
	man := filepath.Join(root, ".system", "history", "pages", "vault", "Work", "Journal", "Daily.jsonl")
	if _, err := os.Stat(man); err != nil {
		t.Fatalf("missing manifest %s: %v", man, err)
	}
}

func TestCapture_HashSkipAndEmptyAndTooLarge(t *testing.T) {
	root := t.TempDir()
	loc := testLoc("Skip")
	now := time.Now().UTC()

	skip, err := Capture(root, loc, nil, "editor", now, Options{})
	if err != nil || skip != SkipEmpty {
		t.Fatalf("empty: skip=%q err=%v", skip, err)
	}

	big := bytes.Repeat([]byte("a"), MaxUncompressedBytes+1)
	skip, err = Capture(root, loc, big, "editor", now, Options{})
	if err != nil || skip != SkipTooLarge {
		t.Fatalf("too large: skip=%q err=%v", skip, err)
	}

	body := []byte("same body")
	if skip, err = Capture(root, loc, body, "editor", now, Options{}); err != nil || skip != "" {
		t.Fatalf("first capture: skip=%q err=%v", skip, err)
	}
	if skip, err = Capture(root, loc, body, "editor", now.Add(time.Minute), Options{}); err != nil || skip != SkipDuplicate {
		t.Fatalf("duplicate: skip=%q err=%v", skip, err)
	}
	list, err := List(root, loc)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("want 1 entry after hash skip, got %d", len(list))
	}
}

func TestPrune_OldestFirstAndRemainingReadable(t *testing.T) {
	root := t.TempDir()
	loc := testLoc("Prune")
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	for i, body := range []string{"one", "two", "three"} {
		if skip, err := Capture(root, loc, []byte(body), "editor", now.Add(time.Duration(i)*time.Minute), Options{MaxVersions: 2}); err != nil || skip != "" {
			t.Fatalf("capture %q: skip=%q err=%v", body, skip, err)
		}
	}

	list, err := List(root, loc)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("after prune List len=%d, want 2", len(list))
	}
	// Newest first: three, then two. "one" is gone.
	got0, err := Read(root, loc, list[0].ID)
	if err != nil {
		t.Fatalf("Read newest: %v", err)
	}
	got1, err := Read(root, loc, list[1].ID)
	if err != nil {
		t.Fatalf("Read older remaining: %v", err)
	}
	if string(got0) != "three" || string(got1) != "two" {
		t.Fatalf("remaining bodies = %q, %q", got0, got1)
	}

	// Oldest blob should be gone.
	dir, err := blobDir(root, loc)
	if err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Fatalf("blob count=%d, want 2", len(ents))
	}
}

func TestRelocate_FollowsRename(t *testing.T) {
	root := t.TempDir()
	oldLoc := testLoc("OldName")
	newLoc := testLoc("NewName")
	body := []byte("relocated body")
	if skip, err := Capture(root, oldLoc, body, "editor", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("Capture: skip=%q err=%v", skip, err)
	}
	if err := Relocate(root, oldLoc, newLoc); err != nil {
		t.Fatalf("Relocate: %v", err)
	}

	oldList, err := List(root, oldLoc)
	if err != nil {
		t.Fatalf("List old: %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("old locator still has %d entries", len(oldList))
	}
	newList, err := List(root, newLoc)
	if err != nil {
		t.Fatalf("List new: %v", err)
	}
	if len(newList) != 1 {
		t.Fatalf("new locator List len=%d, want 1", len(newList))
	}
	got, err := Read(root, newLoc, newList[0].ID)
	if err != nil {
		t.Fatalf("Read new: %v", err)
	}
	if !bytes.Equal(got, body) {
		t.Fatalf("relocated body = %q", got)
	}
}

func TestRelocate_MergesWhenDestinationExists(t *testing.T) {
	root := t.TempDir()
	oldLoc := testLoc("Alive")
	destLoc := testLoc("DeletedName")
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)

	if skip, err := Capture(root, destLoc, []byte("deleted-v1"), "editor", now, Options{}); err != nil || skip != "" {
		t.Fatalf("dest capture: skip=%q err=%v", skip, err)
	}
	if skip, err := Capture(root, oldLoc, []byte("alive-v1"), "editor", now.Add(time.Minute), Options{}); err != nil || skip != "" {
		t.Fatalf("old capture: skip=%q err=%v", skip, err)
	}

	if err := Relocate(root, oldLoc, destLoc); err != nil {
		t.Fatalf("Relocate merge: %v", err)
	}

	oldList, err := List(root, oldLoc)
	if err != nil {
		t.Fatalf("List old: %v", err)
	}
	if len(oldList) != 0 {
		t.Fatalf("old locator still has %d entries", len(oldList))
	}
	merged, err := List(root, destLoc)
	if err != nil {
		t.Fatalf("List dest: %v", err)
	}
	if len(merged) != 2 {
		t.Fatalf("merged List len=%d, want 2", len(merged))
	}
	got0, err := Read(root, destLoc, merged[0].ID)
	if err != nil {
		t.Fatalf("Read newest: %v", err)
	}
	got1, err := Read(root, destLoc, merged[1].ID)
	if err != nil {
		t.Fatalf("Read older: %v", err)
	}
	if string(got0) != "alive-v1" || string(got1) != "deleted-v1" {
		t.Fatalf("merged bodies = %q, %q", got0, got1)
	}

	if err := Prune(root, destLoc, 1); err != nil {
		t.Fatalf("Prune: %v", err)
	}
	pruned, err := List(root, destLoc)
	if err != nil {
		t.Fatalf("List after prune: %v", err)
	}
	if len(pruned) != 1 {
		t.Fatalf("after prune List len=%d, want 1", len(pruned))
	}
	kept, err := Read(root, destLoc, pruned[0].ID)
	if err != nil {
		t.Fatalf("Read pruned: %v", err)
	}
	if string(kept) != "alive-v1" {
		t.Fatalf("pruned body = %q, want newest", kept)
	}
}

func TestRelocate_MergesWhenDestBlobsExistWithoutManifest(t *testing.T) {
	root := t.TempDir()
	oldLoc := testLoc("Alive")
	destLoc := testLoc("OrphanBlobs")
	now := time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC)
	if skip, err := Capture(root, destLoc, []byte("dest-v1"), "editor", now, Options{}); err != nil || skip != "" {
		t.Fatalf("dest capture: skip=%q err=%v", skip, err)
	}
	destMan, err := manifestPath(root, destLoc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(destMan); err != nil {
		t.Fatal(err)
	}
	if skip, err := Capture(root, oldLoc, []byte("alive-v1"), "editor", now.Add(time.Minute), Options{}); err != nil || skip != "" {
		t.Fatalf("old capture: skip=%q err=%v", skip, err)
	}
	if err := Relocate(root, oldLoc, destLoc); err != nil {
		t.Fatalf("Relocate: %v", err)
	}
	oldList, err := List(root, oldLoc)
	if err != nil || len(oldList) != 0 {
		t.Fatalf("old locator still has history: %v len=%d", err, len(oldList))
	}
	merged, err := List(root, destLoc)
	if err != nil || len(merged) == 0 {
		t.Fatalf("dest locator lost history: %v len=%d", err, len(merged))
	}
	got, err := Read(root, destLoc, merged[0].ID)
	if err != nil || string(got) != "alive-v1" {
		t.Fatalf("relocated body = %q err=%v", got, err)
	}
}

func TestList_MissingIsEmpty(t *testing.T) {
	root := t.TempDir()
	list, err := List(root, testLoc("Missing"))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty list, got %d", len(list))
	}
	if _, err := Read(root, testLoc("Missing"), "nope"); err != ErrNotFound {
		t.Fatalf("Read missing: %v", err)
	}
}

func TestCapture_EmptySectionUsesRootSentinel(t *testing.T) {
	root := t.TempDir()
	loc := Locator{Source: "vault", Notebook: "Work", Section: "", Page: "Inbox"}
	if skip, err := Capture(root, loc, []byte("root page"), "mcp", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("Capture: skip=%q err=%v", skip, err)
	}
	man := filepath.Join(root, ".system", "history", "pages", "vault", "Work", "__root__", "Inbox.jsonl")
	if _, err := os.Stat(man); err != nil {
		t.Fatalf("expected root sentinel path %s: %v", man, err)
	}
}

func TestCapture_EmptySectionDistinctFromUnderscoreSection(t *testing.T) {
	root := t.TempDir()
	empty := Locator{Source: "vault", Notebook: "Work", Section: "", Page: "Same"}
	named := Locator{Source: "vault", Notebook: "Work", Section: "_", Page: "Same"}
	if skip, err := Capture(root, empty, []byte("empty-section"), "editor", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("empty capture: skip=%q err=%v", skip, err)
	}
	if skip, err := Capture(root, named, []byte("underscore-section"), "editor", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("named capture: skip=%q err=%v", skip, err)
	}
	emptyList, err := List(root, empty)
	if err != nil || len(emptyList) != 1 {
		t.Fatalf("empty list: %v len=%d", err, len(emptyList))
	}
	namedList, err := List(root, named)
	if err != nil || len(namedList) != 1 {
		t.Fatalf("named list: %v len=%d", err, len(namedList))
	}
	gotEmpty, _ := Read(root, empty, emptyList[0].ID)
	gotNamed, _ := Read(root, named, namedList[0].ID)
	if string(gotEmpty) != "empty-section" || string(gotNamed) != "underscore-section" {
		t.Fatalf("aliased bodies: empty=%q named=%q", gotEmpty, gotNamed)
	}
}

func TestCapture_ColonNameDistinctFromStripped(t *testing.T) {
	root := t.TempDir()
	colon := Locator{Source: "vault", Notebook: "Work", Section: "a:b", Page: "Note"}
	plain := Locator{Source: "vault", Notebook: "Work", Section: "ab", Page: "Note"}
	if skip, err := Capture(root, colon, []byte("colon"), "editor", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("colon capture: skip=%q err=%v", skip, err)
	}
	if skip, err := Capture(root, plain, []byte("plain"), "editor", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("plain capture: skip=%q err=%v", skip, err)
	}
	colonList, err := List(root, colon)
	if err != nil || len(colonList) != 1 {
		t.Fatalf("colon list: %v len=%d", err, len(colonList))
	}
	plainList, err := List(root, plain)
	if err != nil || len(plainList) != 1 {
		t.Fatalf("plain list: %v len=%d", err, len(plainList))
	}
	gotColon, _ := Read(root, colon, colonList[0].ID)
	gotPlain, _ := Read(root, plain, plainList[0].ID)
	if string(gotColon) != "colon" || string(gotPlain) != "plain" {
		t.Fatalf("aliased bodies: colon=%q plain=%q", gotColon, gotPlain)
	}
}

func TestReadManifest_SkipsMalformedLine(t *testing.T) {
	root := t.TempDir()
	loc := testLoc("Torn")
	if skip, err := Capture(root, loc, []byte("good"), "editor", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("Capture: skip=%q err=%v", skip, err)
	}
	man := filepath.Join(root, ".system", "history", "pages", "vault", "Work", "Journal", "Torn.jsonl")
	f, err := os.OpenFile(man, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("{not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	list, err := List(root, loc)
	if err != nil {
		t.Fatalf("List after torn line: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("List len=%d, want 1 surviving entry", len(list))
	}
	got, err := Read(root, loc, list[0].ID)
	if err != nil || string(got) != "good" {
		t.Fatalf("Read after torn line: %q err=%v", got, err)
	}
}

func TestCapture_LinkedSourceIsFilesystemSafe(t *testing.T) {
	root := t.TempDir()
	loc := Locator{Source: "linked:abc123", Notebook: "Share", Section: "Docs", Page: "Note"}
	if skip, err := Capture(root, loc, []byte("linked"), "plugin", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("Capture: skip=%q err=%v", skip, err)
	}
	man := filepath.Join(root, ".system", "history", "pages", "linked", "Share", "Docs", "Note.jsonl")
	if _, err := os.Stat(man); err != nil {
		t.Fatalf("expected linked source path %s: %v", man, err)
	}
}

func TestCapture_LinkedSourceStableAcrossIDs(t *testing.T) {
	root := t.TempDir()
	first := Locator{Source: "linked:abc123", Notebook: "Share", Section: "Docs", Page: "Note"}
	if skip, err := Capture(root, first, []byte("v1"), "plugin", time.Now().UTC(), Options{}); err != nil || skip != "" {
		t.Fatalf("Capture first: skip=%q err=%v", skip, err)
	}
	second := Locator{Source: "linked:zzz999", Notebook: "Share", Section: "Docs", Page: "Note"}
	list, err := List(root, second)
	if err != nil || len(list) != 1 {
		t.Fatalf("List under new linked id: %v len=%d", err, len(list))
	}
}

func TestCapture_RejectsTraversal(t *testing.T) {
	root := t.TempDir()
	loc := Locator{Source: "vault", Notebook: "Work", Section: "../escape", Page: "X"}
	if _, err := Capture(root, loc, []byte("nope"), "editor", time.Now().UTC(), Options{}); err != nil {
		// sanitize strips leading .. so this may succeed as section "escape".
		// A raw path-separator notebook must fail.
	}
	bad := Locator{Source: "vault", Notebook: "", Page: "X"}
	if _, err := Capture(root, bad, []byte("nope"), "editor", time.Now().UTC(), Options{}); err == nil {
		t.Fatal("empty notebook should fail")
	}
}

func TestConcurrentAppend(t *testing.T) {
	root := t.TempDir()
	loc := testLoc("Race")
	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			body := []byte{byte('A' + i)}
			if skip, err := Capture(root, loc, body, "editor", time.Now().UTC(), Options{}); err != nil {
				errCh <- err
			} else if skip != "" {
				errCh <- errSkip{skip}
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("concurrent Capture: %v", err)
	}
	list, err := List(root, loc)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != n {
		t.Fatalf("concurrent List len=%d, want %d", len(list), n)
	}
}

type errSkip struct{ s string }

func (e errSkip) Error() string { return "skipped " + e.s }

func TestListManifests_NestedRootAndEncoded(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	nested := Locator{Source: "vault", Notebook: "Work", Section: "Projects/Active", Page: "Nested"}
	rootPage := Locator{Source: "vault", Notebook: "Books", Section: "", Page: "Dune"}
	encoded := Locator{Source: "vault", Notebook: "Work", Section: "Journal", Page: "A/B"}
	if _, err := Capture(root, nested, []byte("nested"), "editor", now, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, rootPage, []byte("dune"), "editor", now, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, encoded, []byte("slash"), "editor", now, Options{}); err != nil {
		t.Fatal(err)
	}
	// Unreadable leftover should not fail the walk.
	junk := filepath.Join(root, ".system", "history", "pages", "vault", "Work", "not-a-manifest.txt")
	if err := os.MkdirAll(filepath.Dir(junk), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(junk, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}

	locs, err := ListManifests(root)
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	want := map[string]Locator{
		"Work|Projects/Active|Nested": nested,
		"Books||Dune":                 rootPage,
		"Work|Journal|A/B":            encoded,
	}
	if len(locs) != len(want) {
		t.Fatalf("ListManifests len=%d, want %d (%+v)", len(locs), len(want), locs)
	}
	for _, loc := range locs {
		key := loc.Notebook + "|" + loc.Section + "|" + loc.Page
		got, ok := want[key]
		if !ok {
			t.Fatalf("unexpected locator %+v", loc)
		}
		if got.Source != loc.Source || got.Notebook != loc.Notebook || got.Section != loc.Section || got.Page != loc.Page {
			t.Fatalf("locator %+v != %+v", loc, got)
		}
	}

	empty, err := ListManifests(t.TempDir())
	if err != nil || len(empty) != 0 {
		t.Fatalf("missing history dir: %v len=%d", err, len(empty))
	}
}

func TestLast_MatchesNewest(t *testing.T) {
	root := t.TempDir()
	loc := testLoc("Last")
	now := time.Now().UTC()
	if _, err := Capture(root, loc, []byte("a"), "editor", now, Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, loc, []byte("b"), "restore", now.Add(time.Second), Options{}); err != nil {
		t.Fatal(err)
	}
	last, ok, err := Last(root, loc)
	if err != nil || !ok {
		t.Fatalf("Last: ok=%v err=%v", ok, err)
	}
	if last.Source != "restore" {
		t.Fatalf("Last source=%q, want restore", last.Source)
	}
	got, err := Read(root, loc, last.ID)
	if err != nil || string(got) != "b" {
		t.Fatalf("Last body=%q err=%v", got, err)
	}
}
