// IO wiring for the typed-notes dashboard saved views (#863). The pure
// coerce/load/strip/match helpers live in dashboardSavedViews.ts; this module
// is the thin read/write surface against the per-vault config snapshot
// (`settings.config.ui.dashboards.typed_notes.saved_views[]`).
//
// Reads traverse the reactive settings.config snapshot (so a component
// `$derived` that calls loadTypedNotesSavedViews re-runs on config:changed).
// Writes mutate the snapshot in place + SaveSystemConfig it directly — NOT
// saveConfig, which clears settings.dirty and would clobber an unsaved
// Settings-panel draft. Mirrors noteZoom's atomic-write discipline.

import { SaveSystemConfig } from '../../bindings/silt/app.js'
import { settings } from '../settings/store.svelte'
import {
  loadDashboardSavedViews,
  persistableDashboardSavedViews,
  type DashboardSavedView
} from './dashboardSavedViews'

// The config path: ui.dashboards.typed_notes.saved_views[].
const SEGMENTS = ['dashboards', 'typed_notes', 'saved_views'] as const

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
 * Persist the saved-views list. Strips system views + the system marker,
 * writes into ui.dashboards.typed_notes.saved_views, then atomically
 * SaveSystemConfig's the full snapshot. Returns false when config isn't
 * loaded yet or the write throws (fail-soft — the in-memory list still
 * reflects the user's intent for the session).
 */
export async function persistTypedNotesSavedViews(
  views: DashboardSavedView[]
): Promise<boolean> {
  const cfg = settings.config
  if (!cfg) return false
  if (!cfg.ui) cfg.ui = {} as typeof cfg.ui
  const ui = cfg.ui as unknown as Record<string, unknown>
  if (!ui.dashboards) ui.dashboards = {}
  const dashboards = ui.dashboards as Record<string, unknown>
  if (!dashboards.typed_notes) dashboards.typed_notes = {}
  const typedNotes = dashboards.typed_notes as Record<string, unknown>
  typedNotes.saved_views = persistableDashboardSavedViews(views)
  try {
    await SaveSystemConfig(cfg)
    return true
  } catch {
    return false
  }
}
