// Production-surface pin for the underline mark (#1030): the schema-level and
// component-level tests build their own extension lists / inject synthetic
// `activeMarks`, so a `underline: false` in TipTapEditor's StarterKit config
// or a dropped 'underline' in ALL_MARKS would leave them green. This file
// drives the real component and asserts the mark reaches the live schema and
// the selection bubble's aria-pressed state.

import { describe, it, expect, beforeEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, waitFor } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'

const mocks = vi.hoisted(() => ({
  queryTagHierarchy: vi.fn(),
  recordTagUsage: vi.fn().mockResolvedValue(undefined),
  distinctOwners: vi.fn().mockResolvedValue([]),
  saveFileBlocks: vi.fn().mockResolvedValue(undefined),
  acquireFocusLock: vi.fn().mockResolvedValue(undefined),
  refreshFocusLock: vi.fn().mockResolvedValue(undefined),
  releaseFocusLock: vi.fn().mockResolvedValue(undefined),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    QueryTagHierarchy: mocks.queryTagHierarchy,
    RecordTagUsage: mocks.recordTagUsage,
    DistinctOwners: mocks.distinctOwners,
    SaveFileBlocks: mocks.saveFileBlocks,
    AcquireFocusLock: mocks.acquireFocusLock,
    RefreshFocusLock: mocks.refreshFocusLock,
    ReleaseFocusLock: mocks.releaseFocusLock
  })
)

vi.mock('@wailsio/runtime', () => ({
  Events: { On: mocks.eventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {},
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

vi.mock('../notifications/store.svelte', () => ({ pushNotification: vi.fn() }))
vi.mock('../plugins/events', () => ({ dispatch: vi.fn() }))
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

// Placeholder's viewport tracker calls elementFromPoint; the bubble's
// coordsAtPos resolves caret rects through Range.getClientRects. jsdom has
// neither (same stub as the other TipTapEditor component tests).
if (typeof document !== 'undefined' && !document.elementFromPoint) {
  document.elementFromPoint = () => document.body
}
if (typeof window !== 'undefined' && window.Range) {
  const rect = {
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
  } as DOMRect
  Range.prototype.getClientRects = (() => [
    rect
  ]) as unknown as typeof Range.prototype.getClientRects
  Range.prototype.getBoundingClientRect = () => rect
}

interface LiveEditor {
  schema: { marks: Record<string, unknown> }
  commands: {
    selectAll: () => void
    setTextSelection: (range: { from: number; to: number }) => void
    toggleUnderline: () => void
  }
}

async function mountEditor() {
  const rendered = render(TipTapEditor, {
    props: {
      notebook: 'NB',
      section: 'S',
      page: 'P',
      blocks: [mkBlock('NOTE', { clean_text: 'underlined text' })],
      onUpdate: () => {}
    }
  })
  await waitFor(() => {
    expect(rendered.container.querySelector('.ProseMirror')).toBeTruthy()
  })
  const pm = rendered.container.querySelector('.ProseMirror') as HTMLElement & {
    editor: LiveEditor
  }
  return { ...rendered, editor: pm.editor }
}

describe('TipTapEditor underline production surface (#1030)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('registers underline in the live schema and reflects it in the format bubble', async () => {
    const { container, editor, unmount } = await mountEditor()

    // StarterKit.configure in the production component must leave underline on.
    expect(editor.schema.marks.underline).toBeDefined()

    // Select the whole block, underline it, then move the selection so the
    // production onSelectionUpdate recomputes activeMarks from ALL_MARKS.
    editor.commands.selectAll()
    editor.commands.toggleUnderline()
    editor.commands.setTextSelection({ from: 2, to: 6 })
    await tick()

    await waitFor(() => {
      const underlineBtn = container.querySelector(
        '[aria-label="Underline"]'
      ) as HTMLElement | null
      expect(underlineBtn).toBeTruthy()
      expect(underlineBtn?.getAttribute('aria-pressed')).toBe('true')
    })

    unmount()
  })
})
