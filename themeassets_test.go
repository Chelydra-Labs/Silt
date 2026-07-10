package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"silt/backend/themes"
)

// mustParseURL builds a *url.URL for a raw path. Using url.Parse (not
// url.ParseRequestURI) keeps ".." segments and the leading slash intact
// so the handler's traversal guard is exercised against a verbatim path,
// the way Wails would deliver it.
func mustParseURL(path string) *url.URL {
	u, err := url.Parse(path)
	if err != nil {
		panic(err)
	}
	return u
}

// newResolver returns a themesDir resolver closure backed by a mutable
// holder, mirroring the shape of the closure wired in main.go (which reads
// app.vaultPath under the vault mutex). The holder lets a test flip the
// "current themes dir" between requests to prove the handler doesn't bake
// in a startup path.
type resolverHolder struct {
	dir string
}

func (h *resolverHolder) get() string { return h.dir }

// buildAssetTree writes <themesDir>/<id>.assets/<file> with the given bytes
// and returns the absolute file path.
func buildAssetTree(t *testing.T, themesDir, id, filename string, bytes []byte) string {
	t.Helper()
	assetsDir := filepath.Join(themesDir, id+".assets")
	if err := os.MkdirAll(assetsDir, 0o755); err != nil {
		t.Fatalf("mkdir assets: %v", err)
	}
	p := filepath.Join(assetsDir, filename)
	if err := os.WriteFile(p, bytes, 0o644); err != nil {
		t.Fatalf("write asset: %v", err)
	}
	return p
}

// TestThemeAssetHandler_ServesValidAsset: the canonical happy path — a GET
// for an existing <id>.assets/<file> returns the file's bytes with a
// sniffed image Content-Type.
func TestThemeAssetHandler_ServesValidAsset(t *testing.T) {
	themesDir := t.TempDir()
	img := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4} // PNG magic + payload
	buildAssetTree(t, themesDir, "terra-test", "photo.png", img)

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/terra-test.assets/photo.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != string(img) {
		t.Errorf("body bytes drift: got %v want %v", body, img)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "image/") {
		t.Errorf("Content-Type = %q, want an image/* type", ct)
	}
	// A strict CSP locks the response to image-context even if a future
	// Content-Type mix-up would otherwise let an SVG execute script.
	if csp := resp.Header.Get("Content-Security-Policy"); csp != "default-src 'none'; img-src 'self'" {
		t.Errorf("Content-Security-Policy = %q, want %q", csp, "default-src 'none'; img-src 'self'")
	}
}

// TestThemeAssetHandler_InvalidThemeID: an id that fails IsValidThemeID
// (uppercase letters are rejected by the [a-z0-9_-] set) yields 404 — the
// handler never touches the filesystem for a malformed id.
func TestThemeAssetHandler_InvalidThemeID(t *testing.T) {
	themesDir := t.TempDir()
	buildAssetTree(t, themesDir, "terra-test", "x.png", []byte("x"))

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Uppercase letters are outside IsValidThemeID's allowed set.
	resp, err := http.Get(srv.URL + "/Terra-Test.assets/x.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for invalid theme id", resp.StatusCode)
	}
}

