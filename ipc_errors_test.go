package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// TestFormatIPCError_EmitsCodeIndependentOfMessage is the #478 pinning test: a
// backend wording change must NOT regress the frontend mapping. The formatter
// emits the stable code regardless of the message prose, so the frontend
// (which maps on the code) keeps rendering the friendly copy.
func TestFormatIPCError_EmitsCodeIndependentOfMessage(t *testing.T) {
	// An IPCError carrying the block_being_edited code with totally different
	// wording than the production sentinel — the code is what matters.
	err := NewIPCError(CodeBlockBeingEdited, "totally different wording than before")
	out := formatIPCError(err)
	s, ok := out.(string)
	if !ok {
		t.Fatalf("formatIPCError returned %T, want string", out)
	}
	var p ipcErrorPayload
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatalf("output is not JSON: %v (got %q)", err, s)
	}
	if p.Code != string(CodeBlockBeingEdited) {
		t.Errorf("code = %q, want %q — the code must be emitted independent of the message", p.Code, CodeBlockBeingEdited)
	}
	if p.Message != "totally different wording than before" {
		t.Errorf("message = %q, want the different wording preserved", p.Message)
	}
}

// TestFormatIPCError_VaultClosingAndCapability asserts the other two migrated
// sentinels serialize with their stable codes.
func TestFormatIPCError_VaultClosingAndCapability(t *testing.T) {
	out := formatIPCError(vaultClosingError())
	s, _ := out.(string)
	var p ipcErrorPayload
	if err := json.Unmarshal([]byte(s), &p); err != nil {
		t.Fatalf("vaultClosing output not JSON: %v", err)
	}
	if p.Code != string(CodeVaultClosing) {
		t.Errorf("vaultClosing code = %q, want %q", p.Code, CodeVaultClosing)
	}
}

// TestFormatIPCError_PlainErrorPassesThrough verifies an unmigrated error (one
// that is not an *IPCError or *CapabilityDeniedError) falls through to plain
// err.Error() prose — the pre-contract behavior, so nothing breaks while
// sentinels migrate incrementally.
func TestFormatIPCError_PlainErrorPassesThrough(t *testing.T) {
	plain := fmt.Errorf("some unmigrated sentinel prose")
	out := formatIPCError(plain)
	s, ok := out.(string)
	if !ok {
		t.Fatalf("formatIPCError returned %T for a plain error, want string", out)
	}
	if s != "some unmigrated sentinel prose" {
		t.Errorf("plain error output = %q, want the prose unchanged", s)
	}
}

// TestIPCError_ErrorsIsCompat asserts the post-migration errors keep
// errors.Is(err, sentinel) working via the wrapped sentinel — the mechanism
// that lets the 18+ existing test assertions pass without rewriting them.
func TestIPCError_ErrorsIsCompat(t *testing.T) {
	if !errors.Is(blockBeingEditedError(), errBlockBeingEdited) {
		t.Error("errors.Is(blockBeingEditedError(), errBlockBeingEdited) = false; the sentinel wrap regressed")
	}
	if !errors.Is(vaultClosingError(), errVaultClosing) {
		t.Error("errors.Is(vaultClosingError(), errVaultClosing) = false; the sentinel wrap regressed")
	}
	// An IPCError with no sentinel must NOT match any target.
	bare := NewIPCError(CodeBlockBeingEdited, "x")
	if errors.Is(bare, errBlockBeingEdited) {
		t.Error("a sentinel-less IPCError matched errBlockBeingEdited — Is should return false when sentinel is nil")
	}
	// Cross-check: a block_being_edited error does not satisfy vault_closing.
	if errors.Is(blockBeingEditedError(), errVaultClosing) {
		t.Error("blockBeingEditedError() matched errVaultClosing — sentinels cross-contaminated")
	}
}

// TestIPCError_ErrorReturnsMessage asserts the Go-side prose is the human
// message (callers that don't go through the formatter see the same text as
// before migration).
func TestIPCError_ErrorReturnsMessage(t *testing.T) {
	if got := blockBeingEditedError().Error(); got != errBlockBeingEdited.Error() {
		t.Errorf("blockBeingEditedError().Error() = %q, want %q (the sentinel prose)", got, errBlockBeingEdited.Error())
	}
}
