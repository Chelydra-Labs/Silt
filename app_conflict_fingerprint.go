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
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"time"

	"silt/backend/core"
	"silt/backend/parser"
)

// refusedWriteFunnel identifies which save funnel stashed a refused payload
// (#1005). The stash's replacement policy depends on it: same-funnel
// newest-wins, and an editor-owned stash is never evicted by a programmatic
// (MCP/plugin) refusal — see storeRefusedWrite.
type refusedWriteFunnel int

const (
	// funnelEditorBlocks: SaveFileBlocks — the webview's block editor.
	funnelEditorBlocks refusedWriteFunnel = iota
	// funnelEditorMarkdown: SavePageMarkdown — the webview's Source mode.
	funnelEditorMarkdown
	// funnelProgrammatic: whole-page writes from non-editor callers (MCP
	// update_blocks/set_blocks). Their payload already received its refusal
	// error and is re-derivable by the caller; it must never displace a
	// stashed editor buffer, which is user typing whose only backend-side
	// copy is the stash.
	funnelProgrammatic
)

func (f refusedWriteFunnel) isEditorFunnel() bool {
	return f != funnelProgrammatic
}

// refusedPageWrite remembers one editor save the external-edit gate refused,
// so ResolveExternalConflict can replay it after preserving the external
// version. Replacement policy is funnel-based (see refusedWriteFunnel):
// same-funnel newest-wins (the newest buffer is the version worth keeping),
// cross-funnel editor refusals keep the first stash (the two surfaces hold
// different buffers, and the first stashed payload is the one the conflict
// latch's owning surface can settle), and a programmatic refusal never
// evicts an editor-owned stash.
type refusedPageWrite struct {
	source   string
	notebook string // safe (sanitized) locator, as the save funnels compute it
	section  string
	page     string
	blocks   []parser.ParsedBlock // SaveFileBlocks payload; nil for a markdown save
	markdown string               // SavePageMarkdown payload
	funnel   refusedWriteFunnel
	reason   string
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
	filePath, ok := a.resolvePageFilePath(source, notebook, section, page)
	if !ok {
		return
	}
	a.capturePageLoadFingerprint(filePath)
}

// capturePageFingerprintByLocatorForBlocks is the block-editor variant of
// capturePageFingerprintByLocator (#1005): the block funnel hands the editor
// DB rows, so the baseline must be anchored on those rows (see
// capturePageLoadFingerprintForBlocks), not on the raw disk bytes.
func (a *App) capturePageFingerprintByLocatorForBlocks(source, notebook, section, page string, rows []parser.ParsedBlock) {
	filePath, ok := a.resolvePageFilePath(source, notebook, section, page)
	if !ok {
		return
	}
	a.capturePageLoadFingerprintForBlocks(filePath, notebook, section, page, rows)
}

// resolvePageFilePath resolves a page locator to its file path with the exact
// sanitization saveFileBlocksWithSource applies, so a baseline is always keyed
// against the path the save will check.
func (a *App) resolvePageFilePath(source, notebook, section, page string) (string, bool) {
	safeNotebook := sanitizePathSegment(notebook)
	safeSection, secErr := historySection(section)
	if secErr != nil {
		return "", false
	}
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return "", false
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return "", false
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return "", false
	}
	return filePath, true
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

// capturePageLoadFingerprintFromBytes is capturePageLoadFingerprint anchored
// on bytes the caller already holds (the buffer it hands to the editor): the
// baseline must describe exactly those bytes. Re-reading the file instead
// would baseline whatever landed AFTER the editor's read — disarming the
// gate for precisely that edit (fail-open). Hashing the held bytes fails
// safe: any later disk difference mismatches and refuses.
func (a *App) capturePageLoadFingerprintFromBytes(filePath string, content []byte) {
	if a == nil || filePath == "" {
		return
	}
	info, err := os.Stat(filePath)
	if err != nil {
		// Same swallow contract as capturePageLoadFingerprint: a failed
		// capture only means no protection until the next successful one.
		return
	}
	fp := fingerprintFromStat(info)
	sum := sha256.Sum256(content)
	fp.SHA256 = hex.EncodeToString(sum[:])
	fp.stampCaptureTime(time.Now())
	a.storePageLoadFingerprint(core.NormalizeFileLockPath(filePath), fp)
}

// rowBasisSentinelSize marks a baseline pinned to the DB rows rather than to
// any file bytes. Any real stat has Size >= 0, so the fast path can never
// match and every save falls to the hash arbiter (see
// capturePageLoadFingerprintForBlocks). Distinct from absentFileSize so a
// missing-file branch is never confused with a rows-pinned baseline.
const rowBasisSentinelSize int64 = -2

