package main

// Generic audit-log engine shared by the network audit (network.log) and the
// AI audit (ai.log). Both logs are thin instantiations of auditLog[T]; the
// per-entry construction (auditNetwork / auditAI / auditAIEvent) and the
// AI-specific redaction machinery live in plugin_audit.go.
//
// The background-writer drain logic (clear) and the writer goroutine (run /
// process) are copied verbatim from the pre-refactor AI path — the hardened
// one that closes the #451 (clear-deadlock) and #452 (cross-vault clear)
// races. Parameterizing by entry type T does not change the drain semantics;
// the existing drain tests (plugin_audit_ai_test.go,
// app_lifecycle_drain_test.go) are the golden master and must stay green.

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// maxPluginAuditLogBytes bounds a single per-plugin on-disk audit log
// (network.log / ai.log) so it cannot grow unbounded across a long session.
// When exceeded, the log is truncated to the most recent
// maxPluginAuditLogLines.
const (
	maxPluginAuditLogBytes = 1 * 1024 * 1024 // 1 MB
	maxPluginAuditLogLines = 200             // keep-lines on truncation
)

// auditInMemCap bounds the in-memory audit logs (mirrors the original 500-entry
// cap both logs used).
const auditInMemCap = 500

// auditEntry is the constraint the generic engine needs from a log row: a
// timestamp (for seed-sorting + the parse-line validity check) and a plugin id
// (the per-plugin on-disk subdir + diagnostics). Both NetworkAuditEntry and
// AIAuditEntry satisfy it via the accessors defined alongside their structs in
// plugin_audit.go.
type auditEntry interface {
	auditAt() string
	auditPlugin() string
}

// auditOp is one operation queued to the background writer.
type auditOp[T auditEntry] struct {
	entry *T // non-nil = append to on-disk log
	clear bool
	done  chan struct{} // optional: closed when this op is fully processed
}

// auditWriter is the background writer's mutable state. It is non-nil while the
// writer goroutine is running. The fields (ch/stop/done/vaultPath) keep their
// historical names so the #452 cross-vault drain test can poke them directly.
type auditWriter[T auditEntry] struct {
	ch        chan *auditOp[T]
	stop      chan struct{}
	done      chan struct{} // closed when the goroutine has exited
	vaultPath string
	log       *auditLog[T]
}

// auditLog is the shared in-memory + on-disk + background-writer audit engine.
type auditLog[T auditEntry] struct {
	name     string // "network" / "ai" — diagnostics
	filename string // "network.log" / "ai.log"
	mu       sync.Mutex
	entries  []T
	writerMu sync.Mutex
	writer   *auditWriter[T]
}

func newAuditLog[T auditEntry](name, filename string) *auditLog[T] {
	return &auditLog[T]{name: name, filename: filename}
}

// currentWriter returns the active writer state, or nil if the writer is not
// running. Thread-safe; callers may use the returned pointer without holding
// writerMu (the state struct is never mutated after creation; only the
// instance's writer pointer is swapped).
func (l *auditLog[T]) currentWriter() *auditWriter[T] {
	l.writerMu.Lock()
	defer l.writerMu.Unlock()
	return l.writer
}

// startWriter launches the background audit-log writer goroutine for the given
// vault. Idempotent — a second call while the writer is running is a no-op.
func (l *auditLog[T]) startWriter(vaultPath string) {
	l.writerMu.Lock()
	defer l.writerMu.Unlock()
	if l.writer != nil {
		return
	}
	w := &auditWriter[T]{
		ch:        make(chan *auditOp[T], 256),
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
		vaultPath: vaultPath,
		log:       l,
	}
	l.writer = w
	go w.run()
}

// stopWriter signals the writer to drain remaining ops and exit, then blocks
// until the goroutine is done. Idempotent. Guarantees no queued entry is lost
// on vault close.
func (l *auditLog[T]) stopWriter() {
	l.writerMu.Lock()
	w := l.writer
	if w == nil {
		l.writerMu.Unlock()
		return
	}
	l.writer = nil
	l.writerMu.Unlock()
	close(w.stop)
	<-w.done
}

// run is the writer goroutine body. It processes ops in FIFO order until stop
// is closed, then drains every remaining queued op before exiting so no entry
// is lost on shutdown.
func (w *auditWriter[T]) run() {
	defer close(w.done)
	for {
		select {
		case op := <-w.ch:
			w.process(op)
		case <-w.stop:
			// Drain every queued op before exiting so no entry is lost.
			for {
				select {
				case op := <-w.ch:
					w.process(op)
				default:
					return
				}
			}
		}
	}
}

