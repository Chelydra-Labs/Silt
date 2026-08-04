package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/core"
	"silt/backend/db"
	"silt/backend/keyring"
	"silt/backend/mcp"
	"silt/backend/monitor"
	"silt/backend/spellcheck"
	"silt/backend/templates"
	"silt/backend/types"
	"silt/backend/vault"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed VERSION
var versionBytes []byte

// appVersion is the current Silt version, embedded at build time from the
// VERSION file. Used for plugin minSiltVersion enforcement.
var appVersion = strings.TrimSpace(string(versionBytes))

// startupEvent is one queued event emitted before the frontend mounted its
// Events.On listeners. In Wails v3, ServiceStartup fires before the webview
// exists, so a plain emit is lost; emitOrQueue stashes a copy here for
// GetStartupEvents to replay on mount. Payload is the first data arg (or nil),
// matching how Wails delivers a single-arg event as ev.data on the JS side.
type startupEvent struct {
	Name    EventName
	Payload any
}

type App struct {
	ctx      context.Context
	wailsApp *application.App
	// mainWindow is the primary webview window, stored so RequestClose can
	// hide it to the tray without going through v3's unexported windows map.
	mainWindow application.Window
	// openDevToolsMenuItem is View → Open Developer Tools. Enabled when Dev
	// Mode or SILT_DEBUG is on (#684); nil until setupMenus runs.
	openDevToolsMenuItem *application.MenuItem
	// startupEvents captures events emitted before the frontend mounted.
	// emitOrQueue appends here (in addition to emitting) until
	// MarkFrontendReady flips frontendReady; GetStartupEvents drains the slice
	// on mount so the frontend can replay missed startup events through the
	// same handlers its Events.On listeners use. Guarded by startupEventsMu.
	startupEvents     []startupEvent
	startupEventsMu   sync.Mutex
	frontendReady     bool
	startupDropLogged bool
	db                *db.DatabaseManager
	coordinator       *core.ExecutionCoordinator
	watcher           *monitor.DirectoryWatcher
	tracker           *monitor.WriteTracker
	vaultPath         string
	spacesPerTab      int
	wg                sync.WaitGroup

	// cfg is the parsed .system/config.yaml, the single source of truth for
	// non-vault-path settings. configMu guards it; it is replaced wholesale on
	// reload (never mutated in place) so a struct read under RLock is a safe
	// snapshot even though its map/slice fields share references.
	cfg           config.SystemConfig
	configMu      sync.RWMutex
	configWatcher *config.ConfigWatcher
	// configLoadErr holds the initial config.yaml load error, if any. The
	// startup load runs before the frontend subscribes to config:error, so
	// that event is typically lost; GetConfigLoadError surfaces this one-shot.
	configLoadErr error

	// keyringStore stores AI provider API keys off plaintext config.yaml
	// (#218). nil in tests (so the AI bindings fall back to config); set to
	// keyring.Default() in production at startup. Whether the OS keyring is
	// actually reachable is discovered at call time (a session can lock, a
	// D-Bus can drop), so callers fall back to config + a warning on
	// ErrUnavailable rather than failing the AI subsystem.
	keyringStore keyring.Store

	// mcpHost is the in-process local MCP server (#687). Started when
	// ai.local_mcp.enabled and a vault is open; stopped on vault close,
	// switch, and ServiceShutdown. Close-to-tray keeps the process (and MCP)
	// alive; Quit drains via ServiceShutdown. Constructed once (NewApp /
	// ensureMCPHost under mcpHostMu) so getters never race lazy init.
	mcpHost   *mcp.Host
	mcpHostMu sync.Mutex

	// aiCtx is the app-lifecycle context for AI HTTP calls. Cancelled in
	// shutdown() so in-flight completions/embeddings are cancelled on app
	// exit instead of running to their 60s timeout. nil in tests (the
	// aiContext() helper falls back to context.Background()).
	aiCtx       context.Context
	aiCtxCancel context.CancelFunc

	// vaultCtx is the vault-scoped context for AI HTTP calls — a CHILD of
	// aiCtx, re-created on every initializeVaultServices and cancelled in
	// CloseVault/SwitchVault before vaultClosingWG.Wait() so an in-flight
	// call aborts promptly (HTTP client observes context.Canceled in
	// milliseconds) instead of blocking the close/switch for up to the
	// provider timeout (~60s on a slow local model). aiCtxCancel (shutdown)
	// cascades and cancels vaultCtx too; cancelling vaultCtx alone leaves
	// the lifecycle context intact. aiContext() returns vaultCtx when set.
	// nil alongside aiCtx in tests (the helper falls back through aiCtx to
	// context.Background()).
	vaultCtx       context.Context
	vaultCtxCancel context.CancelFunc

	// aiModelCache holds the last ListModels poll per provider block ("chat" /
	// "embedding"), so the Settings dropdown isn't empty on cold start.
	// In-memory only (not persisted). Invalidated when the provider type, base
	// URL, or key changes (UpdateAIProviderConfig / SetAIAPIKey /
	// ClearAIAPIKey). Guarded by aiModelCacheMu (a dedicated mutex, NOT
	// configMu, so a slow list-poll can't stall config access).
	aiModelCacheMu sync.Mutex
	aiModelCache   map[string][]ai.AIModel

	// grants is the per-host plugin capability grant table (F4). It lives in
	// <configDir>/silt/grants.json (NOT in vault-scoped config.yaml) so a
	// vault synced from another host cannot carry the counterpart's grant
	// decisions. Guarded by configMu (grants are config-tier state even
	// though they persist to a different file than config.yaml). Loaded in
	// initializeVaultServices, torn down in teardownVaultServices.
	grants vault.GrantsStore
	// quarantinedLinks holds the IDs of linked notebooks whose on-disk root
	// no longer matches the stored RootFingerprint (F3). Presence in this set
	// means the link is quarantined: excluded from indexing, reads, and
	// writes; the user sees a re-link prompt. Guarded by configMu. Populated
	// at vault open (fingerprint mismatch) and on fsnotify reload (root_path
	// changed); cleared by UnlinkNotebook (re-link = unlink + link).
	quarantinedLinks map[string]struct{}

	// templateWatcher hot-reloads <vault>/.system/templates/ so the picker
	// stays live when a user adds/edits/deletes a custom template externally.
	// Started in initializeVaultServices, stopped in teardownVaultServices
	// (mirrors configWatcher).
	templateWatcher *templates.TemplateWatcher

	// typeWatcher hot-reloads <vault>/.system/types/ so typed pages and the
	// type manager stay live when a user adds/edits/deletes a type externally
	// (mirrors templateWatcher). Started in initializeVaultServices, stopped
	// in teardownVaultServices.
	typeWatcher *types.TypeWatcher

	// linkedConfigs is an mtime-aware cache of each linked notebook's
	// co-located <root>/.system/config.yaml (#133). Keyed by source
	// ('linked:<id>'). linkedConfigFor refreshes an entry when the on-disk
	// mtime advances; the watcher invalidates an entry on external edit so
	// the next read re-loads. Guarded by linkedConfigsMu (a dedicated mutex,
	// NOT configMu) so concurrent GetPluginSettingsForNotebook callers can
	// safely read/write the cache without holding configMu's write lock
	// (which would serialize all config access) and without risking a
	// concurrent-map-write panic under configMu.RLock (a read lock cannot
	// protect map writes).
	linkedConfigsMu sync.Mutex
	linkedConfigs   map[string]linkedConfigEntry

	// pluginRODB is a lazy read-only handle to the in-memory index, used
	// exclusively by PluginRawQuery so a plugin can never mutate the index
	// or schema even if a prefix check or comment-stripping is bypassed.
	pluginRODBMu sync.Mutex
	pluginRODB   *sql.DB

	// pluginDBs holds the per-plugin SQLite store connections (#213). Each
	// plugin that exercises the plugin-db capability gets its own *sql.DB
	// pool (MaxOpenConns=1) at <vault>/.system/plugins/<id>/data/plugin.db —
	// a distinct file from the core index, never ATTACH-able to it. Opened
	// lazily by openPluginDB; closed on teardownPlugin(id), on uninstall
	// (before the folder is removed — Windows file lock), and on vault close.
	// Guarded by pluginDBsMu.
	pluginDBsMu sync.Mutex
	pluginDBs   map[string]*sql.DB

	// rateLimiter caps per-plugin PluginFetch RPS so a network-granted plugin
	// cannot hammer external services (#153). Guarded by its own internal
	// mutex; eviction happens on uninstall.
	rateLimiter *pluginRateLimiter

	// securityStats rolls up per-plugin capability denials and rate-limit
	// hits for Settings → Plugins observability (#518). Session memory only.
	securityStats *pluginSecurityStats

	// pluginSessions maps session tokens → pluginIDs for binding-identity
	// verification (#151). The loader calls RegisterPluginSession at load
	// time; privileged bindings validate the token before proceeding so a
	// plugin cannot impersonate another by passing a different pluginID. This
	// is a stepping stone — the full fix requires per-plugin isolated webviews
	// (#152), which is deferred. Guarded by pluginSessionsMu.
	pluginSessionsMu sync.RWMutex
	pluginSessions   map[string]string // token → pluginID

	// vaultMu guards the LIFECYCLE of the vault-scoped service pointers (db,
	// coordinator, watcher, tracker, vaultPath) against concurrent IPC access.
	// It does NOT guard vault content writes (theme files, settings.json) —
	// those have dedicated serializers so vaultMu.RLock holders are never
	// blocked by a content write, and vice versa (#404).
	// Wails dispatches each bound method on its own goroutine, so without this
	// a lifecycle transition (CloseVault / InitializeVault / MoveVault /
	// SwitchVault) could nil out a.db while an in-flight reader
	// (FetchPageBlocks, UpdateBlockState, …) is between its nil check and its
	// use of the pointer — a nil-deref panic (#141 review).
	//   - Lifecycle cutover sections acquire the exclusive Lock().
	//   - Reader IPC handlers acquire RLock() (defer RUnlock()) for the whole
	//     call so the pointer they checked stays valid for its duration.
	//   - Internal lowercase helpers assume the caller already holds the lock
	//     and never acquire it themselves (RLock is not reentrant; nesting it
	//     on the same goroutine would deadlock under writer contention).
	//   - Pure-delegation wrappers (PickNotebookFolder, PickLinkedNotebook,
	//     PluginMutateBlock, PluginUpdateBlockState) take no lock — their
	//     callee does — so the same goroutine never holds RLock twice.
	//
	// themeWriteMu serializes on-disk theme-file mutations (import, fork,
	// set-background) so two concurrent theme writes can't race on the
	// stat-then-write in ForkEmbeddedTheme or interleave with a re-marshal
	// in SetThemeBackgroundImage. It guards theme JSON files ONLY —
	// settings.json has its own settingsWriteMu (vault.go). A handler that
	// writes both a theme file and settings.json acquires themeWriteMu FIRST,
	// then settingsWriteMu (via UpdateSettings) — never the reverse.
	//
	// Lock ordering: vaultMu is always acquired BEFORE configMu.
	// themeWriteMu is independent of vaultMu/configMu (content write, not a
	// lifecycle or config read): a handler snapshots vaultPath/themesDir
	// under vaultMu.RLock, releases vaultMu, then acquires themeWriteMu for
	// the write — so the two are never held simultaneously.
	vaultMu      sync.RWMutex
	themeWriteMu sync.Mutex

	// closing + vaultClosingWG form the vault-close drain for IPC handlers
	// that release vaultMu mid-call. a.wg tracks every Wails-bound handler
	// for shutdown()'s a.wg.Wait(), but CloseVault/SwitchVault do a.wg.Add(1)
	// themselves (so shutdown can wait for an in-flight close/switch) — so
	// a.wg.Wait() can't be reused from inside them without self-deadlock.
	// vaultClosingWG tracks ONLY handlers that drop vaultMu before finishing
	// (today: PluginAIComplete/PluginAIEmbed, which release the lock after
	// preflight so a 60s LLM call can't hold it). The close path sets closing
	// under vaultMu.Lock, then Waits outside the lock before teardown —
	// draining those calls so a close can't strand an AI call that would
	// otherwise write a stale audit entry or leak into the next vault (#452).
	// withAIPreflight checks closing + Add(1) under one RLock hold, making
	// the gate atomic w.r.t. the close path's set+Wait (no TOCTOU window).
	// Both closing and the WG are guarded by vaultMu (closing is read/written
	// only while holding it); the WG is Add'd under RLock and Done'd with no
	// lock held.
	closing        bool
	vaultClosingWG sync.WaitGroup

	// aiStreams tracks in-flight streamed chat completions (#226). Keyed by
	// stream_id; cancel aborts the upstream HTTP request. Guarded by
	// aiStreamsMu. Entries are removed when the stream finishes or is cancelled.
	aiStreamsMu sync.Mutex
	aiStreams   map[string]*aiStreamSession

	// eventEmit, when non-nil, replaces wails Event.Emit so tests can capture
	// stream events without a live Wails runtime (#631). Production leaves
	// this nil and emit() uses wailsApp.Event.Emit.
	eventEmit func(name string, data ...any)
	// renameHooks is package-local failure injection for rename transaction
	// tests. Production leaves it nil and uses the real operations.
	renameHooks *renameHooks
	// frontmatterWriteAtomic, when non-nil, replaces parser.WriteFileAtomic
	// inside writePageFrontmatterEdit so tests can inject a mid-write failure
	// (MB-1 atomicity: turn-into must leave the file untouched on error).
	frontmatterWriteAtomic func(path string, content []byte) error
}

