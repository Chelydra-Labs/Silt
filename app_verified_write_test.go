package main

// Verified-write mode tests (#1009): per-root detection through the
// rootNetworkFSCheck seam, the trackedPageWrite dispatch (verified roots get
// backup+verify, local roots stay on the plain staged sequence), the typed
// hard-failure error, hazard-2 fingerprint refresh inside the write
// transaction, backup invisibility to indexing, and the one-time tip event.

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/vault"
)

// forceNetworkRoot swaps the detection seam to report every probe as a
// network mount (a CI temp dir cannot reproduce one) and registers the probe
// outcome for root.
func forceNetworkRoot(t *testing.T, app *App, root string) {
	t.Helper()
	orig := rootNetworkFSCheck
	rootNetworkFSCheck = func(string) error {
		return errors.New("network filesystem detected: simulated mount")
	}
	t.Cleanup(func() { rootNetworkFSCheck = orig })
	app.detectVerifiedWriteRoot(root)
}

// captureVerifiedTipEvents swaps the event emitter and marks the frontend
// ready so emitOrQueue emits live; returns a collector for
// storage:verified-write payloads.
func captureVerifiedTipEvents(t *testing.T, app *App) *[]VerifiedWriteTipPayload {
	t.Helper()
	var got []VerifiedWriteTipPayload
	orig := app.eventEmit
	app.eventEmit = func(name string, data ...any) {
		if name == string(EventStorageVerifiedWrite) {
			if p, ok := data[0].(VerifiedWriteTipPayload); ok {
				got = append(got, p)
			}
		}
	}
	t.Cleanup(func() { app.eventEmit = orig })
	app.startupEventsMu.Lock()
	app.frontendReady = true
	app.startupEventsMu.Unlock()
	t.Cleanup(func() {
		app.startupEventsMu.Lock()
		app.frontendReady = false
		app.startupEventsMu.Unlock()
	})
	return &got
}

// 1. Forced network FS → verified write: backup created, verify passes, and
// the backup generation is swept (replaced) by the next write.
func TestVerifiedWrite_ForcedNetworkBackupCreatedAndSwept(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "VNB", "Sub", "BackupSweep"
	blockID := "ccccccc3-cccc-4ccc-8ccc-cccccccccccc"
	seedConflictPage(t, app, notebook, section, page, blockID, "first body")
	filePath := conflictPagePath(app, notebook, section, page)
	original, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	backup := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, section, page+".md")

	// First verified write: the pre-write (seed) bytes become the backup.
	blocks2 := seedBlocksFrom(t, app, notebook, section, page, "second body")
	if err := app.SaveFileBlocks(notebook, section, page, blocks2); err != nil {
		t.Fatalf("verified save 1: %v", err)
	}
	gotBackup, err := os.ReadFile(backup)
	if err != nil {
		t.Fatalf("backup after write 1: %v", err)
	}
	if string(gotBackup) != string(original) {
		t.Errorf("backup after write 1 = %q, want the pre-write bytes", string(gotBackup))
	}

	// Second verified write: the backup generation is swept — it now holds
	// write 1's result, not the seed bytes.
	blocks3 := seedBlocksFrom(t, app, notebook, section, page, "third body")
	if err := app.SaveFileBlocks(notebook, section, page, blocks3); err != nil {
		t.Fatalf("verified save 2: %v", err)
	}
	gotBackup, err = os.ReadFile(backup)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotBackup) == string(original) {
		t.Errorf("backup still holds the swept generation after write 2")
	}
	if !strings.Contains(string(gotBackup), "second body") {
		t.Errorf("backup after write 2 = %q, want write 1's content", string(gotBackup))
	}
	onDisk, _ := os.ReadFile(filePath)
	if !strings.Contains(string(onDisk), "third body") {
		t.Errorf("target = %q, want write 2's content", string(onDisk))
	}
}

// 2. Torn destination (write + retry both torn, restore clean) → typed error,
// original intact, pending copy retained, no partial page.
func TestVerifiedWrite_TornDestinationRestoresOriginal(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "VNB", "", "TornDest"
	blockID := "ddddddd4-dddd-4ddd-8ddd-dddddddddddd"
	seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	filePath := conflictPagePath(app, notebook, section, page)
	original, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	renames := 0
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		renames++
		if renames <= 2 { // write + retry torn; the restore rename stays clean
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	blocks := seedBlocksFrom(t, app, notebook, section, page, "my precious new text")
	err = app.SaveFileBlocks(notebook, section, page, blocks)
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error, got %v", CodeVerifiedWriteFailed, err)
	}

	onDisk, rerr := os.ReadFile(filePath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(onDisk) != string(original) {
		t.Fatalf("original not restored:\n got: %q\nwant: %q", string(onDisk), string(original))
	}
	pendingPath := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, section, page+".md.pending")
	pending, perr := os.ReadFile(pendingPath)
	if perr != nil {
		t.Fatalf("no retained pending copy at %s: %v", pendingPath, perr)
	}
	if !strings.Contains(string(pending), "my precious new text") {
		t.Errorf("pending copy = %q, want the new text retained", string(pending))
	}
	if !strings.Contains(ipc.Message, ".system") || !strings.Contains(ipc.Message, "writebackups") {
		t.Errorf("error must name where the retained copies live: %q", ipc.Message)
	}

	// Hazard 1c self-heal: the failed save skipped the fingerprint refresh,
	// and the restore put the exact baseline bytes back — the next save must
	// pass through the hash arbiter despite the stat drift from the torn
	// writes + restore, not mint a phantom conflict.
	retry := seedBlocksFrom(t, app, notebook, section, page, "retry body")
	if err := app.SaveFileBlocks(notebook, section, page, retry); err != nil {
		t.Fatalf("post-restore next save must self-heal, got %v", err)
	}
}

