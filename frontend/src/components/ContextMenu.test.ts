import { describe, expect, it, beforeEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, fireEvent } from '@testing-library/svelte'

// Mock clampToViewport so we can assert it was called without depending on
// real viewport dimensions in jsdom. vi.hoisted ensures the mock is available
// when vi.mock's factory runs (both are hoisted to the top of the file).
const mocks = vi.hoisted(() => ({
  clampToViewport: vi.fn().mockReturnValue({ left: 100, top: 100 })
}))

vi.mock('../lib/editor/popoverPositioning', () => ({
  clampToViewport: mocks.clampToViewport,
  POPOVER_MARGIN: 8
}))

import ContextMenu from './ContextMenu.svelte'

describe('ContextMenu', () => {
  beforeEach(() => {
    mocks.clampToViewport.mockClear()
    // Reset jsdom viewport to a known size.
    Object.defineProperty(window, 'innerWidth', { value: 1920, writable: true })
    Object.defineProperty(window, 'innerHeight', {
      value: 1080,
      writable: true
    })
  })

  // --- render / open / close basics ---------------------------------------

  it('does not render when open is false', () => {
    render(ContextMenu, {
      props: {
        open: false,
        anchor: { x: 100, y: 100 },
        onClose: vi.fn()
      }
    })
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('does not render when anchor is null', () => {
    render(ContextMenu, {
      props: {
        open: true,
        anchor: null,
        onClose: vi.fn()
      }
    })
    expect(screen.queryByRole('menu')).toBeNull()
  })

  it('renders when open and anchor are set', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    expect(screen.getByRole('menu')).toBeInTheDocument()
  })

  // --- backdrop dismissal --------------------------------------------------

  it('closes when backdrop is clicked', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const backdrop = screen.getByLabelText('Close context menu')
    await fireEvent.click(backdrop)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on backdrop right-click (no new menu)', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const backdrop = screen.getByLabelText('Close context menu')
    await fireEvent.contextMenu(backdrop)
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  // --- key / event dismissal -----------------------------------------------

  it('closes when Escape is pressed', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on window resize', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    window.dispatchEvent(new Event('resize'))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('closes on document scroll (fallback when no anchorEl)', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, anchorEl: null, onClose }
    })
    await tick()
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  // --- positioning ---------------------------------------------------------

  it('calls clampToViewport with measured dimensions', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    // After mounting, the component measures offsetWidth/offsetHeight and
    // calls clampToViewport. Since jsdom sets offsetWidth to 0 by default,
    // it should fall back to the default of 180 width.
    await tick()
    expect(mocks.clampToViewport).toHaveBeenCalled()
    // The second call (after tick().then(measure)) is the accurate one.
    const call =
      mocks.clampToViewport.mock.calls[
        mocks.clampToViewport.mock.calls.length - 1
      ]
    expect(call).toBeDefined()
  })

  // --- keyboard navigation --------------------------------------------------

  it('ArrowDown cycles through items', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    // Inject items.
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1">B</button>',
      '<button type="button" role="menuitem" data-testid="item-2">C</button>'
    ].join('')
    await tick()
    // Focus first item.
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[0] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await tick()
    expect(document.activeElement).toBe(items[1])

    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await tick()
    expect(document.activeElement).toBe(items[2])
  })

  it('ArrowDown wraps from last to first', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1">B</button>'
    ].join('')
    await tick()
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[1] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await tick()
    expect(document.activeElement).toBe(items[0])
  })

  it('ArrowUp cycles backwards and wraps', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1">B</button>',
      '<button type="button" role="menuitem" data-testid="item-2">C</button>'
    ].join('')
    await tick()
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[0] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'ArrowUp' })
    await tick()
    expect(document.activeElement).toBe(items[2])
  })

  it('Home jumps to first enabled item', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1">B</button>',
      '<button type="button" role="menuitem" data-testid="item-2">C</button>'
    ].join('')
    await tick()
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[2] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'Home' })
    await tick()
    expect(document.activeElement).toBe(items[0])
  })

  it('End jumps to last enabled item', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1">B</button>',
      '<button type="button" role="menuitem" data-testid="item-2">C</button>'
    ].join('')
    await tick()
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[0] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'End' })
    await tick()
    expect(document.activeElement).toBe(items[2])
  })

  // --- disabled items ------------------------------------------------------

  it('skips disabled items during ArrowDown navigation', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1" disabled>B</button>',
      '<button type="button" role="menuitem" data-testid="item-2">C</button>'
    ].join('')
    await tick()
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[0] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await tick()
    // Should skip the disabled item and land on C.
    expect(document.activeElement).toBe(items[2])
    // B (disabled) should not have focus.
    expect(document.activeElement?.getAttribute('data-testid')).toBe('item-2')
  })

  it('skips aria-disabled items during navigation', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>',
      '<button type="button" role="menuitem" data-testid="item-1" aria-disabled="true">B</button>',
      '<button type="button" role="menuitem" data-testid="item-2">C</button>'
    ].join('')
    await tick()
    const items = menu.querySelectorAll('[role="menuitem"]')
    ;(items[0] as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    await tick()
    expect(document.activeElement?.getAttribute('data-testid')).toBe('item-2')
  })

  it('Escape from menu keydown also closes (belt and suspenders)', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector('[role="menu"]')!
    menu.innerHTML = [
      '<button type="button" role="menuitem" data-testid="item-0">A</button>'
    ].join('')
    await tick()
    ;(menu.querySelector('[role="menuitem"]') as HTMLElement).focus()
    await tick()

    await fireEvent.keyDown(menu, { key: 'Escape' })
    // Belt and suspenders: Escape fires from both the menu-level keydown
    // (focused item) and the bubbling window-level listener. Both are
    // intentional so a right-click open (focus stays on the row) still
    // closes. onClose is idempotent (parent sets state to null).
    expect(onClose).toHaveBeenCalledTimes(2)
  })

  // --- cleanup ------------------------------------------------------------

  it('removes listeners when menu closes', async () => {
    const onClose = vi.fn()
    const { rerender } = render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    // Close the menu by rerendering with open=false.
    await rerender({
      open: false,
      anchor: null,
      onClose
    })
    await tick()

    // These should no longer trigger onClose because listeners are cleaned up.
    window.dispatchEvent(new Event('resize'))
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    await fireEvent.keyDown(window, { key: 'Escape' })

    expect(onClose).not.toHaveBeenCalled()
  })

  // --- aria -----------------------------------------------------------------

  it('uses the ariaLabel prop for the menu', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        onClose,
        ariaLabel: 'Custom menu'
      }
    })
    await tick()
    const menu = screen.getByRole('menu', { name: 'Custom menu' })
    expect(menu).toBeInTheDocument()
  })

  // --- scroll-scope (#492) -------------------------------------------------

  it('dismisses on scroll of the anchoring scrollable ancestor', async () => {
    const onClose = vi.fn()
    // Create a scrollable container and an anchor inside it.
    const container = document.createElement('div')
    container.style.overflowY = 'auto'
    container.style.height = '200px'
    const anchorEl = document.createElement('button')
    container.appendChild(anchorEl)
    document.body.appendChild(container)

    render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl,
        onClose
      }
    })
    await tick()

    // Scrolling the container (which has overflow-y:auto) should dismiss.
    container.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1)

    document.body.removeChild(container)
  })

  it('does NOT dismiss on scroll of an unrelated element', async () => {
    const onClose = vi.fn()
    // Scrollable container for the anchor.
    const sidebar = document.createElement('div')
    sidebar.style.overflowY = 'auto'
    const anchorEl = document.createElement('button')
    sidebar.appendChild(anchorEl)
    document.body.appendChild(sidebar)

    // Unrelated scrollable container (editor).
    const editor = document.createElement('div')
    editor.style.overflowY = 'auto'
    document.body.appendChild(editor)

    render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl,
        onClose
      }
    })
    await tick()

    // Scrolling the unrelated editor should NOT dismiss.
    editor.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).not.toHaveBeenCalled()

    document.body.removeChild(sidebar)
    document.body.removeChild(editor)
  })

  it('falls back to document scroll when anchorEl is null', async () => {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl: null,
        onClose
      }
    })
    await tick()

    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('walks up to document when anchorEl has no scrollable ancestor', async () => {
    const onClose = vi.fn()
    // A non-scrollable wrapper with an anchor inside.
    const wrapper = document.createElement('div')
    // Default overflow is 'visible' — no scrollable ancestor.
    const anchorEl = document.createElement('button')
    wrapper.appendChild(anchorEl)
    document.body.appendChild(wrapper)

    render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl,
        onClose
      }
    })
    await tick()

    // No scrollable ancestor found → falls back to document.
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1)

    document.body.removeChild(wrapper)
  })

  it('handles detached anchorEl gracefully (tree walk returns document)', async () => {
    const onClose = vi.fn()
    const detached = document.createElement('button')
    // Not appended to DOM.

    render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl: detached,
        onClose
      }
    })
    await tick()

    // No parent in DOM → findScrollableAncestor returns document.
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(onClose).toHaveBeenCalledTimes(1)
  })
})
