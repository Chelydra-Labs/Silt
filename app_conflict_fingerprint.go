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
	"errors"
	"os"
	"path/filepath"
	"time"

	"silt/backend/core"
	"silt/backend/parser"
)

// refusedPageWrite remembers one editor save the external-edit gate refused,
// so ResolveExternalConflict can replay it after preserving the external
// version. A later refusal through the SAME funnel replaces an earlier one —
// the newest buffer is the version worth keeping. A refusal through the
// OTHER funnel (Edit blocks vs Source markdown) never does: the two surfaces
// hold different buffers, and the first stashed payload is the one the
// conflict latch's owning surface can settle.
type refusedPageWrite struct {
	source     string
	notebook   string // safe (sanitized) locator, as the save funnels compute it
	section    string
	page       string
	blocks     []parser.ParsedBlock // SaveFileBlocks payload; nil for a markdown save
	markdown   string               // SavePageMarkdown payload
	isMarkdown bool
	reason     string
}

// FileFingerprint is the load-time snapshot of one page file. Mtime/Ctime are
// split into sec/nsec so the struct is comparable with == (time.Time is not).
// CapturedAt records when this baseline was taken (capture or refresh) so the
// check can distrust stat matches inside the raciness window below.
type FileFingerprint struct {
	Size           int64
	MtimeSec       int64
	MtimeNsec      int64
	CtimeSec       int64
	CtimeNsec      int64
	SHA256         string
	CapturedAtSec  int64
	CapturedAtNsec int64
}

// fingerprintRacyWindow is how long after a baseline capture a stat-equal file
// remains distrusted. FAT/exFAT and SMB mounts — first-class linked-root
// targets — have 1–2s mtime granularity, and ext3/tmpfs are jiffy-coarse, so
// a same-size external rewrite within this window is invisible to the stat
// fast path (git's "racily clean" problem). ~3s covers the coarsest targets.
// NFS is a different gap: attribute caching (default `actimeo` 3–60s) can
// serve a stale stat long past this window, letting the fast path pass with
// no hash arbiter — same family as the Windows in-place-writer gap (see
// app_conflict_fingerprint_windows.go). Vaults on NFS mounts should mount
// with `noac` or a low `actimeo`; making the window configurable has not
// been needed for the supported linked-root targets.
const fingerprintRacyWindow = 3 * time.Second

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

// stampCaptureTime records when this baseline snapshot was taken. Every site
// that stores a fingerprint (capture, post-write refresh, hash-match refresh)
// must call this so the raciness window has an anchor.
func (fp *FileFingerprint) stampCaptureTime(now time.Time) {
	fp.CapturedAtSec, fp.CapturedAtNsec = now.Unix(), int64(now.Nanosecond())
}

// captureFileFingerprint stats + hashes the raw bytes of path. A missing file
// snapshots as absent rather than an error: "did not exist at load" is a
// meaningful baseline. Any other stat/read failure returns the error — the
// callers skip capture rather than persist a wrong baseline.
func captureFileFingerprint(path string) (FileFingerprint, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			absent := FileFingerprint{Size: absentFileSize, SHA256: ""}
			absent.stampCaptureTime(time.Now())
			return absent, nil
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
	fp.stampCaptureTime(time.Now())
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

// capturePageLoadFingerprint records the load baseline for filePath. Called
// only from editor-facing loads (the webview's FetchPageBlocks /
// FetchPageMarkdown — see pageLoadCapture); every editor reload recaptures,
// so the baseline is always "since the editor last loaded this page".
// Programmatic readers (MCP read_page, plugin block ops) must NOT recapture:
// they do not adopt the bytes into any buffer, so re-baselining would disarm
// the gate behind the editor's back. Errors are swallowed: a failed capture
// must not break the load — it only means no conflict protection until the
// next successful capture.
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
	fp.stampCaptureTime(time.Now())
	a.storePageLoadFingerprint(key, fp)
}

// statRacilyClean reports whether a stat-equal current stat could be hiding a
// same-size external rewrite: the file's mtime is still within the raciness
// window of the baseline's capture, so a coarse-granularity filesystem
// (FAT/SMB 1–2s, ext3/tmpfs jiffy-coarse) may not have ticked its timestamps
// past the baseline. Mirrors git's "racily clean" rule: until the window
// elapses, only the hash arbiter can vouch for the bytes.
func (fp FileFingerprint) statRacilyClean(cur FileFingerprint, now time.Time) bool {
	captured := time.Unix(fp.CapturedAtSec, fp.CapturedAtNsec)
	mtime := time.Unix(cur.MtimeSec, cur.MtimeNsec)
	diff := mtime.Sub(captured)
	if diff < 0 {
		diff = -diff
	}
	return diff < fingerprintRacyWindow
}

