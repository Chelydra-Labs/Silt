// Package mcp implements Silt's local Model Context Protocol host (#687).
// The host runs in-process inside the Silt app, binds loopback-only HTTP
// (optional), and exposes vault content tools that call the same App paths as
// Plugin* content APIs. Stdio clients use `silt mcp` to proxy to the running
// instance. Default OFF.
package mcp

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"silt/backend/keyring"
)

// errNonLoopbackEndpoint is returned when WriteEndpointFile is given a non-loopback URL.
var errNonLoopbackEndpoint = errors.New("mcp endpoint must be loopback http(s)")

// DefaultHTTPPort is the preferred loopback port when HTTP is enabled and the
// user has not chosen another. If bind fails, the host may fall back to :0.
const DefaultHTTPPort = 17887

// EndpointFileName is written under the user config dir so `silt mcp` can
// discover a non-default loopback port without scanning.
const EndpointFileName = "mcp-endpoint.json"

// EndpointFile holds the last-known loopback MCP base URL (no secrets).
// Pid identifies the Silt process that published the endpoint so a second
// instance cannot wipe the first on bind failure.
type EndpointFile struct {
	Endpoint string `json:"endpoint"`
	Pid      int    `json:"pid,omitempty"`
}

// EndpointFilePath returns <UserConfigDir>/silt/mcp-endpoint.json.
func EndpointFilePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "silt", EndpointFileName), nil
}

// IsLoopbackEndpoint reports whether base is an http(s) URL whose host is
// loopback (127.0.0.1, ::1, localhost). Used so discovery never dials a
// non-loopback host with the bearer token.
func IsLoopbackEndpoint(base string) bool {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// WriteEndpointFile persists the loopback base URL + this process PID for
// stdio discovery. Non-loopback endpoints are rejected (defense in depth).
// Refuses to overwrite a file owned by another live process (multi-instance).
func WriteEndpointFile(endpoint string) error {
	if !IsLoopbackEndpoint(endpoint) {
		return errNonLoopbackEndpoint
	}
	path, err := EndpointFilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return withEndpointFileLock(path, func() error {
		if existing, ok := readEndpointFileRecord(path); ok {
			if peerOwnsDiscovery(existing) {
				// Another live Silt owns discovery — do not steal the slot.
				return errEndpointOwnedByOther
			}
		}
		b, err := json.Marshal(EndpointFile{
			Endpoint: strings.TrimRight(endpoint, "/"),
			Pid:      os.Getpid(),
		})
		if err != nil {
			return err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, b, 0o600); err != nil {
			return err
		}
		return os.Rename(tmp, path)
	})
}

// errEndpointOwnedByOther is returned when WriteEndpointFile would clobber a
// live peer's discovery record.
var errEndpointOwnedByOther = errors.New("mcp endpoint file owned by another live process")

// aliveCheck reports whether a PID is running. Tests may swap this.
var aliveCheck = processAlive

// healthCheck probes whether endpoint serves Silt MCP /health. Tests may swap.
var healthCheck = endpointServesSiltMCP

// peerOwnsDiscovery reports whether ef is owned by another live Silt instance.
// Requires a live foreign PID AND a successful silt-mcp health probe so a
// crashed instance whose PID was reused by an unrelated process does not
// permanently block discovery reclaim.
func peerOwnsDiscovery(ef EndpointFile) bool {
	if ef.Pid <= 0 || ef.Pid == os.Getpid() {
		return false
	}
	if !aliveCheck(ef.Pid) {
		return false
	}
	return healthCheck(ef.Endpoint)
}

// endpointServesSiltMCP GETs endpoint/health with a short timeout and requires
// the silt-mcp service marker. Used to distinguish a live peer from PID reuse.
func endpointServesSiltMCP(endpoint string) bool {
	if !IsLoopbackEndpoint(endpoint) {
		return false
	}
	base := strings.TrimRight(endpoint, "/")
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(base + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 512))
	if err != nil {
		return false
	}
	return strings.Contains(string(body), "silt-mcp")
}