// 3. Hard failure (restore also torn) → typed error, backup + pending both
// retained on disk for manual recovery.
func TestVerifiedWrite_HardFailureRetainsTempAndBackup(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "VNB", "", "HardFail"
	blockID := "eeeeeee5-eeee-4eee-8eee-eeeeeeeeeeee"
	seedConflictPage(t, app, notebook, section, page, blockID, "keep me safe")
	backup := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, section, page+".md")

	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		// Every rename on this mount tears.
		return os.WriteFile(path, []byte("TORN"), 0o644)
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	blocks := seedBlocksFrom(t, app, notebook, section, page, "doomed write")
	err := app.SaveFileBlocks(notebook, section, page, blocks)
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error, got %v", CodeVerifiedWriteFailed, err)
	}
	if got, rerr := os.ReadFile(backup); rerr != nil || !strings.Contains(string(got), "keep me safe") {
		t.Errorf("backup not retained: %v", rerr)
	}
	if got, rerr := os.ReadFile(backup + ".pending"); rerr != nil || !strings.Contains(string(got), "doomed write") {
		t.Errorf("pending copy not retained: %v", rerr)
	}
}

// 4. Local root (#1009 AC3): NO backup, NO read-back (counter seam), the
// plain staged sequence is unchanged.
func TestVerifiedWrite_LocalRootPlainSequenceUnchanged(t *testing.T) {
	app := newTestApp(t)
	orig := rootNetworkFSCheck
	rootNetworkFSCheck = func(string) error { return nil }
	t.Cleanup(func() { rootNetworkFSCheck = orig })
	app.detectVerifiedWriteRoot(app.vaultPath)

	var verifiedPasses atomic.Int64
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		verifiedPasses.Add(1)
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	notebook, section, page := "VNB", "", "LocalPlain"
	blockID := "fffffff6-ffff-4fff-8fff-ffffffffffff"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "local body")

	edited := editBlocks(blocks, blockID, "local body edited")
	if err := app.SaveFileBlocks(notebook, section, page, edited); err != nil {
		t.Fatalf("local save: %v", err)
	}
	if n := verifiedPasses.Load(); n != 0 {
		t.Errorf("verified mode ran on a local root (%d rename hooks fired)", n)
	}
	if _, err := os.Stat(filepath.Join(app.vaultPath, ".system", "writebackups")); !os.IsNotExist(err) {
		t.Errorf("local write created backups: %v", err)
	}
	onDisk, _ := os.ReadFile(conflictPagePath(app, notebook, section, page))
	if !strings.Contains(string(onDisk), "local body edited") {
		t.Errorf("target = %q, want the edited body", string(onDisk))
	}
}

// 5. Hazard 2: a verified write refreshes the load fingerprint inside the
// same transaction — the next editor save is clean — while a real external
// edit afterwards still refuses (the gate is not disarmed).
func TestVerifiedWrite_Hazard2FingerprintRefreshedInTransaction(t *testing.T) {
	app := newTestApp(t)
	notebook, section, page := "VNB", "", "HazTwo"
	blockID := "11111111-2222-4333-8444-555555555555"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "base body")

	// Editor load — snapshots the baseline the gate checks against.
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}

	forceNetworkRoot(t, app, app.vaultPath)

	// Verified write through the funnel (Silt's own write).
	first := editBlocks(blocks, blockID, "verified edit one")
	if err := app.SaveFileBlocks(notebook, section, page, first); err != nil {
		t.Fatalf("verified save: %v", err)
	}

	// The next editor save must pass cleanly: the post-write fingerprint
	// refresh happened inside the same lock as the verification read.
	second := editBlocks(blocks, blockID, "verified edit two")
	if err := app.SaveFileBlocks(notebook, section, page, second); err != nil {
		t.Fatalf("subsequent save after verified write was not clean (fingerprint not refreshed): %v", err)
	}

	// A genuine external edit still refuses — refresh did not disarm the gate.
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"VNB\"\nsection: \"\"\npage: \"HazTwo\"\ndate: \"2026-01-01\"\ntags: []\n---\nexternal rewrite <!-- id: 99999999-8888-4777-8666-555555555555 -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	third := editBlocks(blocks, blockID, "my unsaved text")
	requireConflict(t, app.SaveFileBlocks(notebook, section, page, third))
}

