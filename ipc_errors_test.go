package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	if out == nil {
		t.Fatal("formatIPCError returned nil for an IPCError")
	}
	var p ipcErrorPayload
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("output is not JSON: %v (got %q)", err, out)
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
	if out == nil {
		t.Fatal("formatIPCError returned nil for vaultClosingError")
	}
	var p ipcErrorPayload
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatalf("vaultClosing output not JSON: %v", err)
	}
	if p.Code != string(CodeVaultClosing) {
		t.Errorf("vaultClosing code = %q, want %q", p.Code, CodeVaultClosing)
	}
}

// TestFormatIPCError_PlainErrorPassesThrough verifies an unmigrated error (one
// that is not an *IPCError or *CapabilityDeniedError) returns nil so Wails'
// default error handler kicks in — the pre-contract behavior, so nothing breaks
// while sentinels migrate incrementally.
func TestFormatIPCError_PlainErrorPassesThrough(t *testing.T) {
	plain := fmt.Errorf("some unmigrated sentinel prose")
	out := formatIPCError(plain)
	if out != nil {
		t.Fatalf("formatIPCError returned %q for a plain error, want nil (default handler)", out)
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

// TestCallErrorBody_CarriesEnvelopeInCause pins the Wails v3 wire contract the
// frontend coercion depends on (#1019): a failed binding call rejects with a
// 422 CallError body carrying the human PROSE on `message` and the
// formatIPCError envelope inlined as a JSON OBJECT on `cause`. The v3 runtime
// throws `new Error(json.message)` with that parsed cause attached, which is
// where the frontend recovers the stable code. If a Wails upgrade changes the
// marshaling (base64-encodes the cause, moves the envelope), every code-based
// frontend discrimination breaks silently — this test fails loudly instead.
func TestCallErrorBody_CarriesEnvelopeInCause(t *testing.T) {
	ipcErr := blockBeingEditedError()
	// Exactly what wails v3 pkg/application/bindings.go builds for a method
	// with one error output: MarshalError (our formatIPCError) becomes the
	// json.RawMessage cause.
	cerr := &application.CallError{
		Message: ipcErr.Error(),
		Cause:   json.RawMessage(formatIPCError(ipcErr)),
		Kind:    application.RuntimeError,
	}
	body, err := json.Marshal(cerr)
	if err != nil {
		t.Fatalf("marshal CallError: %v", err)
	}
	var got struct {
		Message string          `json:"message"`
		Cause   json.RawMessage `json:"cause"`
		Kind    string          `json:"kind"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal CallError body: %v", err)
	}
	if got.Message != errBlockBeingEdited.Error() {
		t.Errorf("message = %q, want the human prose %q", got.Message, errBlockBeingEdited.Error())
	}
	if got.Kind != "RuntimeError" {
		t.Errorf("kind = %q, want RuntimeError", got.Kind)
	}
	var env ipcErrorPayload
	if err := json.Unmarshal(got.Cause, &env); err != nil {
		t.Fatalf("cause is not an inlined JSON object (wire contract broken): %v (cause=%s)", err, got.Cause)
	}
	if env.Code != string(CodeBlockBeingEdited) {
		t.Errorf("cause.code = %q, want %q", env.Code, CodeBlockBeingEdited)
	}
	if env.Message != errBlockBeingEdited.Error() {
		t.Errorf("cause.message = %q, want %q", env.Message, errBlockBeingEdited.Error())
	}
}
