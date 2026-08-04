package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/db"
	"silt/backend/keyring"
	"silt/backend/parser"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeBridge is an in-memory Bridge for host/tool tests.
type fakeBridge struct {
	path    string
	nav     parser.NavigationTree
	pages   map[string][]parser.ParsedBlock
	search  parser.SearchResult
	writes  int
	createN int
	// Typed-notes surface (Phase 7): canned metadata + injectable errors +
	// call counters so tool tests can assert grant/deny/error paths without a
	// real App behind the bridge.
	meta           map[string]PageMetadataResult
	metaErr        error
	setPropN       int
	setPropErr     error
	setPropLast    setPropCall
	setTypeN       int
	setTypeErr     error
	setTypeLast    setTypeCall
	setTypeFlagged []string
}

// setPropCall / setTypeCall capture the last (notebook, section, page, ...)
// tuple the bridge saw, so a test can assert the tool forwarded the args.
type setPropCall struct {
	Notebook, Section, Page, Property, Value string
}

type setTypeCall struct {
	Notebook, Section, Page, Type string
}

func (f *fakeBridge) VaultPath() string { return f.path }

func (f *fakeBridge) SearchBlocksPaged(ctx context.Context, query string, offset, limit int, filters db.SearchFilters) (parser.SearchResult, error) {
	_, _, _, _ = ctx, query, offset, limit
	_ = filters
	return f.search, nil
}

func (f *fakeBridge) FetchPageBlocks(ctx context.Context, notebook, section, page string) ([]parser.ParsedBlock, error) {
	_ = ctx
	key := notebook + "\x00" + section + "\x00" + page
	return f.pages[key], nil
}

