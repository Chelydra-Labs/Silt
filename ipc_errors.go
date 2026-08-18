package main

// IPC error-code contract (#478).
//
// Wails v2 serializes a bound method's `error` return across the IPC bridge,
// but the JS runtime wraps it in `new Error(t.error)` — so a Go error arrives
// in TypeScript as an `Error` whose `.message` is `err.Error()`. A custom Go
// error struct's tagged fields do NOT survive: returning an object from the
// ErrorFormatter flattens to `"[object Object]"` on `.message`.
//
// This package's contract: a typed IPCError carries a machine-readable Code
// alongside the human Message. The App's ErrorFormatter (set in main.go)
// serializes an IPCError (or a CapabilityDeniedError) as a JSON STRING —
// `{"code":"...","message":"...""}` — which survives `new Error()` intact on
// `.message`. The frontend's `coerceIPCError` (lib/ipcError.ts) JSON.parses
// `.message` to recover the code; if parsing fails (a plain-prose error from
// an unmigrated sentinel), it falls back to the raw message. This lets the
// frontend map on a stable code instead of substring-matching Go prose, so a
// backend wording change can no longer silently regress the friendly mapping.

import (
	"encoding/json"
	"errors"
	"silt/backend/plugins"
)

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
	CodePageStillExists       IPCErrorCode = "page_still_exists"
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

// ipcErrorPayload is the JSON shape the ErrorFormatter emits for an IPCError.
// It is serialized to a STRING (not an object) so the Wails JS runtime's
// `new Error(t.error)` preserves it on `.message` — the frontend parses it
// back via coerceIPCError. The shape is also the documented contract for
// plugin SDK error coercion.
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
