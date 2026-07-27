package main

import (
	"fmt"
	"path/filepath"

	"silt/backend/vault"
)

// Portable .silt-vault archive export / import and the file pickers that feed
// them. The archive is the cross-host portability format; copy / move / switch
// (in-tree relocations) live in app_vault_lifecycle.go.

// PickVaultExportPath opens the native save-file dialog filtered to
// *.silt-vault and returns the chosen path ("" on cancel). The frontend feeds
// the returned path to ExportVault. defaultFilename is offered as the initial
// name (e.g. "<vault-name>.silt-vault"); pass "" to let the OS pick a default.
// Mirrors PickExportPath (theme export) — the same SaveFileDialog surface.
func (a *App) PickVaultExportPath(defaultFilename string) (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application context not ready")
	}
	return a.saveFileDialog("Export Silt vault", defaultFilename, []FileFilter{
		{DisplayName: "Silt Vault (*.silt-vault)", Pattern: "*.silt-vault"},
	})
}

// ExportVault streams the active vault into a portable .silt-vault archive at
// destPath. The archive carries every file under the vault root EXCEPT the
// reproducible SQLite index (now stored outside the vault in a per-user local
// DataDir; a legacy in-vault index.sqlite* is still excluded if present —
// identical to CopyVaultTree/MoveVault). Per-entry + whole-archive
// root SHA-256 digests are written into manifest.json (last) so import can
// detect corruption/tampering before extracting.
//
// The active vault is never touched (read-only): no settings change, no
// service teardown, no event other than vault:archive:progress. Streaming +
// determinate progress: the up-front stat pass gives a file count, and an
// event is emitted per file so the UI renders a progress bar for large vaults.
// Runs without holding vaultMu across the (long) write — the path is snapshotted
// under the read lock, exactly like CopyVault.
func (a *App) ExportVault(destPath string) (vault.ExportResult, error) {
	a.wg.Add(1)
	defer a.wg.Done()

	a.vaultMu.RLock()
	src := a.vaultPath
	a.vaultMu.RUnlock()
	if src == "" {
		return vault.ExportResult{}, fmt.Errorf("no vault is currently open")
	}
	vaultName := filepath.Base(filepath.Clean(src))
	return vault.ExportVaultTree(src, destPath, vaultName, appVersion, func(phase string, current, total int) {
		a.emit(EventVaultArchiveProgress, map[string]any{
			"phase":   phase,
			"current": current,
			"total":   total,
		})
	})
}

// PickVaultArchive opens the native open-file dialog filtered to *.silt-vault
// and returns the chosen path ("" on cancel). The frontend feeds the returned
// path to ImportVault. Mirrors PickPluginArchive (the .silt-plugin picker) —
// the same OpenFileDialog surface.
func (a *App) PickVaultArchive() (string, error) {
	if a.wailsApp == nil {
		return "", fmt.Errorf("application context not ready")
	}
	return a.openFileDialog("Import Silt vault", []FileFilter{
		{DisplayName: "Silt Vault (*.silt-vault)", Pattern: "*.silt-vault"},
	})
}

// ImportVault validates a .silt-vault archive and extracts it into destPath
// (an empty local folder the user chose via PickVaultDestination), then opens
// the extracted vault by calling SwitchVault. The validate-before-extract
// posture (manifest parsed, version accepted, every entry zip-slip/absolute/
// size-guarded, whole-archive root digest recomputed) runs BEFORE any file is
// written; extraction streams into a sibling temp dir, verifying each entry's
// SHA-256 during the copy, and the temp dir is atomically renamed into destPath
// only after every entry verifies. A corrupt or hostile archive leaves
// destPath untouched.
//
// On success the backend emits vault:moved (from SwitchVault) so the frontend
// resets navigation and reloads its stores; a fresh SQLite index is rebuilt
// from markdown at destPath (the index is never carried in the archive — §0
// rule 4). Streaming progress is emitted via vault:archive:progress.
func (a *App) ImportVault(archivePath, destPath string) (vault.ImportResult, error) {
	a.wg.Add(1)
	defer a.wg.Done()

	res, err := vault.ImportVaultTree(archivePath, destPath, func(phase string, current, total int) {
		a.emit(EventVaultArchiveProgress, map[string]any{
			"phase":   phase,
			"current": current,
			"total":   total,
		})
	})
	if err != nil {
		return vault.ImportResult{}, err
	}
	// Open the extracted vault. SwitchVault rebuilds the index from markdown
	// and emits vault:moved so the frontend resets navigation + stores. It
	// refuses a path without a .system folder, which a verified archive
	// always produced.
	if err := a.SwitchVault(destPath); err != nil {
		return res, fmt.Errorf("archive extracted to %s but could not be opened: %w", destPath, err)
	}
	return res, nil
}
