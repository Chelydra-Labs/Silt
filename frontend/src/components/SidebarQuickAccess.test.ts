import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { tick } from 'svelte'
import SidebarQuickAccess from './SidebarQuickAccess.svelte'

// copyPageReference resolves the [[shortest]] form through ResolvePageLink
// (pageActions → bindings module, aliased as $silt-app — one mock covers it).
const mocks = vi.hoisted(() => ({
  resolvePageLink: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ResolvePageLink: mocks.resolvePageLink
  })
)

const baseProps = {
  favorites: [],
  recents: [],
  staleKeys: new Set<string>(),
  notebooks: [{ name: 'Work', sections: [] }],
  loading: false,
  error: '',
  onOpen: vi.fn(),
  onToggleFavorite: vi.fn(),
  onOpenInNewTab: vi.fn().mockReturnValue(true),
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
      screen.getByRole('tabpanel', { name: /Quick [Aa]ccess/ })
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
      screen.getByRole('tabpanel', { name: /Quick [Aa]ccess/ })
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

describe('SidebarQuickAccess — page-locator context menu (#1120)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    mocks.resolvePageLink.mockReset()
    mocks.resolvePageLink.mockResolvedValue({
      exists: true,
      shortest: 'Daily'
    })
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) }
    })
  })

  const menu = () => screen.getByTestId('quick-access-locator-menu')

  function menuLabels(): string[] {
    return Array.from(
      menu().querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  it('right-click a pinned row opens the locator menu with the pinned subset', async () => {
    const ref = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    render(SidebarQuickAccess, {
      props: { ...baseProps, favorites: [ref] }
    })
    expect(screen.queryByTestId('quick-access-locator-menu')).toBeNull()

    const row = screen.getByRole('button', { name: 'Work / Journal / Daily' })
    expect(row).toHaveAttribute('aria-keyshortcuts', 'Shift+F10')
    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 40,
      clientY: 50
    })
    row.dispatchEvent(e)
    await tick()

    expect(e.defaultPrevented).toBe(true)
    expect(menu()).toHaveAttribute('role', 'menu')
    expect(menuLabels()).toEqual([
      'Open in new tab',
      'Copy page reference',
      'Unpin'
    ])
    expect(menu().getAttribute('style')).toContain('left: 40px')
    expect(menu().getAttribute('style')).toContain('top: 50px')
  })

  it('right-click a recent row shows Pin to Quick Access when unpinned', async () => {
    const recent = {
      notebook: 'Work',
      section: '',
      page: 'Inbox',
      opened_at: 10
    }
    render(SidebarQuickAccess, {
      props: { ...baseProps, recents: [recent] }
    })
    const row = screen.getByRole('button', { name: 'Work / Inbox' })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    expect(menuLabels()).toEqual([
      'Open in new tab',
      'Copy page reference',
      'Pin to Quick Access'
    ])
  })

  it('hides the open-in-new-tab item when the host does not provide it', async () => {
    const recent = {
      notebook: 'Work',
      section: '',
      page: 'Inbox',
      opened_at: 10
    }
    const { onOpenInNewTab, ...withoutPin } = baseProps
    expect(onOpenInNewTab).toBeTruthy()
    render(SidebarQuickAccess, {
      props: { ...withoutPin, recents: [recent] }
    })
    const row = screen.getByRole('button', { name: 'Work / Inbox' })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    expect(menuLabels()).toEqual(['Copy page reference', 'Pin to Quick Access'])
  })

  it('menu items invoke the host handlers with the row locator', async () => {
    const onToggleFavorite = vi.fn()
    const onOpenInNewTab = vi.fn().mockReturnValue(true)
    const ref = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        favorites: [ref],
        onToggleFavorite,
        onOpenInNewTab
      }
    })
    const row = screen.getByRole('button', { name: 'Work / Journal / Daily' })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()

    await fireEvent.click(
      screen.getByTestId('quick-access-menu-open-in-new-tab')
    )
    expect(onOpenInNewTab).toHaveBeenCalledWith(ref)
    expect(screen.queryByTestId('quick-access-locator-menu')).toBeNull()

    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()
    await fireEvent.click(
      screen.getByTestId('quick-access-menu-toggle-favorite')
    )
    expect(onToggleFavorite).toHaveBeenCalledWith(ref)
  })

  it('copy page reference resolves the shortest form and writes it', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    const ref = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    render(SidebarQuickAccess, {
      props: { ...baseProps, favorites: [ref] }
    })
    const row = screen.getByRole('button', { name: 'Work / Journal / Daily' })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()

    await fireEvent.click(
      screen.getByTestId('quick-access-menu-copy-page-reference')
    )
    await vi.waitFor(() =>
      expect(mocks.resolvePageLink).toHaveBeenCalledWith('Work/Journal/Daily')
    )
    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith('[[Daily]]'))
  })

  it('Shift+F10 on a focused row opens the menu; Escape returns focus', async () => {
    const ref = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    render(SidebarQuickAccess, {
      props: { ...baseProps, favorites: [ref] }
    })
    const row = screen.getByRole('button', { name: 'Work / Journal / Daily' })
    row.focus()
    expect(document.activeElement).toBe(row)

    await fireEvent.keyDown(row, { key: 'F10', shiftKey: true })
    await tick()
    expect(menu()).toBeInTheDocument()
    // The primitive focuses the first item on open.
    expect(menu().contains(document.activeElement)).toBe(true)

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByTestId('quick-access-locator-menu')).toBeNull()
    expect(document.activeElement).toBe(row)
  })

  it('backdrop click dismisses without touching the row', async () => {
    const onOpen = vi.fn()
    const ref = { notebook: 'Work', section: 'Journal', page: 'Daily' }
    render(SidebarQuickAccess, {
      props: { ...baseProps, favorites: [ref], onOpen }
    })
    const row = screen.getByRole('button', { name: 'Work / Journal / Daily' })
    await fireEvent.contextMenu(row, { clientX: 5, clientY: 5 })
    await tick()

    await fireEvent.click(
      screen.getByRole('button', { name: 'Close context menu' })
    )
    expect(screen.queryByTestId('quick-access-locator-menu')).toBeNull()
    expect(onOpen).not.toHaveBeenCalled()
  })

  it('unavailable rows do not open the menu', async () => {
    const ref = { notebook: 'Gone', section: '', page: 'Deleted' }
    render(SidebarQuickAccess, {
      props: {
        ...baseProps,
        favorites: [ref],
        staleKeys: new Set(['Gone\u0000\u0000Deleted'])
      }
    })
    const row = screen.getByRole('button', {
      name: 'Gone / Deleted — Unavailable'
    })
    expect(row).toBeDisabled()
    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 5,
      clientY: 5
    })
    row.dispatchEvent(e)
    await tick()
    // The guard leaves the event alone — the native menu (or nothing, on a
    // disabled control in a real browser) — and no locator menu appears.
    expect(screen.queryByTestId('quick-access-locator-menu')).toBeNull()
  })
})
