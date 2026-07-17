import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  SearchBlocksPaged: vi.fn(),
  ListNavigation: vi.fn(),
  QueryTagHierarchy: vi.fn()
}))

vi.mock('../../bindings/silt/app.js', () => ({
  SearchBlocksPaged: mocks.SearchBlocksPaged,
  ListNavigation: mocks.ListNavigation,
  QueryTagHierarchy: mocks.QueryTagHierarchy
}))

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

  it('does not preventDefault Tab — focus order reaches filter controls', async () => {
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
})