// 6. Backups under .system/ never surface as pages (WalkMarkdown feeds every
// indexing path, cold start and link alike).
func TestVerifiedWrite_BackupsNeverIndexed(t *testing.T) {
	vaultRoot := t.TempDir()
	linkedRoot := t.TempDir()
	for _, root := range []string{vaultRoot, linkedRoot} {
		nb := filepath.Join(root, "NB")
		if err := os.MkdirAll(nb, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(nb, "real.md"), []byte("real page\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		backup := filepath.Join(root, ".system", "writebackups", "NB", "real.md")
		if err := os.MkdirAll(filepath.Dir(backup), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backup, []byte("backup copy\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(backup+".pending", []byte("pending copy\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{vaultRoot, linkedRoot} {
		files, _, _, err := parser.WalkMarkdown(root)
		if err != nil {
			t.Fatalf("WalkMarkdown(%s): %v", root, err)
		}
		for _, f := range files {
			if strings.Contains(f, ".system") {
				t.Errorf("backup surfaced as a page: %s", f)
			}
		}
		if len(files) != 1 {
			t.Errorf("WalkMarkdown(%s) = %v, want exactly the real page", root, files)
		}
	}
}

// 6a. Nested detected roots (a linked notebook inside the vault root) must
// resolve deterministically to the innermost root: map-order iteration
// would flip the chosen root per call, and a .pending copy retained under
// one root is never swept by a success resolved to the other.
func TestVerifiedWrite_NestedRootsResolveDeterministically(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)
	inner := filepath.Clean(filepath.Join(app.vaultPath, "LinkedNB"))
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	forceNetworkRoot(t, app, inner)

	target := filepath.Join(inner, "page.md")
	for i := 0; i < 200; i++ {
		root, rel, ok := app.verifiedWriteScope(target)
		if !ok || root != inner || rel != "page.md" {
			t.Fatalf("scope resolved to (%q, %q, %v), want the innermost root %q with rel %q",
				root, rel, ok, inner, "page.md")
		}
	}
}

// F3 (iter-2): a root that cannot be probed at all (offline/unmounted at
// open or link) must fail safe — recorded NETWORK, so the safety net survives
// the mount coming back mid-session — while a statable local root stays
// LOCAL. On the pre-fix code db.IsNetworkFS swallowed the stat failure
// (returned nil) and the root silently lost the safety net.
func TestVerifiedWrite_UnprobeableRootFailsSafe(t *testing.T) {
	app := newTestApp(t)
	gone := filepath.Join(app.vaultPath, "OfflineNB")
	app.detectVerifiedWriteRoot(gone) // default seam: stat fails → probe error
	app.verifiedRootsMu.RLock()
	goneNetwork := app.rootStorageProbes[filepath.Clean(gone)]
	vaultNetwork := app.rootStorageProbes[filepath.Clean(app.vaultPath)]
	app.verifiedRootsMu.RUnlock()
	if !goneNetwork {
		t.Errorf("unprobeable root recorded LOCAL — the safety net is silently lost if the mount returns non-atomic")
	}
	if vaultNetwork {
		t.Errorf("statable local vault root recorded NETWORK")
	}
}

// The 200-probe test above pins innermost determinism; these cases pin the
// two load-bearing behaviors it does not cover: a file under the OUTER root
// while an inner root exists, and a string-prefix-but-not-component sibling
// (/vault vs /vault-x). Both guard against a future "simplification" to
// naive prefix matching.
func TestVerifiedWrite_NestedRootsResolutionCases(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)
	inner := filepath.Clean(filepath.Join(app.vaultPath, "LinkedNB"))
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	forceNetworkRoot(t, app, inner)
	sibling := filepath.Clean(app.vaultPath + "-x")
	forceNetworkRoot(t, app, sibling)

	cases := []struct {
		name     string
		file     string
		wantRoot string
		wantRel  string
	}{
		{"file under outer root beside inner", filepath.Join(app.vaultPath, "Top.md"), filepath.Clean(app.vaultPath), "Top.md"},
		{"file inside inner root", filepath.Join(inner, "Deep.md"), inner, "Deep.md"},
		{"string-prefix sibling is not a parent", filepath.Join(sibling, "Page.md"), sibling, "Page.md"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 0; i < 20; i++ {
				root, rel, ok := app.verifiedWriteScope(tc.file)
				if !ok || root != tc.wantRoot || rel != tc.wantRel {
					t.Fatalf("scope(%s) = (%q, %q, %v), want (%q, %q, true)",
						tc.file, root, rel, ok, tc.wantRoot, tc.wantRel)
				}
			}
		})
	}
}

// Detection at link time + forget at unlink.
func TestVerifiedWrite_LinkDetectionLifecycle(t *testing.T) {
	app := newTestApp(t)
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes", "a.md"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orig := rootNetworkFSCheck
	rootNetworkFSCheck = func(p string) error {
		if p == root {
			return errors.New("simulated network mount")
		}
		return nil
	}
	t.Cleanup(func() { rootNetworkFSCheck = orig })

	ln, err := app.LinkNotebook(root)
	if err != nil {
		t.Fatalf("LinkNotebook: %v", err)
	}
	app.verifiedRootsMu.RLock()
	network := app.rootStorageProbes[root]
	app.verifiedRootsMu.RUnlock()
	if !network {
		t.Fatalf("linked network root not detected at link time")
	}

	if err := app.UnlinkNotebook(ln.ID); err != nil {
		t.Fatalf("UnlinkNotebook: %v", err)
	}
	app.verifiedRootsMu.RLock()
	_, probed := app.rootStorageProbes[root]
	app.verifiedRootsMu.RUnlock()
	if probed {
		t.Errorf("unlink left the root's storage probe behind")
	}
}

// Tip: emitted once per root per session, not when already dismissed.
func TestVerifiedWrite_TipEmittedOncePerRoot(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)
	got := captureVerifiedTipEvents(t, app)

	notebook, section, page := "VNB", "", "TipOnce"
	blockID := "aaaaaaa7-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "tip body")
	for i := 0; i < 3; i++ {
		edited := editBlocks(blocks, blockID, "tip body "+strings.Repeat("x", i+1))
		if err := app.SaveFileBlocks(notebook, section, page, edited); err != nil {
			t.Fatalf("save %d: %v", i, err)
		}
	}
	if len(*got) != 1 {
		t.Fatalf("tip events = %d, want exactly one for the session", len(*got))
	}
	want := VerifiedWriteTipID(app.vaultPath)
	if (*got)[0].TipID != want || (*got)[0].Root != app.vaultPath {
		t.Errorf("tip payload = %+v, want {root: %s, tip_id: %s}", (*got)[0], app.vaultPath, want)
	}

	// Already dismissed for this root → no emission at all.
	app2 := newTestApp(t)
	forceNetworkRoot(t, app2, app2.vaultPath)
	app2.configMu.Lock()
	app2.cfg.UI.DismissedTips = []string{VerifiedWriteTipID(app2.vaultPath)}
	app2.configMu.Unlock()
	got2 := captureVerifiedTipEvents(t, app2)
	blocks2 := seedConflictPage(t, app2, "VNB", "", "TipDismissed", blockID, "tip body")
	if err := app2.SaveFileBlocks("VNB", "", "TipDismissed", editBlocks(blocks2, blockID, "edited")); err != nil {
		t.Fatalf("save: %v", err)
	}
	if len(*got2) != 0 {
		t.Errorf("dismissed tip re-emitted: %+v", *got2)
	}
}

// #1020: a FUSE-classified probe (what db.IsNetworkFSForWrite now returns for
// a sshfs/rclone root on Linux) must engage verified mode and fire the
// degraded-safety tip like any other network mount — the machinery classifies
// by the probe error, not by mount type. The seam carries the real extended
// detector's error shape (ErrNetworkFilesystem-wrapped, FUSE magic) because a
// real FUSE mount cannot be reproduced on CI.
func TestVerifiedWrite_FUSEProbeEngagesVerifiedModeAndTip(t *testing.T) {
	app := newTestApp(t)
	orig := rootNetworkFSCheck
	rootNetworkFSCheck = func(p string) error {
		return fmt.Errorf("%w: %s is on a network filesystem (type 0x%x). Renames may not be atomic there, so page writes use the verified (backup + read-back) mode",
			db.ErrNetworkFilesystem, p, 0x65735546)
	}
	t.Cleanup(func() { rootNetworkFSCheck = orig })
	app.detectVerifiedWriteRoot(app.vaultPath)

	var verifiedPasses atomic.Int64
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		verifiedPasses.Add(1)
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	tips := captureVerifiedTipEvents(t, app)

	notebook, section, page := "VNB", "", "FuseRoot"
	blockID := "bbbbbbb8-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "fuse body")
	if err := app.SaveFileBlocks(notebook, section, page, editBlocks(blocks, blockID, "fuse body edited")); err != nil {
		t.Fatalf("verified save on FUSE root: %v", err)
	}

	if n := verifiedPasses.Load(); n == 0 {
		t.Errorf("FUSE-classified root did not engage verified mode (no rename hook fired)")
	}
	backup := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, section, page+".md")
	if gotBackup, err := os.ReadFile(backup); err != nil || !strings.Contains(string(gotBackup), "fuse body") {
		t.Errorf("pre-write backup not created on FUSE root: %v", err)
	}
	if len(*tips) != 1 {
		t.Fatalf("tip events = %d, want exactly one", len(*tips))
	}
	want := VerifiedWriteTipID(app.vaultPath)
	if (*tips)[0].TipID != want || (*tips)[0].Root != app.vaultPath {
		t.Errorf("tip payload = %+v, want {root: %s, tip_id: %s}", (*tips)[0], app.vaultPath, want)
	}
}

