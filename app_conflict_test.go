package main

// External-edit conflict detection + resolution tests (#1005). The refusal
// contract: after a page is loaded (FetchPageBlocks / FetchPageMarkdown
// snapshot a fingerprint), an external rewrite of the file must make the next
// whole-page save fail with the external_edit_conflict sentinel while leaving
// the external bytes untouched — and every resolution keeps both versions on
// disk.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/parser"
)

// seedConflictPage stages a one-block page via the save funnel (no load
// fingerprint yet) and returns the parsed blocks.
func seedConflictPage(t *testing.T, app *App, notebook, section, page, blockID, body string) []parser.ParsedBlock {
	t.Helper()
	dir := filepath.Join(app.vaultPath, notebook, section)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\nnotebook: " + notebook + "\nsection: " + section + "\npage: " + page + "\ndate: 2026-01-01\ntags: []\n---\n" +
		body + " <!-- id: " + blockID + " -->\n"
	if err := os.WriteFile(filepath.Join(dir, page+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	blocks, _, _, _, err := parser.ParseFileContent(content, notebook, section, page, "2026-01-01", app.spacesPerTab)
	if err != nil {
		t.Fatalf("ParseFileContent: %v", err)
	}
	if err := app.SaveFileBlocks(notebook, section, page, blocks); err != nil {
		t.Fatalf("SaveFileBlocks seed: %v", err)
	}
	return blocks
}

func conflictPagePath(app *App, notebook, section, page string) string {
	return filepath.Join(app.vaultPath, notebook, section, page+".md")
}

// editBlocks returns a copy of blocks with the first block's text replaced by
// marker (RawText carries the ID comment so the funnel keeps the block).
func editBlocks(blocks []parser.ParsedBlock, blockID, marker string) []parser.ParsedBlock {
	out := make([]parser.ParsedBlock, len(blocks))
	copy(out, blocks)
	out[0].RawText = marker + " <!-- id: " + blockID + " -->"
	out[0].CleanText = marker
	return out
}

// requireConflict asserts err is the typed external-edit refusal.
func requireConflict(t *testing.T, err error) {
	t.Helper()
	if !isExternalEditConflict(err) {
		t.Fatalf("want external_edit_conflict refusal, got %v", err)
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeExternalEditConflict {
		t.Fatalf("error does not carry IPC code %s: %v", CodeExternalEditConflict, err)
	}
}

// 1. External edit → save refused, file byte-identical to the external version.
func TestSaveFileBlocks_RefusesExternalEdit(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "ConfNB", "", "ExtEdit"
	blockID := "aaaaaaa1-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original line")

	// The editor load — snapshots the fingerprint.
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// External writer rewrites the file out from under the editor.
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ConfNB\"\nsection: \"\"\npage: \"ExtEdit\"\ndate: \"2026-01-01\"\ntags: []\n---\nexternal line <!-- id: bbbbbbb2-bbbb-4bbb-8bbb-bbbbbbbbbbbb -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "my unsaved text"))
	requireConflict(t, err)

	got, rerr := os.ReadFile(filePath)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	if string(got) != external {
		t.Fatalf("refused save still modified the file:\n got: %q\nwant: %q", string(got), external)
	}
}

// Source mode funnels through the same refusal.
func TestSavePageMarkdown_RefusesExternalEdit(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "ConfMD", "", "SrcEdit"
	blockID := "aaaaaaa3-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	_ = seedConflictPage(t, app, notebook, section, page, blockID, "original md")

	if _, err := app.FetchPageMarkdown(notebook, section, page); err != nil {
		t.Fatalf("FetchPageMarkdown: %v", err)
	}

	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ConfMD\"\npage: \"SrcEdit\"\ntags: []\n---\nexternal body\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	_, err := app.SavePageMarkdown(notebook, section, page, "my source body\n")
	requireConflict(t, err)

	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("refused markdown save still modified the file: %q", string(got))
	}
}

// 2. mtime-only touch (identical bytes) passes silently and re-baselines.
func TestSaveFileBlocks_MtimeOnlyTouchPasses(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "TouchNB", "", "Touched"
	blockID := "aaaaaaa4-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "stable line")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	filePath := conflictPagePath(app, notebook, section, page)
	future := time.Now().Add(2 * time.Hour)
	if err := os.Chtimes(filePath, future, future); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "edited after touch")); err != nil {
		t.Fatalf("save after mtime-only touch must pass (sync-client tolerance), got: %v", err)
	}

	// The stat refresh from the touch means the NEXT save also fast-paths.
	if err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "second edit")); err != nil {
		t.Fatalf("second save after touch+save must pass: %v", err)
	}
	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "second edit") {
		t.Fatalf("second save did not land: %q", string(got))
	}
}

