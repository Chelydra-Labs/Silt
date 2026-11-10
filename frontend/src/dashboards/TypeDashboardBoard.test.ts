import { describe, expect, it, afterEach, beforeEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  within
} from '@testing-library/svelte'
import { tick } from 'svelte'

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

describe('TypeDashboardBoard — page-locator context menu (#1120)', () => {
  const sections: GroupSection[] = [
    {
      key: 's::read',
      label: 'read',
      items: [makeRow('Dune', 'Dune', 'read')]
    }
  ]

  const menu = () => screen.getByTestId('dash-board-locator-menu')

  function menuLabels(): string[] {
    return Array.from(
      menu().querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  function card() {
    return screen.getByRole('button', { name: /^Dune, Dune/ })
  }

  function renderBoard(extra: Record<string, unknown> = {}) {
    return render(TypeDashboardBoard, {
      props: {
        sections,
        columns: BOOK_COLUMNS,
        grouped: true,
        heroField: 'title',
        onOpenPage: vi.fn(),
        ...extra
      }
    })
  }

  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) }
    })
  })

  it('right-click a card opens the same vocabulary as the table', async () => {
    renderBoard({ onToggleFavorite: vi.fn(), isFavorite: () => false })
    const el = card()
    expect(el).toHaveAttribute('aria-keyshortcuts', 'Shift+F10')

    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 15,
      clientY: 25
    })
    el.dispatchEvent(e)
    await tick()

    expect(e.defaultPrevented).toBe(true)
    expect(menuLabels()).toEqual([
      'Open',
      'Copy page reference',
      'Pin to Quick Access'
    ])
    expect(menu().getAttribute('style')).toContain('left: 15px')
    expect(menu().getAttribute('style')).toContain('top: 25px')
  })

  it('menu items invoke onOpenPage and onToggleFavorite with the card locator', async () => {
    const onOpenPage = vi.fn()
    const onToggleFavorite = vi.fn()
    renderBoard({ onOpenPage, onToggleFavorite, isFavorite: () => true })

    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 5,
      clientY: 5
    })
    card().dispatchEvent(e)
    await tick()

    await fireEvent.click(screen.getByTestId('dash-board-menu-open'))
    expect(onOpenPage).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
    expect(screen.queryByTestId('dash-board-locator-menu')).toBeNull()

    card().dispatchEvent(
      new MouseEvent('contextmenu', {
        bubbles: true,
        cancelable: true,
        clientX: 5,
        clientY: 5
      })
    )
    await tick()
    await fireEvent.click(screen.getByTestId('dash-board-menu-toggle-favorite'))
    expect(onToggleFavorite).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
  })

  it('Shift+F10 on a focused card opens the menu; Escape returns focus; Enter still opens', async () => {
    const onOpenPage = vi.fn()
    renderBoard({ onOpenPage, onToggleFavorite: vi.fn(), isFavorite: () => false })
    const el = card()
    el.focus()

    await fireEvent.keyDown(el, { key: 'F10', shiftKey: true })
    await tick()
    expect(menu()).toBeInTheDocument()
    expect(menu().contains(document.activeElement)).toBe(true)

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByTestId('dash-board-locator-menu')).toBeNull()
    expect(document.activeElement).toBe(el)

    // No keydown regression: Enter still activates the card.
    await fireEvent.keyDown(el, { key: 'Enter' })
    expect(onOpenPage).toHaveBeenCalledOnce()
  })

  it('card click still opens the page without the menu', async () => {
    const onOpenPage = vi.fn()
    renderBoard({ onOpenPage })
    await fireEvent.click(card())
    expect(onOpenPage).toHaveBeenCalledOnce()
    expect(screen.queryByTestId('dash-board-locator-menu')).toBeNull()
  })
})
