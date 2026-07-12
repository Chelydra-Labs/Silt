package spellcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheRoot returns <UserConfigDir>/silt/dictionaries. Overridable in tests via
// SILT_DICTIONARY_CACHE (absolute path).
func CacheRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv("SILT_DICTIONARY_CACHE")); override != "" {
		return override, nil
	}
	cfg, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("user config dir: %w", err)
	}
	return filepath.Join(cfg, "silt", "dictionaries"), nil
}

// LanguageDir is the cache directory for one language pack.
func LanguageDir(root, langID string) string {
	return filepath.Join(root, "languages", sanitizeID(langID))
}

// DomainDir is the cache directory for one domain pack.
func DomainDir(root, domainID string) string {
	return filepath.Join(root, "domains", sanitizeID(domainID))
}

// sanitizeID rejects path traversal and empty IDs. Catalog IDs are simple
// BCP-47-ish tags or kebab-case domain names.
func sanitizeID(id string) string {
	id = strings.TrimSpace(id)
	if id == "" || strings.Contains(id, "..") || strings.ContainsAny(id, `/\`) {
		return ""
	}
	return id
}

// Manifest records what was cached so Ensure can skip re-download when the
// pinned catalog version matches.
type Manifest struct {
	ID        string    `json:"id"`
	Package   string    `json:"package"`
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetched_at"`
	Bytes     int64     `json:"bytes"`
}

func readManifest(dir string) (Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, err
	}
	return m, nil
}

func writeManifest(dir string, m Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(dir, "manifest.json"), data)
}

// atomicWrite writes data to path via a sibling temp file + rename.
func atomicWrite(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// languageInstalled reports whether a complete language pack for the catalog
// version is present on disk.
func languageInstalled(root string, spec LanguageSpec) bool {
	if spec.Bundled {
		return true
	}
	dir := LanguageDir(root, spec.ID)
	m, err := readManifest(dir)
	if err != nil || m.Version != spec.Version {
		return false
	}
	for _, name := range []string{"index.aff", "index.dic"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}

// domainInstalled reports whether a complete domain pack for the catalog
// version is present (or bundled).
func domainInstalled(root string, spec DomainSpec) bool {
	if spec.Bundled {
		return true
	}
	dir := DomainDir(root, spec.ID)
	m, err := readManifest(dir)
	if err != nil || m.Version != spec.Version {
		return false
	}
	if _, err := os.Stat(filepath.Join(dir, "words.txt")); err != nil {
		return false
	}
	return true
}
