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
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
)

// rootNetworkFSCheck is the per-root network-filesystem probe. Package-level
// so tests can swap it to simulate a network mount (which a CI temp dir cannot
// reproduce), mirroring vault.networkFSCheck (#1009). The default is the
// fail-safe wrapper below, not db.IsNetworkFSForWrite directly.
var rootNetworkFSCheck = probeRootNetworkFS

// errStorageProbeUnavailable marks a root that could not be probed at all
// (offline/unmounted at open or link time) — distinct from a probe that
// classified the root either way.
var errStorageProbeUnavailable = errors.New("storage probe unavailable")

// probeRootNetworkFS fails safe where db.IsNetworkFSForWrite stays silent:
// the detector returns nil not only for local filesystems but also when it
// cannot classify the root (stat/statfs/GetVolumeInformation failure on an
// offline mount, or a platform without a detector — e.g. macOS, which is not
// a supported platform and stays undetected). Recording nil for an
// unprobeable root would drop the verified-write safety net for a mount that
// may come back non-atomic mid-session, so a root that cannot even be
// stat'd is treated as non-atomic: verified mode is strictly additive (the
// same staged write wrapped in backup + read-back). The detector is the
// WRITE set (extended with Linux FUSE, #1020), not db.IsNetworkFS — that
// one stays conservative for the SQLite WAL gate and the vault mover.
func probeRootNetworkFS(path string) error {
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("%w: %s: %v", errStorageProbeUnavailable, path, err)
	}
	return db.IsNetworkFSForWrite(path)
}

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
// that returns an error — a detected network mount, or a root that cannot be
// probed at all (offline at open/link; see probeRootNetworkFS) — records the
// root as NETWORK: verified mode is strictly additive (the same staged write
// wrapped in backup + read-back), so an unprobeable root keeps writing AND
// keeps the safety net rather than silently losing it (warn-and-continue,
// #1009). What stays LOCAL is a root the write detector positively
// classifies as local — statable-but-unrecognized mounts (e.g. macOS, an
// unsupported platform) — and, on Windows, a root whose classification fails
// AFTER stat (GetVolumePathName/GetVolumeInformation errors return nil →
// LOCAL). Both are the residual detection-coverage gap of #1020 (which
// closed the Linux FUSE hole by extending the write detector).
func (a *App) detectVerifiedWriteRoot(root string) {
	if root == "" {
		return
	}
	key := filepath.Clean(root)
	epoch := a.sessionEpoch()
	a.verifiedRootsMu.Lock()
	if a.rootStorageProbes == nil {
		a.rootStorageProbes = make(map[string]bool)
	}
	if _, probed := a.rootStorageProbes[key]; probed {
		a.verifiedRootsMu.Unlock()
		return
	}
	a.verifiedRootsMu.Unlock()

	// Probe OUTSIDE the lock: on a dead hard mount the statfs can block up
	// to the mount timeout, and every verifiedWriteScope (RLock) would queue
	// behind it, stalling page writes across all roots for the probe.
	err := rootNetworkFSCheck(key)
	// A non-nil result means NETWORK: either the detector recognized a
	// network mount or the probe could not classify the root at all.
	network := err != nil

	recorded := false
	a.verifiedRootsMu.Lock()
	// Re-check after the unlocked window: a concurrent probe (or a teardown
	// clearing the map) may have recorded while this one ran; first record
	// wins, keeping the once-per-session idempotence. A teardown bumps the
	// epoch — a probe from the PREVIOUS vault session must not seed the next
	// session's map with a stale classification (which would also skip the
	// next session's own probe).
	if a.verifiedRootsEpoch == epoch {
		if a.rootStorageProbes == nil {
			a.rootStorageProbes = make(map[string]bool)
		}
		if _, probed := a.rootStorageProbes[key]; !probed {
			a.rootStorageProbes[key] = network
			recorded = true
		}
	}
	a.verifiedRootsMu.Unlock()
	// The orphan sweep runs once per root per session (this function early-
	// returns above for already-probed roots), after the probe, on the
	// session's own map state.
	a.sweepOrphanedWriteBackups(root)
	if !recorded || !network {
		return
	}
	if errors.Is(err, errStorageProbeUnavailable) {
		log.Printf("verified-write: %s could not be probed (offline or unmounted); treating it as non-atomic so page writes keep the backup+verify safety net until the next open/re-link", key)
		return
	}
	log.Printf("verified-write: %s is on a non-atomic filesystem; page writes use backup+verify mode (reduced crash safety)", key)
}

