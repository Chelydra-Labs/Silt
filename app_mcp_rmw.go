package main

// External-edit protection for the MCP read-modify-write funnel (#1005).
//
// MCP page fetches deliberately do NOT capture the editor load baseline
// (captureNone in app_mcp.go): re-baselining on a client read would disarm
// the editor's gate behind its back. But that left the MCP side's own write
// unprotected — an UpdateBlocks whose handler started minutes after the
// fetch sailed through both gates and silently clobbered any external edit
// landing in between. This file gives the MCP funnel its own scoped basis:
// every fetch snapshots what the client actually observed (mcpReadBaselines
// — the served rows for blocks fetches, the raw bytes for markdown), and the
// MCP save funnel re-checks that basis inside LockFileWrite. Any committed
// Silt page write INVALIDATES the basis (strict read-modify-write, enforced
// at the trackedPageWrite choke point so editor, plugin, and batch writes
// count too): every write consumes the fetch it was built from, so
// overlapping clients each re-read before writing again.
//
// The basis slot is one per path and last-writer-wins, so the marker alone
// cannot outlive a newer fetch: after client A commits and re-reads (the
// normal RMW loop), the fresh basis would green-light client B's stale
// pre-commit update. Each fetch therefore stamps a monotonic per-path
// generation (mcpFetchGen), returned to the client as read_generation and
// echoed back in update_blocks; a committed write records the newest issued
// generation (mcpBasisInvalidatedGen) and any echoed generation at or below
// it refuses. Clients that do not echo (generation 0) keep the legacy
// contract — marker + fingerprint gates only, honest boundary: their basis
// is valid until the next fetch of ANY client. Delete/recreate, rename, and
// vault teardown reset the per-path generation sequence (app_tracked_write
// owns that lifecycle): a stale pre-delete echo can therefore alias a
// post-recreate generation once a third-party fetch re-arms the slot — the
// marker + fingerprint gates still refuse everything the aliasing would let
// through on the paths that matter. A drift refuses WITHOUT
// minting a stash (checkMCPClientBasis): the programmatic caller still holds
// its own payload and its typed external_edit_conflict error already tells
// it to re-read and retry — a sticky desktop-only stash would wedge exactly
// that recovery. The pending-stash and editor-baseline halves of the gate
// run separately via checkExternalEditConflict and keep their stashing
// semantics.

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"silt/backend/core"
	"silt/backend/parser"
)

// captureMCPReadBasis snapshots the bytes an MCP fetch observed for filePath
// (stat + hash of the raw file) and returns the fetch generation stamped onto
// that basis — the value the client echoes back in update_blocks
// (read_generation). Missing-file errors keep the absent-baseline semantics
// of captureFileFingerprint; other errors skip the capture (the write gate
// simply has nothing to compare — legacy behavior; generation 0 = unechoed).
func (a *App) captureMCPReadBasis(filePath string) int64 {
	if a == nil || filePath == "" {
		return 0
	}
	fp, err := captureFileFingerprint(filePath)
	if err != nil {
		return 0
	}
	return a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), fp)
}

// captureMCPReadBasisForBlocks is the blocks-fetch variant of
// captureMCPReadBasis: the fetch serves DB rows, so the basis must anchor on
// those rows exactly like the block editor's load capture
// (capturePageLoadFingerprintForBlocks, #1005). During the watcher's
// re-index debounce the rows can still be the pre-external state while disk
// holds the edit — a byte-anchored basis would pass a client update built
// from those rows and silently revert the external managed edit. On drift
// the basis is pinned to the rows so update_blocks refuses until a
// post-reindex re-fetch re-anchors on the settled bytes. Returns the fetch
// generation the client must echo in update_blocks (0 when no basis armed).
func (a *App) captureMCPReadBasisForBlocks(filePath, notebook, section, page string, rows []parser.ParsedBlock) int64 {
	if a == nil || filePath == "" {
		return 0
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			// Fail closed (#1005): rows were served for a file that is gone.
			// Arm an absent basis so a later update_blocks refuses until a
			// re-fetch re-anchors — without it the absent basis reads as the
			// legacy no-guard contract and an update built from the stale
			// rows would write over whatever recreates the file inside this
			// window. Mirrors captureFileFingerprint's absent semantics.
			return a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath),
				FileFingerprint{Size: absentFileSize})
		}
		// Keep the previous basis (if any): a stale basis fails closed on
		// drift, and an absent one is the legacy no-guard contract.
		return 0
	}
	diskBlocks, _, _, _, perr := parser.ParseFileContent(string(contentBytes),
		notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab)
	if perr != nil {
		return 0
	}
	if blocksContentMatchesRows(diskBlocks, rows) {
		// Anchor on the bytes those rows were served FROM: stat + hash of
		// the content already read (no second read; mirrors
		// capturePageLoadFingerprintFromBytes).
		info, serr := os.Stat(filePath)
		if serr != nil {
			return 0
		}
		fp := fingerprintFromStat(info)
		sum := sha256.Sum256(contentBytes)
		fp.SHA256 = hex.EncodeToString(sum[:])
		fp.stampCaptureTime(time.Now())
		return a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), fp)
	}
	pinned, ok := rowsPinnedFingerprint(rows)
	if !ok {
		return 0
	}
	return a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), pinned)
}

