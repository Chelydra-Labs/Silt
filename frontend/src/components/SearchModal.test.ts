import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  SearchBlocksPaged: vi.fn(),
  ListNavigation: vi.fn(),
  QueryTagHierarchy: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    SearchBlocksPaged: mocks.SearchBlocksPaged,
    ListNavigation: mocks.ListNavigation,
    QueryTagHierarchy: mocks.QueryTagHierarchy
  })
)

import SearchModal from './SearchModal.svelte'

describe('SearchModal keyboard a11y', () => {
  beforeEach(() => {
    mocks.SearchBlocksPaged.mockReset()
    mocks.ListNavigation.mockReset()
    mocks.QueryTagHierarchy.mockReset()
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
})
