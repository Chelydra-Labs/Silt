import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'

const mocks = vi.hoisted(() => ({
  SearchBlocksPaged: vi.fn(),
  ListNavigation: vi.fn(),
  QueryTagHierarchy: vi.fn(),
  DistinctOwners: vi.fn(),
  ResolvePageLink: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    SearchBlocksPaged: mocks.SearchBlocksPaged,
    ListNavigation: mocks.ListNavigation,
    QueryTagHierarchy: mocks.QueryTagHierarchy,
    DistinctOwners: mocks.DistinctOwners,
    ResolvePageLink: mocks.ResolvePageLink
  })
)

import SearchModal from './SearchModal.svelte'

describe('SearchModal', () => {
  beforeEach(() => {
    mocks.SearchBlocksPaged.mockReset()
    mocks.ListNavigation.mockReset()
    mocks.QueryTagHierarchy.mockReset()
    mocks.DistinctOwners.mockReset()
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [],
      total: 0,
      offset: 0,
      limit: 20,
      has_more: false
    })
    mocks.ListNavigation.mockResolvedValue({
      notebooks: [
        { name: 'Work', sections: [] },
        { name: 'Personal', sections: [] }
      ]
    })
    mocks.QueryTagHierarchy.mockResolvedValue([
      { name: 'project', path: 'project', count: 2, children: [] }
    ])
    mocks.DistinctOwners.mockResolvedValue(['Alice', 'Bob'])
    mocks.ResolvePageLink.mockReset()
    mocks.ResolvePageLink.mockResolvedValue({
      exists: true,
      shortest: 'Roadmap 2027'
    })
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) }
    })
  })
  afterEach(() => cleanup())

  it('keeps normal Tab order within the dialog', async () => {
    const onClose = vi.fn()
    render(SearchModal, {
      props: { onClose, onJump: vi.fn() }
    })

    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    input.focus()
    expect(document.activeElement).toBe(input)

    // Capture-phase Tab must not be swallowed by the modal. If preventDefault
    // ran, defaultPrevented would be true on the event after dispatch.
    const tabEvent = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true
    })
    const prevented =
      !window.dispatchEvent(tabEvent) || tabEvent.defaultPrevented
    expect(prevented).toBe(false)

    // Filter controls remain in the tab order (reachable buttons).
    expect(screen.getByRole('button', { name: 'Vault' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'All' })).toBeTruthy()
    expect(screen.getByRole('button', { name: 'Relevance' })).toBeTruthy()
  })

  it('traps focus at both dialog edges and restores prior focus on unmount', async () => {
    const priorButton = document.createElement('button')
    priorButton.textContent = 'Open search'
    document.body.appendChild(priorButton)
    priorButton.focus()

    const view = render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await vi.waitFor(() => expect(document.activeElement).toBe(input))

    const recent = screen.getByRole('button', { name: 'Recent' })
    recent.focus()
    await fireEvent.keyDown(window, { key: 'Tab' })
    expect(document.activeElement).toBe(input)

    input.focus()
    await fireEvent.keyDown(window, { key: 'Tab', shiftKey: true })
    expect(document.activeElement).toBe(recent)

    view.unmount()
    expect(document.activeElement).toBe(priorButton)
    priorButton.remove()
  })

  it('cycles type chips with ArrowLeft/Right when a chip is focused', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })

    const tasksChip = screen.getByRole('button', { name: 'Tasks' })
    tasksChip.focus()
    expect(tasksChip.getAttribute('aria-pressed')).toBe('false')

    await fireEvent.keyDown(window, { key: 'ArrowRight' })
    // From All (default) → Tasks on first right, but focus was on Tasks so
    // cycle advances from current typeFilter (All) to Tasks.
    // typeFilter starts as ''; ArrowRight → Tasks.
    const pressed = screen
      .getAllByRole('button')
      .find(
        (b) =>
          b.getAttribute('aria-pressed') === 'true' &&
          b.closest('[data-type-chips]')
      )
    expect(pressed?.textContent).toContain('Tasks')
  })

  it('passes notebook and tag filters to SearchBlocksPaged (#655)', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    await vi.waitFor(() => {
      expect(screen.getByRole('option', { name: 'Work' })).toBeInTheDocument()
    })

    const notebookSelect = screen.getByLabelText(
      'Filter by notebook'
    ) as HTMLSelectElement
    notebookSelect.value = 'Work'
    await fireEvent.change(notebookSelect)
    const tagInput = screen.getByLabelText('Filter by tag')
    await fireEvent.input(tagInput, { target: { value: 'project' } })

    // Active filter chips appear when filters are set.
    expect(screen.getByLabelText('Clear notebook filter')).toBeInTheDocument()
    expect(screen.getByLabelText('Clear tag filter')).toBeInTheDocument()

    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'hello' } })

    await vi.waitFor(() => {
      const calls = mocks.SearchBlocksPaged.mock.calls
      const match = calls.find(
        (c) =>
          c[0] === 'hello' &&
          c[3]?.notebook === 'Work' &&
          c[3]?.tag === 'project'
      )
      expect(match).toBeTruthy()
    })

    await fireEvent.click(screen.getByLabelText('Clear notebook filter'))
    expect(notebookSelect.value).toBe('')
  })

  it('does not steal Arrow/Enter from notebook select or tag input', async () => {
    const onJump = vi.fn()
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: '1',
          source: 'vault',
          notebook: 'Work',
          section: '',
          page: 'A',
          file_date: '',
          clean_content: 'hello'
        }
      ],
      total: 1,
      offset: 0,
      limit: 20,
      has_more: false
    })
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump }
    })
    await vi.waitFor(() => {
      expect(screen.getByRole('option', { name: 'Work' })).toBeInTheDocument()
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'hello' } })
    await vi.waitFor(() => {
      expect(mocks.SearchBlocksPaged).toHaveBeenCalled()
    })

    const notebookSelect = screen.getByLabelText('Filter by notebook')
    notebookSelect.focus()
    const enterEvt = new KeyboardEvent('keydown', {
      key: 'Enter',
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(enterEvt)
    // Capture handler must not preventDefault / jump when select is focused.
    expect(enterEvt.defaultPrevented).toBe(false)
    expect(onJump).not.toHaveBeenCalled()

    const tagInput = screen.getByLabelText('Filter by tag')
    tagInput.focus()
    const arrowEvt = new KeyboardEvent('keydown', {
      key: 'ArrowDown',
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(arrowEvt)
    expect(arrowEvt.defaultPrevented).toBe(false)
  })

  it('lets Enter activate a focused toolbar button instead of jumping to result 0', async () => {
    const onJump = vi.fn()
    const onClose = vi.fn()
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: '1',
          source: 'vault',
          notebook: 'Work',
          section: '',
          page: 'A',
          file_date: '',
          clean_content: 'hello'
        }
      ],
      total: 1,
      offset: 0,
      limit: 20,
      has_more: false
    })
    render(SearchModal, { props: { onClose, onJump } })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'hello' } })
    await vi.waitFor(() => {
      expect(mocks.SearchBlocksPaged).toHaveBeenCalled()
    })

    const toggle = screen.getByTestId('search-task-filters-toggle')
    toggle.focus()
    expect(document.activeElement).toBe(toggle)
    const enterEvt = new KeyboardEvent('keydown', {
      key: 'Enter',
      bubbles: true,
      cancelable: true
    })
    toggle.dispatchEvent(enterEvt)
    // jsdom does not synthesize the native click, so assert the capture
    // handler stayed out of the way and no result jump/close happened.
    expect(enterEvt.defaultPrevented).toBe(false)
    expect(onJump).not.toHaveBeenCalled()
    expect(onClose).not.toHaveBeenCalled()

    // The pointer path still opens the disclosure.
    await fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
  })

  it('sends empty notebook/tag when filters are cleared (#655)', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'x' } })
    await vi.waitFor(() => {
      expect(mocks.SearchBlocksPaged).toHaveBeenCalled()
    })
    const filters =
      mocks.SearchBlocksPaged.mock.calls[
        mocks.SearchBlocksPaged.mock.calls.length - 1
      ][3]
    expect(filters.notebook).toBe('')
    expect(filters.tag).toBe('')
  })

  it('shows Clear active filters for scope and sort and resets both defaults', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'nothing' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Vault' }))
    await fireEvent.click(screen.getByRole('button', { name: 'Recent' }))

    const clear = await screen.findByRole('button', {
      name: 'Clear active filters'
    })
    await fireEvent.click(clear)

    expect(screen.getByRole('button', { name: '+ Linked' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(screen.getByRole('button', { name: 'Relevance' })).toHaveAttribute(
      'aria-pressed',
      'true'
    )
    expect(
      screen.queryByRole('button', { name: 'Clear active filters' })
    ).not.toBeInTheDocument()

    await vi.waitFor(() => {
      const calls = mocks.SearchBlocksPaged.mock.calls
      expect(
        calls.some(
          (call) =>
            call[0] === 'nothing' &&
            call[3]?.vaultOnly === false &&
            call[3]?.sort === 'relevance'
        )
      ).toBe(true)
    })
  })

  it('renders standalone (.silt) results without a label or path leak', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 'st-1',
          source: 'vault',
          notebook: '.silt',
          section: '',
          page: 'tasks.md',
          file_date: '2026-07-23',
          clean_content: 'Standalone task from quick-add',
          snippet: null
        }
      ],
      total: 1,
      offset: 0,
      limit: 20,
      has_more: false
    })
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'quick-add' } })
    await vi.waitFor(() => {
      expect(
        screen.getByText('Standalone task from quick-add')
      ).toBeInTheDocument()
    })
    // No redundant "Standalone task" label; no .silt path leak; source
    // badge and file date remain for context.
    expect(screen.queryByText('Standalone task')).toBeNull()
    expect(screen.queryByText('.silt')).toBeNull()
    expect(screen.queryByText('tasks.md')).toBeNull()
    expect(screen.getByLabelText('Vault source')).toBeInTheDocument()
    expect(screen.getByText('2026-07-23')).toBeInTheDocument()
  })

  it('shows the typed source qualifier and sends identical click/Enter navigation payloads', async () => {
    const result = {
      id: 'block-712',
      source: 'linked:team-drive',
      notebook: 'Work',
      section: 'Meetings',
      page: 'Weekly sync',
      file_date: '2026-07-22',
      clean_content: 'Discuss the launch sequence',
      snippet: 'Discuss the <mark>launch</mark> sequence'
    }
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [result],
      total: 1,
      offset: 0,
      limit: 20,
      has_more: false
    })

    const clickJump = vi.fn()
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: clickJump }
    })
    const clickInput = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(clickInput, { target: { value: 'launch' } })
    await vi.waitFor(() => {
      expect(screen.getByLabelText('Linked source')).toBeInTheDocument()
    })
    expect(screen.getAllByText('Work')).toHaveLength(2)
    expect(screen.getByText('Meetings')).toBeInTheDocument()
    expect(screen.getByText('Weekly sync')).toBeInTheDocument()
    expect(screen.getByText('launch')).toBeInTheDocument()
    await fireEvent.click(screen.getByRole('button', { name: /launch/i }))

    cleanup()
    const enterJump = vi.fn()
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: enterJump }
    })
    const enterInput = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(enterInput, { target: { value: 'launch' } })
    await vi.waitFor(() => {
      expect(screen.getByLabelText('Linked source')).toBeInTheDocument()
    })
    await fireEvent.keyDown(window, { key: 'Enter' })

    expect(clickJump).toHaveBeenCalledWith(result)
    expect(enterJump).toHaveBeenCalledWith(result)
    expect(enterJump.mock.calls[0][0]).toEqual(clickJump.mock.calls[0][0])
  })

  it('sends task-attribute filters to SearchBlocksPaged (#1010)', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    await fireEvent.click(screen.getByTestId('search-task-filters-toggle'))

    const status = screen.getByLabelText(
      'Filter by task status'
    ) as HTMLSelectElement
    status.value = 'DOING'
    await fireEvent.change(status)

    const owner = screen.getByLabelText('Filter by owner')
    await fireEvent.input(owner, { target: { value: 'alice' } })

    await fireEvent.click(screen.getByRole('button', { name: 'P2+' }))
    await fireEvent.click(screen.getByTestId('search-overdue-toggle'))

    const dueAfter = screen.getByLabelText('Due on or after')
    await fireEvent.input(dueAfter, { target: { value: '2026-01-01' } })
    const dueBefore = screen.getByLabelText('Due on or before')
    await fireEvent.input(dueBefore, { target: { value: '2026-12-31' } })

    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'postgres' } })

    await vi.waitFor(() => {
      const match = mocks.SearchBlocksPaged.mock.calls.find(
        (c) =>
          c[0] === 'postgres' &&
          c[3]?.status === 'DOING' &&
          c[3]?.owner === 'alice' &&
          c[3]?.priorityMin === 2 &&
          c[3]?.overdue === true &&
          c[3]?.dueAfter === '2026-01-01' &&
          c[3]?.dueBefore === '2026-12-31'
      )
      expect(match).toBeTruthy()
    })
  })

  it('shows the page-title suppression hint only while a filter hides it', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    expect(screen.queryByTestId('search-title-leg-hint')).toBeNull()

    await fireEvent.click(screen.getByTestId('search-task-filters-toggle'))
    await fireEvent.click(screen.getByRole('button', { name: 'P1+' }))
    expect(screen.getByTestId('search-title-leg-hint')).toBeInTheDocument()

    // Clearing the priority floor restores the title leg (no hint).
    await fireEvent.click(screen.getByLabelText('Clear priority filter'))
    expect(screen.queryByTestId('search-title-leg-hint')).toBeNull()
  })

  it('groups task-attribute controls behind a Task filters disclosure (#1071)', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const toggle = screen.getByTestId('search-task-filters-toggle')
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    // Collapsed: the attribute controls are out of the tab order/density.
    expect(screen.queryByLabelText('Filter by task status')).toBeNull()
    // Query-adjacent controls stay visible.
    expect(screen.getByRole('button', { name: 'All' })).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Relevance' })
    ).toBeInTheDocument()

    await fireEvent.click(toggle)
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByLabelText('Filter by task status')).toBeInTheDocument()
    expect(
      screen.queryByTestId('search-task-filters-count')
    ).not.toBeInTheDocument()

    await fireEvent.click(screen.getByRole('button', { name: 'P2+' }))
    expect(screen.getByTestId('search-task-filters-count')).toHaveTextContent(
      '1'
    )

    // Collapsing keeps the badge (active filters remain in effect).
    await fireEvent.click(toggle)
    expect(screen.queryByLabelText('Filter by task status')).toBeNull()
    expect(screen.getByTestId('search-task-filters-count')).toHaveTextContent(
      '1'
    )
  })

  it('seeds the owner datalist from DistinctOwners and explains task-row semantics (#1071)', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    await fireEvent.click(screen.getByTestId('search-task-filters-toggle'))

    await vi.waitFor(() => {
      expect(mocks.DistinctOwners).toHaveBeenCalledWith('')
      const datalist = document.getElementById(
        'search-owner-options'
      ) as HTMLDataListElement
      expect(Array.from(datalist.options).map((o) => o.value)).toEqual([
        'Alice',
        'Bob'
      ])
    })

    await fireEvent.click(screen.getByRole('button', { name: 'P1+' }))
    expect(screen.getByTestId('search-title-leg-hint')).toHaveTextContent(
      /notes and headings are hidden/i
    )
  })

  it('renders page-level rows and opens them with the empty block id', async () => {
    const pageRow = {
      id: '',
      source: 'vault',
      notebook: 'Work',
      section: 'Plans',
      page: 'Roadmap 2027',
      file_date: '',
      clean_content: 'Roadmap 2027',
      snippet: '<mark>Roadmap</mark> 2027'
    }
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [pageRow],
      total: 1,
      offset: 0,
      limit: 20,
      has_more: false
    })
    const onJump = vi.fn()
    render(SearchModal, { props: { onClose: vi.fn(), onJump } })

    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const row = await screen.findByRole('button', { name: /Roadmap 2027/i })
    expect(screen.getByText('Page')).toBeInTheDocument()
    await fireEvent.click(row)
    expect(onJump).toHaveBeenCalledWith(pageRow)
  })

  it('displays task attributes projected on result rows', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        {
          id: 't1',
          source: 'vault',
          notebook: 'Work',
          section: 'Sprint',
          page: 'Sprint 41',
          file_date: '2026-07-20',
          clean_content: 'Ship search filters',
          status: 'DOING',
          owner: 'alice',
          due_date: '2026-08-01',
          priority: 1,
          snippet: 'Ship <mark>search</mark> filters'
        }
      ],
      total: 1,
      offset: 0,
      limit: 20,
      has_more: false
    })
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'search' } })
    const meta = await screen.findByTestId('search-result-task-meta')
    expect(meta).toHaveTextContent('DOING')
    expect(meta).toHaveTextContent('alice')
    expect(meta).toHaveTextContent('2026-08-01')
    expect(meta).toHaveTextContent('P1')
  })
})

