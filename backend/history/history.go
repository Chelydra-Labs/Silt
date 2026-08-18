// Package history stores opt-in previous-content snapshots of page markdown
// under <root>/.system/history/. It is a durable filesystem store, not a
// SQLite index: deleting index.sqlite must leave these files intact.
package history

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	// MaxUncompressedBytes is the capture skip threshold (1 MiB).
	MaxUncompressedBytes = 1 << 20
	maxManifestBytes     = 2 << 20
	historyDirName       = "history"
	systemDirName        = ".system"
	pagesDirName         = "pages"
	blobsDirName         = "blobs"
	// EmptySectionName is the on-disk stand-in for a notebook-root page
	// (section == ""). CreateSection / validateSectionPath reject this
	// segment so a real section cannot alias those snapshots.
	EmptySectionName = "__root__"
	emptySectionName = EmptySectionName
	manifestExt      = ".jsonl"
	blobExt          = ".md.gz"
)

// Skip reasons returned by Capture when no new version is stored.
const (
	SkipEmpty     = "empty"
	SkipDuplicate = "duplicate"
	SkipTooLarge  = "too_large"
)

// ErrNotFound is returned when a version id is missing from the page manifest.
var ErrNotFound = errors.New("page version not found")

// Locator is the path identity of a page. Source is "vault" or "linked:<id>".
type Locator struct {
	Source   string
	Notebook string
	Section  string
	Page     string
}

// Entry is one retained snapshot, newest-first from List.
type Entry struct {
	ID     string    `json:"id"`
	Time   time.Time `json:"ts"`
	Hash   string    `json:"hash"`
	Bytes  int       `json:"bytes"`
	Source string    `json:"source"`
}

// Options controls Capture-side prune. Interval is caller-enforced.
type Options struct {
	MaxVersions int
}

type manifestLine struct {
	ID     string `json:"id"`
	TS     string `json:"ts"`
	Hash   string `json:"hash"`
	Bytes  int    `json:"bytes"`
	Source string `json:"source"`
}

var mu sync.Mutex

// Capture stores prev as a new version unless it is empty, oversized, or a
// duplicate of the last stored hash. Interval gating belongs to the caller.
func Capture(root string, loc Locator, prev []byte, reason string, now time.Time, opts Options) (skip string, err error) {
	mu.Lock()
	defer mu.Unlock()
	return captureLocked(root, loc, prev, reason, now, opts)
}

// List returns versions newest-first. A missing manifest is an empty list.
func List(root string, loc Locator) ([]Entry, error) {
	mu.Lock()
	defer mu.Unlock()
	entries, err := readManifest(root, loc)
	if err != nil {
		if os.IsNotExist(err) {
			return []Entry{}, nil
		}
		return nil, err
	}
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}
	return entries, nil
}

// ListManifests walks .system/history/pages for every page that has a
// manifest. Unreadable files are skipped (fail open). A missing pages
// directory is an empty result.
func ListManifests(root string) ([]Locator, error) {
	pages := filepath.Join(root, systemDirName, historyDirName, pagesDirName)
	absPages, err := filepath.Abs(pages)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(absPages); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Locator
	walkErr := filepath.WalkDir(absPages, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(d.Name()), manifestExt) {
			return nil
		}
		rel, err := filepath.Rel(absPages, path)
		if err != nil {
			return nil
		}
		loc, err := locatorFromRel(rel)
		if err != nil {
			return nil
		}
		out = append(out, loc)
		return nil
	})
	if walkErr != nil {
		return out, walkErr
	}
	return out, nil
}

func locatorFromRel(rel string) (Locator, error) {
	rel = filepath.ToSlash(rel)
	rel = strings.TrimSuffix(rel, manifestExt)
	parts := strings.Split(rel, "/")
	if len(parts) < 4 {
		return Locator{}, fmt.Errorf("history: short manifest path %q", rel)
	}
	source := unescapeSegment(parts[0])
	notebook := unescapeSegment(parts[1])
	page := unescapeSegment(parts[len(parts)-1])
	secParts := parts[2 : len(parts)-1]
	section := ""
	if len(secParts) == 1 && secParts[0] == emptySectionName {
		section = ""
	} else {
		decoded := make([]string, 0, len(secParts))
		for _, p := range secParts {
			if p == "" || p == emptySectionName {
				continue
			}
			decoded = append(decoded, unescapeSegment(p))
		}
		section = strings.Join(decoded, "/")
	}
	if source == "" || notebook == "" || page == "" {
		return Locator{}, fmt.Errorf("history: invalid manifest path %q", rel)
	}
	return Locator{Source: source, Notebook: notebook, Section: section, Page: page}, nil
}