// captureMCPReadBasisFromBytes arms the markdown funnel's basis on bytes the
// caller already read and is about to serve — the only way to guarantee
// basis == observed without an unguardable second read (the match arm of
// captureMCPReadBasisForBlocks is the blocks-funnel equivalent).
func (a *App) captureMCPReadBasisFromBytes(filePath string, content []byte) int64 {
	if a == nil || filePath == "" {
		return 0
	}
	info, err := os.Stat(filePath)
	if err != nil {
		// The bytes were just read; a stat failure here is transient at
		// worst. Keep any prior basis (stale fails closed on drift, absent
		// is the legacy no-guard contract).
		return 0
	}
	fp := fingerprintFromStat(info)
	sum := sha256.Sum256(content)
	fp.SHA256 = hex.EncodeToString(sum[:])
	fp.stampCaptureTime(time.Now())
	return a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), fp)
}

// captureMCPReadBasisForBlocksByLocator resolves a page locator to its file
// path the same way the save funnels do and snapshots the rows-anchored
// read-basis (see captureMCPReadBasisForBlocks). Called by the MCP bridge
// after a blocks fetch, with the rows it just served.
func (a *App) captureMCPReadBasisForBlocksByLocator(notebook, section, page string, rows []parser.ParsedBlock) int64 {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if filePath, ok := a.resolvePageFilePath(a.resolveSourceByName(sanitizePathSegment(notebook)), notebook, section, page); ok {
		return a.captureMCPReadBasisForBlocks(filePath, notebook, section, page, rows)
	}
	return 0
}

// mcpBasisInvalidatedSize marks a read-basis invalidated by a committed
// Silt page write (strict read-modify-write, #1005). Any real stat has
// Size >= 0, so the marker can never stat-match; unlike an absent basis
// (never fetched — legacy no-guard contract) the marker REFUSES: a committed
// write invalidates every prior fetch, so each update_blocks must be built
// from a fresh fetch. Only a new fetch re-arms the basis.
const mcpBasisInvalidatedSize int64 = -3

// invalidateMCPReadBasis arms the strict-RMW marker for filePath. Called
// in-lock from trackedPageWrite on every committed Silt page write (and the
// two direct-write sites that honor the same contract inline): a basis
// refreshed from the written bytes would instead let a SECOND client's
// update — built from the pre-write fetch — gate basis==disk and silently
// revert this write (cross-client clobber amplifier); and a post-write disk
// recapture can adopt interleaved bytes the client never observed.
// Invalidation is the only state that fails closed for every stale fetcher
// at once. It also records the newest issued fetch generation: the marker
// alone dies the moment ANY later fetch (including the committing client's
// own re-read) re-arms the shared slot, so the echoed-generation check in
// checkMCPClientBasis keeps refusing pre-write generations after that.
func (a *App) invalidateMCPReadBasis(filePath string) {
	if a == nil || filePath == "" {
		return
	}
	key := core.NormalizeFileLockPath(filePath)
	a.mcpReadBaselinesMu.Lock()
	defer a.mcpReadBaselinesMu.Unlock()
	if a.mcpReadBaselines == nil {
		a.mcpReadBaselines = make(map[string]FileFingerprint)
	}
	a.mcpReadBaselines[key] = FileFingerprint{Size: mcpBasisInvalidatedSize}
	if a.mcpFetchGen == nil {
		a.mcpFetchGen = make(map[string]int64)
	}
	if a.mcpBasisInvalidatedGen == nil {
		a.mcpBasisInvalidatedGen = make(map[string]int64)
	}
	// The newest generation issued so far — every fetch at or below it
	// predates this commit.
	a.mcpBasisInvalidatedGen[key] = a.mcpFetchGen[key]
}

