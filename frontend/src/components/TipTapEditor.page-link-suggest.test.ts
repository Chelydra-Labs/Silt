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

  it('debounces search, preserves server order, and picks the shortest target', async () => {
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
      'Airplane Notes Vault · Work / Plans',
      'Planning Vault · Work / Plans',
      'Plan Vault · Work / Plans'
    ])

    await fireEvent.click(options[2])
    await tick()
    expect(mocks.resolvePageLink).toHaveBeenCalledWith('Work/Plans/Plan')
    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs).toMatchObject({ target: 'Plan', alias: null })
    expect(container.querySelector('.page-link-suggest')).toBeNull()
    unmount()
  })

  it('asks for two non-space characters before searching', async () => {
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[a ')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    expect(container.textContent).toContain('Type at least 2 characters')
    expect(mocks.searchPages).not.toHaveBeenCalled()

    editor.commands.insertContent('b')
    await vi.advanceTimersByTimeAsync(150)
    expect(mocks.searchPages).toHaveBeenCalledWith('a b', 50)
    unmount()
  })

  it('distinguishes colliding roots and inserts the linked source qualification', async () => {
    mocks.searchPages.mockResolvedValue([
      page('Roadmap'),
      { ...page('Roadmap'), source: 'linked:team-drive' }
    ])
    mocks.resolvePageLink.mockImplementation(async (target: string) => ({
      exists: true,
      shortest: target
    }))
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    const options = Array.from(
      container.querySelectorAll<HTMLButtonElement>('[role="option"]')
    )
    expect(options.map((option) => option.textContent?.trim())).toEqual([
      'Roadmap Vault · Work / Plans',
      'Roadmap Linked · team-drive · Work / Plans'
    ])
    await fireEvent.click(options[1])
    await tick()

    expect(mocks.resolvePageLink).toHaveBeenCalledWith(
      'linked:team-drive/Work/Plans/Roadmap'
    )
    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs?.target).toBe('linked:team-drive/Work/Plans/Roadmap')
    unmount()
  })

  it('uses an unqualified vault lookup while persisting the server shortest target', async () => {
    mocks.searchPages.mockResolvedValue([
      page('Roadmap'),
      { ...page('Roadmap'), source: 'linked:team-drive' }
    ])
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      shortest: 'Roadmap'
    })
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    const vaultOption = Array.from(
      container.querySelectorAll<HTMLButtonElement>('[role="option"]')
    ).find((option) => option.textContent?.includes('Vault'))!
    await fireEvent.click(vaultOption)
    await tick()

    expect(mocks.resolvePageLink).toHaveBeenCalledWith('Work/Plans/Roadmap')
    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs?.target).toBe('Roadmap')
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
    editor.commands.insertContent('[[ro')
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

  it('navigates page results with ArrowDown and ArrowUp from the alias input', async () => {
    mocks.searchPages.mockResolvedValue([
      page('Roadmap'),
      page('Roadmap Archive'),
      page('Roadmap Notes')
    ])
    const { container, getByRole, getByLabelText, editor, unmount } =
      await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    await fireEvent.click(getByRole('button', { name: 'Use display alias' }))
    const alias = getByLabelText('Page link display alias') as HTMLInputElement
    alias.focus()
    const options = () =>
      Array.from(
        container.querySelectorAll<HTMLButtonElement>('[role="option"]')
      )

    expect(
      options().map((option) => option.getAttribute('aria-selected'))
    ).toEqual(['true', 'false', 'false'])
    expect(await fireEvent.keyDown(alias, { key: 'ArrowDown' })).toBe(false)
    expect(
      options().map((option) => option.getAttribute('aria-selected'))
    ).toEqual(['false', 'true', 'false'])
    expect(document.activeElement).toBe(alias)

    expect(await fireEvent.keyDown(alias, { key: 'ArrowUp' })).toBe(false)
    expect(
      options().map((option) => option.getAttribute('aria-selected'))
    ).toEqual(['true', 'false', 'false'])
    expect(document.activeElement).toBe(alias)
    unmount()
  })

  it('selects the active page with Enter from the alias input', async () => {
    mocks.searchPages.mockResolvedValue([
      page('Roadmap'),
      page('Roadmap Archive')
    ])
    mocks.resolvePageLink.mockImplementation(async (target: string) => ({
      exists: true,
      ambiguous: false,
      shortest: target.endsWith('Roadmap Archive')
        ? 'Roadmap Archive'
        : 'Roadmap'
    }))
    const { getByRole, getByLabelText, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    await fireEvent.click(getByRole('button', { name: 'Use display alias' }))
    const alias = getByLabelText('Page link display alias') as HTMLInputElement
    await fireEvent.input(alias, { target: { value: 'Project history' } })
    await fireEvent.keyDown(alias, { key: 'ArrowDown' })
    expect(await fireEvent.keyDown(alias, { key: 'Enter' })).toBe(false)
    await tick()

    expect(mocks.resolvePageLink).toHaveBeenCalledWith(
      'Work/Plans/Roadmap Archive'
    )
    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs).toMatchObject({
      target: 'Roadmap Archive',
      alias: 'Project history'
    })
    unmount()
  })

  it('does not consume unrelated or composing keys in the alias input', async () => {
    mocks.searchPages.mockResolvedValue([
      page('Roadmap'),
      page('Roadmap Notes')
    ])
    const { container, getByRole, getByLabelText, editor, unmount } =
      await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    await fireEvent.click(getByRole('button', { name: 'Use display alias' }))
    const alias = getByLabelText('Page link display alias') as HTMLInputElement
    const typing = new KeyboardEvent('keydown', {
      key: 'a',
      bubbles: true,
      cancelable: true
    })
    alias.dispatchEvent(typing)
    expect(typing.defaultPrevented).toBe(false)

    const composingArrow = new KeyboardEvent('keydown', {
      key: 'ArrowDown',
      bubbles: true,
      cancelable: true,
      isComposing: true
    })
    alias.dispatchEvent(composingArrow)
    expect(composingArrow.defaultPrevented).toBe(false)
    expect(
      container
        .querySelector<HTMLButtonElement>('[role="option"]')
        ?.getAttribute('aria-selected')
    ).toBe('true')
    expect(mocks.resolvePageLink).not.toHaveBeenCalled()
    unmount()
  })

  it('sanitizes alias input and Escape closes the picker and restores editor focus', async () => {
    mocks.searchPages.mockResolvedValue([page('Roadmap')])
    const { container, getByRole, getByLabelText, editor, unmount } =
      await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[ro')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    await fireEvent.click(getByRole('button', { name: 'Use display alias' }))
    const alias = getByLabelText('Page link display alias') as HTMLInputElement
    await fireEvent.input(alias, {
      target: { value: 'Road]|map\nshared' }
    })
    // A single-line input drops pasted line breaks before input handling; the
    // remaining wiki-link delimiters are removed by our normalizer.
    expect(alias.value).toBe('Roadmapshared')

    await fireEvent.keyDown(alias, { key: 'Escape' })
    await tick()
    expect(container.querySelector('.page-link-suggest')).toBeNull()
    expect(document.activeElement).toBe(container.querySelector('.ProseMirror'))
    expect(editor.state.doc.textContent).toBe('[[ro')
    unmount()
  })

  it('shows search errors without changing typed text', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.searchPages.mockRejectedValueOnce(new Error('offline'))
    const { container, editor, unmount } = await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[of')
    await vi.advanceTimersByTimeAsync(150)
    await tick()
    expect(container.textContent).toContain('Page suggestions unavailable')
    expect(container.textContent).toContain('Retry search')
    expect(editor.state.doc.textContent).toBe('[[of')
    expect(errorSpy).toHaveBeenCalledWith(
      'SearchPages failed:',
      expect.any(Error)
    )
    errorSpy.mockRestore()
    unmount()
  })

  it('keeps prior results and alias available when a refreshed search fails', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    mocks.searchPages
      .mockResolvedValueOnce([page('Roadmap')])
      .mockRejectedValueOnce(new Error('offline'))
    const { container, getByRole, getByLabelText, editor, unmount } =
      await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    await fireEvent.click(getByRole('button', { name: 'Use display alias' }))
    await fireEvent.input(getByLabelText('Page link display alias'), {
      target: { value: 'Project map' }
    })
    editor.commands.focus('end')
    editor.commands.insertContent('x')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    expect(container.textContent).toContain('Couldn’t refresh suggestions')
    expect(getByRole('option', { name: /Roadmap/ })).toBeTruthy()
    expect(
      (getByLabelText('Page link display alias') as HTMLInputElement).value
    ).toBe('Project map')
    expect(editor.state.doc.textContent).toBe('[[roadx')
    errorSpy.mockRestore()
    unmount()
  })

  it('shows resolution progress, blocks duplicate picks, and retries without losing input', async () => {
    const errorSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
    const firstResolution = deferred<{
      exists: boolean
      ambiguous: boolean
      shortest: string
    }>()
    mocks.searchPages.mockResolvedValue([page('Roadmap')])
    mocks.resolvePageLink
      .mockReturnValueOnce(firstResolution.promise)
      .mockResolvedValueOnce({
        exists: true,
        ambiguous: false,
        shortest: 'Roadmap'
      })
    const { container, getByRole, getByLabelText, editor, unmount } =
      await mountEditor()
    editor.commands.focus('end')
    editor.commands.insertContent('[[road')
    await vi.advanceTimersByTimeAsync(150)
    await tick()

    await fireEvent.click(getByRole('button', { name: 'Use display alias' }))
    const alias = getByLabelText('Page link display alias') as HTMLInputElement
    await fireEvent.input(alias, { target: { value: 'Project map' } })
    const option = getByRole('option', { name: /Roadmap/ })
    await fireEvent.click(option)
    await tick()

    expect(container.textContent).toContain('Resolving Roadmap')
    expect(container.querySelectorAll('[role="option"]')).toHaveLength(0)
    await fireEvent.keyDown(container.querySelector('.ProseMirror')!, {
      key: 'Enter'
    })
    expect(mocks.resolvePageLink).toHaveBeenCalledTimes(1)
    expect(editor.state.doc.textContent).toBe('[[road')

    firstResolution.resolve({
      exists: false,
      ambiguous: false,
      shortest: ''
    })
    await vi.advanceTimersByTimeAsync(0)
    await tick()
    expect(container.textContent).toContain('Couldn’t insert this link')
    expect(container.textContent).toContain('Roadmap')
    expect(
      (getByLabelText('Page link display alias') as HTMLInputElement).value
    ).toBe('Project map')
    expect(editor.state.doc.textContent).toBe('[[road')

    await fireEvent.click(getByRole('button', { name: 'Retry link' }))
    await tick()
    expect(mocks.resolvePageLink).toHaveBeenNthCalledWith(
      2,
      'Work/Plans/Roadmap'
    )
    const link = editor
      .getJSON()
      .content?.[0].content?.find((node) => node.type === 'pageLinkNode')
    expect(link?.attrs).toMatchObject({
      target: 'Roadmap',
      alias: 'Project map'
    })
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
