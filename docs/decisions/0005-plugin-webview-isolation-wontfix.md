# ADR 0005: Per-Plugin Isolated Webviews — Will Not Implement

## Status

Accepted — 2026-07-10

## Context

Issue #502 (unblocking #151 pin-pluginID-at-registration and #152
per-plugin isolated webviews) proposed moving each plugin's JavaScript
out of the main webview and into a dedicated, capability-restricted
Wails v3 window. This was a headline motivation for the Wails v3
upgrade: the main webview would no longer host untrusted plugin JS, and
plugin capabilities would be enforced per-plugin-webview via the Wails 3
capability model, on top of the existing Go-layer session verification.

ADR 0004 (Wails v3 Migration) deferred #502 to a follow-up while the
Go-layer session/capability boundary remained the security control. This
ADR records the outcome of that follow-up investigation.

## Investigation

The Wails v3 multi-window and capability APIs were studied against the
`wailsapp/wails` `v3.0.0-alpha2.117` source (the migration's pinned
version). Findings:

1. **Multi-window is supported.** `application.NewWindow` /
   `app.Window.NewWithOptions` create additional webview windows at
   runtime, each with its own `URL`/`HTML`, and `window.Close()` tears
   them down. A hidden worker-style window is possible (`Hidden: true`).
   So the *mechanical* "one window per plugin" idea is feasible.

2. **Go bindings are application-global.** This is the blocking finding.
   Every service registered via `application.NewService` is reachable
   from **every** window. There is no per-window binding whitelist, no
   per-window capability scope, and no mechanism to restrict which bound
   methods a given window may call. The `WebviewWindowOptions.Permissions`
   map only covers webview-level OS permissions (camera, microphone,
   geolocation, clipboard, notifications) — not Go-binding access. The
   `app.Capabilities()` struct describes platform features
   (`HasNativeDrag`, `GTKVersion`, `WebKitVersion`), not permission
   enforcement.

3. Therefore #502's acceptance criterion #2 — *"Plugin capabilities are
   enforced per-plugin-webview via the Wails 3 capability model"* — is
   **not achievable** with the current Wails v3 API. The capability
   model the issue assumed exists does not.

A per-window isolation layer could only be built by hand: a custom Go
middleware that injects window identity into every `Plugin*` binding
call and enforces pluginID↔window binding at the application layer
(Wails will not). That is a large, novel security surface, hard to
audit, and it would still not give the OS/process-level sandboxing that
motivated the issue. The cost is disproportionate to the security gain
because the load-bearing control already exists.

## Decision

**Do not implement per-plugin isolated webviews.** The existing security
boundary remains authoritative:

- **Go-layer session verification** — `validatePluginSession(pluginID,
  token)` runs before every privileged `Plugin*` binding, so a plugin
  cannot invoke another plugin's session or call a raw binding with a
  foreign pluginID.
- **Go-layer capability grants** — `requireGrant(pluginID, capability)`
  gates every privileged capability (`read-files`, `write-files`,
  `network`, `os-open`, `os-clipboard`, `os-notify`, `ui-surface`,
  `editor-schema`, `content-mutate`, `plugin-db`, `ai`).
- **Iframe sandbox for rendered plugin UI** — third-party plugin UI
  surfaces render in a sandboxed `<iframe srcdoc>` with a restrictive
  CSP (`connect-src 'none'`); all network traffic routes through the
  postMessage bridge → Go-proxied `PluginFetch` (SSRF-defended,
  rate-limited, audit-logged). Plugin JS that drives logic-only plugins
  loads in the main webview but reaches the backend only through the
  same gated, session-token-checked bindings.

Together these enforce the property #502 was after — a plugin cannot
escalate beyond its granted capabilities or impersonate another plugin —
without per-window binding isolation that Wails v3 cannot provide.

## Consequences

- **#502 is closed as `wontfix`.** #151 and #152 remain blocked on a
  Wails v3 capability (per-window binding scoping) that does not exist
  today.
- **ARCHITECTURE.md §7.2** records this verdict and points here; the
  iframe sandbox + Go-layer session/capability checks are documented as
  the security boundary (they already were).
- **Revisit if** Wails v3 ships per-window binding scoping (or a
  per-frame permission model that covers Go bindings). Until then, the
  iframe sandbox is the rendering-isolation primitive and the Go layer
  is the authority-isolation primitive.
- The `PluginContext` SDK public API is unchanged; first-party and
  third-party plugins load and operate exactly as today.
