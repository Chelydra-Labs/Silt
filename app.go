package main

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"silt/backend/ai"
	"silt/backend/config"
	"silt/backend/core"
	"silt/backend/db"
	"silt/backend/keyring"
	"silt/backend/monitor"
	"silt/backend/parser"
	"silt/backend/templates"
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
	Name    string
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
}

// aiStreamSession is one in-flight PluginAIComplete(stream=true) call.
// ready is closed when the frontend has attached Events.On listeners
// (PluginAIStreamReady) so terminal events are not lost to a race.
type aiStreamSession struct {
	pluginID  string
	cancel    context.CancelFunc
	ready     chan struct{}
	readyOnce sync.Once
}

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
	}
}

// ServiceStartup is the Wails v3 service lifecycle hook (replaces the v2
// OnStartup callback). The context is valid until just before shutdown.
func (a *App) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	a.ctx = ctx
	a.wailsApp = application.Get()
	a.aiCtx, a.aiCtxCancel = context.WithCancel(context.Background())
	settings, err := vault.LoadSettings()
	if err != nil && !errors.Is(err, vault.ErrSettingsFingerprintMismatch) {
		// The settings file exists on disk but is unreadable or
		// malformed. Don't silently fall through to "no vault" — the
		// user has a vault setup, something is just broken.
		a.emitOrQueue("vault:init-error",
			fmt.Sprintf("failed to load settings.json: %v", err))
		return nil
	}
	// F20: settings loaded fine but the trust-anchor fingerprint changed
	// since last launch (possible tampering, or a legit external edit the
	// user hasn't acknowledged yet). Surface a confirmation dialog so the
	// user can accept or reject the change. The settings are still used
	// in-memory (they are valid JSON with a valid schema).
	if errors.Is(err, vault.ErrSettingsFingerprintMismatch) {
		a.emitOrQueue("settings:fingerprint-mismatch", nil)
	}
	if settings.VaultPath != "" {
		if _, statErr := os.Stat(settings.VaultPath); statErr == nil {
			if initErr := a.initializeVaultServices(settings.VaultPath); initErr != nil {
				a.emitOrQueue("vault:init-error", initErr.Error())
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
	a.emit("vault:closing", struct{}{})
	// Wait for any in-flight Wails-bound calls (UpdateBlockState,
	// QueryTasks) to complete before tearing
	// down the DB, tracker, and watcher. Without this a fast window
	// close could race an in-progress file write.
	a.wg.Wait()
	// Take the write lock for the terminal teardown so any reader that
	// slipped in between wg.Wait() returning and this point can't
	// dereference a service mid-close. (No new handlers arrive after the
	// Wails context is cancelled, but the lock makes the guarantee
	// structural rather than relying on dispatch ordering.)
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

// teardownVaultServices closes and nils every vault-scoped service in the
// reverse order of initializeVaultServices. Shared by shutdown (app exit)
// and CloseVault (workspace switch) so the two paths can't drift. Safe to
// call when services are already nil (each close is guarded).
func (a *App) teardownVaultServices() {
	// Stop the audit writers FIRST so they drain queued entries for the closing
	// vault before any service they depend on (just vaultPath at this point)
	// goes away. After this returns, every enqueued audit write is on disk.
	stopNetworkAuditWriter()
	stopAIAuditWriter()
	// Clear the in-memory audit slices so a subsequent vault open seeds from
	// the new vault's on-disk logs, not the closed vault's leftover entries.
	// Without this, the seed guard (len == 0) would skip reseeding and the
	// new vault would display the old vault's audit history (#446 hardening).
	networkAuditMu.Lock()
	networkAudit = nil
	networkAuditMu.Unlock()
	aiAuditMu.Lock()
	aiAudit = nil
	aiAuditMu.Unlock()
	if a.watcher != nil {
		// Drop every focus lease before tearing the watcher down so a clean
		// exit can't strand a file under fsnotify suppression (#38).
		a.watcher.ReleaseAllFocus()
		_ = a.watcher.Close()
		a.watcher = nil
	}
	if a.templateWatcher != nil {
		_ = a.templateWatcher.Close()
		a.templateWatcher = nil
	}
	if a.configWatcher != nil {
		_ = a.configWatcher.Close()
		a.configWatcher = nil
	}
	if a.tracker != nil {
		a.tracker.Stop()
		a.tracker = nil
	}
	// Close the read-only plugin handle too (it points at the closing index).
	a.pluginRODBMu.Lock()
	if a.pluginRODB != nil {
		_ = a.pluginRODB.Close()
		a.pluginRODB = nil
	}
	a.pluginRODBMu.Unlock()
	// Close every per-plugin DB pool (#213). These point at files under the
	// closing vault's .system/plugins/<id>/data/, so they must be released
	// before the vault path goes away (and before any folder removal on a
	// vault move — Windows file lock).
	a.closeAllPluginDBs()
	if a.db != nil {
		// Close runs PRAGMA wal_checkpoint(TRUNCATE) so the WAL is merged
		// into the main index file on a clean close (#29).
		_ = a.db.Close()
		a.db = nil
	}
	a.coordinator = nil
	a.vaultPath = ""
	// Drop Dev Mode menu enablement unless SILT_DEBUG keeps it on (#684).
	a.syncOpenDevToolsMenuItemEnabled(strings.EqualFold(os.Getenv("SILT_DEBUG"), "1"))
	// Session security aggregates are vault-scoped in practice; clear on
	// vault close so the next vault doesn't inherit denial badges (#518).
	if a.securityStats != nil {
		a.securityStats.clear()
	}
	// F4: clear the per-host grants store so a subsequent vault open starts
	// fresh (LoadGrants + seedFirstPartyGrants repopulate). The on-disk file
	// is untouched — it persists across vault sessions.
	a.configMu.Lock()
	a.grants = nil
	a.quarantinedLinks = nil
	a.configMu.Unlock()
	templates.ResetPluginRegistry()
}

// CloseVault tears down the active vault's services in the reverse order of
// initializeVaultServices (via the shared teardownVaultServices helper).
// After it returns, IsVaultInitialized is false so the UI re-shows the
// onboarding screen. It does NOT clear the saved settings.json path — the
// user can re-open the same vault via InitializeVault / a new selection.
// Idempotent: safe to call when no vault is open. DRAINS in-flight AI calls
// (PluginAIComplete/PluginAIEmbed) before teardown via the closing flag +
// vaultClosingWG, so a close can't strand an AI call that would otherwise
// append a stale audit entry or write into the next vault (#452). The drain
// is distinct from a.wg (which tracks every handler for shutdown) because
// CloseVault does a.wg.Add(1) itself, so a.wg.Wait() here would self-deadlock.
func (a *App) CloseVault() error {
	a.wg.Add(1)
	defer a.wg.Done()

	// Fast nil-check under the lock so the "nothing to close" decision can't
	// race a concurrent Initialize.
	a.vaultMu.Lock()
	if a.vaultPath == "" && a.db == nil {
		a.vaultMu.Unlock()
		return nil
	}
	// Set the closing flag under the exclusive lock so withAIPreflight's
	// RLock-hold check+Add becomes atomic w.r.t. this set. New AI calls now
	// reject (errVaultClosing) before issuing HTTP; calls already past
	// preflight are tracked in vaultClosingWG and drained next. Snapshot the
	// cancel func + vaultPath under the same hold so the reads after the
	// Unlock are correct-by-construction (not reliant on a cross-goroutine
	// happens-before argument that a future caller could break).
	a.closing = true
	cancel := a.vaultCtxCancel
	vp := a.vaultPath
	a.vaultMu.Unlock()

	// Cancel the vault-scoped AI context OUTSIDE the lock so every in-flight
	// HTTP call observes context.Canceled in milliseconds — the HTTP client
	// aborts the request, the call returns, vaultClosingWG.Done() fires, and
	// the Wait() below unblocks promptly. Without this the drain blocks for
	// up to the provider timeout (~60s on a slow local model) with no UI
	// feedback, risking a force-quit mid-teardown (#471). The closing flag
	// (set above) still rejects NEW calls; vaultCtx is re-created on the
	// next initializeVaultServices, so cancelling it here is safe.
	if cancel != nil {
		cancel()
	} else if vp != "" {
		// vaultPath is set but vaultCtxCancel is nil — the vault was opened
		// without going through initializeVaultServices (every production
		// path does). The drain below will fall back to the provider-timeout
		// bound (the pre-#471 behavior). Log so a future code path that
		// bypasses the initializer surfaces immediately rather than silently
		// regressing the close latency.
		log.Printf("CloseVault: vaultCtxCancel is nil with an open vault (%s) — vault-scoped AI cancellation skipped (initializeVaultServices did not run for this vault)", vp)
	}

	// Drain in-flight AI calls OUTSIDE the lock. They released vaultMu after
	// preflight (the HTTP call doesn't hold it), so the lock can't serialize
	// them — and holding it across the (now short) completion would block
	// every reader IPC. With vaultCtx cancelled above, the wait is bounded
	// by the HTTP client's context-observe latency (milliseconds), not the
	// provider timeout.
	a.vaultClosingWG.Wait()

	// Emit vault:closing BEFORE teardown so the frontend plugin loader can run
	// every plugin's onVaultClose hook (#106) while IPC is still live. The
	// event is best-effort: if no frontend is mounted (e.g. headless test),
	// the emit is a no-op (a.emit guards wailsApp == nil internally).
	a.emit("vault:closing", struct{}{})
	// Hold the write lock across the teardown so concurrent readers can't
	// dereference a service pointer mid-close.
	a.vaultMu.Lock()
	a.teardownVaultServices()
	a.closing = false
	a.vaultMu.Unlock()
	return nil
}

func (a *App) initializeVaultServices(vaultPath string) error {
	// Caller (InitializeVault / SwitchVault / rollbackMove) holds vaultMu.Lock.
	// Reopen the vault for AI calls: a prior CloseVault/SwitchVault set
	// closing=true and would have reset it on teardown, but a reinit reaching
	// here after a failed/partial close must not inherit a stuck flag, or AI
	// calls would reject forever (#452).
	a.closing = false
	// (Re)create the vault-scoped AI context. Cancel any PRIOR context first:
	// CloseVault/SwitchVault cancel proactively, but MoveVault/rollbackMove
	// re-enter here via teardownVaultServices → initializeVaultServices WITHOUT
	// cancelling (teardown doesn't touch vaultCtx), so without this guard each
	// vault move / failed-move rollback would orphan the old context in
	// aiCtx.children until shutdown. aiCtx may be nil for a bare App used only
	// in unit tests that bypass startup() — fall back to context.Background()
	// in that case so direct initializeVaultServices callers
	// (app_lifecycle_drain_test.go) still get a cancellable per-vault context.
	parent := a.aiCtx
	if parent == nil {
		parent = context.Background()
	}
	if a.vaultCtxCancel != nil {
		a.vaultCtxCancel()
	}
	a.vaultCtx, a.vaultCtxCancel = context.WithCancel(parent)
	// Load system config first: its editor.tab_indent_spaces drives
	// ScanWorkspace and every subsequent parse, so it must be applied before
	// the initial index is built. A missing/invalid config is non-fatal —
	// defaults keep the vault usable — but a parse error is surfaced.
	cfg, cfgErr := config.Load(vaultPath)
	if cfgErr != nil {
		a.emit("config:error", cfgErr.Error())
	}
	// F4: load the per-host grants store BEFORE applyConfigLocked so the
	// first-party seed merges into the real store, not a transient empty one.
	// Grants live in <configDir>/silt/grants.json (NOT vault-scoped config.yaml)
	// so a synced vault cannot carry the counterpart's grant decisions.
	grantsStore, grantsErr := vault.LoadGrants()
	if grantsErr != nil {
		// A corrupt grants file is non-fatal — log + start with an empty
		// store. The user re-grants on first use (the safe default). Every
		// third-party plugin will prompt; first-party plugins seed regardless.
		log.Printf("initializeVaultServices: grants load failed (starting with empty store): %v", grantsErr)
		grantsStore = vault.GrantsStore{}
	}
	a.configMu.Lock()
	a.grants = grantsStore
	a.configMu.Unlock()
	a.applyConfigLocked(cfg) // sets a.cfg + a.spacesPerTab + seeds first-party grants into a.grants
	// The config:error event above fires before the frontend mounts and
	// subscribes, so it is typically lost. Stash the error for
	// GetConfigLoadError() to surface on the frontend's initial loadConfig().
	a.configMu.Lock()
	a.configLoadErr = cfgErr
	a.configMu.Unlock()

	// F4 migration: if the vault's config.yaml still carries a legacy
	// `plugins.grants:` block AND the host store was empty before we seeded
	// first-party grants, this is a pre-F4 vault opening on a host that has
	// never seen it. Emit grants:migration-required so the frontend shows a
	// one-time confirmation dialog. The user's confirm calls
	// ConfirmGrantsMigration, which writes the legacy grants to the host file
	// and rewrites config.yaml without the grants block. If the user denies,
	// the host store stays seeded with first-party only; every third-party
	// plugin re-prompts on first use (the safe default).
	if len(grantsStore) == 0 && grantsErr == nil {
		legacy := vault.LoadLegacyVaultGrants(vaultPath)
		// Strip first-party entries — they are always seeded implicitly, never
		// migrated (the user never granted them manually).
		hasThirdParty := false
		for pid := range legacy {
			if !isFirstPartyPlugin(pid) {
				hasThirdParty = true
				break
			}
		}
		if hasThirdParty {
			a.emitOrQueue("grants:migration-required", legacy)
		}
	}

	// #218: move any plaintext AI provider keys into the OS keyring on first
	// run after upgrade. Best-effort + idempotent — if the keyring is off or
	// unavailable, plaintext keys are left in config (the documented fallback).
	// Runs AFTER applyConfigLocked so a.cfg is populated. Keyring writes run
	// UNDER the caller's vaultMu hold (locked variant) so a concurrent
	// SwitchVault/MoveVault cutover cannot retarget the write to another
	// vault (#654). Caller holds vaultMu.Lock — use the locked variant
	// (Lock→RLock deadlocks).
	//
	// Path-scoped keyring user ids need the target vault path before migrate
	// (teardown cleared a.vaultPath; the final assignment below is after the
	// watcher starts). Set it here so migrate does not hash an empty path.
	a.vaultPath = vaultPath
	// vaultMu held exclusively — pass enablement without re-locking (#684).
	devToolsMenuOn := strings.EqualFold(os.Getenv("SILT_DEBUG"), "1")
	if !devToolsMenuOn {
		a.configMu.RLock()
		devToolsMenuOn = a.cfg.UI.OpenDevtoolsOnStartup != nil && *a.cfg.UI.OpenDevtoolsOnStartup
		a.configMu.RUnlock()
	}
	a.syncOpenDevToolsMenuItemEnabled(devToolsMenuOn)
	a.migrateAIKeysToKeyringLocked()

	// F3: verify linked-notebook fingerprints before the vault scan. Legacy
	// links (pre-F3, no fingerprint) get one assigned silently; mismatched
	// links are quarantined (excluded from indexing/reads/writes) and emit
	// linked-notebook:quarantined so the frontend shows a re-link prompt.
	a.configMu.Lock()
	a.quarantinedLinks = make(map[string]struct{})
	a.configMu.Unlock()
	a.verifyLinkedNotebookFingerprints()

	// Persistent on-disk WAL index at <vault>/.system/index.sqlite. Survives
	// restarts so a warm launch re-indexes only changed files (#29). Markdown
	// remains the source of truth; deleting the 3 index files forces a clean
	// full rebuild. The .system dir is created by ScaffoldVault.
	systemDir := filepath.Join(vaultPath, ".system")
	if err := os.MkdirAll(systemDir, 0o700); err != nil {
		return fmt.Errorf("failed to ensure .system dir: %w", err)
	}
	indexPath := filepath.Join(systemDir, "index.sqlite")
	dbMgr, err := db.NewDatabaseManager(indexPath)
	if err != nil {
		return fmt.Errorf("failed to start database: %w", err)
	}

	// SQLDB() only at vault open: handle is live and single-threaded here.
	// Query/write paths must use DatabaseManager package methods (handle/
	// withDB) so Close returns ErrDBClosed instead of nil-derefing.
	coord := core.NewExecutionCoordinator(dbMgr.SQLDB())
	tracker := monitor.NewWriteTracker()

	// Migrate old per-day file model: <page>/<date>.md → <page>.md.
	// Runs before the scan so the indexer sees the new model. Idempotent.
	migrationWarnings := vault.MigratePerDayFiles(vaultPath, a.spacesPerTab)

	results, walkWarnings, err := parser.ScanWorkspace(vaultPath, a.spacesPerTab)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to scan workspace: %w", err)
	}

	// Append the standalone-tasks file (<vault>/.silt/tasks.md) if it exists.
	// WalkMarkdown skips dot-directories so this targeted read is the only
	// way the file enters the index (#368). parseSingleFile derives
	// notebook=".silt" from the path; ListNavigation hides dot-prefixed
	// notebooks so it never surfaces in the page browser.
	results = append(results, parser.ScanStandaloneTasks(vaultPath, a.spacesPerTab)...)

	// Incremental re-index: keep only files whose mtime+size differ from the
	// last recorded index (or that were never indexed). On a cold start (no
	// index file yet) every file is "changed" and gets a full index. Pruning
	// stale `files` rows for paths no longer on disk handles deletes/renames.
	var changed []parser.ScanResult
	var seenPaths []string
	for _, res := range results {
		seenPaths = append(seenPaths, res.Path)
		if res.Err != nil || res.Notebook == "" {
			// Unreadable or unresolvable files are forwarded to the indexer so
			// they appear in the skipped list; they do not get a files row.
			changed = append(changed, res)
			continue
		}
		unchanged, uerr := dbMgr.IsFileUnchanged(res.Path, res.MTime.UnixNano(), res.Size)
		if uerr != nil {
			log.Printf("initializeVaultServices: IsFileUnchanged(%s): %v", res.Path, uerr)
			changed = append(changed, res)
			continue
		}
		if unchanged {
			continue
		}
		changed = append(changed, res)
	}

	// indexedCount = files that passed metadata validation and were actually
	// written to the index (NOT len(changed); errored/unresolvable files in
	// `changed` are reported in `skipped` and excluded from this count). Used
	// below to decide whether a post-index WAL checkpoint is worth running.
	indexedCount, skipped, err := dbMgr.IndexScanResults(changed)
	if err != nil {
		_ = dbMgr.Close()
		return fmt.Errorf("failed to index scan results: %w", err)
	}

	// Record the freshly-indexed files' stats and prune paths that vanished
	// since the last run (rename/delete). Only files that were actually
	// indexed (valid metadata, no scan error) get a files row — a file that
	// failed to parse shouldn't be marked "unchanged" next time.
	var allWarnings []string
	for _, res := range changed {
		if res.Err != nil {
			allWarnings = append(allWarnings, fmt.Sprintf("%s: %v", res.Path, res.Err))
			continue
		}
		if res.Notebook == "" {
			for _, w := range res.Warnings {
				allWarnings = append(allWarnings, fmt.Sprintf("%s: %s", res.Path, w))
			}
			if len(res.Warnings) == 0 {
				allWarnings = append(allWarnings, fmt.Sprintf("%s: missing notebook/section/page", res.Path))
			}
			continue
		}
		if res.MTime.IsZero() {
			// No stat → can't record a skip key; leave it to be re-parsed
			// next time rather than risk a false "unchanged".
			continue
		}
		if err := dbMgr.MarkFileIndexed(nil, res.Path, res.MTime.UnixNano(), res.Size); err != nil {
			log.Printf("initializeVaultServices: MarkFileIndexed(%s): %v", res.Path, err)
		}
	}
	pruned, pruneErr := dbMgr.PruneStaleFiles(seenPaths)
	if pruneErr != nil {
		log.Printf("initializeVaultServices: PruneStaleFiles: %v", pruneErr)
	}
	for _, p := range pruned {
		allWarnings = append(allWarnings, fmt.Sprintf("%s: removed from index (file no longer exists)", p))
	}

	// Merge the indexer's per-file skip list into the warning stream.
	allWarnings = append(allWarnings, skipped...)
	// Surface walk-level warnings (symlink skips, permission errors) from #32.
	allWarnings = append(allWarnings, walkWarnings...)
	allWarnings = append(allWarnings, migrationWarnings...)

	if indexedCount > 0 {
		// A checkpoint after the bulk insert keeps the WAL bounded for the
		// session. No-op on in-memory.
		if err := dbMgr.Checkpoint(); err != nil {
			log.Printf("initializeVaultServices: post-index checkpoint: %v", err)
		}
	}
	if len(allWarnings) > 0 {
		a.emitOrQueue("vault:init-warnings", allWarnings)
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

	// Route co-located per-notebook config edits to the cache invalidator +
	// linked-config:changed event (#133). The handler is called from the
	// watcher goroutine; it only touches configMu + the event emitter.
	watcher.SetLinkedConfigHandler(a.onLinkedConfigChange)
	// Route mass-re-mint detections to the index:re-mint-warning event (#443).
	// The handler is called from the watcher goroutine; it only emits a Wails
	// event (safe — no vaultMu/configMu access).
	watcher.SetReMintWarningHandler(a.onReMintWarning)

	// Start hot-reload of .system/config.yaml. External edits re-parse and
	// emit config:changed without a restart (SPECS.md §9.2). Silt's own
	// SaveSystemWrite is ignored via the watcher's self-loop tracker.
	if a.ctx != nil {
		cw, wErr := config.NewConfigWatcher(vaultPath,
			func(reloaded config.SystemConfig) { a.applyConfig(reloaded) },
			func(e error) { a.emit("config:error", e.Error()) })
		if wErr != nil {
			log.Printf("config watcher disabled: %v", wErr)
		} else {
			cw.Start()
			a.configWatcher = cw
		}
	}

	// Start hot-reload of .system/templates/ so the picker stays live when a
	// user adds/edits/deletes a custom template externally (the same posture
	// as the config and theme watchers). The onChange callback invalidates the
	// cache and emits templates:changed; the frontend store re-lists.
	if a.ctx != nil {
		tw, tErr := templates.NewTemplateWatcher(a.templatesDir(), func() {
			templates.InvalidateTemplateCache()
			a.emit("templates:changed", struct{}{})
		})
		if tErr != nil {
			log.Printf("template watcher disabled: %v", tErr)
		} else {
			tw.Start()
			a.templateWatcher = tw
		}
	}

	// Seed the in-memory network + AI audit logs from the on-disk per-plugin
	// log files (network.log / ai.log) so entries survive a restart (#157 /
	// #446). The writers are started AFTER seeding so they never race the
	// seed (#235).
	seedNetworkAuditFromDisk(vaultPath)
	startNetworkAuditWriter(vaultPath)
	seedAIAuditFromDisk(vaultPath)
	startAIAuditWriter(vaultPath)

	// Report any paths the watcher could not subscribe to (fsnotify
	// limits, permissions, etc.) so the UI can inform the user.
	if failed := watcher.FailedPaths(); len(failed) > 0 {
		a.emitOrQueue("vault:watch-coverage", failed)
	}

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
