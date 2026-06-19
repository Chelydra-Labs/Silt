// Package vault: archive.go implements the .silt-vault portable archive
// format (#143). A .silt-vault archive is a ZIP (custom extension) carrying a
// manifest.json + the vault contents at the archive root, EXCLUDING the
// reproducible SQLite index (rebuilt from markdown on import, identical
// contract to CopyVaultTree/MoveVault in mover.go — ARCHITECTURE.md §0 rule 4).
//
// The format is the local-first contract made portable: markdown is the
// product, YAML travels in .system/, the SQLite index is disposable working
// memory, and the whole archive is checksummed (per-entry + whole-archive
// SHA-256) so tampering/corruption is detectable before a single file is
// extracted on import.
package vault

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"time"
)

// ArchiveExtension is the custom extension for a portable Silt vault archive.
// The underlying container is a ZIP.
const ArchiveExtension = ".silt-vault"

// ArchiveManifestPath is the path (at the archive root) of the manifest entry.
// It is written LAST by ExportVaultTree so the whole-archive SHA-256 it
// carries covers every content entry.
const ArchiveManifestPath = "manifest.json"

// SupportedArchiveVersion is the archive-format version this build produces
// and accepts on import. An archive whose version differs is rejected on
// import (forward-compat: a higher version from a newer Silt is refused with a
// clear message rather than half-extracted).
const SupportedArchiveVersion = "1.0.0"

// maxArchiveUncompressedSize bounds the total extracted size of a .silt-vault
// archive so a hostile or accidental huge file can't exhaust the user's disk.
// Vault scale is larger than a plugin (SPECS §8.4 caps plugins at 100 MB), so
// the ceiling is raised accordingly; a typical vault of thousands of small
// markdown pages is well under this.
const maxArchiveUncompressedSize = 2 * 1024 * 1024 * 1024 // 2 GB

// maxArchiveEntrySize bounds a single extracted file. Per-file defense-in-depth
// alongside the total-archive cap; it also bounds the io.LimitReader so a
// forged-header zip-bomb cannot expand past the declared size during extraction
// (mirrors plugins.copyZipEntry).
const maxArchiveEntrySize = 256 * 1024 * 1024 // 256 MB

// ErrArchiveRejected is returned by the import validator when an archive
// cannot be safely imported. The wrapped message is user-actionable.
var ErrArchiveRejected = errors.New("vault archive rejected")

// ArchiveManifest is the manifest.json schema carried at the root of a
// .silt-vault archive. It is the archive's self-description: provenance
// (Silt version, optional vault name, creation time), scale (file/page counts,
// total bytes), and integrity (whole-archive SHA-256 + per-entry digests).
type ArchiveManifest struct {
	// ArchiveVersion is the format version (see SupportedArchiveVersion).
	// Informational on export (always the current version); enforced on import.
	ArchiveVersion string `json:"archive_version"`
	// SiltVersion is the version of Silt that produced the archive, read from
	// the embedded VERSION (App.GetAppVersion). Forward/compat diagnostic.
	SiltVersion string `json:"silt_version"`
	// VaultName is the optional display name of the vault; derived from the
	// source folder name when empty on export. Carried so an imported vault
	// can present a friendly label even after extraction into an arbitrary
	// empty folder.
	VaultName string `json:"vault_name,omitempty"`
	// CreatedAt is the archive creation time, RFC3339 (UTC).
	CreatedAt string `json:"created_at"`
	// PageFileCount is the count of .md page files archived. The issue asks
	// for a "block count", but counting blocks requires a full parse of every
	// file; the honest, cheap proxy is the page-file count (each page is one
	// .md, the streaming unit — SPECS §3.1). The field name reflects what is
	// actually counted.
	PageFileCount int `json:"page_file_count"`
	// FileCount is the total count of all regular (non-index, non-symlink)
	// files archived, including .system/ config/themes/templates/plugins.
	FileCount int `json:"file_count"`
	// TotalBytes is the sum of the uncompressed sizes of every archived file.
	TotalBytes int64 `json:"total_bytes"`
	// ArchiveSHA256 is the lowercase-hex SHA-256 of the whole archive file
	// EXCLUDING the manifest entry itself (the manifest cannot hash itself).
	// Computed over the exact byte range of all content entries + the ZIP
	// central-directory structure that precedes the manifest write. Import
	// recomputes this over the same range and asserts equality before any
	// extraction.
	ArchiveSHA256 string `json:"archive_sha256"`
	// Entries carries the per-entry integrity records (slash-form relpath,
	// uncompressed size, lowercase-hex SHA-256). Import verifies each entry
	// against its record as it streams out of the archive.
	Entries []ArchiveEntry `json:"entries"`
}