// rowsPinnedFingerprint builds the baseline that refuses every write until a
// re-fetch re-anchors on re-indexed rows: stat half poisoned to the sentinel
// (the fast path can never match), hash pinned to a canonical encoding of
// the rows. Shared by every rows-anchored capture (editor load baseline,
// MCP read basis) so the pin encoding stays single-sourced.
func rowsPinnedFingerprint(rows []parser.ParsedBlock) (FileFingerprint, bool) {
	canonical, err := json.Marshal(rows)
	if err != nil {
		// ParsedBlock marshals unconditionally; keep the guard honest.
		return FileFingerprint{}, false
	}
	sum := sha256.Sum256(canonical)
	fp := FileFingerprint{
		Size:     rowBasisSentinelSize,
		MtimeSec: 0, MtimeNsec: 0, CtimeSec: 0, CtimeNsec: 0,
		SHA256: hex.EncodeToString(sum[:]),
	}
	fp.stampCaptureTime(time.Now())
	return fp, true
}

// hydratableBlockFields returns b reduced to the fields the block funnel's DB
// projection (db.FetchPageBlocks) hydrates. The load-baseline comparison uses
// it on both sides so a projection that cannot carry a field never reads as
// drift — and, on the disk-parse side, so parse-derived context (ParentID,
// LineNumber, FileDate) is ignored for the same reason the cross-page drift
// gate ignores it. [pin::]/[blocked_by::]/unknown tokens hydrate from the raw
// line (queries_blocks.go), so they are carried like-with-like. Known blind
// spot: a region block's Language (code-fence info string) and FileDate have
// NO storage anywhere on the row and stay zeroed — a fence-info-only external
// edit inside the re-index debounce compares equal and can be reverted by the
// next save instead of surfacing as a conflict. Closing that needs a schema
// column for region language; do not widen this whitelist past what the
// projection actually hydrates.
func hydratableBlockFields(b parser.ParsedBlock) parser.ParsedBlock {
	b.ParentID = ""
	b.LineNumber = 0
	b.FileDate = ""
	b.Author = ""
	b.Timestamp = ""
	b.Language = ""
	return b
}

// blocksContentMatchesRows reports whether a fresh parse of the disk bytes
// carries the same managed content, in order, as the rows the block funnel is
// about to hand the editor. (DeepEqual because ParsedBlock carries slice
// fields; the reduction in hydratableBlockFields nils them all out.)
func blocksContentMatchesRows(disk, rows []parser.ParsedBlock) bool {
	if len(disk) != len(rows) {
		return false
	}
	for i := range disk {
		if !reflect.DeepEqual(hydratableBlockFields(disk[i]), hydratableBlockFields(rows[i])) {
			return false
		}
	}
	return true
}