func (f *fakeBridge) FetchPageMarkdown(ctx context.Context, notebook, section, page string) (string, error) {
	blocks, err := f.FetchPageBlocks(ctx, notebook, section, page)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, bl := range blocks {
		b.WriteString(bl.RawText)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func (f *fakeBridge) ListNavigation(ctx context.Context) (parser.NavigationTree, error) {
	_ = ctx
	return f.nav, nil
}

func (f *fakeBridge) CreatePage(ctx context.Context, notebook, section, page, dateStr string) (string, error) {
	_, _ = ctx, dateStr
	f.createN++
	key := notebook + "\x00" + section + "\x00" + page
	if f.pages == nil {
		f.pages = map[string][]parser.ParsedBlock{}
	}
	f.pages[key] = nil
	return key, nil
}

func (f *fakeBridge) UpdateBlocks(ctx context.Context, notebook, section, page string, blocks []parser.ParsedBlock) error {
	_ = ctx
	f.writes++
	key := notebook + "\x00" + section + "\x00" + page
	if f.pages == nil {
		f.pages = map[string][]parser.ParsedBlock{}
	}
	f.pages[key] = blocks
	return nil
}

func (f *fakeBridge) PageExists(ctx context.Context, notebook, section, page string) (bool, error) {
	_ = ctx
	if f.pages == nil {
		return false, nil
	}
	_, ok := f.pages[notebook+"\x00"+section+"\x00"+page]
	return ok, nil
}

func (f *fakeBridge) GetPageMetadata(ctx context.Context, notebook, section, page string) (PageMetadataResult, error) {
	_ = ctx
	if f.metaErr != nil {
		return PageMetadataResult{}, f.metaErr
	}
	if f.meta == nil {
		return PageMetadataResult{Notebook: notebook, Section: section, Page: page}, nil
	}
	if r, ok := f.meta[notebook+"\x00"+section+"\x00"+page]; ok {
		return r, nil
	}
	return PageMetadataResult{Notebook: notebook, Section: section, Page: page}, nil
}

func (f *fakeBridge) SetPageProperty(ctx context.Context, notebook, section, page, property, value string) error {
	_ = ctx
	f.setPropN++
	f.setPropLast = setPropCall{notebook, section, page, property, value}
	return f.setPropErr
}

func (f *fakeBridge) SetPageType(ctx context.Context, notebook, section, page, typeName string) ([]string, error) {
	_ = ctx
	f.setTypeN++
	f.setTypeLast = setTypeCall{notebook, section, page, typeName}
	return f.setTypeFlagged, f.setTypeErr
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return p
}

func TestHost_LifecycleStartStop(t *testing.T) {
	kr := keyring.NewFake()
	aud := &MemoryAuditor{}
	h := NewHost(Options{Keyring: kr, Auditor: aud, Version: "test"})
	bridge := &fakeBridge{path: t.TempDir()}
	port := freePort(t)

	if err := h.Start(bridge, Config{Enabled: true, HTTPEnabled: true, HTTPPort: port}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	st := h.Status()
	if st.State != "running" {
		t.Fatalf("state=%q want running (%s)", st.State, st.Message)
	}
	if st.Endpoint == "" {
		t.Fatal("expected endpoint")
	}
	u := strings.TrimPrefix(st.Endpoint, "http://")
	addr, err := net.ResolveTCPAddr("tcp", u)
	if err != nil {
		t.Fatal(err)
	}
	if err := AssertLoopbackAddr(addr); err != nil {
		t.Fatalf("loopback: %v", err)
	}

	resp, err := http.Get(st.Endpoint + "/health")
	if err != nil {
		t.Fatalf("health: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), "silt-mcp") {
		t.Fatalf("health status=%d body=%s", resp.StatusCode, body)
	}

	h.Stop()
	time.Sleep(50 * time.Millisecond)
	if _, err := http.Get(st.Endpoint + "/health"); err == nil {
		t.Fatal("expected connection error after Stop")
	}
}

func TestHost_AuthReject(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	bridge := &fakeBridge{path: t.TempDir()}
	port := freePort(t)
	if err := h.Start(bridge, Config{Enabled: true, HTTPEnabled: true, HTTPPort: port}); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	ep := h.Endpoint()

	resp, err := http.Post(ep+"/", "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: status=%d", resp.StatusCode)
	}

	req, _ := http.NewRequest(http.MethodPost, ep+"/", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer wrong")
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad token: status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, ep+"/", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+h.Token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://evil.example")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("evil origin: status=%d", resp.StatusCode)
	}

	// Prefix-spoof must not pass (hostname must be exact loopback).
	req, _ = http.NewRequest(http.MethodPost, ep+"/", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+h.Token())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1.evil.com")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("spoofed origin prefix: status=%d", resp.StatusCode)
	}
}

func TestIsLoopbackEndpoint(t *testing.T) {
	cases := []struct {
		u    string
		want bool
	}{
		{"http://127.0.0.1:17887", true},
		{"http://localhost/mcp", true},
		{"http://[::1]:9", true},
		{"https://127.0.0.1", true},
		{"http://evil.example", false},
		{"http://127.0.0.1.evil.com", false},
		{"ftp://127.0.0.1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLoopbackEndpoint(tc.u); got != tc.want {
			t.Errorf("IsLoopbackEndpoint(%q)=%v want %v", tc.u, got, tc.want)
		}
	}
}

func TestIsAllowedOrigin(t *testing.T) {
	cases := []struct {
		o    string
		want bool
	}{
		{"", true},
		{"null", true},
		{"http://127.0.0.1", true},
		{"http://127.0.0.1:17887", true},
		{"https://localhost", true},
		{"http://[::1]", true},
		{"http://127.0.0.1.evil.com", false},
		{"http://localhost.attacker.tld", false},
		{"https://evil.example", false},
		{"ftp://127.0.0.1", false},
	}
	for _, tc := range cases {
		if got := isAllowedOrigin(tc.o); got != tc.want {
			t.Errorf("isAllowedOrigin(%q)=%v want %v", tc.o, got, tc.want)
		}
	}
}

func TestHost_DisabledAndNoVault(t *testing.T) {
	h := NewHost(Options{Keyring: keyring.NewFake(), Auditor: &MemoryAuditor{}})
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if h.Status().State != "disabled" {
		t.Fatalf("want disabled got %s", h.Status().State)
	}
	if err := h.Start(nil, Config{Enabled: true, HTTPEnabled: true}); err != nil {
		t.Fatal(err)
	}
	if h.Status().State != "no_vault" {
		t.Fatalf("want no_vault got %s", h.Status().State)
	}
}

func TestHost_VaultSwitchRestarts(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}})
	p1 := freePort(t)
	b1 := &fakeBridge{path: t.TempDir() + "/a"}
	if err := h.Start(b1, Config{Enabled: true, HTTPEnabled: true, HTTPPort: p1}); err != nil {
		t.Fatal(err)
	}
	ep1 := h.Endpoint()
	p2 := freePort(t)
	b2 := &fakeBridge{path: t.TempDir() + "/b"}
	if err := h.Start(b2, Config{Enabled: true, HTTPEnabled: true, HTTPPort: p2}); err != nil {
		t.Fatal(err)
	}
	ep2 := h.Endpoint()
	if ep1 == ep2 {
		t.Fatalf("expected new endpoint after switch: %s", ep2)
	}
	time.Sleep(30 * time.Millisecond)
	if _, err := http.Get(ep1 + "/health"); err == nil {
		t.Fatal("old endpoint still up")
	}
	resp, err := http.Get(ep2 + "/health")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	h.Stop()
}

