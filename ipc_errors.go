package main

// IPC error-code contract (#478).
//
// Wails v3 carries a bound method's `error` return across the IPC bridge as
// a 422 CallError body: `{"message": <prose>, "cause": <envelope>,
// "kind": "RuntimeError"}`. This app's MarshalError hook (formatIPCError,
// wired in main.go) supplies that cause — for an IPCError or
// CapabilityDeniedError it is the `{"code":"...","message":"..."}`
// envelope; any other error gets nil (Wails' default handling, the
// pre-contract behavior). The JS runtime throws `new Error(json.message)`
// with the already-parsed cause attached on `.cause`, so the frontend's
// `coerceIPCError` (lib/ipcError.ts) reads the cause to recover the code and
// falls back to plain prose when there is none. This lets the frontend map on
// a stable code instead of substring-matching Go prose, so a backend wording
// change can no longer silently regress the friendly mapping.

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"silt/backend/parser"
	"silt/backend/plugins"
)

// errExternalEditConflict is the sentinel for a refused whole-page save whose
// file changed on disk since the page was loaded (#1005). Kept as the
// errors.Is target (mirroring errBlockBeingEdited) so test assertions and the
// IPC code contract both work; return sites hand back an
// externalEditConflictError() carrying CodeExternalEditConflict.
var errExternalEditConflict = fmt.Errorf("this page changed on disk since it was opened")

// externalEditConflictError refuses the save, naming the file so the frontend
// banner can point the user at the right page.
func externalEditConflictError(filePath string) *IPCError {
	return wrapSentinelAsIPCError(CodeExternalEditConflict,
		fmt.Sprintf("%s: %s", errExternalEditConflict.Error(), filepath.Base(filePath)),
		errExternalEditConflict)
}

// errExternalEditConflictWrap refuses the save when the file's current state
// cannot be established (stat/read failure). Fail closed: an unverifiable
// baseline must not degrade into a blind overwrite. The extra
// errExternalEditUnverified sentinel marks this refusal as unverified so the
// stash policy can treat it differently from a verified hash mismatch: an
// unverifiable conflict must not mint a stash (#1005).
func errExternalEditConflictWrap(err error) error {
	return wrapSentinelAsIPCError(CodeExternalEditConflict,
		fmt.Sprintf("could not verify this page is unchanged since it was opened: %v", err),
		errors.Join(errExternalEditConflict, errExternalEditUnverified))
}

// errExternalEditUnverified is the marker sentinel wrapped by
// errExternalEditConflictWrap (alongside errExternalEditConflict): the refusal
// fired because the file's state could not be established, not because a
// hash mismatch proved an external version exists. Stashing on such a
// refusal would let a transient stat/read error (EACCES, EIO, a briefly
// disconnected SMB/NFS mount) mint a sticky, manually-resolvable stash that
// outlives the error — refusing alone lets the next save self-heal.
var errExternalEditUnverified = fmt.Errorf("page state could not be verified")

// verifiedWriteFailedError surfaces a hard verified-write failure (#1009):
// the write could not be landed durably even after retry + restore. Fail
// loud. The recovery prose is single-sourced in VerifiedWriteError.Error(),
// built from the retention state that actually holds on disk — this boundary
// only attaches the typed code and passes the message through verbatim; a
// second wording here could drift from what the fields prove.
func verifiedWriteFailedError(vwe *parser.VerifiedWriteError) *IPCError {
	return NewIPCError(CodeVerifiedWriteFailed, vwe.Error())
}

// IPCErrorCode is the stable, serialized error code crossing the Wails
// boundary. Add new constants here as user-facing sentinels migrate (#478
// ships the three most common; the rest migrate as they're touched).
type IPCErrorCode string

const (
	// CodeBlockBeingEdited: the focus-lock guard rejected a write because the
	// target file is open in the editor (errBlockBeingEdited). Maps to the
	// friendly "save or close it first" copy.
	CodeBlockBeingEdited IPCErrorCode = "block_being_edited"
	// CodeVaultClosing: a plugin AI call was rejected because a vault close/
	// switch is in progress (errVaultClosing). Transient; the UI is unmounting.
	CodeVaultClosing IPCErrorCode = "vault_closing"
	// CodeCapabilityDenied: a plugin SDK binding was rejected for lack of a
	// granted capability (plugins.CapabilityDeniedError).
	CodeCapabilityDenied IPCErrorCode = "capability_denied"
	// CodePageExists: CreatePageFromTemplate refused to write because the
	// target page path already exists (no clobber; #652). Frontend offers
	// rename or open-existing. Deleted restore uses this only for dest occupancy.
	CodePageExists IPCErrorCode = "page_exists"
	// CodePageStillExists: RestoreDeletedPageVersion refused because the
	// original live file is still on disk. Frontend should open live page
	// history, not Restore as….
	CodePageStillExists IPCErrorCode = "page_still_exists"
	// CodePageHistoryExists: Restore-as refused because the dest name has
	// leftover snapshots. Relocating would merge those onto the source
	// locator; restore dest in place or pick another name.
	CodePageHistoryExists IPCErrorCode = "page_history_exists"
	// CodePageWriteStale: SaveFileBlocks/SavePageMarkdown snapshotted a page
	// write epoch, then a restore committed before the save acquired the
	// file lock. The save must not overwrite the restored body.
	CodePageWriteStale        IPCErrorCode = "page_write_stale"
	CodeInvalidNavigationPath IPCErrorCode = "invalid_navigation_path"
	CodeNavigationNotFound    IPCErrorCode = "navigation_not_found"
	CodeNavigationConflict    IPCErrorCode = "navigation_conflict"
	CodeNavigationUnavailable IPCErrorCode = "navigation_unavailable"
	CodeNavigationDuplicate   IPCErrorCode = "navigation_duplicate"
	CodeNavigationReveal      IPCErrorCode = "navigation_reveal_failed"
	// CodeAmbiguousTarget: PromoteUnlinkedMention rejected because the target
	// could not be resolved to a single page — the explicit path is missing
	// from inventory and the leaf name is ambiguous. Empty titles return a
	// plain error instead (not this code). The unlinked-mentions UI offers
	// candidate chips for one-click disambiguation; this is the server-side
	// safety net when no unique path is supplied.
	CodeAmbiguousTarget IPCErrorCode = "ambiguous_target"
	// CodeExternalEditConflict: a whole-page editor save was refused because
	// the file changed on disk since it was loaded (external writer: Obsidian,
	// sync client, second Silt window). Distinct from CodePageWriteStale —
	// the resolution UX is keep-mine/theirs/both, not reload-and-retry (#1005).
	CodeExternalEditConflict IPCErrorCode = "external_edit_conflict"
	// CodeVerifiedWriteFailed: a page write on a detected non-atomic
	// filesystem could not be verified after backup + retry + restore (#1009).
	// The message is single-sourced from the actual retention state and names
	// only recovery copies that exist on disk. When the restore itself could
	// not be verified (Restored=false), the target may still hold torn bytes —
	// the message says so, the named .pending/backup copies are the manual
	// recovery path, and consumers must never index or adopt the target's
	// bytes after this code.
	CodeVerifiedWriteFailed IPCErrorCode = "verified_write_failed"
)