// aiStreamSession type lives in app_ai_stream.go (#762). App fields
// aiStreams / aiStreamsMu stay here with the rest of the App struct.

// linkedConfigEntry is one slot in App.linkedConfigs. mtime is the on-disk
// modification time of the co-located config.yaml at the moment cfg was
// parsed; a later mtime triggers a re-read.
type linkedConfigEntry struct {
	cfg   config.SystemConfig
	mtime time.Time
}

func NewApp() *App {
	return &App{
		spacesPerTab:   4,
		rateLimiter:    newPluginRateLimiter(),
		securityStats:  newPluginSecurityStats(),
		pluginSessions: make(map[string]string),
		aiModelCache:   make(map[string][]ai.AIModel),
		aiStreams:      make(map[string]*aiStreamSession),
		// keyringStore is the OS credential store for AI provider keys (#218).
		// Tests leave this nil (so the AI bindings fall back to config.yaml);
		// production wires the real OS-backed store. Reachability is probed at
		// call time, not here — a keyring can be present at build/init yet
		// unavailable at runtime (locked GNOME session, dropped D-Bus).
		keyringStore: keyring.Default(),
		// Eager MCP host so GetLocalMCP* never races lazy construction.
		mcpHost: mcp.NewHost(mcp.Options{
			Keyring: keyring.Default(),
			Version: appVersion,
		}),
	}
}

