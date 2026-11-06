import type { Editor } from 'svelte-tiptap'
import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
import { serializeInlineContent } from './converters'
import { findActiveBlock } from './keymaps'
import { readClipboardPayload } from './clipboardHtml'
import { pasteClipboardPayload } from './paste/pastePayload'
import { pushNotification } from '../../notifications/store.svelte'

/**
 * Context-menu state shape the clipboard handlers read from. The host owns
 * this state (because the context menu also drives focus / Escape / outside-
 * click handling); clipboard just reads the active-block fields.
 */
export interface ClipboardMenuState {
  activeBlockId?: string
  activeBlockNode?: ProseMirrorNode
}

/**
 * Bag the host passes in. `notify` is the toast pusher; `menu()` returns the
 * current menu state (or null) so the handlers see live values without
 * holding a stale snapshot.
 */
export interface ClipboardDeps {
  editor: Editor
  notify: typeof pushNotification
  menu: () => ClipboardMenuState | null
}

/**
 * Write text to the clipboard, surfacing a failure toast so the user knows a
 * copy/cut didn't land. Mirrors pasteFromClipboard's failure feedback — without
 * this, every copy/cut menu action fails silently if the clipboard is
 * unavailable, which undermines trust exactly when the user is about to paste.
 */
async function writeTextOrNotify(
  deps: ClipboardDeps,
  text: string
): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    deps.notify({
      kind: 'error',
      message: 'Copy failed: clipboard could not be written.'
    })
    return false
  }
}

async function writeSelectionOrNotify(
  deps: ClipboardDeps,
  html: string,
  text: string
): Promise<boolean> {
  try {
    const clipboard =
      typeof navigator === 'undefined' ? undefined : navigator.clipboard
    const ClipboardItemConstructor = globalThis.ClipboardItem
    const supportsHtml =
      typeof ClipboardItemConstructor?.supports !== 'function' ||
      ClipboardItemConstructor.supports('text/html')
    if (
      clipboard &&
      typeof clipboard.write === 'function' &&
      ClipboardItemConstructor &&
      supportsHtml
    ) {
      await clipboard.write([
        new ClipboardItemConstructor({
          'text/html': new Blob([html], { type: 'text/html' }),
          'text/plain': new Blob([text], { type: 'text/plain' })
        })
      ])
    } else {
      await clipboard!.writeText(text)
    }
    return true
  } catch {
    deps.notify({
      kind: 'error',
      message: 'Copy failed: clipboard could not be written.'
    })
    return false
  }
}

function captureSelection(editor: Editor) {
  const { state } = editor
  const selection = state.selection
  const { dom, text } = editor.view.serializeForClipboard(selection.content())
  return { doc: state.doc, selection, html: dom.innerHTML, text }
}

export function cutSelection(deps: ClipboardDeps): void {
  const { editor } = deps
  const captured = captureSelection(editor)
  void writeSelectionOrNotify(deps, captured.html, captured.text).then(
    (written) => {
      if (
        written &&
        !editor.isDestroyed &&
        editor.isEditable &&
        editor.state.doc === captured.doc &&
        editor.state.selection.eq(captured.selection)
      ) {
        editor.commands.deleteSelection()
      }
    }
  )
}

export function copySelection(deps: ClipboardDeps): void {
  const captured = captureSelection(deps.editor)
  void writeSelectionOrNotify(deps, captured.html, captured.text)
}

export async function pasteFromClipboard(deps: ClipboardDeps): Promise<void> {
  const { editor, notify } = deps
  try {
    const payload = await readClipboardPayload()
    // `pasteClipboardPayload` picks the flavour the way Ctrl+V does (text in a
    // code context, no HTML, or an unusable/oversized payload; HTML otherwise)
    // and drives the view's paste entry points so the full pipeline runs.
    const choice = pasteClipboardPayload(editor.view, payload)
    // `unavailable` means every source failed (API absent, permission denied);
    // a readable-but-empty clipboard stays silent, exactly like Ctrl+V.
    if (choice.kind === 'none' && payload.unavailable) {
      notify({
        kind: 'error',
        message: 'Paste failed: the clipboard is unavailable.'
      })
    }
  } catch {
    // readClipboardPayload() is total and the selection predicate does not
    // throw, so this is a genuine paste-pipeline failure: say the paste
    // failed rather than blaming the clipboard read.
    notify({
      kind: 'error',
      message: 'Paste failed: the clipboard contents could not be inserted.'
    })
  }
}

/**
 * Serialize the current selection (or the active block, if the selection is
 * empty) to markdown via `serializeInlineContent` and write it to the
 * clipboard. The empty-selection-block path is what the right-click "Copy as
 * Markdown" menu item drives when the user hasn't selected anything.
 */
export async function copyAsMarkdown(deps: ClipboardDeps): Promise<void> {
  const { editor, menu } = deps
  const { selection } = editor.state
  let md = ''
  if (selection.empty) {
    const active = menu()?.activeBlockNode
    if (active) {
      const json = active.toJSON()
      md = json.content ? serializeInlineContent(json.content) : ''
    }
  } else {
    const slice = editor.state.doc.slice(selection.from, selection.to)
    const parts: string[] = []
    slice.content.forEach((node) => {
      const json = node.toJSON()
      parts.push(
        json.content ? serializeInlineContent(json.content) : json.text || ''
      )
    })
    md = parts.join('\n')
  }
  await writeTextOrNotify(deps, md)
}

export async function copyAsPlainText(deps: ClipboardDeps): Promise<void> {
  const { editor } = deps
  const { selection } = editor.state
  const text = selection.empty
    ? ''
    : editor.state.doc.textBetween(selection.from, selection.to, '\n')
  await writeTextOrNotify(deps, text)
}

export async function copyBlockReference(deps: ClipboardDeps): Promise<void> {
  const id = deps.menu()?.activeBlockId
  if (id) {
    await writeTextOrNotify(deps, `((${id}))`)
  }
}

export async function copyBlockEmbed(deps: ClipboardDeps): Promise<void> {
  const id = deps.menu()?.activeBlockId
  if (id) {
    await writeTextOrNotify(deps, `{{embed:${id}}}`)
  }
}

/**
 * Insert a copy of the active block node immediately after it. The cloned
 * node's `id` attr is stripped so the unique-id plugin mints a fresh UUID
 * (otherwise the round-trip through markdown would emit two blocks with the
 * same id and the indexer would dedupe them).
 */
export function duplicateBlock(deps: ClipboardDeps): void {
  const { editor, menu } = deps
  const active = menu()?.activeBlockNode
  if (!active) return
  const block = findActiveBlock(editor)
  if (!block) return
  const endPos = editor.state.selection.$from.after(block.depth)
  const json = active.toJSON()
  if (json.attrs && json.attrs.id) {
    delete json.attrs.id
  }
  editor.chain().insertContentAt(endPos, json).focus().run()
}

/**
 * Delete the active block. Refuses to delete the last remaining block in the
 * doc so the editor is never empty (ProseMirror requires at least one node).
 */
export function deleteBlock(deps: ClipboardDeps): void {
  const { editor } = deps
  const { doc } = editor.state
  if (doc.childCount <= 1) return
  const block = findActiveBlock(editor)
  if (!block) return
  const pos = editor.state.selection.$from
  const from = pos.before(block.depth)
  const to = pos.after(block.depth)
  editor.chain().deleteRange({ from, to }).focus().run()
}