func TestHost_LoopbackOnlyBind(t *testing.T) {
	// Binding 0.0.0.0 is never attempted by Host; AssertLoopbackAddr rejects non-loopback.
	ip := net.ParseIP("8.8.8.8")
	addr := &net.TCPAddr{IP: ip, Port: 9}
	if err := AssertLoopbackAddr(addr); err == nil {
		t.Fatal("expected non-loopback error")
	}
	loop := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9}
	if err := AssertLoopbackAddr(loop); err != nil {
		t.Fatal(err)
	}
}

func connectTools(t *testing.T, bridge Bridge, cfg Config) (*mcpsdk.ClientSession, *MemoryAuditor) {
	t.Helper()
	aud := &MemoryAuditor{}
	env := &toolEnv{
		bridge: bridge,
		cfg:    func() Config { return cfg },
		audit:  aud,
		client: func(context.Context) string { return "test" },
	}
	srv := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "silt", Version: "test"}, nil)
	registerTools(srv, env)
	t1, t2 := mcpsdk.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), t1, nil); err != nil {
		t.Fatal(err)
	}
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "c", Version: "t"}, nil)
	cs, err := client.Connect(context.Background(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs, aud
}

func toolText(t *testing.T, res *mcpsdk.CallToolResult) string {
	t.Helper()
	if res == nil {
		return ""
	}
	var parts []string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			parts = append(parts, tc.Text)
		}
	}
	return strings.Join(parts, "")
}

