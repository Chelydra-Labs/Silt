// Same-ID external reload: restore keeps block IDs, so the sync $effect
// must still setContent when forceExternalReload is armed.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, waitFor } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'
import {
  editorKey,
  getEditor,
  _resetEditorRegistryForTests
} from '../lib/editor/editorRegistry.svelte'

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
})
