// Shared focus-trap utility for blocking overlay dialogs. Handles Tab /
// Shift+Tab wrap so focus stays inside the dialog; each surface keeps its own
// Esc handling (dialogs do different things on Esc). Created so the
// properties-family dialogs (and the new PropertiesEditModal) share one
// focusable-element query instead of the copy-pasted selectors that drifted
// across the older dialogs (omitting `select`/`textarea`/`a[href]`/
// `[contenteditable]`).
//
// This is a committed seed: the broader hand-rolled-modal consolidation
// (ConfirmDialog/ChoiceDialog/NamePromptDialog/BlockPickerModal/
// TaskSubEditorModal) is tracked separately and governed by
// docs/decisions/0009-modal-primitive.md.
//
// Known gaps (intentional — no current consumer trips them):
//  - Positive `tabindex` elements are returned in DOM order, not tabindex-
//    value order. The spec's sequential-focus order ranks by tabindex value,
//    but every dialog here uses tabindex 0/-1 only, so DOM order is correct.
//  - `querySelectorAll` does not pierce shadow boundaries. None of the
//    consumed dialogs host a shadow-DOM web component with focusables today.

// Spec-aligned candidate set for sequential focus (HTML "tabindex focus flag"
// + "actually focusable" proxies). Includes the native form controls +
// editing hosts the prior hand-rolled traps omitted.
const FOCUSABLE_SELECTOR = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  '[tabindex]:not([tabindex="-1"])',
  '[contenteditable=""]',
  '[contenteditable="true"]',
  'audio[controls]',
  'video[controls]'
].join(',')

/**
 * Whether an element should actually receive focus. The selector above already
 * excludes `:disabled`; this filters the remaining non-tabbable cases that a
 * CSS selector cannot express reliably: `hidden`, the `inert` subtree,
 * `aria-hidden="true"` subtrees, and content nested inside a closed `<details>`.
 *
 * Size/offsetParent probing is intentionally omitted: jsdom has no layout
 * engine (so offsetWidth/Height are always 0 and offsetParent always null,
 * which would filter every element), and the dialogs that consume this trap
 * never hide a focusable element with CSS — their focusable set is always
 * visible. `display:none`/`visibility:hidden` elements are already skipped by
 * the browser's own sequential-focus walk, so the trap (which only intervenes
 * at the first/last boundary) does not need to detect them.
 */
function isTabbable(el: HTMLElement): boolean {
  if (el.hidden) return false
  if (el.hasAttribute('disabled')) return false
  // A negative tabindex removes the element from sequential focus order (it
  // can still be .focus()-ed programmatically, but Tab must skip it). The bare
  // `button/input/...:not([disabled])` selectors otherwise wrongly include the
  // backdrop "click-to-close" sentinel buttons (tabindex="-1") every overlay
  // dialog in the repo uses — they'd become the first/last wrap target.
  const tabindex = el.getAttribute('tabindex')
  if (tabindex !== null && Number.parseInt(tabindex, 10) < 0) return false
  // `inert` makes a whole subtree non-tabbable.
  if (el.closest('[inert]')) return false
  // `aria-hidden="true"` hides from the a11y tree (and thus focus order).
  if (el.closest('[aria-hidden="true"]')) return false
  // Content inside a closed <details> is not rendered and not tabbable.
  const details = el.closest('details')
  if (details && !details.hasAttribute('open')) return false
  return true
}

/** Ordered list of tabbable elements within `container` (empty when none). */
export function getFocusable(container: HTMLElement): HTMLElement[] {
  const candidates = Array.from(
    container.querySelectorAll<HTMLElement>(FOCUSABLE_SELECTOR)
  )
  return candidates.filter(isTabbable)
}

/**
 * Install a Tab / Shift+Tab focus trap scoped to `container`. Returns a
 * disposer that removes the listener. Only wraps focus — each surface owns its
 * own Escape behavior. Installs on `window` in the capture phase (matches the
 * existing per-dialog pattern) so a Tab anywhere resolves against this
 * container's focusable set.
 */
export function trapFocus(container: HTMLElement): () => void {
  function onKeydown(e: KeyboardEvent): void {
    if (e.key !== 'Tab') return
    const els = getFocusable(container)
    if (els.length === 0) return
    const first = els[0]
    const last = els[els.length - 1]
    const active = document.activeElement as HTMLElement | null
    if (e.shiftKey) {
      if (active === first || !container.contains(active)) {
        e.preventDefault()
        last.focus()
      }
    } else if (active === last || !container.contains(active)) {
      e.preventDefault()
      first.focus()
    }
  }
  window.addEventListener('keydown', onKeydown, true)
  return () => window.removeEventListener('keydown', onKeydown, true)
}
