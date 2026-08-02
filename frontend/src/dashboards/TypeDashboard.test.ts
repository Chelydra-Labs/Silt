import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor,
  within
} from '@testing-library/svelte'

// Two pages of a "book" type so the table, sort, filter, and group paths all
// have something to render against.
const BOOK_ROWS = [
  {
    source: 'vault',
    notebook: 'Work',
    section: 'Reading',
    page: 'Dune',
    properties: [
      { name: 'title', valueText: 'Dune', valueType: 'text' },
      { name: 'rating', valueText: '5', valueType: 'number' },
      { name: 'status', valueText: 'read', valueType: 'select' },
      { name: 'finished', valueText: 'true', valueType: 'checkbox' }
    ]
  },
  {
    source: 'vault',
    notebook: 'Work',
    section: 'Reading',
    page: 'Neuromancer',
    properties: [
      { name: 'title', valueText: 'Neuromancer', valueType: 'text' },
      { name: 'rating', valueText: '3', valueType: 'number' },
      { name: 'status', valueText: 'reading', valueType: 'select' },
      { name: 'finished', valueText: 'false', valueType: 'checkbox' }
    ]
  }
]

const BOOK_TYPE = {
  id: 'book',
  name: 'Book',
  icon: 'menu_book',
  heroField: 'title',
  properties: [
    { name: 'title', label: 'Title', type: 'text' },
    { name: 'rating', label: 'Rating', type: 'number' },
    {
      name: 'status',
      label: 'Status',
      type: 'select',
      options: ['read', 'reading']
    },
    { name: 'finished', label: 'Finished', type: 'checkbox' }
  ]
}

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListTypes: vi.fn(),
    QueryPagesByType: vi.fn()
  })
)
vi.mock('$silt-app', () => appMocks)

import TypeDashboard from './TypeDashboard.svelte'

beforeEach(() => {
  appMocks.ListTypes.mockReset()
  appMocks.QueryPagesByType.mockReset()
  appMocks.ListTypes.mockResolvedValue({ types: [BOOK_TYPE] })
  appMocks.QueryPagesByType.mockResolvedValue(BOOK_ROWS)
})

afterEach(cleanup)

async function mount(props: Record<string, unknown> = {}) {
  const onOpenPage = vi.fn()
  const onBack = vi.fn()
  const rendered = render(TypeDashboard, {
    props: { typeName: 'book', onOpenPage, onBack, ...props }
  })
  // Wait for ListTypes + the debounced QueryPagesByType to resolve.
  await waitFor(() => {
    expect(appMocks.QueryPagesByType).toHaveBeenCalled()
  })
  await tick()
  return { rendered, onOpenPage, onBack }
}

