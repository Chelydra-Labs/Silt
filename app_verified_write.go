package main

// Per-root non-atomic filesystem detection and the verified-write dispatch
// (#1009). Network mounts (NFS/SMB/FUSE) do not guarantee that the staged
// writer's rename is atomic, so a rename can leave a torn page file. Roots
// are probed once per session at open/link time; every page write under a
// detected root routes through parser.WriteFileAtomicVerified (pre-write
// backup + post-rename read-back + restore) instead of the plain staged
// write. Local roots keep the plain sequence byte-identical (#1009 AC3).
// Detection is warn-and-continue: a network root is never blocked, only
// degraded-safety-notified (one-time dismissible tip, logged at detection).

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
)

// rootNetworkFSCheck is the per-root network-filesystem probe. Package-level
// so tests can swap it to simulate a network mount (which a CI temp dir cannot
// reproduce), mirroring vault.networkFSCheck (#1009). Defaults to db.IsNetworkFS.
var rootNetworkFSCheck = db.IsNetworkFS

// writeBackupDirName is the root-relative directory holding verified-write
// backups: <root>/.system/writebackups/<relPath>. Same mount as the target —
// a backup on another mount would not survive the unmount that most needs it.
// `.system` is already skipped by the watcher and WalkMarkdown, so backups
// never surface as pages.
const writeBackupDirName = ".system/writebackups"

// detectVerifiedWriteRoots probes every writable root (the vault root plus
// each non-quarantined linked root) once per session and records which are
// non-atomic. Called from initializeVaultServices after config load and
// linked-notebook fingerprint verification, so quarantined links (unwritable
// until re-linked) are skipped; LinkNotebook re-probes on (re-)link.
func (a *App) detectVerifiedWriteRoots() {
	a.configMu.RLock()
	roots := []string{}
	if a.vaultPath != "" {
		roots = append(roots, a.vaultPath)
	}
	for _, ln := range a.cfg.LinkedNotebooks {
		if ln.RootPath == "" {
			continue
		}
		if _, quarantined := a.quarantinedLinks[ln.ID]; quarantined {
			continue
		}
		roots = append(roots, ln.RootPath)
	}
	a.configMu.RUnlock()
	for _, root := range roots {
		a.detectVerifiedWriteRoot(root)
	}
}

// detectVerifiedWriteRoot probes one root and records the outcome. Idempotent
// per session (each root is probed at most once, at open or link). A probe
// error records the root as NETWORK: verified mode is strictly additive (the
// same staged write wrapped in backup + read-back), so an unprobeable root
// keeps writing AND keeps the safety net rather than silently losing it
// (warn-and-continue, #1009).
func (a *App) detectVerifiedWriteRoot(root string) {
	if root == "" {
		return
	}
	key := filepath.Clean(root)
	a.verifiedRootsMu.Lock()
	if a.rootStorageProbes == nil {
		a.rootStorageProbes = make(map[string]bool)
	}
	if _, probed := a.rootStorageProbes[key]; probed {
		a.verifiedRootsMu.Unlock()
		return
	}
	err := rootNetworkFSCheck(key)
	// db.IsNetworkFS signals a network mount by returning a non-nil error
	// (its sentinel on platforms with a detector); nil means local.
	network := err != nil
	a.rootStorageProbes[key] = network
	a.verifiedRootsMu.Unlock()
	if network {
		log.Printf("verified-write: %s is on a non-atomic filesystem; page writes use backup+verify mode (reduced crash safety)", key)
	}
}

// verifiedWriteScope returns the detected non-atomic root containing filePath,
// plus filePath's path relative to that root, when the write should use the
// verified mode. ok=false → plain staged write (local path, byte-identical
// behavior, #1009 AC3). Lexical containment only: every page path is
// constructed by joining the same app-resolved root, so paths are
// session-consistent by construction.
func (a *App) verifiedWriteScope(filePath string) (root, rel string, ok bool) {
	if filePath == "" {
		return "", "", false
	}
	a.verifiedRootsMu.RLock()
	networks := make([]string, 0, len(a.rootStorageProbes))
	for r, network := range a.rootStorageProbes {
		if network {
			networks = append(networks, r)
		}
	}
	a.verifiedRootsMu.RUnlock()
	if len(networks) == 0 {
		return "", "", false
	}

	abs, err := filepath.Abs(filepath.Clean(filePath))
	if err != nil {
		return "", "", false
	}
	for _, r := range networks {
		rel, err := filepath.Rel(r, abs)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == "." {
			continue
		}
		return r, rel, true
	}
	return "", "", false
}

