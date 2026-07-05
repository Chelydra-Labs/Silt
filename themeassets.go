package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"silt/backend/themes"
)

// themeAssetHandler serves per-theme background assets
// (<themeID>.assets/<file>) from the current themes directory. The themes
// directory is DYNAMIC — it is <vault>/.system/themes, resolved from
// App.vaultPath at runtime (a vault can be opened or switched after the
// server is configured) — so the handler takes a resolver closure instead
// of capturing a static startup path.
//
// Wails' AssetServer tries the embedded frontend Assets first and only
// falls through to Handler on an os.ErrNotExist miss; large background
// image references (e.g. url("<themeID>.assets/<file>")) are not in the
// embed, so every such GET lands here. The handler is fully responsible
// for Content-Type (http.ServeContent sniffs + handles Range/Last-Modified)
// and for path sanitization — req.URL.Path is the RAW, un-cleaned path, so
// a ".." segment must be rejected before it reaches the filesystem.
func themeAssetHandler(themesDirResolver func() string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only GET/HEAD make sense for asset serving; ServeContent handles
		// HEAD itself. Anything else is a miss.
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			log.Printf("themeasset: 404 %s (method %s not allowed)", r.URL.Path, r.Method)
			http.NotFound(w, r)
			return
		}
		// The path arrives as "/<themeID>.assets/<file>" (Wails serves the
		// webview from a single host root). Strip the leading slash so the
		// "<id>.assets/" split is unambiguous.
		rel := strings.TrimPrefix(r.URL.Path, "/")
		idx := strings.Index(rel, ".assets/")
		if idx <= 0 {
			// No "<id>.assets/" segment, or empty id prefix — not ours.
			// Letting NotFound fall through is correct: Wails already
			// attempted the embedded Assets, so this is genuinely missing.
			log.Printf("themeasset: 404 %s (no <id>.assets/ segment)", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		themeID := rel[:idx]
		if !themes.IsValidThemeID(themeID) {
			// Reject path-traversal / format tricks at the id boundary
			// before any filesystem touch.
			log.Printf("themeasset: 404 %s (invalid theme id)", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		filename := rel[idx+len(".assets/"):]
		if !isSafeAssetFilename(filename) {
			// A traversal attempt or absolute path — refuse rather than 404
			// so a probe is distinguishable from a benign miss in logs.
			log.Printf("themeasset: 403 %s (unsafe filename shape)", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if _, ok := themes.AllowedBackgroundExts[strings.ToLower(filepath.Ext(filename))]; !ok {
			// Mirrors the ingestion gate: StoreBackgroundAsset only writes
			// image extensions, so a non-image here was dropped by a sync
			// client. Treat as not-found — never confirm it exists.
			log.Printf("themeasset: 404 %s (disallowed extension)", r.URL.Path)
			http.NotFound(w, r)
			return
		}

		themesDir := themesDirResolver()
		if themesDir == "" {
			// No vault open yet — nothing to serve.
			log.Printf("themeasset: 404 %s (no vault open)", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		assetsRoot := filepath.Join(themesDir, themeID+".assets")
		// filepath.Clean + a prefix check on the resolved path is the
		// CWE-22 backstop: even if isSafeAssetFilename somehow admitted a
		// traversal-shaped name, the join + clean + prefix check still
		// confines the read to assetsRoot.
		fullPath := filepath.Join(assetsRoot, filepath.Clean(filename))
		if !isWithinDir(fullPath, assetsRoot) {
			log.Printf("themeasset: 403 %s (escapes assets dir)", r.URL.Path)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		f, err := os.Open(fullPath)
		if err != nil {
			log.Printf("themeasset: 404 %s (open failed)", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			log.Printf("themeasset: 404 %s (stat failed)", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if fi.IsDir() {
			log.Printf("themeasset: 404 %s (is directory)", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		// Defense against an SVG (or a sniffed-as-html file) being
		// interpreted as an active document in the webview: even though
		// ServeContent sets an image Content-Type, lock the response to
		// image-only so a future content-type mix-up can't execute
		// script. The webview loads these via url() in CSS, which is
		// image-context only.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; img-src 'self'")
		// ServeContent sets Content-Type via sniffing when the response
		// header is unset, and handles Range requests + If-Modified-Since,
		// both of which the webview's image loader emits.
		http.ServeContent(w, r, fi.Name(), fi.ModTime(), f)
	})
}

// isSafeAssetFilename accepts only a single path component (no separators,
// no parent-dir traversal, no absolute/drive prefix). Mirrors the intent of
// sanitizeBgFilename in backend/themes/background.go: stored asset names
// are always one safe component of the form "<slug>-<hash>.<ext>", so a URL
// that doesn't fit that shape is hostile or broken.
func isSafeAssetFilename(name string) bool {
	if name == "" || name == "." || name == ".." {
		return false
	}
	if filepath.IsAbs(name) {
		return false
	}
	if strings.ContainsAny(name, `/\`) {
		return false
	}
	if strings.Contains(name, "..") {
		return false
	}
	return true
}

// isWithinDir reports whether path is dir itself or lives directly under it,
// using cleaned paths and the OS-specific separator so a "<root>" prefix
// cannot match an unrelated "<root>-evil" sibling directory.
func isWithinDir(path, dir string) bool {
	cleanPath := filepath.Clean(path)
	cleanDir := filepath.Clean(dir)
	if cleanPath == cleanDir {
		return true
	}
	return strings.HasPrefix(cleanPath, cleanDir+string(filepath.Separator))
}
