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
	"silt/backend/vault"
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
	if err := a.MutateBlock(blockID, newText); err != nil {
		return false, err
	}
	return true, nil
}

// PluginUpdateBlockState wraps UpdateBlockState for the plugin SDK.
// Session-token verified (#236).
func (a *App) PluginUpdateBlockState(pluginID, sessionToken, blockID, status string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.UpdateBlockState(blockID, status); err != nil {
		return false, err
	}
	return true, nil
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
	if err := a.mutateTaskBlock(blockID, "PluginUpdateTaskMeta", func(b *parser.ParsedBlock) {
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
	// "" clears the token; a non-empty value must be a valid YYYY-MM-DD so a
	// malformed date can never reach disk. Validated up front (before the lock
	// mutateTaskBlock takes) to preserve the early-rejection behavior.
	if dueDate != "" {
		if _, derr := time.Parse("2006-01-02", dueDate); derr != nil {
			return false, fmt.Errorf("invalid dueDate %q (want YYYY-MM-DD or empty to clear)", dueDate)
		}
	}
	// Delegate the canonical write chain to mutateTaskBlock (#476): retires a
	// ~135-line inline duplicate. mutateTaskBlock's emit-on-failure path
	// (fileDate fallback, emit after locks release) is byte-for-byte the same
	// pattern this function already used, so the refactor is behavior-preserving
	// for due-date writes and adds focus-lock protection (#444) for free.
	if err := a.mutateTaskBlock(blockID, "PluginSetTaskDueDate", func(b *parser.ParsedBlock) {
		b.DueDate = dueDate
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
	if err := a.mutateTaskBlock(blockID, "PluginSetTaskEstimate", func(b *parser.ParsedBlock) {
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

// GetSystemConfig returns an independent snapshot of the parsed system config.
// IPC callers must not be able to mutate maps and slices in the live config
// after the read lock is released.
func (a *App) GetSystemConfig() (config.SystemConfig, error) {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return config.Defaults(), fmt.Errorf("vault not loaded")
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return config.Clone(a.cfg), nil
}

// GetConfigLoadError returns the error from the initial config.yaml load (if
// any) and clears it. The startup load runs before the frontend subscribes to
// config:error, so that event can be missed; this binding lets the frontend
// retrieve the one-shot error on its first loadConfig() so a broken config is
// surfaced rather than silently masked by Defaults(). Returns "" when there
// was no error (or it was already retrieved).
func (a *App) GetConfigLoadError() string {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.configLoadErr == nil {
		return ""
	}
	msg := a.configLoadErr.Error()
	a.configLoadErr = nil
	return msg
}

// saveConfigTracked persists cfg atomically while managing the config watcher's
// self-write suppression window. The window is armed BEFORE the write so Silt's
// own atomic save (temp + rename → a burst of fsnotify events) is suppressed,
// then cleared via UnregisterSelfWrite if the write fails — otherwise a failed
// save would leave the 500ms window open and silently drop a legitimate
// external edit landing inside it. Safe to call while holding configMu:
// config.Save and the watcher's selfMu are independent of configMu. This is the
// single source of truth for the register/unregister discipline; every atomic
// config setter routes its persist through here.
func (a *App) saveConfigTracked(cfg config.SystemConfig) error {
	if a.configWatcher != nil {
		a.configWatcher.RegisterSelfWrite()
	}
	if err := config.Save(a.vaultPath, cfg); err != nil {
		if a.configWatcher != nil {
			a.configWatcher.UnregisterSelfWrite()
		}
		return err
	}
	return nil
}

// SaveSystemConfig validates, persists atomically, and applies the new config.
// The self-write is registered first so the hot-reload watcher ignores the
// fsnotify event from our own atomic write.
func (a *App) SaveSystemConfig(cfg config.SystemConfig) error {
	if cfg.Editor.TabIndentSpaces <= 0 {
		return fmt.Errorf("invalid config: editor.tab_indent_spaces must be positive")
	}
	if cfg.Editor.FontSizePx <= 0 {
		return fmt.Errorf("invalid config: editor.font_size_px must be positive")
	}
	if cfg.Editor.LineHeight <= 0 {
		return fmt.Errorf("invalid config: editor.line_height must be positive")
	}
	if cfg.Editor.AutoSaveDelayMs < 0 {
		return fmt.Errorf("invalid config: editor.auto_save_delay_ms must be non-negative")
	}
	if err := config.ValidateHotkeys(cfg.Hotkeys); err != nil {
		return err
	}
	// Clone the frontend snapshot before the serialized mutation. Besides
	// preventing stale navigation state from being adopted, this keeps mutable
	// maps and slices in the caller's value out of the live config on failure.
	incoming := config.Clone(cfg)
	var quarantined []map[string]string
	err := a.mutateConfig(func(current *config.SystemConfig) error {
		// SaveSystemConfig remains for unrelated settings, but navigation state
		// is backend-owned and must never be replaced by a stale whole snapshot.
		// The linked-notebook registry is likewise backend-owned: a snapshot
		// taken before a link was added must not remove that link.
		incoming.UI.NavOrder = config.CloneNavOrder(current.UI.NavOrder)
		incoming.UI.OpenTabs = append([]config.TabRef(nil), current.UI.OpenTabs...)
		if current.UI.ActiveTab == nil {
			incoming.UI.ActiveTab = nil
		} else {
			active := *current.UI.ActiveTab
			incoming.UI.ActiveTab = &active
		}
		incoming.UI.ExpandedSections = append([]config.NavigationSectionRef(nil), current.UI.ExpandedSections...)
		incoming.UI.RecentPages = append([]config.RecentPage(nil), current.UI.RecentPages...)
		incoming.UI.Favorites = append([]config.NavigationPageRef(nil), current.UI.Favorites...)
		incoming.LinkedNotebooks = append([]config.LinkedNotebook(nil), current.LinkedNotebooks...)
		incoming.AI.Chat.APIKey = current.AI.Chat.APIKey
		incoming.AI.Embedding.APIKey = current.AI.Embedding.APIKey
		// Treat this like a config reload for linked-notebook security: frontend
		// snapshots are not trusted sources for roots or their fingerprints.
		quarantined = a.reconcileLinkedNotebookSecurityLocked(&incoming)
		*current = incoming
		a.spacesPerTab = incoming.Editor.TabIndentSpaces
		return nil
	})
	if err != nil {
		return err
	}
	a.configMu.Lock()
	a.seedFirstPartyGrants()
	a.configMu.Unlock()
	for _, q := range quarantined {
		a.emit("linked-notebook:quarantined", q)
	}
	return nil
}

// applyConfig stores the parsed config under configMu, applies the live
// Go-side knobs (tab indent width), then emits config:changed so the frontend
// refreshes editor settings, hotkeys, and per-plugin settings.
func (a *App) applyConfig(cfg config.SystemConfig) {
	quarantined := a.applyConfigLocked(cfg)
	a.emit("config:changed", cfg)
	// F3: emit linked-notebook:quarantined for any links whose root_path
	// changed in the external edit (synced-vault attack vector).
	for _, q := range quarantined {
		a.emit("linked-notebook:quarantined", q)
	}
}

// applyConfigLocked updates a.cfg + live knobs under the write lock. Split out
// so initializeVaultServices can set the config (and spacesPerTab) before the
// first scan without emitting an event for a vault the frontend hasn't seen yet.
// Returns a slice of newly-quarantined linked-notebook event payloads (for
// applyConfig to emit after unlock — the lock is held here so quarantineLink
// cannot be called).
func (a *App) applyConfigLocked(cfg config.SystemConfig) []map[string]string {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	quarantined := a.reconcileLinkedNotebookSecurityLocked(&cfg)
	a.cfg = cfg
	if cfg.Editor.TabIndentSpaces > 0 {
		a.spacesPerTab = cfg.Editor.TabIndentSpaces
	}
	a.seedFirstPartyGrants()
	return quarantined
}

// reconcileLinkedNotebookSecurityLocked applies the trusted-root rules to a
// candidate config. The caller holds configMu and commits the candidate after
// this function returns.
func (a *App) reconcileLinkedNotebookSecurityLocked(cfg *config.SystemConfig) []map[string]string {
	// F3: when a config reloads from disk (fsnotify), preserve the in-memory
	// RootFingerprint for each linked notebook and quarantine any link whose
	// root changed or was added by an external edit. The M2 (synced-vault)
	// adversary can edit config.yaml freely — without these checks they could
	// redirect an existing link's root_path to an attacker folder, or inject a
	// brand-new link pointing at a hostile root, both with no fingerprint.
	var quarantined []map[string]string
	if a.quarantinedLinks == nil {
		a.quarantinedLinks = make(map[string]struct{})
	}
	// Snapshot the set of known link IDs so we can detect new entries.
	knownIDs := make(map[string]bool, len(a.cfg.LinkedNotebooks))
	for _, existing := range a.cfg.LinkedNotebooks {
		knownIDs[existing.ID] = true
	}
	newlyQuarantined := make(map[string]bool) // IDs quarantined in THIS call
	for i, reloaded := range cfg.LinkedNotebooks {
		if !knownIDs[reloaded.ID] {
			// NEW link from an external edit — the M2 adversary injected a
			// link to an attacker-chosen root. Quarantine immediately; the
			// user confirms via the re-link modal or unlinks.
			a.quarantinedLinks[reloaded.ID] = struct{}{}
			newlyQuarantined[reloaded.ID] = true
			quarantined = append(quarantined, map[string]string{
				"id":           reloaded.ID,
				"display_name": reloaded.DisplayName,
				"reason":       "new_link_from_external_edit",
			})
			log.Printf("applyConfigLocked: quarantined new link %s (appeared in external config edit)", reloaded.DisplayName)
			continue
		}
		for _, existing := range a.cfg.LinkedNotebooks {
			if reloaded.ID != existing.ID {
				continue
			}
			if reloaded.RootPath != existing.RootPath {
				// root_path changed via external edit — quarantine and
				// preserve the trusted in-memory root + fingerprint.
				a.quarantinedLinks[reloaded.ID] = struct{}{}
				newlyQuarantined[reloaded.ID] = true
				cfg.LinkedNotebooks[i].RootPath = existing.RootPath
				cfg.LinkedNotebooks[i].RootFingerprint = existing.RootFingerprint
				quarantined = append(quarantined, map[string]string{
					"id":           reloaded.ID,
					"display_name": reloaded.DisplayName,
					"reason":       "root_path_changed",
				})
				log.Printf("applyConfigLocked: quarantined %s (root_path changed in external edit)", reloaded.DisplayName)
			} else {
				// RootPath unchanged — preserve the fingerprint captured at link time.
				cfg.LinkedNotebooks[i].RootFingerprint = existing.RootFingerprint
			}
			break
		}
	}
	// P2 prune: remove stale quarantine entries for links that no longer exist
	// in the reloaded config (user unlinked, or synced config removed them).
	// Keep entries for links quarantined in THIS call (they ARE in the config).
	for id := range a.quarantinedLinks {
		if newlyQuarantined[id] {
			continue
		}
		stillExists := false
		for _, ln := range cfg.LinkedNotebooks {
			if ln.ID == id {
				stillExists = true
				break
			}
		}
		if !stillExists {
			delete(a.quarantinedLinks, id)
		}
	}
	return quarantined
}

// seedFirstPartyGrants populates the per-host grants store with every
// capability for each first-party plugin ID, so bundled plugins are implicitly
// trusted WITHOUT a special-case bypass in requireGrant. This closes the
// spoofing vector where a third-party plugin passes 'silt-attachments' as
// pluginID to bypass all capability checks (#113 security hardening).
//
// F4: grants now live in the per-host store (a.grants), not vault-scoped
// config.yaml. Seeding is in-memory only for the session — the store is NOT
// re-persisted on every applyConfigLocked (that would write grants.json on
// every config reload for no reason). The seeded entries persist for the vault
// session; a fresh launch re-seeds from LoadGrants + this function.
func (a *App) seedFirstPartyGrants() {
	if a.grants == nil {
		a.grants = vault.GrantsStore{}
	}
	for id := range plugins.FirstPartyPluginIDs {
		if a.grants[id] == nil {
			a.grants[id] = map[string]string{}
		}
		for cap := range plugins.KnownCapabilities {
			a.grants[id][string(cap)] = plugins.QualGranted
		}
	}
}

// UpdatePluginSetting atomically updates a single per-plugin setting key and
// persists it — the targeted read-modify-write that replaces the frontend
// read-mutate-saveConfig dance which could race an external config.yaml edit
// (e.g. an external editor) landing between the read and the Go-side atomic write (#120).
// Only plugins.plugin_settings[pluginID][key] is touched; every other config
// field is preserved verbatim, so a concurrent external edit to an unrelated
// section is not clobbered.
//
// TOCTOU hardening (#475): config.yaml is RE-READ from disk inside configMu.Lock
// immediately before the mutation, so an external edit that landed after the
// last config.Load (vault init / watcher applyConfig) is merged into the save
// rather than overwritten. The mutation is applied to the fresh read, a.cfg is
// refreshed, then config.Save serializes the result. The external-edit loss
// window shrinks to the lock-hold duration (no cross-IPC gap). If the re-read
// fails (corrupt file), the call refuses loudly with an error — safer than
// silently overwriting the corrupt file with the stale in-memory cfg, which
// would destroy the user's only signal that their config is broken.
//
// Atomicity: configMu is held across the re-read, in-memory mutation, AND the
// disk save, so concurrent internal callers (and the watcher's applyConfig)
// cannot interleave a snapshot-and-save and lose an update. The external-edit
// race is handled by RegisterSelfWrite (suppresses the watcher's reaction to
// our own write) + this lock. Like SaveSystemConfig it does NOT emit
// config:changed (the frontend store updates optimistically; external edits
// still flow through the watcher -> applyConfig with emit).
func (a *App) UpdatePluginSetting(pluginID string, key string, value any) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if pluginID == "" || key == "" {
		return fmt.Errorf("pluginID and key are required")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	// Re-read config.yaml under the lock so an external edit to an unrelated
	// key is preserved rather than overwritten by the stale in-memory cfg.
	// A re-read failure (corrupt/unreadable file) is returned as an error —
	// refusing loudly is safer than silently overwriting the corrupt file
	// with the stale in-memory cfg, which would destroy the user's only
	// signal that their config is broken.
	freshCfg, loadErr := config.Load(a.vaultPath)
	if loadErr != nil {
		return fmt.Errorf("cannot read config.yaml before update: %w", loadErr)
	}
	if freshCfg.Plugins.PluginSettings == nil {
		freshCfg.Plugins.PluginSettings = map[string]any{}
	}
	entry, _ := freshCfg.Plugins.PluginSettings[pluginID].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
	}
	entry[key] = value
	freshCfg.Plugins.PluginSettings[pluginID] = entry
	a.cfg = freshCfg
	return a.saveConfigTracked(a.cfg)
}

// AppendDismissedTip records a one-time UI tip ID as dismissed (#197). Mirrors
// the atomic pattern of UpdatePluginSetting: vaultMu.RLock + configMu.Lock held
// across the in-memory mutation and config.Save, with RegisterSelfWrite
// suppressing the watcher's reaction to our own write. Idempotent — calling
// twice with the same tipID produces a single-entry slice. Like the other
// internal atomic setters, it does NOT emit config:changed; the frontend
// settings store mirrors the change optimistically and external edits still
// flow through watcher → applyConfig.
func (a *App) AppendDismissedTip(tipID string) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	if tipID == "" {
		return fmt.Errorf("tipID is required")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	if a.cfg.UI.DismissedTips == nil {
		a.cfg.UI.DismissedTips = []string{}
	}
	for _, existing := range a.cfg.UI.DismissedTips {
		if existing == tipID {
			return nil
		}
	}
	a.cfg.UI.DismissedTips = append(a.cfg.UI.DismissedTips, tipID)
	return a.saveConfigTracked(a.cfg)
}

// SetShowFormatToolbar atomically writes the format-toolbar visibility to
// config.yaml. It exists so the global format-toolbar toggle (hotkey / floating
// button) does NOT route through the frontend's saveConfig — that path clears
// the settings dirty flag and would silently clobber a user's unsaved EditorTab
// draft. Mirrors AppendDismissedTip: lock, mutate the one field, self-write
// suppress, save. The frontend mirrors the field into its config snapshot
// without touching dirty.
func (a *App) SetShowFormatToolbar(value bool) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.cfg.UI.ShowFormatToolbar = &value
	return a.saveConfigTracked(a.cfg)
}

// SetFocusMode atomically writes the editor focus-mode flag. Same rationale as
// SetShowFormatToolbar — avoids clobbering an unsaved settings draft.
func (a *App) SetFocusMode(value bool) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	a.cfg.Editor.FocusMode = &value
	return a.saveConfigTracked(a.cfg)
}

// SetOpenDevtoolsOnStartup atomically writes the Dev Mode (open DevTools on
// startup) flag. The About → Developer toggle routes through here instead of
// the full-config saveConfig path because that path clones a Svelte 5 $state
// proxy via structuredClone (throws DataCloneError in the webview, silently
// swallowed) and would clobber an unsaved EditorTab draft. Mirrors
// SetShowFormatToolbar.
func (a *App) SetOpenDevtoolsOnStartup(value bool) error {
	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
		return fmt.Errorf("vault not loaded")
	}
	a.configMu.Lock()
	a.cfg.UI.OpenDevtoolsOnStartup = &value
	err := a.saveConfigTracked(a.cfg)
	a.configMu.Unlock()
	a.vaultMu.RUnlock()
	if err != nil {
		return err
	}
	// After locks released — sync takes vaultMu/configMu via devToolsRuntimeEnabled.
	a.syncOpenDevToolsMenuItem()
	return nil
}

// syncOpenDevToolsMenuItem enables View → Open Developer Tools when runtime
// DevTools are allowed (vault Dev Mode or SILT_DEBUG=1). Prefer disabled over
// hidden so the item stays visible in the menu structure (#684).
// Must not be called while holding vaultMu exclusively (RLock would deadlock).
func (a *App) syncOpenDevToolsMenuItem() {
	a.syncOpenDevToolsMenuItemEnabled(a.devToolsRuntimeEnabled())
}

// syncOpenDevToolsMenuItemEnabled applies a precomputed enable flag — use when
// the caller already holds vaultMu (e.g. initializeVaultServices).
func (a *App) syncOpenDevToolsMenuItemEnabled(enabled bool) {
	if a.openDevToolsMenuItem == nil {
		return
	}
	a.openDevToolsMenuItem.SetEnabled(enabled)
}

// OpenDevTools opens the webview developer tools when Dev Mode is enabled
// (#679). No-op (returns nil) when the flag is off, SILT_DEBUG is unset, the
// main window is unavailable, or no vault is loaded — production builds
// without the Wails devtools tag may also no-op at the platform layer.
//
// Runtime gate matches launch-time shouldOpenDevtools: vault config flag OR
// SILT_DEBUG=1. Vault is not required when SILT_DEBUG is set (process-global).
func (a *App) OpenDevTools() error {
	if a.mainWindow == nil {
		return nil
	}
	if !a.devToolsRuntimeEnabled() {
		return nil
	}
	a.mainWindow.OpenDevTools()
	return nil
}

// devToolsRuntimeEnabled reports whether runtime OpenDevTools should proceed.
// Mirrors shouldOpenDevtools (main.go): SILT_DEBUG=1 or vault Dev Mode flag.
func (a *App) devToolsRuntimeEnabled() bool {
	if strings.EqualFold(os.Getenv("SILT_DEBUG"), "1") {
		return true
	}
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.vaultPath == "" {
		return false
	}
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.UI.OpenDevtoolsOnStartup != nil && *a.cfg.UI.OpenDevtoolsOnStartup
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