// capturePageLoadFingerprintForBlocks anchors the BLOCK editor's load baseline
// on the content its buffer actually holds (#1005). Unlike the markdown
// funnel (which hands the editor the disk bytes), the block funnel returns DB
// rows — during the watcher's re-index debounce those rows can still be the
// PRE-external state while the file already holds an external edit, and a
// plain disk capture would baseline bytes the editor does not hold: its first
// save then passes both gates and reverts the external managed edit. The rows
// are compared against a fresh parse of the disk bytes on the projected,
// content-bearing fields. On a match the baseline anchors the disk bytes
// exactly as before. On drift the baseline is pinned to the rows — the stat
// half poisoned so the fast path can never pass, the hash pinned to a
// canonical encoding of the rows so the arbiter refuses every save until a
// reload re-captures on the re-indexed rows: the external edit surfaces as a
// resolvable conflict instead of a silent revert.
func (a *App) capturePageLoadFingerprintForBlocks(filePath, notebook, section, page string, rows []parser.ParsedBlock) {
	if a == nil || filePath == "" {
		return
	}
	contentBytes, err := os.ReadFile(filePath)
	if err != nil {
		// Same swallow contract as capturePageLoadFingerprint: a missing or
		// unreadable file keeps the plain capture's absent/stat semantics.
		a.capturePageLoadFingerprint(filePath)
		return
	}
	diskBlocks, _, _, _, perr := parser.ParseFileContent(string(contentBytes),
		notebook, section, page, fileOrDefaultDate(filePath), a.spacesPerTab)
	if perr != nil {
		// Unparseable bytes: keep the byte-anchored baseline rather than
		// guessing content equivalence from a failed parse.
		a.capturePageLoadFingerprintFromBytes(filePath, contentBytes)
		return
	}
	if blocksContentMatchesRows(diskBlocks, rows) {
		a.capturePageLoadFingerprintFromBytes(filePath, contentBytes)
		return
	}
	pinned, ok := rowsPinnedFingerprint(rows)
	if !ok {
		a.capturePageLoadFingerprintFromBytes(filePath, contentBytes)
		return
	}
	a.storePageLoadFingerprint(core.NormalizeFileLockPath(filePath), pinned)
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

// hasRowsPinnedSentinel reports whether filePath's load baseline is the
// rows-pinned sentinel (#1005). Markdown-funnel loads consult it before
// re-anchoring: overwriting the sentinel with disk bytes would disarm the
// block editor's drift protection (see fetchPageMarkdown).
func (a *App) hasRowsPinnedSentinel(filePath string) bool {
	fp, ok := a.loadPageFingerprint(core.NormalizeFileLockPath(filePath))
	return ok && fp.Size == rowBasisSentinelSize
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
	baseline, ok := a.loadPageFingerprint(key)
	if !ok {
		// Page never loaded this session — nothing to re-baseline.
		return
	}
	if baseline.Size == rowBasisSentinelSize {
		// Rows-pinned sentinel (#1005): the editor holds rows that drifted
		// from disk, and only a reload re-anchors. This writer runs for every
		// trackedPageWrite caller — including plugin block ops rebased on
		// CURRENT disk bytes — and re-baselining here would disarm the pin,
		// letting the editor's pending save pass both gates and silently
		// revert the external edit the sentinel exists to surface. Same
		// decline-on-sentinel as refreshFingerprintAfterNormalization.
		// The sentinel outlives even a conflict resolution (resolve's writes
		// route here and decline too): recovery is the unconditional
		// re-anchor on the next blocks fetch — resolution emits block:changed,
		// which reloads the open surface. A user edit that autosaves in that
		// async window is false-refused once and needs a second resolution;
		// fail-closed beats a silent revert.
		return
	}
	a.rebaselineToFingerprintFromWrite(key, filePath, written)
}

// rebaselineToFingerprintFromWrite is the shared store half of a post-write
// re-baseline: stat the freshly written file and pin hash(written).
func (a *App) rebaselineToFingerprintFromWrite(key, filePath string, written []byte) {
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

// refreshFingerprintAfterNormalization is the watcher-hook variant of
// refreshFingerprintAfterWrite (#1005), wired to OnPageNormalized. The
// watcher's normalization write derives from whatever bytes the file held —
// including an external edit the live editor never adopted. Re-baselining
// unconditionally would make the pending editor save stat/hash-match the
// normalization bytes and silently revert that external edit, and would
// overwrite a rows-pinned sentinel baseline (which exists precisely to
// refuse until a reload re-anchors). So the refresh only runs when the
// stored baseline still hashes to the PRE-write bytes: the only delta since
// the editor's load is then Silt's own normalization, and re-baselining
// prevents a phantom conflict. Anything else — external edit, sentinel pin
// (whose hash is a canonical-rows encoding, never raw file bytes) — is left
// to drift so the save refuses and the conflict surfaces as resolvable.
func (a *App) refreshFingerprintAfterNormalization(filePath string, prior, written []byte) {
	if a == nil || filePath == "" {
		return
	}
	// The normalization write also invalidates prior MCP fetch bases —
	// unconditionally, unlike the guarded editor refresh below: reverting a
	// minted-id normalization would strip the id comments every ((uuid))
	// reference targets, and the MCP side has no prior-bytes guard to lean
	// on (the read_generation boundary is the only marker after a re-fetch
	// re-arms the shared slot).
	a.invalidateMCPReadBasis(filePath)
	key := core.NormalizeFileLockPath(filePath)
	baseline, ok := a.loadPageFingerprint(key)
	if !ok {
		// Page never loaded this session — nothing to re-baseline.
		return
	}
	priorSum := sha256.Sum256(prior)
	if hex.EncodeToString(priorSum[:]) != baseline.SHA256 {
		return
	}
	a.rebaselineToFingerprintFromWrite(key, filePath, written)
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
	return a.fingerprintGate(filePath, baseline, func(fp FileFingerprint) {
		a.storePageLoadFingerprint(key, fp)
	})
}

// isExternalEditConflict reports whether err is the external-edit refusal.
func isExternalEditConflict(err error) bool {
	return errors.Is(err, errExternalEditConflict)
}

func (a *App) storeRefusedWrite(key string, rw refusedPageWrite) {
	a.refusedWritesMu.Lock()
	defer a.refusedWritesMu.Unlock()
	if prev, exists := a.refusedWrites[key]; exists {
		// A programmatic payload (MCP whole-page write) never evicts an
		// editor-owned stash (#1005): the editor buffer is user typing whose
		// only backend-side copy is the stash, while the programmatic caller
		// already received its refusal error and can re-derive its payload.
		if prev.funnel.isEditorFunnel() && !rw.funnel.isEditorFunnel() {
			return
		}
		// Cross-funnel editor refusals keep the first stash: the two surfaces
		// hold different buffers, and the first stashed payload is the one
		// the conflict latch's owning surface can settle — replacing it would
		// silently destroy the other surface's only backend-side copy.
		if prev.funnel.isEditorFunnel() &&
			rw.funnel.isEditorFunnel() &&
			prev.funnel != rw.funnel {
			return
		}
		// Otherwise (same funnel, or a programmatic stash being superseded by
		// an editor refusal): newest-wins.
	}
	if a.refusedWrites == nil {
		a.refusedWrites = make(map[string]refusedPageWrite)
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
