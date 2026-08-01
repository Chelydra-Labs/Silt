// Reactive controller for TipTapEditor's popover cluster (Cluster A): the
// six floating popovers anchored at the selection — inline link URL input,
// color picker, custom table-size picker, LaTeX equation popover, and the
// block-embed picker — plus the dismiss-on-scroll/resize helper.
//
// Behaviour-preserving relocation from TipTapEditor.svelte: the $state cells,
// open/apply/cancel handlers, and the link-autofocus $effect moved here
// verbatim (only the editor reference is indirected through getEditor()). Same
// accessor-injection pattern as createEditorEvents / createSlashMenu: a factory
// that closes over its deps. State is exposed via getters so reads in the
// host template stay reactive — returning $state in a plain object would
// snapshot the initial value (see createSlashMenu's return for the precedent).
//
// The factory is invoked during the component's init, so the autofocus $effect
// registers against the component's effect scope (not orphaned); no $effect.root
// is needed because the popovers live and die with the editor component.
import type { Editor } from 'svelte-tiptap'
import { insertTable } from '../../../lib/editor'

// Shape of the LaTeX equation popover. The /math slash command (CREATE) and
// the silt:edit-math event (EDIT) both populate it; structurally identical to
// the MathPopover exported from useEditorEvents and SlashMathPopover from
// useSlashMenu (kept local so the popover controllers stay decoupled).
export type PopoversMathPopover = {
  latex: string
  displayMode: boolean
  coords: { left: number; top: number }
  onCommit: (latex: string) => void
}

// Cursor anchor rect the custom table-size picker flips/clamps against.
export type PopoversTableSizeAnchor = {
  top: number
  bottom: number
  left: number
}

export interface PopoversDeps {
  /** Current ProseMirror editor (bindable prop; may be null during teardown).
   *  Re-read on every handler call so deferred callbacks see the live editor
   *  after an edit↔source switch, not a stale capture. */
  getEditor: () => Editor | null
}

