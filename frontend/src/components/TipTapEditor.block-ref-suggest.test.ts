import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import { fireEvent, render } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'

const mocks = vi.hoisted(() => ({
  searchBlocks: vi.fn(),
  queryTagHierarchy: vi.fn().mockResolvedValue([]),
  recordTagUsage: vi.fn().mockResolvedValue(undefined),
  distinctOwners: vi.fn().mockResolvedValue([]),
  resolveBlockReference: vi.fn().mockResolvedValue({ exists: true }),
  saveFileBlocks: vi.fn().mockResolvedValue(undefined),
  acquireFocusLock: vi.fn().mockResolvedValue(undefined),
  refreshFocusLock: vi.fn().mockResolvedValue(undefined),
  releaseFocusLock: vi.fn().mockResolvedValue(undefined),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('../../bindings/silt/app.js', () => ({
  SearchBlocks: mocks.searchBlocks,
  QueryTagHierarchy: mocks.queryTagHierarchy,
  RecordTagUsage: mocks.recordTagUsage,
  DistinctOwners: mocks.distinctOwners,
  ResolveBlockReference: mocks.resolveBlockReference,
  SaveFileBlocks: mocks.saveFileBlocks,
  AcquireFocusLock: mocks.acquireFocusLock,
  RefreshFocusLock: mocks.refreshFocusLock,
  ReleaseFocusLock: mocks.releaseFocusLock
}))

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

vi.mock('../settings/store.svelte', () => ({
  settings: { config: null },
  saveConfig: vi.fn(),
  appendDismissedTip: vi.fn()
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

interface TestEditor {
  commands: {
    focus: (position?: string) => void
    insertContent: (content: string) => void
  }
  getJSON: () => {
    content?: Array<{
      content?: Array<{ type: string; attrs?: { uuid?: string } }>
    }>
  }
  state: { doc: { textContent: string } }
}

async function mountEditor() {
  const rendered = render(TipTapEditor, {
    props: {
      notebook: 'NB',
      section: 'S',
      page: 'P',
      blocks: [mkBlock('NOTE', { clean_text: '' })],
      onUpdate: () => {}
    }
  })
  await vi.advanceTimersByTimeAsync(0)
  await tick()
  const pm = rendered.container.querySelector('.ProseMirror') as unknown as {
    editor: TestEditor
  }
  return { ...rendered, editor: pm.editor }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((res, rej) => {
    resolve = res
    reject = rej
  })
  return { promise, resolve, reject }
}

const hit = (id: string, text: string) => ({
  id,
  notebook: 'NB',
  section: 'S',
  page: 'P',
  clean_content: text
})

describe('TipTapEditor block-reference typeahead', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.searchBlocks.mockResolvedValue([])
  })

  afterEach(() => {
    vi.useRealTimers()
  })

  it('opens on (( and picking a result inserts a live block reference node', async () => {
    mocks.searchBlocks.mockResolvedValue([hit('picked-id', 'Design decision')])
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('((design')
    await tick()

    expect(container.querySelector('.block-ref-suggest')).toBeTruthy()
    expect(container.textContent).toContain('Searching blocks')
    expect(mocks.searchBlocks).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(180)
    await tick()
    expect(mocks.searchBlocks).toHaveBeenCalledWith('design')

    const option = Array.from(
      container.querySelectorAll<HTMLButtonElement>('[role="option"]')
    ).find((button) => button.textContent?.includes('Design decision'))
    expect(option).toBeTruthy()
    await fireEvent.click(option!)
    await tick()

    const ref = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'blockReferenceNode')
    expect(ref?.attrs?.uuid).toBe('picked-id')
    expect(container.querySelector('.block-ref-suggest')).toBeNull()
    unmount()
  })

  it('Escape cancels the popup without changing trigger text', async () => {
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('((')
    await tick()
    expect(container.querySelector('.block-ref-suggest')).toBeTruthy()

    await fireEvent.keyDown(container.querySelector('.ProseMirror')!, {
      key: 'Escape'
    })
    await tick()
    expect(container.querySelector('.block-ref-suggest')).toBeNull()
    expect(editor.state.doc.textContent).toBe('((')
    unmount()
  })

  it('shows an error state without swallowing a later successful query', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.searchBlocks.mockRejectedValueOnce(new Error('offline'))
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('((first')
    await vi.advanceTimersByTimeAsync(180)
    await tick()
    expect(container.textContent).toContain('Block search unavailable')

    mocks.searchBlocks.mockResolvedValueOnce([hit('second-id', 'Recovered')])
    editor.commands.insertContent(' second')
    await vi.advanceTimersByTimeAsync(180)
    await tick()
    expect(container.textContent).toContain('Recovered')
    expect(errorSpy).toHaveBeenCalledWith(
      'SearchBlocks failed:',
      expect.any(Error)
    )
    errorSpy.mockRestore()
    unmount()
  })

  it('cancels the superseded request and ignores its late result', async () => {
    const first = deferred<ReturnType<typeof hit>[]>()
    const second = deferred<ReturnType<typeof hit>[]>()
    const cancel = vi.fn().mockResolvedValue(undefined)
    Object.assign(first.promise, { cancel })
    mocks.searchBlocks
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)

    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('((old')
    await vi.advanceTimersByTimeAsync(180)
    editor.commands.insertContent(' new')
    await tick()
    expect(cancel).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(180)

    second.resolve([hit('new-id', 'Current result')])
    await tick()
    first.resolve([hit('old-id', 'Stale result')])
    await tick()
    expect(container.textContent).toContain('Current result')
    expect(container.textContent).not.toContain('Stale result')
    unmount()
  })
})
