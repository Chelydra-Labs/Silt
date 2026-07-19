package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"silt/backend/db"
	"silt/backend/mcp"
	"silt/backend/parser"
)

// mcpBridge adapts *App to mcp.Bridge so tools call the same content paths
// as the UI / Plugin* APIs (single vault writer).
//
// vaultPath is a snapshot taken while the caller already holds vaultMu
// (R or W). VaultPath() must not take vaultMu — Host.Start is invoked from
// syncMCPHostLocked under exclusive vaultMu.Lock, and a nested RLock deadlocks.
type mcpBridge struct {
	app       *App
	vaultPath string
}

func (b mcpBridge) VaultPath() string {
	return b.vaultPath
}

func (b mcpBridge) SearchBlocksPaged(ctx context.Context, query string, offset, limit int, filters db.SearchFilters) (parser.SearchResult, error) {
	_ = ctx
	return b.app.SearchBlocksPaged(query, offset, limit, filters)
}

func (b mcpBridge) FetchPageBlocks(ctx context.Context, notebook, section, page string) ([]parser.ParsedBlock, error) {
	_ = ctx
	return b.app.FetchPageBlocks(notebook, section, page)
}

func (b mcpBridge) FetchPageMarkdown(ctx context.Context, notebook, section, page string) (string, error) {
	_ = ctx
	return b.app.FetchPageMarkdown(notebook, section, page)
}

func (b mcpBridge) ListNavigation(ctx context.Context) (parser.NavigationTree, error) {
	_ = ctx
	return b.app.ListNavigation()
}

func (b mcpBridge) CreatePage(ctx context.Context, notebook, section, page, dateStr string) (string, error) {
	_ = ctx
	return b.app.CreatePage(notebook, section, page, dateStr)
}

func (b mcpBridge) UpdateBlocks(ctx context.Context, notebook, section, page string, blocks []parser.ParsedBlock) error {
	_ = ctx
	return b.app.SaveFileBlocks(notebook, section, page, blocks)
}

func (b mcpBridge) PageExists(ctx context.Context, notebook, section, page string) (bool, error) {
	_ = ctx
	// Resolve the same path SaveFileBlocks would use without creating it.
	b.app.vaultMu.RLock()
	defer b.app.vaultMu.RUnlock()
	if b.app.db == nil || b.app.vaultPath == "" {
		return false, fmt.Errorf("no vault open")
	}
	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return false, fmt.Errorf("invalid path")
	}
	source := b.app.resolveSourceByName(safeNotebook)
	notebookDir, err := b.app.resolveNotebookDir(safeNotebook, source)
	if err != nil {
		return false, err
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return false, fmt.Errorf("path escapes notebook root")
	}
	_, err = os.Stat(filePath)
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// ensureMCPHost returns the MCP host, constructing it once if missing
// (tests that build a bare App). Serialized so concurrent GetLocalMCP*
// getters cannot race the write against syncMCPHostLocked.
func (a *App) ensureMCPHost() *mcp.Host {
	a.mcpHostMu.Lock()
	defer a.mcpHostMu.Unlock()
	if a.mcpHost != nil {
		return a.mcpHost
	}
	a.mcpHost = mcp.NewHost(mcp.Options{
		Keyring: a.keyringStore,
		Version: appVersion,
	})
	return a.mcpHost
}

// syncMCPHost starts or stops the MCP host from the current vault + config.
// Takes vaultMu.RLock. Do not call while holding vaultMu.Lock — use
// syncMCPHostLocked from initializeVaultServices instead.
func (a *App) syncMCPHost() {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.syncMCPHostLocked()
}

