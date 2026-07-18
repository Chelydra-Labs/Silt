import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import SidebarSection from './SidebarSection.svelte'
import type { NavSection } from '../lib/sidebar/types'

type NavSectionShape = {
  name: string
  path?: string
  pages: { name: string; count: number }[]
  children?: NavSectionShape[]
}

type DropTargetShape = { level: string; name: string; before: boolean }
type DragItemShape = { level: string; name: string; section?: string }

function completeSection(section: NavSectionShape, parent = ''): NavSection {
  const path =
    section.path ?? (parent ? `${parent}/${section.name}` : section.name)
  return {
    ...section,
    path,
    children: section.children?.map((child) => completeSection(child, path))
  }
}

function makeProps(
  overrides: {
    section?: NavSectionShape
    depth?: number
    activeSection?: string
    expandedSections?: Set<string>
    dropTarget?: DropTargetShape | null
    dragItem?: DragItemShape | null
  } = {}
) {
  return {
    section: completeSection(
      overrides.section ?? {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      }
    ),
    depth: overrides.depth ?? 0,
    activeNotebook: 'Work',
    activeSection: overrides.activeSection ?? '',
    activePage: '',
    expandedSections: overrides.expandedSections ?? new Set<string>(),
    navOrder: { pages: {} as Record<string, string[]> },
    dropTarget: (overrides.dropTarget ?? null) as DropTargetShape | null,
    dragItem: (overrides.dragItem ?? null) as DragItemShape | null,
    focusedTreeItemId: 'section:Work:Journal',
    onTreeItemFocus: vi.fn(),
    onToggleSection: vi.fn(),
    onSelectPage: vi.fn(),
    onPinPage: vi.fn(),
    onSelectSection: vi.fn(),
    onCreatePageInline: vi.fn(),
    onDragStart: vi.fn(),
    onDragOver: vi.fn(),
    onDragLeave: vi.fn(),
    onDrop: vi.fn(),
    onDragEnd: vi.fn(),
    onContextMenu: vi.fn()
  }
}

