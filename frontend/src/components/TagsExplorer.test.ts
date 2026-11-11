import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import { tick } from 'svelte'

const mocks = vi.hoisted(() => ({
  QueryBlocksByTag: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    QueryBlocksByTag: mocks.QueryBlocksByTag
  })
)

import TagsExplorer from './TagsExplorer.svelte'

describe('TagsExplorer', () => {
  beforeEach(() => {
    mocks.QueryBlocksByTag.mockReset()
  })
  afterEach(() => cleanup())

  it('renders empty ViewState when no tag is selected', async () => {
    render(TagsExplorer, { selectedTag: '' })
    await tick()
    expect(screen.getByText('No tag selected')).toBeInTheDocument()
    expect(
      screen.getByText('Select a tag in the sidebar to see its blocks.')
    ).toBeInTheDocument()
  })

  it('renders error ViewState with role="alert" and allows retry on IPC failure', async () => {
    mocks.QueryBlocksByTag.mockRejectedValueOnce(new Error('database locked'))
    render(TagsExplorer, { selectedTag: 'project' })
    await tick()

    await waitFor(() => {
      expect(screen.getByRole('alert')).toBeInTheDocument()
    })
    expect(screen.getByText('Failed to load tagged blocks')).toBeInTheDocument()
    expect(screen.getByText('database locked')).toBeInTheDocument()

    // Test retry
    mocks.QueryBlocksByTag.mockResolvedValueOnce([
      {
        id: 'blk-1',
        notebook: 'Work',
        section: 'Projects',
        page: 'Roadmap',
        clean_content: 'Ship v2'
      }
    ])
    const retryBtn = screen.getByRole('button', { name: /Retry/i })
    await fireEvent.click(retryBtn)
    await tick()

    await waitFor(() => {
      expect(screen.getByText('Ship v2')).toBeInTheDocument()
    })
  })

  it('renders empty ViewState when tag has 0 blocks', async () => {
    mocks.QueryBlocksByTag.mockResolvedValueOnce([])
    render(TagsExplorer, { selectedTag: 'unused' })
    await tick()

    await waitFor(() => {
      expect(screen.getByText('No blocks tagged')).toBeInTheDocument()
    })
    expect(screen.getByText('No blocks have #unused.')).toBeInTheDocument()
  })

  it('renders tagged blocks and dispatches navigate-to-block on click', async () => {
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    mocks.QueryBlocksByTag.mockResolvedValueOnce([
      {
        id: 'blk-1',
        notebook: 'Work',
        section: 'Projects',
        page: 'Roadmap',
        file_date: '2026-05-01',
        clean_content: 'Ship v2 features'
      }
    ])

    render(TagsExplorer, { selectedTag: 'roadmap' })
    await tick()

    await waitFor(() => {
      expect(screen.getByText('Ship v2 features')).toBeInTheDocument()
    })
    expect(screen.getByTestId('tags-count')).toHaveTextContent(
      '#roadmap · 1 block'
    )

    const blockBtn = screen.getByRole('button', { name: /Ship v2 features/i })
    await fireEvent.click(blockBtn)
    expect(handler).toHaveBeenCalledTimes(1)
    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({
        detail: {
          notebook: 'Work',
          section: 'Projects',
          page: 'Roadmap',
          date: '2026-05-01',
          blockId: 'blk-1'
        }
      })
    )

    window.removeEventListener('navigate-to-block', handler)
  })
})

