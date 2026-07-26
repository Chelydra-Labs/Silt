// Reactive controller for TipTapEditor's custom-event bus. Owns the `silt:`
// window-event registrations that decouple the editor surface from the
// toolbars/menus that drive it (FormatToolbar, HeadingLevelMenu,
// SelectionBubble, MathNodeView dispatch these events; TipTapEditor listens).
//
// Handlers bridge into the editor's existing reactive state via injected
// accessors so no state is duplicated — same injection shape as createSlashMenu
// (getEditor + popover callbacks). Follows the repo's .svelte.ts controller
// pattern (aiProviderController, localMcpController): a factory that closes
// over its deps and exposes attach()/detach() for the component to wire into
// its existing lifecycle (top-level register, onDestroy unregister).
//
// NOTE: `silt:open-spellcheck` is NOT owned here — it lives inside the
// spellcheck $effect in TipTapEditor.svelte, coupled to the editor DOM
// contextmenu handler and the spellMenu state (see the recon notes in the
// decompose PR). Moving it would split that cohesive spellcheck unit.
import type { Editor } from 'svelte-tiptap'
import { convertToBlock, setBlockAlign } from '../../../lib/editor'

// Shape of the LaTeX equation popover owned by TipTapEditor. The /math slash
// command (CREATE) and the silt:edit-math event (EDIT) both populate it.
export type MathPopover = {
  latex: string
  displayMode: boolean
  coords: { left: number; top: number }
  onCommit: (latex: string) => void
}

// Accessors into TipTapEditor's reactive state. Handlers read/mutate the
// editor's existing state through these — no state is copied into the bus.
export interface EditorEventsDeps {
  /** Current ProseMirror editor (bindable prop; may be null during teardown). */
  getEditor: () => Editor | null
  /** Opens the inline link URL input near the selection (insert/edit). */
  openLinkInput: (prefill?: string) => void
  /** Opens the color picker popover anchored at the selection. */
  openColorPickerPopover: (markType: 'textColor' | 'backgroundColor') => void
  /** Replaces the math popover state (null closes it). */
  setMathPopover: (popover: MathPopover | null) => void
}

export function createEditorEvents(deps: EditorEventsDeps) {
  const { getEditor, openLinkInput, openColorPickerPopover, setMathPopover } =
    deps

  // --- silt:* dispatch handlers --------------------------------------------
  // Bodies are copied verbatim from TipTapEditor.svelte; the only changes are
  // accessor wiring (editorInstance ← getEditor(), mathPopover= ← setMathPopover()).

  function onOpenLinkInput(e: Event): void {
    const detail = (e as CustomEvent<{ href?: string }>).detail
    openLinkInput(detail?.href)
  }
  function onChangeBlockType(e: Event): void {
    const detail = (e as CustomEvent).detail
    const editorInstance = getEditor()
    if (!editorInstance) return
    if (detail?.type === 'headerBlock') {
      convertToBlock(editorInstance, 'headerBlock', detail.depth || 1)
    } else if (detail?.type === 'noteBlock') {
      convertToBlock(editorInstance, 'noteBlock')
    } else if (detail?.type === 'taskBlock') {
      convertToBlock(editorInstance, 'taskBlock')
    }
  }
  function onSetBlockAlign(e: Event): void {
    const align = (e as CustomEvent).detail as string
    const editorInstance = getEditor()
    if (align && editorInstance) setBlockAlign(editorInstance, align)
  }
  function onOpenColorPicker(e: Event): void {
    const markType = (e as CustomEvent).detail as
      'textColor' | 'backgroundColor'
    if (markType) openColorPickerPopover(markType)
  }
  function onEditMath(e: Event): void {
    const detail = (e as CustomEvent).detail as MathPopover | null
    if (!detail) return
    setMathPopover({
      latex: detail.latex,
      displayMode: detail.displayMode,
      coords: detail.coords,
      onCommit: detail.onCommit
    })
  }

  function attach(): void {
    window.addEventListener('silt:open-link-input', onOpenLinkInput)
    window.addEventListener('silt:change-block-type', onChangeBlockType)
    window.addEventListener('silt:set-block-align', onSetBlockAlign)
    window.addEventListener('silt:open-color-picker', onOpenColorPicker)
    window.addEventListener('silt:edit-math', onEditMath)
  }

  function detach(): void {
    window.removeEventListener('silt:open-link-input', onOpenLinkInput)
    window.removeEventListener('silt:change-block-type', onChangeBlockType)
    window.removeEventListener('silt:set-block-align', onSetBlockAlign)
    window.removeEventListener('silt:open-color-picker', onOpenColorPicker)
    window.removeEventListener('silt:edit-math', onEditMath)
  }

  return { attach, detach }
}

export type EditorEventsController = ReturnType<typeof createEditorEvents>
