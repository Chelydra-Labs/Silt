// Package mcp implements Silt's local Model Context Protocol host (#687).
// The host runs in-process inside the Silt app, binds loopback-only HTTP
// (optional), and exposes vault content tools that call the same App paths as
// Plugin* content APIs. Stdio clients use `silt mcp` to proxy to the running
// instance. Default OFF.
package mcp

// DefaultHTTPPort is the preferred loopback port when HTTP is enabled and the
// user has not chosen another. If bind fails, the host may fall back to :0.
const DefaultHTTPPort = 17887

// KeyringService is the OS keyring service name for the MCP bearer token.
const KeyringService = "Silt"

// KeyringUser is the keyring account for the active MCP auth token.
const KeyringUser = "mcp-local-auth-token"

// Config is the vault-scoped local AI / MCP integration block (config.yaml
// under ai.local_mcp). Defaults are all off / read-only.
type Config struct {
	// Enabled master switch. When false the host never starts.
	Enabled bool `yaml:"enabled" json:"enabled"`
	// HTTPEnabled starts the loopback Streamable HTTP transport.
	// Stdio clients still work via `silt mcp` when the host is running.
	HTTPEnabled bool `yaml:"http_enabled" json:"http_enabled"`
	// HTTPPort is the loopback TCP port (127.0.0.1 only). 0 = ephemeral.
	// Default DefaultHTTPPort when HTTPEnabled and unset (0 with enabled).
	HTTPPort int `yaml:"http_port" json:"http_port"`
	// WriteEnabled grants create_page / update_blocks. Read tools are always
	// available when the host is running. Default false.
	WriteEnabled bool `yaml:"write_enabled" json:"write_enabled"`
}

// NormalizeConfig clamps port and applies safe defaults. Does not force
// Enabled — that stays user-controlled.
func NormalizeConfig(c Config) Config {
	if c.HTTPPort < 0 {
		c.HTTPPort = 0
	}
	if c.HTTPPort > 65535 {
		c.HTTPPort = DefaultHTTPPort
	}
	if c.HTTPEnabled && c.HTTPPort == 0 {
		// Prefer a stable default so client configs can hardcode the URL;
		// bind failure is handled at start time with an actionable error.
		c.HTTPPort = DefaultHTTPPort
	}
	return c
}

// Status is the frontend-facing availability snapshot.
type Status struct {
	// State: "disabled" | "no_vault" | "starting" | "running" | "error"
	State string `json:"state"`
	// Message is a short human-readable detail (error text or endpoint).
	Message string `json:"message,omitempty"`
	// Endpoint is the loopback base URL when HTTP is listening (e.g. http://127.0.0.1:17887).
	Endpoint string `json:"endpoint,omitempty"`
	// WriteEnabled mirrors the active config grant.
	WriteEnabled bool `json:"write_enabled"`
	// VaultPath is the active vault root (empty when no vault).
	VaultPath string `json:"vault_path,omitempty"`
}