// TestThemeAssetHandler_RejectsTraversal: an explicit path-traversal
// attempt (raw ".." segment in req.URL.Path) must NOT escape the assets
// dir. The raw path reaches the handler un-cleaned (Wails passes it
// verbatim), so this is the CWE-22 guard. We bypass httptest.NewServer
// and call the handler directly via httptest.NewRecorder so the Go HTTP
// client transport can't normalize the path before the handler sees it.
// Expect 403 (refused) or 404 (miss) — never 200 with foreign bytes.
func TestThemeAssetHandler_RejectsTraversal(t *testing.T) {
	themesDir := t.TempDir()
	// Drop a sentinel file ONE level above the themes dir; if traversal
	// succeeded, the handler would serve its bytes.
	parent := filepath.Dir(themesDir)
	sentinel := filepath.Join(parent, "secret.txt")
	if err := os.WriteFile(sentinel, []byte("top-secret"), 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	// A legitimate asset so the assets dir exists.
	buildAssetTree(t, themesDir, "terra-test", "real.png", []byte("real"))

	h := themeAssetHandler(func() string { return themesDir })

	cases := []string{
		// ".." segment inside the filename portion (decoded from URL).
		"/terra-test.assets/../secret.txt",
		// Absolute-path attempt inside the filename portion.
		"/terra-test.assets//etc/passwd",
		// Bare traversal with no .assets segment (must miss, not serve).
		"/../../../etc/passwd",
		// Traversal entirely inside the id portion.
		"/../../etc/passwd.assets/x",
	}
	for _, p := range cases {
		req := &http.Request{Method: http.MethodGet, URL: mustParseURL(p)}
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK {
			t.Errorf("%q: traversal returned 200 (sentinel would leak)", p)
		}
		if strings.Contains(rr.Body.String(), "top-secret") {
			t.Errorf("%q: response body leaked sentinel contents", p)
		}
	}
}

// TestThemeAssetHandler_NonAssetPathFallsThrough: a path with no
// "<id>.assets/" segment is a miss for this handler — Wails would have
// served it from the embedded Assets already, so 404 is the right answer.
func TestThemeAssetHandler_NonAssetPathFallsThrough(t *testing.T) {
	themesDir := t.TempDir()
	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/index.html")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for non-assets path", resp.StatusCode)
	}
}

// TestThemeAssetHandler_NoVaultOpen: before a vault is open the resolver
// returns "" — the handler must 404 rather than crash on an empty path.
func TestThemeAssetHandler_NoVaultOpen(t *testing.T) {
	h := themeAssetHandler(func() string { return "" })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/terra-test.assets/photo.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 when no vault is open", resp.StatusCode)
	}
}

// TestThemeAssetHandler_RejectsDisallowedExtension: the handler mirrors
// StoreBackgroundAsset's ingestion allowlist (png/jpg/jpeg/webp/gif/svg).
// A file with any other extension — even if a sync client dropped it into
// <id>.assets/ — is treated as not-found so the existence of a non-image
// on disk is never confirmed to a probe. The .png case stays green via
// TestThemeAssetHandler_ServesValidAsset above.
func TestThemeAssetHandler_RejectsDisallowedExtension(t *testing.T) {
	themesDir := t.TempDir()
	// A real .txt on disk under a valid id + safe filename shape — the
	// extension gate is the only thing that should keep it from being served.
	buildAssetTree(t, themesDir, "terra-test", "notes.txt", []byte("plain text, not an image"))

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/terra-test.assets/notes.txt")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for disallowed extension", resp.StatusCode)
	}
	// And the body must not echo the file's bytes back.
	body, _ := io.ReadAll(resp.Body)
	if strings.Contains(string(body), "plain text, not an image") {
		t.Errorf("response leaked contents of disallowed-extension file")
	}
}

// TestThemeAssetHandler_DynamicThemesDir: the resolver is consulted on
// EVERY request, so flipping the "current" themes dir between two GETs
// serves the asset from whichever vault is active at request time. This
// pins the G2 requirement that the handler cannot capture a static path.
func TestThemeAssetHandler_DynamicThemesDir(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	buildAssetTree(t, dir1, "terra-test", "photo.png", []byte("from-dir-1"))
	buildAssetTree(t, dir2, "terra-test", "photo.png", []byte("from-dir-2"))

	holder := &resolverHolder{dir: dir1}
	h := themeAssetHandler(holder.get)
	srv := httptest.NewServer(h)
	defer srv.Close()

	get := func() string {
		t.Helper()
		resp, err := http.Get(srv.URL + "/terra-test.assets/photo.png")
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200", resp.StatusCode)
		}
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	if got := get(); got != "from-dir-1" {
		t.Errorf("before switch: got %q, want from-dir-1", got)
	}
	// Simulate a vault switch — the same handler instance now resolves
	// to a different themes dir.
	holder.dir = dir2
	if got := get(); got != "from-dir-2" {
		t.Errorf("after switch: got %q, want from-dir-2", got)
	}
}

