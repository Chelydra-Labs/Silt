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
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
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
// ArchiveSHA256 is the whole-archive integrity root: the lowercase-hex SHA-256
// over the canonical serialization of every entry record (path + size + per-
// entry digest), computed AFTER all entries are collected and carried in the
// manifest (written last). It binds the entire archive's identity + content in
// a single self-contained digest the manifest cannot hold over its own raw
// bytes (a manifest cannot hash itself).
//
// Go's archive/zip.Writer buffers all output and only writes to the underlying
// file on Close(), so a raw-byte-region hash is not computable live; the root
// digest is the standard self-contained alternative (Merkle-root style) and is
// compression-independent. Import validates in two layers: (1) recompute the
// root over the manifest's declared entries and assert equality (detects
// manifest tampering) BEFORE extracting, then (2) verify each entry's actual
// content hash during extraction (detects content corruption). Any changed
// path/size/content changes the root or the per-entry hash.
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

// pageFileCount returns the count of entries that are user page files: a
// `.md` file whose first path component is NOT `.system` (so .system/plugins/
// README.md and .system/templates/*.md — which are system files, not pages —
// are excluded). Used as the honest proxy for the issue's "block count" (see
// ArchiveManifest.PageFileCount docs).
func pageFileCount(entries []ArchiveEntry) int {
	n := 0
	for _, e := range entries {
		if isPageFile(e.Path) {
			n++
		}
	}
	return n
}

// isPageFile reports whether relSlash is a user page file: a .md under a
// notebook (first path component is not ".system"). System markdown (the
// plugins README, user templates) is intentionally excluded from the page
// count.
func isPageFile(relSlash string) bool {
	if filepath.Ext(relSlash) != ".md" {
		return false
	}
	first := relSlash
	if i := strings.IndexByte(relSlash, '/'); i >= 0 {
		first = relSlash[:i]
	}
	return first != ".system"
}

// nowRFC3339UTC returns the current time as an RFC3339 UTC string, for the
// manifest CreatedAt. UTC so the timestamp is stable regardless of the host
// timezone.
func nowRFC3339UTC() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// IsIndexArtifactName reports whether relSlash (a slash-separated path
// relative to the vault root) is one of the reproducible SQLite index files
// excluded from a copy/move/archive (ARCHITECTURE.md §0 rule 4). Exported so
// cross-package callers (tests, future import-side guards) share the single
// canonical exclusion predicate with mover.go::isIndexArtifact.
func IsIndexArtifactName(relSlash string) bool {
	return isIndexArtifact(relSlash)
}

// ProgressFn is the streaming-progress callback signature shared by export and
// import. current is the number of files processed so far; total is the file
// count discovered in the up-front stat pass (so the bar is determinate).
// phase is "export" or "import" (and "extract" during the import extract pass).
type ProgressFn func(phase string, current, total int)