// ArchiveEntry is the per-file integrity record carried in the manifest.
type ArchiveEntry struct {
	// Path is the archive-root-relative, slash-separated path of the entry
	// (e.g. "Work/Inbox.md" or ".system/config.yaml"). Always forward-slash
	// for cross-platform portability.
	Path string `json:"path"`
	// Size is the uncompressed byte length of the entry.
	Size int64 `json:"size"`
	// SHA256 is the lowercase-hex SHA-256 of the entry's uncompressed bytes.
	SHA256 string `json:"sha256"`
}

// ExportResult describes a completed archive write.
type ExportResult struct {
	FilesArchived  int   `json:"files_archived"`
	BytesArchived  int64 `json:"bytes_archived"`
	PageFileCount  int   `json:"page_file_count"`
	SkippedIndex   bool  `json:"skipped_index"`
	SkippedSymlinks int  `json:"skipped_symlinks"`
}

// ImportResult describes a completed archive extraction.
type ImportResult struct {
	FilesExtracted int      `json:"files_extracted"`
	BytesExtracted int64    `json:"bytes_extracted"`
	PageFileCount  int      `json:"page_file_count"`
	Manifest       ArchiveManifest `json:"manifest"`
}

// walkedFile is a regular file discovered during the export tree walk: its
// absolute source path plus its archive-root-relative, slash-form path. Index
// artifacts (.system/index.sqlite*) and symlinks are excluded by
// computeFileTree (same exclusion rule as CopyVaultTree).
type walkedFile struct {
	srcPath string // absolute path on disk
	relPath string // slash-form, archive-root-relative
}

// computeFileTree walks root and returns the ordered list of regular files to
// archive, EXCLUDING the reproducible SQLite index artifacts and symlinks
// (which are counted + logged, not followed — mirrors CopyVaultTree). The
// order is deterministic (filepath.WalkDir lexical order) so two exports of
// the same vault produce byte-identical entry lists.
func computeFileTree(root string) (files []walkedFile, skippedIndex bool, skippedSymlinks int, err error) {
	walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if isIndexArtifact(relSlash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			skippedIndex = true
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		switch {
		case info.IsDir():
			return nil
		case info.Mode().IsRegular():
			files = append(files, walkedFile{srcPath: path, relPath: relSlash})
			return nil
		default:
			// Skip symlinks and special files (same posture as CopyVaultTree:
			// a symlinked notebook is absent from the archive, so count + log
			// it so the user learns the archive is incomplete).
			if info.Mode()&os.ModeSymlink != 0 {
				skippedSymlinks++
				log.Printf("vault.ExportVaultTree: skipping symlink (not followed): %s", relSlash)
			}
			return nil
		}
	})
	if walkErr != nil {
		return nil, false, 0, walkErr
	}
	return files, skippedIndex, skippedSymlinks, nil
}

// hashStream copies r into w through a sha256 hasher and returns the bytes
// written and the lowercase-hex digest. Used on both export (hash while
// streaming into the ZIP entry) and import (verify while extracting), so the
// checksum is computed during the single copy pass — no second read.
func hashStream(w io.Writer, r io.Reader) (int64, string, error) {
	h := sha256.New()
	n, err := io.Copy(w, io.TeeReader(r, h))
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

// hashFile is an alias for sha256OfFile (in mover.go) kept under a
// manifest-local name for readability at call sites. Returns lowercase-hex
// SHA-256.
func hashFile(path string) (string, error) {
	return sha256OfFile(path)
}

// manifestBytes marshals m to canonical JSON (sorted keys via json.Marshal,
// 2-space indent for human-readability — the archive is user-inspectable).
func manifestBytes(m ArchiveManifest) ([]byte, error) {
	return json.MarshalIndent(m, "", "  ")
}

// deriveVaultName returns the base name of the vault folder, used as the
// manifest VaultName when the caller does not supply one.
func deriveVaultName(vaultPath string) string {
	if vaultPath == "" {
		return ""
	}
	return filepath.Base(filepath.Clean(vaultPath))
}

// pageFileCount returns the count of entries whose path ends in .md. Used as
// the honest proxy for the issue's "block count" (see ArchiveManifest docs).
func pageFileCount(entries []ArchiveEntry) int {
	n := 0
	for _, e := range entries {
		if filepath.Ext(e.Path) == ".md" {
			n++
		}
	}
	return n
}

// nowRFC3339UTC returns the current time as an RFC3339 UTC string, for the
// manifest CreatedAt. UTC so the timestamp is stable regardless of the host
// timezone.
func nowRFC3339UTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}
