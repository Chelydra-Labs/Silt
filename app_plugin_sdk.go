package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"silt/backend/config"
	"silt/backend/parser"
	"silt/backend/plugins"
	"strings"
	"time"
)

// maxPluginQueryRows caps the number of rows returned by PluginRawQuery so a
// plugin can't exhaust frontend memory with an unbounded SELECT. A `var`
// (not `const`) so tests can temporarily lower the cap without seeding
// thousands of rows.
var maxPluginQueryRows = 5000

// --- Plugin SDK bindings -------------------------------------------------

// openPluginRODB lazily opens a read-only handle to the same on-disk index
// (or the in-memory shared cache before a vault is open) for use by
// PluginRawQuery. The handle is capped at one connection to match the main
// DB's pool size. query_only=ON causes SQLite to reject any write at the
// engine level — the primary guarantee that plugins can't mutate the index.
// On success the handle is cached; on failure it is NOT cached — the next
// call retries — so a transient startup error doesn't permanently break
// plugin queries. On a vault switch (CloseVault) the cached handle is closed
// and the next call re-opens against the new vault's index.
func (a *App) openPluginRODB() (*sql.DB, error) {
	a.pluginRODBMu.Lock()
	defer a.pluginRODBMu.Unlock()
	if a.pluginRODB != nil {
		return a.pluginRODB, nil
	}
	dsn := "file::memory:?cache=shared"
	if a.db != nil && a.db.IsOnDisk() {
		dsn = a.db.Path()
	}
	ro, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open read-only plugin DB: %w", err)
	}
	ro.SetMaxOpenConns(1)
	if _, err := ro.Exec("PRAGMA query_only = ON"); err != nil {
		ro.Close()
		return nil, fmt.Errorf("failed to enable query_only on plugin DB: %w", err)
	}
	a.pluginRODB = ro
	return ro, nil
}

// stripSQLComments removes SQL line ("--") and block ("/* ... */") comments
// from anywhere in the string, preserving single-quoted string literals
// (so a string containing "--" or "/*" is not corrupted). The result is then
// checked against the SELECT/WITH prefix list and the statement-class blocker.
// This is a defense-in-depth scanner, not a SQL parser — the connection-level
// isolation (query_only on the core index; distinct file on the plugin DB)
// is the primary guarantee.
func stripSQLComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inString := false
	i := 0
	for i < len(s) {
		c := s[i]
		// Handle single-quoted string literals (SQL escapes '' inside).
		if c == '\'' {
			if inString && i+1 < len(s) && s[i+1] == '\'' {
				b.WriteByte('\'')
				b.WriteByte('\'')
				i += 2
				continue
			}
			inString = !inString
			b.WriteByte(c)
			i++
			continue
		}
		if inString {
			b.WriteByte(c)
			i++
			continue
		}
		// Line comment: -- until end of line.
		if c == '-' && i+1 < len(s) && s[i+1] == '-' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		// Block comment: /* until */.
		if c == '/' && i+1 < len(s) && s[i+1] == '*' {
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				// Unterminated block comment — strip the rest.
				break
			}
			i += 2 + end + 2
			continue
		}
		b.WriteByte(c)
		i++
	}
	return strings.TrimSpace(b.String())
}

// PluginRawQueryResult is the structured return value for PluginRawQuery.
// `Rows` is the row slice; `Truncated` is true when the result hit
// `maxPluginQueryRows` and the caller should warn the user that more
// rows exist beyond the cap. The cap itself is a security/memory
// safeguard against malicious or accidentally unbounded SELECTs, not a
// design limit on legitimate queries — surfacing `Truncated` lets the
// plugin SDK give the UI a chance to render a "N+ more rows" hint
// rather than silently dropping data on the floor.
type PluginRawQueryResult struct {
	Rows      []map[string]any `json:"rows"`
	Truncated bool             `json:"truncated"`
}

