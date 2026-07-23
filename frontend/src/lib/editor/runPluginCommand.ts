// Error-boundary for plugin slash-command handlers (#581).
//
// A plugin's `onSelect` is typed `=> void` but may legitimately return a
// Promise, and a buggy plugin can throw synchronously or reject asynchronously.
// Without a boundary the throw escapes into the editor's dispatch path and the
// rejection goes unhandled, freezing the editor for the rest of the session
// with no signal. This helper catches both and routes them to a `report`
// callback (log + toast) so the editor stays usable.
//
// Extracted from TipTapEditor.handleSlashSelect so the sync-throw and
// async-reject paths are unit-testable without mounting the full editor.

import type { Editor } from '@tiptap/core'
import type { SlashCommand } from './slash-registry'

/**
 * Invoke a plugin command's `onSelect`, catching synchronous throws and
 * asynchronous rejections. The `report` callback receives any failure.
 */
export function runPluginCommand(
  cmd: SlashCommand,
  editor: unknown,
  pos: number,
  report: (err: unknown) => void
): void {
  try {
    // onSelect is typed `=> void`, but a plugin may return a Promise;
    // duck-type the result so a rejection is caught too.
    const result = cmd.onSelect?.(editor as Editor, pos) as unknown
    if (result && typeof (result as { catch?: unknown }).catch === 'function') {
      ;(result as Promise<unknown>).catch(report)
    }
  } catch (err) {
    report(err)
  }
}