// rootDigest computes the whole-archive integrity root: SHA-256 over the
// canonical serialization of every entry record. The canonical form is, for
// each entry in order: uint32 BE length of path, path bytes, int64 BE size,
// 32 raw bytes of the entry's SHA-256. This binds each entry's identity +
// content length + content digest into a single self-contained digest.
//
// (binary.BigEndian is used so the encoding is platform-independent and
// stable across versions.)
func rootDigest(entries []ArchiveEntry) string {
	h := sha256.New()
	var buf [8]byte
	for _, e := range entries {
		binary.BigEndian.PutUint32(buf[:4], uint32(len(e.Path)))
		h.Write(buf[:4])
		h.Write([]byte(e.Path))
		binary.BigEndian.PutUint64(buf[:], uint64(e.Size))
		h.Write(buf[:])
		if sum, err := hex.DecodeString(e.SHA256); err == nil {
			h.Write(sum)
		}
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ExportVaultTree streams the vault at src into a .silt-vault archive at
// destPath. It writes content entries (markdown + the whole .system/ tree
// EXCEPT the reproducible index.sqlite*), computing each entry's SHA-256
// during the single copy pass, then writes manifest.json LAST carrying the
// per-entry digests + the whole-archive root digest (ArchiveSHA256).
//
// The active vault is never touched by this primitive. Streaming: each file is
// copied one at a time through a MultiWriter (no load-whole-file), and
// onProgress is called after each file so the UI renders a determinate bar. On
// any error the partial destination is removed (the caller should have chosen
// a fresh path via the save-file picker, so cleanup only removes what this
// call wrote).
func ExportVaultTree(src, destPath, vaultName, siltVersion string, onProgress ProgressFn) (ExportResult, error) {
	if src == "" || destPath == "" {
		return ExportResult{}, fmt.Errorf("source and destination paths must not be empty")
	}
	srcAbs, err := absClean(src)
	if err != nil {
		return ExportResult{}, fmt.Errorf("resolve source: %w", err)
	}
	if _, err := os.Stat(srcAbs); err != nil {
		return ExportResult{}, fmt.Errorf("source vault not found: %w", err)
	}

	// Up-front walk gives the ordered file list + a determinate total for the
	// progress bar. Cheap (stat-only); the hash is computed during the copy.
	files, skippedIndex, skippedSymlinks, err := computeFileTree(srcAbs)
	if err != nil {
		return ExportResult{}, fmt.Errorf("scan vault tree: %w", err)
	}
	total := len(files)

	// Create the destination file. The save-file picker is expected to supply
	// a non-existent path; truncate handles a same-name re-export.
	if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
		return ExportResult{}, fmt.Errorf("create destination dir: %w", err)
	}
	f, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return ExportResult{}, fmt.Errorf("create archive file: %w", err)
	}

	success := false
	defer func() {
		_ = f.Close()
		if !success {
			_ = os.Remove(destPath)
		}
	}()

	// archive/zip.Writer buffers all output and writes to f on Close, so there
	// is no live byte stream to hash; the root digest is computed from the
	// collected entry records after the loop (see rootDigest).
	zw := zip.NewWriter(f)

	result := ExportResult{SkippedIndex: skippedIndex, SkippedSymlinks: skippedSymlinks}
	entries := make([]ArchiveEntry, 0, total)
	pageCount := 0

	for i, wf := range files {
		entry, cerr := copyFileToZip(zw, wf)
		if cerr != nil {
			return ExportResult{}, fmt.Errorf("archive %s: %w", wf.relPath, cerr)
		}
		entries = append(entries, entry)
		result.FilesArchived++
		result.BytesArchived += entry.Size
		if isPageFile(entry.Path) {
			pageCount++
		}
		if onProgress != nil {
			onProgress("export", i+1, total)
		}
	}
	result.PageFileCount = pageCount

	// VaultName defaults to the source folder's base name when the caller did
	// not supply one, so an imported vault can present a friendly label even
	// after extraction into an arbitrary empty folder.
	if vaultName == "" {
		vaultName = deriveVaultName(srcAbs)
	}
	manifest := ArchiveManifest{
		ArchiveVersion: SupportedArchiveVersion,
		SiltVersion:    siltVersion,
		VaultName:      vaultName,
		CreatedAt:      nowRFC3339UTC(),
		PageFileCount:  pageCount,
		FileCount:      result.FilesArchived,
		TotalBytes:     result.BytesArchived,
		ArchiveSHA256:  rootDigest(entries),
		Entries:        entries,
	}
	mb, err := manifestBytes(manifest)
	if err != nil {
		return ExportResult{}, fmt.Errorf("marshal manifest: %w", err)
	}
	mw, err := zw.Create(ArchiveManifestPath)
	if err != nil {
		return ExportResult{}, fmt.Errorf("create manifest entry: %w", err)
	}
	if _, err := mw.Write(mb); err != nil {
		return ExportResult{}, fmt.Errorf("write manifest: %w", err)
	}

	if err := zw.Close(); err != nil {
		return ExportResult{}, fmt.Errorf("finalize archive: %w", err)
	}
	if err := f.Sync(); err != nil {
		return ExportResult{}, fmt.Errorf("sync archive: %w", err)
	}

	success = true
	return result, nil
}

// copyFileToZip creates a ZIP entry for wf at its slash-form relpath, streams
// the source file into it through a per-entry sha256 hasher (so the digest is
// computed during the single copy pass — no second read), and returns the
// per-entry integrity record.
//
// Entries are stored with Method=Store (no compression). Compression is a
// documented future enhancement; markdown vaults are modest and the issue
// prioritizes portability + integrity over size. Store also keeps the archive
// trivially inspectable with any unzip tool.
func copyFileToZip(zw *zip.Writer, wf walkedFile) (ArchiveEntry, error) {
	info, err := os.Stat(wf.srcPath)
	if err != nil {
		return ArchiveEntry{}, err
	}
	fh := &zip.FileHeader{Name: wf.relPath, Method: zip.Store}
	fh.SetMode(info.Mode())
	fw, err := zw.CreateHeader(fh)
	if err != nil {
		return ArchiveEntry{}, err
	}
	in, err := os.Open(wf.srcPath)
	if err != nil {
		return ArchiveEntry{}, err
	}
	defer in.Close()
	perEntry := sha256.New()
	out := io.MultiWriter(fw, perEntry)
	n, err := io.Copy(out, in)
	if err != nil {
		return ArchiveEntry{}, err
	}
	return ArchiveEntry{
		Path:   wf.relPath,
		Size:   n,
		SHA256: hex.EncodeToString(perEntry.Sum(nil)),
	}, nil
}
