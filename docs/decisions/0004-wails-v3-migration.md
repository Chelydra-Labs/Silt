# ADR 0004: Wails v3 Migration

## Status

Accepted — 2026-07-09

## Context

Silt was built on Wails v2.13.0, which provides a single-window webview
desktop framework with Go↔frontend IPC bindings. Wails v3 introduces
system tray, native menus, multi-window support, a Taskfile-based build
system, and a restructured service/binding model.

Milestone #40 (issues #493–#505) tracks the full migration. The owner
accepted v3 alpha stability ("the API is reasonably stable, and
applications are running in production") and removed macOS from scope
(owner does not use a Mac).

## Decision

Upgrade to Wails v3 `v3.0.0-alpha2.117` on a single migration branch.
Pin the exact version in `go.mod`, `package.json` (`@wailsio/runtime`
`3.0.0-alpha.97`), and CI (CLI installed from `go.mod` pin). Windows +
Linux only; macOS is out of scope.

### Key architectural decisions

1. **Single `App` service:** Register `*App` as one Wails v3 service via
   `application.NewServiceWithOptions`. Do not split into multiple
   services during the cutover — the 187 bound methods stay on one
   struct to preserve name/signature parity.

2. **Error envelope:** v3's `ServiceOptions.MarshalError` replaces v2's
   `ErrorFormatter`. `formatIPCError` now returns `[]byte` (valid JSON
   for `*IPCError` / `*CapabilityDeniedError`; `nil` to fall back to the
   default handler for unmigrated sentinels). The three stable error
   codes (`block_being_edited`, `vault_closing`, `capability_denied`)
   and Go-side `errors.Is` compatibility are preserved.

3. **Asset server:** v3 uses a single `AssetOptions.Handler` (not v2's
   two-tier embed+handler). The embedded `frontend/dist` is served via
   `AssetFileServerFS`; a `Middleware` intercepts `<themeID>.assets/`
   requests and routes them to `themeAssetHandler`. This preserves the
   v2 behavior (embed-first, dynamic theme assets on miss) with more
   explicit URL-pattern routing.

4. **Service lifecycle:** `App.ServiceStartup(ctx, options)` replaces
   `App.startup(ctx)`; `App.ServiceShutdown()` replaces
   `App.shutdown(ctx)`. The `wailsApp *application.App` field is set
   before `Run()` and via `application.Get()` in `ServiceStartup`.

5. **Event wrappers:** All `runtime.EventsEmit(a.ctx, name, data)` calls
   are replaced with `a.emit(name, data)`, which calls
   `a.wailsApp.Event.Emit(name, data)` and no-ops when `wailsApp` is nil
   (tests). Frontend `EventsOn(name, cb)` → `Events.On(name, (ev) =>
   cb(ev.data))` — the v3 callback receives a `WailsEvent` with `.data`.

6. **Dialog/clipboard/browser wrappers:** Thin wrapper methods
   (`openDirectoryDialog`, `openFileDialog`, `saveFileDialog`,
   `clipboardGetText`, `clipboardSetText`, `browserOpenURL`) encapsulate
   the v3 builder-pattern API. Call sites use the wrappers, not raw v3
   types.

7. **Build system:** v3 uses a Taskfile-based build. `Taskfile.yml` at
   the repo root includes platform-specific Taskfiles under `build/`.
   `wails3 build` dispatches to `windows:build` or `linux:build`;
   `wails3 dev` reads `build/config.yml` for dev-mode file watching and
   Vite proxying. `wails3 generate bindings` replaces `wails generate
   module`; output is `frontend/bindings/` (gitignored).

8. **Plugin isolation:** Session-token verification (`validatePluginSession`)
   is retained. Per-plugin isolated webviews (#502) are deferred to a
   follow-up — the Go-layer session/capability boundary remains the
   security control.

## Consequences

- `wails dev` → `wails3 dev`; `wails build` → `wails3 build`
- Frontend bindings move from `frontend/wailsjs/` to
  `frontend/bindings/`
- Frontend runtime moves from `wailsjs/runtime/runtime.js` to the
  `@wailsio/runtime` npm package
- `ARCHITECTURE.md` §4 (Wails Bridge) requires updating to describe the
  v3 service model (deferred to Phase 6)
- The `--wails-draggable` CSS property is preserved (v3 still supports it)
- File drop (`OnFileDrop`/`OnFileDropOff`) is stubbed pending v3's
  `data-file-drop-target` + `EnableFileDrop` window option migration