describe('SidebarSection (#88 deep-nesting)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  afterEach(() => {
    cleanup()
  })

  it('renders a section header and its pages when expanded', () => {
    const props = makeProps({
      section: {
        name: 'Journal',
        pages: [
          { name: 'Daily', count: 5 },
          { name: 'Weekly', count: 2 }
        ]
      },
      expandedSections: new Set(['Journal'])
    })
    render(SidebarSection, { props })
    expect(screen.getByText('Journal')).toBeInTheDocument()
    expect(screen.getByText('Daily')).toBeInTheDocument()
    expect(screen.getByText('Weekly')).toBeInTheDocument()
  })

  it('does not render pages when not expanded', () => {
    const props = makeProps({
      section: { name: 'Journal', pages: [{ name: 'Daily', count: 5 }] },
      expandedSections: new Set<string>()
    })
    render(SidebarSection, { props })
    expect(screen.getByText('Journal')).toBeInTheDocument()
    expect(screen.queryByText('Daily')).not.toBeInTheDocument()
  })

  it('renders nested children recursively (#88)', () => {
    const deepSection: NavSectionShape = {
      name: 'Projects',
      path: 'Projects',
      pages: [],
      children: [
        {
          name: 'Active',
          path: 'Projects/Active',
          pages: [{ name: 'SiteLaunch', count: 3 }],
          children: [
            {
              name: 'Sub',
              path: 'Projects/Active/Sub',
              pages: [{ name: 'DeepPage', count: 1 }],
              children: []
            }
          ]
        }
      ]
    }
    const props = makeProps({
      section: deepSection,
      expandedSections: new Set([
        'Projects',
        'Projects/Active',
        'Projects/Active/Sub'
      ])
    })
    render(SidebarSection, { props })
    expect(screen.getByText('Projects')).toBeInTheDocument()
    expect(screen.getByText('Active')).toBeInTheDocument()
    expect(screen.getByText('Sub')).toBeInTheDocument()
    expect(screen.getByText('DeepPage')).toBeInTheDocument()
  })

  it('applies refreshed configured order to nested siblings', async () => {
    const props = makeProps({
      section: {
        name: 'Projects',
        path: 'Projects',
        pages: [],
        children: [
          { name: 'Alpha', path: 'Projects/Alpha', pages: [] },
          { name: 'Zeta', path: 'Projects/Zeta', pages: [] }
        ]
      },
      expandedSections: new Set(['Projects'])
    })
    const { rerender } = render(SidebarSection, { props })

    const nestedLabels = () =>
      Array.from(
        document.querySelectorAll<HTMLElement>('span[title^="Projects/"]')
      ).map((label) => label.textContent)

    expect(nestedLabels()).toEqual(['Alpha', 'Zeta'])

    await rerender({
      ...props,
      navOrder: {
        pages: {},
        sections: { 'Work/Projects': ['Zeta', 'Alpha'] }
      }
    })

    expect(nestedLabels()).toEqual(['Zeta', 'Alpha'])
  })

  it('toggles expansion on click', async () => {
    const onToggle = vi.fn()
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      }
    })
    props.onToggleSection = onToggle
    render(SidebarSection, { props })
    const header = screen.getByRole('treeitem', { name: /Journal/ })
    await fireEvent.click(header)
    expect(onToggle).toHaveBeenCalledWith('Journal')
  })

  it('synchronizes the roving focus controller on focus', async () => {
    const onFocus = vi.fn()
    const props = makeProps({
      section: { name: 'Journal', path: 'Journal', pages: [] }
    })
    props.onTreeItemFocus = onFocus
    render(SidebarSection, { props })
    const header = screen.getByRole('treeitem', { name: /Journal/ })
    header.focus()
    expect(onFocus).toHaveBeenCalledWith('section:Work:Journal')
  })

  it('reports aria-level for nested sections', () => {
    const props = makeProps({
      section: { name: 'Top', pages: [] },
      depth: 0
    })
    render(SidebarSection, { props })
    const top = screen.getByRole('treeitem', { name: /Top/ })
    expect(top).toHaveAttribute('aria-level', '1')

    cleanup()

    const props2 = makeProps({
      section: { name: 'Deep', pages: [] },
      depth: 2
    })
    render(SidebarSection, { props: props2 })
    const deep = screen.getByRole('treeitem', { name: /Deep/ })
    expect(deep).toHaveAttribute('aria-level', '3')
  })

  it('emits selectPage when a page is clicked', async () => {
    const onSelectPage = vi.fn()
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      },
      expandedSections: new Set(['Journal'])
    })
    props.onSelectPage = onSelectPage
    render(SidebarSection, { props })
    await fireEvent.click(screen.getByText('Daily'))
    expect(onSelectPage).toHaveBeenCalledWith('Journal', 'Daily')
  })

  it('keeps empty-section guidance out of the tree tab sequence', () => {
    const props = makeProps({
      section: { name: 'Empty', path: 'Empty', pages: [], children: [] },
      expandedSections: new Set(['Empty'])
    })
    props.focusedTreeItemId = 'section:Work:Empty'
    render(SidebarSection, { props })
    const header = screen.getByRole('treeitem', { name: /Empty/ })
    expect(
      screen.getByText('Use the section menu to add one')
    ).toBeInTheDocument()
    expect(header.querySelector('button')).toBeNull()
    expect(document.querySelectorAll('[tabindex="0"]')).toHaveLength(1)
  })

  it('emits pinPage on double-click (#142)', async () => {
    const onPinPage = vi.fn()
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      },
      expandedSections: new Set(['Journal'])
    })
    props.onPinPage = onPinPage
    render(SidebarSection, { props })
    await fireEvent.dblClick(screen.getByText('Daily'))
    expect(onPinPage).toHaveBeenCalledWith('Journal', 'Daily')
  })

  it('emits pinPage on middle-click (#142)', async () => {
    const onPinPage = vi.fn()
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      },
      expandedSections: new Set(['Journal'])
    })
    props.onPinPage = onPinPage
    render(SidebarSection, { props })
    const page = screen.getByText('Daily')
    page.dispatchEvent(new MouseEvent('auxclick', { bubbles: true, button: 1 }))
    expect(onPinPage).toHaveBeenCalledWith('Journal', 'Daily')
  })

  it('applies drag-over-top/bottom on page when dropTarget targets it (#176)', () => {
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [
          { name: 'Daily', count: 5 },
          { name: 'Weekly', count: 2 }
        ]
      },
      expandedSections: new Set(['Journal']),
      dropTarget: { level: 'page', name: 'Journal\u0000Weekly', before: true }
    })
    render(SidebarSection, { props })
    const weekly = screen.getByText('Weekly').closest('button')!
    expect(weekly.classList.contains('drag-over-top')).toBe(true)
    expect(weekly.classList.contains('drag-over-bottom')).toBe(false)
  })

  it('applies drag-over-bottom on page when dropTarget is after (#176)', () => {
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      },
      expandedSections: new Set(['Journal']),
      dropTarget: { level: 'page', name: 'Journal\u0000Daily', before: false }
    })
    render(SidebarSection, { props })
    const daily = screen.getByText('Daily').closest('button')!
    expect(daily.classList.contains('drag-over-bottom')).toBe(true)
  })

  it('applies drag-over-into on section header when a page is dragged over it (#177)', () => {
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: [{ name: 'Daily', count: 5 }]
      },
      dragItem: { level: 'page', name: 'SomePage', section: 'Other' },
      dropTarget: { level: 'section', name: 'Journal', before: false }
    })
    render(SidebarSection, { props })
    const header = screen.getByRole('treeitem', { name: /Journal/ })
    expect(header.classList.contains('drag-over-into')).toBe(true)
    // Section reorder indicators should NOT show (dragItem is a page, not a section).
    expect(header.classList.contains('drag-over-top')).toBe(false)
    expect(header.classList.contains('drag-over-bottom')).toBe(false)
  })

  it('shows section reorder indicator (not into) when a section is dragged (#176)', () => {
    const props = makeProps({
      section: {
        name: 'Journal',
        path: 'Journal',
        pages: []
      },
      dragItem: { level: 'section', name: 'Other' },
      dropTarget: { level: 'section', name: 'Journal', before: true }
    })
    render(SidebarSection, { props })
    const header = screen.getByRole('treeitem', { name: /Journal/ })
    expect(header.classList.contains('drag-over-top')).toBe(true)
    expect(header.classList.contains('drag-over-into')).toBe(false)
  })

  it('passes the canonical nested path as the section drop target', async () => {
    const props = makeProps({
      section: {
        name: 'Current',
        path: 'Projects/Current',
        pages: []
      },
      depth: 1
    })
    render(SidebarSection, { props })
    await fireEvent.drop(screen.getByRole('treeitem', { name: /Current/ }))
    expect(props.onDrop).toHaveBeenCalledWith(
      expect.anything(),
      'section',
      'Projects/Current',
      'Work',
      'Projects/Current'
    )
  })
})