// ServiceStartup is the Wails v3 service lifecycle hook (replaces the v2
// OnStartup callback). The context is valid until just before shutdown.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	a.wailsApp = application.Get()
	a.aiCtx, a.aiCtxCancel = context.WithCancel(context.Background())
	// Front-load the one-time dictionary-cache relocation so the first
	// spellcheck action is not blocked by the copy. CacheRoot also calls it
	// lazily as a fallback; sync.Once dedupes the two paths.
	go spellcheck.MigrateDictionaryCache()
	settings, err := vault.LoadSettings()
	if err != nil && !errors.Is(err, vault.ErrSettingsFingerprintMismatch) {
		// The settings file exists on disk but is unreadable or
		// malformed. Don't silently fall through to "no vault" — the
		// user has a vault setup, something is just broken.
		a.emitOrQueue(EventVaultInitError,
			fmt.Sprintf("failed to load settings.json: %v", err))
		return nil
	}
	// F20: settings loaded fine but the trust-anchor fingerprint changed
	// since last launch (possible tampering, or a legit external edit the
	// user hasn't acknowledged yet). Surface a confirmation dialog so the
	// user can accept or reject the change. The settings are still used
	// in-memory (they are valid JSON with a valid schema).
	if errors.Is(err, vault.ErrSettingsFingerprintMismatch) {
		a.emitOrQueue(EventSettingsFingerprintMismatch, nil)
	}
	if settings.VaultPath != "" {
		if _, statErr := os.Stat(settings.VaultPath); statErr == nil {
			if initErr := a.initializeVaultServices(settings.VaultPath); initErr != nil {
				a.emitOrQueue(EventVaultInitError, initErr.Error())
			}
		}
	}
	return nil
}

