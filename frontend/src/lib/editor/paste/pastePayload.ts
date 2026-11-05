// Clipboard payload selection — the single place that decides whether a paste
// should use the HTML flavour or the plain-text flavour of the clipboard.
//
// Entry points that do not have a ClipboardEvent (the context-menu Paste
// action today, a toolbar/plugin paste later) must reuse this instead of
// hardcoding "HTML first": ProseMirror's own Ctrl+V handler is context-aware.

import type { ResolvedPos } from '@tiptap/pm/model'
import type { EditorView } from '@tiptap/pm/view'
import { classifyPastedHTML } from './normalizePastedHTML'

/** The two clipboard flavours a menu paste can read. */
export interface PastePayload {
  html: string
  text: string
}

export type PastePayloadChoice =
  | { kind: 'html'; value: string }
  | { kind: 'text'; value: string }
  | { kind: 'none' }

/**
 * Mirror ProseMirror's paste decision (`prosemirror-view`'s `doPaste` call
 * site): it pastes text when the caret is inside a `code: true` node, when
 * there is no HTML, or when HTML is unusable/oversized. `view.pasteHTML`
 * discards the text payload outright, so anything that picks HTML blindly
 * splits a code block instead of inserting at the caret.
 *
 * True to PM, the code check reads `selection.$from`, which keeps
 * NodeSelection behaviour identical to Ctrl+V.
 */
export function selectPastePayload(
  $from: ResolvedPos,
  { html, text }: PastePayload
): PastePayloadChoice {
  const inCode = $from.parent.type.spec.code
  const decision = classifyPastedHTML(html)
  if (text && (inCode || !html || decision === 'text')) {
    return { kind: 'text', value: text }
  }
  // Oversized HTML with no text alternative is dropped rather than parsed,
  // matching the Ctrl+V hook (`siltHandlePasteEvent`).
  if (html && decision !== 'text') return { kind: 'html', value: html }
  return { kind: 'none' }
}

/**
 * Select the flavour and hand it to the view's paste entry points — never
 * `insertContent`, so the full paste pipeline (transformPastedHTML →
 * transformPasted → handlePaste, `uiEvent:'paste'` rules, whitespace collapse,
 * slice-context repair) runs for menu pastes too.
 */
export function pasteClipboardPayload(
  view: EditorView,
  payload: PastePayload
): PastePayloadChoice {
  const choice = selectPastePayload(view.state.selection.$from, payload)
  if (choice.kind === 'html') view.pasteHTML(choice.value)
  else if (choice.kind === 'text') view.pasteText(choice.value)
  return choice
}
