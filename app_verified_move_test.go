package main

// Verified-move tests (#1021): moves dispatch per root like writes — on a
// detected non-atomic root every rename/move/tree/trash operation copies
// through the verified machinery (the source survives every copy-side
// failure and is removed only after the copy is verified durable), while
// local roots keep the plain os.Rename. Torn copies are simulated with the
// VerifiedWriteAfterRenameHook seam, as in the verified-write tests.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/parser"
)

// swapVerifiedMoveSeam counts verifiedMoveFile calls while delegating to the
// real primitive (the dispatch's verified branch is what must fire).
func swapVerifiedMoveSeam(t *testing.T) *int {
	t.Helper()
	calls := 0
	orig := verifiedMoveFile
	verifiedMoveFile = func(oldPath, newPath, backupPath string) error {
		calls++
		return parser.MoveFileVerified(oldPath, newPath, backupPath)
	}
	t.Cleanup(func() { verifiedMoveFile = orig })
	return &calls
}

// 1. Torn copy mid-move: the old path must stay intact (a bare rename would
// have consumed it before any verification — the #1021 data-loss shape), the
// new path must never hold a partial file, and the typed error must name
// where the original still lives.
func TestVerifiedMove_TornCopyMidMoveKeepsOriginal(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, page := "VMNB", "TornMove"
	blockID := "3a3a3a3a-4b4b-4c5c-8d6d-7e7e7e7e7e7e"
	seedConflictPage(t, app, notebook, "", page, blockID, "precious move body")
	oldPath := conflictPagePath(app, notebook, "", page)
	original, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	newPath := conflictPagePath(app, notebook, "", "TornMoveNew")

	renames := 0
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		renames++
		if strings.HasSuffix(path, "TornMoveNew.md") && renames <= 2 {
			// move copy + retry torn; the restore rename stays clean
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err = app.RenamePage(notebook, "", page, "TornMoveNew")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error, got %v", CodeVerifiedWriteFailed, err)
	}

	got, rerr := os.ReadFile(oldPath)
	if rerr != nil {
		t.Fatalf("original vanished after torn move: %v", rerr)
	}
	if string(got) != string(original) {
		t.Fatalf("original bytes altered by a torn move:\n got: %q\nwant: %q", got, original)
	}
	if _, serr := os.Stat(newPath); !os.IsNotExist(serr) {
		t.Errorf("new path holds a file after a failed move (stat err = %v) — a partial must never present as the page", serr)
	}
	pending := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, "TornMoveNew.md.pending")
	gotPending, perr := os.ReadFile(pending)
	if perr != nil || !strings.Contains(string(gotPending), "precious move body") {
		t.Errorf("pending copy = %q (err = %v), want the pre-move bytes retained", gotPending, perr)
	}
	if !strings.Contains(ipc.Message, oldPath) {
		t.Errorf("typed error must name the intact original: %q", ipc.Message)
	}
	// The abort landed before clearAndReindex: the page is still loadable at
	// its old locator (fail-safe — the next scan reconciles, nothing more).
	if blocks, ferr := app.FetchPageBlocks(notebook, "", page); ferr != nil || len(blocks) == 0 {
		t.Errorf("old locator must stay indexed after a failed move (blocks=%d err=%v)", len(blocks), ferr)
	}
}