// ServiceShutdown is the Wails v3 service lifecycle hook (replaces the v2
// OnShutdown callback). Called after all OnShutdown hooks, in reverse
// registration order.
func (a *App) ServiceShutdown() error {
	// Cancel in-flight AI HTTP calls so they don't outlive the process.
	if a.aiCtxCancel != nil {
		a.aiCtxCancel()
	}
	// Emit vault:closing so the frontend plugin loader runs every plugin's
	// onVaultClose/onShutdown hook (#106) before IPC tears down.
	a.emit(EventVaultClosing, struct{}{})
	// Wait for any in-flight Wails-bound calls (UpdateBlockState,
	// QueryTasks, SetLocalMCPConfig) to complete before tearing
	// down the DB, tracker, and watcher. Without this a fast window
	// close could race an in-progress file write or MCP Start.
	a.wg.Wait()
	// Close the type + monitor watchers BEFORE taking the teardown Lock: both
	// Close() join their loop goroutines, whose handlers take vaultMu, so
	// closing them under the teardown Lock deadlocks (MB-1).
	a.stopWatchersOutsideLock()
	// Take the write lock for the terminal teardown so any reader that
	// slipped in between wg.Wait() returning and this point can't
	// dereference a service mid-close — including a concurrent
	// syncMCPHost that would otherwise race Host.Start against stop.
	// MCP is stopped inside teardownVaultServices under this lock only
	// (not before), so Quit cannot leave a brief post-stop Start window.
	a.vaultMu.Lock()
	// Share the exact teardown path with CloseVault so both nil every
	// service field. Nilling here matters: if a "change vault" IPC lands
	// during OS-driven close (race), CloseVault's nothing-to-close guard
	// sees the nil'd fields and becomes a no-op instead of double-closing
	// already-closed handles.
	a.teardownVaultServices()
	a.vaultMu.Unlock()
	return nil
}

// IsVaultInitialized returns whether a workspace vault has been configured and loaded.
func (a *App) IsVaultInitialized() bool {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	return a.vaultPath != "" && a.db != nil
}

// GetAppVersion returns the Silt version (embedded from the VERSION file at
// build time). Surfaced for the About tab and plugin minSiltVersion checks.
func (a *App) GetAppVersion() string {
	return appVersion
}

// InitializeVault prompts the user for a folder, sets it up, and loads the services.
func (a *App) InitializeVault() (bool, error) {
	selectedPath, err := a.openDirectoryDialog("Select Silt Vault Directory")
	if err != nil {
		return false, fmt.Errorf("failed to select vault folder: %w", err)
	}

	if selectedPath == "" {
		return false, nil // Cancelled
	}

	if err := vault.ScaffoldVault(selectedPath); err != nil {
		return false, fmt.Errorf("failed to scaffold vault: %w", err)
	}

	// Persist settings + boot services under the write lock so a concurrent
	// reader/CloseVault sees the transition atomically (no window where
	// settings.json points at the new path but a.db is still the old one).
	a.vaultMu.Lock()
	defer a.vaultMu.Unlock()
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