// writePageStaged is the write step of trackedPageWrite: the staged write
// itself, dispatched per root. Runs INSIDE the caller's LockFileWrite
// transaction — the verified mode's read-back and any restore must observe
// the same file state the transaction pinned (#1009 hazard 2), and its success
// still flows through refreshFingerprintAfterWrite in the same lock (the
// verification re-read is Silt's own write, never an external edit).
func (a *App) writePageStaged(filePath string, content []byte) error {
	root, rel, verified := a.verifiedWriteScope(filePath)
	if !verified {
		return atomicPageWrite(filePath, content)
	}
	a.notifyVerifiedWriteOnce(root)
	backupPath := filepath.Join(root, filepath.FromSlash(writeBackupDirName), rel)
	err := parser.WriteFileAtomicVerified(filePath, backupPath, content)
	var vwe *parser.VerifiedWriteError
	if errors.As(err, &vwe) {
		return verifiedWriteFailedError(vwe)
	}
	return err
}

// forgetVerifiedWriteRoot drops a root's detection state (unlink). A stale
// entry would keep routing writes through the verified mode for a root Silt
// can no longer write at all; a re-link re-probes.
func (a *App) forgetVerifiedWriteRoot(root string) {
	if root == "" {
		return
	}
	key := filepath.Clean(root)
	a.verifiedRootsMu.Lock()
	delete(a.rootStorageProbes, key)
	delete(a.verifiedTipNotified, key)
	a.verifiedRootsMu.Unlock()
}

// clearVerifiedWriteRoots drops all detection state (vault teardown). The
// maps are vault-session-scoped: the next vault's roots are re-probed at open.
func (a *App) clearVerifiedWriteRoots() {
	a.verifiedRootsMu.Lock()
	a.rootStorageProbes = nil
	a.verifiedTipNotified = nil
	a.verifiedRootsMu.Unlock()
}

// notifyVerifiedWriteOnce surfaces the degraded-safety tip on the first
// verified write to a root: emitted at most once per root per session, and
// not at all when the user dismissed it for this root (ui.dismissed_tips is
// vault-scoped config, so the dismissal persists across sessions).
func (a *App) notifyVerifiedWriteOnce(root string) {
	a.verifiedRootsMu.Lock()
	if a.verifiedTipNotified == nil {
		a.verifiedTipNotified = make(map[string]bool)
	}
	if a.verifiedTipNotified[root] {
		a.verifiedRootsMu.Unlock()
		return
	}
	a.verifiedTipNotified[root] = true
	a.verifiedRootsMu.Unlock()

	tipID := VerifiedWriteTipID(root)
	a.configMu.RLock()
	dismissed := a.cfg.UI.DismissedTips
	a.configMu.RUnlock()
	for _, id := range dismissed {
		if id == tipID {
			return
		}
	}
	a.emitOrQueue(EventStorageVerifiedWrite, VerifiedWriteTipPayload{Root: root, TipID: tipID})
}

// VerifiedWriteTipPayload is the storage:verified-write event body: the
// degraded root and the dismissed_tips ID that settles the tip.
type VerifiedWriteTipPayload struct {
	Root  string `json:"root"`
	TipID string `json:"tip_id"`
}

// VerifiedWriteTipID derives the stable dismissed_tips ID for a root. Hashed
// (not embedded) so arbitrary mount paths cannot grow the config slice with
// unbounded, filesystem-hostile strings.
func VerifiedWriteTipID(root string) string {
	sum := sha256.Sum256([]byte(filepath.Clean(root)))
	return "verified_write_network_" + hex.EncodeToString(sum[:6])
}