describe('TagsExplorer — block context menu (#1120)', () => {
  const menu = () => screen.getByTestId('tags-locator-menu')

  function menuLabels(): string[] {
    return Array.from(
      menu().querySelectorAll<HTMLElement>('[role="menuitem"]')
    ).map((el) => {
      const span = el.querySelector<HTMLSpanElement>('span:not([aria-hidden])')
      return (span ?? el).textContent?.trim() ?? ''
    })
  }

  function renderWithHit(overrides: Partial<Record<string, unknown>> = {}) {
    mocks.QueryBlocksByTag.mockResolvedValueOnce([
      {
        id: 'blk-1',
        notebook: 'Work',
        section: 'Projects',
        page: 'Roadmap',
        file_date: '2026-05-01',
        clean_content: 'Ship v2 features',
        ...overrides
      }
    ])
    render(TagsExplorer, { selectedTag: 'roadmap' })
    return screen.findByText('Ship v2 features')
  }

  function contextMenuOn(el: Element) {
    const e = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 30,
      clientY: 40
    })
    el.dispatchEvent(e)
    return e
  }

  beforeEach(() => {
    mocks.QueryBlocksByTag.mockReset()
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) }
    })
  })

  it('right-click a tagged block opens the menu with the three block items', async () => {
    await renderWithHit()
    const row = screen.getByRole('button', { name: /Ship v2 features/i })
    expect(row).toHaveAttribute('aria-keyshortcuts', 'Shift+F10')

    const e = contextMenuOn(row)
    await tick()

    expect(e.defaultPrevented).toBe(true)
    expect(menu()).toHaveAttribute('role', 'menu')
    expect(menuLabels()).toEqual([
      'Open block',
      'Open containing page',
      'Copy block reference'
    ])
    expect(menu().getAttribute('style')).toContain('left: 30px')
    expect(menu().getAttribute('style')).toContain('top: 40px')
  })

  it('Open block dispatches navigate-to-block with the captured hit', async () => {
    await renderWithHit()
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    contextMenuOn(screen.getByRole('button', { name: /Ship v2 features/i }))
    await tick()
    await fireEvent.click(screen.getByTestId('tags-menu-open-block'))

    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({
        detail: {
          notebook: 'Work',
          section: 'Projects',
          page: 'Roadmap',
          date: '2026-05-01',
          blockId: 'blk-1'
        }
      })
    )
    expect(screen.queryByTestId('tags-locator-menu')).toBeNull()
    window.removeEventListener('navigate-to-block', handler)
  })

  it('Open containing page dispatches navigate-to-page for the hit page', async () => {
    await renderWithHit()
    const handler = vi.fn()
    window.addEventListener('navigate-to-page', handler)

    contextMenuOn(screen.getByRole('button', { name: /Ship v2 features/i }))
    await tick()
    await fireEvent.click(screen.getByTestId('tags-menu-open-page'))

    expect(handler).toHaveBeenCalledWith(
      expect.objectContaining({
        detail: { notebook: 'Work', section: 'Projects', page: 'Roadmap' }
      })
    )
    window.removeEventListener('navigate-to-page', handler)
  })

  it('Copy block reference writes the ((uuid)) text form', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    await renderWithHit()

    contextMenuOn(screen.getByRole('button', { name: /Ship v2 features/i }))
    await tick()
    await fireEvent.click(screen.getByTestId('tags-menu-copy-block-reference'))

    await vi.waitFor(() => expect(writeText).toHaveBeenCalledWith('((blk-1))'))
  })

  it('Shift+F10 on a focused row opens the menu; Escape returns focus', async () => {
    await renderWithHit()
    const row = screen.getByRole('button', { name: /Ship v2 features/i })
    row.focus()

    await fireEvent.keyDown(row, { key: 'F10', shiftKey: true })
    await tick()
    expect(menu()).toBeInTheDocument()
    expect(menu().contains(document.activeElement)).toBe(true)

    await fireEvent.keyDown(window, { key: 'Escape' })
    await tick()
    expect(screen.queryByTestId('tags-locator-menu')).toBeNull()
    expect(document.activeElement).toBe(row)
  })

  it('backdrop click dismisses without opening the block', async () => {
    await renderWithHit()
    const handler = vi.fn()
    window.addEventListener('navigate-to-block', handler)

    contextMenuOn(screen.getByRole('button', { name: /Ship v2 features/i }))
    await tick()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Close context menu' })
    )
    expect(screen.queryByTestId('tags-locator-menu')).toBeNull()
    expect(handler).not.toHaveBeenCalled()
    window.removeEventListener('navigate-to-block', handler)
  })

  it('keeps the native menu on rows without a page locator', async () => {
    await renderWithHit({ notebook: '', page: '' })
    const row = screen.getByRole('button', { name: /Ship v2 features/i })
    expect(row).not.toHaveAttribute('aria-keyshortcuts')

    const e = contextMenuOn(row)
    await tick()
    expect(e.defaultPrevented).toBe(false)
    expect(screen.queryByTestId('tags-locator-menu')).toBeNull()
  })
})
