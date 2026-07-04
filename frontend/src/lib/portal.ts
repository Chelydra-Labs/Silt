/**
 * Svelte `use:portal` action: relocate a node to `document.body` (or a target)
 * so it escapes ancestor `overflow` / stacking-context / containing-block
 * traps. Used by `<Popover>` so a dropdown can float above a scroll container
 * that would otherwise clip it (position:absolute/fixed alone is unreliable
 * when an ancestor carries transform/filter/will-change).
 *
 * Simple form: the node is adopted into the target on mount and removed on
 * destroy. A portaled node's close/outro transition is cut (entrance still
 * plays) — acceptable for click-away dropdowns; upgrade to the
 * mount()/unmount({ outro: true }) Wormhole pattern if exit animation matters.
 */
export function portal(node: HTMLElement, target: HTMLElement = document.body) {
  target.appendChild(node)
  return {
    destroy() {
      node.remove()
    }
  }
}
