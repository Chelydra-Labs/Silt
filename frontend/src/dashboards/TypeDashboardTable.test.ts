import { describe, expect, it, afterEach, vi } from 'vitest'
import { render, screen, cleanup } from '@testing-library/svelte'

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
        rows: [makeRow('Dune', 'Dune', 'Work', 'Reading')]
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
        rows: [makeRow('Plan', '', 'Work', '')]
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