// TestThemeAssetHandler_HeadRequest: HEAD (which http.ServeContent handles)
// returns the headers without a body — the webview's image loader uses
// HEAD for preflight/size checks.
func TestThemeAssetHandler_HeadRequest(t *testing.T) {
	themesDir := t.TempDir()
	img := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 9, 9}
	buildAssetTree(t, themesDir, "terra-test", "photo.png", img)

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Head(srv.URL + "/terra-test.assets/photo.png")
	if err != nil {
		t.Fatalf("HEAD: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if resp.ContentLength != int64(len(img)) {
		t.Errorf("Content-Length = %d, want %d", resp.ContentLength, len(img))
	}
}

// TestThemeAssetHandler_MissingFile: a valid id + safe filename shape +
// allowed extension that simply doesn't exist on disk is a benign miss —
// 404, the same surface Wails already returns for an absent embedded asset.
// The assets dir IS created (with a different real file) so this exercises
// the os.Open-fails branch rather than a never-existed-dir short-circuit.
func TestThemeAssetHandler_MissingFile(t *testing.T) {
	themesDir := t.TempDir()
	// A real asset exists for the theme, proving the assets dir is live.
	buildAssetTree(t, themesDir, "terra-test", "real.png", []byte("real"))

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/terra-test.assets/ghost.png")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for missing file", resp.StatusCode)
	}
}

// TestThemeAssetHandler_RejectsNonGetHead: only GET/HEAD reach the asset
// pipeline; anything else (POST here, but PUT/DELETE/etc. behave the same)
// is a miss. The method gate runs before any filesystem touch, so even a
// POST against a path whose file exists never serves or modifies it.
func TestThemeAssetHandler_RejectsNonGetHead(t *testing.T) {
	themesDir := t.TempDir()
	img := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}
	buildAssetTree(t, themesDir, "terra-test", "photo.png", img)

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/terra-test.assets/photo.png", "text/plain", strings.NewReader("ignore"))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for POST", resp.StatusCode)
	}
}

// TestThemeAssetHandler_RangeRequestServesPartialContent: http.ServeContent
// (which the handler delegates to) honors a Range request, returning 206
// Partial Content with exactly the requested byte range. The webview's image
// loader emits Range requests for progressive/large background loads, so a
// regression here would show up as broken partial fetches.
func TestThemeAssetHandler_RangeRequestServesPartialContent(t *testing.T) {
	themesDir := t.TempDir()
	img := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 10, 20, 30, 40, 50, 60, 70, 80}
	buildAssetTree(t, themesDir, "terra-test", "photo.png", img)

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/terra-test.assets/photo.png", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Range", "bytes=0-3") // first 4 bytes (PNG magic)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 Partial Content", resp.StatusCode)
	}
	if resp.ContentLength != 4 {
		t.Errorf("Content-Length = %d, want 4 (bytes 0-3)", resp.ContentLength)
	}
	if ar := resp.Header.Get("Accept-Ranges"); ar != "bytes" {
		t.Errorf("Accept-Ranges = %q, want %q", ar, "bytes")
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	want := img[:4]
	if !bytes.Equal(body, want) {
		t.Errorf("partial body = %v, want %v (first 4 bytes)", body, want)
	}
}

// TestThemeAssetHandler_ConditionalRequestNotModified: ServeContent honors
// If-Modified-Since against the file's mtime. Sending the exact mtime yields
// 304 Not Modified (empty body); sending a timestamp from before the mtime
// serves the full body. The webview revalidates cached theme backgrounds this
// way, so a 200-every-time regression would needlessly re-download.
func TestThemeAssetHandler_ConditionalRequestNotModified(t *testing.T) {
	themesDir := t.TempDir()
	img := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 1, 2, 3, 4}
	p := buildAssetTree(t, themesDir, "terra-test", "photo.png", img)

	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat asset: %v", err)
	}
	modTime := fi.ModTime()

	h := themeAssetHandler(func() string { return themesDir })
	srv := httptest.NewServer(h)
	defer srv.Close()

	get := func(ims time.Time) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/terra-test.assets/photo.png", nil)
		if err != nil {
			t.Fatalf("NewRequest: %v", err)
		}
		req.Header.Set("If-Modified-Since", ims.UTC().Format(http.TimeFormat))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("GET: %v", err)
		}
		return resp
	}

	// Fresh revalidation with the asset's own mtime → not modified.
	resp := get(modTime)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("status = %d, want 304 for matching If-Modified-Since", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) != 0 {
		t.Errorf("304 response should have an empty body, got %d bytes", len(body))
	}

	// A stale validator (a day before the mtime) → the asset is newer than
	// the client thinks, so the full body is served.
	resp2 := get(modTime.Add(-24 * time.Hour))
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 for stale If-Modified-Since", resp2.StatusCode)
	}
	body2, _ := io.ReadAll(resp2.Body)
	if !bytes.Equal(body2, img) {
		t.Errorf("full body = %v, want %v", body2, img)
	}
}

