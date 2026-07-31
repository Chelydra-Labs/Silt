/**
 * Per-vault note page zoom (#843 UI, #849 persist). Scales note content only —
 * not editor.font_size_px or app chrome. Factor is mirrored into ui.note_zoom
 * via an atomic Go setter so wheel updates never clobber settings drafts.
 */

import { SetNoteZoom } from '../../bindings/silt/app.js'
import { settings } from '../settings/store.svelte'

// Bounds sit on 0.1 steps from 1.0 (0.75 is not on that grid).
export const NOTE_ZOOM_MIN = 0.7
export const NOTE_ZOOM_MAX = 2.0
export const NOTE_ZOOM_STEP = 0.1
export const NOTE_ZOOM_DEFAULT = 1

/** Snap to 0.1 steps via integer tenths, then clamp. */
export function clampNoteZoom(value: number): number {
  const tenths = Math.round(value * 10)
  const minT = Math.round(NOTE_ZOOM_MIN * 10)
  const maxT = Math.round(NOTE_ZOOM_MAX * 10)
  return Math.min(maxT, Math.max(minT, tenths)) / 10
}

let factor = $state(NOTE_ZOOM_DEFAULT)
let persistTimer: ReturnType<typeof setTimeout> | null = null
let hydrated = false

function schedulePersist(next: number) {
  if (!hydrated) return
  if (persistTimer) clearTimeout(persistTimer)
  persistTimer = setTimeout(() => {
    persistTimer = null
    void persistZoom(next)
  }, 200)
}

async function persistZoom(value: number) {
  const cfg = settings.config
  if (!cfg?.ui) return
  try {
    await SetNoteZoom(value)
    ;(cfg.ui as { note_zoom?: number }).note_zoom = value
  } catch {
    /* fail soft — local factor still applies for the session */
  }
}

export const noteZoom = {
  get factor() {
    return factor
  },
  /** Whole-number percent for labels (e.g. 100). */
  get percent() {
    return Math.round(factor * 100)
  },
  setFactor(value: number) {
    factor = clampNoteZoom(value)
    schedulePersist(factor)
  },
  zoomIn() {
    factor = clampNoteZoom(factor + NOTE_ZOOM_STEP)
    schedulePersist(factor)
  },
  zoomOut() {
    factor = clampNoteZoom(factor - NOTE_ZOOM_STEP)
    schedulePersist(factor)
  },
  reset() {
    factor = NOTE_ZOOM_DEFAULT
    schedulePersist(factor)
  },
  /**
   * Apply vault config without writing back (load / config:changed).
   * Call once settings.config is available.
   */
  hydrateFromConfig(raw: unknown) {
    const n =
      typeof raw === 'number' && Number.isFinite(raw)
        ? clampNoteZoom(raw)
        : NOTE_ZOOM_DEFAULT
    factor = n
    hydrated = true
  }
}
