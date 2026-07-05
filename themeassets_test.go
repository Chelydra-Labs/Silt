package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
