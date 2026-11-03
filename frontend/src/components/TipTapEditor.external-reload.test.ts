// Same-ID external reload: restore keeps block IDs, so the sync $effect
// must still setContent when forceExternalReload is armed.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, waitFor, fireEvent } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'
import {
  editorKey,
  getEditor,
  _resetEditorRegistryForTests
} from '../lib/editor/editorRegistry.svelte'
import { getActiveEditor } from '../lib/editor/activeEditor.svelte'

if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (
  typeof window !== 'undefined' &&
  window.Range &&
  !Range.prototype.getClientRects
) {
  const zeroRect: DOMRect = {
    x: 0,
    y: 0,
    left: 0,
    top: 0,
    right: 0,
    bottom: 0,
    width: 0,
    height: 0,
    toJSON() {
      return this
    }
  }
  Range.prototype.getClientRects = (() => [
    zeroRect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => zeroRect
}

const mocks = vi.hoisted(() => ({
  saveFileBlocks: vi.fn().mockResolvedValue(undefined),
  resolveExternalConflict: vi
    .fn()
    .mockResolvedValue('Journal-2026-06-22.conflict-20260622-090000'),
  acquireFocusLock: vi.fn().mockResolvedValue(undefined),
  refreshFocusLock: vi.fn().mockResolvedValue(undefined),
  releaseFocusLock: vi.fn().mockResolvedValue(undefined),
  openDevTools: vi.fn().mockResolvedValue(undefined),
  distinctOwners: vi.fn().mockResolvedValue([]),
  queryTagHierarchy: vi.fn().mockResolvedValue([]),
  recordTagUsage: vi.fn().mockResolvedValue(undefined),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    SaveFileBlocks: mocks.saveFileBlocks,
    ResolveExternalConflict: mocks.resolveExternalConflict,
    AcquireFocusLock: mocks.acquireFocusLock,
    RefreshFocusLock: mocks.refreshFocusLock,
    ReleaseFocusLock: mocks.releaseFocusLock,
    OpenDevTools: mocks.openDevTools,
    DistinctOwners: mocks.distinctOwners,
    QueryTagHierarchy: mocks.queryTagHierarchy,
    RecordTagUsage: mocks.recordTagUsage
  })
)

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: <T>(fn: T) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('../settings/store.svelte', () => ({
  settings: { config: null },
  saveConfig: vi.fn(),
  appendDismissedTip: vi.fn()
}))

vi.mock('../theme/store.svelte', () => ({
  themeState: { mode: 'dark' }
}))

vi.mock('../notifications/store.svelte', () => ({
  pushNotification: vi.fn()
}))

vi.mock('../plugins/events', () => ({
  dispatch: vi.fn()
}))

vi.mock('../lib/perf/frame-budget', () => ({
  measureFrameBudget: vi.fn((_label: string, fn: () => unknown) => fn())
}))

vi.mock('../lib/editor/spellcheck/dictionary', () => ({
  loadDictionary: vi.fn().mockResolvedValue({ loaded: true }),
  isDictionaryLoaded: vi.fn(() => true),
  resetDictionary: vi.fn(),
  setCustomWords: vi.fn(),
  setDomainWords: vi.fn(),
  loadDomainPacks: vi.fn().mockResolvedValue(undefined),
  checkWord: vi.fn(() => true),
  ignoreWordSession: vi.fn(),
  suggest: vi.fn(() => []),
  getDictionaryLoadError: vi.fn(() => null),
  parseWordListText: vi.fn(() => [])
}))

const STABLE_ID = 'aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee'

function pmText(container: HTMLElement): string {
  return container.querySelector('.ProseMirror')?.textContent ?? ''
}

describe('TipTapEditor same-ID external reload', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    _resetEditorRegistryForTests()
  })
  afterEach(() => {
    _resetEditorRegistryForTests()
  })

  it('applies restored body when block IDs stay the same', async () => {
    const first = [
      mkBlock('NOTE', { id: STABLE_ID, clean_text: 'before restore' })
    ]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    expect(pmText(view.container)).toContain('before restore')

    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    expect(handle).toBeTruthy()
    handle!.forceExternalReload()

    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'after restore' })],
      onUpdate: () => {}
    })

    await waitFor(() => {
      expect(pmText(view.container)).toContain('after restore')
    })
    expect(pmText(view.container)).not.toContain('before restore')
    view.unmount()
  })

  it('releases the write hold when restore matches the on-screen body', async () => {
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'same body' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    expect(handle).toBeTruthy()
    handle!.forceExternalReload()
    await expect(handle!.flush()).resolves.toBe(false)

    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'same body' })],
      onUpdate: () => {}
    })

    await waitFor(async () => {
      await expect(handle!.flush()).resolves.toBe(true)
    })
    view.unmount()
  })

  it('applies a same-ID body change without forceExternalReload when unfocused', async () => {
    const first = [
      mkBlock('NOTE', { id: STABLE_ID, clean_text: 'before restore' })
    ]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'after restore' })],
      onUpdate: () => {}
    })
    await waitFor(() => {
      expect(pmText(view.container)).toContain('after restore')
    })
    view.unmount()
  })

  // #1005: the registry handle routes conflict resolution through the
  // autosave manager — keep_mine settles clean, keep_theirs arms the
  // external-reload hold until the post-resolution blocks land.
  it('resolves an external-edit conflict via the registry handle', async () => {
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    expect(handle).toBeTruthy()

    // keep_mine: no write hold — the backend replayed the refused save.
    await handle!.resolveExternalConflict!('keep_mine')
    expect(mocks.resolveExternalConflict).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'keep_mine'
    )
    await expect(handle!.flush()).resolves.toBe(true)

    // keep_theirs: writes are held until the armed reload applies fresh
    // blocks (the same-key armed branch releases).
    await handle!.resolveExternalConflict!('keep_theirs')
    expect(mocks.resolveExternalConflict).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'keep_theirs'
    )
    await expect(handle!.flush()).resolves.toBe(false)

    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'their text' })],
      onUpdate: () => {}
    })
    await waitFor(async () => {
      await expect(handle!.flush()).resolves.toBe(true)
    })
    expect(pmText(view.container)).toContain('their text')
    view.unmount()
  })

  // #1005: an unrelated registry actor's clearExternalReload (history
  // restore failure, MCP reload handler) must not void a conflict-grade arm
  // — dropping pendingExternalReload would leave the held buffer savable on
  // the next keystroke, over the version the user chose to keep.
  it('clearExternalReload is a no-op while a conflict hold is armed', async () => {
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    expect(handle).toBeTruthy()

    await handle!.resolveExternalConflict!('keep_theirs')
    await expect(handle!.flush()).resolves.toBe(false)

    // The unrelated clear must not release the armed reload…
    handle!.clearExternalReload()
    await expect(handle!.flush()).resolves.toBe(false)

    // …so the post-resolution blocks still land and release the hold.
    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'their text' })],
      onUpdate: () => {}
    })
    await waitFor(async () => {
      await expect(handle!.flush()).resolves.toBe(true)
    })
    expect(pmText(view.container)).toContain('their text')
    view.unmount()
  })

  // #1005 non-owning surface: while a conflict-grade hold is armed, the
  // buffer was declared discardable and the armed reload will replace
  // whatever is typed — so the editor must be read-only with an inline
  // notice instead of accepting-and-then-destroying keystrokes, and must
  // become editable again when the armed reload lands.
  it('is read-only while a conflict hold is armed, editable when the reload lands', async () => {
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    expect(
      view.container
        .querySelector('.ProseMirror')
        ?.getAttribute('contenteditable')
    ).toBe('true')
    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    expect(handle).toBeTruthy()

    // Cross-surface resolution arms the conflict-grade reload.
    handle!.forceExternalReload({ conflictHold: true })
    await waitFor(() => {
      expect(
        view.container
          .querySelector('.ProseMirror')
          ?.getAttribute('contenteditable')
      ).toBe('false')
    })
    expect(view.container.querySelector('.conflict-hold-notice')).toBeTruthy()
    await expect(handle!.flush()).resolves.toBe(false)

    // The armed reload lands: content replaces, the hold releases, and the
    // surface is editable again with the notice gone.
    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'their text' })],
      onUpdate: () => {}
    })
    await waitFor(() => {
      expect(
        view.container
          .querySelector('.ProseMirror')
          ?.getAttribute('contenteditable')
      ).toBe('true')
    })
    expect(view.container.querySelector('.conflict-hold-notice')).toBeNull()
    expect(pmText(view.container)).toContain('their text')
    view.unmount()
  })

  // #1005 typed-past flush: keep_mine/keep_both must flush the debounce tail
  // through the stash BEFORE the resolve IPC — the settle's block:changed
  // replaces the blurred buffer with the settled blocks, so an un-flushed
  // tail would vanish from the doc. The flush re-refuses newest-wins and the
  // resolve pops that newest stash, so the settled outcome is this buffer.
  it('flushes typed-past edits before keep_mine resolves', async () => {
    const calls: string[] = []
    mocks.saveFileBlocks.mockImplementation(async () => {
      calls.push('save')
      return undefined
    })
    mocks.resolveExternalConflict.mockImplementation(async () => {
      calls.push('resolve')
      return 'Journal.conflict-20260622-090000'
    })
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    // Type past the refusal: the editor is dirty when the banner is clicked.
    // Focus routes the live instance through the active-editor tracker; the
    // bindable prop is not readable from the test harness.
    const pmEl = view.container.querySelector('.ProseMirror') as HTMLElement
    await fireEvent.focus(pmEl)
    const editor = getActiveEditor()
    expect(editor).toBeTruthy()
    editor!.commands.insertContent('typed tail')
    // Click resolve immediately — well inside the 500ms debounce, so the
    // only path that can save before the resolve IPC is the flush itself.
    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    await handle!.resolveExternalConflict!('keep_mine')
    expect(calls).toEqual(['save', 'resolve'])
    view.unmount()
  })

  // #1005 IPC freeze: the backend pops the stash inside the resolve IPC, so
  // keystrokes landing during the round-trip could never be re-stashed — the
  // surface must be read-only for the window (parity with the source viewer).
  it('freezes the editor for the resolution IPC window', async () => {
    let releaseResolve!: (v: string) => void
    mocks.resolveExternalConflict.mockImplementation(
      async () =>
        new Promise<string>((res) => {
          releaseResolve = res
        })
    )
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    const pm = () => view.container.querySelector('.ProseMirror')
    expect(pm()!.getAttribute('contenteditable')).toBe('true')

    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    const settled = handle!.resolveExternalConflict!('keep_both') as Promise<
      string | void
    >
    await waitFor(() => {
      expect(pm()!.getAttribute('contenteditable')).toBe('false')
    })
    releaseResolve('Journal.conflict-20260622-090000')
    await settled
    await waitFor(() => {
      expect(pm()!.getAttribute('contenteditable')).toBe('true')
    })
    view.unmount()
  })

  // #1005 history boundary: the armed reload (keep_theirs settle) must reset
  // the undo history — TipTap's setContent is an undoable replace, so one
  // Ctrl+Z would resurrect the refused-era buffer and its undo-driven
  // autosave would clobber the kept version on the re-baselined page.
  it('armed reload is a history boundary: undo cannot resurrect the refused buffer', async () => {
    // Earlier tests in this file override the resolve mock with deferred
    // promises; vi.clearAllMocks does not restore implementations, so pin it.
    mocks.resolveExternalConflict.mockResolvedValue(
      'Journal-2026-06-22.conflict-20260622-090000'
    )
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    const pmEl = view.container.querySelector('.ProseMirror') as HTMLElement
    await fireEvent.focus(pmEl)
    const editor = getActiveEditor()
    expect(editor).toBeTruthy()
    // Refused-era typing: one undo step away from resurrection after the
    // reload if the boundary is missing.
    editor!.commands.insertContent('refused-era typing')

    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    await handle!.resolveExternalConflict!('keep_theirs')
    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'their text' })],
      onUpdate: () => {}
    })
    await waitFor(() => {
      expect(pmText(view.container)).toContain('their text')
    })

    expect(editor!.can().undo()).toBe(false)
    // Belt-and-braces: even a forced undo command must not bring the text back.
    editor!.commands.undo()
    expect(pmText(view.container)).not.toContain('refused-era typing')
    view.unmount()
  })

  // #1005 equal-content settle echo: after keep_mine resolves while blurred,
  // the settle echo can carry a re-rendered raw_text (backend id-comment
  // normalization) whose doc is IDENTICAL to the endorsed buffer. The
  // replace branch used to setContent + resetUndoHistory unconditionally —
  // wiping the user's own pre-conflict undo history while protecting
  // nothing (there is no replacement to draw a boundary for).
  it('equal-content settle echo preserves the undo history', async () => {
    // Earlier tests override the resolve mock with deferred promises; pin it.
    mocks.resolveExternalConflict.mockResolvedValue(
      'Journal-2026-06-22.conflict-20260622-090000'
    )
    const first = [mkBlock('NOTE', { id: STABLE_ID, clean_text: 'my text' })]
    const view = render(TipTapEditor, {
      props: {
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily',
        blocks: first,
        onUpdate: () => {}
      }
    })
    await waitFor(() => {
      expect(view.container.querySelector('.ProseMirror')).toBeTruthy()
    })
    const pmEl = view.container.querySelector('.ProseMirror') as HTMLElement
    await fireEvent.focus(pmEl)
    const editor = getActiveEditor()
    expect(editor).toBeTruthy()
    editor!.commands.insertContent('typed tail')
    // keep_mine resolves while blurred: the settle echo must not cost the
    // user their undo history.
    await fireEvent.blur(pmEl)
    const handle = getEditor(editorKey('Work', 'Journal', 'Daily'))
    await handle!.resolveExternalConflict!('keep_mine')
    await view.rerender({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      blocks: [
        mkBlock('NOTE', {
          id: STABLE_ID,
          clean_text: 'typed tailmy text',
          raw_text:
            'typed tailmy text <!-- id: ' + STABLE_ID + ' @ 2026-01-02 -->'
        })
      ],
      onUpdate: () => {}
    })
    await new Promise((r) => setTimeout(r, 50))

    // The echo was doc-equal: undo still reaches the pre-typing state and
    // the text still matches the endorsed buffer.
    expect(editor!.can().undo()).toBe(true)
    expect(pmText(view.container)).toContain('typed tailmy text')
    editor!.commands.undo()
    expect(pmText(view.container)).not.toContain('typed tail')
    expect(pmText(view.container)).toContain('my text')
    view.unmount()
  })
})
