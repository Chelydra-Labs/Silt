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
import { Events } from '@wailsio/runtime'
import { EventName } from '../../generated/enums'

// Editors register on mount and unregister on destroy. Lookups are keyed by
// the page triple (the same `\x00`-joined key the search/grouping code uses).

export interface EditorHandle {
  /** `${notebook}\x00${section}\x00${page}` — matches PageGroup.key. */
  key: string
  /** True if the editor holds unsaved edits not yet persisted to disk. */
  isDirty: () => boolean
  /** Flush the pending autosave; resolves true if the editor is clean after. */
  flush: () => Promise<boolean>
  /** Force the editor to reload from its blocks prop on the next external
   *  block update, bypassing the focused-edit guard. Only safe right after a
   *  flush synced the editor to disk, so there is nothing unsaved to clobber.
   *  `conflictHold: true` grades the write hold as conflict-grade (#1005) —
   *  used when a cross-surface conflict resolution made the buffer stale and
   *  discardable, so a keystroke cannot void the armed reload. */
  forceExternalReload: (opts?: { conflictHold?: boolean }) => void
  /** Drop a pending forceExternalReload if the out-of-band write failed.
   *  `conflict: true` is reserved for the resolution-failure path that
   *  armed a conflict-grade hold — the plain clear deliberately cannot
   *  strip one (#1005). */
  clearExternalReload: (opts?: { conflict?: boolean }) => void
  /** Move focus into the owning editor surface (ProseMirror or the Source
   *  textarea). Returns false when the surface has nothing focusable, so
   *  callers can fall back (#1005: post-resolution focus must land in the
   *  editor that owns the buffer, not the page chrome). */
  focusEditor?: () => boolean
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
  /** Resolve an external-edit save refusal (#1005): keep_mine / keep_theirs /
   *  keep_both. Rejects if the resolution IPC fails — the caller's banner
   *  keeps offering the actions. Resolves to the created conflict copy's
   *  page name for the success announcement (void when unavailable). Optional
   *  so non-editor surfaces can register without it. */
  resolveExternalConflict?: (
    resolution: 'keep_mine' | 'keep_theirs' | 'keep_both'
  ) => Promise<string | void>
}

// Imperative handle registry — not UI-reactive state.
// eslint-disable-next-line svelte/prefer-svelte-reactivity -- non-reactive registry
const editors = new Map<string, EditorHandle>()

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

/** Resolve an editor for a caller locator. Exact key first, then a
 *  case-insensitive match so plugin restore can flush a tab registered
 *  under the canonical triple when the caller passed a case variant. */
export function getEditorForLocator(
  notebook: string,
  section: string,
  page: string
): EditorHandle | undefined {
  const exact = editors.get(editorKey(notebook, section, page))
  if (exact) return exact
  const want = editorKey(notebook, section, page).toLowerCase()
  for (const [key, handle] of editors) {
    if (key.toLowerCase() === want) return handle
  }
  return undefined
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

/** Arm forceExternalReload for a page after a Go-side restore (Local MCP). */
export function bindPageExternalReload(): () => void {
  return Events.On(EventName.EventPageExternalReload, (event) => {
    const ev = event?.data as
      { notebook?: string; section?: string; page?: string } | undefined
    if (!ev?.notebook || ev.page == null) return
    getEditorForLocator(
      ev.notebook,
      ev.section ?? '',
      ev.page
    )?.forceExternalReload()
  })
}

/** Test helper: apply a page:external-reload payload to the registry. */
export function _applyPageExternalReloadForTests(ev: {
  notebook: string
  section: string
  page: string
}): void {
  getEditorForLocator(ev.notebook, ev.section, ev.page)?.forceExternalReload()
}