// F1 residual (iter-2 fix-now #1): MovePage's post-move `section:` rewrite
// failing a verified write must surface the typed error and abort before
// clearAndReindex — logging + dropping it reported a successful move while
// indexing the untrusted bytes at the new path.
func TestVerifiedWrite_MovePageTornFrontmatterRewriteFailsLoud(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, page := "VNB", "TornMove"
	blockID := "9a9a9a9a-1b1b-4c1c-8d1d-2e2e2e2e2e2e"
	seedConflictPage(t, app, notebook, "From", page, blockID, "move me safely")
	oldPath := conflictPagePath(app, notebook, "From", page)
	moved, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}

	renames := 0
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		renames++
		// Rename #1 is the verified MOVE's staged write (clean — the move
		// must land so this test stays on its scenario: a torn FRONTMATTER
		// rewrite behind a completed move). Renames #2–3 tear the rewrite +
		// its retry; the restore rename stays clean.
		if renames >= 2 && renames <= 3 {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err = app.MovePage(notebook, "From", "DestSec", page)
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("MovePage must fail with the typed verified error, got %v", err)
	}

	// The moved bytes must sit intact at the new path (restored) — and the
	// rewritten content is retained for recovery.
	newPath := conflictPagePath(app, notebook, "DestSec", page)
	onDisk, rerr := os.ReadFile(newPath)
	if rerr != nil {
		t.Fatalf("moved page missing after torn rewrite: %v", rerr)
	}
	if string(onDisk) != string(moved) {
		t.Fatalf("moved bytes not restored at the new path:\n got: %q\nwant: %q", string(onDisk), string(moved))
	}
	pending := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, "DestSec", page+".md.pending")
	gotPending, perr := os.ReadFile(pending)
	if perr != nil {
		t.Fatalf("no retained pending copy at %s: %v", pending, perr)
	}
	if !strings.Contains(string(gotPending), "DestSec") {
		t.Errorf("pending copy = %q, want the rewritten frontmatter retained", string(gotPending))
	}
	// The abort landed before clearAndReindex: the new location must not be
	// indexed as a landed move.
	if idx, ferr := app.FetchPageBlocks(notebook, "DestSec", page); ferr == nil && len(idx) > 0 {
		t.Errorf("untrusted move must not be indexed at the new section: %d blocks", len(idx))
	}
}

