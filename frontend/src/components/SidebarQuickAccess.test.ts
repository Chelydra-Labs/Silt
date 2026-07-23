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
  onOpen: vi.fn(),
  onToggleFavorite: vi.fn(),
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
  it('renders nothing when there are no saved pages and not loading/error', () => {
    const { container } = render(SidebarQuickAccess, { props: baseProps })
    expect(container.firstElementChild).toBeNull()
  })

  it('renders card well container when saved pages exist', () => {
    const favorites = [{ notebook: 'Work', section: '', page: 'Pinned page' }]
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        favorites
      }
    })
    expect(
      screen.getByRole('region', { name: 'Quick access' })
    ).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Pinned/ })).toBeInTheDocument()
    expect(screen.getByRole('tab', { name: /Recent/ })).toBeInTheDocument()
    expect(
      screen.getByRole('button', {
        name: 'Unpin Pinned page from Quick Access'
      })
    ).toBeInTheDocument()
  })

  it('shows loading indicator inside container when loading', () => {
    render(SidebarQuickAccess, {
      props: { ...baseProps, loading: true }
    })
    expect(screen.getByText('Loading saved pages…')).toBeInTheDocument()
  })

  it('announces an error and retries without replacing the panel', async () => {
    const onRetry = vi.fn()
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
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
      props: { ...baseProps, recents: [recent], onOpen }
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
      props: { ...baseProps, recents }
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
        recents: makeRecents(RECENT_COLLAPSED_LIMIT)
      }
    })
    expect(
      screen.queryByRole('button', { name: 'Show more' })
    ).not.toBeInTheDocument()
  })

  it('switches between Pinned and Recent segmented tabs', async () => {
    const favorites = [{ notebook: 'Work', section: 'Notes', page: 'Starred' }]
    const recents = [
      { notebook: 'Work', section: 'Inbox', page: 'Recent Note', opened_at: 1 }
    ]

    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        favorites,
        recents
      }
    })

    const pinnedTab = screen.getByRole('tab', { name: /Pinned/ })
    const recentTab = screen.getByRole('tab', { name: /Recent/ })

    expect(pinnedTab).toHaveAttribute('aria-selected', 'true')
    expect(recentTab).toHaveAttribute('aria-selected', 'false')
    expect(
      screen.getByRole('button', { name: 'Work / Notes / Starred' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Work / Inbox / Recent Note' })
    ).not.toBeInTheDocument()

    await fireEvent.click(recentTab)

    expect(pinnedTab).toHaveAttribute('aria-selected', 'false')
    expect(recentTab).toHaveAttribute('aria-selected', 'true')
    expect(
      screen.getByRole('button', { name: 'Work / Inbox / Recent Note' })
    ).toBeInTheDocument()
    expect(
      screen.queryByRole('button', { name: 'Work / Notes / Starred' })
    ).not.toBeInTheDocument()
  })

  it('highlights active item when location matches props', () => {
    const favorites = [
      { notebook: 'Work', section: 'Projects', page: 'Sprint' }
    ]
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        favorites,
        activeNotebook: 'Work',
        activeSection: 'Projects',
        activePage: 'Sprint'
      }
    })

    const pageBtn = screen.getByRole('button', {
      name: 'Work / Projects / Sprint'
    })
    expect(pageBtn).toHaveClass('active')
  })
})
