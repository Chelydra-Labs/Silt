/**
 * Keyboard invocation of context menus (Menu key / Shift+F10), shared by every
 * surface that hosts a context menu so the key detection and event synthesis
 * live in one place.
 */

/** True for the Menu key and for Shift+F10 — the two standard "open context menu" keys. */
export function isContextMenuKey(event: KeyboardEvent): boolean {
  return event.key === 'ContextMenu' || (event.shiftKey && event.key === 'F10')
}

/**
 * Synthesize a contextmenu event at the element's bottom-left corner (where a
 * keyboard-invoked menu should appear) and dispatch it on the element, so the
 * exact same per-node `oncontextmenu` handler runs for keyboard and mouse.
 */
export function openContextMenuFromKeyboard(el: HTMLElement): void {
  const rect = el.getBoundingClientRect()
  el.dispatchEvent(
    new MouseEvent('contextmenu', {
      bubbles: true,
      clientX: rect.left,
      clientY: rect.bottom
    })
  )
}
