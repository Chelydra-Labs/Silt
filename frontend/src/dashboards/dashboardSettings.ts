// IO wiring for the typed-notes dashboard saved views (#863). The pure
// coerce/load/strip/match helpers live in dashboardSavedViews.ts; this module
// is the thin read/write surface against the per-vault config snapshot
// (`settings.config.ui.dashboards.typed_notes.saved_views[]`).
//
// Reads traverse the reactive settings.config snapshot (so a component
// `$derived` that calls loadTypedNotesSavedViews re-runs on config:changed).
// Writes go through SetTypedNotesSavedViews — the Go-side TOCTOU-hardened
// targeted setter that re-reads config.yaml under the config lock before
// mutating the nested slice (#120/#475), so a concurrent external config edit
// is preserved rather than clobbered by a stale full-snapshot save. The local
// snapshot is mirrored optimistically after the Go write succeeds.

import { SetTypedNotesSavedViews } from '../../bindings/silt/app.js'
import { settings } from '../settings/store.svelte'
import {
  loadDashboardSavedViews,
  persistableDashboardSavedViews,
  type DashboardSavedView
} from './dashboardSavedViews'

// The config path: ui.dashboards.typed_notes.saved_views[].
const SEGMENTS = ['dashboards', 'typed_notes', 'saved_views'] as const

// Soft cap on persisted saved views — guards against unbounded config.yaml
// growth (mirrors MaxFavoritePages / MaxRecentPages). Views are appended at
// the tail on save, so keep the NEWEST MAX_SAVED_VIEWS (drop the oldest head)
// — past the cap, dropping the just-created view would silently vanish it.
const MAX_SAVED_VIEWS = 256

/**
 * The raw saved-views array from the live vault config (or [] when the path
 * is unset / config isn't loaded yet). Returns `unknown[]` so callers pipe
 * straight into the pure coercer without coupling to the config shape.
 */
export function readTypedNotesSavedViewsRaw(): unknown[] {
  const ui = settings.config?.ui as Record<string, unknown> | undefined
  let cur: unknown = ui
  for (const seg of SEGMENTS) {
    if (!cur || typeof cur !== 'object') return []
    cur = (cur as Record<string, unknown>)[seg]
  }
  return Array.isArray(cur) ? cur : []
}

/** Coerced + deduped saved views for the typed-notes dashboard. */
export function loadTypedNotesSavedViews(): DashboardSavedView[] {
  return loadDashboardSavedViews(readTypedNotesSavedViewsRaw())
}

/**
 * Persist the saved-views list via the TOCTOU-hardened Go setter. Strips
 * system views + the system marker, hands the opaque list to
 * SetTypedNotesSavedViews (which re-reads config.yaml under the lock, mutates
 * only the nested slice, and saves atomically), then mirrors the change into
 * the local snapshot. Returns null on success or an error message on failure
 * (fail-loud — the caller surfaces it in the toast rather than a generic
 * boolean).
 */
export async function persistTypedNotesSavedViews(
  views: DashboardSavedView[]
): Promise<string | null> {
  const cfg = settings.config
  if (!cfg) return 'Settings not loaded'
  let persistable = persistableDashboardSavedViews(views)
  if (persistable.length > MAX_SAVED_VIEWS) {
    // Keep the newest entries (saved views append at the tail): slice(-N)
    // retains the last MAX_SAVED_VIEWS and drops the oldest head.
    persistable = persistable.slice(-MAX_SAVED_VIEWS)
  }
  try {
    await SetTypedNotesSavedViews(persistable)
    // Optimistic mirror: the Go setter does not emit config:changed, so update
    // the local snapshot to match the persisted slice (keeps the reactive
    // loadTypedNotesSavedViews $derived in sync without a round-trip).
    if (!cfg.ui) cfg.ui = {} as typeof cfg.ui
    const ui = cfg.ui as unknown as Record<string, unknown>
    if (!ui.dashboards) ui.dashboards = {}
    const dashboards = ui.dashboards as Record<string, unknown>
    if (!dashboards.typed_notes) dashboards.typed_notes = {}
    const typedNotes = dashboards.typed_notes as Record<string, unknown>
    typedNotes.saved_views = persistable
    return null
  } catch (e) {
    return e instanceof Error ? e.message : String(e)
  }
}
