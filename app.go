package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"notes-sharp/backend/core"
	"notes-sharp/backend/db"
	"notes-sharp/backend/monitor"
	"notes-sharp/backend/parser"
	"notes-sharp/backend/vault"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const (
	maxTimelineLimit    = 200
	defaultTimelineLimit = 30
)

var updateTaskRegex = regexp.MustCompile(`^([\s]*)-\s\[[ x/]\]\s(?:TODO|DOING|DONE)\sTASK(.*)$`)

var updateLineIDRegex = regexp.MustCompile(`<!-- id: ([a-f0-9\-]{36}) -->`)

type App struct {
	ctx          context.Context
	db           *db.DatabaseManager
	coordinator  *core.ExecutionCoordinator
	watcher      *monitor.DirectoryWatcher
	tracker      *monitor.WriteTracker
	vaultPath    string
	spacesPerTab int
	wg           sync.WaitGroup
}

func NewApp() *App {
	return &App{
		spacesPerTab: 4,
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	settings, err := vault.LoadSettings()
	if err != nil {
		// The settings file exists on disk but is unreadable or
		// malformed. Don't silently fall through to "no vault" — the
		// user has a vault setup, something is just broken.
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "vault:init-error",
				fmt.Sprintf("failed to load settings.json: %v", err))
		}
		return
	}
	if settings.VaultPath != "" {
		if _, statErr := os.Stat(settings.VaultPath); statErr == nil {
			if initErr := a.initializeVaultServices(settings.VaultPath); initErr != nil {
				if a.ctx != nil {
					runtime.EventsEmit(a.ctx, "vault:init-error", initErr.Error())
				}
			}
		}
	}
}

func (a *App) shutdown(ctx context.Context) {
	// Wait for any in-flight Wails-bound calls (UpdateBlockState,
	// QueryTasks, FetchSectionTimeline) to complete before tearing
	// down the DB, tracker, and watcher. Without this a fast window
	// close could race an in-progress file write.
	a.wg.Wait()

	if a.watcher != nil {
		_ = a.watcher.Close()
	}
	if a.tracker != nil {
		a.tracker.Stop()
	}
	if a.db != nil {
		_ = a.db.Close()
	}
}

func (a *App) initializeVaultServices(vaultPath string) error {
	dbMgr, err := db.NewDatabaseManager()
	if err != nil {
		return fmt.Errorf("failed to start database: %w", err)
	}

	coord := core.NewExecutionCoordinator(dbMgr.SQLDB())
	tracker := monitor.NewWriteTracker()

	results, err := parser.ScanWorkspace(vaultPath, a.spacesPerTab)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to scan workspace: %w", err)
	}
	if len(results) > 0 {
		var allWarnings []string
		for _, res := range results {
			if len(res.Warnings) > 0 {
				allWarnings = append(allWarnings, res.Warnings...)
			}
		}

		_, skipped, err := dbMgr.IndexScanResults(results)
		if err != nil {
			_ = dbMgr.Close()
			return fmt.Errorf("failed to index scan results: %w", err)
		}
		if len(skipped) > 0 && a.ctx != nil {
			runtime.EventsEmit(a.ctx, "vault:init-warnings", skipped)
		}
		if len(allWarnings) > 0 && a.ctx != nil {
			runtime.EventsEmit(a.ctx, "vault:init-warnings", allWarnings)
		}
	}

	watcher, err := monitor.NewDirectoryWatcher(vaultPath, dbMgr, tracker, coord, a.spacesPerTab)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to start watcher: %w", err)
	}
	if err := watcher.Start(); err != nil {
		_ = watcher.Close()
		_ = dbMgr.Close()
		return fmt.Errorf("failed to execute watcher start: %w", err)
	}

	a.db = dbMgr
	a.coordinator = coord
	a.tracker = tracker
	a.watcher = watcher
	a.vaultPath = vaultPath

	// Report any paths the watcher could not subscribe to (fsnotify
	// limits, permissions, etc.) so the UI can inform the user.
	if failed := watcher.FailedPaths(); len(failed) > 0 && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "vault:watch-coverage", failed)
	}

	return nil
}

// IsVaultInitialized returns whether a workspace vault has been configured and loaded.
func (a *App) IsVaultInitialized() bool {
	return a.vaultPath != "" && a.db != nil
}

// InitializeVault prompts the user for a folder, sets it up, and loads the services.
func (a *App) InitializeVault() (bool, error) {
	selectedPath, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Select notes# Vault Directory",
	})
	if err != nil {
		return false, fmt.Errorf("failed to select vault folder: %w", err)
	}

	if selectedPath == "" {
		return false, nil // Cancelled
	}

	if err := vault.ScaffoldVault(selectedPath); err != nil {
		return false, fmt.Errorf("failed to scaffold vault: %w", err)
	}

	settings := &vault.AppSettings{
		VaultPath: selectedPath,
	}
	if err := vault.SaveSettings(settings); err != nil {
		return false, fmt.Errorf("failed to save settings: %w", err)
	}

	if err := a.initializeVaultServices(selectedPath); err != nil {
		return false, fmt.Errorf("failed to boot vault services: %w", err)
	}

	return true, nil
}

// FetchSectionTimeline returns blocks grouped by days for scroll virtualization.
func (a *App) FetchSectionTimeline(notebook, section string, offset int, limit int) ([]parser.DayGroup, error) {
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	// Clamp server-side so a frontend bug sending limit=1_000_000 cannot
	// materialize an arbitrarily large in-memory slice.
	if limit <= 0 || limit > maxTimelineLimit {
		limit = maxTimelineLimit
	}
	if offset < 0 {
		offset = 0
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res []parser.DayGroup
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.FetchTimelineDays(notebook, section, limit, offset)
	})

	return res, err
}

