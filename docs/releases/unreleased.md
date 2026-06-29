# Fixes

- **Added "Open DevTools on startup" toggle** in Settings > Editor. Requires `-devtools` build flag (now default). `Ctrl+Shift+F12` shortcut also available.
- **Version-scoped WebView2 cache** (`%APPDATA%/Silt/webview2/<version>/`) so upgrades never inherit a corrupted `EBWebView` folder from a previous install. This fixes blank-page rendering that affects some machines due to stale WebView2 browser cache (MicrosoftEdge/WebView2Feedback#2979).
- **Diagnostic console logging** (`[Silt]` / `[VSC]` prefixes) in the browser console to surface page-rendering state.
