import { describe, expect, it, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'

import TypeDashboardBoard from './TypeDashboardBoard.svelte'
import type {
  DashboardColumn,
  GroupSection,
  TypeDashboardRow
} from './dashboards'

// Component-level tests for the board view. The integration paths through
// TypeDashboard cover the wiring (toggle, onOpenPage, group-by); this file
// covers render details that the binning integration can't reach — notably
// the "No pages" placeholder for an empty section (binByProperty never
// emits sections without rows, so the placeholder only matters when the
// board is mounted directly with a section whose rows were filtered out).

const BOOK_COLUMNS: DashboardColumn[] = [
  { key: '', label: 'Page', kind: 'page-name' },
  { key: 'title', label: 'Title', kind: 'text' },
  {
    key: 'status',
    label: 'Status',
    kind: 'select',
    options: ['read', 'reading']
  },
  { key: 'rating', label: 'Rating', kind: 'number' }
]

function makeRow(page: string, hero: string, status: string): TypeDashboardRow {
  return {
    source: 'vault',
    notebook: 'Work',
    section: 'Reading',
    page,
    properties: [
      { name: 'title', valueText: hero, valueType: 'text' },
      { name: 'status', valueText: status, valueType: 'select' },
      { name: 'rating', valueText: '5', valueType: 'number' }
    ]
  }
}

afterEach(cleanup)

describe('TypeDashboardBoard', () => {
  it('renders one column per group with the group label + count', () => {
    const sections: GroupSection[] = [
      {
        key: 's::read',
        label: 'read',
        items: [makeRow('Dune', 'Dune', 'read')]
      },
      {
        key: 's::reading',
        label: 'reading',
        items: [makeRow('Neuromancer', 'Neuromancer', 'reading')]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage: vi.fn()
      }
    })
    expect(
      screen.getByRole('group', { name: /^read \(1\)/ })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('group', { name: /^reading \(1\)/ })
    ).toBeInTheDocument()
  })

  it('renders a card per row showing page name + hero subtitle', () => {
    const sections: GroupSection[] = [
      {
        key: 's::read',
        label: 'read',
        items: [makeRow('Dune', 'Dune', 'read')]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage: vi.fn()
      }
    })
    // Card aria-label includes both page name and hero.
    const card = screen.getByRole('button', { name: /^Dune, Dune/ })
    expect(card).toBeInTheDocument()
    // Name + hero both render the page title here — assert at least one
    // matching node exists inside the card.
    expect(within(card).getAllByText('Dune').length).toBeGreaterThan(0)
  })

  it('clicking a card calls onOpenPage with the full locator', async () => {
    const onOpenPage = vi.fn()
    const sections: GroupSection[] = [
      {
        key: 's::read',
        label: 'read',
        items: [makeRow('Dune', 'Dune', 'read')]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage
      }
    })
    await fireEvent.click(screen.getByRole('button', { name: /^Dune/ }))
    expect(onOpenPage).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
  })

  it('Space on a focused card opens the page', async () => {
    const onOpenPage = vi.fn()
    const sections: GroupSection[] = [
      {
        key: 's::read',
        label: 'read',
        items: [makeRow('Dune', 'Dune', 'read')]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage
      }
    })
    const card = screen.getByRole('button', { name: /^Dune/ })
    card.focus()
    await fireEvent.keyDown(card, { key: ' ' })
    expect(onOpenPage).toHaveBeenCalled()
  })

  it('shows a "No pages" placeholder inside an empty group column', () => {
    // binByProperty never emits empty sections, but the board still defends
    // against one so future groupings (e.g. all-schema-options binning)
    // render correctly without a special case at the call site.
    const sections: GroupSection[] = [
      { key: 's::read', label: 'read', items: [] },
      {
        key: 's::reading',
        label: 'reading',
        items: [makeRow('Neuromancer', 'Neuromancer', 'reading')]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage: vi.fn()
      }
    })
    const readCol = screen.getByRole('group', { name: /^read \(0\)/ })
    expect(within(readCol).getByText('No pages')).toBeInTheDocument()
  })

  it('without grouping, renders a single All pages lane', () => {
    const sections: GroupSection[] = [
      {
        key: '__all__',
        label: '',
        items: [
          makeRow('Dune', 'Dune', 'read'),
          makeRow('Neuromancer', 'Neuromancer', 'reading')
        ]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: false,
        heroField: 'title',
        onOpenPage: vi.fn()
      }
    })
    // Single lane covering all rows; count is in the aria-label.
    expect(
      screen.getByRole('group', { name: /^All pages \(2\)/ })
    ).toBeInTheDocument()
    expect(screen.getAllByRole('button').length).toBeGreaterThanOrEqual(2)
  })

  it('renders chips for non-hero property values', () => {
    const sections: GroupSection[] = [
      {
        key: 's::read',
        label: 'read',
        items: [makeRow('Dune', 'Dune', 'read')]
      }
    ]
    render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage: vi.fn()
      }
    })
    // 'status' (select) renders as a chip inside the card; 'title' is the
    // hero so not chipped. The chip lives inside .card-chips (aria-hidden),
    // scoped to the card to disambiguate from the column-header label.
    const card = screen.getByRole('button', { name: /^Dune/ })
    expect(card.querySelector('.chip')?.textContent).toBe('read')
  })
})