// PluginRawQuery runs a read-only SQL query against the in-memory index.
// Only SELECT / WITH statements are permitted; anything else is rejected so a
// plugin can never mutate the index or schema through this hook. The query
// is also executed against a connection with `PRAGMA query_only = ON`, which
// makes the engine reject any write attempt (including stacked queries like
// `SELECT 1; DROP TABLE blocks;`) regardless of how the prefix check is
// bypassed. Results are returned as PluginRawQueryResult: the row slice plus
// a Truncated flag the SDK can surface when the result hit maxPluginQueryRows.
// Session-token verified (#236) — closes the impersonation vector where a
// malicious main-webview plugin bypasses the SDK and calls App.PluginRawQuery
// directly.
func (a *App) PluginRawQuery(pluginID, sessionToken, sqlText string, params []any) (PluginRawQueryResult, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return PluginRawQueryResult{}, err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return PluginRawQueryResult{}, fmt.Errorf("vault database not loaded")
	}
	trimmed := stripSQLComments(sqlText)
	upper := strings.ToUpper(trimmed)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return PluginRawQueryResult{}, fmt.Errorf("PluginRawQuery permits only SELECT/WITH statements")
	}

	roDB, err := a.openPluginRODB()
	if err != nil {
		return PluginRawQueryResult{}, err
	}

	a.wg.Add(1)
	defer a.wg.Done()

	out := PluginRawQueryResult{Rows: []map[string]any{}}
	err = a.coordinator.WithDBReadResult(func() error {
		rows, err := roDB.Query(trimmed, params...)
		if err != nil {
			return err
		}
		defer rows.Close()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				return err
			}
			row := make(map[string]any, len(cols))
			for i, c := range cols {
				row[c] = values[i]
			}
			out.Rows = append(out.Rows, row)
			// Cap the result set so a malicious plugin can't exhaust memory
			// with SELECT * FROM blocks on a large vault. Surface the cap
			// hit to the caller via Truncated only when there are actually
			// more rows beyond the cap (avoids a false Truncated at the exact
			// boundary where the last row fills the cap).
			if len(out.Rows) >= maxPluginQueryRows {
				if rows.Next() {
					out.Truncated = true
				}
				break
			}
		}
		return rows.Err()
	})
	return out, err
}

// PluginMutateBlock wraps MutateBlock for the plugin SDK, returning success.
// Session-token verified (#236) — a plugin cannot mutate another plugin's
// blocks by spoofing the call without the SDK.
func (a *App) PluginMutateBlock(pluginID, sessionToken, blockID, newText string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return false, fmt.Errorf("vault database not loaded")
	}
	// Block text is single-line; collapse any newlines to spaces.
	cleanText := strings.ReplaceAll(newText, "\n", " ")

	a.wg.Add(1)
	defer a.wg.Done()

	if err := a.writeBlockText(blockID, historyReasonPlugin, func(_ string) (string, error) {
		return cleanText, nil
	}); err != nil {
		return false, err
	}
	return true, nil
}

// PluginBlockStateResult is the SDK return for a status transition: Ok is true
// when the transition applied, and SpawnedId carries the UUID of the recurrence
// instance spawned by a recurring TODO/DOING → DONE transition (empty
// otherwise). Returning the id from the atomic Go transition lets the agent
// chain off it directly instead of re-querying for the sibling (#812).
type PluginBlockStateResult struct {
	Ok        bool   `json:"ok"`
	SpawnedId string `json:"spawned_id"`
}

// PluginUpdateBlockState wraps UpdateBlockState for the plugin SDK.
// Session-token verified (#236).
func (a *App) PluginUpdateBlockState(pluginID, sessionToken, blockID, status string) (PluginBlockStateResult, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return PluginBlockStateResult{}, err
	}
	spawnedID, err := a.updateBlockStateWithReason(blockID, status, historyReasonPlugin)
	if err != nil {
		return PluginBlockStateResult{}, err
	}
	return PluginBlockStateResult{Ok: true, SpawnedId: spawnedID}, nil
}

