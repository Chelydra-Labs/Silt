import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import { tick } from 'svelte'

// copyPageReference (helper-owned menu item) resolves the [[shortest]] form
// through ResolvePageLink — one $silt-app mock covers the bindings import.
const mocks = vi.hoisted(() => ({
  resolvePageLink: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ResolvePageLink: mocks.resolvePageLink
  })
)

import TypeDashboardTable from './TypeDashboardTable.svelte'
import type {
  DashboardColumn,
  GroupSection,
  TypeDashboardRow
} from './dashboards'

// Component-level coverage for the table view. Integration paths (sort,
// group toggle, onOpenPage) are exercised through TypeDashboard.test.ts;
// this file covers the page-cell accessible name, which the integration
// tests assert only by prefix.

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

function makeRow(
  page: string,
  hero: string,
  notebook: string,
  section: string
): TypeDashboardRow {
  return {
    source: 'vault',
    notebook,
    section,
    page,
    properties: [
      { name: 'title', valueText: hero, valueType: 'text' },
      { name: 'status', valueText: 'read', valueType: 'select' },
      { name: 'rating', valueText: '5', valueType: 'number' }
    ]
  }
}

afterEach(cleanup)

describe('TypeDashboardTable', () => {
  it('page-cell aria-label includes hero, notebook, and section context', () => {
    const sections: GroupSection[] = [
      {
        key: '__all__',
        label: '',
        items: [makeRow('Dune', 'Dune', 'Work', 'Reading')]
      }
    ]
    render(TypeDashboardTable, {
      props: {
        columns: BOOK_COLUMNS,
        sections,
        grouped: false,
        sort: { property: '', desc: false },
        heroField: 'title',
        collapsed: new Set<string>(),
        onSort: vi.fn(),
        onToggleGroup: vi.fn(),
        onOpenPage: vi.fn()
      }
    })
    // Mirrors the board card's accessible-name format so screen-reader users
    // get the same disambiguation sighted users see in the rendered cell.
    const button = screen.getByRole('button', {
      name: 'Open page Dune, Dune, Work › Reading'
    })
    expect(button).toBeInTheDocument()
  })

  it('page-cell aria-label omits empty context segments', () => {
    // No hero (heroField unset) and no section — the label should not append
    // stray separators.
    const sections: GroupSection[] = [
      {
        key: '__all__',
        label: '',
        items: [makeRow('Plan', '', 'Work', '')]
      }
    ]
    render(TypeDashboardTable, {
      props: {
        columns: BOOK_COLUMNS,
        sections,
        grouped: false,
        sort: { property: '', desc: false },
        heroField: '',
        collapsed: new Set<string>(),
        onSort: vi.fn(),
        onToggleGroup: vi.fn(),
        onOpenPage: vi.fn()
      }
    })
    const button = screen.getByRole('button', {
      name: 'Open page Plan, Work'
    })
    expect(button).toBeInTheDocument()
  })
})

describe('TypeDashboardTable — page-locator context menu (#1120)', () => {
  const sections: GroupSection[] = [
    {
      key: '__all__',
      label: '',
      items: [makeRow('Dune', 'Dune', 'Work', 'Reading')]
    }
  ]

  const menu = () => screen.getByTestId('dash-table-locator-menu')

  function menuLabels(): string[] {
    return Array.from(
      menu().querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  function rowButton() {
    return screen.getByRole('button', {
      name: 'Open page Dune, Dune, Work › Reading'
    })
  }

  async function openMenuOnRow() {
    const row = rowButton()
    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 20,
      clientY: 30
    })
    row.dispatchEvent(e)
    await tick()
    return e
  }

  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) }
    })
  })

  function renderTable(extra: Record<string, unknown> = {}) {
    return render(TypeDashboardTable, {
      props: {
        columns: BOOK_COLUMNS,
        sections,
        grouped: false,
        sort: { property: '', desc: false },
        heroField: 'title',
        collapsed: new Set<string>(),
        onSort: vi.fn(),
        onToggleGroup: vi.fn(),
        onOpenPage: vi.fn(),
        ...extra
      }
    })
  }

  beforeEach(() => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) }
    })
    mocks.resolvePageLink.mockReset()
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      shortest: 'Dune'
    })
  })

  it('right-click a row opens the standard vocabulary with the favorite item', async () => {
    renderTable({
      onToggleFavorite: vi.fn(),
      isFavorite: () => false
    })
    const row = rowButton()
    expect(row).toHaveAttribute('aria-keyshortcuts', 'Shift+F10')

    const e = await openMenuOnRow()
    expect(e.defaultPrevented).toBe(true)
    expect(menuLabels()).toEqual([
      'Open',
      'Copy page reference',
      'Pin to Quick Access'
    ])
    expect(menu().getAttribute('style')).toContain('left: 20px')
    expect(menu().getAttribute('style')).toContain('top: 30px')
  })

  it('shows Unpin when the row locator is already a favorite', async () => {
    renderTable({
      onToggleFavorite: vi.fn(),
      isFavorite: (loc: { page: string }) => loc.page === 'Dune'
    })
    await openMenuOnRow()
    expect(menuLabels()).toEqual(['Open', 'Copy page reference', 'Unpin'])
  })

  it('omits the favorite item when the host does not wire favorites', async () => {
    renderTable()
    await openMenuOnRow()
    expect(menuLabels()).toEqual(['Open', 'Copy page reference'])
  })

  it('menu items invoke onOpenPage and onToggleFavorite with the row locator', async () => {
    const onOpenPage = vi.fn()
    const onToggleFavorite = vi.fn()
    renderTable({ onOpenPage, onToggleFavorite, isFavorite: () => false })

    await openMenuOnRow()
    await fireEvent.click(screen.getByTestId('dash-table-menu-open'))
    expect(onOpenPage).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
    expect(screen.queryByTestId('dash-table-locator-menu')).toBeNull()

    await openMenuOnRow()
    await fireEvent.click(screen.getByTestId('dash-table-menu-toggle-favorite'))
    // The helper hands back the surface's full locator shape.
    expect(onToggleFavorite).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
  })

  it('copy page reference resolves the shortest form and writes it', async () => {
    renderTable({ onToggleFavorite: vi.fn(), isFavorite: () => false })
    await openMenuOnRow()
    await fireEvent.click(
      screen.getByTestId('dash-table-menu-copy-page-reference')
    )
    await waitFor(() =>
      expect(mocks.resolvePageLink).toHaveBeenCalledWith('Work/Reading/Dune')
    )
    await waitFor(() =>
      expect(navigator.clipboard.writeText).toHaveBeenCalledWith('[[Dune]]')
    )
  })

  it('Shift+F10 on a focused row opens the menu; Escape returns focus', async () => {
    renderTable({ onToggleFavorite: vi.fn(), isFavorite: () => false })
    const row = rowButton()
    row.focus()

    await fireEvent.keyDown(row, { key: 'F10', shiftKey: true })
    await tick()
    expect(menu()).toBeInTheDocument()
    expect(menu().contains(document.activeElement)).toBe(true)

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByTestId('dash-table-locator-menu')).toBeNull()
    expect(document.activeElement).toBe(row)
  })

  it('row click still opens the page without the menu', async () => {
    const onOpenPage = vi.fn()
    renderTable({ onOpenPage })
    await fireEvent.click(rowButton())
    expect(onOpenPage).toHaveBeenCalledWith({
      source: 'vault',
      notebook: 'Work',
      section: 'Reading',
      page: 'Dune'
    })
    expect(screen.queryByTestId('dash-table-locator-menu')).toBeNull()
  })
})
