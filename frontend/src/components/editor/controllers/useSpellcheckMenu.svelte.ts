// Reactive controller for TipTapEditor's spellcheck corrections menu (#196 /
// #336 / #337): the spellMenu $state, the openSpellMenuAt opener, and the
// contextmenu + silt:open-spellcheck listener $effect that wires right-clicks
// on misspelled words (and the FormatToolbar spellcheck button) to the menu.
//
// Behaviour-preserving relocation from TipTapEditor.svelte. The dictionary-
// loading $effect (loadDictionary / setCustomWords / requestSpellcheckRecheck)
// STAYS in TipTapEditor.svelte — it is intrinsically coupled to ProseMirror
// decoration rechecks and the editor-readiness flow (see the header comment
// there). Only the menu surface + its DOM listeners move here. Same accessor-
// injection pattern as createPopoversController / createEditorEvents: a factory
// that closes over getEditor so deferred callbacks always see the live editor.
import type { Editor } from 'svelte-tiptap'
import {
  findMisspellingAt,
  findMisspellingAtOrAfter
} from '../../../lib/editor/spellcheck/SpellcheckExtension'
import { settings } from '../../../settings/store.svelte'

export interface SpellcheckMenuDeps {
  /** Current ProseMirror editor (bindable prop; may be null during teardown).
   *  Re-read on every handler call so deferred callbacks see the live editor
   *  after an edit↔source switch, not a stale capture. */
  getEditor: () => Editor | null
}

export type SpellMenuState = {
  word: string
  range: { from: number; to: number }
  anchor: { x: number; y: number }
}

export function createSpellcheckMenu(deps: SpellcheckMenuDeps) {
  const { getEditor } = deps

  let spellMenu = $state<SpellMenuState | null>(null)

  function openSpellMenuAt(
    editor: Editor,
    pos: number,
    coords: { x: number; y: number },
    useFallback = false
  ): void {
    // Right-click (useFallback=false): only open if the user clicked ON a
    // misspelled word. Toolbar button (useFallback=true): if the cursor isn't
    // on a misspelling, jump to the next one so the button isn't a silent no-op.
    const m =
      findMisspellingAt(editor, pos) ??
      (useFallback ? findMisspellingAtOrAfter(editor, pos) : null)
    if (!m) return
    spellMenu = {
      word: m.word,
      range: { from: m.from, to: m.to },
      anchor: coords
    }
  }

  // Tracks the active listener cleanup so dispose() can remove listeners
  // explicitly from onDestroy. The $effect cleanup also runs on scope
  // teardown; removeEventListener is idempotent so the double call is safe.
  let detachListeners: (() => void) | null = null

  // Registers the contextmenu (right-click on a misspelled word) and
  // silt:open-spellcheck (FormatToolbar button) listeners against the editor
  // DOM. Re-runs when the editor instance changes (edit↔source switch) or
  // spellcheck_enabled flips, detaching the previous pair first.
  $effect(() => {
    const editor = getEditor()
    // Guard like every other view-access site: during edit↔source switches
    // svelte-tiptap tears the editor down, nulling editorView. isDestroyed is
    // `editorView?.isDestroyed ?? true`, so it's true exactly when view.dom
    // would throw "view is not available" — bail before touching the proxy.
    if (!editor || editor.isDestroyed) return
    if (settings.config?.editor?.spellcheck_enabled === false) return
    const dom = editor.view.dom
    const onContext = (e: MouseEvent) => {
      if (settings.config?.editor?.spellcheck_enabled === false) return
      const pos = editor.view.posAtCoords({ left: e.clientX, top: e.clientY })
      if (pos == null) return
      const m = findMisspellingAt(editor, pos.pos)
      if (!m) return
      e.preventDefault()
      e.stopPropagation()
      openSpellMenuAt(editor, pos.pos, { x: e.clientX, y: e.clientY })
    }
    const onOpenBtn = (e: Event) => {
      const detail = (e as CustomEvent).detail as
        { x: number; y: number } | undefined
      const head = editor.state.selection.head
      openSpellMenuAt(editor, head, detail ?? { x: 100, y: 100 }, true)
    }
    dom.addEventListener('contextmenu', onContext)
    window.addEventListener('silt:open-spellcheck', onOpenBtn)
    const cleanup = () => {
      dom.removeEventListener('contextmenu', onContext)
      window.removeEventListener('silt:open-spellcheck', onOpenBtn)
    }
    detachListeners = cleanup
    return cleanup
  })

  function dispose(): void {
    detachListeners?.()
    detachListeners = null
    spellMenu = null
  }

  return {
    // spellMenu has a setter — the SpellcheckMenu onClose writes null to close.
    get spellMenu() {
      return spellMenu
    },
    set spellMenu(v: SpellMenuState | null) {
      spellMenu = v
    },
    openSpellMenuAt,
    dispose
  }
}

export type SpellcheckMenuController = ReturnType<typeof createSpellcheckMenu>