// PluginUpdateTaskMeta updates per-task metadata (pin, progress) by
// round-tripping through the markdown file. Both fields are file-resident
// user intent (ARCHITECTURE §0) — the change is written to the .md file
// as [pin:: true] / [progress:: N] tokens via the parser + renderer, then
// re-indexed so SQLite reflects the new state.
//
// Sentinels allow partial updates:
//
//	pin:      -2 = clear (remove the [pin::] token), -1 = no change,
//	          0 = explicitly unpin ([pin:: false]), 1 = pin ([pin:: true])
//	progress: -1 = no change, 0-100 = set value (0 clears the token)
//
// The tri-state pin sentinel preserves a typed [pin:: false] across UI
// toggles: the renderer emits exactly one pin token from the *bool, so
// pin → unpin → pin can never produce two competing tokens (#123).
func (a *App) PluginUpdateTaskMeta(pluginID, sessionToken, blockID string, pin int, progress int) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	// Capability gate — mirrors every sibling Plugin* task setter
	// (PluginSetTaskOwner/Priority/Tags/Title/Order/DueDate/BlockedBy,
	// PluginAppendTaskComment). pin/progress are file-resident user intent, so
	// a third-party plugin without CapContentMutate must not toggle them.
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	// Input validation runs before acquiring the lock; mutateTaskBlock takes
	// its own RLock. (Pre-refactor these checks happened under the lock — pure
	// input/range checks don't need it.)
	if pin < -2 || pin > 1 {
		return false, fmt.Errorf("invalid pin value %d (valid: -2=clear, -1=no change, 0=unpin, 1=pin)", pin)
	}
	if progress < -1 || progress > 100 {
		return false, fmt.Errorf("invalid progress value %d (valid: -1=no change, 0-100)", progress)
	}
	if pin == -1 && progress == -1 {
		return true, nil // no-op
	}
	// Delegate the canonical write chain to mutateTaskBlock (#476). This
	// retires a ~135-line inline duplicate and, as a side effect, UPGRADES the
	// emit semantics: the inline version emitted inside the lock with no
	// re-parse-failure fallback (a latent deadlock footgun + missing
	// emit-on-failure); mutateTaskBlock emits AFTER the locks release with a
	// fileDate fallback (the round-3 emit-on-failure fix). It also inherits
	// the focus-lock guard (#444) every other task-setter now shares.
	if err := a.mutateTaskBlock(blockID, "PluginUpdateTaskMeta", historyReasonPlugin, func(b *parser.ParsedBlock) {
		if pin != -1 {
			switch pin {
			case -2:
				b.Pinned = nil // remove the token
			case 0:
				v := false
				b.Pinned = &v // [pin:: false]
			case 1:
				v := true
				b.Pinned = &v // [pin:: true]
			}
		}
		if progress != -1 {
			b.Progress = progress
		}
	}); err != nil {
		return false, err
	}
	return true, nil
}

// SetTaskDueDate rewrites a task's [due:: YYYY-MM-DD] inline token on disk.
// Empty dueDate clears the token. Shared by PluginSetTaskDueDate and Local MCP.
func (a *App) SetTaskDueDate(blockID, dueDate string) error {
	// "" clears the token; a non-empty value must be a valid YYYY-MM-DD so a
	// malformed date can never reach disk. Validated up front (before the lock
	// mutateTaskBlock takes) to preserve the early-rejection behavior.
	if dueDate != "" {
		if _, derr := time.Parse("2006-01-02", dueDate); derr != nil {
			return fmt.Errorf("invalid dueDate %q (want YYYY-MM-DD or empty to clear)", dueDate)
		}
	}
	return a.setTaskDueDate(blockID, dueDate, historyReasonEditor)
}

func (a *App) setTaskDueDate(blockID, dueDate, reason string) error {
	if dueDate != "" {
		if _, derr := time.Parse("2006-01-02", dueDate); derr != nil {
			return fmt.Errorf("invalid dueDate %q (want YYYY-MM-DD or empty to clear)", dueDate)
		}
	}
	return a.mutateTaskBlock(blockID, "SetTaskDueDate", reason, func(b *parser.ParsedBlock) {
		b.DueDate = dueDate
	})
}

// PluginSetTaskDueDate rewrites a task's [due:: YYYY-MM-DD] inline token on
// disk atomically (#293). Pass the empty string to clear the due date. This
// is the mutation surface behind calendar drag-and-drop rescheduling: drop a
// task card on a day cell → set due date to that day. It reuses the same
// LockBlockWrite + LockFileWrite + WriteFileAtomic + re-index + emit chain
// as every other writer, so there is one on-disk format definition.
//
// Gated by content-mutate (#156). Session-token verified (#236).
func (a *App) PluginSetTaskDueDate(pluginID, sessionToken, blockID, dueDate string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskDueDate(blockID, dueDate, historyReasonPlugin); err != nil {
		return false, err
	}
	return true, nil
}

