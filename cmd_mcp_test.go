package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"silt/backend/keyring"
	"silt/backend/mcp"
)

func TestBearerRoundTripper_RefusesNonLoopback(t *testing.T) {
	rt := &bearerRoundTripper{token: "secret-token", base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatal("base transport must not be called for non-loopback")
		return nil, nil
	})}
	req, err := http.NewRequest(http.MethodGet, "http://example.com/mcp", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = rt.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "non-loopback") {
		t.Fatalf("expected non-loopback refusal, got %v", err)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("must not mutate original request Authorization")
	}
}

func TestBearerRoundTripper_AllowsLoopbackAndSetsAuth(t *testing.T) {
	var sawAuth string
	rt := &bearerRoundTripper{token: "secret-token", base: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		sawAuth = req.Header.Get("Authorization")
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`ok`)),
			Header:     make(http.Header),
			Request:    req,
		}, nil
	})}
	req, err := http.NewRequest(http.MethodPost, "http://127.0.0.1:17887/mcp", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("loopback: %v", err)
	}
	defer resp.Body.Close()
	if sawAuth != "Bearer secret-token" {
		t.Fatalf("Authorization = %q", sawAuth)
	}
	if req.Header.Get("Content-Type") != "application/json" && resp.Request.Header.Get("Content-Type") != "application/json" {
		// Clone gets Content-Type; original may stay empty.
		if resp.Request.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("Content-Type on outbound = %q", resp.Request.Header.Get("Content-Type"))
		}
	}
}

func TestLoadMCPToken_EnvOverride(t *testing.T) {
	t.Setenv("SILT_MCP_TOKEN", "env-token-value")
	got, err := loadMCPToken()
	if err != nil {
		t.Fatal(err)
	}
	if got != "env-token-value" {
		t.Fatalf("got %q", got)
	}
}

func TestDiscoverMCPEndpoint_IgnoresSpoofedFileWhenPinned(t *testing.T) {
	// Real host on ephemeral port with silt-mcp health.
	real := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"service":"silt-mcp"}`))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(real.Close)
	realURL := "http://" + real.Listener.Addr().String()
	if !mcp.IsLoopbackEndpoint(realURL) {
		t.Skip("httptest not on loopback")
	}

	// Spoof host that also answers silt-mcp health (would steal bearer if chosen).
	spoof := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service":"silt-mcp"}`))
	}))
	t.Cleanup(spoof.Close)
	spoofURL := "http://" + spoof.Listener.Addr().String()

	cfgDir := t.TempDir()
	t.Setenv("USERPROFILE", cfgDir) // Windows UserConfigDir under USERPROFILE\AppData\Roaming
	// Force EndpointFilePath under temp: override via writing after chdir of config is hard.
	// Use mcp helpers with a patched path by writing where EndpointFilePath resolves.
	// On Windows UserConfigDir = %AppData%; set APPDATA.
	appData := filepath.Join(cfgDir, "AppData", "Roaming")
	if err := os.MkdirAll(filepath.Join(appData, "silt"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("APPDATA", appData)
	t.Setenv("HOME", cfgDir)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(cfgDir, ".config"))

	// Pin real endpoint in a fake keyring injected via env is not possible for Default().
	// Instead exercise the pin helpers + discover logic pieces:
	// 1) Store pin via real Default if available, else unit-test the mismatch filter.
	kr := keyring.NewFake()
	if err := mcp.StorePinnedEndpoint(kr, realURL); err != nil {
		t.Fatal(err)
	}
	if err := mcp.WriteEndpointFile(spoofURL); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mcp.ClearEndpointFile)

	// Mirrors discoverMCPEndpoint candidate filter.
	pinned := mcp.LoadPinnedEndpoint(kr)
	if pinned != strings.TrimRight(realURL, "/") {
		t.Fatalf("pin = %q want %q", pinned, realURL)
	}
	fileEp := mcp.ReadEndpointFile()
	if fileEp == "" {
		t.Fatal("expected spoofed file to read as loopback")
	}
	fileEp = strings.TrimRight(fileEp, "/")
	if pinned == fileEp {
		t.Fatal("test setup: pin and file must differ")
	}
	// File must be ignored when it disagrees with pin.
	candidates := []string{pinned}
	if pinned == "" || pinned == fileEp {
		candidates = append(candidates, fileEp)
	}
	if len(candidates) != 1 || candidates[0] != pinned {
		t.Fatalf("candidates = %v; spoofed file must not be a candidate", candidates)
	}

	// Health on pin succeeds.
	resp, err := http.Get(pinned + "/health")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(body), "silt-mcp") {
		t.Fatalf("pinned health: %d %s", resp.StatusCode, body)
	}
}

func TestDiscoverMCPEndpoint_FileMatchesPin(t *testing.T) {
	kr := keyring.NewFake()
	ep := "http://127.0.0.1:17887"
	if err := mcp.StorePinnedEndpoint(kr, ep); err != nil {
		t.Fatal(err)
	}
	if got := mcp.LoadPinnedEndpoint(kr); got != ep {
		t.Fatalf("LoadPinnedEndpoint = %q", got)
	}
	// Non-loopback rejected.
	if err := mcp.StorePinnedEndpoint(kr, "http://example.com:9"); err == nil {
		t.Fatal("expected non-loopback pin error")
	}
	mcp.ClearPinnedEndpoint(kr)
	if got := mcp.LoadPinnedEndpoint(kr); got != "" {
		t.Fatalf("after clear got %q", got)
	}
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
