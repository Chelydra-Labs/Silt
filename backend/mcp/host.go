package mcp

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"silt/backend/keyring"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Host is the in-process MCP server lifecycle manager.
//
// Lock order: startMu outside mu. startMu serializes the full Start/stop
// side-effect sequence (token, auditor, bind, Shutdown). mu protects short
// status/config snapshots. Never acquire startMu while holding mu.
type Host struct {
	startMu  sync.Mutex // Start/Stop lifecycle (outside mu)
	mu       sync.RWMutex
	cfg      Config
	bridge   Bridge
	keyring  keyring.Store
	token    string
	server   *mcpsdk.Server
	httpSrv  *http.Server
	listener net.Listener
	endpoint string
	state    string // disabled|no_vault|starting|running|error
	errMsg   string
	audit    Auditor
	version  string
}

// Options configures a new Host.
type Options struct {
	Keyring keyring.Store
	// Version is reported in the MCP Implementation metadata.
	Version string
	// Auditor overrides the default file auditor (tests).
	Auditor Auditor
}

// NewHost creates a stopped host. Call Start when enabled + vault open.
func NewHost(opts Options) *Host {
	kr := opts.Keyring
	if kr == nil {
		kr = keyring.Default()
	}
	ver := opts.Version
	if ver == "" {
		ver = "dev"
	}
	h := &Host{
		keyring: kr,
		version: ver,
		state:   "disabled",
		audit:   opts.Auditor,
	}
	return h
}

// Status returns a snapshot for the Settings UI.
func (h *Host) Status() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	st := Status{
		State:        h.state,
		Message:      h.errMsg,
		Endpoint:     h.endpoint,
		WriteEnabled: h.cfg.WriteEnabled,
	}
	if h.bridge != nil {
		st.VaultPath = h.bridge.VaultPath()
	}
	if h.state == "running" && h.endpoint != "" {
		st.Message = "MCP listening on " + h.endpoint
	}
	return st
}

// Config returns the active config snapshot.
func (h *Host) Config() Config {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cfg
}

// canSkipRestartLocked reports whether Start can apply cfg without tearing
// down the HTTP listener. Caller must hold h.mu.
func (h *Host) canSkipRestartLocked(bridge Bridge, cfg Config) bool {
	if h.state != "running" || h.listener == nil {
		return false
	}
	if !cfg.Enabled || bridge == nil || bridge.VaultPath() == "" {
		return false
	}
	// Transport identity must match; write grant may differ.
	if h.cfg.Enabled != cfg.Enabled ||
		h.cfg.HTTPEnabled != cfg.HTTPEnabled ||
		h.cfg.HTTPPort != cfg.HTTPPort {
		return false
	}
	if h.bridge == nil || h.bridge.VaultPath() != bridge.VaultPath() {
		return false
	}
	return true
}

// Token returns the bearer token (empty if none). Never log this.
func (h *Host) Token() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.token
}

// Endpoint returns the loopback base URL when HTTP is up.
func (h *Host) Endpoint() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.endpoint
}