describe('TypeDashboard', () => {
  it('renders the type picker with types from ListTypes', async () => {
    await mount()
    // The type picker button shows the selected type's name.
    expect(screen.getByRole('button', { name: /^Book/ })).toBeInTheDocument()
    // Open the picker — the option is present.
    await fireEvent.click(screen.getByRole('button', { name: /^Book/ }))
    const listbox = screen.getByRole('listbox', { name: 'Select a type' })
    expect(within(listbox).getByText('Book')).toBeInTheDocument()
  })

  it('renders the table with pages from QueryPagesByType', async () => {
    await mount()
    // Both page names render as navigation buttons.
    expect(
      screen.getByRole('button', { name: 'Open page Dune' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Open page Neuromancer' })
    ).toBeInTheDocument()
  })

  it('column header click toggles sort direction and re-queries', async () => {
    await mount()
    // The QueryPagesByType on mount used the default sort (page path, asc).
    const initialCall = appMocks.QueryPagesByType.mock.calls.length

    // Click the "Rating" column header to sort ascending by rating.
    await fireEvent.click(
      screen.getByRole('button', { name: 'Sort by Rating' })
    )
    await waitFor(() => {
      expect(appMocks.QueryPagesByType.mock.calls.length).toBeGreaterThan(
        initialCall
      )
    })
    const lastCall = appMocks.QueryPagesByType.mock.calls.at(-1)!
    expect(lastCall[0]).toBe('book')
    expect(lastCall[2]).toBe('rating')
    expect(lastCall[3]).toBe(false)

    // Click again → toggles to descending.
    await fireEvent.click(
      screen.getByRole('button', { name: 'Sort by Rating' })
    )
    await waitFor(() => {
      const desc = appMocks.QueryPagesByType.mock.calls.at(-1)!
      expect(desc[2]).toBe('rating')
      expect(desc[3]).toBe(true)
    })
    // aria-sort reflects the descending state on the Rating header cell.
    const ratingHeader = screen
      .getByRole('button', { name: 'Sort by Rating' })
      .closest('th')
    expect(ratingHeader).toHaveAttribute('aria-sort', 'descending')
  })

  it('passes filter state to QueryPagesByType', async () => {
    await mount()
    const before = appMocks.QueryPagesByType.mock.calls.length
    // The Status filter is a dropdown (select kind with options).
    const statusButton = screen.getByRole('button', { name: 'Filter Status' })
    await fireEvent.click(statusButton)
    // Pick the "read" option from the open listbox.
    const statusListbox = screen.getByRole('listbox', { name: 'Filter Status' })
    await fireEvent.click(within(statusListbox).getByText('read'))
    // Debounced reload (180ms) fires with the filter applied.
    await waitFor(() => {
      const last = appMocks.QueryPagesByType.mock.calls.at(-1)!
      expect(last[1]).toEqual({ status: 'read' })
    })
    expect(appMocks.QueryPagesByType.mock.calls.length).toBeGreaterThan(before)
  })

  it('bins rows into collapsible sections when group-by is set', async () => {
    await mount()
    // Open the group-by picker.
    await fireEvent.click(screen.getByRole('button', { name: /Group:/ }))
    const groupListbox = screen.getByRole('listbox', { name: 'Group by' })
    await fireEvent.click(within(groupListbox).getByText('Status'))
    await tick()

    // Two distinct status values → two group toggle buttons, each labelled
    // with its value + count.
    const readToggle = screen.getByRole('button', {
      name: /read group, \d+ pages/
    })
    const readingToggle = screen.getByRole('button', {
      name: /reading group, \d+ pages/
    })
    expect(readToggle).toBeInTheDocument()
    expect(readingToggle).toBeInTheDocument()

    // Both pages are visible before collapsing.
    expect(
      screen.getByRole('button', { name: 'Open page Dune' })
    ).toBeInTheDocument()

    // Collapse the "read" group → its Dune row disappears; Neuromancer stays.
    await fireEvent.click(readToggle)
    await tick()
    expect(screen.queryByRole('button', { name: 'Open page Dune' })).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Open page Neuromancer' })
    ).toBeInTheDocument()
  })

  it('clicking a page row calls onOpenPage with the locator', async () => {
    const { onOpenPage } = await mount()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Open page Dune' })
    )
    expect(onOpenPage).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
  })

  it('shows an empty state when no types are defined', async () => {
    appMocks.ListTypes.mockResolvedValue({ types: [] })
    render(TypeDashboard, {
      props: { typeName: '', onOpenPage: vi.fn(), onBack: vi.fn() }
    })
    await waitFor(() => {
      expect(screen.getByText('No types defined yet')).toBeInTheDocument()
    })
    expect(appMocks.QueryPagesByType).not.toHaveBeenCalled()
  })

  it('shows an empty state when the query returns no pages', async () => {
    appMocks.QueryPagesByType.mockResolvedValue([])
    await mount()
    expect(screen.getByText('No pages of this type')).toBeInTheDocument()
  })

  it('the Back button calls onBack', async () => {
    const { onBack } = await mount()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Back to editor' })
    )
    expect(onBack).toHaveBeenCalledOnce()
  })
})
