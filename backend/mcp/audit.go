package mcp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// AuditEntry is one MCP tool invocation. Args are redacted (keys + shapes only;
// never note bodies or full text).
type AuditEntry struct {
	TS       string         `json:"ts"`
	Client   string         `json:"client,omitempty"`
	Tool     string         `json:"tool"`
	Vault    string         `json:"vault"`   // path hash, not full path
	Outcome  string         `json:"outcome"` // "ok" | "error" | "denied"
	Error    string         `json:"error,omitempty"`
	ArgsMeta map[string]any `json:"args,omitempty"`
}

// Auditor writes MCP audit lines. Production uses fileAuditor under
// <vault>/.system/logs/mcp-audit.jsonl; tests use MemoryAuditor.
type Auditor interface {
	Record(e AuditEntry)
	Close()
}

// MemoryAuditor stores entries in memory (tests).
type MemoryAuditor struct {
	mu      sync.Mutex
	Entries []AuditEntry
}

func (m *MemoryAuditor) Record(e AuditEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Entries = append(m.Entries, e)
}

func (m *MemoryAuditor) Close() {}

// fileAuditor appends JSON lines under the vault log dir.
type fileAuditor struct {
	mu   sync.Mutex
	path string
	f    *os.File
}

func newFileAuditor(vaultPath string) (*fileAuditor, error) {
	dir := filepath.Join(vaultPath, ".system", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "mcp-audit.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &fileAuditor{path: path, f: f}, nil
}

func (a *fileAuditor) Record(e AuditEntry) {
	if e.TS == "" {
		e.TS = time.Now().UTC().Format(time.RFC3339Nano)
	}
	b, err := json.Marshal(e)
	if err != nil {
		log.Printf("mcp audit: marshal: %v", err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f == nil {
		return
	}
	if _, err := a.f.Write(append(b, '\n')); err != nil {
		log.Printf("mcp audit: write: %v", err)
	}
}

func (a *fileAuditor) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.f != nil {
		_ = a.f.Close()
		a.f = nil
	}
}

// VaultPathHash returns a short stable hash of the vault path for audit rows
// (never log the full path in shared logs if avoidable).
func VaultPathHash(vaultPath string) string {
	if vaultPath == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(vaultPath))
	return hex.EncodeToString(sum[:8])
}

// RedactArgs builds a metadata map from tool args: string lengths, counts,
// and non-content identifiers (notebook/section/page/ids). Never includes
// free-text body fields.
func RedactArgs(args map[string]any) map[string]any {
	if len(args) == 0 {
		return nil
	}
	out := make(map[string]any, len(args))
	for k, v := range args {
		switch k {
		case "text", "content", "markdown", "body", "query":
			if s, ok := v.(string); ok {
				out[k+"_len"] = len([]rune(s))
			} else {
				out[k+"_present"] = true
			}
		case "blocks":
			switch b := v.(type) {
			case []any:
				out["blocks_count"] = len(b)
			default:
				out["blocks_present"] = true
			}
		case "block_ids":
			// Non-secret UUIDs for forensic correlation with read_page.
			switch ids := v.(type) {
			case []string:
				out["block_ids"] = ids
				out["block_ids_count"] = len(ids)
			case []any:
				clean := make([]string, 0, len(ids))
				for _, id := range ids {
					if s, ok := id.(string); ok && s != "" {
						clean = append(clean, s)
					}
				}
				out["block_ids"] = clean
				out["block_ids_count"] = len(clean)
			default:
				out["block_ids_present"] = true
			}
		default:
			// Safe identifiers and numbers only.
			switch v.(type) {
			case string, float64, int, int64, bool, nil:
				out[k] = v
			default:
				out[k+"_type"] = "complex"
			}
		}
	}
	return out
}