// themeAssetMiddleware mirrors the AssetOptions.Middleware closure wired in
// main.go: it routes "<validId>.assets/<file>" requests to the per-theme
// handler and falls through to Wails' embedded AssetFileServerFS (next) for
// everything else. main.go owns the real closure; this in-test copy pins the
// routing CONTRACT so a change to the predicate is surfaced here. Promoting
// this into a shared helper would mean editing main.go, which a different
// lane owns — so the duplication is deliberate and annotated rather than
// hidden behind an abstraction neither side can wire.
func themeAssetMiddleware(themeHandler, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rel := strings.TrimPrefix(r.URL.Path, "/")
		if strings.Contains(rel, ".assets/") && themes.IsValidThemeID(strings.SplitN(rel, ".assets/", 2)[0]) {
			themeHandler.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// TestThemeAssetMiddleware_FallsThroughToEmbeddedHandler: a non-theme path
// (index.html, /assets/*.js, bare "/") must reach the embedded handler (next),
// not the theme handler — those files live in the Wails frontend embed, not on
// disk. A valid-id ".assets/" path routes to the theme handler; an invalid-id
// ".assets/" path falls through (the IsValidThemeID gate rejects it before the
// theme handler ever touches the filesystem).
func TestThemeAssetMiddleware_FallsThroughToEmbeddedHandler(t *testing.T) {
	themesDir := t.TempDir()
	img := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 9, 9}
	buildAssetTree(t, themesDir, "terra-test", "photo.png", img)

	themeHandler := themeAssetHandler(func() string { return themesDir })
	// embedded stands in for Wails' AssetFileServerFS. The marker header lets
	// the test prove a request reached `next` rather than being intercepted.
	embedded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Served-By", "embedded")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("embedded-bundle"))
	})
	srv := httptest.NewServer(themeAssetMiddleware(themeHandler, embedded))
	defer srv.Close()

	// Valid theme-asset path → theme handler serves the on-disk asset, and
	// must NOT reach the embedded handler.
	resp, err := http.Get(srv.URL + "/terra-test.assets/photo.png")
	if err != nil {
		t.Fatalf("GET theme asset: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.Header.Get("X-Served-By") == "embedded" {
		t.Error("valid theme-asset path routed to embedded handler; should have gone to the theme handler")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("theme asset status = %d, want 200", resp.StatusCode)
	}
	if !bytes.Equal(body, img) {
		t.Errorf("theme asset body = %v, want the on-disk asset bytes %v", body, img)
	}

	// Non-asset paths → embedded handler.
	for _, p := range []string{"/index.html", "/assets/app.js", "/"} {
		resp, err := http.Get(srv.URL + p)
		if err != nil {
			t.Fatalf("GET %s: %v", p, err)
		}
		resp.Body.Close()
		if got := resp.Header.Get("X-Served-By"); got != "embedded" {
			t.Errorf("%s: X-Served-By = %q, want %q (should fall through to embedded)", p, got, "embedded")
		}
	}

	// A ".assets/" segment with an INVALID theme id (uppercase is outside
	// the [a-z0-9_-] set) must fall through to embedded too — the middleware's
	// IsValidThemeID gate is what keeps malformed/probe paths off the theme
	// handler before it could touch the filesystem.
	resp, err = http.Get(srv.URL + "/Terra-Test.assets/x.png")
	if err != nil {
		t.Fatalf("GET invalid-id asset: %v", err)
	}
	resp.Body.Close()
	if got := resp.Header.Get("X-Served-By"); got != "embedded" {
		t.Errorf("invalid-id .assets path: X-Served-By = %q, want %q (should fall through, not reach theme handler)", got, "embedded")
	}
}
