package db

import (
	"path/filepath"
	"strings"
)

// DetectCloudSyncedFolder reports whether path is under a known cloud-sync
// root (Google Drive / Microsoft OneDrive / Dropbox / Apple iCloud). It is
// best-effort: it never returns an error and a non-match never blocks opening
// the vault. When matched, the provider name feeds the vault:init-warnings
// channel so the user is nudged toward moving the vault to a purely local
// folder. A cloud-synced local folder (NTFS under a sync engine) looks
// like ordinary local storage to detectNetworkFilesystem, so this detector is
// the only signal for that case.
func DetectCloudSyncedFolder(path string) (provider string, ok bool) {
	return detectCloudSyncedFolder(path)
}

// cloudRoot is a candidate cloud-sync directory and its human-readable
// provider name.
type cloudRoot struct {
	dir      string
	provider string
}

// addRoot absolutizes dir and appends it as a cloudRoot candidate. Empty or
// unresolvable dirs are skipped.
func addRoot(roots *[]cloudRoot, dir, provider string) {
	if dir == "" {
		return
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return
	}
	*roots = append(*roots, cloudRoot{dir: abs, provider: provider})
}

// pathWithin reports whether path is inside dir (path == dir counts as within).
// Comparison is case-insensitive — correct on Windows/macOS; on Linux the only
// consequence is a possible over-match against a same-named non-cloud folder,
// which is harmless for a best-effort warning.
func pathWithin(p, dir string) bool {
	if p == "" || dir == "" {
		return false
	}
	pa, err := filepath.Abs(p)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(dir)), strings.ToLower(filepath.Clean(pa)))
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel == "." || (!strings.HasPrefix(rel, "../") && rel != "..")
}
