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

// A second type whose schema lacks Book's `status` property — switching to it
// with a stale status filter would blank the dashboard.
const MOVIE_TYPE = {
  id: 'movie',
  name: 'Movie',
  icon: 'film',
  heroField: 'title',
  properties: [
    { name: 'title', label: 'Title', type: 'text' },
    { name: 'director', label: 'Director', type: 'text' }
  ]
}

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListTypes: vi.fn(),
    QueryPagesByType: vi.fn()
  })
)
vi.mock('$silt-app', () => appMocks)

// Capture Events.On registrations so the `types:changed` handler can be fired
// in-test (mirrors pageTypeState.test.ts's approach).
const eventsHandlers = {} as Record<string, (ev?: { data?: unknown }) => void>
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: vi.fn((name: string, handler: (ev?: { data?: unknown }) => void) => {
      eventsHandlers[name] = handler
      return () => {}
    })
  }
}))

import TypeDashboard from './TypeDashboard.svelte'

beforeEach(() => {
  appMocks.ListTypes.mockReset()
  appMocks.QueryPagesByType.mockReset()
  appMocks.ListTypes.mockResolvedValue({ types: [BOOK_TYPE] })
  appMocks.QueryPagesByType.mockResolvedValue(BOOK_ROWS)
  for (const k of Object.keys(eventsHandlers)) delete eventsHandlers[k]
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

  it('type picker listbox supports arrow-key navigation with roving tabindex', async () => {
    appMocks.ListTypes.mockResolvedValue({ types: [BOOK_TYPE, MOVIE_TYPE] })
    await mount()

    await fireEvent.click(screen.getByRole('button', { name: /^Book/ }))
    const listbox = screen.getByRole('listbox', { name: 'Select a type' })
    const options = within(listbox).getAllByRole('option')
    expect(options).toHaveLength(2)

    // On open, focus moves to the selected option (Book); it carries the
    // roving tabindex=0, siblings are -1.
    await waitFor(() => {
      expect(document.activeElement).toBe(options[0])
    })
    expect(options[0]).toHaveAttribute('tabindex', '0')
    expect(options[1]).toHaveAttribute('tabindex', '-1')

    // ArrowDown moves focus + the roving tab stop to the next option.
    await fireEvent.keyDown(options[0], { key: 'ArrowDown' })
    await waitFor(() => {
      expect(document.activeElement).toBe(options[1])
    })
    expect(options[1]).toHaveAttribute('tabindex', '0')
    expect(options[0]).toHaveAttribute('tabindex', '-1')

    // ArrowUp moves back.
    await fireEvent.keyDown(options[1], { key: 'ArrowUp' })
    await waitFor(() => {
      expect(document.activeElement).toBe(options[0])
    })

    // Home/End jump to boundaries.
    await fireEvent.keyDown(options[0], { key: 'End' })
    await waitFor(() => {
      expect(document.activeElement).toBe(options[1])
    })
    await fireEvent.keyDown(options[1], { key: 'Home' })
    await waitFor(() => {
      expect(document.activeElement).toBe(options[0])
    })

    // Enter activates the focused option (browser synthesizes a click on a
    // real <button>; jsdom does not, so fire one to mirror the real behavior).
    await fireEvent.keyDown(options[0], { key: 'ArrowDown' })
    await waitFor(() => expect(document.activeElement).toBe(options[1]))
    await fireEvent.click(options[1])
    // Movie was selected → next query targets the movie type, with the
    // type-switch filter reset.
    await waitFor(() => {
      const last = appMocks.QueryPagesByType.mock.calls.at(-1)!
      expect(last[0]).toBe('movie')
      expect(last[1]).toEqual({})
    })
  })

  it('renders the table with pages from QueryPagesByType', async () => {
    await mount()
    // Both page names render as navigation buttons.
    // Accessible name now carries hero + notebook + section context (mirrors
    // the board card); match by prefix so this test isn't brittle to format.
    expect(
      screen.getByRole('button', { name: /Open page Dune/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: /Open page Neuromancer/ })
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
      screen.getByRole('button', { name: /Open page Dune/ })
    ).toBeInTheDocument()

    // Collapse the "read" group → its Dune row disappears; Neuromancer stays.
    await fireEvent.click(readToggle)
    await tick()
    expect(screen.queryByRole('button', { name: /Open page Dune/ })).toBeNull()
    expect(
      screen.getByRole('button', { name: /Open page Neuromancer/ })
    ).toBeInTheDocument()
  })

  it('clicking a page row calls onOpenPage with the locator', async () => {
    const { onOpenPage } = await mount()
    await fireEvent.click(
      screen.getByRole('button', { name: /Open page Dune/ })
    )
    expect(onOpenPage).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
  })

  it('clears the filter when switching types so the new schema starts fresh', async () => {
    appMocks.ListTypes.mockResolvedValue({ types: [BOOK_TYPE, MOVIE_TYPE] })
    await mount()

    // Apply a status filter valid only on the Book type.
    await fireEvent.click(screen.getByRole('button', { name: 'Filter Status' }))
    const statusListbox = screen.getByRole('listbox', { name: 'Filter Status' })
    await fireEvent.click(within(statusListbox).getByText('read'))
    await waitFor(() => {
      expect(appMocks.QueryPagesByType.mock.calls.at(-1)![1]).toEqual({
        status: 'read'
      })
    })

    // Switch to the Movie type — the stale Book-only status filter must drop.
    await fireEvent.click(screen.getByRole('button', { name: /^Book/ }))
    const typeListbox = screen.getByRole('listbox', { name: 'Select a type' })
    await fireEvent.click(within(typeListbox).getByText('Movie'))

    // The next query for the Movie type carries no stale Book filter.
    await waitFor(() => {
      const last = appMocks.QueryPagesByType.mock.calls.at(-1)!
      expect(last[0]).toBe('movie')
      expect(last[1]).toEqual({})
    })
  })

  it('reloads types and refreshes the query on the types:changed event', async () => {
    await mount()
    const listCallsAfterMount = appMocks.ListTypes.mock.calls.length
    const queryCallsAfterMount = appMocks.QueryPagesByType.mock.calls.length

    // Fire the subscribed `types:changed` handler (a type file was edited
    // externally while the dashboard is mounted).
    eventsHandlers['types:changed']?.()
    await waitFor(() => {
      expect(appMocks.ListTypes.mock.calls.length).toBeGreaterThan(
        listCallsAfterMount
      )
    })
    await waitFor(() => {
      expect(appMocks.QueryPagesByType.mock.calls.length).toBeGreaterThan(
        queryCallsAfterMount
      )
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

  it('shows "No matches" with a Clear-filters button when filters exclude all rows', async () => {
    // First query (mount, no filter) returns rows so the table renders; the
    // second query (after a filter is applied) returns [] to simulate the
    // "filters excluded everything" branch.
    appMocks.QueryPagesByType.mockResolvedValueOnce(BOOK_ROWS)
    appMocks.QueryPagesByType.mockResolvedValueOnce([])
    await mount()

    // Apply a status filter — debounced reload fires with filter={status:'read'}.
    const statusButton = screen.getByRole('button', { name: 'Filter Status' })
    await fireEvent.click(statusButton)
    const statusListbox = screen.getByRole('listbox', { name: 'Filter Status' })
    await fireEvent.click(within(statusListbox).getByText('read'))
    await waitFor(() => {
      expect(screen.getByText('No matches')).toBeInTheDocument()
    })
    // Clear-filters button is present.
    const clearBtn = screen.getByRole('button', { name: /Clear filter/ })
    expect(clearBtn).toBeInTheDocument()
    // Clicking it resets the filter and the rows re-appear (next mock returns rows).
    appMocks.QueryPagesByType.mockResolvedValue(BOOK_ROWS)
    await fireEvent.click(clearBtn)
    await waitFor(() => {
      expect(
        screen.getByRole('button', { name: /Open page Dune/ })
      ).toBeInTheDocument()
    })
  })

  describe('board view', () => {
    it('switching to Board renders a card per page with the page name + hero', async () => {
      await mount()
      // Toggle to Board via the view-mode radiogroup.
      await fireEvent.click(screen.getByRole('radio', { name: 'Board view' }))
      // The cards keep the same aria-labels (page name + hero) the table rows
      // surfaced; assert both are visible in the board.
      expect(screen.getByRole('button', { name: /^Dune/ })).toBeInTheDocument()
      expect(
        screen.getByRole('button', { name: /^Neuromancer/ })
      ).toBeInTheDocument()
    })

    it('clicking a board card calls onOpenPage with the locator', async () => {
      const { onOpenPage } = await mount()
      await fireEvent.click(screen.getByRole('radio', { name: 'Board view' }))
      await fireEvent.click(screen.getByRole('button', { name: /^Dune/ }))
      expect(onOpenPage).toHaveBeenCalledWith({
        source: 'vault',
        notebook: 'Work',
        section: 'Reading',
        page: 'Dune'
      })
    })

    it('Enter on a focused board card opens the page', async () => {
      const { onOpenPage } = await mount()
      await fireEvent.click(screen.getByRole('radio', { name: 'Board view' }))
      const card = screen.getByRole('button', { name: /^Dune/ })
      card.focus()
      await fireEvent.keyDown(card, { key: 'Enter' })
      expect(onOpenPage).toHaveBeenCalledWith({
        source: 'vault',
        notebook: 'Work',
        section: 'Reading',
        page: 'Dune'
      })
    })

    it('renders one column per group value, each with its count', async () => {
      await mount()
      // Group by Status so the two distinct values produce two columns.
      await fireEvent.click(screen.getByRole('button', { name: /Group:/ }))
      const groupListbox = screen.getByRole('listbox', { name: 'Group by' })
      await fireEvent.click(within(groupListbox).getByText('Status'))
      await tick()
      await fireEvent.click(screen.getByRole('radio', { name: 'Board view' }))

      // Each column is a role=group labelled "label (count)".
      const readCol = screen.getByRole('group', { name: /^read \(\d+\)/ })
      const readingCol = screen.getByRole('group', { name: /^reading \(\d+\)/ })
      expect(readCol).toBeInTheDocument()
      expect(readingCol).toBeInTheDocument()
      // The "read" column contains Dune (page-name span); the "reading"
      // column contains Neuromancer. Both spans (name + hero) render the
      // same text for these rows, so use getAllByText and assert presence.
      expect(within(readCol).getAllByText('Dune').length).toBeGreaterThan(0)
      expect(
        within(readingCol).getAllByText('Neuromancer').length
      ).toBeGreaterThan(0)
    })

    it('view-mode radiogroup supports arrow-key navigation', async () => {
      await mount()
      const listRadio = screen.getByRole('radio', { name: 'List view' })
      const boardRadio = screen.getByRole('radio', { name: 'Board view' })
      // List is active on mount.
      expect(listRadio).toHaveAttribute('aria-checked', 'true')
      // Click Board to make it the active mode, then ArrowLeft wraps to List.
      await fireEvent.click(boardRadio)
      expect(boardRadio).toHaveAttribute('aria-checked', 'true')
      boardRadio.focus()
      await fireEvent.keyDown(boardRadio, { key: 'ArrowLeft' })
      await tick()
      expect(listRadio).toHaveAttribute('aria-checked', 'true')
      expect(boardRadio).toHaveAttribute('aria-checked', 'false')
    })
  })
})
