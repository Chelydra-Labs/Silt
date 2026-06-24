package main

// audit_trail.go — durable trust-boundary audit log (#252, audit F23).
//
// A vault-scoped <vault>/.system/audit.log captures one-line JSON entries for
// every plugin install/uninstall/enable/disable, capability grant/revoke, and
// linked-notebook link/unlink. The log is a forensic append-only diagnostic
// artifact — best-effort, 0o600, capped at 1 MB with tail rotation. It is NOT
// a source of truth (the storage tiers in ARCHITECTURE.md §0 own that); it is
// the host-side record that answers "when did this plugin get granted
// network?" without forensics on config.yaml mtime.
//
// The write/rotate pattern mirrors plugin_audit.go (the per-plugin network
// audit log). Both use single-line JSON per entry (#254) so the format is
// self-describing and parseable by jq / SIEM ingest.

import (
	"encoding/json"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// maxAuditLogBytes bounds the on-disk audit.log so it cannot grow unbounded
// across a long session. When exceeded, the log is truncated to the most
// recent maxAuditLogLines (tail rotation, mirroring truncateNetworkLog).
const (
	maxAuditLogBytes = 1 * 1024 * 1024 // 1 MB
	maxAuditLogLines = 500             // keep-lines on truncation
)

// auditTrailMu serializes concurrent appends + rotations to audit.log so the
// cap check and the append are atomic relative to each other. Held only across
// the file I/O, never across the mutation that triggered the audit entry.
var auditTrailMu sync.Mutex

// AuditEntry is one row of the durable audit trail. Common fields (At, Actor,
// Action) are always present; action-specific fields use omitempty so the JSON
// line stays compact.
type AuditEntry struct {
	At          string `json:"at"`                    // RFC3339
	Actor       string `json:"actor"`                 // OS username (host user)
	Action      string `json:"action"`                // install/uninstall/enable/disable/grant/revoke/link/unlink
	PluginID    string `json:"plugin_id,omitempty"`   // install/uninstall/enable/disable/grant/revoke
	Author      string `json:"author,omitempty"`      // install
	Version     string `json:"version,omitempty"`     // install
	SHA256      string `json:"sha256,omitempty"`      // install (index.js content hash)
	Capability  string `json:"capability,omitempty"`  // grant/revoke
	Qualifier   string `json:"qualifier,omitempty"`   // grant
	ID          string `json:"id,omitempty"`          // link/unlink (linked-notebook id)
	RootPath    string `json:"root_path,omitempty"`   // link/unlink (linked-notebook root)
	Fingerprint string `json:"fingerprint,omitempty"` // link (pairs with F3 root fingerprint)
}

// auditActor returns the OS username Silt is running as. Var so tests can
// verify determinism without depending on the host user; the production value
// uses os/user.Current().Username. Returns "unknown" on error (e.g.
// cross-compiled without cgo on some platforms).
var auditActor = func() string {
	u, err := user.Current()
	if err != nil || u.Username == "" {
		return "unknown"
	}
	return u.Username
}

// newAuditEntry constructs an AuditEntry with the common timestamp + actor
// fields filled. Action-specific fields are set by the caller before passing
// to appendAuditEntry.
func newAuditEntry(action string) *AuditEntry {
	return &AuditEntry{
		At:     time.Now().Format(time.RFC3339),
		Actor:  auditActor(),
		Action: action,
	}
}

// auditLogPath returns the on-disk path for the vault's audit trail.
func auditLogPath(vaultPath string) string {
	return filepath.Join(vaultPath, ".system", "audit.log")
}

// appendAuditEntry writes one JSON entry to <vault>/.system/audit.log. The
// write is append-only (O_APPEND), 0o600, and serialized by auditTrailMu.
// Best-effort — errors are logged, never surfaced to the caller (the audit
// trail is a diagnostic aid, not a correctness path; a failed audit write
// must not roll back the mutation that triggered it). The file is rotated
// (tail-truncated to maxAuditLogLines) when it exceeds maxAuditLogBytes.
func appendAuditEntry(vaultPath string, entry *AuditEntry) {
	if vaultPath == "" {
		return
	}
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("appendAuditEntry: json.Marshal failed: %v", err)
		return
	}
	logPath := auditLogPath(vaultPath)
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)

	auditTrailMu.Lock()
	defer auditTrailMu.Unlock()

	if info, err := os.Stat(logPath); err == nil && info.Size() > maxAuditLogBytes {
		rotateAuditLog(logPath, maxAuditLogLines)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		log.Printf("appendAuditEntry: open failed: %v", err)
		return
	}
	_, _ = f.Write(append(data, '\n'))
	_ = f.Close()
}

// rotateAuditLog reads the log file, keeps the last keepLines, and rewrites it.
// Best-effort — errors are silently ignored (the audit log is diagnostic).
// Mirrors truncateNetworkLog.
func rotateAuditLog(path string, keepLines int) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) <= keepLines {
		return
	}
	kept := lines[len(lines)-keepLines:]
	_ = os.WriteFile(path, []byte(strings.Join(kept, "\n")+"\n"), 0o600)
}

// GetAuditLog returns the durable audit trail entries (oldest-first) for the
// Settings → Diagnostics panel. Pre-vault returns an empty slice. Lines that
// fail to parse as JSON are silently skipped (best-effort — a corrupt line
// should not hide the entries around it).
func (a *App) GetAuditLog() ([]AuditEntry, error) {
	if a.vaultPath == "" {
		return []AuditEntry{}, nil
	}
	data, err := os.ReadFile(auditLogPath(a.vaultPath))
	if err != nil {
		if os.IsNotExist(err) {
			return []AuditEntry{}, nil
		}
		return nil, err
	}
	var entries []AuditEntry
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		var entry AuditEntry
		if err := json.Unmarshal([]byte(line), &entry); err == nil {
			entries = append(entries, entry)
		}
	}
	if entries == nil {
		return []AuditEntry{}, nil
	}
	return entries, nil
}

// ClearAuditLog empties the audit trail file (user-initiated, from Settings →
// Diagnostics). The file is truncated to zero bytes; its 0o600 permissions are
// preserved. Mirrors ClearNetworkAudit. No-op pre-vault.
func (a *App) ClearAuditLog() error {
	if a.vaultPath == "" {
		return nil
	}
	auditTrailMu.Lock()
	defer auditTrailMu.Unlock()
	return os.WriteFile(auditLogPath(a.vaultPath), []byte{}, 0o600)
}