func TestTools_ReadHappyPath(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		pages: map[string][]parser.ParsedBlock{
			"Work\x00\x00Home": {
				{ID: "b1", Type: parser.BlockNote, RawText: "hello vault", CleanText: "hello vault"},
			},
		},
		search: parser.SearchResult{
			Results: []parser.TaskResult{{ID: "b1", Notebook: "Work", Page: "Home", Snippet: "hello vault"}},
			Total:   1,
			Limit:   20,
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "search_blocks",
		Arguments: map[string]any{"query": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("search error: %s", toolText(t, res))
	}
	if !strings.Contains(toolText(t, res), "hello vault") {
		t.Fatalf("search: %s", toolText(t, res))
	}

	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "read_page",
		Arguments: map[string]any{"notebook": "Work", "section": "", "page": "Home"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("read: %s", toolText(t, res))
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(toolText(t, res)), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["count"] == nil {
		t.Fatalf("missing count: %v", payload)
	}

	res, err = cs.CallTool(ctx, &mcpsdk.CallToolParams{Name: "list_notebooks", Arguments: map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("list: %s", toolText(t, res))
	}
}

func TestTools_WriteWithoutGrantRejected(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir()}
	cs, aud := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: false})
	ctx := context.Background()

	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_page",
		Arguments: map[string]any{"notebook": "Work", "section": "", "page": "X"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected isError for write without grant")
	}
	if bridge.createN != 0 {
		t.Fatalf("create ran: %d", bridge.createN)
	}
	found := false
	for _, e := range aud.Entries {
		if e.Tool == "create_page" && e.Outcome == "denied" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected denied audit entry: %+v", aud.Entries)
	}

	// With grant
	cs2, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err = cs2.CallTool(ctx, &mcpsdk.CallToolParams{
		Name:      "create_page",
		Arguments: map[string]any{"notebook": "Work", "section": "", "page": "X"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("create with grant: %s", toolText(t, res))
	}
	if bridge.createN != 1 {
		t.Fatalf("createN=%d", bridge.createN)
	}
}

func TestTools_UpdateBlocksWithGrant(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		pages: map[string][]parser.ParsedBlock{
			"Work\x00Sec\x00P": {{ID: "id-1", Type: parser.BlockNote, CleanText: "old"}},
		},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	ctx := context.Background()
	res, err := cs.CallTool(ctx, &mcpsdk.CallToolParams{
		Name: "update_blocks",
		Arguments: map[string]any{
			"notebook": "Work",
			"section":  "Sec",
			"page":     "P",
			"blocks": []any{
				map[string]any{"id": "id-1", "type": "NOTE", "text": "hello"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("update_blocks: %s", toolText(t, res))
	}
	if bridge.writes != 1 {
		t.Fatalf("writes=%d", bridge.writes)
	}
	key := "Work\x00Sec\x00P"
	if len(bridge.pages[key]) != 1 || bridge.pages[key][0].CleanText != "hello" {
		t.Fatalf("pages: %+v", bridge.pages[key])
	}
}

func TestTools_UpdateBlocksMissingPageRejected(t *testing.T) {
	bridge := &fakeBridge{path: t.TempDir(), pages: map[string][]parser.ParsedBlock{}}
	cs, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "update_blocks",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "Missing",
			"blocks": []any{map[string]any{"type": "NOTE", "text": "x"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected missing page rejection")
	}
	if bridge.writes != 0 {
		t.Fatalf("writes=%d", bridge.writes)
	}
}

func TestTools_UpdateBlocksEmptyRejected(t *testing.T) {
	bridge := &fakeBridge{
		path:  t.TempDir(),
		pages: map[string][]parser.ParsedBlock{"Work\x00\x00P": {}},
	}
	cs, _ := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "update_blocks",
		Arguments: map[string]any{
			"notebook": "Work", "section": "", "page": "P", "blocks": []any{},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsError {
		t.Fatal("expected empty wipe rejection")
	}
	if bridge.writes != 0 {
		t.Fatalf("writes=%d", bridge.writes)
	}
}

func TestHost_ContentTypeRequired(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	port := freePort(t)
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: true, HTTPEnabled: true, HTTPPort: port}); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()

	req, _ := http.NewRequest(http.MethodPost, h.Endpoint()+"/", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+h.Token())
	// No Content-Type
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("empty CT: status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodPost, h.Endpoint()+"/", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+h.Token())
	req.Header.Set("Content-Type", "text/plain")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: status=%d", resp.StatusCode)
	}
}

func TestEndpointFile_RoundTrip(t *testing.T) {
	// Isolate config dir via temp HOME/USERPROFILE is platform-specific;
	// exercise Write/Read against a path we control by temporarily writing
	// through the public API when UserConfigDir is available.
	ep := "http://127.0.0.1:19999"
	if err := WriteEndpointFile(ep); err != nil {
		t.Skipf("UserConfigDir unavailable: %v", err)
	}
	t.Cleanup(ClearEndpointFile)
	got := ReadEndpointFile()
	if got != ep {
		t.Fatalf("got %q want %q", got, ep)
	}
	ClearEndpointFile()
	if ReadEndpointFile() != "" {
		t.Fatal("expected empty after clear")
	}
}

// TestHost_PinsEndpointInKeyring ensures Start stores the loopback URL in the
// keyring so silt mcp discovery cannot be hijacked by rewriting the endpoint file.
func TestHost_PinsEndpointInKeyring(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	port := freePort(t)
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: true, HTTPEnabled: true, HTTPPort: port}); err != nil {
		t.Fatal(err)
	}
	ep := strings.TrimRight(h.Endpoint(), "/")
	pinned := LoadPinnedEndpoint(kr)
	if pinned != ep {
		t.Fatalf("pinned=%q endpoint=%q", pinned, ep)
	}
	h.Stop()
	if LoadPinnedEndpoint(kr) != "" {
		t.Fatal("expected pin cleared on Stop")
	}
}

// TestHost_RestartKeepsEndpointUntilRewrite ensures vault-switch restart does
// not wipe the discovery file before the new listener is up (and rewrites it).
func TestHost_WriteGrantToggleSkipsHTTPRestart(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	bridge := &fakeBridge{path: t.TempDir()}
	port := freePort(t)
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: port, WriteEnabled: false}
	if err := h.Start(bridge, cfg); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	ep1 := h.Endpoint()
	ln1 := h.listener

	cfg.WriteEnabled = true
	if err := h.Start(bridge, cfg); err != nil {
		t.Fatal(err)
	}
	if h.Endpoint() != ep1 {
		t.Fatalf("endpoint changed on write-grant toggle: %q → %q", ep1, h.Endpoint())
	}
	if h.listener != ln1 {
		t.Fatal("listener recreated on write-grant-only toggle")
	}
	if !h.Config().WriteEnabled {
		t.Fatal("expected WriteEnabled true after skip-restart Start")
	}
}

func TestHost_RestartKeepsEndpointUntilRewrite(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	p1 := freePort(t)
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: true, HTTPEnabled: true, HTTPPort: p1}); err != nil {
		t.Fatal(err)
	}
	ep1 := h.Endpoint()
	if ep1 == "" {
		t.Fatal("expected endpoint after first start")
	}
	if err := WriteEndpointFile(ep1); err != nil {
		t.Skipf("endpoint file: %v", err)
	}
	// Mid-restart stop must not clear the file.
	h.startMu.Lock()
	h.stopLocked(false)
	h.startMu.Unlock()
	if got := ReadEndpointFile(); got != ep1 {
		t.Fatalf("after stop(false) endpoint file=%q want %q", got, ep1)
	}
	p2 := freePort(t)
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: true, HTTPEnabled: true, HTTPPort: p2}); err != nil {
		t.Fatal(err)
	}
	defer h.Stop()
	ep2 := h.Endpoint()
	if ep2 == "" || ep2 == ep1 {
		// Ports differ so endpoints should differ; file should match new.
		t.Logf("ep1=%s ep2=%s", ep1, ep2)
	}
	if got := ReadEndpointFile(); got != ep2 {
		t.Fatalf("after restart endpoint file=%q want %q", got, ep2)
	}
}