// Iter-2 fix-now #5: DuplicatePage's target write failing a verified write
// must keep the typed code and recovery prose — the generic
// navigation_duplicate wrap erased both, and the frontend's fixed copy for
// that code dropped the recovery path entirely.
func TestVerifiedWrite_DuplicatePageTornWriteKeepsTypedError(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "VNB", "", "TornDup"
	blockID := "8b8b8b8b-2c2c-4d2d-8e3e-4f4f4f4f4f4f"
	seedConflictPage(t, app, notebook, section, page, blockID, "duplicate me safely")

	renames := 0
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		renames++
		if strings.HasSuffix(path, "TornDupCopy.md") && renames <= 2 {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err := app.DuplicatePage(notebook, section, page, "TornDupCopy")
	var ipc *IPCError
	if !errors.As(err, &ipc) {
		t.Fatalf("want IPCError, got %v", err)
	}
	if ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("code = %s, want %s (typed passthrough, not the re-wrapped navigation_duplicate)", ipc.Code, CodeVerifiedWriteFailed)
	}
	if !strings.Contains(ipc.Message, "TornDupCopy") || !strings.Contains(ipc.Message, "writebackups") {
		t.Errorf("recovery prose must name the file and where the retained copy lives: %q", ipc.Message)
	}
}

// seedBlocksFrom re-reads the page's current blocks so a test can edit from
// the latest on-disk generation (SaveFileBlocks renders whole pages).
func seedBlocksFrom(t *testing.T, app *App, notebook, section, page, body string) []parser.ParsedBlock {
	t.Helper()
	rows, err := app.FetchPageBlocks(notebook, section, page)
	if err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	if len(rows) == 0 {
		t.Fatalf("no blocks to edit")
	}
	blockID := rows[0].ID
	return editBlocks(rows, blockID, body)
}

// F1 (#1009): the rename frontmatter rewrite is a Silt-owned page write, and
// the old file is already consumed by os.Rename when it runs — a torn write
// at the new path would be unrecoverable page loss. On a forced-network root
// the rewrite must land in verified mode: torn → restored to the moved bytes,
// rewritten content retained as .pending, typed error surfaced.
func TestVerifiedWrite_RenamePageTornFrontmatterRewriteRestores(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "VNB", "", "TornRename"
	blockID := "1f1f1f1f-2f2f-4f2f-8f2f-3f3f3f3f3f3f"
	seedConflictPage(t, app, notebook, section, page, blockID, "rename me safely")
	oldPath := conflictPagePath(app, notebook, section, page)
	moved, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}

	renames := 0
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		renames++
		// Rename #1 is the verified MOVE's staged write (clean — the move
		// must land so this test stays on its scenario: a torn FRONTMATTER
		// rewrite behind a completed rename). Renames #2–3 tear the rewrite +
		// its retry; the restore rename stays clean.
		if renames >= 2 && renames <= 3 {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err = app.RenamePage(notebook, section, page, "TornRenameNew")
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error, got %v", CodeVerifiedWriteFailed, err)
	}

	// The moved bytes must sit intact at the new path — no torn partial.
	newPath := conflictPagePath(app, notebook, section, "TornRenameNew")
	onDisk, rerr := os.ReadFile(newPath)
	if rerr != nil {
		t.Fatalf("new page missing after torn rewrite: %v", rerr)
	}
	if string(onDisk) != string(moved) {
		t.Fatalf("moved bytes not restored at the new path:\n got: %q\nwant: %q", string(onDisk), string(moved))
	}
	if _, serr := os.Stat(oldPath); !os.IsNotExist(serr) {
		t.Errorf("old path still holds a file: %v", serr)
	}
	// The rewritten content (new page: frontmatter) is retained for recovery.
	pending := filepath.Join(app.vaultPath, ".system", "writebackups", notebook, section, "TornRenameNew.md.pending")
	gotPending, perr := os.ReadFile(pending)
	if perr != nil {
		t.Fatalf("no retained pending copy at %s: %v", pending, perr)
	}
	if !strings.Contains(string(gotPending), "TornRenameNew") {
		t.Errorf("pending copy = %q, want the rewritten frontmatter retained", string(gotPending))
	}
}