// 3. Normal save (no external interference) is unaffected.
func TestSaveFileBlocks_NormalSaveUnaffected(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "NormNB", "", "Norm"
	blockID := "aaaaaaa5-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original line")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	if err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "normal edit")); err != nil {
		t.Fatalf("normal save must pass: %v", err)
	}
	got, _ := os.ReadFile(conflictPagePath(app, notebook, section, page))
	if !strings.Contains(string(got), "normal edit") {
		t.Fatalf("normal save did not land: %q", string(got))
	}
}

// 4. Every resolution keeps both versions on disk, with the winner on the
// original path and the loser on a .conflict-* sibling that is indexed as a
// real page.
func TestResolveExternalConflict_KeepMine(t *testing.T) {
	testResolution(t, "keep_mine")
}

func TestResolveExternalConflict_KeepBoth(t *testing.T) {
	testResolution(t, "keep_both")
}

func TestResolveExternalConflict_KeepTheirs(t *testing.T) {
	testResolution(t, "keep_theirs")
}

func testResolution(t *testing.T, resolution string) {
	t.Helper()
	app := newTestApp(t)
	notebook, section, page := "ResNB", "", "Res"+resolution
	blockID := "aaaaaaa6-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original line")
	mine := editBlocks(blocks, blockID, "my edited line")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ResNB\"\npage: \"" + page + "\"\ntags: []\n---\nexternal line <!-- id: ccccccc7-cccc-4ccc-8ccc-cccccccccccc -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	err := app.SaveFileBlocks(notebook, section, page, mine)
	requireConflict(t, err)

	// The resolution reports the conflict copy it created (named in the UI's
	// success announcement).
	copyPage, rerr := app.ResolveExternalConflict(notebook, section, page, resolution)
	if rerr != nil {
		t.Fatalf("ResolveExternalConflict(%s): %v", resolution, rerr)
	}
	if !strings.Contains(copyPage, ".conflict-") {
		t.Fatalf("resolution must name the conflict copy, got %q", copyPage)
	}

	// Exactly one conflict sibling exists, carrying the loser's bytes.
	sibling := findConflictSibling(t, app, notebook, section, page)
	sibBytes, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatalf("read sibling: %v", err)
	}
	origBytes, _ := os.ReadFile(filePath)

	if resolution == "keep_theirs" {
		if string(origBytes) != external {
			t.Fatalf("keep_theirs: original must keep the external version, got %q", string(origBytes))
		}
		if !strings.Contains(string(sibBytes), "my edited line") {
			t.Fatalf("keep_theirs: conflict copy must carry my version, got %q", string(sibBytes))
		}
	} else {
		if !strings.Contains(string(origBytes), "my edited line") {
			t.Fatalf("%s: original must carry my version, got %q", resolution, string(origBytes))
		}
		if !strings.Contains(string(sibBytes), "external line") {
			t.Fatalf("%s: conflict copy must carry the external version, got %q", resolution, string(sibBytes))
		}
	}

	// The sibling is indexed as its own page (visible recovery, not a ghost).
	siblingPage := strings.TrimSuffix(filepath.Base(sibling), ".md")
	if idx, err := app.FetchPageBlocks(notebook, section, siblingPage); err != nil {
		t.Fatalf("FetchPageBlocks sibling: %v", err)
	} else if len(idx) == 0 {
		t.Fatalf("conflict copy %s is not indexed as a page", siblingPage)
	}

	// The baseline is settled: a follow-up save through the funnel passes.
	fresh, err := app.FetchPageBlocks(notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks after resolution: %v", err)
	}
	if err := app.SaveFileBlocks(notebook, section, page, fresh); err != nil {
		t.Fatalf("post-resolution save must pass: %v", err)
	}
}

func findConflictSibling(t *testing.T, app *App, notebook, section, page string) string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(app.vaultPath, notebook, section, page+".conflict-*.md"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("want exactly one conflict sibling, got %v", matches)
	}
	return matches[0]
}