// 2. Local root: the plain rename path — no verified-move calls, no backup
// churn, and the file identity survives the move into trash (a copy would
// change it; DeletePage's trash move does no content rewrite, so identity
// isolates the move mechanism itself).
func TestVerifiedMove_LocalRootTakesPlainRename(t *testing.T) {
	app := newTestApp(t)
	orig := rootNetworkFSCheck
	rootNetworkFSCheck = func(string) error { return nil }
	t.Cleanup(func() { rootNetworkFSCheck = orig })
	app.detectVerifiedWriteRoot(app.vaultPath)

	moveCalls := swapVerifiedMoveSeam(t)

	notebook, page := "VMNB", "LocalMove"
	blockID := "4b4b4b4b-5c5c-4d6d-8e7e-8f8f8f8f8f8f"
	seedConflictPage(t, app, notebook, "", page, blockID, "local body")
	src := conflictPagePath(app, notebook, "", page)
	before, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}

	if err := app.DeletePage(notebook, "", page); err != nil {
		t.Fatalf("local delete: %v", err)
	}
	if n := *moveCalls; n != 0 {
		t.Errorf("local root took the verified move path (%d primitive calls)", n)
	}
	var trashed string
	_ = filepath.WalkDir(app.trashBase(), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, page+".md") {
			trashed = p
		}
		return nil
	})
	if trashed == "" {
		t.Fatal("no trashed copy found under .system/trash")
	}
	after, err := os.Stat(trashed)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Errorf("local trash move changed the file identity — the plain rename must be preserved")
	}
	if _, err := os.Stat(filepath.Join(app.vaultPath, ".system", "writebackups")); !os.IsNotExist(err) {
		t.Errorf("local move created backup churn: %v", err)
	}
}

// 3. moveToTrash on a forced-network root takes the copy+verify path and the
// trashed copy holds the exact original bytes — the manual trash restore
// stays possible.
func TestVerifiedMove_TrashOnNetworkRootCopiesVerified(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, page := "VMNB", "TrashMe"
	blockID := "5c5c5c5c-6d6d-4e7e-8f8f-9a9a9a9a9a9a"
	seedConflictPage(t, app, notebook, "", page, blockID, "trash me safely")
	src := conflictPagePath(app, notebook, "", page)
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	moveCalls := swapVerifiedMoveSeam(t)
	if err := app.DeletePage(notebook, "", page); err != nil {
		t.Fatalf("delete via verified move: %v", err)
	}
	if n := *moveCalls; n != 1 {
		t.Errorf("verified move primitive calls = %d, want 1 (copy+verify into trash)", n)
	}
	if _, serr := os.Stat(src); !os.IsNotExist(serr) {
		t.Errorf("source not removed after the verified copy landed (stat err = %v)", serr)
	}

	var trashed string
	_ = filepath.WalkDir(app.trashBase(), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, page+".md") {
			trashed = p
		}
		return nil
	})
	if trashed == "" {
		t.Fatal("no trashed copy found under .system/trash")
	}
	got, rerr := os.ReadFile(trashed)
	if rerr != nil || string(got) != string(original) {
		t.Errorf("trashed copy = %q (err = %v), want the exact original bytes for manual restore", got, rerr)
	}
	if idx, ferr := app.FetchPageBlocks(notebook, "", page); ferr == nil && len(idx) > 0 {
		t.Errorf("deleted page still indexed: %d blocks", len(idx))
	}
}

// 4. Torn trash move: the delete must fail loud with the typed error and the
// page must survive intact at its origin — a bare rename torn mid-move would
// strand the file between the vault and the trash.
func TestVerifiedMove_TornTrashMoveKeepsOriginal(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, page := "VMNB", "TornTrash"
	blockID := "6d6d6d6d-7e7e-4f8f-9a0a-0b0b0b0b0b0b"
	seedConflictPage(t, app, notebook, "", page, blockID, "do not strand me")
	src := conflictPagePath(app, notebook, "", page)
	original, err := os.ReadFile(src)
	if err != nil {
		t.Fatal(err)
	}

	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		if strings.Contains(path, filepath.Join(".system", "trash")) {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err = app.DeletePage(notebook, "", page)
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error (wrapped by the trash funnel), got %v", CodeVerifiedWriteFailed, err)
	}
	got, rerr := os.ReadFile(src)
	if rerr != nil {
		t.Fatalf("page vanished after a torn trash move: %v", rerr)
	}
	if string(got) != string(original) {
		t.Fatalf("page bytes altered by a torn trash move:\n got: %q\nwant: %q", got, original)
	}
	if blocks, ferr := app.FetchPageBlocks(notebook, "", page); ferr != nil || len(blocks) == 0 {
		t.Errorf("page must stay indexed after a failed delete (blocks=%d err=%v)", len(blocks), ferr)
	}
}

