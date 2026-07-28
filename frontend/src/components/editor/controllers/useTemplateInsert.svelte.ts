// Reactive controller for TipTapEditor's template-insert cluster (#769):
// the showTemplatePicker / pendingTemplateBlocks / templateInsertReturnFocus
// $state cells and the seven handlers they gate (isEditorEffectivelyEmpty,
// isCursorAtDocEnd, insertTemplateBlocks, handleTemplateInsert,
// clearTemplateInsertDialog, confirmTemplateAtCursor, confirmTemplateAppend,
// cancelTemplateInsert).
//
// Behaviour-preserving relocation from TipTapEditor.svelte: pure orchestration
// — no IPC, no stores, no listeners. Same accessor-injection pattern as
// createPopoversController / createEditorEvents: a factory that closes over
// getEditor so deferred callbacks always see the live editor after an
// edit↔source switch (never a stale capture). State is exposed via getters so
// template reads stay reactive — returning $state in a plain object would
// snapshot the initial value.
import type { Editor } from 'svelte-tiptap'
import { blocksToDoc } from '../../../lib/editor'
import type { ParsedBlock } from '../../../lib/editor'

export interface TemplateInsertDeps {
  /** Current ProseMirror editor (bindable prop; may be null during teardown).
   *  Re-read on every handler call so deferred callbacks see the live editor
   *  after an edit↔source switch, not a stale capture. */
  getEditor: () => Editor | null
}

export function createTemplateInsert(deps: TemplateInsertDeps) {
  const { getEditor } = deps

  let showTemplatePicker = $state(false)
  // Pending template insert when the page is non-empty and the cursor is not
  // at the end (#664). ChoiceDialog offers insert-at-cursor vs append-to-end.
  let pendingTemplateBlocks = $state<ParsedBlock[] | null>(null)
  let templateInsertReturnFocus = $state<HTMLElement | null>(null)

  function openTemplatePicker(): void {
    showTemplatePicker = true
  }

  // True when the page has no meaningful content (empty trailing note only).
  function isEditorEffectivelyEmpty(editor: Editor): boolean {
    const text = editor.state.doc.textContent.trim()
    if (text.length > 0) return false
    // Allow a single empty note/header; anything else counts as non-empty.
    let blockCount = 0
    editor.state.doc.forEach(() => {
      blockCount += 1
    })
    return blockCount <= 1
  }

  // Cursor is at (or past) the end of the last block's content.
  function isCursorAtDocEnd(editor: Editor): boolean {
    const from = editor.state.selection.$from
    const end = editor.state.doc.content.size
    return from.pos >= end - 1
  }

  function insertTemplateBlocks(
    blocks: ParsedBlock[],
    mode: 'cursor' | 'append'
  ): void {
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    const doc = blocksToDoc(blocks)
    if (mode === 'append') {
      const end = editorInstance.state.doc.content.size
      editorInstance.commands.insertContentAt(end, doc.content)
    } else {
      editorInstance.commands.insertContent(doc.content)
    }
    editorInstance.commands.focus()
  }

  // Insert rendered template blocks. Empty pages insert immediately; non-empty
  // pages with the cursor mid-content confirm first (#664).
  function handleTemplateInsert(blocks: ParsedBlock[]): void {
    const editorInstance = getEditor()
    if (!editorInstance || editorInstance.isDestroyed) return
    if (
      isEditorEffectivelyEmpty(editorInstance) ||
      isCursorAtDocEnd(editorInstance)
    ) {
      insertTemplateBlocks(blocks, 'cursor')
      return
    }
    // Capture focus before the dialog mounts so restore works for
    // programmatic open (not only button-click paths).
    templateInsertReturnFocus =
      (document.activeElement as HTMLElement | null) ?? null
    pendingTemplateBlocks = blocks
  }

  function clearTemplateInsertDialog(): void {
    pendingTemplateBlocks = null
    templateInsertReturnFocus = null
  }

  function confirmTemplateAtCursor(): void {
    if (!pendingTemplateBlocks) return
    insertTemplateBlocks(pendingTemplateBlocks, 'cursor')
    clearTemplateInsertDialog()
  }

  function confirmTemplateAppend(): void {
    if (!pendingTemplateBlocks) return
    insertTemplateBlocks(pendingTemplateBlocks, 'append')
    clearTemplateInsertDialog()
  }

  function cancelTemplateInsert(): void {
    clearTemplateInsertDialog()
  }

  return {
    // showTemplatePicker has a setter — the TemplatePicker onClose writes it.
    get showTemplatePicker() {
      return showTemplatePicker
    },
    set showTemplatePicker(v: boolean) {
      showTemplatePicker = v
    },
    get pendingTemplateBlocks() {
      return pendingTemplateBlocks
    },
    get templateInsertReturnFocus() {
      return templateInsertReturnFocus
    },
    openTemplatePicker,
    handleTemplateInsert,
    confirmTemplateAtCursor,
    confirmTemplateAppend,
    cancelTemplateInsert
  }
}

export type TemplateInsertController = ReturnType<typeof createTemplateInsert>