// 5a. Hazard 1 (first half): an external edit that ONLY rewrites block-ID
// comment bytes still conflicts — the fingerprint hashes raw bytes, so ID
// rewrites are detected, never exempted.
func TestExternalEdit_OnlyBlockIDRewriteStillConflicts(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "HazardNB", "", "IdsOnly"
	blockID := "aaaaaaa8-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "stable text")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// External "validator" rewrites ONLY the ID comment (same byte length).
	filePath := conflictPagePath(app, notebook, section, page)
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(raw), blockID) {
		t.Fatalf("seed file lacks the ID comment: %q", string(raw))
	}
	rewritten := strings.Replace(string(raw), blockID, "99999999-9999-4999-8999-999999999999", 1)
	if len(rewritten) != len(string(raw)) {
		t.Fatalf("test bug: ID rewrite changed the byte length")
	}
	if err := os.WriteFile(filePath, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("external ID rewrite: %v", err)
	}

	err = app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "stable text"))
	requireConflict(t, err)
}

// 5b. Hazard 1 (second half): Silt's OWN ID-rewriting write (Sprint-48-style
// normalization through the save funnel) refreshes the fingerprint, so the
// next save is clean — the fingerprint never keys on parsed IDs.
func TestSiltIDRewrite_RefreshesFingerprint(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "HazardNB2", "", "IdNormalize"
	blockID := "aaaaaaa9-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "some text")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// Silt rewrites the block's ID through its own write path (the save
	// funnel re-renders the ID comment from blocks[].ID).
	renormalized := make([]parser.ParsedBlock, len(blocks))
	copy(renormalized, blocks)
	newID := "88888888-8888-4888-8888-888888888888"
	renormalized[0].ID = newID
	renormalized[0].RawText = "some text <!-- id: " + newID + " -->"

	if err := app.SaveFileBlocks(notebook, section, page, renormalized); err != nil {
		t.Fatalf("ID-normalizing save must pass: %v", err)
	}

	// The very next save is clean — the funnel refreshed the baseline.
	if err := app.SaveFileBlocks(notebook, section, page, editBlocks(renormalized, newID, "edited after normalize")); err != nil {
		t.Fatalf("save after Silt's own ID rewrite must be clean: %v", err)
	}
	got, _ := os.ReadFile(conflictPagePath(app, notebook, section, page))
	if !strings.Contains(string(got), "edited after normalize") {
		t.Fatalf("second save did not land: %q", string(got))
	}
}

// 6. Race: an external edit landing between the save's epoch snapshot and the
// file-lock acquisition is still caught — the fingerprint check runs inside
// the lock, against post-acquisition disk state.
func TestExternalEditRace_CheckRunsInsideLock(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "RaceConfNB", "", "Racy"
	blockID := "bbbbbbb1-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original line")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// The hook fires after SaveFileBlocks snapshots its epoch and before it
	// waits on LockFileWrite (the afterPageWriteEpochSnapshot seam).
	afterPageWriteEpochSnapshot = func() {
		filePath := conflictPagePath(app, notebook, section, page)
		external := "---\nnotebook: \"RaceConfNB\"\npage: \"Racy\"\ntags: []\n---\nlate external line\n"
		if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
			t.Errorf("late external write: %v", err)
		}
	}
	t.Cleanup(func() { afterPageWriteEpochSnapshot = nil })

	err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "my racing edit"))
	requireConflict(t, err)

	got, _ := os.ReadFile(conflictPagePath(app, notebook, section, page))
	if !strings.Contains(string(got), "late external line") {
		t.Fatalf("late external edit was clobbered: %q", string(got))
	}
}

// A page loaded while absent from disk still refuses once an external writer
// creates it (absent baseline → any real bytes conflict).
func TestExternallyCreatedPage_ConflictsWithSave(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "CreatedNB", "", "Ghost"
	blockID := "bbbbbbb2-bbbb-4bbb-8bbb-bbbbbbbbbbbb"

	// Index a page whose file is (re)moved before load.
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "seeded line")
	filePath := conflictPagePath(app, notebook, section, page)
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// External tool recreates the file.
	if err := os.WriteFile(filePath, []byte("---\npage: \"Ghost\"\n---\nexternal recreation\n"), 0o644); err != nil {
		t.Fatalf("external create: %v", err)
	}

	err := app.SaveFileBlocks(notebook, section, page, blocks)
	requireConflict(t, err)
}

// Resolution without a pending refusal fails loudly (e.g. the app restarted
// between refusal and click) and does not write anything.
func TestResolveExternalConflict_WithoutPendingRefusalFails(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "NoStashNB", "", "NoStash"
	blockID := "bbbbbbb3-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	_ = seedConflictPage(t, app, notebook, section, page, blockID, "stable")

	_, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine")
	if err == nil {
		t.Fatal("resolution without a pending refusal must fail")
	}
	requireConflict(t, err)

	// No conflict copy was minted.
	matches, _ := filepath.Glob(filepath.Join(app.vaultPath, notebook, section, page+".conflict-*.md"))
	if len(matches) != 0 {
		t.Fatalf("failed resolution still wrote conflict copies: %v", matches)
	}
}

