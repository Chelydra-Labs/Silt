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
		// Linux/BSD: XDG_DATA_HOME or ~/.local/share.
		base := os.Getenv("XDG_DATA_HOME")
		if base == "" {
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
// absolute vault path. Distinct vault paths produce distinct keys (SHA-256
// truncated to 64 bits). On case-insensitive filesystems (Windows, macOS) the
// path is case-folded first so equivalent casings share one index directory.
func VaultKey(vaultPath string) string {
	abs, err := filepath.Abs(vaultPath)
	if err != nil {
		abs = vaultPath
	}
	clean := filepath.Clean(abs)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		clean = strings.ToLower(clean)
	}
	sum := sha256.Sum256([]byte(clean))
	return hex.EncodeToString(sum[:8]) // 16 hex chars (64 bits)
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