// F1 (#1009): the watcher's normalization write is wired (vault init) to the
// app's per-root staged-write dispatcher. Runs the real initializeVaultServices
// so the production wiring is what's under test, then forces a network root
// via the probe seam and a torn write via the rename hook: the callback must
// run verified mode and restore the original.
func TestVerifiedWrite_WatcherNormalizationWriteVerifiedOnNetworkRoot(t *testing.T) {
	vaultPath := t.TempDir()
	t.Setenv("SILT_DATA_DIR", t.TempDir())
	if err := vault.ScaffoldVault(vaultPath); err != nil {
		t.Fatalf("ScaffoldVault: %v", err)
	}

	// Swap the probe BEFORE init so the production open path itself records
	// the root as a network mount.
	orig := rootNetworkFSCheck
	rootNetworkFSCheck = func(string) error {
		return errors.New("network filesystem detected: simulated mount")
	}
	t.Cleanup(func() { rootNetworkFSCheck = orig })

	// The note is seeded BEFORE init: a post-init os.WriteFile would fire a
	// live fsnotify event whose reindex consumes the torn-hook counter below
	// before the direct call under test runs.
	filePath := filepath.Join(vaultPath, "Work", "Journal", "Daily.md")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("# Journal\n\n- [ ] external sync dropped this in\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app := &App{spacesPerTab: 4}
	if err := app.initializeVaultServices(vaultPath); err != nil {
		t.Fatalf("initializeVaultServices: %v", err)
	}
	t.Cleanup(func() { _ = app.CloseVault() })

	if app.watcher == nil || app.watcher.WritePageFile == nil {
		t.Fatalf("normalization write not wired to the staged-write dispatcher")
	}

	// The live watcher may reindex this file off its own init-scan events and
	// call the wired writer concurrently with the direct call under test
	// (racing the shared torn-hook counter). Suppress it the production way:
	// an editor holding focus makes reindexFile stand down for the path.
	app.watcher.LockFocus(filePath)
	t.Cleanup(func() { app.watcher.UnlockFocus(filePath) })

	original, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}

	renames := 0
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		renames++
		if renames <= 2 { // write + retry torn; the restore rename stays clean
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	err = app.watcher.WritePageFile(filePath, []byte("# Journal\n\n- [ ] external sync dropped this in <!-- id: minted -->\n"))
	var ipc *IPCError
	if !errors.As(err, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("want typed %s error, got %v", CodeVerifiedWriteFailed, err)
	}
	onDisk, rerr := os.ReadFile(filePath)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if string(onDisk) != string(original) {
		t.Fatalf("external content not restored after torn normalization write:\n got: %q\nwant: %q", string(onDisk), string(original))
	}
}

// 8. Conflict resolution × torn verified replay failure (keep_mine, blocks
// funnel): the landed-detection heuristic reads target bytes ≠ theirs as
// "mine landed", but a torn verified failure also produces that — partial
// bytes. The resolution must fail loud, keep theirs in the sibling, and not
// re-baseline to the torn content.
func TestResolveConflict_TornVerifiedReplayFailsLoud(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "ResVW", "", "TornReplay"
	blockID := "22222222-3333-4444-8555-666666666666"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	mine := editBlocks(blocks, blockID, "my unsaved line")
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ResVW\"\nsection: \"\"\npage: \"TornReplay\"\ndate: \"2026-01-01\"\ntags: []\n---\nexternal line <!-- id: 99999999-aaaa-4bbb-8ccc-666666666666 -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page, mine))

	// Tear every rename on the ORIGINAL path: replay write, retry, and
	// restore all land torn (VerifiedWriteError with Restored=false). The
	// archive copy to the sibling stays clean.
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		if path == filePath {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	copyPage, rerr := app.ResolveExternalConflict(notebook, section, page, "keep_mine")
	var ipc *IPCError
	if !errors.As(rerr, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("resolution must fail with the typed verified error, got copy=%q err=%v", copyPage, rerr)
	}

	// theirs survives in the sibling — the funnel must not delete it just
	// because the torn bytes differ from theirs.
	sibling := findConflictSibling(t, app, notebook, section, page)
	sibBytes, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sibBytes), "external line") {
		t.Fatalf("sibling must keep theirs, got %q", string(sibBytes))
	}

	// The torn bytes must not have been adopted: no re-baseline to partial
	// content — the next save still refuses against the torn disk state.
	requireConflict(t, app.SaveFileBlocks(notebook, section, page, mine))
}

