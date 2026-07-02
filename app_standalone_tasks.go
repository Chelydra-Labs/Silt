package main

import (
	"fmt"
	"os"
	"path/filepath"
	"silt/backend/config"
	"silt/backend/parser"
	"silt/backend/plugins"
	"strconv"
	"strings"
	"time"
)

// Standalone-tasks storage (#368).
//
// Standalone tasks (created from calendar/kanban quick-add without an
// associated note) live in a single dedicated non-note markdown file at
// <vault>/.silt/tasks.md. This preserves the markdown-source-of-truth
// invariant (ARCHITECTURE §0): every task is a GFM checkbox in a .md file,
// the tasks SQLite table stays a re-derivable cache, and there is no new
// SQL table or nullable block_id.
//
// The synthetic notebook name ".silt" is dot-prefixed, so ListNavigation
// (which skips dot-prefixed notebook names) auto-excludes the file from the
// page browser. WalkMarkdown also skips dot-directories, so a targeted scan
// (ScanStandaloneTasks) reads just this one well-known file and feeds it
// through the normal parse→index pipeline. The path→metadata derivation in
// parseSingleFile maps ".silt/tasks.md" → notebook=".silt", section="",
// page="tasks" — no special-casing in the parser.

const (
	// standaloneTasksNotebook is the synthetic notebook name under which
	// standalone task blocks are indexed. Dot-prefixed so it is hidden from
	// ListNavigation and WalkMarkdown's general dot-dir skip still applies
	// to any other file a user might drop in .silt/.
	standaloneTasksNotebook = ".silt"
	// standaloneTasksSection is always empty: the tasks file lives directly
	// under the .silt notebook folder.
	standaloneTasksSection = ""
	// standaloneTasksPage is the page (file basename) holding every
	// standalone task block.
	standaloneTasksPage = "tasks"
)

// standaloneTasksFilePath returns <vault>/.silt/tasks.md.
func (a *App) standaloneTasksFilePath() string {
	return filepath.Join(a.vaultPath, standaloneTasksNotebook, standaloneTasksPage+".md")
}

// ensureStandaloneTasksFile creates <vault>/.silt/ and tasks.md (with minimal
// frontmatter) if absent. Idempotent. Returns the file path. The caller is
// expected to hold vaultMu (read is fine; writes go through the atomic writer
// under LockFileWrite by the caller).
func (a *App) ensureStandaloneTasksFile() (string, error) {
	if a.vaultPath == "" {
		return "", fmt.Errorf("vault not loaded")
	}
	filePath := a.standaloneTasksFilePath()
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create standalone tasks dir: %w", err)
	}
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		today := time.Now().Format("2006-01-02")
		scaffold := fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n",
			strconv.Quote(standaloneTasksNotebook), strconv.Quote(standaloneTasksSection),
			strconv.Quote(standaloneTasksPage), strconv.Quote(today))
		a.tracker.RegisterWrite(filePath)
		if err := parser.WriteFileAtomic(filePath, []byte(scaffold)); err != nil {
			return "", fmt.Errorf("create standalone tasks file: %w", err)
		}
	} else if err != nil {
		return "", fmt.Errorf("stat standalone tasks file: %w", err)
	}
	return filePath, nil
}

// PluginCreateTask creates a standalone task (a GFM checkbox block) in
// <vault>/.silt/tasks.md, the dedicated non-note markdown file (#368). The
// task is queryable via QueryTasks / sqliteQuery immediately and survives a
// full re-index because it round-trips through the markdown-source-of-truth.
//
// title is required (single-line; newlines collapse to spaces). dueDate is
// optional ("YYYY-MM-DD" or "" for no due date). status defaults to TODO;
// accepted values are TODO / DOING / DONE.
//
// Gated by content-mutate (#156). Session-token verified (#236).
func (a *App) PluginCreateTask(pluginID, sessionToken, title, dueDate, status string) (string, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return "", err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return "", err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return "", fmt.Errorf("vault database not loaded")
	}

	cleanTitle := strings.ReplaceAll(strings.TrimSpace(title), "\n", " ")
	if cleanTitle == "" {
		return "", fmt.Errorf("title is required")
	}
	taskStatus := strings.ToUpper(strings.TrimSpace(status))
	if taskStatus == "" {
		taskStatus = "TODO"
	}
	switch taskStatus {
	case "TODO", "DOING", "DONE":
	default:
		return "", fmt.Errorf("invalid status %q (want TODO, DOING, or DONE)", status)
	}
	// dueDate is optional; "" means no due-date token. A non-empty value is
	// validated as YYYY-MM-DD so a malformed date can never land on disk.
	if dueDate != "" {
		if _, derr := time.Parse("2006-01-02", dueDate); derr != nil {
			return "", fmt.Errorf("invalid dueDate %q (want YYYY-MM-DD)", dueDate)
		}
	}

	a.wg.Add(1)
	defer a.wg.Done()

	filePath, err := a.ensureStandaloneTasksFile()
	if err != nil {
		return "", err
	}

	newID := newUUID()
	today := time.Now().Format("2006-01-02")
	newBlock := parser.ParsedBlock{
		ID:        newID,
		Type:      parser.BlockTask,
		Status:    taskStatus,
		CleanText: cleanTitle,
		DueDate:   dueDate,
		FileDate:  today,
	}

	source := config.LinkedNotebooksVaultSource // ".silt" is an in-vault synthetic notebook.

	var writeErr error
	a.coordinator.LockFileWrite(filePath, func() {
		// Read existing blocks (empty on first creation) and append.
		blocks, err2 := a.FetchPageBlocks(standaloneTasksNotebook, standaloneTasksSection, standaloneTasksPage)
		if err2 != nil {
			writeErr = fmt.Errorf("read standalone tasks: %w", err2)
			return
		}
		blocks = append(blocks, newBlock)
		writeErr = a.writePageFileLocked(filePath, source, standaloneTasksNotebook, standaloneTasksSection, standaloneTasksPage, blocks)
	})
	if writeErr != nil {
		return "", writeErr
	}

	a.emitBlockChanged(newID, standaloneTasksNotebook, standaloneTasksSection, standaloneTasksPage, today)
	return newID, nil
}
