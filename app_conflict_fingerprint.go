package main

// External-edit conflict detection (#1005).
//
// A whole-page editor save (SaveFileBlocks / SavePageMarkdown) renders the
// editor's buffer over whatever base the page had when it was LOADED. If an
// external writer (Obsidian, a sync client, a second Silt window) rewrote the
// file in between, that save silently discards the external version. Tier 0
// says no unrecoverable loss, so the save funnels refuse instead of clobber.
//
// The detector is git's model: snapshot {size, mtime, ctime, sha256} at load;
// inside LockFileWrite re-stat — a match passes (zero extra IO on the happy
// path), drift re-reads + hashes, a hash match passes AND refreshes the stat
// (sync clients "touch" files without changing bytes), and a hash mismatch is
// an external edit.
//
// Milestone hazard 1: the fingerprint derives from the path + stat + hash of
// the RAW FILE BYTES only — never from parsed block IDs or any parsed
// structure. Sprint 48 will rewrite block-ID comments inside files; that must
// never invalidate a fingerprint and fire a false conflict on the next save.
// (The flip side is deliberate: an external edit that ONLY rewrites block-ID
// bytes changes the hash and IS detected as a conflict.)

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"

	"silt/backend/core"
)

// FileFingerprint is the load-time snapshot of one page file. Mtime/Ctime are
// split into sec/nsec so the struct is comparable with == (time.Time is not).
type FileFingerprint struct {
	Size      int64
	MtimeSec  int64
	MtimeNsec int64
	CtimeSec  int64
	CtimeNsec int64
	SHA256    string
}

// absentFileSize marks a fingerprint captured while the file did not exist.
// Any later stat (size >= 0) then differs, and the hash arbiter ("") never
// matches real bytes — so an externally CREATED page conflicts too.
const absentFileSize int64 = -1

// fingerprintFromStat builds the stat half of a fingerprint (no hash).
func fingerprintFromStat(info os.FileInfo) FileFingerprint {
	fp := FileFingerprint{Size: info.Size()}
	mtime := info.ModTime()
	fp.MtimeSec, fp.MtimeNsec = mtime.Unix(), int64(mtime.Nanosecond())
	fp.CtimeSec, fp.CtimeNsec = statCtime(info)
	return fp
}

// captureFileFingerprint stats + hashes the raw bytes of path. A missing file
// snapshots as absent rather than an error: "did not exist at load" is a
// meaningful baseline. Any other stat/read failure returns the error — the
// callers skip capture rather than persist a wrong baseline.
func captureFileFingerprint(path string) (FileFingerprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return FileFingerprint{Size: absentFileSize, SHA256: ""}, nil
		}
		return FileFingerprint{}, err
	}
	fp := fingerprintFromStat(info)
	data, err := os.ReadFile(path)
	if err != nil {
		return FileFingerprint{}, err
	}
	sum := sha256.Sum256(data)
	fp.SHA256 = hex.EncodeToString(sum[:])
	return fp, nil
}

// fingerprintStatEqual compares only the stat half (the fast path never
// hashes). An absent baseline only matches a still-absent file.
func (fp FileFingerprint) fingerprintStatEqual(cur FileFingerprint) bool {
	return fp.Size == cur.Size &&
		fp.MtimeSec == cur.MtimeSec && fp.MtimeNsec == cur.MtimeNsec &&
		fp.CtimeSec == cur.CtimeSec && fp.CtimeNsec == cur.CtimeNsec
}

// capturePageFingerprintByLocator resolves a page locator to its file path the
// same way the whole-page save funnels do, then snapshots the baseline. The
// sanitization MUST mirror saveFileBlocksWithSource or the baseline would be
// keyed against a different path than the save checks.
func (a *App) capturePageFingerprintByLocator(source, notebook, section, page string) {
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, secErr := historySection(section)
	if secErr != nil {
		return
	}
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return
	}
	a.capturePageLoadFingerprint(filePath)
}

// capturePageLoadFingerprint records the load baseline for filePath. Called at
// the two editor load points (FetchPageBlocks / FetchPageMarkdown); every
// reload recaptures, so the baseline is always "since the app last loaded this
// page". Errors are swallowed: a failed capture must not break the load — it
// only means no conflict protection until the next successful capture.
func (a *App) capturePageLoadFingerprint(filePath string) {
	if a == nil || filePath == "" {
		return
	}
	fp, err := captureFileFingerprint(filePath)
	if err != nil {
		return
	}
	a.storePageLoadFingerprint(core.NormalizeFileLockPath(filePath), fp)
}

func (a *App) storePageLoadFingerprint(key string, fp FileFingerprint) {
	a.pageLoadFingerprintsMu.Lock()
	defer a.pageLoadFingerprintsMu.Unlock()
	if a.pageLoadFingerprints == nil {
		a.pageLoadFingerprints = make(map[string]FileFingerprint)
	}
	a.pageLoadFingerprints[key] = fp
}

func (a *App) loadPageFingerprint(key string) (FileFingerprint, bool) {
	a.pageLoadFingerprintsMu.RLock()
	defer a.pageLoadFingerprintsMu.RUnlock()
	fp, ok := a.pageLoadFingerprints[key]
	return fp, ok
}

// refreshFingerprintAfterWrite re-baselines filePath to the bytes Silt just
// wrote (Silt's own write is never an external edit). Stat gives the post-write
// times; the hash comes from the in-memory bytes — no extra file read. If the
// stat fails (or a test stubbed the write), the stale entry is left in place:
// the next check then drifts to the hash arbiter, which self-heals.
func (a *App) refreshFingerprintAfterWrite(filePath string, written []byte) {
	if a == nil || filePath == "" {
		return
	}
	key := core.NormalizeFileLockPath(filePath)
	if _, ok := a.loadPageFingerprint(key); !ok {
		// Page never loaded this session — nothing to re-baseline.
		return
	}
	info, err := os.Stat(filePath)
	if err != nil {
		return
	}
	fp := fingerprintFromStat(info)
	sum := sha256.Sum256(written)
	fp.SHA256 = hex.EncodeToString(sum[:])
	a.storePageLoadFingerprint(key, fp)
}
