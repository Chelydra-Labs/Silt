package parser

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Verified-write mode of the staged writer (#1009).
//
// On filesystems where the step-4 rename of WriteFileAtomic is not atomic
// (NFS/SMB/FUSE mounts), a "successful" rename can still leave a torn file at
// the target. Silt does NOT switch to copy-in-place (that trades a torn rename
// for a torn write); instead the staged sequence is wrapped with recovery:
//
//  1. Pre-write backup: the current target, if any, is copied to backupPath on
//     the SAME mount, fsync'd (content and directory entry). One generation
//     only — the next write's copy sweeps (replaces) it.
//  2. The staged sequence itself (WriteFileAtomic), unchanged.
//  3. Post-rename read-back: size + sha256 of the target vs the written bytes.
//     Pass → done (and a stale .pending copy from an earlier hard failure is
//     swept).
//  4. Torn result → the staged write is retried once. Still torn → the target
//     is restored from the pre-write bytes (or removed, for a new page), the
//     incoming bytes are retained next to the backup, and a *VerifiedWriteError
//     is returned: the target's bytes are untrusted and must never be indexed
//     or adopted as the page.
//
// The caller runs this inside its file write transaction, so steps 3–4 race
// no other Silt writer; the app layer re-baselines its load fingerprint from
// the same transaction (see app_tracked_write.go).

// VerifiedWriteAfterRenameHook is a nil-in-production test seam: when set, it
// runs after every rename inside WriteFileAtomicVerified (initial write,
// retry, restore) so tests can simulate a torn destination by corrupting the
// target post-rename (#1009) — a state a CI temp dir cannot reproduce for
// real. Exported because the app-level tests drive it through the save
// funnels; a non-nil return fails the write like any other step failure.
var VerifiedWriteAfterRenameHook func(path string) error

// VerifiedWriteError reports a verified write that could not land durably.
// It is the fail-loud terminal state of the recovery ladder: the caller must
// surface it (never retry silently), and Error() names the retained copies so
// the user has a manual recovery path even across a process restart.
//
// The copy fields are populated ONLY when the copy actually landed on disk —
// a path that does not exist must never be offered as a recovery path.
type VerifiedWriteError struct {
	// Target is the page path the write was attempting.
	Target string
	// Backup is the pre-write backup path, set only when the backup copy was
	// actually written ("" when no backup was requested, the page was new, or
	// the pre-write step failed before any copy existed).
	Backup string
	// Pending is where the incoming bytes were retained (backupPath +
	// ".pending"; "" when retention itself failed — see RetentionErr — or no
	// backup location was configured).
	Pending string
	// Restored reports whether the target was verified back at its exact
	// pre-write state (for a new page: absent again).
	Restored bool
	// PreWriteFailed marks a failure in the pre-write backup step: the staged
	// write never started, so the target was not modified by this write and
	// no recovery copy exists.
	PreWriteFailed bool
	// newPage records that the target had no previous version (the pre-write
	// state is "absent"): a successful restore removed the torn file rather
	// than re-writing a previous version, and Error() must not claim one.
	newPage bool
	// Err is the underlying step failure (rename, read-back, restore), if any.
	Err error
	// RetentionErr is set when the incoming bytes could NOT be retained
	// (pending copy failed) — the user should know their text exists only in
	// the erroring caller's memory (an editor buffer).
	RetentionErr error
}

// Error is the single source of the recovery message: every consumer (the
// IPC conversion boundary, logs, tests) renders it verbatim, so the prose
// cannot fork between surfaces. It states only what the fields prove —
// which recovery copies exist on disk and whether the target was restored.
func (e *VerifiedWriteError) Error() string {
	name := filepath.Base(e.Target)
	var msg string
	switch {
	case e.PreWriteFailed:
		msg = fmt.Sprintf("%s could not be written safely and was not modified", name)
	case e.Restored && e.newPage:
		// A new page's pre-write state is "absent": the restore removed the
		// torn file — there was no previous version to restore.
		msg = fmt.Sprintf("%s could not be written safely and the partial file was removed", name)
	case e.Restored:
		msg = fmt.Sprintf("%s could not be written safely and the previous version was restored", name)
	default:
		if e.newPage {
			// No previous version ever existed; the removal failed, so the
			// truthful state is torn partial bytes still at the target —
			// where the next startup scan will index them.
			msg = fmt.Sprintf("%s could not be written safely and the torn partial file remains at %s", name, e.Target)
		} else {
			msg = fmt.Sprintf("%s could not be written safely and its previous version could not be restored", name)
		}
	}
	switch {
	case e.Pending != "":
		msg += fmt.Sprintf(". Your unsaved text is kept at %s", e.Pending)
		if e.Backup != "" {
			msg += fmt.Sprintf(" and the pre-write backup is at %s", e.Backup)
		}
	case e.Backup != "":
		msg += fmt.Sprintf(". The pre-write backup is at %s (the unsaved text could not be retained on disk: %v)", e.Backup, e.RetentionErr)
	default:
		msg += ". Neither a backup nor an unsaved copy could be retained on disk"
		// Same diagnosis aid as the branch above: without the cause, a
		// "my text is gone" field report has only the server log to go on.
		if e.RetentionErr != nil {
			msg += fmt.Sprintf(": %v", e.RetentionErr)
		}
	}
	return msg
}

func (e *VerifiedWriteError) Unwrap() error { return e.Err }

// IsVerifiedWriteError reports whether err is (or wraps) a VerifiedWriteError.
func IsVerifiedWriteError(err error) bool {
	var vwe *VerifiedWriteError
	return errors.As(err, &vwe)
}

// WriteFileAtomicVerified performs the verified staged write described at the
// top of this file. backupPath may be empty, which disables the on-disk backup
// and pending retention (read-back verification and in-memory restore still
// run — a caller without a backup location keeps verification, just not the
// cross-restart recovery copies).
func WriteFileAtomicVerified(path, backupPath string, content []byte) error {
	// 1. Pre-write backup (same mount); bytes retained in memory for restore.
	prev, hadPrev, err := backupTarget(path, backupPath)
	if err != nil {
		// Nothing has been written yet — the target is untouched. Fail loud:
		// proceeding without the promised backup would silently downgrade the
		// verified contract. The backup copy did not land and retention never
		// ran, so both copy fields stay empty — the error must not name
		// recovery paths that do not exist.
		return &VerifiedWriteError{
			Target: path, Err: err, PreWriteFailed: true,
		}
	}

	// 2+3. Staged write, then read-back verify. A failed/torn first pass is
	// retried exactly once (a fresh staged write — the first attempt's temp
	// was consumed by its rename) before the recovery ladder takes over.
	if err := verifiedStagedPass(path, content); err != nil {
		if retryErr := verifiedStagedPass(path, content); retryErr != nil {
			return restoreAfterFailure(path, backupPath, prev, hadPrev, content, errors.Join(err, retryErr))
		}
	}

	// Verified: the target durably holds the new bytes. Sweep any .pending
	// copy left by an earlier hard failure — it is superseded content. The
	// pre-write backup itself is deliberately RETAINED as the single
	// crash-recovery generation until the next write's step 1 replaces it.
	if p := pendingPath(backupPath); p != "" {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			// Best-effort: a stale recovery copy outliving its failure is
			// clutter, not a data-safety problem.
			_ = err
		}
	}
	return nil
}