// checkMCPClientBasis is the MCP save funnel's client-basis gate: the bytes
// the client's fetch observed must still be the bytes on disk (the caller
// MUST hold LockFileWrite for filePath). clientGen is the read_generation
// the client echoed from its fetch (0 = legacy client that did not echo:
// only the marker + fingerprint gates apply). Refusals here are deliberately
// NOT stash-eligible (see saveFileBlocksMCP): the programmatic caller holds
// its own payload and its typed error already told it to re-read; a sticky
// stash would wedge the documented re-fetch+retry recovery until a desktop
// UI resolved it. The pending-stash + editor-baseline halves run separately
// via checkExternalEditConflict.
func (a *App) checkMCPClientBasis(filePath string, clientGen int64) error {
	key := core.NormalizeFileLockPath(filePath)
	a.mcpReadBaselinesMu.RLock()
	basis, ok := a.mcpReadBaselines[key]
	invalidated := a.mcpBasisInvalidatedGen[key]
	a.mcpReadBaselinesMu.RUnlock()
	if !ok {
		// No MCP read this session — nothing this funnel added to guard.
		return nil
	}
	if basis.Size == mcpBasisInvalidatedSize {
		// Strict RMW: a Silt write landed since this fetch. The client
		// holds its payload; recovery is re-read + retry, not resolution.
		return wrapSentinelAsIPCError(CodeExternalEditConflict,
			fmt.Sprintf("%s since it was last read — read it again before writing: %s",
				errExternalEditConflict.Error(), filepath.Base(filePath)),
			errExternalEditConflict)
	}
	if clientGen > 0 && clientGen <= invalidated {
		// A committed Silt write landed after the fetch this update was
		// built from. The shared slot has since been re-armed by some newer
		// fetch (basis==disk again), so only the echoed generation still
		// identifies this client's fetch as predating the commit — without
		// it the stale update would silently revert the committed write.
		return wrapSentinelAsIPCError(CodeExternalEditConflict,
			fmt.Sprintf("%s since it was last read — read it again before writing: %s",
				errExternalEditConflict.Error(), filepath.Base(filePath)),
			errExternalEditConflict)
	}
	return a.fingerprintGate(filePath, basis, func(fp FileFingerprint) {
		// Stat refresh of the SAME read (sync-client touch tolerance) —
		// must not mint a new generation, so bypass storeMCPReadBasis.
		a.mcpReadBaselinesMu.Lock()
		if a.mcpReadBaselines != nil {
			a.mcpReadBaselines[key] = fp
		}
		a.mcpReadBaselinesMu.Unlock()
	})
}

// storeMCPReadBasis stores a fetch-captured basis, stamping a fresh monotonic
// per-path generation onto it, and returns that generation. The stat-refresh
// store inside fingerprintGate must NOT mint a generation — that is the same
// read, re-anchored — so it assigns the map slot directly instead of calling
// here.
func (a *App) storeMCPReadBasis(key string, fp FileFingerprint) int64 {
	a.mcpReadBaselinesMu.Lock()
	defer a.mcpReadBaselinesMu.Unlock()
	if a.mcpReadBaselines == nil {
		a.mcpReadBaselines = make(map[string]FileFingerprint)
	}
	if a.mcpFetchGen == nil {
		a.mcpFetchGen = make(map[string]int64)
	}
	a.mcpFetchGen[key]++
	gen := a.mcpFetchGen[key]
	a.mcpReadBaselines[key] = fp
	return gen
}

// fingerprintGate is the stat fast-path + hash arbiter shared by the
// external-edit gates: a stat match outside the raciness window passes, drift
// (or a racy match) re-reads and hashes, a hash match passes AND refreshes
// the stat via store (sync-client touch tolerance), a mismatch is the
// external-edit refusal. Absent baselines and file states that cannot be
// verified refuse fail-closed exactly like checkExternalEditConflict.
func (a *App) fingerprintGate(filePath string, baseline FileFingerprint, store func(FileFingerprint)) error {
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			absent := FileFingerprint{Size: absentFileSize, SHA256: ""}
			if baseline.fingerprintStatEqual(absent) {
				return nil
			}
			return externalEditConflictError(filePath)
		}
		return errExternalEditConflictWrap(err)
	}
	cur := fingerprintFromStat(info)
	if baseline.fingerprintStatEqual(cur) && !baseline.statRacilyClean(cur, time.Now()) {
		return nil
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return errExternalEditConflictWrap(err)
	}
	sum := sha256.Sum256(data)
	cur.SHA256 = hex.EncodeToString(sum[:])
	if cur.SHA256 == baseline.SHA256 {
		cur.stampCaptureTime(time.Now())
		store(cur)
		return nil
	}
	return externalEditConflictError(filePath)
}