func TestHost_DisableClearsEndpointFile(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	port := freePort(t)
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: true, HTTPEnabled: true, HTTPPort: port}); err != nil {
		t.Fatal(err)
	}
	if ReadEndpointFile() == "" {
		t.Skip("endpoint file not written (config dir?)")
	}
	if err := h.Start(&fakeBridge{path: t.TempDir()}, Config{Enabled: false}); err != nil {
		t.Fatal(err)
	}
	if got := ReadEndpointFile(); got != "" {
		t.Fatalf("expected cleared endpoint after disable, got %q", got)
	}
}

func TestFileAuditor_RotatesWhenOverSizeCap(t *testing.T) {
	dir := t.TempDir()
	// Force a tiny cap for the test by writing a large file then recording.
	path := filepath.Join(dir, ".system", "logs", "mcp-audit.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-fill past 1 MB so the next Record triggers rotation.
	big := make([]byte, maxMCPAuditLogBytes+100)
	for i := range big {
		big[i] = 'x'
	}
	// Make it look like JSONL lines.
	line := strings.Repeat("a", 200) + "\n"
	var b strings.Builder
	for b.Len() < maxMCPAuditLogBytes+100 {
		b.WriteString(line)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	fa, err := newFileAuditor(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer fa.Close()
	fa.Record(AuditEntry{Tool: "search_blocks", Outcome: "ok", Vault: "v"})
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() > maxMCPAuditLogBytes {
		t.Errorf("after rotate size=%d want ≤ %d", info.Size(), maxMCPAuditLogBytes)
	}
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "search_blocks") {
		t.Fatal("expected new audit line after rotate")
	}
}

func TestRedactArgs_NoBodies(t *testing.T) {
	m := RedactArgs(map[string]any{
		"query":     "secret note body text",
		"notebook":  "Work",
		"text":      "should not appear",
		"blocks":    []any{1, 2, 3},
		"block_ids": []string{"id-a", "id-b"},
	})
	raw, _ := json.Marshal(m)
	s := string(raw)
	if strings.Contains(s, "secret note") || strings.Contains(s, "should not") {
		t.Fatalf("leaked body: %s", s)
	}
	if m["notebook"] != "Work" {
		t.Fatalf("notebook: %v", m["notebook"])
	}
	if m["blocks_count"] != 3 {
		t.Fatalf("blocks_count: %v", m["blocks_count"])
	}
	ids, ok := m["block_ids"].([]string)
	if !ok || len(ids) != 2 || ids[0] != "id-a" {
		t.Fatalf("block_ids: %v", m["block_ids"])
	}
}

func TestTools_UpdateBlocksAuditsBlockIDsAndTypeDefault(t *testing.T) {
	bridge := &fakeBridge{
		path: t.TempDir(),
		pages: map[string][]parser.ParsedBlock{
			"Work\x00Sec\x00P": {{ID: "blk-1", Type: parser.BlockNote}},
		},
	}
	cs, aud := connectTools(t, bridge, Config{Enabled: true, WriteEnabled: true})
	res, err := cs.CallTool(context.Background(), &mcpsdk.CallToolParams{
		Name: "update_blocks",
		Arguments: map[string]any{
			"notebook": "Work",
			"section":  "Sec",
			"page":     "P",
			"blocks": []any{
				map[string]any{"id": "blk-1", "text": "no type field"},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("%s", toolText(t, res))
	}
	var found bool
	for _, e := range aud.Entries {
		if e.Tool != "update_blocks" || e.Outcome != "ok" {
			continue
		}
		found = true
		meta := e.ArgsMeta
		if meta == nil {
			t.Fatalf("expected ArgsMeta on audit entry: %+v", e)
		}
		if meta["type_defaulted_count"] == nil {
			t.Fatalf("expected type_defaulted_count in audit: %+v", meta)
		}
		raw, _ := json.Marshal(meta)
		if !strings.Contains(string(raw), "blk-1") {
			t.Fatalf("expected block id in audit meta: %s", raw)
		}
	}
	if !found {
		t.Fatalf("no ok update_blocks audit: %+v", aud.Entries)
	}
}

func TestVaultPathHash_Stable(t *testing.T) {
	a := VaultPathHash("/tmp/vault")
	b := VaultPathHash("/tmp/vault")
	if a == "" || a != b {
		t.Fatalf("%q vs %q", a, b)
	}
	if VaultPathHash("/other") == a {
		t.Fatal("expected different hash")
	}
}

// silence unused in case of build tags
var _ = fmt.Sprintf