// verifiedStagedPass runs one staged write and verifies the target's bytes.
// It returns nil only when the read-back size+hash matches content exactly.
func verifiedStagedPass(path string, content []byte) error {
	if err := WriteFileAtomic(path, content); err != nil {
		return fmt.Errorf("staged write failed: %w", err)
	}
	if hook := VerifiedWriteAfterRenameHook; hook != nil {
		if err := hook(path); err != nil {
			return fmt.Errorf("post-rename hook failed: %w", err)
		}
	}
	ok, err := verifyTargetBytes(path, content)
	if err != nil {
		return fmt.Errorf("read-back verification failed: %w", err)
	}
	if !ok {
		return errors.New("torn destination: target bytes do not match the staged write")
	}
	return nil
}

// restoreAfterFailure is the recovery ladder once the (retried) staged write
// could not be verified: retain the incoming bytes, put the target back to its
// pre-write state, and always return a hard *VerifiedWriteError.
func restoreAfterFailure(path, backupPath string, prev []byte, hadPrev bool, content []byte, cause error) error {
	vwe := &VerifiedWriteError{Target: path, Err: cause}
	// Only a page that HAD a previous version wrote a backup in step 1;
	// naming backupPath for a new page would point at a file that was never
	// created.
	if hadPrev {
		vwe.Backup = backupPath
	}
	if p := pendingPath(backupPath); p != "" {
		if rerr := writeDurableFile(p, content); rerr != nil {
			// Retention failing must not block the restore; record it so the
			// surfaced error can say the new text was NOT retained on disk.
			// Pending stays empty: the path was never written.
			vwe.RetentionErr = rerr
		} else {
			vwe.Pending = p
		}
	}

	if hadPrev {
		// Restore = one more staged write of the exact pre-write bytes, then
		// re-verify. The restore is NOT retried: two consecutive torn results
		// mean the mount cannot be trusted to land anything.
		if rerr := WriteFileAtomic(path, prev); rerr != nil {
			vwe.Err = errors.Join(cause, fmt.Errorf("restore write failed: %w", rerr))
			return vwe
		}
		if hook := VerifiedWriteAfterRenameHook; hook != nil {
			if herr := hook(path); herr != nil {
				vwe.Err = errors.Join(cause, fmt.Errorf("restore hook failed: %w", herr))
				return vwe
			}
		}
		ok, verr := verifyTargetBytes(path, prev)
		if verr != nil || !ok {
			if verr != nil {
				verr = fmt.Errorf("restore read-back failed: %w", verr)
			} else {
				verr = errors.New("restore left the target torn")
			}
			vwe.Err = errors.Join(cause, verr)
			return vwe
		}
		vwe.Restored = true
		return vwe
	}

	// New page: the pre-write state is "absent". Remove the torn target so no
	// partial file is presented as the page.
	vwe.newPage = true
	if rerr := os.Remove(path); rerr != nil && !os.IsNotExist(rerr) {
		vwe.Err = errors.Join(cause, fmt.Errorf("removing torn new page failed: %w", rerr))
		return vwe
	}
	vwe.Restored = true
	return vwe
}

