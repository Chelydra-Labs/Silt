package main

// EventName is the canonical set of Wails event names emitted by the Go backend
// and the frontend's @wailsio/runtime listeners. This package owns the single
// source of truth; cmd/genenums parses this const block and emits a matching TS
// module (frontend/src/generated/enums.ts) so the frontend consumes generated
// values instead of re-typing bare string literals. Adopting these consts at
// the emit/listen sites happens in a follow-up phase; this file is additive.
//
// Naming smell notes (intentional, documented here so a future editor does not
// "fix" them):
//   - theme:changed (singular) carries a specific theme's applied tokens; the
//     frontend swaps CSS in place. themes:changed (plural) signals the
//     available-themes list changed (import/delete). The split is semantic, not
//     a typo — both must stay.
//   - linked-config:changed vs linked-notebook:quarantined use different first
//     segments by design: one is a config-file event, the other a notebook
//     lifecycle event.
//   - The ai:complete:* bases are owner-scoped at emit time (the producer
//     appends ":<pluginID>"); the frontend subscribes with the same suffix. The
//     base consts here are the prefix only.
type EventName string

const (
	// Block / content mutations.
	EventBlockChanged EventName = "block:changed"

	// Configuration.
	EventConfigChanged               EventName = "config:changed"
	EventConfigError                 EventName = "config:error"
	EventLinkedConfigChanged         EventName = "linked-config:changed"
	EventLinkedNotebookQuarantined   EventName = "linked-notebook:quarantined"
	EventSettingsFingerprintMismatch EventName = "settings:fingerprint-mismatch"
	EventGrantsMigrationRequired     EventName = "grants:migration-required"

	// Plugins.
	EventPluginsChanged EventName = "plugins:changed"
	EventSecurityEvent  EventName = "security:event"

	// Templates.
	EventTemplatesChanged EventName = "templates:changed"

	// Note types (typed-notes feature). Emitted by the type watcher on
	// external schema edits and by SaveType/DeleteType so typed pages + the type
	// manager stay live.
	EventTypesChanged EventName = "types:changed"

	// Emitted when a typed-page projection fails so the UI can surface a
	// stale-dashboard warning instead of silently drifting.
	EventTypesProjectionError EventName = "types:projection-error"

	// Emitted by the reprojection worker as a batch progresses so the type
	// dashboard can show a non-blocking progress indicator. Payload is
	// map[string]any{"state": "running"|"done", "processed": uint64,
	// "total": uint64}. A "running" emit carries the current processed count;
	// the final "done" emit signals the batch completed.
	EventTypesReprojectionProgress EventName = "types:reprojection:progress"

	// Themes (singular vs plural is intentional — see the doc comment above).
	EventThemeChanged      EventName = "theme:changed"
	EventThemesChanged     EventName = "themes:changed"
	EventThemeFilesDropped EventName = "theme:files-dropped"

	// Vault lifecycle.
	EventVaultClosing         EventName = "vault:closing"
	EventVaultMoved           EventName = "vault:moved"
	EventVaultInitError       EventName = "vault:init-error"
	EventVaultInitWarnings    EventName = "vault:init-warnings"
	EventVaultWatchCoverage   EventName = "vault:watch-coverage"
	EventVaultArchiveProgress EventName = "vault:archive:progress"

	// Index / page links.
	EventIndexReMintWarning EventName = "index:re-mint-warning"
	EventPageLinksRewritten EventName = "page-links:rewritten"

	// Self-update / spellcheck download progress.
	EventUpdateDownloadProgress     EventName = "update:download:progress"
	EventSpellcheckDownloadProgress EventName = "spellcheck:download:progress"

	// AI streaming chat completions — owner-scoped bases (":<pluginID>" appended at emit).
	EventAICompleteDelta     EventName = "ai:complete:delta"
	EventAICompleteDone      EventName = "ai:complete:done"
	EventAICompleteError     EventName = "ai:complete:error"
	EventAICompleteToolDelta EventName = "ai:complete:tool-delta"

	// Native menu actions.
	EventMenuNewPage             EventName = "menu:new-page"
	EventMenuOpenVault           EventName = "menu:open-vault"
	EventMenuSave                EventName = "menu:save"
	EventMenuToggleSidebar       EventName = "menu:toggle-sidebar"
	EventMenuToggleFormatToolbar EventName = "menu:toggle-format-toolbar"
	EventMenuFind                EventName = "menu:find"
	EventMenuFocusMode           EventName = "menu:focus-mode"
	EventMenuSettings            EventName = "menu:settings"
	EventMenuAbout               EventName = "menu:about"
)