// keep_theirs without a stash (post-restart) is still disk-meaningful: theirs
// is on the original path, so the call settles it — indexes theirs, refreshes
// the baseline — and reports that nothing was written, instead of failing and
// leaving the editor refusing forever against a stale baseline.
func TestResolveExternalConflict_KeepTheirsWithoutStashSettlesBaseline(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "NoStashKT", "", "NoStashKT"
	blockID := "bbbbbb4b-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "stable")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"NoStashKT\"\npage: \"NoStashKT\"\ntags: []\n---\nexternal body\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	copyPage, err := app.ResolveExternalConflict(notebook, section, page, "keep_theirs")
	if err != nil {
		t.Fatalf("keep_theirs without stash must settle, got: %v", err)
	}
	if copyPage != "" {
		t.Fatalf("nothing was written, so no copy must be named, got %q", copyPage)
	}

	got, _ := os.ReadFile(filePath)
	if string(got) != external {
		t.Fatalf("keep_theirs without stash must not touch the file: %q", string(got))
	}
	// The baseline adopted theirs: the stale-buffer save that WAS refused now
	// passes and lands instead of re-refusing forever.
	if err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "after settle")); err != nil {
		t.Fatalf("save after keep_theirs-without-stash must pass: %v", err)
	}
}

// Invalid resolution strings are rejected without touching anything.
func TestResolveExternalConflict_RejectsUnknownResolution(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.ResolveExternalConflict("NB", "", "P", "overwrite"); err == nil {
		t.Fatal("unknown resolution must be rejected")
	}
}

// Racily-clean regression (#1005): a same-size external rewrite written
// moments after the baseline capture cannot be distinguished from the baseline
// by stat alone on coarse-timestamp filesystems (FAT/SMB 1–2s, ext3/tmpfs
// jiffy-coarse) — the stat halves compare equal. The capture-time raciness
// window must distrust that match and let the hash arbiter refuse the save.
// Deterministic on every filesystem: the baseline is captured milliseconds
// before the rewrite, so mtime is always inside the window.
func TestExternalEdit_RacySameSizeRewriteRefuses(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "RacyStatNB", "", "RacyStat"
	blockID := "cccccc1c-cccc-4ccc-8ccc-ccccccccccc1"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "stable text")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// Same byte length, different content, immediately after the capture.
	filePath := conflictPagePath(app, notebook, section, page)
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	rewritten := strings.Replace(string(raw), "stable text", "edited text", 1)
	if len(rewritten) != len(string(raw)) {
		t.Fatalf("test bug: rewrite changed the byte length")
	}
	if err := os.WriteFile(filePath, []byte(rewritten), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	err = app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "stable text"))
	requireConflict(t, err)

	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "edited text") {
		t.Fatalf("racily-clean external edit was clobbered: %q", string(got))
	}
}

// A conflict copy of a frontmatter-less external version must surface as a
// page under the ORIGINAL page's notebook/section (parse defaults + minted
// fallback keys), not a junk `notebook=”` row that is invisible until a
// restart.
func TestResolveExternalConflict_FrontmatterLessCopySurfaces(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "FmlessNB", "", "NoFm"
	blockID := "cccccc2c-cccc-4ccc-8ccc-ccccccccccc2"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "silt line")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	// External rewrite with NO frontmatter at all (script/hand-created file).
	filePath := conflictPagePath(app, notebook, section, page)
	external := "external line with no frontmatter\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}
	err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "my line"))
	requireConflict(t, err)

	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict: %v", err)
	}

	// The copy carries the sibling's home locator in its minted frontmatter…
	sibling := findConflictSibling(t, app, notebook, section, page)
	sibBytes, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatalf("read sibling: %v", err)
	}
	sib := string(sibBytes)
	if !strings.Contains(sib, "notebook: \"FmlessNB\"") {
		t.Fatalf("copy must mint the original notebook, got %q", sib)
	}
	if !strings.Contains(sib, "external line with no frontmatter") {
		t.Fatalf("copy must carry theirs' body verbatim, got %q", sib)
	}

	// …and is indexed under (notebook, section, siblingPage), so it surfaces
	// as visible recovery without a rescan/restart.
	siblingPage := strings.TrimSuffix(filepath.Base(sibling), ".md")
	if idx, err := app.FetchPageBlocks(notebook, section, siblingPage); err != nil {
		t.Fatalf("FetchPageBlocks sibling: %v", err)
	} else if len(idx) == 0 {
		t.Fatalf("frontmatter-less conflict copy %s is not indexed as a page", siblingPage)
	}
}

