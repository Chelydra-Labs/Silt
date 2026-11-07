package monitor

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"silt/backend/core"
	"silt/backend/db"
	"silt/backend/parser"
)

// sentinelHandlerErr is the failure future tests inject via the atomic
// reindex handler. A private sentinel makes errors.Is identify the handler
// as the source, distinct from any real DB error.
var sentinelHandlerErr = errors.New("atomic reindex handler forced failure")

// newWatcherTestHarness builds a minimal DirectoryWatcher + DB against a
// temp vault so the atomic-reindex-handler tests don't duplicate the
// boilerplate across cases.
func newWatcherTestHarness(t *testing.T) (*DirectoryWatcher, *db.DatabaseManager, string) {
	t.Helper()
	vaultPath := t.TempDir()
	dm, err := db.NewDatabaseManager("")
	if err != nil {
		t.Fatalf("NewDatabaseManager: %v", err)
	}
	t.Cleanup(func() { _ = dm.Close() })
	coord := core.NewExecutionCoordinator(dm.SQLDB())
	tracker := NewWriteTracker()
	t.Cleanup(tracker.Stop)
	dw, err := NewDirectoryWatcher(vaultPath, dm, tracker, coord, 4)
	if err != nil {
		t.Fatalf("NewDirectoryWatcher: %v", err)
	}
	return dw, dm, vaultPath
}

// writeTypedMarkdown stages a typed page on disk with a stable block id so
// reindexFile has something deterministic to parse and index.
func writeTypedMarkdown(t *testing.T, vaultPath, rel string, blockID string) string {
	t.Helper()
	filePath := filepath.Join(vaultPath, rel)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "# Page heading <!-- id: " + blockID + " -->\n"
	if err := os.WriteFile(filePath, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return filePath
}

// TestReindexFile_InvokesAtomicHandler proves SetAtomicReindexHandler
// installs the callback the watcher delegates the index step to: when set,
// reindexFile calls it with the parsed blocks/meta and gates
// MarkFileIndexed on a nil return.
func TestReindexFile_InvokesAtomicHandler(t *testing.T) {
	dw, dm, vaultPath := newWatcherTestHarness(t)
	filePath := writeTypedMarkdown(t, vaultPath, "Work/Notes/Page.md",
		"aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	var called bool
	var sawSource, sawNotebook, sawPage string
	var sawBlocks int
	dw.SetAtomicReindexHandler(func(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata) error {
		called = true
		sawSource, sawNotebook, sawPage = source, notebook, page
		sawBlocks = len(blocks)
		// Production handler would publish atomically; for this test we
		// perform the atomic publish directly so MarkFileIndexed gating is
		// exercised against real DB state.
		var err error
		dw.coordinator.WithDBWrite(func() {
			err = dm.IndexFileWithProjection(source, notebook, section, page, blocks, meta.Tags, "", nil, db.PageCoreFields{Type: meta.Type, Date: meta.Date, Aliases: meta.Aliases, Created: meta.Created}, meta.Warnings...)
		})
		return err
	})
	defer dw.SetAtomicReindexHandler(nil)

	dw.reindexFile(filePath)

	if !called {
		t.Fatal("atomic reindex handler was not invoked")
	}
	if sawSource != "vault" || sawNotebook != "Work" || sawPage != "Page" {
		t.Errorf("handler coords = %q/%q/%q, want vault/Work/Page", sawSource, sawNotebook, sawPage)
	}
	if sawBlocks == 0 {
		t.Error("handler received zero blocks — parser did not produce any")
	}

	// Index step ran: one block visible.
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&n); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if n != 1 {
		t.Errorf("blocks = %d, want 1 (handler published)", n)
	}
	// MarkFileIndexed ran on success: the files table carries the path.
	var known int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM files WHERE path = ?", filePath).Scan(&known); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if known != 1 {
		t.Errorf("files row = %d after successful handler, want 1 (MarkFileIndexed skipped)", known)
	}
}

// TestReindexFile_HandlerErrorSkipsMarkFileIndexed pins the contract that a
// failing handler aborts the index step entirely: no blocks are written
// (handler is the sole publisher), and MarkFileIndexed is skipped so a
// future startup scan re-attempts the file rather than treating it as
// up-to-date.
func TestReindexFile_HandlerErrorSkipsMarkFileIndexed(t *testing.T) {
	dw, dm, vaultPath := newWatcherTestHarness(t)
	filePath := writeTypedMarkdown(t, vaultPath, "Work/Notes/Page.md",
		"bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	dw.SetAtomicReindexHandler(func(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata) error {
		return sentinelHandlerErr
	})
	defer dw.SetAtomicReindexHandler(nil)

	dw.reindexFile(filePath)

	// No blocks written (handler is the sole publisher; it did not run).
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&n); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if n != 0 {
		t.Errorf("blocks = %d after failing handler, want 0 (handler is the sole publisher)", n)
	}
	// MarkFileIndexed skipped: the files table must NOT carry the path,
	// so the next startup scan re-attempts the file rather than treating
	// it as up-to-date.
	var known int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM files WHERE path = ?", filePath).Scan(&known); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if known != 0 {
		t.Errorf("files row = %d after failing handler, want 0 (MarkFileIndexed must skip)", known)
	}
}