// verifyTargetBytes reports whether path's bytes are exactly want, by length
// and sha256 — the cheapest sound equality check for the sizes involved.
func verifyTargetBytes(path string, want []byte) (bool, error) {
	got, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if len(got) != len(want) {
		return false, nil
	}
	gotSum, wantSum := sha256.Sum256(got), sha256.Sum256(want)
	return gotSum == wantSum, nil
}

// backupTarget copies the current target (if it exists) to backupPath and
// returns its bytes. When backupPath is empty or the target is absent, no
// backup is written; the bytes are still read when present so the caller can
// restore from memory.
func backupTarget(path, backupPath string) (prev []byte, hadPrev bool, err error) {
	prev, err = os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if backupPath == "" {
		return prev, true, nil
	}
	if err := writeDurableFile(backupPath, prev); err != nil {
		return nil, true, fmt.Errorf("backup copy to %s failed: %w", backupPath, err)
	}
	return prev, true, nil
}

// writeDurableFile durably writes data to dest: MkdirAll, create/truncate,
// write, fsync, close, then fsync the parent directory so the copy's
// directory entry is as durable as its content — the backup/.pending files
// are the crash-recovery contract, and file-content fsync alone does not
// guarantee the entry survives power loss. A plain copy (not a staged
// rename) — backup and retained-copy files are private recovery state that
// is never swapped into a page path.
func writeDurableFile(dest string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return syncDirEntry(filepath.Dir(dest))
}

// pendingPath names the retained copy of the incoming bytes for a hard
// failure: <backupPath>.pending, so it sits beside the backup it belongs to.
func pendingPath(backupPath string) string {
	if backupPath == "" {
		return ""
	}
	return backupPath + ".pending"
}