// 9. Same torn replay through the markdown funnel: the typed code must
// survive historyWriteError's generic write-error mapping, or the funnel
// cannot tell a torn verified failure from an ordinary failed write.
func TestResolveConflict_TornVerifiedReplayFailsLoud_MarkdownFunnel(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "ResVW", "", "TornReplayMd"
	blockID := "33333333-4444-4555-8666-777777777777"
	seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ResVW\"\nsection: \"\"\npage: \"TornReplayMd\"\ndate: \"2026-01-01\"\ntags: []\n---\nexternal line <!-- id: 88888888-aaaa-4bbb-8ccc-777777777777 -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := app.SavePageMarkdown(notebook, section, page, "my markdown body\n")
	requireConflict(t, err)

	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		if path == filePath {
			return os.WriteFile(path, []byte("TORN-PARTIAL"), 0o644)
		}
		return nil
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	copyPage, rerr := app.ResolveExternalConflict(notebook, section, page, "keep_mine")
	var ipc *IPCError
	if !errors.As(rerr, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("resolution must fail with the typed verified error, got copy=%q err=%v", copyPage, rerr)
	}

	sibling := findConflictSibling(t, app, notebook, section, page)
	sibBytes, err := os.ReadFile(sibling)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sibBytes), "external line") {
		t.Fatalf("sibling must keep theirs, got %q", string(sibBytes))
	}
}

// 10. Torn verified failure on the archive copy (keep_mine): the stat-only
// landed check sees the torn sibling file, but its bytes are partial — the
// resolution must fail rather than index torn bytes as theirs' copy and
// replay mine over theirs' only complete version.
func TestResolveConflict_TornVerifiedConflictCopyFailsLoud(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "ResVW", "", "TornCopy"
	blockID := "44444444-5555-4666-8777-888888888888"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	mine := editBlocks(blocks, blockID, "my unsaved line")
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ResVW\"\nsection: \"\"\npage: \"TornCopy\"\ndate: \"2026-01-01\"\ntags: []\n---\nexternal line <!-- id: 77777777-aaaa-4bbb-8ccc-888888888888 -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page, mine))

	// Simulate a torn NEW sibling page whose cleanup also fails: every
	// rename onto the sibling leaves un-removable garbage, so the verified
	// writer ends with Restored=false and a stat-able (garbage) sibling —
	// the one sibling shape the stat-only landed check misreads.
	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		if path == filePath {
			return nil
		}
		_ = os.RemoveAll(path)
		return os.MkdirAll(filepath.Join(path, "torn"), 0o700)
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	copyPage, rerr := app.ResolveExternalConflict(notebook, section, page, "keep_mine")
	var ipc *IPCError
	if !errors.As(rerr, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("resolution must fail with the typed verified error, got copy=%q err=%v", copyPage, rerr)
	}

	// The replay never ran: theirs still holds the original path.
	origBytes, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(origBytes), "external line") {
		t.Fatalf("theirs must stay on the original path, got %q", string(origBytes))
	}
}

// 11. keep_theirs with a torn sibling copy: same stat-only family — the copy
// of my stashed version tore; it must not be indexed as the conflict page,
// and the resolution must fail rather than settle theirs and report success.
func TestResolveConflict_KeepTheirs_TornVerifiedSiblingCopyFailsLoud(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	notebook, section, page := "ResVW", "", "TornKeepTheirs"
	blockID := "55555555-6666-4777-8888-999999999999"
	blocks := seedConflictPage(t, app, notebook, section, page, blockID, "original body")
	mine := editBlocks(blocks, blockID, "my unsaved line")
	if _, err := app.FetchPageBlocks(notebook, section, page); err != nil {
		t.Fatalf("FetchPageBlocks: %v", err)
	}
	filePath := conflictPagePath(app, notebook, section, page)
	external := "---\nnotebook: \"ResVW\"\nsection: \"\"\npage: \"TornKeepTheirs\"\ndate: \"2026-01-01\"\ntags: []\n---\nexternal line <!-- id: 66666666-aaaa-4bbb-8ccc-999999999999 -->\n"
	if err := os.WriteFile(filePath, []byte(external), 0o644); err != nil {
		t.Fatal(err)
	}
	requireConflict(t, app.SaveFileBlocks(notebook, section, page, mine))

	origHook := parser.VerifiedWriteAfterRenameHook
	parser.VerifiedWriteAfterRenameHook = func(path string) error {
		if path == filePath {
			return nil
		}
		_ = os.RemoveAll(path)
		return os.MkdirAll(filepath.Join(path, "torn"), 0o700)
	}
	t.Cleanup(func() { parser.VerifiedWriteAfterRenameHook = origHook })

	copyPage, rerr := app.ResolveExternalConflict(notebook, section, page, "keep_theirs")
	var ipc *IPCError
	if !errors.As(rerr, &ipc) || ipc.Code != CodeVerifiedWriteFailed {
		t.Fatalf("resolution must fail with the typed verified error, got copy=%q err=%v", copyPage, rerr)
	}

	// The garbage sibling must not be indexed as a page by the funnel.
	sibling := findConflictSibling(t, app, notebook, section, page)
	siblingPage := strings.TrimSuffix(filepath.Base(sibling), ".md")
	if idx, ferr := app.FetchPageBlocks(notebook, section, siblingPage); ferr == nil && len(idx) > 0 {
		t.Fatalf("torn sibling copy must not be indexed as a page, got %d blocks", len(idx))
	}
}