export function createPopoversController(deps: PopoversDeps) {
  const { getEditor } = deps

  // Validates hex color strings before applying to marks (#170). Prevents
  // injection of arbitrary CSS or characters that break the converter regex.
  const HEX_COLOR_RE = /^#[0-9a-fA-F]{3,8}$/

  // Block-embed picker (#593): selecting /embed opens BlockPickerModal.
  let showEmbedPicker = $state(false)

  // Inline link URL input (#168). Shows a small <input> near the selection
  // when the user clicks the link button or presses Ctrl+K. Enter applies,
  // Esc cancels, blur applies.
  let showLinkInput = $state(false)
  let linkInputValue = $state('')
  let linkInputCoords = $state<{ left: number; top: number } | null>(null)

  // Color picker popover (#170). Shows ColorPickerMenu as a floating element
  // near the selection, replacing the window.prompt slash-command path.
  let showColorPicker = $state(false)
  let colorPickerMarkType = $state<'textColor' | 'highlight'>('textColor')
  let colorPickerCoords = $state<{ left: number; top: number } | null>(null)

  // Custom-table size picker (#172) — an in-app popover replacing window.prompt.
  let showTableSizePicker = $state(false)
  let tableSizeCoords = $state<{
    top: number
    bottom: number
    left: number
  } | null>(null)

  // LaTeX equation popover (Phase 5 / #328). Owned here so it renders as a
  // sibling of the editor surface; math NodeViews request editing via the
  // silt:edit-math window event (events controller writes here via
  // setMathPopover).
  let mathPopover = $state<PopoversMathPopover | null>(null)

  // --- Inline link URL input (#168 / #689) ---------------------------------
  // Opens for insert or edit. Existing links are never removed here — the
  // selection bubble exposes Edit/Open/Copy/Remove explicitly (#689).
  function openLinkInput(prefill?: string): void {
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    const { selection } = editorInstance.state
    const inLink = editorInstance.isActive('link')
    // New links need a non-empty selection; edit can open with the caret in a link.
    if (selection.empty && !inLink && prefill == null) return
    try {
      const coords = editorInstance.view.coordsAtPos(selection.from)
      linkInputCoords = { left: coords.left, top: coords.bottom }
    } catch {
      linkInputCoords = null
    }
    let initial = prefill ?? ''
    if (!initial && inLink) {
      try {
        const attrs = editorInstance.getAttributes('link') as { href?: string }
        initial = attrs?.href ?? ''
      } catch {
        initial = ''
      }
    }
    linkInputValue = initial
    showLinkInput = true
  }

  function applyLinkInput(): void {
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    const url = linkInputValue.trim()
    if (url) {
      // setLink updates an existing mark or applies a new one without toggle
      // flip-flop when the selection is already linked (#689 edit path).
      editorInstance.chain().focus().setLink({ href: url }).run()
    } else {
      editorInstance.chain().focus().run()
    }
    showLinkInput = false
    linkInputValue = ''
  }

  function cancelLinkInput(): void {
    showLinkInput = false
    linkInputValue = ''
    const editorInstance = getEditor()
    editorInstance?.chain().focus().run()
  }

  // --- Custom table size picker (#172) -------------------------------------
  function openTableSizePicker(anchor: PopoversTableSizeAnchor): void {
    tableSizeCoords = anchor
    showTableSizePicker = true
  }

  function confirmTableSize(rows: number, cols: number): void {
    showTableSizePicker = false
    tableSizeCoords = null
    const editorInstance = getEditor()
    if (editorInstance && !editorInstance.isDestroyed) {
      insertTable(editorInstance, rows, cols)
    }
  }
  function cancelTableSize(): void {
    showTableSizePicker = false
    tableSizeCoords = null
    const editorInstance = getEditor()
    editorInstance?.chain().focus().run()
  }

  // --- LaTeX equation popover (Phase 5 / #328) -----------------------------
  // EDIT site: a math NodeView dispatches silt:edit-math (events controller),
  // which writes here via setMathPopover. CREATE site (/math slash command)
  // also writes here via setMathPopover.
  function setMathPopover(popover: PopoversMathPopover | null): void {
    mathPopover = popover
  }

  function commitMathPopover(latex: string): void {
    const cb = mathPopover?.onCommit
    mathPopover = null
    cb?.(latex)
    const editorInstance = getEditor()
    if (editorInstance && !editorInstance.isDestroyed) {
      editorInstance.commands.focus()
    }
  }

  function cancelMathPopover(): void {
    mathPopover = null
    const editorInstance = getEditor()
    if (editorInstance && !editorInstance.isDestroyed) {
      editorInstance.commands.focus()
    }
  }

  // --- Color picker popover (#170) -----------------------------------------
  function openColorPickerPopover(markType: 'textColor' | 'highlight'): void {
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    try {
      const { selection } = editorInstance.state
      const coords = editorInstance.view.coordsAtPos(selection.from)
      colorPickerCoords = { left: coords.left, top: coords.bottom }
    } catch {
      colorPickerCoords = null
    }
    colorPickerMarkType = markType
    showColorPicker = true
  }

  function applyColorFromPopover(color: string | null): void {
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    if (color && HEX_COLOR_RE.test(color)) {
      editorInstance
        .chain()
        .focus()
        .setMark(colorPickerMarkType, { color })
        .run()
    } else if (!color) {
      editorInstance.chain().focus().unsetMark(colorPickerMarkType).run()
    }
    showColorPicker = false
    editorInstance.chain().focus().run()
  }

  // --- Block embed picker (#593) -------------------------------------------
  // Picking a block inserts a complete embed portal node (not the raw
  // {{embed:uuid}} token — see TipTapEditor for the NodeView rationale).
  function openEmbedPicker(): void {
    showEmbedPicker = true
  }

  function handleEmbedPick(blockId: string): void {
    showEmbedPicker = false
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    editorInstance.commands.insertContent({
      type: 'embedNode',
      attrs: { id: crypto.randomUUID(), uuid: blockId, bullet: '' }
    })
    editorInstance.commands.focus()
  }
  function closeEmbedPicker(): void {
    showEmbedPicker = false
    const editorInstance = getEditor()
    editorInstance?.chain().focus().run()
  }

  // Dismiss the selection-anchored popovers (link / color / math) when an
  // ancestor scrolls or the window resizes (#594). They capture their anchor
  // coordinates once and render position:fixed, so without this they would
  // float at stale screen positions. Dismiss is chosen over reposition for
  // parity with the selection bubble (repositioning mid-text-entry is jarring).
  function dismissFloatingPopovers(): void {
    if (showLinkInput) showLinkInput = false
    if (showColorPicker) showColorPicker = false
    if (mathPopover) mathPopover = null
  }

  // Auto-focus the link input when it appears. Registers against the host
  // component's effect scope because the factory runs during init.
  $effect(() => {
    if (showLinkInput) {
      requestAnimationFrame(() => {
        const input = document.querySelector<HTMLInputElement>('.link-input')
        input?.focus()
      })
    }
  })

  return {
    // State — getters so template reads track the signal (setter on
    // linkInputValue backs the bind:value in the link input template).
    get showEmbedPicker() {
      return showEmbedPicker
    },
    get showLinkInput() {
      return showLinkInput
    },
    get linkInputValue() {
      return linkInputValue
    },
    set linkInputValue(v: string) {
      linkInputValue = v
    },
    get linkInputCoords() {
      return linkInputCoords
    },
    get showColorPicker() {
      return showColorPicker
    },
    get colorPickerMarkType() {
      return colorPickerMarkType
    },
    get colorPickerCoords() {
      return colorPickerCoords
    },
    get showTableSizePicker() {
      return showTableSizePicker
    },
    get tableSizeCoords() {
      return tableSizeCoords
    },
    get mathPopover() {
      return mathPopover
    },
    // Methods — stable closure references, no binding hazard.
    openLinkInput,
    applyLinkInput,
    cancelLinkInput,
    openTableSizePicker,
    confirmTableSize,
    cancelTableSize,
    setMathPopover,
    commitMathPopover,
    cancelMathPopover,
    openColorPickerPopover,
    applyColorFromPopover,
    openEmbedPicker,
    handleEmbedPick,
    closeEmbedPicker,
    dismissFloatingPopovers
  }
}

export type PopoversController = ReturnType<typeof createPopoversController>