// 5. Torn tree move mid-walk: the whole section — nested pages and non-page
// attachments included — must survive byte-identical at the old directory,
// with nothing landed at the new one. Pre-#1021 the bare directory rename
// followed by torn forward writes left the rolled-back pages holding torn
// bytes.
func TestVerifiedMove_TornTreeMoveKeepsTreeWhole(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook := "VMNB"
	alphaID := "7e7e7e7e-8f8f-4a9a-0b1b-1c1c1c1c1c1c"
	betaID := "8f8f8f8f-9a9a-4b0b-1c2c-2d2d2d2d2d2d"
	seedConflictPage(t, app, notebook, "OldSec", "Alpha", alphaID, "alpha body")
	seedConflictPage(t, app, notebook, "OldSec/Deep", "Beta", betaID, "beta body")
	attach := filepath.Join(app.vaultPath, notebook, "OldSec", "att.bin")
	attachBytes := []byte("attachment-bytes-\x00\x01")
	if err := os.WriteFile(attach, attachBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	alphaPath := conflictPagePath(app, notebook, "OldSec", "Alpha")
	betaPath := conflictPagePath(app, notebook, "OldSec/Deep", "Beta")
	alphaOrig, _ := os.ReadFile(alphaPath)
	betaOrig, _ := os.ReadFile(betaPath)

	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		if strings.Contains(path, "NewSec") {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err := app.RenameSection(notebook, "OldSec", "NewSec")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error, got %v", CodeVerifiedWriteFailed, err)
	}

	if got, rerr := os.ReadFile(alphaPath); rerr != nil || string(got) != string(alphaOrig) {
		t.Errorf("Alpha not intact at old dir after torn tree move (err=%v):\n got: %q\nwant: %q", rerr, got, alphaOrig)
	}
	if got, rerr := os.ReadFile(betaPath); rerr != nil || string(got) != string(betaOrig) {
		t.Errorf("Beta not intact at old dir after torn tree move (err=%v):\n got: %q\nwant: %q", rerr, got, betaOrig)
	}
	if got, rerr := os.ReadFile(attach); rerr != nil || string(got) != string(attachBytes) {
		t.Errorf("attachment not intact at old dir (err=%v)", rerr)
	}
	// Nothing landed at the new directory: not even an empty husk.
	if _, serr := os.Stat(filepath.Join(app.vaultPath, notebook, "NewSec")); !os.IsNotExist(serr) {
		t.Errorf("NewSec exists after a failed tree move (stat err = %v)", serr)
	}
	if blocks, ferr := app.FetchPageBlocks(notebook, "OldSec", "Alpha"); ferr != nil || len(blocks) == 0 {
		t.Errorf("old section must stay indexed after a failed tree move (blocks=%d err=%v)", len(blocks), ferr)
	}
}

// 6. Index coherence after a verified move that succeeds: the old locator's
// rows are cleared, the new one indexed with block IDs preserved, and the
// file lives only at the new path.
func TestVerifiedMove_RenamePageOnNetworkRootIndexesNewPath(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, page := "VMNB", "IdxMove"
	blockID := "9a9a9a9a-0b0b-4c1c-2d3d-3e3e3e3e3e3e"
	seedConflictPage(t, app, notebook, "", page, blockID, "index me after the move")

	if err := app.RenamePage(notebook, "", page, "IdxMoveNew"); err != nil {
		t.Fatalf("verified rename on network root: %v", err)
	}
	if _, serr := os.Stat(conflictPagePath(app, notebook, "", page)); !os.IsNotExist(serr) {
		t.Errorf("old path still holds a file (stat err = %v)", serr)
	}
	if _, serr := os.Stat(conflictPagePath(app, notebook, "", "IdxMoveNew")); serr != nil {
		t.Fatalf("new path missing after verified move: %v", serr)
	}
	blocks, err := app.FetchPageBlocks(notebook, "", "IdxMoveNew")
	if err != nil || len(blocks) == 0 {
		t.Fatalf("new locator not indexed after verified move (blocks=%d err=%v)", len(blocks), err)
	}
	if blocks[0].ID != blockID {
		t.Errorf("block ID not preserved across the verified move: %q", blocks[0].ID)
	}
	if idx, ferr := app.FetchPageBlocks(notebook, "", page); ferr == nil && len(idx) > 0 {
		t.Errorf("old locator still indexed after verified move: %d blocks", len(idx))
	}
}

// 7. Plain-error phase-1 abort (harden PR #1049, D1): a copy that fails with
// a PLAIN error (a source read failing mid-copy — stale NFS handle) leaves
// the tree whole at oldDir, and rollback must NOT run: it would write
// snapshot bytes under newDir and copy them back over the intact originals —
// verified rewrites of untouched bytes on an already-failing mount. Pinned by
// the copy-seam call count (a rollback's move-back would add a second copy
// wave), the absence of backup churn under the ORIGINAL tree, and the
// tree-level abort clause.
func TestVerifiedMove_PlainErrorTreeAbortSkipsRollback(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook := "VMNB"
	alphaID := "2d2d2d2d-3e3e-4f4f-8a1a-4c4c4c4c4c4c"
	betaID := "3e3e3e3e-4f4f-4a5a-8b2b-5d5d5d5d5d5d"
	seedConflictPage(t, app, notebook, "OldSec", "Alpha", alphaID, "alpha body")
	seedConflictPage(t, app, notebook, "OldSec/Deep", "Beta", betaID, "beta body")
	oldDir := filepath.Join(app.vaultPath, notebook, "OldSec")
	newDir := filepath.Join(app.vaultPath, notebook, "NewSec")
	alphaPath := conflictPagePath(app, notebook, "OldSec", "Alpha")
	betaPath := conflictPagePath(app, notebook, "OldSec/Deep", "Beta")
	alphaOrig, _ := os.ReadFile(alphaPath)
	betaOrig, _ := os.ReadFile(betaPath)

	calls := 0
	origCopy := verifiedMoveCopy
	verifiedMoveCopy = func(src, dst, backup string) error {
		calls++
		if strings.HasSuffix(src, "Beta.md") {
			return fmt.Errorf("read %s: stale NFS file handle", src)
		}
		return parser.CopyFileVerified(src, dst, backup)
	}
	t.Cleanup(func() { verifiedMoveCopy = origCopy })

	err := app.RenameSection(notebook, "OldSec", "NewSec")
	if err == nil {
		t.Fatal("plain-error tree abort must surface an error")
	}
	var ipc *IPCError
	if errors.As(err, &ipc) {
		t.Fatalf("plain phase-1 abort must stay a plain error, got typed %s: %v", ipc.Code, err)
	}
	if !strings.Contains(err.Error(), "stale NFS file handle") {
		t.Errorf("surfaced error lost the real cause: %v", err)
	}
	if !strings.Contains(err.Error(), "The section is unchanged at "+oldDir) {
		t.Errorf("tree-level abort clause missing: %v", err)
	}
	// Rollback must not run: one copy wave only. A rollback would rewrite
	// the snapshot bytes under newDir and verified-copy them back over the
	// intact originals (calls → 4).
	if calls != 2 {
		t.Errorf("verifiedMoveCopy calls = %d, want 2 (each file attempted once; a rollback would add a copy-back wave)", calls)
	}
	if got, rerr := os.ReadFile(alphaPath); rerr != nil || string(got) != string(alphaOrig) {
		t.Errorf("Alpha not intact at old dir after plain-error abort (err=%v)", rerr)
	}
	if got, rerr := os.ReadFile(betaPath); rerr != nil || string(got) != string(betaOrig) {
		t.Errorf("Beta not intact at old dir after plain-error abort (err=%v)", rerr)
	}
	if _, serr := os.Stat(newDir); !os.IsNotExist(serr) {
		t.Errorf("NewSec exists after an aborted tree move (stat err = %v)", serr)
	}
	if blocks, ferr := app.FetchPageBlocks(notebook, "OldSec", "Alpha"); ferr != nil || len(blocks) == 0 {
		t.Errorf("old section must stay indexed after an aborted tree move (blocks=%d err=%v)", len(blocks), ferr)
	}
}

// 8. Phase-2 source-removal failure (harden PR #1049): a mount rejecting a
// source removal mid-phase-2 leaves the tree SPLIT (every file verified at
// newDir, stragglers at oldDir). The abort is not phase-1, so the rename core
// rolls back: snapshot bytes rewritten under newDir, tree moved back —
// converging on the intact original, nothing left at the new directory.
func TestVerifiedMove_SourceRemovalFailureSplitTreeRollsBack(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook := "VMNB"
	alphaID := "4f4f4f4f-5a5a-4b6b-8c3c-6e6e6e6e6e6e"
	betaID := "5a5a5a5a-6b6b-4c7c-8d4d-7f7f7f7f7f7f"
	seedConflictPage(t, app, notebook, "OldSec", "Alpha", alphaID, "alpha body")
	seedConflictPage(t, app, notebook, "OldSec/Deep", "Beta", betaID, "beta body")
	oldDir := filepath.Join(app.vaultPath, notebook, "OldSec")
	newDir := filepath.Join(app.vaultPath, notebook, "NewSec")
	alphaPath := conflictPagePath(app, notebook, "OldSec", "Alpha")
	betaPath := conflictPagePath(app, notebook, "OldSec/Deep", "Beta")
	alphaOrig, _ := os.ReadFile(alphaPath)
	betaOrig, _ := os.ReadFile(betaPath)

	origRemove := verifiedMoveRemoveSource
	verifiedMoveRemoveSource = func(p string) error {
		if strings.HasPrefix(p, oldDir) && strings.HasSuffix(p, "Beta.md") {
			return fmt.Errorf("remove %s: mount is read-only", p)
		}
		return os.Remove(p)
	}
	t.Cleanup(func() { verifiedMoveRemoveSource = origRemove })

	err := app.RenameSection(notebook, "OldSec", "NewSec")
	if err == nil {
		t.Fatal("phase-2 removal failure must surface an error")
	}
	if !strings.Contains(err.Error(), "remove source") {
		t.Errorf("surfaced error must name the failed source removal: %v", err)
	}
	if !strings.Contains(err.Error(), "is unchanged at") {
		t.Errorf("converged rollback must state the tree is unchanged at its origin: %v", err)
	}
	// Rollback converged the split tree: whole at oldDir, nothing at newDir.
	if got, rerr := os.ReadFile(alphaPath); rerr != nil || string(got) != string(alphaOrig) {
		t.Errorf("Alpha not converged back to old dir (err=%v):\n got: %q\nwant: %q", rerr, got, alphaOrig)
	}
	if got, rerr := os.ReadFile(betaPath); rerr != nil || string(got) != string(betaOrig) {
		t.Errorf("Beta not converged back to old dir (err=%v):\n got: %q\nwant: %q", rerr, got, betaOrig)
	}
	if _, serr := os.Stat(newDir); !os.IsNotExist(serr) {
		t.Errorf("NewSec exists after rollback (stat err = %v) — the split tree did not converge", serr)
	}
	if blocks, ferr := app.FetchPageBlocks(notebook, "OldSec", "Alpha"); ferr != nil || len(blocks) == 0 {
		t.Errorf("old section must be re-indexed after rollback (blocks=%d err=%v)", len(blocks), ferr)
	}
}

// 9. A verified tree move that succeeds: the whole tree (pages with updated
// frontmatter, attachments byte-identical) lands at the new directory, the
// old directory is gone (empty husk removed), and the index follows.
func TestVerifiedMove_TreeRenameOnNetworkRootSucceeds(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook := "VMNB"
	alphaID := "0b0b0b0b-1c1c-4d2d-3e4e-4f4f4f4f4f4f"
	betaID := "1c1c1c1c-2d2d-4e3e-4f5f-5a5a5a5a5a5a"
	seedConflictPage(t, app, notebook, "OldSec", "Alpha", alphaID, "alpha body")
	seedConflictPage(t, app, notebook, "OldSec/Deep", "Beta", betaID, "beta body")
	attach := filepath.Join(app.vaultPath, notebook, "OldSec", "att.bin")
	attachBytes := []byte("attachment-bytes-\x00\x02")
	if err := os.WriteFile(attach, attachBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := app.RenameSection(notebook, "OldSec", "NewSec"); err != nil {
		t.Fatalf("verified tree rename on network root: %v", err)
	}

	if _, serr := os.Stat(filepath.Join(app.vaultPath, notebook, "OldSec")); !os.IsNotExist(serr) {
		t.Errorf("old section directory still exists after verified tree move (stat err = %v)", serr)
	}
	newAlpha, err := os.ReadFile(conflictPagePath(app, notebook, "NewSec", "Alpha"))
	if err != nil {
		t.Fatalf("Alpha missing at new dir: %v", err)
	}
	if !strings.Contains(string(newAlpha), "NewSec") {
		t.Errorf("Alpha frontmatter not rewritten to the new section: %q", newAlpha)
	}
	if got, rerr := os.ReadFile(filepath.Join(app.vaultPath, notebook, "NewSec", "Deep", "Beta.md")); rerr != nil || !strings.Contains(string(got), "NewSec/Deep") {
		t.Errorf("nested Beta missing or not rewritten at new dir (err=%v): %q", rerr, got)
	}
	if got, rerr := os.ReadFile(filepath.Join(app.vaultPath, notebook, "NewSec", "att.bin")); rerr != nil || string(got) != string(attachBytes) {
		t.Errorf("attachment not copied byte-identical into the new tree (err=%v)", rerr)
	}
	if blocks, ferr := app.FetchPageBlocks(notebook, "NewSec", "Alpha"); ferr != nil || len(blocks) == 0 {
		t.Errorf("new section not indexed after verified tree move (blocks=%d err=%v)", len(blocks), ferr)
	}
	if blocks, ferr := app.FetchPageBlocks(notebook, "NewSec/Deep", "Beta"); ferr != nil || len(blocks) == 0 {
		t.Errorf("nested new section not indexed after verified tree move (blocks=%d err=%v)", len(blocks), ferr)
	}
	if idx, ferr := app.FetchPageBlocks(notebook, "OldSec", "Alpha"); ferr == nil && len(idx) > 0 {
		t.Errorf("old section still indexed after verified tree move: %d blocks", len(idx))
	}
}

// 10. #1049 iter-1: a section rename into its own subtree (or onto its own
// ancestor, or onto itself) must be refused in prepare — the verified tree
// move would create newDir inside oldDir's hierarchy, stranding an origin
// husk that can never be emptied, and the reverse rename would be refused
// forever after.
func TestVerifiedMove_NestedSectionRenameRefused(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook := "VMNB"
	alphaID := "6b6b6b6b-7c7c-4d8d-9e3f-8a8a8a8a8a8a"
	seedConflictPage(t, app, notebook, "OldSec", "Alpha", alphaID, "alpha body")
	alphaPath := conflictPagePath(app, notebook, "OldSec", "Alpha")
	alphaOrig, _ := os.ReadFile(alphaPath)

	for _, tc := range []struct{ oldSec, newSec string }{
		{"OldSec", "OldSec/Sub"},  // into its own subtree
		{"OldSec/Deep", "OldSec"}, // onto its own ancestor
		// (old == new is short-circuited to a no-op success by RenameSection
		// before prepare — not a nesting case.)
	} {
		err := app.RenameSection(notebook, tc.oldSec, tc.newSec)
		if err == nil {
			t.Fatalf("rename %q → %q must be refused", tc.oldSec, tc.newSec)
		}
		var ipc *IPCError
		if !errors.As(err, &ipc) || ipc.Code != CodeInvalidNavigationPath {
			t.Errorf("rename %q → %q: want invalid_navigation_path, got %v", tc.oldSec, tc.newSec, err)
		}
	}

	// Nothing moved, nothing created.
	if _, serr := os.Stat(filepath.Join(app.vaultPath, notebook, "OldSec", "Sub")); !os.IsNotExist(serr) {
		t.Errorf("OldSec/Sub exists after refused rename (stat err = %v)", serr)
	}
	if got, rerr := os.ReadFile(alphaPath); rerr != nil || string(got) != string(alphaOrig) {
		t.Errorf("Alpha not intact after refused renames (err=%v)", rerr)
	}

	// Symlink alias (iter-2 finding): newDir through a symlink that resolves
	// inside oldDir must be refused by the resolving predicate — a lexical
	// prefix check misses it and the move strands the origin husk.
	oldDir := filepath.Join(app.vaultPath, notebook, "OldSec")
	aliasDir := filepath.Join(app.vaultPath, notebook, "Alias")
	if err := os.Symlink(oldDir, aliasDir); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	err := app.RenameSection(notebook, "OldSec", "Alias/Sub")
	if err == nil {
		t.Fatal("rename through a symlink alias into the section's own subtree must be refused")
	}
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeInvalidNavigationPath {
		t.Errorf("symlink-alias rename: want invalid_navigation_path, got %v", err)
	}
	if _, serr := os.Stat(filepath.Join(oldDir, "Sub")); !os.IsNotExist(serr) {
		t.Errorf("OldSec/Sub exists after refused symlink-alias rename (stat err = %v)", serr)
	}
}

// 11. #1049 iter-1: a LOCAL atomic-rename failure (EPERM/EBUSY class) must
// abort with the same whole-at-origin semantics as a verified phase-1
// failure — never reach rollbackRename, which would rewrite the intact
// originals and strand a duplicate tree at newDir.
func TestVerifiedMove_LocalRenameFailureAborts(t *testing.T) {
	app := newTestApp(t)
	// Deliberately NOT forceNetworkRoot: local root, plain-rename branch.

	notebook := "VMNB"
	alphaID := "7c7c7c7c-8d8d-4e9e-8f4a-9b9b9b9b9b9b"
	seedConflictPage(t, app, notebook, "OldSec", "Alpha", alphaID, "alpha body")
	oldDir := filepath.Join(app.vaultPath, notebook, "OldSec")
	newDir := filepath.Join(app.vaultPath, notebook, "NewSec")
	alphaPath := conflictPagePath(app, notebook, "OldSec", "Alpha")
	alphaOrig, _ := os.ReadFile(alphaPath)

	copies := swapVerifiedMoveSeam(t)
	origLocal := verifiedMoveLocalRename
	verifiedMoveLocalRename = func(_, _ string) error {
		return fmt.Errorf("rename: device or resource busy")
	}
	t.Cleanup(func() { verifiedMoveLocalRename = origLocal })

	err := app.RenameSection(notebook, "OldSec", "NewSec")
	if err == nil {
		t.Fatal("local rename failure must surface an error")
	}
	var ipc *IPCError
	if errors.As(err, &ipc) {
		t.Fatalf("local rename failure must stay a plain error, got typed %s: %v", ipc.Code, err)
	}
	if !strings.Contains(err.Error(), "device or resource busy") {
		t.Errorf("surfaced error lost the real cause: %v", err)
	}
	if !strings.Contains(err.Error(), "The section is unchanged at "+oldDir) {
		t.Errorf("tree-level abort clause missing: %v", err)
	}
	if *copies != 0 {
		t.Errorf("verified file moves = %d, want 0 on a local root (a rollback would rewrite the intact originals)", *copies)
	}
	if got, rerr := os.ReadFile(alphaPath); rerr != nil || string(got) != string(alphaOrig) {
		t.Errorf("Alpha not intact at old dir after local abort (err=%v)", rerr)
	}
	if _, serr := os.Stat(newDir); !os.IsNotExist(serr) {
		t.Errorf("NewSec exists after local abort (stat err = %v)", serr)
	}
}
