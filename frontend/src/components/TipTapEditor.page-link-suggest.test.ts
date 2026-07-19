import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { tick } from 'svelte'
import { fireEvent, render } from '@testing-library/svelte'
import TipTapEditor from './TipTapEditor.svelte'
import { mkBlock } from '../lib/editor/nodeview-test-harness'

const mocks = vi.hoisted(() => ({
  searchPages: vi.fn(),
  resolvePageLink: vi.fn(),
  queryTagHierarchy: vi.fn().mockResolvedValue([]),
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
  SearchPages: mocks.searchPages,
  ResolvePageLink: mocks.resolvePageLink,
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
      content?: Array<{
        type: string
        attrs?: { target?: string; alias?: string | null }
      }>
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
  const promise = new Promise<T>((res) => {
    resolve = res
  })
  return { promise, resolve }
}

const page = (name: string, section = 'Plans') => ({
  source: 'vault',
  notebook: 'Work',
  section,
  page: name
})

describe('TipTapEditor page-link typeahead', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    vi.clearAllMocks()
    mocks.searchPages.mockResolvedValue([])
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      ambiguous: false,
      shortest: 'Plan'
    })
  })

  afterEach(() => vi.useRealTimers())

  it('debounces search, displays fuzzy-ranked pages, and picks the shortest target', async () => {
    mocks.searchPages.mockResolvedValue([
      page('Airplane Notes'),
      page('Planning'),
      page('Plan')
    ])
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[plan')
    await tick()

    expect(container.textContent).toContain('Searching pages')
    expect(mocks.searchPages).not.toHaveBeenCalled()
    await vi.advanceTimersByTimeAsync(150)
    await tick()
    expect(mocks.searchPages).toHaveBeenCalledWith('plan', 50)
    const options = Array.from(
      container.querySelectorAll<HTMLButtonElement>('[role="option"]')
    )
    expect(options.map((option) => option.textContent?.trim())).toEqual([
      'Plan Work / Plans',
      'Planning Work / Plans',
      'Airplane Notes Work / Plans'
    ])

    await fireEvent.click(options[0])
    await tick()
    expect(mocks.resolvePageLink).toHaveBeenCalledWith('Work/Plans/Plan')
    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs).toMatchObject({ target: 'Plan', alias: null })
    expect(container.querySelector('.page-link-suggest')).toBeNull()
    unmount()
  })

  it('offers an accessible alias control and inserts its value', async () => {
    mocks.searchPages.mockResolvedValue([page('Roadmap')])
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      shortest: 'Roadmap'
    })
    const { container, getByRole, getByLabelText, editor, unmount } =
      await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    const toggle = getByRole('button', { name: 'Use display alias' })
    expect(toggle.getAttribute('aria-pressed')).toBe('false')
    await fireEvent.click(toggle)
    const alias = getByLabelText('Page link display alias')
    await fireEvent.input(alias, { target: { value: 'Project map' } })
    await fireEvent.click(getByRole('option', { name: /Roadmap/ }))
    await tick()

    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs?.alias).toBe('Project map')
    unmount()
  })

  it('shows search errors without changing typed text', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.searchPages.mockRejectedValueOnce(new Error('offline'))
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[')
    await vi.advanceTimersByTimeAsync(150)
    await tick()
    expect(container.textContent).toContain('Page suggestions unavailable')
    expect(editor.state.doc.textContent).toBe('[[')
    expect(errorSpy).toHaveBeenCalledWith(
      'SearchPages failed:',
      expect.any(Error)
    )
    errorSpy.mockRestore()
    unmount()
  })

  it('cancels a superseded request and ignores its late result', async () => {
    const first = deferred<ReturnType<typeof page>[]>()
    const second = deferred<ReturnType<typeof page>[]>()
    const cancel = vi.fn().mockResolvedValue(undefined)
    Object.assign(first.promise, { cancel })
    mocks.searchPages
      .mockReturnValueOnce(first.promise)
      .mockReturnValueOnce(second.promise)
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[old')
    await vi.advanceTimersByTimeAsync(150)
    editor.commands.insertContent(' new')
    await tick()
    expect(cancel).toHaveBeenCalledOnce()
    await vi.advanceTimersByTimeAsync(150)

    second.resolve([page('Old New Current result')])
    await tick()
    first.resolve([page('Old New Stale result')])
    await tick()
    expect(container.textContent).toContain('Current result')
    expect(container.textContent).not.toContain('Stale result')
    unmount()
  })
})
