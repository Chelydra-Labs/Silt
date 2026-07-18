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
  collapsed: true,
  onOpen: vi.fn(),
  onToggleFavorite: vi.fn(),
  onCollapsedChange: vi.fn(),
  onRetry: vi.fn()
}

afterEach(cleanup)

describe('SidebarQuickAccess', () => {
  it('stays collapsed by default when there are no saved pages', async () => {
    render(SidebarQuickAccess, { props: baseProps })
    const toggle = screen.getByRole('button', { name: 'Quick access' })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('No saved pages yet.')).not.toBeInTheDocument()
    await fireEvent.click(toggle)
    expect(screen.getByText('No saved pages yet.')).toBeInTheDocument()
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
    expect(screen.getByText('Favorites')).toBeInTheDocument()
    expect(screen.queryByText('Recent')).not.toBeInTheDocument()
    await fireEvent.click(toggle)
    expect(onCollapsedChange).toHaveBeenCalledWith(true)
    await rerender({
      ...baseProps,
      collapsed: true,
      favorites,
      onCollapsedChange
    })
    expect(toggle).toHaveAttribute('aria-expanded', 'false')
    expect(screen.queryByText('Favorites')).not.toBeInTheDocument()
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