// PluginSetTaskStartDate rewrites a task's [start:: YYYY-MM-DD] planning date
// on disk. The start date is independent metadata: it does not derive a due
// date or duration, and changing either of those fields does not change it.
// Pass the empty string to clear the token.
//
// Gated by content-mutate. Session-token verified.
func (a *App) PluginSetTaskStartDate(pluginID, sessionToken, blockID, startDate string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if startDate != "" {
		if _, derr := time.Parse("2006-01-02", startDate); derr != nil {
			return false, fmt.Errorf("invalid startDate %q (want YYYY-MM-DD or empty to clear)", startDate)
		}
	}
	if err := a.mutateTaskBlock(blockID, "PluginSetTaskStartDate", historyReasonPlugin, func(b *parser.ParsedBlock) {
		b.StartDate = startDate
	}); err != nil {
		return false, err
	}
	return true, nil
}

// PluginSetTaskEstimate rewrites a task's [estimate::] duration token on disk
// (#439). Pass the empty string to clear the estimate. Non-empty values must
// parse via parser.ParseEstimateMinutes (m/h/d units) so invalid durations
// never reach the file. Gated by content-mutate; session-token verified.
func (a *App) PluginSetTaskEstimate(pluginID, sessionToken, blockID, estimate string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	estimate = strings.TrimSpace(estimate)
	if estimate != "" {
		if _, ok := parser.ParseEstimateMinutes(estimate); !ok {
			return false, fmt.Errorf("invalid estimate %q (want e.g. 30m, 2h, 1d, or empty to clear)", estimate)
		}
	}
	if err := a.mutateTaskBlock(blockID, "PluginSetTaskEstimate", historyReasonPlugin, func(b *parser.ParsedBlock) {
		b.Estimate = estimate
	}); err != nil {
		return false, err
	}
	return true, nil
}

