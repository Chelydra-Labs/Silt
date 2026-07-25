// Package paths resolves Silt's per-user local storage locations.
//
// Silt splits its on-disk footprint into OS-conventional tiers:
//   - ConfigDir (os.UserConfigDir): settings.json, mcp-endpoint.json — small
//     files that follow the user across machines (roam on Windows).
//   - LocalDataDir (this package): the regeneratable SQLite index and other
//     reproducible app data — non-roaming, non-evicted, expensive to rebuild.
//   - CacheDir (os.UserCacheDir): downloadable/regenerable assets (dictionaries).
//
// The index was relocated out of the synced vault so a cloud-sync engine or
// antivirus cannot lock or corrupt it. LocalDataDir is chosen per-OS so a
// warm-start index survives: %LOCALAPPDATA% on Windows (non-roaming, via
// os.UserCacheDir), ~/Library/Application Support on macOS (avoids the
// OS-evicted ~/Library/Caches), and $XDG_DATA_HOME on Linux.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LocalDataDir returns the per-user local data directory for Silt's
// regeneratable app data. Override with SILT_DATA_DIR (absolute) for tests.
func LocalDataDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv("SILT_DATA_DIR")); override != "" {
		return override, nil
	}
	switch runtime.GOOS {
	case "windows":
		// os.UserCacheDir() returns %LOCALAPPDATA% on Windows — non-roaming
		// and not OS-evicted, the right home for a regeneratable index.
		base, err := os.UserCacheDir()
		if err != nil {
			return "", fmt.Errorf("local data dir: %w", err)
		}
		return filepath.Join(base, "Silt"), nil
	case "darwin":
		// os.UserConfigDir() returns ~/Library/Application Support on macOS.
		// ~/Library/Caches (os.UserCacheDir) is evicted under storage pressure
		// — unsuitable for a warm-start-sensitive index.
		base, err := os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("local data dir: %w", err)
		}
		return filepath.Join(base, "Silt"), nil
	default:
		// Linux/BSD: XDG_DATA_HOME or ~/.local/share. Per the XDG spec a
		// relative XDG_DATA_HOME is invalid and is ignored (falling back to the
		// default) rather than resolving against the process cwd.
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" || !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("local data dir: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
		return filepath.Join(base, "Silt"), nil
	}
}

// VaultKey returns a stable, filesystem-safe key for a vault, derived from the
// canonical (symlink-resolved, cleaned) vault path. Distinct physical vaults
// produce distinct keys. Symlinks are resolved so two paths to the same vault
// (a real path and a symlinked alias) collapse to one key. Case is folded only
// on Windows, whose filesystems are case-insensitive by default; macOS is left
// case-sensitive because case-sensitive HFS/APFS is a supported config where
// folding would corrupt by colliding two distinct physical vaults (the safer
// failure mode — a duplicate index dir — wins on default case-insensitive macOS).
//
// This derivation is load-bearing: changing it orphans every existing per-user
// index. The full 256-bit digest is used because the cost is zero and a
// collision's failure mode is silent index corruption.
func VaultKey(vaultPath string) string {
	// Resolve symlinks first; fall back to Abs if the path doesn't exist yet.
	clean, err := filepath.EvalSymlinks(vaultPath)
	if err != nil {
		clean, err = filepath.Abs(vaultPath)
		if err != nil {
			clean = vaultPath
		}
	}
	clean = filepath.Clean(clean)
	if runtime.GOOS == "windows" {
		clean = strings.ToLower(clean)
	}
	sum := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(sum[:])
}

// LocalIndexPath returns the on-disk path for a vault's relocated index:
//
//	<LocalDataDir>/indexes/<VaultKey(vaultPath)>/index.sqlite
//
// The parent directory is created (0o700) if missing. This is the single
// source of truth for the production index path; all callers route through it.
func LocalIndexPath(vaultPath string) (string, error) {
	base, err := LocalDataDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "indexes", VaultKey(vaultPath))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("ensure index dir %s: %w", dir, err)
	}
	return filepath.Join(dir, "index.sqlite"), nil
}