// ReadEndpointFile returns the last written endpoint, or empty if missing or
// not loopback (tampered / stale file must not leak the bearer off-box).
func ReadEndpointFile() string {
	path, err := EndpointFilePath()
	if err != nil {
		return ""
	}
	ef, ok := readEndpointFileRecord(path)
	if !ok || !IsLoopbackEndpoint(ef.Endpoint) {
		return ""
	}
	return ef.Endpoint
}

// readEndpointFileRecord loads the discovery JSON (supports legacy endpoint-only files).
func readEndpointFileRecord(path string) (EndpointFile, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return EndpointFile{}, false
	}
	var ef EndpointFile
	if json.Unmarshal(b, &ef) != nil {
		return EndpointFile{}, false
	}
	ef.Endpoint = strings.TrimRight(strings.TrimSpace(ef.Endpoint), "/")
	return ef, ef.Endpoint != ""
}

// ClearEndpointFile removes the discovery file when this process owns it, or
// when the recorded owner PID is dead (stale). Never deletes a live peer's file.
func ClearEndpointFile() {
	path, err := EndpointFilePath()
	if err != nil {
		return
	}
	_ = withEndpointFileLock(path, func() error {
		if existing, ok := readEndpointFileRecord(path); ok {
			if peerOwnsDiscovery(existing) {
				return nil // leave peer discovery intact
			}
		}
		_ = os.Remove(path)
		return nil
	})
}

// clearEndpointFileForce removes the discovery file unconditionally (tests).
func clearEndpointFileForce() {
	path, err := EndpointFilePath()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// StorePinnedEndpoint writes the loopback base URL to the OS keyring so
// discovery cannot be hijacked by rewriting mcp-endpoint.json alone.
func StorePinnedEndpoint(kr keyring.Store, endpoint string) error {
	if kr == nil {
		return nil
	}
	if !IsLoopbackEndpoint(endpoint) {
		return errNonLoopbackEndpoint
	}
	return kr.Set(KeyringService, KeyringEndpointUser, strings.TrimRight(endpoint, "/"))
}

// LoadPinnedEndpoint returns the keyring-pinned loopback endpoint, or empty
// when missing, unavailable, or not loopback.
func LoadPinnedEndpoint(kr keyring.Store) string {
	if kr == nil {
		return ""
	}
	ep, err := kr.Get(KeyringService, KeyringEndpointUser)
	if err != nil || ep == "" {
		return ""
	}
	ep = strings.TrimRight(strings.TrimSpace(ep), "/")
	if !IsLoopbackEndpoint(ep) {
		return ""
	}
	return ep
}

// ClearPinnedEndpoint removes the keyring-pinned endpoint (best-effort).
func ClearPinnedEndpoint(kr keyring.Store) {
	if kr == nil {
		return
	}
	_ = kr.Delete(KeyringService, KeyringEndpointUser)
}

// ClearDiscovery drops the endpoint file (if owned/stale) and the keyring pin
// only when the file clear is allowed for this process. If a live peer owns
// the endpoint file, the pin is left alone so the peer's silt mcp discovery
// keeps working.
func ClearDiscovery(kr keyring.Store) {
	path, err := EndpointFilePath()
	ownedOrStale := true
	if err == nil {
		if existing, ok := readEndpointFileRecord(path); ok {
			if peerOwnsDiscovery(existing) {
				ownedOrStale = false
			}
		}
	}
	ClearEndpointFile()
	if ownedOrStale {
		ClearPinnedEndpoint(kr)
	}
}

// KeyringService is the OS keyring service name for the MCP bearer token.
const KeyringService = "Silt"

// KeyringUser is the keyring account for the active MCP auth token.
const KeyringUser = "mcp-local-auth-token"

// KeyringEndpointUser is the keyring account for the pinned loopback MCP base
// URL. silt mcp prefers this over mcp-endpoint.json so a same-user process
// that only rewrites the discovery file cannot redirect the bearer.
const KeyringEndpointUser = "mcp-local-endpoint"

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