// #1019 advisory: a probe that completes after vault teardown must not record
// into the NEXT session's map (its stale classification would also suppress
// that session's own probe). The teardown epoch bump discards it.
func TestVerifiedWrite_ProbeAfterTeardownDoesNotSeedNextSession(t *testing.T) {
	app := newTestApp(t)
	root := t.TempDir()

	staleStarted := make(chan struct{})
	releaseStale := make(chan struct{})
	freshStarted := make(chan struct{})
	releaseFresh := make(chan struct{})
	orig := rootNetworkFSCheck
	var calls int
	rootNetworkFSCheck = func(string) error {
		calls++
		switch calls {
		case 1: // session A's probe: slow, classifies LOCAL
			close(staleStarted)
			<-releaseStale
			return nil
		default: // session B's probe: classifies NETWORK
			close(freshStarted)
			<-releaseFresh
			return errors.New("simulated network mount")
		}
	}
	t.Cleanup(func() { rootNetworkFSCheck = orig })

	staleDone := make(chan struct{})
	go func() {
		defer close(staleDone)
		app.detectVerifiedWriteRoot(root) // session A
	}()
	<-staleStarted

	// Teardown mid-probe: the session ends while A's probe is still blocked
	// on the dead mount.
	app.clearVerifiedWriteRoots()

	freshDone := make(chan struct{})
	go func() {
		defer close(freshDone)
		app.detectVerifiedWriteRoot(root) // session B
	}()
	<-freshStarted

	// Session A's stale result arrives FIRST — without the epoch guard it
	// would win first-record and pin the next session to LOCAL.
	close(releaseStale)
	<-staleDone
	close(releaseFresh)
	<-freshDone

	app.verifiedRootsMu.RLock()
	network, probed := app.rootStorageProbes[root]
	app.verifiedRootsMu.RUnlock()
	if !probed || !network {
		t.Errorf("root = (%v, probed=%v), want the fresh session's NETWORK classification to survive", network, probed)
	}
}

// #1019 advisory: backups of deleted pages are reclaimed at the session's
// probe (slow unbounded growth + lingering deleted content otherwise), while
// backups of live pages and .pending recovery copies are kept.
func TestVerifiedWrite_OrphanedBackupsSwept(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	backupDir := filepath.Join(app.vaultPath, ".system", "writebackups", "NB", "Sub")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Live page: backup kept.
	if err := os.MkdirAll(filepath.Join(app.vaultPath, "NB", "Sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(app.vaultPath, "NB", "Sub", "Live.md"), []byte("live"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(backupDir, "Live.md"), []byte("live old"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deleted page: backup swept.
	if err := os.WriteFile(filepath.Join(backupDir, "Gone.md"), []byte("deleted page content"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Deleted page's retained copy: NEVER swept — it is the post-failure
	// recovery copy of text that exists nowhere else.
	if err := os.WriteFile(filepath.Join(backupDir, "Gone.md.pending"), []byte("unsaved text"), 0o644); err != nil {
		t.Fatal(err)
	}

	app.sweepOrphanedWriteBackups(app.vaultPath)

	if _, err := os.Stat(filepath.Join(backupDir, "Live.md")); err != nil {
		t.Errorf("live page's backup was swept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "Gone.md")); !os.IsNotExist(err) {
		t.Errorf("deleted page's backup not swept (stat err = %v)", err)
	}
	if _, err := os.Stat(filepath.Join(backupDir, "Gone.md.pending")); err != nil {
		t.Errorf("recovery .pending copy was swept: %v", err)
	}
}

// Wiring half of the sweep: it must run as part of a session's probe (once
// per root per session), not only when called directly.
func TestVerifiedWrite_OrphanSweepRunsAtProbe(t *testing.T) {
	app := newTestApp(t)
	forceNetworkRoot(t, app, app.vaultPath)

	backupDir := filepath.Join(app.vaultPath, ".system", "writebackups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		t.Fatal(err)
	}
	orphan := filepath.Join(backupDir, "RenamedAway.md")
	if err := os.WriteFile(orphan, []byte("old content of a page that no longer exists"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Simulate the next session: teardown clears the probe state, the probe
	// re-runs, and the sweep reclaims the orphan.
	app.clearVerifiedWriteRoots()
	forceNetworkRoot(t, app, app.vaultPath)

	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Errorf("orphaned backup survived the session probe (stat err = %v)", err)
	}
}
