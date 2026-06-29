# Fixes

- **Added Dev tab in Settings** with the "Open DevTools on startup" toggle and instructions for `Ctrl+Shift+F12`.
- **WebView2 cache auto-cleared on version upgrade** via a `.silt-version` marker file. Same-version restarts preserve the cache; upgrades get a clean slate.
- **CI: force-clean Vite cache and dist before frontend build** to prevent stale build artifacts from embedding corrupted assets into the production binary.
