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
// MCP save funnel re-checks that basis inside LockFileWrite. A committed
// update_blocks INVALIDATES the basis (strict read-modify-write: every
// write consumes the fetch it was built from, so overlapping clients each
// re-read before writing again). A drift refuses WITHOUT minting a stash
// (checkMCPClientBasis): the programmatic caller still holds its own payload
// and its typed external_edit_conflict error already tells it to re-read and
// retry — a sticky desktop-only stash would wedge exactly that recovery. The
// pending-stash and editor-baseline halves of the gate run separately via
// checkExternalEditConflict and keep their stashing semantics.

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
// (stat + hash of the raw file). Missing-file errors keep the absent-baseline
// semantics of captureFileFingerprint; other errors skip the capture (the
// write gate simply has nothing to compare — legacy behavior).
func (a *App) captureMCPReadBasis(filePath string) {
	if a == nil || filePath == "" {
		return
	}
	fp, err := captureFileFingerprint(filePath)
	if err != nil {
		return
	}
	a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), fp)
}

// captureMCPReadBasisForBlocks is the blocks-fetch variant of
// captureMCPReadBasis: the fetch serves DB rows, so the basis must anchor on
// those rows exactly like the block editor's load capture
// (capturePageLoadFingerprintForBlocks, #1005). During the watcher's
// re-index debounce the rows can still be the pre-external state while disk
// holds the edit — a byte-anchored basis would pass a client update built
// from those rows and silently revert the external managed edit. On drift
// the basis is pinned to the rows so update_blocks refuses until a
// post-reindex re-fetch re-anchors on the settled bytes.
func (a *App) captureMCPReadBasisForBlocks(filePath, notebook, section, page string, rows []parser.ParsedBlock) {
	if a == nil || filePath == "" {
		return
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		// Keep the previous basis (if any): a stale basis fails closed on
		// drift, and an absent one is the legacy no-guard contract.
		return
	}
	diskBlocks, _, _, _, perr := parser.ParseFileContent(string(contentBytes),
		notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab)
	if perr != nil {
		return
	}
	if blocksContentMatchesRows(diskBlocks, rows) {
		// Anchor on the bytes those rows were served FROM: stat + hash of
		// the content already read (no second read; mirrors
		// capturePageLoadFingerprintFromBytes).
		info, serr := os.Stat(filePath)
		if serr != nil {
			return
		}
		fp := fingerprintFromStat(info)
		sum := sha256.Sum256(contentBytes)
		fp.SHA256 = hex.EncodeToString(sum[:])
		fp.stampCaptureTime(time.Now())
		a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), fp)
		return
	}
	pinned, ok := rowsPinnedFingerprint(rows)
	if !ok {
		return
	}
	a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), pinned)
}

// captureMCPReadBasisForBlocksByLocator resolves a page locator to its file
// path the same way the save funnels do and snapshots the rows-anchored
// read-basis (see captureMCPReadBasisForBlocks). Called by the MCP bridge
// after a blocks fetch, with the rows it just served.
func (a *App) captureMCPReadBasisForBlocksByLocator(notebook, section, page string, rows []parser.ParsedBlock) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if filePath, ok := a.resolvePageFilePath(a.resolveSourceByName(sanitizePathSegment(notebook)), notebook, section, page); ok {
		a.captureMCPReadBasisForBlocks(filePath, notebook, section, page, rows)
	}
}

// mcpBasisInvalidatedSize marks a read-basis invalidated by a committed
// update_blocks (strict read-modify-write, #1005). Any real stat has
// Size >= 0, so the marker can never stat-match; unlike an absent basis
// (never fetched — legacy no-guard contract) the marker REFUSES: a committed
// write invalidates every prior fetch, so each update_blocks must be built
// from a fresh fetch. Only a new fetch re-arms the basis.
const mcpBasisInvalidatedSize int64 = -3

// invalidateMCPReadBasis arms the strict-RMW marker for filePath. Called
// in-lock from the committed update_blocks write: a basis refreshed from the
// written bytes would instead let a SECOND client's update — built from the
// pre-write fetch — gate basis==disk and silently revert this write
// (cross-client clobber amplifier); and a post-write disk recapture can
// adopt interleaved bytes the client never observed. Invalidation is the
// only state that fails closed for every stale fetcher at once.
func (a *App) invalidateMCPReadBasis(filePath string) {
	if a == nil || filePath == "" {
		return
	}
	a.storeMCPReadBasis(core.NormalizeFileLockPath(filePath), FileFingerprint{
		Size: mcpBasisInvalidatedSize,
	})
}

// checkMCPClientBasis is the MCP save funnel's client-basis gate: the bytes
// the client's fetch observed must still be the bytes on disk (the caller
// MUST hold LockFileWrite for filePath). Refusals here are deliberately NOT
// stash-eligible (see saveFileBlocksWithGate): the programmatic caller holds
// its own payload and its typed error already told it to re-read; a sticky
// stash would wedge the documented re-fetch+retry recovery until a desktop
// UI resolved it. The pending-stash + editor-baseline halves run separately
// via checkExternalEditConflict.
func (a *App) checkMCPClientBasis(filePath string) error {
	key := core.NormalizeFileLockPath(filePath)
	a.mcpReadBaselinesMu.RLock()
	basis, ok := a.mcpReadBaselines[key]
	a.mcpReadBaselinesMu.RUnlock()
	if !ok {
		// No MCP read this session — nothing this funnel added to guard.
		return nil
	}
	if basis.Size == mcpBasisInvalidatedSize {
		// Strict RMW: an update_blocks landed since this fetch. The client
		// holds its payload; recovery is re-read + retry, not resolution.
		return wrapSentinelAsIPCError(CodeExternalEditConflict,
			fmt.Sprintf("%s since it was last read — read it again before writing: %s",
				errExternalEditConflict.Error(), filepath.Base(filePath)),
			errExternalEditConflict)
	}
	return a.fingerprintGate(filePath, basis, func(fp FileFingerprint) {
		a.storeMCPReadBasis(key, fp)
	})
}

func (a *App) storeMCPReadBasis(key string, fp FileFingerprint) {
	a.mcpReadBaselinesMu.Lock()
	defer a.mcpReadBaselinesMu.Unlock()
	if a.mcpReadBaselines == nil {
		a.mcpReadBaselines = make(map[string]FileFingerprint)
	}
	a.mcpReadBaselines[key] = fp
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
