import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import SidebarQuickAccess from './SidebarQuickAccess.svelte'
import { RECENT_COLLAPSED_LIMIT } from '../lib/sidebar/navigationPreferences'

const baseProps = {
  favorites: [],
  recents: [],
  staleKeys: new Set<string>(),
  notebooks: [{ name: 'Work', sections: [] }],
  loading: false,
  error: '',
  collapsed: true,
  onOpen: vi.fn(),
  onToggleFavorite: vi.fn(),
  onCollapsedChange: vi.fn(),
  onRetry: vi.fn()
}

function makeRecents(count: number) {
  return Array.from({ length: count }, (_, i) => ({
    notebook: 'Work',
    section: '',
    page: `Page ${i + 1}`,
    opened_at: 100 - i
  }))
}

afterEach(cleanup)

describe('SidebarQuickAccess', () => {
  it('stays collapsed by default when there are no saved pages', async () => {
    render(SidebarQuickAccess, { props: baseProps })
    const toggle = screen.getByRole('button', { name: 'Quick access' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(
      screen.queryByText(
        'No pinned or recent pages yet. Open a page or pin one to Quick Access.'
      )
    ).not.toBeInTheDocument()
    await fireEvent.click(toggle)
    expect(
      screen.getByText(
        'No pinned or recent pages yet. Open a page or pin one to Quick Access.'
      )
    ).toBeInTheDocument()
  })

  it('expands useful content by default and remains collapsible', async () => {
    const favorites = [{ notebook: 'Work', section: '', page: 'Pinned page' }]
    const onCollapsedChange = vi.fn()
    const { rerender } = render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        collapsed: false,
        favorites,
        onCollapsedChange
      }
    })
    const toggle = screen.getByRole('button', { name: 'Quick access' })
    expect(toggle).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Pinned')).toBeInTheDocument()
    expect(screen.queryByText('Recent')).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: 'Unpin Pinned page from Quick Access'
      })
    ).toBeInTheDocument()
    await fireEvent.click(toggle)
    expect(onCollapsedChange).toHaveBeenCalledWith(true)
    await rerender({
      ...baseProps,
      collapsed: true,
      favorites,
      onCollapsedChange
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Pinned')).not.toBeInTheDocument()
  })

  it('opens automatically when loading produces useful content', async () => {
    const { rerender } = render(SidebarQuickAccess, {
      props: { ...baseProps, collapsed: false, loading: true }
    })
    expect(
      screen.getByRole('button', { name: 'Quick access' })
    ).toHaveAttribute('aria-expanded', 'false')
    await rerender({
      ...baseProps,
      collapsed: false,
      recents: [
        {
          notebook: 'Work',
          section: '',
          page: 'Inbox',
          opened_at: 10
        }
      ]
    })
    expect(
      screen.getByRole('button', { name: 'Quick access' })
    ).toHaveAttribute('aria-expanded', 'true')
    expect(screen.getByText('Recent')).toBeInTheDocument()
  })

  it('announces an error and retries without replacing the panel', async () => {
    const onRetry = vi.fn()
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        collapsed: false,
        error: 'Saved pages are unavailable.',
        onRetry
      }
    })
    expect(screen.getByRole('status')).toHaveTextContent(
      'Saved pages are unavailable.'
    )
    await fireEvent.click(screen.getByRole('button', { name: 'Try again' }))
    expect(onRetry).toHaveBeenCalledOnce()
  })

  it('keeps a long full path in the accessible name while truncating visually', () => {
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        collapsed: false,
        favorites: [
          {
            notebook: 'Work',
            section: 'Projects/An unusually long client name/Launch',
            page: 'Readiness review'
          }
        ]
      }
    })
    expect(
      screen.getByRole('button', {
        name: 'Work / Projects/An unusually long client name/Launch / Readiness review'
      })
    ).toHaveAttribute(
      'title',
      'Work / Projects/An unusually long client name/Launch / Readiness review'
    )
  })

  it('opens an available recent through the supplied callback', async () => {
    const onOpen = vi.fn()
    const recent = {
      notebook: 'Work',
      section: '',
      page: 'Inbox',
      opened_at: 10
    }
    render(SidebarQuickAccess, {
      props: { ...baseProps, collapsed: false, recents: [recent], onOpen }
    })
    await fireEvent.click(screen.getByRole('button', { name: 'Work / Inbox' }))
    expect(onOpen).toHaveBeenCalledWith(recent)
  })

  it('pins a recent page through the supplied callback', async () => {
    const onToggleFavorite = vi.fn()
    const recent = {
      notebook: 'Work',
      section: '',
      page: 'Inbox',
      opened_at: 10
    }
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        collapsed: false,
        recents: [recent],
        onToggleFavorite
      }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: 'Pin Inbox to Quick Access' })
    )
    expect(onToggleFavorite).toHaveBeenCalledWith(recent)
  })

  it(`shows at most ${RECENT_COLLAPSED_LIMIT} recents by default with Show more`, async () => {
    const recents = makeRecents(RECENT_COLLAPSED_LIMIT + 2)
    render(SidebarQuickAccess, {
      props: { ...baseProps, collapsed: false, recents }
    })
    expect(
      screen.getByRole('button', { name: 'Work / Page 1' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Work / Page 3' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Work / Page 4' })
    ).not.toBeInTheDocument()
    const more = screen.getByRole('button', { name: 'Show more' })
    expect(more).toHaveAttribute('aria-expanded', 'false')
    await fireEvent.click(more)
    expect(
      screen.getByRole('button', { name: 'Work / Page 4' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Work / Page 5' })
    ).toBeInTheDocument()
    const less = screen.getByRole('button', { name: 'Show less' })
    expect(less).toHaveAttribute('aria-expanded', 'true')
    await fireEvent.click(less)
    expect(
      screen.queryByRole('button', { name: 'Work / Page 4' })
    ).not.toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Show more' })
    ).toBeInTheDocument()
  })

  it('hides Show more when recent count is within the collapsed limit', () => {
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        collapsed: false,
        recents: makeRecents(RECENT_COLLAPSED_LIMIT)
      }
    })
    expect(
      screen.queryByRole('button', { name: 'Show more' })
    ).not.toBeInTheDocument()
  })

  it('persists disclosure toggles and does not override a deliberate collapse', async () => {
    const onCollapsedChange = vi.fn()
    const recent = {
      notebook: 'Work',
      section: '',
      page: 'Inbox',
      opened_at: 10
    }
    const { rerender } = render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        collapsed: true,
        recents: [recent],
        onCollapsedChange
      }
    })
    const toggle = screen.getByRole('button', { name: 'Quick access' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    await fireEvent.click(toggle)
    expect(onCollapsedChange).toHaveBeenCalledWith(false)

    await rerender({
      ...baseProps,
      collapsed: true,
      recents: [recent, { ...recent, page: 'Roadmap', opened_at: 11 }],
      onCollapsedChange
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
  })
})
