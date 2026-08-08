package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func writeAuditLines(t *testing.T, vault string, entries []AuditEntry) {
	t.Helper()
	path := AuditLogPath(vault)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatal(err)
		}
	}
}

func TestReadAuditLog_NewestFirstAndSkipsCorrupt(t *testing.T) {
	vault := t.TempDir()
	writeAuditLines(t, vault, []AuditEntry{
		{TS: "2026-01-01T00:00:00Z", Tool: "search_blocks", Outcome: "ok", Vault: "v"},
		{TS: "2026-01-01T00:00:01Z", Tool: "read_page", Outcome: "error", Vault: "v"},
	})
	// Append a corrupt line + a valid one.
	path := AuditLogPath(vault)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("not-json\n")
	_ = json.NewEncoder(f).Encode(AuditEntry{
		TS: "2026-01-01T00:00:02Z", Tool: "create_page", Outcome: "denied", Vault: "v",
	})
	_ = f.Close()

	got, err := ReadAuditLog(vault, 0)
	if err != nil {
		t.Fatalf("ReadAuditLog: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len=%d want 3 (corrupt skipped): %+v", len(got), got)
	}
	if got[0].Tool != "create_page" || got[2].Tool != "search_blocks" {
		t.Fatalf("expected newest-first order: %+v", got)
	}
}

func TestReadAuditLog_MissingFileEmpty(t *testing.T) {
	got, err := ReadAuditLog(t.TempDir(), 0)
	if err != nil {
		t.Fatalf("ReadAuditLog: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestReadAuditLog_EmptyVaultPathError(t *testing.T) {
	if _, err := ReadAuditLog("", 0); err == nil {
		t.Fatal("expected error for empty vault path")
	}
}

func TestClearAuditLog_EmptiesFile(t *testing.T) {
	vault := t.TempDir()
	writeAuditLines(t, vault, []AuditEntry{
		{TS: "2026-01-01T00:00:00Z", Tool: "search_blocks", Outcome: "ok", Vault: "v"},
	})
	if err := ClearAuditLog(vault); err != nil {
		t.Fatalf("ClearAuditLog: %v", err)
	}
	got, err := ReadAuditLog(vault, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty after clear, got %+v", got)
	}
}

func TestReadAuditLog_LimitCap(t *testing.T) {
	vault := t.TempDir()
	writeAuditLines(t, vault, []AuditEntry{
		{TS: "2026-01-01T00:00:00Z", Tool: "a", Outcome: "ok", Vault: "v"},
		{TS: "2026-01-01T00:00:01Z", Tool: "b", Outcome: "ok", Vault: "v"},
		{TS: "2026-01-01T00:00:02Z", Tool: "c", Outcome: "ok", Vault: "v"},
	})
	got, err := ReadAuditLog(vault, 2)
	if err != nil {
		t.Fatalf("ReadAuditLog: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(got), got)
	}
	// Newest-first: last two lines are b then c on disk → c, b after reverse.
	if got[0].Tool != "c" || got[1].Tool != "b" {
		t.Fatalf("want newest-first [c,b], got %+v", got)
	}
}

func TestFileAuditor_ClearThenRecord(t *testing.T) {
	vault := t.TempDir()
	fa, err := newFileAuditor(vault)
	if err != nil {
		t.Fatal(err)
	}
	defer fa.Close()
	fa.Record(AuditEntry{Tool: "search_blocks", Outcome: "ok", Vault: "v"})
	// Clear serializes under the same mutex as Record (no sleep needed).
	if err := fa.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	got, err := ReadAuditLog(vault, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty after Clear, got %+v", got)
	}
	fa.Record(AuditEntry{Tool: "read_page", Outcome: "ok", Vault: "v"})
	got, err = ReadAuditLog(vault, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Tool != "read_page" {
		t.Fatalf("post-clear record: %+v", got)
	}
}

func TestHost_ClearAudit_WithFileAuditor(t *testing.T) {
	vault := t.TempDir()
	h := NewHost(Options{Keyring: nil, Version: "test"})
	bridge := &fakeBridge{path: vault}
	if err := h.Start(bridge, Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer h.Stop()

	// Record via the live auditor.
	h.mu.RLock()
	aud := h.audit
	h.mu.RUnlock()
	if aud == nil {
		t.Fatal("expected auditor")
	}
	aud.Record(AuditEntry{Tool: "search_blocks", Outcome: "ok", Vault: "v"})

	if err := h.ClearAudit(vault); err != nil {
		t.Fatalf("ClearAudit: %v", err)
	}
	got, err := ReadAuditLog(vault, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}

func TestHost_ClearAudit_WhenStopped(t *testing.T) {
	vault := t.TempDir()
	writeAuditLines(t, vault, []AuditEntry{
		{TS: "2026-01-01T00:00:00Z", Tool: "search_blocks", Outcome: "ok", Vault: "v"},
	})
	h := NewHost(Options{Version: "test"})
	if err := h.ClearAudit(vault); err != nil {
		t.Fatalf("ClearAudit: %v", err)
	}
	got, err := ReadAuditLog(vault, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty, got %+v", got)
	}
}