// process handles one op. Entry appends to the per-plugin on-disk log; clear
// empties every on-disk log. If done is non-nil it is closed after the op
// completes so the caller (clear) can synchronize.
func (w *auditWriter[T]) process(op *auditOp[T]) {
	if op.entry != nil {
		w.log.appendLine(w.vaultPath, op.entry)
	}
	if op.clear {
		w.log.clearFiles(w.vaultPath)
	}
	if op.done != nil {
		close(op.done)
	}
}

// append writes one entry to the in-memory log (capped) and dispatches the
// on-disk write to the background writer when running, else inline. Mirrors the
// pre-refactor appendAIAuditEntry path (the hardened one that snapshots
// vaultPath under vaultMu so the inline fallback never reads a.vaultPath
// unsynchronized).
func (l *auditLog[T]) append(a *App, entry T) {
	a.vaultMu.RLock()
	vaultPathSnapshot := a.vaultPath
	a.vaultMu.RUnlock()
	l.mu.Lock()
	l.entries = append(l.entries, entry)
	if len(l.entries) > auditInMemCap {
		l.entries = l.entries[len(l.entries)-auditInMemCap:]
	}
	w := l.currentWriter()
	if w != nil {
		select {
		case w.ch <- &auditOp[T]{entry: &entry}:
		default:
			log.Printf("%s audit: writer queue full; dropping on-disk write for plugin %q", l.name, entry.auditPlugin())
		}
	} else {
		l.appendLine(vaultPathSnapshot, &entry)
	}
	l.mu.Unlock()
}

// clear empties the in-memory audit log AND truncates the on-disk per-plugin log
// files so a clear is durable across restarts. When the background writer is
// running, the on-disk truncation is enqueued and processed in FIFO order with
// concurrent appends so a fetch that fires after the clear click cannot
// interleave a line into a file we just emptied. When the writer is not running
// (tests, pre-initialize), the truncation runs inline.
//
// VERBATIM drain logic from the pre-refactor ClearAIAudit — the hardened path
// that closes #451 (clear-deadlock when the writer exits mid-call) and #452
// (cross-vault clear truncating the wrong vault). Parameterizing by entry type
// does not change the semantics.
func (l *auditLog[T]) clear(a *App) error {
	// Snapshot vaultPath under vaultMu BEFORE acquiring the audit mu so the
	// w==nil inline path never reads a.vaultPath unsynchronized (a concurrent
	// SwitchVault/ImportVault may flip it). The writer branches use w.vaultPath
	// (the vault the writer was created for) instead — see the <-w.stop branch.
	a.vaultMu.RLock()
	vaultPathSnapshot := a.vaultPath
	a.vaultMu.RUnlock()
	l.mu.Lock()
	l.entries = nil
	w := l.currentWriter()
	if w == nil {
		// Writer not running: truncate inline.
		l.clearFiles(vaultPathSnapshot)
		l.mu.Unlock()
		return nil
	}
	// The clear op MUST be enqueued while holding the audit mu so it is ordered
	// relative to concurrent appends (FIFO clear-vs-append invariant). The send
	// is NON-BLOCKING to avoid a latent deadlock: a blocking send can succeed on
	// the buffered channel even after the writer goroutine has exited
	// (stopWriter nils the instance pointer, closes w.stop, the writer drains
	// and exits), leaving <-op.done blocked forever (#451).
	op := &auditOp[T]{clear: true, done: make(chan struct{})}
	select {
	case w.ch <- op:
		l.mu.Unlock()
		// Wait for the writer to process the clear, but ALSO watch w.stop. If
		// the writer exits before processing the op (or the select picks w.stop
		// when both are ready), we must still guarantee the truncation runs
		// against the WRITER's vault — never a.vaultPath, which a concurrent
		// SwitchVault may have moved to a different vault (#452 cross-vault
		// data-loss regression). Waiting on w.done ensures the writer's drain
		// completes first (our op is in the buffer and the drain processes every
		// buffered op before returning).
		select {
		case <-op.done:
		case <-w.stop:
			<-w.done
			l.clearFiles(w.vaultPath)
		}
	default:
		// Queue full. Truncate the WRITER's vault inline — not a.vaultPath — so
		// a concurrent switch can't redirect the truncation to the new vault.
		// KNOWN FIFO TRADE-OFF: the writer appends queued entries to disk
		// WITHOUT holding the audit mu, so an entry op already in the queue when
		// this truncation runs can be re-appended by the writer AFTER the
		// truncation, resurrecting a pre-clear line in the on-disk log. The
		// in-memory log (cleared above under the lock) is always correct; only
		// the on-disk diagnostic file can carry a stale tail. Accepted because
		// the path is near-unreachable and the data is diagnostic.
		l.clearFiles(w.vaultPath)
		l.mu.Unlock()
	}
	return nil
}