// UpdateBlockState changes task status and updates the file and cache.
//
// To avoid TOCTOU races between the DB read and the file write, we look up the
// block's UUID, file metadata, and the lock by file path, then re-locate the
// target line inside the file write lock by scanning for the UUID comment. The
// UUID is the source of truth for the target line, not the cached line number.
func (a *App) UpdateBlockState(blockID string, newState string) error {
	// Guard against a meaningless no-op that the frontend might interpret
	// as an error. The only valid task status values are TODO, DOING, DONE.
	switch newState {
	case "TODO", "DOING", "DONE":
	default:
		return fmt.Errorf("invalid target status: %s (valid: TODO, DOING, DONE)", newState)
	}

	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var notebook, section, fileDate, blockType string
	err := a.coordinator.WithDBReadResult(func() error {
		row := a.db.SQLDB().QueryRow("SELECT notebook, section, file_date, type FROM blocks WHERE id = ?", blockID)
		return row.Scan(&notebook, &section, &fileDate, &blockType)
	})
	if err != nil {
		return fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}

	if blockType != string(parser.BlockTask) {
		return fmt.Errorf("block %s is not a task", blockID)
	}

	// Defense-in-depth against path traversal: notebook/section originate
	// from user-editable YAML frontmatter and date is a filename.
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safeFileDate := sanitizePathSegment(fileDate)
	if safeNotebook == "" || safeSection == "" || safeFileDate == "" {
		return fmt.Errorf("invalid file metadata for block %s: notebook=%q section=%q date=%q", blockID, notebook, section, fileDate)
	}
	filePath := filepath.Join(a.vaultPath, safeNotebook, safeSection, safeFileDate+".md")
	if !isPathWithinVault(filePath, a.vaultPath) {
		return fmt.Errorf("resolved file path %q escapes vault %q", filePath, a.vaultPath)
	}

	var writeErr error
	a.coordinator.LockFileWrite(filePath, func() {
		contentBytes, err := os.ReadFile(filePath)
		if err != nil {
			writeErr = err
			return
		}

		lines := strings.Split(string(contentBytes), "\n")
		lineIdx := findLineByBlockID(lines, blockID)
		if lineIdx < 0 {
			writeErr = fmt.Errorf("block %s not found in file %s", blockID, filePath)
			return
		}

		targetLine := lines[lineIdx]

		if !updateTaskRegex.MatchString(targetLine) {
			writeErr = fmt.Errorf("target line does not match task syntax")
			return
		}

		var newChar string
		var newKeyword string
		switch newState {
		case "TODO":
			newChar = " "
			newKeyword = "TODO"
		case "DOING":
			newChar = "/"
			newKeyword = "DOING"
		case "DONE":
			newChar = "x"
			newKeyword = "DONE"
		}

		newLine := updateTaskRegex.ReplaceAllString(targetLine, fmt.Sprintf("${1}- [%s] %s TASK${2}", newChar, newKeyword))
		lines[lineIdx] = newLine
		newContent := strings.Join(lines, "\n")

		a.tracker.RegisterWrite(filePath)

		if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
			writeErr = err
			return
		}

		// Re-parse with the sanitized metadata so the re-indexed row
		// uses the same cleaned values that went into the file path.
		blocks, meta, _, _, err := parser.ParseFileContent(newContent, safeNotebook, safeSection, safeFileDate, a.spacesPerTab)
		if err == nil {
			_ = a.db.IndexFileBlocks(meta.Notebook, meta.Section, meta.Date, blocks, meta.Tags, meta.Warnings...)
		}
	})

	return writeErr
}

// QueryTasks retrieves indexed items matching the active filters.
func (a *App) QueryTasks(filter parser.TaskQueryFilter) ([]parser.TaskResult, error) {
	if a.db == nil {
		return nil, fmt.Errorf("vault database not loaded")
	}

	a.wg.Add(1)
	defer a.wg.Done()

	var res []parser.TaskResult
	var err error
	a.coordinator.WithDBRead(func() {
		res, err = a.db.QueryTasksWithFilters(filter)
	})

	return res, err
}

// findLineByBlockID returns the 0-based index of the line in `lines` whose
// trailing `<!-- id: UUID -->` comment matches blockID, or -1 if no such line
// exists.
func findLineByBlockID(lines []string, blockID string) int {
	for i, line := range lines {
		matches := updateLineIDRegex.FindStringSubmatch(line)
		if len(matches) >= 2 && matches[1] == blockID {
			return i
		}
	}
	return -1
}

// sanitizePathSegment strips path-traversal characters from a single path
// component: directory separators, NUL, and `..` sequences. The intent is to
// safely fold untrusted frontmatter strings into file paths.
func sanitizePathSegment(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return -1
		}
		return r
	}, s)
	for strings.Contains(cleaned, "..") {
		cleaned = strings.ReplaceAll(cleaned, "..", "")
	}
	return strings.TrimSpace(cleaned)
}

// isPathWithinVault reports whether target is the same as or a descendant of
// vaultRoot. Both paths are cleaned and made absolute before comparison so
// that `..` segments in the joined path are resolved before the check.
func isPathWithinVault(target, vaultRoot string) bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(vaultRoot)
	if err != nil {
		return false
	}
	absTarget = filepath.Clean(absTarget)
	absRoot = filepath.Clean(absRoot)
	if absTarget == absRoot {
		return true
	}
	prefix := absRoot + string(os.PathSeparator)
	return strings.HasPrefix(absTarget, prefix)
}