func unescapeSegment(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '%' && i+2 < len(s) {
			var v byte
			ok := true
			for _, c := range []byte{s[i+1], s[i+2]} {
				v <<= 4
				switch {
				case c >= '0' && c <= '9':
					v |= c - '0'
				case c >= 'a' && c <= 'f':
					v |= c - 'a' + 10
				case c >= 'A' && c <= 'F':
					v |= c - 'A' + 10
				default:
					ok = false
				}
			}
			if ok {
				b.WriteByte(v)
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Last returns the newest stored entry (manifest last line).
func Last(root string, loc Locator) (Entry, bool, error) {
	mu.Lock()
	defer mu.Unlock()
	entries, err := readManifest(root, loc)
	if err != nil {
		if os.IsNotExist(err) {
			return Entry{}, false, nil
		}
		return Entry{}, false, err
	}
	if len(entries) == 0 {
		return Entry{}, false, nil
	}
	return entries[len(entries)-1], true, nil
}

// Read returns the uncompressed snapshot for id.
func Read(root string, loc Locator, id string) ([]byte, error) {
	mu.Lock()
	defer mu.Unlock()
	entries, err := readManifest(root, loc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	var found *Entry
	for i := range entries {
		if entries[i].ID == id {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		return nil, ErrNotFound
	}
	blob, err := blobPath(root, loc, found.Hash)
	if err != nil {
		return nil, err
	}
	return readGzipLimited(blob, MaxUncompressedBytes)
}

// Prune drops oldest versions beyond max and deletes blobs this page no longer cites.
func Prune(root string, loc Locator, max int) error {
	mu.Lock()
	defer mu.Unlock()
	return pruneLocked(root, loc, max)
}

// Relocate moves a page's manifest and blob directory after a rename/move.
func Relocate(root string, oldLoc, newLoc Locator) error {
	mu.Lock()
	defer mu.Unlock()
	oldMan, err := manifestPath(root, oldLoc)
	if err != nil {
		return err
	}
	newMan, err := manifestPath(root, newLoc)
	if err != nil {
		return err
	}
	oldBlobs, err := blobDir(root, oldLoc)
	if err != nil {
		return err
	}
	newBlobs, err := blobDir(root, newLoc)
	if err != nil {
		return err
	}
	if sameExistingPath(oldMan, newMan) || sameExistingPath(oldBlobs, newBlobs) {
		return nil
	}
	if _, err := os.Stat(oldMan); os.IsNotExist(err) {
		// Nothing to move; still try blobs in case of a partial prior write.
		if _, berr := os.Stat(oldBlobs); os.IsNotExist(berr) {
			return nil
		}
	} else if err != nil {
		return err
	}
	// Always copy-then-commit: write the dest manifest only after blobs are
	// in place, then delete the source. Dest blob-dir-without-manifest is
	// treated as dest-exists (merge), same as a dest manifest.
	return mergeRelocateLocked(root, oldLoc, newLoc, oldMan, oldBlobs, newBlobs)
}

// SameStore reports whether two locators resolve to the same on-disk
// manifest (byte-identical path, or the same file on a case-insensitive volume).
func SameStore(root string, a, b Locator) bool {
	am, err := manifestPath(root, a)
	if err != nil {
		return false
	}
	bm, err := manifestPath(root, b)
	if err != nil {
		return false
	}
	if am == bm {
		return true
	}
	return sameExistingPath(am, bm)
}

func sameExistingPath(a, b string) bool {
	if a == b {
		return true
	}
	fa, errA := os.Stat(a)
	fb, errB := os.Stat(b)
	if errA != nil || errB != nil {
		return false
	}
	return os.SameFile(fa, fb)
}

func mergeRelocateLocked(root string, oldLoc, newLoc Locator, oldMan, oldBlobs, newBlobs string) error {
	newMan, err := manifestPath(root, newLoc)
	if err != nil {
		return err
	}
	if sameExistingPath(oldMan, newMan) || sameExistingPath(oldBlobs, newBlobs) {
		return nil
	}
	incoming, err := readManifest(root, oldLoc)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	dest, err := readManifest(root, newLoc)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if len(incoming) == 0 {
		if _, berr := os.Stat(oldBlobs); os.IsNotExist(berr) {
			return nil
		}
	}

	merged := mergeEntries(dest, incoming)
	if err := os.MkdirAll(newBlobs, 0o700); err != nil {
		return err
	}
	if ents, rerr := os.ReadDir(oldBlobs); rerr == nil {
		for _, ent := range ents {
			if ent.IsDir() {
				continue
			}
			src := filepath.Join(oldBlobs, ent.Name())
			dst := filepath.Join(newBlobs, ent.Name())
			if _, sterr := os.Stat(dst); sterr == nil {
				continue
			}
			if err := copyFile(src, dst); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(rerr) {
		return rerr
	}

	var buf bytes.Buffer
	for _, e := range merged {
		b, err := json.Marshal(toLine(e))
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := writeFileAtomic(newMan, buf.Bytes()); err != nil {
		return err
	}
	if err := os.Remove(oldMan); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.RemoveAll(oldBlobs); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func mergeEntries(dest, incoming []Entry) []Entry {
	seen := make(map[string]struct{}, len(dest)+len(incoming))
	out := make([]Entry, 0, len(dest)+len(incoming))
	for _, e := range dest {
		if e.ID == "" {
			continue
		}
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		out = append(out, e)
	}
	for _, e := range incoming {
		if e.ID == "" {
			continue
		}
		if _, ok := seen[e.ID]; ok {
			continue
		}
		seen[e.ID] = struct{}{}
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Time.Before(out[j].Time)
	})
	return out
}

func captureLocked(root string, loc Locator, prev []byte, reason string, now time.Time, opts Options) (string, error) {
	if len(prev) == 0 {
		return SkipEmpty, nil
	}
	if len(prev) > MaxUncompressedBytes {
		return SkipTooLarge, nil
	}
	sum := sha256.Sum256(prev)
	hash := hex.EncodeToString(sum[:])
	entries, err := readManifest(root, loc)
	if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	if len(entries) > 0 && entries[len(entries)-1].Hash == hash {
		return SkipDuplicate, nil
	}
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}
	id := versionID(now, hash)
	man, err := manifestPath(root, loc)
	if err != nil {
		return "", err
	}
	blob, err := blobPath(root, loc, hash)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(blob), 0o700); err != nil {
		return "", err
	}
	if _, err := os.Stat(blob); os.IsNotExist(err) {
		gz, err := gzipBytes(prev)
		if err != nil {
			return "", err
		}
		if err := writeFileAtomic(blob, gz); err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	line := manifestLine{
		ID:     id,
		TS:     now.Format(time.RFC3339Nano),
		Hash:   hash,
		Bytes:  len(prev),
		Source: reason,
	}
	if err := appendJSONL(man, line); err != nil {
		return "", err
	}
	if opts.MaxVersions > 0 {
		if err := pruneLocked(root, loc, opts.MaxVersions); err != nil {
			return "", err
		}
	}
	return "", nil
}

func pruneLocked(root string, loc Locator, max int) error {
	if max < 1 {
		return fmt.Errorf("history prune: max must be >= 1")
	}
	entries, err := readManifest(root, loc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(entries) <= max {
		return nil
	}
	keep := entries[len(entries)-max:]
	cited := make(map[string]struct{}, len(keep))
	for _, e := range keep {
		cited[e.Hash] = struct{}{}
	}
	man, err := manifestPath(root, loc)
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	for _, e := range keep {
		b, err := json.Marshal(toLine(e))
		if err != nil {
			return err
		}
		buf.Write(b)
		buf.WriteByte('\n')
	}
	if err := writeFileAtomic(man, buf.Bytes()); err != nil {
		return err
	}
	dir, err := blobDir(root, loc)
	if err != nil {
		return err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, ent := range ents {
		if ent.IsDir() {
			continue
		}
		name := ent.Name()
		hash := strings.TrimSuffix(name, blobExt)
		if _, ok := cited[hash]; ok {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
	return nil
}

func readManifest(root string, loc Locator) ([]Entry, error) {
	path, err := manifestPath(root, loc)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > maxManifestBytes {
		return nil, fmt.Errorf("history manifest exceeds %d bytes", maxManifestBytes)
	}
	var entries []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 256*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var raw manifestLine
		if err := json.Unmarshal(line, &raw); err != nil {
			// A torn append must not poison the rest of the page history.
			continue
		}
		if strings.TrimSpace(raw.ID) == "" || strings.TrimSpace(raw.Hash) == "" {
			continue
		}
		ts, err := time.Parse(time.RFC3339Nano, raw.TS)
		if err != nil {
			ts, err = time.Parse(time.RFC3339, raw.TS)
			if err != nil {
				continue
			}
		}
		entries = append(entries, Entry{
			ID:     raw.ID,
			Time:   ts,
			Hash:   raw.Hash,
			Bytes:  raw.Bytes,
			Source: raw.Source,
		})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return entries, nil
}

func toLine(e Entry) manifestLine {
	return manifestLine{
		ID:     e.ID,
		TS:     e.Time.UTC().Format(time.RFC3339Nano),
		Hash:   e.Hash,
		Bytes:  e.Bytes,
		Source: e.Source,
	}
}

func appendJSONL(path string, line manifestLine) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(line)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(b, '\n'))
	cerr := f.Close()
	if werr != nil {
		return werr
	}
	return cerr
}

func gzipBytes(plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		_ = zw.Close()
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func readGzipLimited(path string, max int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(out) > max {
		return nil, fmt.Errorf("history blob exceeds %d-byte uncompressed cap", max)
	}
	return out, nil
}

func versionID(now time.Time, hash string) string {
	short := hash
	if len(short) > 12 {
		short = short[:12]
	}
	return now.UTC().Format("20060102T150405.000Z07:00") + "-" + short
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		_ = out.Close()
		if !ok {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	if err := out.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}

func writeFileAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	for i := 0; ; i++ {
		err = os.Rename(tmpPath, path)
		if err == nil {
			break
		}
		if i >= 10 {
			return err
		}
		time.Sleep(10 * time.Millisecond)
	}
	if runtime.GOOS != "windows" {
		dirFile, derr := os.Open(dir)
		if derr != nil {
			return derr
		}
		syncErr := dirFile.Sync()
		closeErr := dirFile.Close()
		if syncErr != nil {
			return syncErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	success = true
	return nil
}

func manifestPath(root string, loc Locator) (string, error) {
	rel, err := pageRel(loc)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, systemDirName, historyDirName, pagesDirName, rel+manifestExt)
	return checkedPath(root, p)
}

func blobDir(root string, loc Locator) (string, error) {
	rel, err := pageRel(loc)
	if err != nil {
		return "", err
	}
	p := filepath.Join(root, systemDirName, historyDirName, blobsDirName, rel)
	return checkedPath(root, p)
}

func blobPath(root string, loc Locator, hash string) (string, error) {
	if !isHexSHA256(hash) {
		return "", fmt.Errorf("history: invalid blob hash")
	}
	dir, err := blobDir(root, loc)
	if err != nil {
		return "", err
	}
	return checkedPath(root, filepath.Join(dir, hash+blobExt))
}

func pageRel(loc Locator) (string, error) {
	source, err := sanitizeSource(loc.Source)
	if err != nil {
		return "", err
	}
	notebook, err := sanitizeSegment(loc.Notebook)
	if err != nil {
		return "", fmt.Errorf("history: notebook: %w", err)
	}
	page, err := sanitizeSegment(loc.Page)
	if err != nil {
		return "", fmt.Errorf("history: page: %w", err)
	}
	section := strings.TrimSpace(loc.Section)
	var parts []string
	parts = append(parts, source, notebook)
	if section == "" {
		parts = append(parts, emptySectionName)
	} else {
		for _, seg := range strings.Split(section, "/") {
			if strings.TrimSpace(seg) == "" {
				continue
			}
			clean, err := sanitizeSegment(seg)
			if err != nil {
				return "", fmt.Errorf("history: section: %w", err)
			}
			parts = append(parts, clean)
		}
		if len(parts) == 2 {
			parts = append(parts, emptySectionName)
		}
	}
	parts = append(parts, page)
	return filepath.Join(parts...), nil
}

func sanitizeSource(source string) (string, error) {
	source = strings.TrimSpace(source)
	if source == "" {
		source = "vault"
	}
	// Linked roots already isolate storage via historyRoot. A vault-local
	// linked:<id> would orphan snapshots on unlink/relink; keep one stable
	// prefix on that root.
	if strings.HasPrefix(source, "linked:") || strings.HasPrefix(source, "linked_") {
		source = "linked"
	}
	source = strings.ReplaceAll(source, ":", "_")
	return sanitizeSegment(source)
}

func sanitizeSegment(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		if r < 32 || r == '/' || r == '\\' || r == ':' || r == '*' || r == '?' || r == '"' || r == '<' || r == '>' || r == '|' || r == '%' {
			fmt.Fprintf(&b, "%%%02X", r)
			continue
		}
		b.WriteRune(r)
	}
	cleaned := strings.TrimSpace(b.String())
	for strings.HasPrefix(cleaned, "..") {
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, ".."))
	}
	if cleaned == "." || cleaned == "" {
		return "", errors.New("empty path segment")
	}
	upper := strings.ToUpper(cleaned)
	if reservedWindowsName(upper) {
		cleaned = "_" + cleaned
	}
	return cleaned, nil
}

func reservedWindowsName(upper string) bool {
	if upper == "CON" || upper == "PRN" || upper == "AUX" || upper == "NUL" {
		return true
	}
	if len(upper) == 4 && (strings.HasPrefix(upper, "COM") || strings.HasPrefix(upper, "LPT")) && unicode.IsDigit(rune(upper[3])) {
		return true
	}
	return false
}

func checkedPath(root, p string) (string, error) {
	hist := filepath.Join(root, systemDirName, historyDirName)
	absHist, err := filepath.Abs(hist)
	if err != nil {
		return "", err
	}
	absP, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(absHist, absP)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("history path escapes %s", hist)
	}
	return absP, nil
}

func isHexSHA256(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
