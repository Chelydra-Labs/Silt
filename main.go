package main

import (
	"embed"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"silt/backend/config"
	"silt/backend/themes"
	"silt/backend/vault"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

// launchBackgroundColour resolves the OS-level window paint colour shown
// before the webview renders. It reads the stored ThemeMode + active theme
// id, resolves the effective mode's bg.void from the in-process theme
// cache (or the embedded default as the final fallback), and converts it
// to RGBA. This removes the pre-CSS flash and tracks the active theme
// even when it is not the embedded default (#73). A custom theme with a
// different bg.void used to show the default void for a few ms until the
// runtime injector caught up; the cache lookup short-circuits that gap
// because it serves the on-disk theme from a single read at startup.
func launchBackgroundColour() application.RGBA {
	fallback := func() application.RGBA { return application.RGBA{Red: 12, Green: 12, Blue: 14, Alpha: 1} }
	settings, err := vault.LoadSettings()
	if err != nil && !errors.Is(err, vault.ErrSettingsFingerprintMismatch) {
		// No settings → no active id → embedded default bg.void (always
		// available from the binary).
		if th, perr := themes.ParseDefault(); perr == nil {
			mode := effectiveMode("")
			r, g, b, ok := themes.HexToRGB(th.BGVoid(mode))
			if ok {
				return application.RGBA{Red: r, Green: g, Blue: b, Alpha: 1}
			}
		}
		return fallback()
	}
	mode := effectiveMode(settings.ThemeMode)
	themesDir := ""
	if settings.VaultPath != "" {
		themesDir = filepath.Join(settings.VaultPath, ".system", "themes")
	}
	th, err := themes.CachedThemeByID(themesDir, settings.ActiveTheme)
	if err != nil {
		if th, perr := themes.ParseDefault(); perr == nil {
			r, g, b, ok := themes.HexToRGB(th.BGVoid(mode))
			if ok {
				return application.RGBA{Red: r, Green: g, Blue: b, Alpha: 1}
			}
		}
		return fallback()
	}
	r, g, b, ok := themes.HexToRGB(th.BGVoid(mode))
	if !ok {
		return fallback()
	}
	return application.RGBA{Red: r, Green: g, Blue: b, Alpha: 1}
}

func shouldOpenDevtools() bool {
	if strings.EqualFold(os.Getenv("SILT_DEBUG"), "1") {
		return true
	}
	settings, err := vault.LoadSettings()
	if err != nil || settings.VaultPath == "" {
		return false
	}
	cfg, err := config.Load(settings.VaultPath)
	if err != nil {
		return false
	}
	return cfg.UI.OpenDevtoolsOnStartup != nil && *cfg.UI.OpenDevtoolsOnStartup
}

func clearCacheOnVersionChange(cacheDir, currentVersion string) {
	markerFile := filepath.Join(cacheDir, ".silt-version")
	stored, err := os.ReadFile(markerFile)
	if err == nil && strings.TrimSpace(string(stored)) == currentVersion {
		return
	}
	os.RemoveAll(cacheDir)
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return
	}
	os.WriteFile(markerFile, []byte(currentVersion), 0644)
}

func main() {
	app := NewApp()

	// Single WebView2 cache folder, cleared on version change to prevent
	// stale EBWebView corruption from carrying over across upgrades (#342).
	webviewCacheDir := filepath.Join(os.Getenv("APPDATA"), "Silt", "webview2")
	if os.Getenv("APPDATA") == "" {
		home, _ := os.UserHomeDir()
		webviewCacheDir = filepath.Join(home, ".config", "silt", "webview2")
	}
	clearCacheOnVersionChange(webviewCacheDir, appVersion)

	// The embed directive captures frontend/dist/* with the directory
	// prefix. Sub it to root so AssetFileServerFS serves /index.html etc.
	frontendFS, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatalf("failed to create frontend sub-FS: %v", err)
	}

	// Theme asset handler: serves per-theme background images
	// (<themeID>.assets/<file>) from the CURRENT themes directory, which
	// is dynamic (vault open/switch after startup). The resolver reads
	// vaultPath under vaultMu on each request.
	themeHandler := themeAssetHandler(func() string {
		app.vaultMu.RLock()
		defer app.vaultMu.RUnlock()
		return app.themesDir()
	})

	wailsApp := application.New(application.Options{
		Name: "Silt",
		Services: []application.Service{
			application.NewServiceWithOptions(app, application.ServiceOptions{
				// #478: serialize IPCError-carriers as a JSON string on
				// the Wails error envelope so the frontend can map on a
				// stable code instead of substring-matching Go prose.
				MarshalError: formatIPCError,
			}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(frontendFS),
			// Intercept theme background asset requests (<id>.assets/<file>)
			// before they hit the embed handler — those files are not in the
			// embed and are resolved from the dynamic themes directory.
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					rel := strings.TrimPrefix(r.URL.Path, "/")
					if strings.Contains(rel, ".assets/") {
						themeHandler.ServeHTTP(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
		Windows: application.WindowsOptions{
			WebviewUserDataPath: webviewCacheDir,
		},
	})

	// Set the app reference before Run so ServiceStartup (and every IPC
	// handler) can emit events, open dialogs, etc. application.Get() is
	// the fallback used inside ServiceStartup.
	app.wailsApp = wailsApp

	// Main window — frameless, maximized, with the theme-aware launch colour.
	wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:                  "Silt",
		Width:                  1024,
		Height:                 768,
		StartState:             application.WindowStateMaximised,
		Frameless:              true,
		BackgroundColour:       launchBackgroundColour(),
		OpenInspectorOnStartup: shouldOpenDevtools(),
	})

	if err := wailsApp.Run(); err != nil {
		log.Printf("Error: %s", err.Error())
	}
}