// Start brings up the MCP host for the given vault bridge and config.
// Idempotent restart: stop then start. Endpoint discovery file is kept across
// mid-restart so `silt mcp` does not lose the port during vault switch; it is
// rewritten on successful HTTP bind and cleared on final stop / disable / bind failure.
//
// Pure write-grant (or bridge pointer) toggles while already running skip the
// HTTP drain/rebind — toolEnv reads cfg via Config() on each call.
//
// Concurrent Start/Stop are serialized on startMu so bind/Shutdown cannot
// interleave mid-sequence.
func (h *Host) Start(bridge Bridge, cfg Config) error {
	cfg = NormalizeConfig(cfg)

	h.startMu.Lock()
	defer h.startMu.Unlock()

	h.mu.Lock()
	if h.canSkipRestartLocked(bridge, cfg) {
		h.cfg = cfg
		h.bridge = bridge
		h.errMsg = ""
		h.mu.Unlock()
		return nil
	}
	h.mu.Unlock()

	// Keep endpoint file during restart; clear only on terminal outcomes below.
	h.stopLocked(false)

	h.mu.Lock()
	h.cfg = cfg
	h.bridge = bridge
	h.errMsg = ""
	if !cfg.Enabled {
		h.state = "disabled"
		h.mu.Unlock()
		ClearDiscovery(h.keyring)
		return nil
	}
	if bridge == nil || bridge.VaultPath() == "" {
		h.state = "no_vault"
		h.errMsg = "open a vault to start local MCP"
		h.mu.Unlock()
		ClearDiscovery(h.keyring)
		return nil
	}
	h.state = "starting"
	h.mu.Unlock()

	token, err := h.ensureToken()
	if err != nil {
		h.setError(fmt.Sprintf("auth token: %v", err))
		ClearDiscovery(h.keyring)
		return err
	}

	// Auditor
	aud := h.audit
	if aud == nil {
		fa, aerr := newFileAuditor(bridge.VaultPath())
		if aerr != nil {
			log.Printf("mcp: audit file unavailable: %v", aerr)
			aud = &MemoryAuditor{}
		} else {
			aud = fa
		}
	}

	env := &toolEnv{
		bridge: bridge,
		cfg:    h.Config,
		audit:  aud,
		client: func(context.Context) string { return "local-mcp" },
	}

	srv := mcpsdk.NewServer(&mcpsdk.Implementation{
		Name:    "silt",
		Version: h.version,
	}, nil)
	registerTools(srv, env)
	// Single server-level receiving middleware: observes tools/call input
	// rejections before any handler runs and audits them as rejected_schema
	// (handlers still own ok/error/denied/rejected). Installed exactly once
	// per Server, before any client connects over HTTP.
	srv.AddReceivingMiddleware(schemaAuditMiddleware(env))

	h.mu.Lock()
	h.server = srv
	h.token = token
	h.audit = aud
	h.mu.Unlock()

	// Always prefer HTTP when enabled so stdio proxy can dial.
	// When HTTP is disabled the host still reports running for status, but
	// `silt mcp` cannot connect (it proxies over loopback HTTP only).
	if cfg.HTTPEnabled {
		if err := h.startHTTP(srv, token, cfg.HTTPPort); err != nil {
			h.setError(err.Error())
			// Listener is down; drop stale discovery so clients do not dial a dead port.
			ClearDiscovery(h.keyring)
			return err
		}
	} else {
		// No HTTP listener — stdio proxy cannot dial; clear any prior endpoint.
		ClearDiscovery(h.keyring)
	}

	h.mu.Lock()
	h.state = "running"
	if !cfg.HTTPEnabled {
		h.errMsg = "MCP enabled (stdio via `silt mcp` requires HTTP transport — enable HTTP in settings)"
	}
	h.mu.Unlock()
	return nil
}

// Stop drains and closes the HTTP listener and clears the discovery endpoint
// file. Safe when not running. Prefer Start's internal stopLocked(false) for restarts.
func (h *Host) Stop() {
	h.startMu.Lock()
	defer h.startMu.Unlock()
	h.stopLocked(true)
}

// stopLocked shuts down HTTP resources. Caller must hold startMu.
// When clearEndpoint is true, removes the discovery file (final stop / disable)
// only if this process owns it (or the owner is stale). When false, leaves the
// file so a concurrent silt mcp discovery still sees the last-known port until
// the new Start rewrites it or clears on failure.
func (h *Host) stopLocked(clearEndpoint bool) {
	h.mu.Lock()
	httpSrv := h.httpSrv
	ln := h.listener
	aud := h.audit
	h.httpSrv = nil
	h.listener = nil
	h.server = nil
	h.endpoint = ""
	h.bridge = nil
	if h.state != "disabled" && h.state != "error" {
		h.state = "disabled"
	}
	h.mu.Unlock()

	if httpSrv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := httpSrv.Shutdown(ctx)
		cancel()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				log.Printf("mcp http shutdown timed out after 5s (in-flight tool calls cut)")
			} else {
				log.Printf("mcp http shutdown: %v", err)
			}
		}
	}
	if ln != nil {
		_ = ln.Close()
	}
	if clearEndpoint {
		ClearDiscovery(h.keyring)
	}
	if aud != nil {
		// Don't close MemoryAuditor shared across tests (Options.Auditor).
		if _, ok := aud.(*MemoryAuditor); !ok {
			aud.Close()
			h.mu.Lock()
			// Drop closed file auditor so the next Start opens a fresh one.
			if h.audit == aud {
				h.audit = nil
			}
			h.mu.Unlock()
		}
	}
}

func (h *Host) setError(msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.state = "error"
	h.errMsg = msg
}