// TestReindexFile_DefaultHandlerIsAtomic proves that even when no App-side
// handler is installed, reindexFile still goes through IndexFileWithProjection
// (atomic block+projection clear) rather than the legacy block-only
// IndexFileBlocks path. This is the property that lets the watcher have NO
// non-atomic route regardless of how it's wired.
func TestReindexFile_DefaultHandlerIsAtomic(t *testing.T) {
	dw, dm, vaultPath := newWatcherTestHarness(t)
	filePath := writeTypedMarkdown(t, vaultPath, "Work/Notes/Page.md",
		"cccccccc-cccc-cccc-cccc-cccccccccccc")

	// Seed a stale projection row to prove the default atomic path clears
	// it in the SAME transaction as the block insert. (Production would
	// compute the schema-aware payload; the default's empty payload
	// clears, which is the conservative correctness behavior.)
	if err := dm.IndexPageProjection("vault", "Work", "Notes", "Page", "task",
		[]db.ProjectedProperty{{Property: "owner", ValueText: "Alice", ValueSort: "Alice", ValueType: "text"}}); err != nil {
		t.Fatalf("seed projection: %v", err)
	}

	// No SetAtomicReindexHandler — reindexFile must fall back to its
	// defaultAtomicReindex, which routes through IndexFileWithProjection.
	dw.reindexFile(filePath)

	// Block inserted.
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&n); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if n != 1 {
		t.Errorf("blocks = %d, want 1 (default handler inserted)", n)
	}
	// Stale projection cleared atomically (default payload is typeID="").
	row, err := dm.GetPageProjection("vault", "Work", "Notes", "Page")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row != nil {
		t.Errorf("default handler must clear stale projection (atomic clear with empty payload); got %+v", row)
	}
}

// TestReindexFile_DefaultHandlerPreservesNotifyAndMtime is a regression
// guard that the default-handler path still fires notifyPageChanged and
// records the file mtime, so swapping the index step to the atomic default
// did not regress the watcher's surrounding contracts.
func TestReindexFile_DefaultHandlerPreservesNotifyAndMtime(t *testing.T) {
	dw, dm, vaultPath := newWatcherTestHarness(t)
	filePath := writeTypedMarkdown(t, vaultPath, "Work/Notes/Page.md",
		"dddddddd-dddd-dddd-dddd-dddddddddddd")

	notifyFired := make(chan struct{}, 1)
	dw.SetPageChangedHandler(func(notebook, section, page string) {
		select {
		case notifyFired <- struct{}{}:
		default:
		}
	})

	dw.reindexFile(filePath)

	select {
	case <-notifyFired:
		// good: notify fired after the atomic publish + MarkFileIndexed
	case <-time.After(notifyTimeout):
		t.Fatal("notifyPageChanged did not fire after reindexFile")
	}

	// Files table carries the path (MarkFileIndexed ran on the default path).
	var known int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM files WHERE path = ?", filePath).Scan(&known); err != nil {
		t.Fatalf("count files: %v", err)
	}
	if known != 1 {
		t.Errorf("files row = %d, want 1 (MarkFileIndexed must run on default path)", known)
	}
}

// notifyTimeout is the bounded wait the notify-contract test waits on
// before declaring notifyPageChanged did not fire.
const notifyTimeout = 2 * time.Second

// TestReindexFile_HanderComputeProjection proves the handler is the single
// place where the projection payload is computed: the handler receives meta
// with the frontmatter intact and can derive typeID + props. This wires the
// monitor→App boundary without the watcher needing any schema knowledge.
func TestReindexFile_HandlerComputeProjection(t *testing.T) {
	dw, dm, vaultPath := newWatcherTestHarness(t)
	// Markdown with a `type:` line + a custom frontmatter field the
	// handler will project.
	rel := "Work/Notes/Typed.md"
	filePath := filepath.Join(vaultPath, rel)
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	content := "---\n" +
		"notebook: \"Work\"\n" +
		"section: \"Notes\"\n" +
		"page: \"Typed\"\n" +
		"type: \"task\"\n" +
		"status: \"done\"\n" +
		"---\n" +
		"# Typed <!-- id: cccccccc-cccc-cccc-cccc-cccccccccccc -->\n"
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Handler simulates the App-side computation: derive typeID + props
	// from meta.Type + meta.Frontmatter, then publish atomically.
	dw.SetAtomicReindexHandler(func(source, notebook, section, page string, blocks []parser.ParsedBlock, meta parser.FileMetadata) error {
		typeID := ""
		var props []db.ProjectedProperty
		if meta.Type == "task" {
			typeID = "task"
			if v, ok := meta.Frontmatter["status"]; ok {
				if s, ok := v.(string); ok {
					props = append(props, db.ProjectedProperty{
						Property: "status", ValueText: s, ValueSort: s, ValueType: "text",
					})
				}
			}
		}
		var err error
		dw.coordinator.WithDBWrite(func() {
			err = dm.IndexFileWithProjection(source, notebook, section, page, blocks, meta.Tags, typeID, props, db.PageCoreFields{Type: meta.Type, Date: meta.Date, Aliases: meta.Aliases, Created: meta.Created}, meta.Warnings...)
		})
		return err
	})
	defer dw.SetAtomicReindexHandler(nil)

	dw.reindexFile(filePath)

	// Atomic publish: blocks AND projection landed together.
	var n int
	if err := dm.SQLDB().QueryRow("SELECT COUNT(*) FROM blocks").Scan(&n); err != nil {
		t.Fatalf("count blocks: %v", err)
	}
	if n != 1 {
		t.Errorf("blocks = %d, want 1", n)
	}
	row, err := dm.GetPageProjection("vault", "Work", "Notes", "Typed")
	if err != nil {
		t.Fatalf("GetPageProjection: %v", err)
	}
	if row == nil || row.TypeName != "task" {
		t.Fatalf("projection missing or wrong type: %+v", row)
	}
	if len(row.Properties) != 1 || row.Properties[0].Property != "status" || row.Properties[0].ValueText != "done" {
		t.Errorf("projection property = %+v, want [status=done] (handler-computed)", row.Properties)
	}
}