// checkExternalEditConflict is the pre-write gate for editor-derived
// whole-page saves. The caller MUST hold LockFileWrite for filePath — the
// check must observe the same file state the write is about to act on, so an
// edit landing between load and lock acquisition is still caught.
//
//   - A refused-write stash is pending for the path → refuse. The stash IS
//     the conflict state: nothing backend-side should be able to disarm it
//     by re-baselining (any load can recapture the fingerprint, so the
//     fingerprint alone cannot be authoritative once a save has been
//     refused). The funnels keep refusing — re-stashing same-funnel
//     newest-wins, keeping the first stash cross-funnel — until
//     ResolveExternalConflict settles it.
//   - No baseline for the path (never loaded this session) → pass: legacy
//     behavior, nothing to compare against.
//   - Stat match outside the raciness window → pass (happy path: one os.Stat,
//     no read, no hash).
//   - Stat drift, or a stat match still inside the raciness window → re-read
//   - hash. Hash match → pass and refresh the stat (sync-client touch
//     tolerance). Hash mismatch → refuse.
//
// A stat/read error refuses the save too (fail closed): writing over a file
// whose state cannot be established is exactly the clobber risk this gate
// exists to prevent.
func (a *App) checkExternalEditConflict(filePath string) error {
	if a == nil || filePath == "" {
		return nil
	}
	key := core.NormalizeFileLockPath(filePath)
	if err := a.externalEditStashRefusalKey(key, filePath); err != nil {
		return err
	}
	baseline, ok := a.loadPageFingerprint(key)
	if !ok {
		return nil
	}
	// Fast path: stat only. The hash is read + computed solely on drift so
	// the happy path costs one os.Stat (see BenchmarkExternalEditCheck).
	info, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			absent := FileFingerprint{Size: absentFileSize, SHA256: ""}
			if baseline.fingerprintStatEqual(absent) {
				return nil
			}
			return externalEditConflictError(filePath)
		}
		return errExternalEditConflictWrap(err)
	}
	cur := fingerprintFromStat(info)
	if baseline.fingerprintStatEqual(cur) && !baseline.statRacilyClean(cur, time.Now()) {
		return nil
	}
	// Drift — or a stat match still inside the raciness window, where a
	// coarse-granularity filesystem cannot distinguish a same-size external
	// rewrite from the baseline: the hash decides between a touch (sync
	// client re-statting identical bytes) and a real external rewrite.
	data, err := os.ReadFile(filePath)
	if err != nil {
		return errExternalEditConflictWrap(err)
	}
	sum := sha256.Sum256(data)
	cur.SHA256 = hex.EncodeToString(sum[:])
	if cur.SHA256 == baseline.SHA256 {
		// Same bytes, new stat — adopt the new stat so the next save's
		// fast path matches again without re-reading the file.
		cur.stampCaptureTime(time.Now())
		a.storePageLoadFingerprint(key, cur)
		return nil
	}
	return externalEditConflictError(filePath)
}

// isExternalEditConflict reports whether err is the external-edit refusal.
func isExternalEditConflict(err error) bool {
	return errors.Is(err, errExternalEditConflict)
}

func (a *App) storeRefusedWrite(key string, rw refusedPageWrite) {
	a.refusedWritesMu.Lock()
	defer a.refusedWritesMu.Unlock()
	if a.refusedWrites == nil {
		a.refusedWrites = make(map[string]refusedPageWrite)
	}
	if prev, exists := a.refusedWrites[key]; exists && prev.isMarkdown != rw.isMarkdown {
		// Cross-funnel refusal (#1005): the stashed payload belongs to the
		// latch's owning surface. Letting this one replace it would silently
		// destroy the other surface's buffer — its only copy outside the
		// backend. Keep the first stash; the refused save still errors
		// identically for its own surface.
		return
	}
	a.refusedWrites[key] = rw
}

func (a *App) popRefusedWrite(key string) (refusedPageWrite, bool) {
	a.refusedWritesMu.Lock()
	defer a.refusedWritesMu.Unlock()
	rw, ok := a.refusedWrites[key]
	if ok {
		delete(a.refusedWrites, key)
	}
	return rw, ok
}

// refusedWritePending reports whether an unresolved refused save is stashed
// for the path — the backend-authoritative conflict state the save funnels
// refuse against (see checkExternalEditConflict).
func (a *App) refusedWritePending(key string) bool {
	a.refusedWritesMu.Lock()
	defer a.refusedWritesMu.Unlock()
	_, ok := a.refusedWrites[key]
	return ok
}

// externalEditStashRefusalKey returns the typed external-edit refusal when a
// pending refused-write stash makes the path's conflict live, or nil. Every
// gated funnel consults it — including read-modify-write funnels that skip
// the load-baseline half (see externalEditGateMode): a live stash is the
// conflict state regardless of what the fingerprint says.
func (a *App) externalEditStashRefusalKey(key, filePath string) error {
	if a.refusedWritePending(key) {
		return externalEditConflictError(filePath)
	}
	return nil
}

// restoreRefusedWriteIfAbsent puts a popped stash back only when no newer
// refusal has replaced it meanwhile — a failed resolution must never clobber
// a newer stashed buffer with an older one.
func (a *App) restoreRefusedWriteIfAbsent(key string, rw refusedPageWrite) {
	a.refusedWritesMu.Lock()
	defer a.refusedWritesMu.Unlock()
	if _, exists := a.refusedWrites[key]; exists {
		return
	}
	if a.refusedWrites == nil {
		a.refusedWrites = make(map[string]refusedPageWrite)
	}
	a.refusedWrites[key] = rw
}