// Page-locator context menu on result rows (#1120): page-title hits AND
// block/task hits (their locator names a real notes page); standalone `.silt`
// rows are suppressed — their click target is the Tasks view, not a page.
describe('SearchModal — result-row context menu (#1120)', () => {
  const pageRow = {
    id: '',
    source: 'vault',
    notebook: 'Work',
    section: 'Plans',
    page: 'Roadmap 2027',
    file_date: '',
    clean_content: 'Roadmap 2027',
    snippet: '<mark>Roadmap</mark> 2027'
  }
  const taskRow = {
    id: 'block-9',
    source: 'vault',
    notebook: 'Work',
    section: 'Sprint',
    page: 'Sprint 41',
    file_date: '2026-07-20',
    clean_content: 'Ship the locator menu'
  }

  beforeEach(() => {
    mocks.SearchBlocksPaged.mockReset()
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [pageRow, taskRow],
      total: 2,
      offset: 0,
      limit: 20,
      has_more: false
    })
  })

  async function searchAndRow(name: RegExp): Promise<HTMLElement> {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn(), onReplaceInVault: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const row = await screen.findByRole('button', { name })
    expect(row).toHaveAttribute('aria-keyshortcuts', 'Shift+F10')
    return row
  }

  function menuLabels(): string[] {
    return Array.from(
      screen
        .getByTestId('search-locator-menu')
        .querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  it('right-click a page result opens the locator menu', async () => {
    const row = await searchAndRow(/Roadmap 2027/i)
    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 120,
      clientY: 90
    })
    row.dispatchEvent(e)
    await tick()
    expect(e.defaultPrevented).toBe(true)
    const menu = screen.getByTestId('search-locator-menu')
    expect(menu).toHaveAttribute('role', 'menu')
    expect(menu.getAttribute('style')).toContain('left: 120px')
    expect(menu.getAttribute('style')).toContain('top: 90px')
    expect(menuLabels()).toEqual([
      'Copy page reference',
      'Reveal in sidebar',
      'Replace in vault…'
    ])
  })

  it('block/task results share the menu; standalone .silt rows get none', async () => {
    mocks.SearchBlocksPaged.mockResolvedValue({
      results: [
        taskRow,
        {
          id: 'st-1',
          source: 'vault',
          notebook: '.silt',
          section: '',
          page: 'tasks.md',
          file_date: '2026-07-23',
          clean_content: 'Standalone task from quick-add'
        }
      ],
      total: 2,
      offset: 0,
      limit: 20,
      has_more: false
    })
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn(), onReplaceInVault: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'task' } })

    const taskResult = await screen.findByRole('button', {
      name: /Ship the locator menu/i
    })
    await fireEvent.contextMenu(taskResult, { clientX: 5, clientY: 5 })
    await tick()
    expect(screen.getByTestId('search-locator-menu')).toBeInTheDocument()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Close context menu' })
    )

    const standalone = screen.getByRole('button', {
      name: /Standalone task from quick-add/i
    })
    expect(standalone).not.toHaveAttribute('aria-keyshortcuts')
    await fireEvent.contextMenu(standalone, { clientX: 5, clientY: 5 })
    await tick()
    expect(screen.queryByTestId('search-locator-menu')).toBeNull()
  })

  it('copy page reference resolves the shortest form and writes it', async () => {
    const row = await searchAndRow(/Roadmap 2027/i)
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    await fireEvent.click(screen.getByTestId('search-menu-copy-page-reference'))
    expect(screen.queryByTestId('search-locator-menu')).toBeNull()
    await vi.waitFor(() =>
      expect(mocks.ResolvePageLink).toHaveBeenCalledWith(
        'Work/Plans/Roadmap 2027'
      )
    )
    await vi.waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith(
        '[[Roadmap 2027]]'
      )
    )
  })

  it('reveal jumps to the result and closes the modal', async () => {
    const onJump = vi.fn()
    const onClose = vi.fn()
    render(SearchModal, {
      props: { onClose, onJump, onReplaceInVault: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const row = await screen.findByRole('button', { name: /Roadmap 2027/i })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    await fireEvent.click(screen.getByTestId('search-menu-reveal-in-sidebar'))
    expect(onJump).toHaveBeenCalledWith(pageRow)
    expect(onClose).toHaveBeenCalled()
  })

  it('replace in vault uses the toolbar handler with the live query', async () => {
    const onReplaceInVault = vi.fn()
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn(), onReplaceInVault }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const row = await screen.findByRole('button', { name: /Roadmap 2027/i })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    await fireEvent.click(screen.getByTestId('search-menu-replace-in-vault'))
    expect(screen.queryByTestId('search-locator-menu')).toBeNull()
    expect(onReplaceInVault).toHaveBeenCalledWith('roadmap')
  })

  it('omits the replace-in-vault item when the host does not provide it', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const row = await screen.findByRole('button', { name: /Roadmap 2027/i })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    expect(menuLabels()).toEqual(['Copy page reference', 'Reveal in sidebar'])
  })

  it('Shift+F10 opens the menu; Escape dismisses the menu but not the modal', async () => {
    const onClose = vi.fn()
    render(SearchModal, {
      props: { onClose, onJump: vi.fn(), onReplaceInVault: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const row = await screen.findByRole('button', { name: /Roadmap 2027/i })
    row.focus()

    await fireEvent.keyDown(row, { key: 'F10', shiftKey: true })
    await tick()
    const menu = screen.getByTestId('search-locator-menu')
    expect(menu.contains(document.activeElement)).toBe(true)

    // Dispatch on the focused menu item (production path): the Dialog's
    // capture handler must stand down so the menu's own Escape dismissal —
    // and only that — runs.
    await fireEvent.keyDown(document.activeElement!, { key: 'Escape' })
    await tick()
    expect(screen.queryByTestId('search-locator-menu')).toBeNull()
    expect(document.activeElement).toBe(row)
    expect(onClose).not.toHaveBeenCalled()
    expect(input).toBeInTheDocument()

    // Escape with no menu open closes the modal as before.
    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(onClose).toHaveBeenCalled()
  })

  it('arrow keys rove inside the open menu without moving the modal selection', async () => {
    render(SearchModal, {
      props: { onClose: vi.fn(), onJump: vi.fn(), onReplaceInVault: vi.fn() }
    })
    const input = screen.getByPlaceholderText(
      /Search notebooks, sections, or task content/i
    )
    await fireEvent.input(input, { target: { value: 'roadmap' } })
    const first = await screen.findByRole('button', { name: /Roadmap 2027/i })
    await fireEvent.contextMenu(first, { clientX: 5, clientY: 5 })
    await tick()

    // Both rows rendered; selection starts at 0 (Roadmap). Roving Down inside
    // the menu must not advance the modal's highlighted result.
    const before = first.className
    await fireEvent.keyDown(document.activeElement!, { key: 'ArrowDown' })
    await tick()
    const items = screen
      .getByTestId('search-locator-menu')
      .querySelectorAll('[role="menuitem"]')
    expect(document.activeElement).toBe(items[1])
    expect(first.className).toBe(before)
  })
})