func (h *Host) ensureToken() (string, error) {
	// Prefer existing keyring token so client configs stay stable across restarts.
	if h.keyring != nil {
		if t, err := h.keyring.Get(KeyringService, KeyringUser); err == nil && t != "" {
			return t, nil
		} else if err != nil && !errors.Is(err, keyring.ErrNotFound) && !errors.Is(err, keyring.ErrUnavailable) {
			return "", err
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	if h.keyring != nil {
		if err := h.keyring.Set(KeyringService, KeyringUser, tok); err != nil {
			// Fall back to in-memory token when keyring unavailable.
			if !errors.Is(err, keyring.ErrUnavailable) {
				return "", err
			}
			log.Printf("mcp: keyring unavailable — token is process-local only")
		}
	}
	return tok, nil
}

// startHTTP binds 127.0.0.1 only and wraps the Streamable handler with bearer auth.
func (h *Host) startHTTP(srv *mcpsdk.Server, token string, port int) error {
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("bind %s: %w (is another Silt instance using this port?)", addr, err)
	}
	// Defense in depth: refuse non-loopback.
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok {
		if !tcp.IP.IsLoopback() {
			_ = ln.Close()
			return fmt.Errorf("refusing non-loopback bind %v", tcp.IP)
		}
	}

	// CrossOriginProtection is intentionally left nil. The go-sdk v1.6.0
	// default flipped from ON to OFF; we accept the OFF default because CSRF
	// is already prevented by the bearer scheme (tokens are not cookies, so a
	// cross-origin page cannot cause the browser to attach one) combined with
	// the origin allowlist in authMiddleware (isAllowedOrigin). The loopback
	// bind above separately blocks remote network access.
	// Stateful transport intentionally stays on protocol 2025-11-25: go-sdk
	// 1.7.0 serves the 2026-07-28 protocol only in stateless mode (SEP-2575),
	// and flipping Stateless would change session semantics for every client.
	// New-protocol clients negotiate down automatically; revisit only if
	// 2026-07-28-only clients must be supported.
	handler := mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server {
		return srv
	}, &mcpsdk.StreamableHTTPOptions{JSONResponse: true})

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service":"silt-mcp"}`))
	})
	mux.Handle("/", h.authMiddleware(token, handler))

	httpSrv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
		MaxHeaderBytes:    64 << 10, // 64 KiB — MCP never needs larger headers
	}

	h.mu.Lock()
	h.listener = ln
	h.httpSrv = httpSrv
	h.endpoint = fmt.Sprintf("http://%s", ln.Addr().String())
	h.mu.Unlock()

	go func() {
		if err := httpSrv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("mcp http: %v", err)
			h.setError(err.Error())
		}
	}()
	// Publish endpoint for `silt mcp` discovery (non-default ports).
	// Pin in keyring first so a rewritten endpoint file alone cannot steal the bearer.
	ep := fmt.Sprintf("http://%s", ln.Addr().String())
	if err := StorePinnedEndpoint(h.keyring, ep); err != nil {
		if !errors.Is(err, keyring.ErrUnavailable) {
			log.Printf("mcp: pin endpoint: %v", err)
		}
	}
	if err := WriteEndpointFile(ep); err != nil {
		// Peer ownership is expected when a second instance is running; HTTP
		// still serves this process — only shared discovery is skipped.
		if errors.Is(err, errEndpointOwnedByOther) {
			log.Printf("mcp: endpoint file owned by another live Silt instance — leaving peer discovery intact")
		} else {
			log.Printf("mcp: endpoint file: %v", err)
		}
	}
	return nil
}

func (h *Host) authMiddleware(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Origin: allow empty (non-browser) and localhost variants only.
		if origin := r.Header.Get("Origin"); origin != "" && !isAllowedOrigin(origin) {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		// Content-Type required for mutating methods (reject empty / non-JSON).
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			ct := strings.ToLower(strings.TrimSpace(r.Header.Get("Content-Type")))
			if ct == "" || !strings.HasPrefix(ct, "application/json") {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		auth := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(auth, prefix) {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		got := strings.TrimSpace(auth[len(prefix):])
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			http.Error(w, "invalid token", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isAllowedOrigin accepts empty (non-browser), "null" (some local clients),
// and exact loopback hostnames only. Prefix matching is intentionally avoided
// so http://127.0.0.1.evil.com cannot spoof the CSRF Origin check.
func isAllowedOrigin(origin string) bool {
	o := strings.ToLower(strings.TrimSpace(origin))
	if o == "" || o == "null" {
		return true
	}
	u, err := url.Parse(o)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || host == "localhost" || host == "::1"
}

// AssertLoopbackAddr returns an error if addr is not a loopback TCP address.
// Exported for tests.
func AssertLoopbackAddr(addr net.Addr) error {
	tcp, ok := addr.(*net.TCPAddr)
	if !ok {
		return fmt.Errorf("not tcp: %T", addr)
	}
	if !tcp.IP.IsLoopback() {
		return fmt.Errorf("non-loopback: %v", tcp.IP)
	}
	return nil
}