// syncMCPHostLocked is the body of syncMCPHost when the caller already holds
// vaultMu (R or exclusive Lock). initializeVaultServices runs under exclusive
// Lock — the bridge must snapshot vaultPath so Host.Start never re-enters
// vaultMu via Bridge.VaultPath().
func (a *App) syncMCPHostLocked() {
	h := a.ensureMCPHost()
	a.configMu.RLock()
	cfg := a.cfg.AI.LocalMCP
	a.configMu.RUnlock()

	// Caller holds vaultMu — read fields directly (no nested RLock).
	hasVault := a.vaultPath != "" && a.db != nil
	vaultSnap := a.vaultPath

	if !cfg.Enabled || !hasVault {
		h.Stop()
		// Re-apply status semantics without starting.
		_ = h.Start(nil, cfg)
		return
	}
	bridge := mcpBridge{app: a, vaultPath: vaultSnap}
	if err := h.Start(bridge, cfg); err != nil {
		log.Printf("mcp: start failed: %v", err)
	}
}

// stopMCPHost is called from teardown / ServiceShutdown.
func (a *App) stopMCPHost() {
	if a.mcpHost != nil {
		a.mcpHost.Stop()
	}
}

// GetLocalMCPStatus returns availability for Settings UI.
func (a *App) GetLocalMCPStatus() mcp.Status {
	h := a.ensureMCPHost()
	return h.Status()
}

// GetLocalMCPConfig returns the vault-scoped local MCP config (no secrets).
func (a *App) GetLocalMCPConfig() mcp.Config {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	return a.cfg.AI.LocalMCP
}

// SetLocalMCPConfig patches and persists local MCP settings, then resyncs the host.
// Persistence goes through saveConfigTracked (same as other atomic setters) so
// the config watcher suppresses self-writes and concurrent setters cannot
// clobber this patch via a raw config.Save race.
func (a *App) SetLocalMCPConfig(enabled, httpEnabled, writeEnabled bool, httpPort int) error {
	a.wg.Add(1)
	defer a.wg.Done()

	a.vaultMu.RLock()
	if a.vaultPath == "" {
		a.vaultMu.RUnlock()
		return fmt.Errorf("no vault open")
	}

	a.configMu.Lock()
	local := mcp.NormalizeConfig(mcp.Config{
		Enabled:      enabled,
		HTTPEnabled:  httpEnabled,
		HTTPPort:     httpPort,
		WriteEnabled: writeEnabled,
	})
	// Prefer HTTP on so the stdio proxy can dial loopback out of the box.
	if enabled && !local.HTTPEnabled {
		local.HTTPEnabled = true
		local = mcp.NormalizeConfig(local)
	}
	a.cfg.AI.LocalMCP = local
	saveErr := a.saveConfigTracked(a.cfg)
	a.configMu.Unlock()
	a.vaultMu.RUnlock()
	if saveErr != nil {
		return saveErr
	}
	// After locks released — sync takes vaultMu/configMu internally.
	a.syncMCPHost()
	return nil
}

// GetLocalMCPToken returns the bearer token for local clients (Settings copy).
// Empty when MCP has never been started. Never log the return value.
func (a *App) GetLocalMCPToken() string {
	h := a.ensureMCPHost()
	tok := h.Token()
	if tok != "" {
		return tok
	}
	// Fall back to keyring so the UI can show the token before first Start.
	if a.keyringStore != nil {
		if t, err := a.keyringStore.Get(mcp.KeyringService, mcp.KeyringUser); err == nil {
			return t
		}
	}
	return ""
}

// GetLocalMCPInstallHint returns a short install snippet for clients (no token).
func (a *App) GetLocalMCPInstallHint() string {
	st := a.GetLocalMCPStatus()
	ep := st.Endpoint
	if ep == "" {
		ep = fmt.Sprintf("http://127.0.0.1:%d", mcp.DefaultHTTPPort)
	}
	return fmt.Sprintf(`# OpenCode (opencode.json)
{
  "mcp": {
    "silt": {
      "type": "local",
      "command": ["silt", "mcp"],
      "enabled": true
    }
  }
}

# Or Streamable HTTP (Bearer token from OS keyring / Settings):
# URL: %s
# Authorization: Bearer <token from Silt Settings → AI → Local MCP>
`, ep)
}
