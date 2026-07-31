/**
 * Per-session note page zoom (#843). Scales note content only — not
 * editor.font_size_px or app chrome. Session-only for now; vault persist
 * can land later without changing call sites.
 */

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
  },
  zoomIn() {
    factor = clampNoteZoom(factor + NOTE_ZOOM_STEP)
  },
  zoomOut() {
    factor = clampNoteZoom(factor - NOTE_ZOOM_STEP)
  },
  reset() {
    factor = NOTE_ZOOM_DEFAULT
  }
}