// IPCError carries a machine-readable Code across the Wails boundary so the
// frontend can map on the code, not a substring of the prose. Error() returns
// Message (the human text) so callers that don't go through the formatter
// (Go-side tests, internal calls) see the same prose as before.
//
// sentinel is the optional package-private sentinel this error wraps (e.g.
// errBlockBeingEdited). When set, errors.Is(err, sentinel) returns true via
// the Is method below — preserving the 18+ existing errors.Is assertions
// without rewriting them after migration.
type IPCError struct {
	Code     IPCErrorCode
	Message  string
	sentinel error // optional; for errors.Is compatibility with a pre-migration sentinel
}

// Error returns the human message (the Go-side prose), matching pre-migration
// behavior for callers that consume the error directly.
func (e *IPCError) Error() string { return e.Message }

// Is reports whether this error wraps the pre-migration sentinel. This keeps
// `errors.Is(err, errBlockBeingEdited)` working after the sentinel's return
// sites migrate to IPCError-carriers, so the existing 18+ test assertions
// pass unchanged. sentinel may be nil (e.g. an IPCError with no legacy
// counterpart); in that case Is returns false for every target.
func (e *IPCError) Is(target error) bool {
	if e.sentinel == nil {
		return false
	}
	return errors.Is(e.sentinel, target)
}

// NewIPCError constructs an IPCError carrying a code + message but no legacy
// sentinel (errors.Is against any pre-migration sentinel returns false).
func NewIPCError(code IPCErrorCode, msg string) *IPCError {
	return &IPCError{Code: code, Message: msg}
}

// wrapSentinelAsIPCError constructs an IPCError carrying a code + message that
// ALSO satisfies errors.Is(err, sentinel) — used when migrating a pre-migration
// sentinel var to the code contract without rewriting its errors.Is callers.
func wrapSentinelAsIPCError(code IPCErrorCode, msg string, sentinel error) *IPCError {
	return &IPCError{Code: code, Message: msg, sentinel: sentinel}
}

// ipcErrorPayload is the JSON shape the MarshalError hook emits for an
// IPCError. Wails v3 inlines these bytes as the CallError's `cause`, which
// the JS runtime attaches (already parsed) to the thrown Error's `.cause` —
// the frontend recovers it via coerceIPCError. The shape is also the
// documented contract for plugin SDK error coercion.
type ipcErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// capabilityDeniedPayload extends ipcErrorPayload with the structured fields a
// capability denial carries (plugin id + capability name), so the frontend can
// surface a precise "grant X to plugin Y" message without parsing prose.
type capabilityDeniedPayload struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	Plugin     string `json:"plugin,omitempty"`
	Capability string `json:"capability,omitempty"`
	Requested  string `json:"requested,omitempty"`
	Granted    string `json:"granted,omitempty"`
	Disabled   bool   `json:"disabled,omitempty"`
}

// formatIPCError serializes an error into the JSON-byte form the Wails v3
// MarshalError callback returns. It recognizes the three user-facing sentinels
// this contract migrates (#478): *IPCError (covers errBlockBeingEdited +
// errVaultClosing via their helper constructors) and *CapabilityDeniedError.
// Any other error returns nil to fall back to Wails' default error handling
// (the pre-contract behavior), so unmigrated sentinels keep working while the
// frontend falls back to substring matching for them.
func formatIPCError(err error) []byte {
	if err == nil {
		return nil
	}
	var ipc *IPCError
	if errors.As(err, &ipc) {
		b, _ := json.Marshal(ipcErrorPayload{Code: string(ipc.Code), Message: ipc.Message})
		return b
	}
	var cap *plugins.CapabilityDeniedError
	if errors.As(err, &cap) {
		b, _ := json.Marshal(capabilityDeniedPayload{
			Code:       string(CodeCapabilityDenied),
			Message:    cap.Error(),
			Plugin:     cap.Plugin,
			Capability: cap.Capability,
			Requested:  cap.Requested,
			Granted:    cap.Granted,
			Disabled:   cap.Disabled,
		})
		return b
	}
	return nil
}
