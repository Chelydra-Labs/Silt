package main

// External-edit protection for the MCP read-modify-write funnel (#1005).
//
// MCP page fetches deliberately do NOT capture the editor load baseline
// (captureNone in app_mcp.go): re-baselining on a client read would disarm
// the editor's gate behind its back. But that left the MCP side's own write
// unprotected — an UpdateBlocks whose handler started minutes after the
// fetch sailed through both gates and silently clobbered any external edit
// landing in between. This file gives the MCP funnel its own scoped basis:
// every fetch snapshots the file bytes the client actually observed
// (mcpReadBaselines), and the MCP save funnel re-checks that basis inside
// LockFileWrite, refusing (and stashing under the programmatic funnel, never
// evicting an editor stash) on drift — the client gets the typed
// external_edit_conflict error and can re-read and retry.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"time"

	"silt/backend/core"
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
	a.mcpReadBaselinesMu.Lock()
	defer a.mcpReadBaselinesMu.Unlock()
	if a.mcpReadBaselines == nil {
		a.mcpReadBaselines = make(map[string]FileFingerprint)
	}
	a.mcpReadBaselines[core.NormalizeFileLockPath(filePath)] = fp
}

// checkMCPReadBasis is the MCP save funnel's pre-write gate: everything the
// editor funnels check (pending-stash refusal, editor load baseline when an
// editor also holds the page) PLUS the client's own read-basis. The caller
// MUST hold LockFileWrite for filePath.
func (a *App) checkMCPReadBasis(filePath string) error {
	if err := a.checkExternalEditConflict(filePath); err != nil {
		return err
	}
	key := core.NormalizeFileLockPath(filePath)
	a.mcpReadBaselinesMu.RLock()
	basis, ok := a.mcpReadBaselines[key]
	a.mcpReadBaselinesMu.RUnlock()
	if !ok {
		// No MCP read this session — nothing this funnel added to guard.
		return nil
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