// GetPluginRegistry returns the `plugins:` block of .system/config.yaml from
// the in-memory config (the single source of truth maintained by the config
// package + hot-reload watcher), so callers never re-read the file.
func (a *App) GetPluginRegistry() (parser.PluginRegistry, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	registry := parser.PluginRegistry{Active: []string{}, Disabled: []string{}}
	if a.vaultPath == "" {
		return registry, fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	registry.Active = a.cfg.Plugins.Active
	registry.Disabled = a.cfg.Plugins.Disabled
	registry.Settings = a.cfg.Plugins.PluginSettings
	if registry.Active == nil {
		registry.Active = []string{}
	}
	if registry.Disabled == nil {
		registry.Disabled = []string{}
	}
	if registry.Settings == nil {
		registry.Settings = map[string]any{}
	}
	return registry, nil
}

// GetPluginSettingsForNotebook resolves a plugin's settings map for the
// ACTIVE notebook, applying the co-located per-notebook override layer (#133).
//
// Merge precedence: vault-scoped config.yaml is the baseline; a linked
// notebook's co-located <root>/.system/config.yaml overlays it per-key
// (linked wins). For a vault notebook (or no active notebook), the vault
// settings are returned unchanged. For a linked notebook with no co-located
// file, the vault settings are returned (the normal case). The merge is
// computed on every call from the live, mtime-cached co-located config, so an
// external edit to either file is reflected on the next call (the watcher
// also emits linked-config:changed to drive reactive refreshes).
//
// pluginID selects which plugin's entry is returned (e.g. "silt-tasks"). An
// unknown pluginID yields an empty map, not an error — a plugin with no
// stored settings is the same as a plugin whose settings are all defaults.
//
// notebookName is the display name (the sidebar label); it is resolved to a
// source via resolveSourceByName. An empty notebookName is treated as the
// vault scope (no active notebook).
func (a *App) GetPluginSettingsForNotebook(pluginID, notebookName string) (map[string]any, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	if pluginID == "" {
		return nil, fmt.Errorf("pluginID is required")
	}

	// Snapshot the vault entry + resolve the source under the config read
	// lock. linkedConfigFor uses its OWN mutex (linkedConfigsMu) for the
	// co-located cache, so we release configMu before calling it — this
	// avoids holding configMu during disk I/O on a cache miss and avoids
	// the concurrent-map-write panic that would arise if linkedConfigFor
	// wrote to linkedConfigs under an RLock.
	//
	// CRITICAL: vaultEntry is cloned (via MergePluginSettings) INSIDE the
	// RLock, not after release. UpdatePluginSetting mutates this map
	// in-place (entry[key]=value) under configMu.Lock(); cloning after
	// RUnlock would expose the clone iteration to a concurrent write.
	a.configMu.RLock()
	vaultEntry, _ := a.cfg.Plugins.PluginSettings[pluginID].(map[string]any)
	if vaultEntry == nil {
		vaultEntry = map[string]any{}
	}
	// Deep-clone under the lock so the returned map is a safe snapshot.
	vaultClone := config.MergePluginSettings(vaultEntry, nil)
	source := config.LinkedNotebooksVaultSource
	var ln config.LinkedNotebook
	if notebookName != "" {
		source = a.resolveSourceByNameLocked(notebookName)
		if source != config.LinkedNotebooksVaultSource {
			for _, candidate := range a.cfg.LinkedNotebooks {
				if candidate.Source() == source {
					ln = candidate
					break
				}
			}
		}
	}
	a.configMu.RUnlock()

	if source == config.LinkedNotebooksVaultSource {
		// Vault notebook (or no active notebook): return the cloned snapshot.
		return vaultClone, nil
	}

	// Linked notebook: if the registry didn't find the source (stale),
	// degrade gracefully to vault settings (already cloned).
	if ln.ID == "" {
		log.Printf("GetPluginSettingsForNotebook(%s,%s): source %q not in registry; returning vault settings", pluginID, notebookName, source)
		return vaultClone, nil
	}
	linkedCfg, err := a.linkedConfigFor(ln)
	if err != nil {
		// Fail-loud: an unparseable co-located config surfaces as an error
		// rather than silently degrading to vault settings (the user must
		// see their broken file). A MISSING file is not an error (LoadLinked
		// returns Defaults + nil in that case).
		return nil, fmt.Errorf("linked config for %s: %w", ln.DisplayName, err)
	}
	linkedEntry, _ := linkedCfg.Plugins.PluginSettings[pluginID].(map[string]any)
	if linkedEntry == nil {
		linkedEntry = map[string]any{}
	}
	return config.MergePluginSettings(vaultClone, linkedEntry), nil
}

// ListPlugins enumerates plugin folders under .system/plugins/, surfacing
// manifest name/version and the disabled sentinel for the manager UI.
func (a *App) ListPlugins() ([]parser.PluginInfo, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return nil, fmt.Errorf("vault not loaded")
	}
	pluginsDir := filepath.Join(a.vaultPath, ".system", "plugins")
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []parser.PluginInfo{}, nil
		}
		return nil, fmt.Errorf("failed to read plugins dir: %w", err)
	}
	var infos []parser.PluginInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		dir := filepath.Join(pluginsDir, name)
		info := parser.PluginInfo{ID: name, Disabled: plugins.IsDisabled(dir)}
		if manifestBytes, err := os.ReadFile(filepath.Join(dir, "plugin.json")); err == nil {
			info.HasManifest = true
			var m parser.PluginManifest
			if json.Unmarshal(manifestBytes, &m) == nil {
				info.Name = m.Name
				info.Version = m.Version
				info.Author = m.Author
				info.Description = m.Description
				info.Icon = m.Icon
				info.Capabilities = m.Capabilities
				info.Settings = m.Settings
				info.Homepage = m.Homepage
				info.UpdateURL = m.UpdateURL
				info.ContentSHA256 = m.ContentSHA256
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "index.js")); err == nil {
			info.HasIndex = true
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// ReadPluginSource returns the ESM source of a plugin's index.js for the
// dynamic loader.
func (a *App) ReadPluginSource(pluginID string) (string, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	safeID := sanitizePathSegment(pluginID)
	if safeID == "" {
		return "", fmt.Errorf("invalid plugin id")
	}
	srcPath := filepath.Join(a.vaultPath, ".system", "plugins", safeID, "index.js")
	if !isPathWithinRoot(srcPath, a.vaultPath) {
		return "", fmt.Errorf("path escapes vault")
	}
	bytes, err := os.ReadFile(srcPath)
	if err != nil {
		return "", fmt.Errorf("failed to read plugin source: %w", err)
	}
	return string(bytes), nil
}
