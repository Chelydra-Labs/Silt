import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import SidebarQuickAccess from './SidebarQuickAccess.svelte'

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
  it('renders empty states when there are no saved pages and not loading/error', () => {
    render(SidebarQuickAccess, { props: baseProps })
    expect(
      screen.getByRole('tabpanel', { name: 'Quick Access' })
    ).toBeInTheDocument()
    expect(
      screen.getByText('No pinned pages yet. Pin pages to access them quickly.')
    ).toBeInTheDocument()
    expect(screen.getByText('No recent pages yet.')).toBeInTheDocument()
  })

  it('renders Pinned and Recent sections when saved pages exist', () => {
    const favorites = [{ notebook: 'Work', section: '', page: 'Pinned page' }]
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        favorites
      }
    })
    expect(
      screen.getByRole('tabpanel', { name: 'Quick Access' })
    ).toBeInTheDocument()
    expect(screen.getByText(/Pinned \(1\)/)).toBeInTheDocument()
    expect(screen.getByText(/Recent \(0\)/)).toBeInTheDocument()
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

  it('renders all recents in full-height view without truncation', () => {
    const recents = makeRecents(5)
    render(SidebarQuickAccess, {
      props: { ...baseProps, recents }
    })
    expect(
      screen.getByRole('button', { name: 'Work / Page 1' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Work / Page 4' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Work / Page 5' })
    ).toBeInTheDocument()
  })

  it('renders both Pinned and Recent pages simultaneously', () => {
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

    expect(
      screen.getByRole('button', { name: 'Work / Notes / Starred' })
    ).toBeInTheDocument()
    expect(
      screen.getByRole('button', { name: 'Work / Inbox / Recent Note' })
    ).toBeInTheDocument()
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
