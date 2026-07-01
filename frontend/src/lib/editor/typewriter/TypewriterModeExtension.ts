import { Extension } from '@tiptap/core'
import { Plugin, PluginKey } from '@tiptap/pm/state'
import { settings } from '../../../settings/store.svelte'

/**
 * Typewriter mode (#187) — keeps the active line at a configurable viewport
 * ratio (default 0.5, matching iA Writer). A pure scroll-presentation concern:
 * zero content/schema/on-disk impact.
 *
 * Implementation: a ProseMirror PluginView.update that, after each state
 * update driven by KEYBOARD input, sets the scroll container's scrollTop so the
 * cursor lands at the configured ratio. Reads editor.typewriter_mode +
 * typewriter_mode_ratio live from the settings store (so toggling in Settings
 * applies without an editor reload). Mouse-driven selection changes are filtered
 * out via a flag (mousedown sets it; keydown clears it) so drag-selection +
 * click-to-position don't fight the recenter. handleScrollToSelection returns
 * true while enabled to suppress ProseMirror's native make-visible scroll.
 * Always instant (no animation) — iA Writer's behavior; makes
 * prefers-reduced-motion moot.
 */

const key = new PluginKey('siltTypewriter')

/** Walk up from the editor DOM to the first vertical-scroll ancestor. */
function findScrollContainer(el: HTMLElement | null): HTMLElement | null {
  let node = el?.parentElement
  while (node) {
    const overflowY = getComputedStyle(node).overflowY
    if (overflowY === 'auto' || overflowY === 'scroll') return node
    node = node.parentElement
  }
  return null
}

export const TypewriterMode = Extension.create({
  name: 'siltTypewriter',

  addProseMirrorPlugins() {
    // True after a mouse-driven selection/click; cleared on the next keydown.
    // Prevents the recenter from fighting click-to-position + drag-select.
    let mouseScroll = false
    return [
      new Plugin({
        key,
        props: {
          // Suppress ProseMirror's native scroll-to-selection so our update owns
          // the scroll while typewriter mode is on.
          handleScrollToSelection: () =>
            settings.config?.editor?.typewriter_mode === true,
          handleDOMEvents: {
            mousedown: () => {
              mouseScroll = true
              return false
            },
            keydown: () => {
              mouseScroll = false
              return false
            }
          }
        },
        view: () => ({
          update: (view: any, prevState: any) => {
            if (settings.config?.editor?.typewriter_mode !== true) return
            // Only act on selection or doc change; ignore pure metadata updates.
            if (
              view.state.selection.eq(prevState.selection) &&
              view.state.doc.eq(prevState.doc)
            ) {
              return
            }
            if (mouseScroll) {
              // One-shot: consume the flag so it only suppresses the
              // immediately-following update. Without this, a click leaves
              // mouseScroll true until the next keydown, so programmatic
              // selection jumps (FindBar, search results) would also be
              // skipped — typewriter mode would silently fail to recenter.
              mouseScroll = false
              return
            }
            const ratio = settings.config?.editor?.typewriter_mode_ratio ?? 0.5
            const container = findScrollContainer(view.dom as HTMLElement)
            if (!container) return
            const coords = view.coordsAtPos(view.state.selection.head)
            const rect = container.getBoundingClientRect()
            const target =
              container.scrollTop +
              (coords.top - rect.top) -
              rect.height * ratio
            const clamped = Math.max(
              0,
              Math.min(target, container.scrollHeight - container.clientHeight)
            )
            container.scrollTop = clamped
          },
          destroy() {}
        })
      })
    ]
  }
})
