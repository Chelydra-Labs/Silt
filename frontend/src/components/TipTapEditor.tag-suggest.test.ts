import { beforeEach, describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import { fireEvent, render, waitFor } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'
import { settings } from '../settings/store.svelte'

const mocks = vi.hoisted(() => ({
  queryTagHierarchy: vi.fn(),
  recordTagUsage: vi.fn().mockResolvedValue(undefined),
  distinctOwners: vi.fn().mockResolvedValue([]),
  searchBlocks: vi.fn().mockResolvedValue([]),
  resolveBlockReference: vi.fn().mockResolvedValue({ exists: true }),
  saveFileBlocks: vi.fn().mockResolvedValue(undefined),
  acquireFocusLock: vi.fn().mockResolvedValue(undefined),
  refreshFocusLock: vi.fn().mockResolvedValue(undefined),
  releaseFocusLock: vi.fn().mockResolvedValue(undefined),
  eventsOn: vi.fn(() => () => {})
}))

vi.mock('../../bindings/silt/app.js', () => ({
  QueryTagHierarchy: mocks.queryTagHierarchy,
  RecordTagUsage: mocks.recordTagUsage,
  DistinctOwners: mocks.distinctOwners,
  SearchBlocks: mocks.searchBlocks,
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
  await waitFor(() => {
    expect(rendered.container.querySelector('.ProseMirror')).toBeTruthy()
  })
  const pm = rendered.container.querySelector('.ProseMirror') as unknown as {
    editor: TestEditor
  }
  return { ...rendered, editor: pm.editor }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

describe('TipTapEditor tag typeahead', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    settings.config = {
      ui: { recent_tags: ['work/project'] }
    } as unknown as typeof settings.config
    mocks.queryTagHierarchy.mockResolvedValue([
      { name: 'archive', path: 'archive', count: 20, children: [] },
      {
        name: 'work',
        path: 'work',
        count: 5,
        children: [
          {
            name: 'project',
            path: 'work/project',
            count: 2,
            children: []
          }
        ]
      }
    ])
  })

  it('shows recents first and records a picked literal tag path', async () => {
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('#')
    await tick()

    const options = Array.from(
      container.querySelectorAll<HTMLButtonElement>('[role="option"]')
    )
    expect(options.map((option) => option.textContent?.trim())).toEqual([
      '#work/project 2 uses',
      '#archive 20 uses',
      '#work 5 uses'
    ])

    await fireEvent.click(options[0])
    await tick()
    expect(editor.state.doc.textContent).toBe('#work/project')
    expect(mocks.recordTagUsage).toHaveBeenCalledWith('work/project')
    expect(container.querySelector('.tag-suggest')).toBeNull()
    unmount()
  })

  it('refreshes an expired hierarchy when the picker opens and keeps cached tags visible', async () => {
    const now = vi.spyOn(Date, 'now').mockReturnValue(10_000)
    const refresh = deferred<
      Array<{
        name: string
        path: string
        count: number
        children: never[]
      }>
    >()
    const { container, editor, unmount } = await mountEditor()
    await waitFor(() =>
      expect(mocks.queryTagHierarchy).toHaveBeenCalledTimes(1)
    )

    editor.commands.focus('end')
    editor.commands.insertContent('#')
    await tick()
    expect(mocks.queryTagHierarchy).toHaveBeenCalledTimes(1)

    await fireEvent.keyDown(container.querySelector('.ProseMirror')!, {
      key: 'Escape'
    })
    now.mockReturnValue(15_001)
    mocks.queryTagHierarchy.mockReturnValueOnce(refresh.promise)
    editor.commands.insertContent(' #')
    await tick()

    expect(mocks.queryTagHierarchy).toHaveBeenCalledTimes(2)
    expect(container.textContent).toContain('#work/project')
    expect(container.textContent).toContain('Loading tags')

    refresh.resolve([{ name: 'fresh', path: 'fresh', count: 1, children: [] }])
    await waitFor(() => expect(container.textContent).toContain('#fresh'))
    expect(container.textContent).toContain('1 use')
    unmount()
    now.mockRestore()
  })

  it('surfaces hierarchy errors without breaking typed text', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.queryTagHierarchy.mockRejectedValueOnce(new Error('offline'))
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('#')
    await waitFor(() => {
      expect(container.textContent).toContain('Tag suggestions unavailable')
    })

    expect(editor.state.doc.textContent).toBe('#')
    expect(errorSpy).toHaveBeenCalledWith(
      'QueryTagHierarchy failed:',
      expect.any(Error)
    )
    errorSpy.mockRestore()
    unmount()
  })

  it('reorders an open picker on hot config without remounting the editor', async () => {
    const { container, editor, unmount } = await mountEditor()
    const editorElement = container.querySelector('.ProseMirror')
    editor.commands.focus('end')
    editor.commands.insertContent('#')
    await tick()
    expect(container.querySelector('[role="option"]')?.textContent).toContain(
      '#work/project'
    )

    settings.config = {
      ui: { recent_tags: ['archive'] }
    } as unknown as typeof settings.config
    await tick()

    expect(container.querySelector('[role="option"]')?.textContent).toContain(
      '#archive'
    )
    expect(container.querySelector('.ProseMirror')).toBe(editorElement)
    unmount()
  })
})
