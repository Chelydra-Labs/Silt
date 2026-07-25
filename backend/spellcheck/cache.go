package spellcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// contentSHA256 returns a hex SHA-256 of the concatenated parts.
func contentSHA256(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		_, _ = h.Write(p)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// CacheRoot returns <UserCacheDir>/silt/dictionaries. Overridable in tests via
// SILT_DICTIONARY_CACHE (absolute path). On the first non-overridden call it
// relocates any legacy cache from <UserConfigDir>/silt/dictionaries once
// (dictionaries are downloadable/regenerable, so the OS cache dir is correct).
func CacheRoot() (string, error) {
	if override := strings.TrimSpace(os.Getenv("SILT_DICTIONARY_CACHE")); override != "" {
		return override, nil
	}
	migrateDictionaryCacheOnce()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("user cache dir: %w", err)
	}
	return filepath.Join(cacheDir, "silt", "dictionaries"), nil
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
// pinned catalog version matches. SHA256 is a hex digest of the pack payload
// (language: aff+dic; domain: words.txt) for cache integrity on later loads.
type Manifest struct {
	ID        string    `json:"id"`
	Package   string    `json:"package"`
	Version   string    `json:"version"`
	FetchedAt time.Time `json:"fetched_at"`
	Bytes     int64     `json:"bytes"`
	SHA256    string    `json:"sha256,omitempty"`
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
// version is present on disk and passes integrity when a SHA256 was stored.
func languageInstalled(root string, spec LanguageSpec) bool {
	if spec.Bundled {
		return true
	}
	dir := LanguageDir(root, spec.ID)
	m, err := readManifest(dir)
	if err != nil || m.Version != spec.Version {
		return false
	}
	aff, err1 := os.ReadFile(filepath.Join(dir, "index.aff"))
	dic, err2 := os.ReadFile(filepath.Join(dir, "index.dic"))
	if err1 != nil || err2 != nil {
		return false
	}
	if m.SHA256 != "" && contentSHA256(aff, dic) != m.SHA256 {
		return false
	}
	return true
}

// domainInstalled reports whether a complete domain pack for the catalog
// version is present (or bundled) and passes integrity when a SHA256 was stored.
func domainInstalled(root string, spec DomainSpec) bool {
	if spec.Bundled {
		return true
	}
	dir := DomainDir(root, spec.ID)
	m, err := readManifest(dir)
	if err != nil || m.Version != spec.Version {
		return false
	}
	words, err := os.ReadFile(filepath.Join(dir, "words.txt"))
	if err != nil {
		return false
	}
	if m.SHA256 != "" && contentSHA256(words) != m.SHA256 {
		return false
	}
	return true
}
