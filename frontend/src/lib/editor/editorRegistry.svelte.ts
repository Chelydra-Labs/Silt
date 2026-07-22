// Editor reconciliation registry (#345).
//
// Silt renders one TipTapEditor per displayed tab, each with its own
// debounced autosave buffer. A vault-wide write that bypasses the editor
// (global replace is the motivating case) can silently collide with an
// editor's unsaved buffer: the replace reads stale disk content (missing
// the unsaved edits), writes the replaced result, and the editor's pending
// autosave then clobbers the replace — or the editor reloads and the user
// loses their unsaved edits.
//
// This registry lets an out-of-band writer (global replace) coordinate with
// every mounted editor: flush affected dirty buffers BEFORE writing so the
// replace operates on the real current content, then force-reload the
// affected editors AFTER writing so they show the replaced content instead
// of a stale in-memory buffer.
//
// Editors register on mount and unregister on destroy. Lookups are keyed by
// the page triple (the same `\x00`-joined key the search/grouping code uses).

import { SvelteMap } from 'svelte/reactivity'

export interface EditorHandle {
  /** `${notebook}\x00${section}\x00${page}` — matches PageGroup.key. */
  key: string
  /** True if the editor holds unsaved edits not yet persisted to disk. */
  isDirty: () => boolean
  /** Flush the pending autosave; resolves true if the editor is clean after. */
  flush: () => Promise<boolean>
  /** Force the editor to reload from its blocks prop on the next external
   *  block update, bypassing the focused-edit guard. Only safe right after a
   *  flush synced the editor to disk, so there is nothing unsaved to clobber. */
  forceExternalReload: () => void
  /** Show an in-editor proposed-edit preview over a selection range (#543).
   *  No-op if the editor has no selection / is not mounted. Returns true if
   *  the preview was set. */
  setProposedEdit: (opts: {
    from: number
    to: number
    markdown: string
    onAccept?: () => void
    fileDate?: string
  }) => boolean
  /** Clear any active in-editor proposed-edit preview (#543). */
  clearProposedEdit: () => void
  /** Whether an in-editor proposed-edit preview is currently shown (#543). */
  hasProposal: () => boolean
  /** Accept the active in-editor proposed edit — applies the editor
   *  transaction and clears the preview. Returns false if no preview is
   *  active. The onAccept callback set on setProposedEdit fires. */
  acceptProposedEdit: () => boolean
  /** Verify the text at [from,to) still matches the captured selection.
   *  Guards against stale PM positions when the user edits during AI
   *  streaming (#543 review fix). */
  verifySelectionText: (from: number, to: number, expected: string) => boolean
}

const editors = new SvelteMap<string, EditorHandle>()

/** Build the registry lookup key for a page triple. This is the canonical
 *  `${notebook}\x00${section}\x00${page}` format the editor registers under;
 *  centralizing it here keeps consumers (menu Save, future writers) from
 *  drifting out of sync with the registration key. */
export function editorKey(
  notebook: string,
  section: string,
  page: string
): string {
  return `${notebook}\x00${section}\x00${page}`
}

/** Register a mounted editor. Returns an unregister function. */
export function registerEditor(handle: EditorHandle): () => void {
  editors.set(handle.key, handle)
  return () => {
    // Only delete if still ours (a re-registration may have replaced us).
    if (editors.get(handle.key) === handle) {
      editors.delete(handle.key)
    }
  }
}

/** Look up the mounted editor for a page key, if any. */
export function getEditor(key: string): EditorHandle | undefined {
  return editors.get(key)
}

/** All currently mounted editors (one per displayed tab). */
export function getAllEditors(): EditorHandle[] {
  return [...editors.values()]
}

/** Drop every registered editor handle. Called on vault close/switch so a
 *  teardown that bypasses Svelte's `$effect` cleanup can't leave handles
 *  holding closures (and autosave buffers) over the PREVIOUS vault — a stale
 *  handle would otherwise route a future flush into the wrong vault (#345). */
export function clearAllEditors(): void {
  editors.clear()
}

/** Test-only: clear the registry between tests. */
export function _resetEditorRegistryForTests(): void {
  editors.clear()
}
