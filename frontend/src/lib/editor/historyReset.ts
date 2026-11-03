import type { Editor } from '@tiptap/core'
import type { Plugin } from '@tiptap/pm/state'
import { EditorState } from '@tiptap/pm/state'
import { history as historyPluginFactory } from '@tiptap/pm/history'

// prosemirror-state's Plugin type does not declare the runtime `key` field
// (every plugin instance carries one); narrow it locally for the lookup.
type KeyedPlugin = Plugin & { key: string }

// External content replacement is a history boundary (#1005, parity with the
// source viewer's seedBuffer reset): TipTap's setContent is a plain undoable
// replace transaction, so without a reset one undo step resurrects the
// pre-replacement document. After keep_theirs that resurrected buffer is the
// refused-era text, and its undo-driven autosave lands on a re-baselined page
// and silently overwrites the version the user chose to keep.
//
// The installed TipTap 3.x ships no `clearHistory` command and
// prosemirror-history does not export its plugin key or state class, so the
// reset adopts an empty history state through the plugin's meta protocol —
// the same channel the undo/redo commands themselves use (a transaction meta
// of `{ historyState }` under the plugin's key replaces the stacks).

/** Clears the editor's undo/redo stacks. No-op when no history plugin is
 *  registered (undo-redo disabled) or the editor is gone. */
export function resetUndoHistory(editor: Editor | null): void {
  if (!editor || editor.isDestroyed) return
  const historyPlugin = editor.state.plugins.find(
    (p) =>
      typeof (p as KeyedPlugin).key === 'string' &&
      (p as KeyedPlugin).key.startsWith('history$')
  ) as KeyedPlugin | undefined
  if (!historyPlugin) return
  // A scratch plugin instance init()s the pristine empty state without
  // touching the live editor (the live doc only satisfies the state's
  // shape requirement; nothing reads it).
  const scratch = historyPluginFactory()
  const scratchState = EditorState.create({
    doc: editor.state.doc,
    plugins: [scratch]
  })
  const fresh = scratch.getState(scratchState)
  if (!fresh) return
  const tr = editor.state.tr
    .setMeta('addToHistory', false)
    .setMeta('preventUpdate', true)
  tr.setMeta(historyPlugin.key, { historyState: fresh })
  editor.view.dispatch(tr)
}