// sweepOrphanedWriteBackups removes pre-write backups whose target page no
// longer exists. The next write at the same relative path sweeps a backup
// naturally (O_TRUNC), but a page deleted or renamed away leaves its last
// backup orphaned forever — unbounded growth of a hidden directory that also
// retains deleted-page content. Runs once per root per session (after the
// probe); a root classified local this session may still carry backups from
// an earlier session in which it was detected network, so the sweep is
// unconditional. .pending copies are deliberately NOT swept: they are the
// post-failure recovery copy of text that exists nowhere else.
func (a *App) sweepOrphanedWriteBackups(root string) {
	if root == "" {
		return
	}
	backupRoot := filepath.Join(root, filepath.FromSlash(writeBackupDirName))
	_ = filepath.WalkDir(backupRoot, func(p string, d fs.DirEntry, err error) error {
		// An unreadable entry — or a missing backup dir, the common case for
		// roots that never wrote verified — has nothing to sweep; nil keeps
		// the walk going instead of aborting the whole sweep.
		if err != nil {
			return nil
		}
		if d.IsDir() || strings.HasSuffix(p, ".pending") {
			return nil
		}
		rel, relErr := filepath.Rel(backupRoot, p)
		if relErr != nil {
			return nil
		}
		target := filepath.Join(root, rel)
		if _, statErr := os.Stat(target); os.IsNotExist(statErr) {
			if rmErr := os.Remove(p); rmErr != nil {
				log.Printf("verified-write: could not remove orphaned backup %s: %v", p, rmErr)
			}
		}
		return nil
	})
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
	// Deterministic deepest-first. With nested detected roots (a linked
	// notebook inside the vault root), map-order iteration would resolve the
	// same page to different roots per call, and a .pending copy retained
	// under one root is never swept by a success resolved to the other —
	// breaking the one-generation sweep contract. For containing roots the
	// longer path is the deeper one, and lexicographic sort puts it last.
	sort.Strings(networks)

	abs, err := filepath.Abs(filepath.Clean(filePath))
	if err != nil {
		return "", "", false
	}
	for i := len(networks) - 1; i >= 0; i-- {
		rel, err := filepath.Rel(networks[i], abs)
		if err != nil {
			continue
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == "." {
			continue
		}
		return networks[i], rel, true
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
		// The user-facing prose deliberately omits the OS-level cause, and
		// the IPC error carries only the prose — this boundary is the last
		// place the diagnostic exists, so log it: a field report of a
		// failed save on a mount must be diagnosable (#1009).
		log.Printf("verified-write failed for %s (restored=%v): %v (retention: %v)",
			vwe.Target, vwe.Restored, vwe.Err, vwe.RetentionErr)
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

// sessionEpoch returns the current vault-session generation for the root
// probes. Guarded by verifiedRootsMu like the maps it invalidates.
func (a *App) sessionEpoch() uint64 {
	a.verifiedRootsMu.RLock()
	defer a.verifiedRootsMu.RUnlock()
	return a.verifiedRootsEpoch
}

// clearVerifiedWriteRoots drops all detection state (vault teardown). The
// maps are vault-session-scoped: the next vault's roots are re-probed at open.
// The epoch bump also discards probes still in flight from the closing
// session, so a slow/dead-mount probe cannot pollute the next session's map.
func (a *App) clearVerifiedWriteRoots() {
	a.verifiedRootsMu.Lock()
	a.rootStorageProbes = nil
	a.verifiedTipNotified = nil
	a.verifiedRootsEpoch++
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
