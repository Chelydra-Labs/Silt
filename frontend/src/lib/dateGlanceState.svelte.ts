import type { Editor } from '@tiptap/core'

// Shared state for the Date Glance popover (#730). One popover instance,
// rendered once in App.svelte, driven by this state.
//
// Placement (dual-mode):
// - Toolbar chip open → position against the registered chip element.
// - /calendar slash or editor-focused hotkey → position against an ephemeral
//   caret anchor built from coordsAtPos (same contract as table/math popovers).
// - Never fall back to document.body (that pinned the popover at the viewport
//   origin / top-left). Open is refused loudly when no placeable anchor exists.
//
// insertEditor is captured at open time (before the popover takes focus and
// blurs the editor), so the day-pick handler can re-focus + insert at the cursor.
//
// openGen increments on every successful open so the popover can detect re-opens
// (not just false→true transitions) and reset its view each time.

/** Caret / session placement rect (viewport coords from coordsAtPos). */
export interface DateGlancePlacementRect {
  top: number
  bottom: number
  left: number
}

export interface DateGlanceOpenOptions {
  /**
   * Prefer this caret/session rect over the chip. When set, an ephemeral
   * fixed-position element is mounted for Popover to measure.
   */
  rect?: DateGlancePlacementRect | null
}

export interface DateGlanceState {
  open: boolean
  /** Persistent chip element registered by DateGlanceChip. */
  anchor: HTMLElement | null
  /**
   * Element Popover should measure this open session.
   * Chip open → chip element; caret open → ephemeral placement node.
   * Null when closed or when open was refused.
   */
  activeAnchor: HTMLElement | null
  insertEditor: Editor | null
  /** Bumped on every successful open; lets consumers detect re-opens. */
  openGen: number
}

export const dateGlance: DateGlanceState = $state({
  open: false,
  anchor: null,
  activeAnchor: null,
  insertEditor: null,
  openGen: 0
})

/** Ephemeral caret marker; owned by this module, removed on close. */
let sessionPlacementEl: HTMLElement | null = null

function clearSessionPlacement(): void {
  if (sessionPlacementEl) {
    sessionPlacementEl.remove()
    sessionPlacementEl = null
  }
  if (
    dateGlance.activeAnchor &&
    dateGlance.activeAnchor !== dateGlance.anchor
  ) {
    dateGlance.activeAnchor = null
  }
}

/**
 * Mount (or reuse) a zero-size fixed marker at the caret rect so the shared
 * Popover can measure it with getBoundingClientRect — no Popover API fork.
 */
function mountSessionPlacement(rect: DateGlancePlacementRect): HTMLElement {
  clearSessionPlacement()
  const el = document.createElement('div')
  el.setAttribute('data-date-glance-placement', '')
  el.setAttribute('aria-hidden', 'true')
  // Zero-size fixed marker at the caret; Popover measures bottom+gap below it.
  Object.assign(el.style, {
    position: 'fixed',
    left: `${rect.left}px`,
    top: `${rect.top}px`,
    width: '0px',
    height: `${Math.max(0, rect.bottom - rect.top)}px`,
    pointerEvents: 'none',
    visibility: 'hidden'
  })
  document.body.appendChild(el)
  sessionPlacementEl = el
  return el
}

function isLiveAnchor(el: HTMLElement | null): el is HTMLElement {
  return !!el && el.isConnected
}

/**
 * Resolve the element Popover should anchor to for this open.
 * Order: explicit caret rect → live chip → null (refuse open).
 */
function resolveActiveAnchor(
  options?: DateGlanceOpenOptions
): HTMLElement | null {
  if (options?.rect) {
    return mountSessionPlacement(options.rect)
  }
  if (isLiveAnchor(dateGlance.anchor)) {
    return dateGlance.anchor
  }
  return null
}

/** Register the persistent anchor element (the status-bar chip) on mount. */
export function setDateGlanceAnchor(el: HTMLElement | null): void {
  dateGlance.anchor = el
  // If the glance is open on the chip and the chip unmounts, drop activeAnchor
  // so we never measure a detached node. Session placement is independent.
  if (
    dateGlance.open &&
    dateGlance.activeAnchor &&
    dateGlance.activeAnchor !== sessionPlacementEl &&
    !isLiveAnchor(dateGlance.activeAnchor)
  ) {
    dateGlance.activeAnchor = null
  }
}

/**
 * Read caret viewport coords from a TipTap editor. Returns null and logs when
 * coordsAtPos fails (destroyed view, bad pos) — callers must not invent a
 * top-left fallback.
 */
export function caretRectFromEditor(
  editor: Editor
): DateGlancePlacementRect | null {
  try {
    if (editor.isDestroyed) return null
    const { selection } = editor.state
    const c = editor.view.coordsAtPos(selection.from)
    return { top: c.top, bottom: c.bottom, left: c.left }
  } catch (e) {
    console.error('[silt] date-glance caret coords failed:', e)
    return null
  }
}

/**
 * Open Date Glance. Returns true when opened with a placeable anchor.
 * Refuses (false + console.error) when neither a caret rect nor a live chip
 * is available — never anchors to document.body.
 */
export function openDateGlance(
  insertEditor: Editor | null = null,
  options?: DateGlanceOpenOptions
): boolean {
  // Drop any prior session marker before resolving the next placement.
  if (sessionPlacementEl) {
    sessionPlacementEl.remove()
    sessionPlacementEl = null
  }

  const active = resolveActiveAnchor(options)
  if (!active) {
    console.error(
      '[silt] date-glance open refused: no placeable anchor (chip missing and no caret rect)'
    )
    dateGlance.open = false
    dateGlance.activeAnchor = null
    dateGlance.insertEditor = null
    return false
  }

  dateGlance.activeAnchor = active
  dateGlance.insertEditor = insertEditor
  dateGlance.open = true
  dateGlance.openGen++
  return true
}

/**
 * Open near the editor caret when possible; otherwise the chip.
 * Used by /calendar and the global hotkey.
 */
export function openDateGlanceNearEditor(editor: Editor | null): boolean {
  if (editor && !editor.isDestroyed) {
    const rect = caretRectFromEditor(editor)
    if (rect) return openDateGlance(editor, { rect })
    // Coords failed: chip if live, else refuse (no body / no magic coords).
    return openDateGlance(editor)
  }
  return openDateGlance(null)
}

/**
 * Toggle the popover. When opening with an editor, prefer caret placement
 * (hotkey path). Chip clicks call openDateGlance directly without a rect.
 */
export function toggleDateGlance(insertEditor: Editor | null = null): boolean {
  if (dateGlance.open) {
    closeDateGlance()
    return false
  }
  return openDateGlanceNearEditor(insertEditor)
}

export function closeDateGlance(): void {
  dateGlance.open = false
  dateGlance.insertEditor = null
  dateGlance.activeAnchor = null
  clearSessionPlacement()
}

/**
 * Drop the insert target. Called when the editor unmounts (page switch) so a
 * destroyed editor doesn't receive a stale insert. Silt has one editor at a
 * time (one page = one TipTap instance), so clearing unconditionally is safe —
 * a $state proxy makes reference-equality comparison unreliable anyway.
 */
export function clearInsertEditor(): void {
  dateGlance.insertEditor = null
}