// The resolution replays the NEWEST stashed buffer: a second refused save
// replaces the first stash, and keep_mine must land the second version's
// text (the in-lock pop ordering that makes a racing re-refusal safe).
func TestResolveExternalConflict_ReplaysNewestStash(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "NewestNB", "", "Newest"
	blockID := "cccccc3c-cccc-4ccc-8ccc-ccccccccccc3"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original line")

	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"NewestNB\"\npage: \"Newest\"\ntags: []\n---\nexternal line\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatalf("external write: %v", err)
	}

	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "first refused attempt")))
	requireConflict(t, app.SaveFileBlocks(notebook, section, page,
		editBlocks(blocks, blockID, "second refused attempt")))

	if _, err := app.ResolveExternalConflict(notebook, section, page, "keep_mine"); err != nil {
		t.Fatalf("ResolveExternalConflict: %v", err)
	}

	got, _ := os.ReadFile(filePath)
	if !strings.Contains(string(got), "second refused attempt") {
		t.Fatalf("resolution must replay the newest stashed buffer, got %q", string(got))
	}
}

// Markdown-stash resolutions (Source mode refused via SavePageMarkdown) take
// the isMarkdown branches the blocks suite cannot reach: keep_mine replays
// the stashed body through writePageMarkdownLocked, keep_theirs mints the
// sibling from the stashed body (nil contentBytes → minted frontmatter).
func TestResolveExternalConflict_MarkdownStash(t *testing.T) {
	for _, resolution := range []string{"keep_mine", "keep_theirs"} {
		t.Run(resolution, func(t *testing.T) {
			app := newTestApp(t)
			notebook, section, page := "MdResNB", "", "Md"+resolution
			blockID := "dddddd1d-dddd-4ddd-8ddd-dddddddddddd"
			_ = seedConflictPage(t, app, notebook, section, page, blockID, "original md line")

			if _, err := app.FetchPageMarkdown(notebook, section, page); err != nil {
				t.Fatalf("FetchPageMarkdown: %v", err)
			}

			filePath := conflictPagePath(app, notebook, section, page)
			external := "---\nnotebook: \"MdResNB\"\npage: \"" + page + "\"\ntags: []\n---\nexternal md body\n"
			if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
				t.Fatalf("external write: %v", err)
			}
			_, err := app.SavePageMarkdown(notebook, section, page, "my stashed body\n")
			requireConflict(t, err)

			copyPage, rerr := app.ResolveExternalConflict(notebook, section, page, resolution)
			if rerr != nil {
				t.Fatalf("ResolveExternalConflict(%s): %v", resolution, rerr)
			}
			if !strings.Contains(copyPage, ".conflict-") {
				t.Fatalf("resolution must name the conflict copy, got %q", copyPage)
			}

			origBytes, _ := os.ReadFile(filePath)
			sibling := findConflictSibling(t, app, notebook, section, page)
			sibBytes, _ := os.ReadFile(sibling)

			if resolution == "keep_theirs" {
				if !strings.Contains(string(origBytes), "external md body") {
					t.Fatalf("keep_theirs: original must keep theirs, got %q", string(origBytes))
				}
				if !strings.Contains(string(sibBytes), "my stashed body") {
					t.Fatalf("keep_theirs: copy must carry my stashed body, got %q", string(sibBytes))
				}
			} else {
				if !strings.Contains(string(origBytes), "my stashed body") {
					t.Fatalf("keep_mine: original must carry my stashed body, got %q", string(origBytes))
				}
				if !strings.Contains(string(sibBytes), "external md body") {
					t.Fatalf("keep_mine: copy must carry theirs, got %q", string(sibBytes))
				}
			}

			// The sibling is indexed as its own page and the funnel passes
			// again after the resolution re-baselined.
			siblingPage := strings.TrimSuffix(filepath.Base(sibling), ".md")
			if idx, err := app.FetchPageBlocks(notebook, section, siblingPage); err != nil {
				t.Fatalf("FetchPageBlocks sibling: %v", err)
			} else if len(idx) == 0 {
				t.Fatalf("markdown conflict copy %s is not indexed as a page", siblingPage)
			}
			if _, err := app.FetchPageMarkdown(notebook, section, page); err != nil {
				t.Fatalf("FetchPageMarkdown after resolution: %v", err)
			}
			if _, err := app.SavePageMarkdown(notebook, section, page, "post-resolution body\n"); err != nil {
				t.Fatalf("post-resolution markdown save must pass: %v", err)
			}
		})
	}
}