// snapshot returns a copy of the in-memory log.
func (l *auditLog[T]) snapshot() []T {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]T, len(l.entries))
	copy(out, l.entries)
	return out
}

// reset nils the in-memory log under the lock (test/teardown helper).
func (l *auditLog[T]) reset() {
	l.mu.Lock()
	l.entries = nil
	l.mu.Unlock()
}

// appendLine writes one entry to the per-plugin on-disk log file. Best-effort —
// errors are logged, never surfaced (the audit log is diagnostic). NOT
// goroutine-safe — callers must serialize (the background writer in production;
// the audit mu in the inline fallback).
//
// The on-disk format is a single-line JSON object per entry (#254).
func (l *auditLog[T]) appendLine(vaultPath string, entry *T) {
	if vaultPath == "" {
		return
	}
	logPath := filepath.Join(vaultPath, ".system", "plugins", (*entry).auditPlugin(), l.filename)
	data, err := json.Marshal(entry)
	if err != nil {
		log.Printf("%s.appendLine: json.Marshal failed: %v", l.name, err)
		return
	}
	_ = os.MkdirAll(filepath.Dir(logPath), 0o700)
	if info, err := os.Stat(logPath); err == nil && info.Size() > maxPluginAuditLogBytes {
		truncateAuditLog(logPath, maxPluginAuditLogLines)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err == nil {
		_, _ = f.Write(append(data, '\n'))
		_ = f.Close()
	}
}

// clearFiles empties every per-plugin on-disk log of this kind under the vault's
// .system/plugins/ tree. Best-effort (errors silently ignored).
func (l *auditLog[T]) clearFiles(vaultPath string) {
	if vaultPath == "" {
		return
	}
	pluginsDir := filepath.Join(vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(pluginsDir, e.Name(), l.filename)
		if _, err := os.Stat(logPath); err == nil {
			_ = os.WriteFile(logPath, []byte{}, 0o600)
		}
	}
}

// parseLine parses one JSON-format line from an on-disk log into an entry.
// Returns ok=false on any parse failure (best-effort).
func (l *auditLog[T]) parseLine(line string) (T, bool) {
	var entry T
	if err := json.Unmarshal([]byte(line), &entry); err == nil && entry.auditAt() != "" {
		return entry, true
	}
	var zero T
	return zero, false
}

// seedFromDisk reads every on-disk log of this kind under the vault's
// .system/plugins/ tree and seeds the in-memory log so entries survive a
// restart. Called once during initializeVaultServices. The in-memory log is
// capped at auditInMemCap entries (most recent).
func (l *auditLog[T]) seedFromDisk(vaultPath string) {
	if vaultPath == "" {
		return
	}
	pluginsDir := filepath.Join(vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return
	}
	var seeded []T
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		logPath := filepath.Join(pluginsDir, e.Name(), l.filename)
		data, err := os.ReadFile(logPath)
		if err != nil || len(data) == 0 {
			continue
		}
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		for _, line := range lines {
			if entry, ok := l.parseLine(line); ok {
				seeded = append(seeded, entry)
			}
		}
	}
	// Sort by timestamp (oldest first) so we can trim to the most recent.
	sort.Slice(seeded, func(i, j int) bool {
		return seeded[i].auditAt() < seeded[j].auditAt()
	})
	if len(seeded) > auditInMemCap {
		seeded = seeded[len(seeded)-auditInMemCap:]
	}
	l.mu.Lock()
	// Only seed if the in-memory log is empty (don't overwrite entries that may
	// have been added between vault open and this call).
	if len(l.entries) == 0 {
		l.entries = seeded
	}
	l.mu.Unlock()
}

// truncateAuditLog reads the log file, keeps the last keepLines lines, and
// rewrites it. Best-effort — errors are silently ignored (the audit log is not a
// security boundary, just a diagnostic aid). Shared by both audit kinds.
func truncateAuditLog(path string, keepLines int) {
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
