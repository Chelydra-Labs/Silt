import { describe, expect, it, beforeEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, fireEvent } from '@testing-library/svelte'

// Mock clampToViewport so we can assert it was called without depending on
// real viewport dimensions in jsdom. vi.hoisted ensures the mock is available
// when vi.mock's factory runs (both are hoisted to the top of the file).
// findScrollableAncestor stays real so scroll-scope tests exercise the shared
// walker (popoverPositioning.ts).
const mocks = vi.hoisted(() => ({
  clampToViewport: vi.fn().mockReturnValue({ left: 100, top: 100 })
}))

vi.mock('../lib/editor/popoverPositioning', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('../lib/editor/popoverPositioning')>()
  return {
    ...actual,
    clampToViewport: mocks.clampToViewport
  }
})

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

  // --- focus management -------------------------------------------------------

  it('restores focus to the trigger element on close', async () => {
    const onClose = vi.fn()
    const trigger = document.createElement('button')
    trigger.textContent = 'Trigger'
    document.body.appendChild(trigger)
    trigger.focus()

    const { rerender } = render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl: trigger,
        onClose
      }
    })
    await tick()
    // Focus moved to the menu item.
    expect(document.activeElement).not.toBe(trigger)

    // Close the menu.
    await rerender({ open: false, anchor: null, anchorEl: null, onClose })
    await tick()
    // Focus restored to trigger.
    expect(document.activeElement).toBe(trigger)

    document.body.removeChild(trigger)
  })

  it('does not restore focus to the trigger when closed via Tab', async () => {
    const onClose = vi.fn()
    const trigger = document.createElement('button')
    trigger.textContent = 'Trigger'
    document.body.appendChild(trigger)
    trigger.focus()
    const focusSpy = vi.spyOn(trigger, 'focus')

    const { rerender } = render(ContextMenu, {
      props: {
        open: true,
        anchor: { x: 100, y: 100 },
        anchorEl: trigger,
        onClose
      }
    })
    await tick()
    // Focus moved into the menu by the open effect.
    const menu = document.activeElement as HTMLElement
    expect(menu).not.toBe(trigger)

    // Tab closes the menu; the default Tab action must carry focus out,
    // so the teardown may not pull focus back to the trigger.
    await fireEvent.keyDown(menu, { key: 'Tab' })
    expect(onClose).toHaveBeenCalledTimes(1)
    await rerender({ open: false, anchor: null, anchorEl: null, onClose })
    await tick()
    expect(focusSpy).not.toHaveBeenCalled()

    // The suppression is per-presentation: a subsequent open must restore
    // focus on close again (the flag must not leak into later closes).
    await rerender({
      open: true,
      anchor: { x: 100, y: 100 },
      anchorEl: trigger,
      onClose
    })
    await tick()
    await fireEvent.keyDown(document.activeElement as HTMLElement, {
      key: 'Escape'
    })
    await rerender({ open: false, anchor: null, anchorEl: null, onClose })
    await tick()
    expect(focusSpy).toHaveBeenCalledTimes(1)

    document.body.removeChild(trigger)
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
    // Escape on a focused menu item fires only the menu-level keydown;
    // e.stopPropagation() prevents the window-level listener from also firing.
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  // --- Escape isolation (#1142 harden) -------------------------------------
  //
  // App-level Escape listeners (e.g. sidebar drawer dismissal) register at
  // app mount — before the menu's own listener. Dismissing a menu must never
  // close the surface behind it.

  it('swallows Escape for app-level listeners when focus is outside the menu', async () => {
    const onClose = vi.fn()
    // Registered before the menu opens, like a drawer Escape handler.
    const appListener = vi.fn()
    window.addEventListener('keydown', appListener)
    try {
      render(ContextMenu, {
        props: { open: true, anchor: { x: 100, y: 100 }, onClose }
      })
      await tick()
      // Focus stays on an element outside the menu — the fallback path the
      // window listener exists for.
      const outside = document.createElement('button')
      document.body.appendChild(outside)
      outside.focus()
      await fireEvent.keyDown(outside, { key: 'Escape' })

      expect(onClose).toHaveBeenCalledTimes(1)
      expect(appListener).not.toHaveBeenCalled()
      document.body.removeChild(outside)
    } finally {
      window.removeEventListener('keydown', appListener)
    }
  })

  it('keeps in-menu Escape away from app-level listeners too', async () => {
    const onClose = vi.fn()
    const appListener = vi.fn()
    window.addEventListener('keydown', appListener)
    try {
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

      await fireEvent.keyDown(menu.querySelector('[role="menuitem"]')!, {
        key: 'Escape'
      })

      expect(onClose).toHaveBeenCalledTimes(1)
      expect(appListener).not.toHaveBeenCalled()
    } finally {
      window.removeEventListener('keydown', appListener)
    }
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

  // --- submenus (WAI-ARIA APG flyout pattern) ------------------------------
  //
  // Parent items opt in via aria-haspopup="menu" + a sibling [role="menu"]
  // right after them, both authored in the children snippet.

  const SUBMENU_MARKUP = [
    '<button type="button" role="menuitem" data-testid="leaf-1">Leaf 1</button>',
    '<button type="button" role="menuitem" aria-haspopup="menu" data-testid="parent-1">Parent 1</button>',
    '<div role="menu" data-testid="sub-1" aria-label="Submenu one">',
    '<button type="button" role="menuitem" data-testid="sub-item-1">Sub A</button>',
    '<button type="button" role="menuitem" data-testid="sub-item-2">Sub B</button>',
    '</div>',
    '<button type="button" role="menuitem" data-testid="leaf-2">Leaf 2</button>'
  ].join('')

  async function setupSubmenuMenu() {
    const onClose = vi.fn()
    render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector(
      '[data-context-menu-root]'
    ) as HTMLElement
    menu.innerHTML = SUBMENU_MARKUP
    await tick()
    return {
      onClose,
      menu,
      leaf1: screen.getByTestId('leaf-1') as HTMLElement,
      parent: screen.getByTestId('parent-1') as HTMLElement,
      sub: screen.getByTestId('sub-1') as HTMLElement,
      sub1: screen.getByTestId('sub-item-1') as HTMLElement,
      sub2: screen.getByTestId('sub-item-2') as HTMLElement,
      leaf2: screen.getByTestId('leaf-2') as HTMLElement
    }
  }

  async function openFlyout() {
    const ctx = await setupSubmenuMenu()
    ctx.parent.focus()
    await fireEvent.keyDown(ctx.menu, { key: 'ArrowRight' })
    return ctx
  }

  it('seeds aria-expanded=false on parent items when the menu opens', async () => {
    const onClose = vi.fn()
    const { rerender } = render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    ;(
      document.querySelector('[data-context-menu-root]') as HTMLElement
    ).innerHTML = SUBMENU_MARKUP
    // Re-present the menu at a new anchor so the open effects run again,
    // now with the injected markup in the DOM.
    await rerender({ open: true, anchor: { x: 120, y: 120 }, onClose })
    await tick()
    expect(screen.getByTestId('parent-1')).toHaveAttribute(
      'aria-expanded',
      'false'
    )
  })

  it('closes open flyouts when re-anchored while the menu stays open', async () => {
    const onClose = vi.fn()
    const { rerender } = render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    await tick()
    const menu = document.querySelector(
      '[data-context-menu-root]'
    ) as HTMLElement
    menu.innerHTML = SUBMENU_MARKUP
    await tick()
    const parent = screen.getByTestId('parent-1') as HTMLElement
    const sub = screen.getByTestId('sub-1') as HTMLElement
    parent.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    expect(sub).toHaveClass('is-open')

    // The documented re-anchor flow: new anchor, open stays true.
    await rerender({ open: true, anchor: { x: 300, y: 300 }, onClose })
    await tick()
    expect(sub).not.toHaveClass('is-open')
    expect(parent).toHaveAttribute('aria-expanded', 'false')
  })

  it('seeds aria-expanded on parent items inserted while the menu stays open', async () => {
    const ctx = await setupSubmenuMenu()
    // Dynamic content: this parent missed the open-time seed pass.
    const late = document.createElement('button')
    late.type = 'button'
    late.setAttribute('role', 'menuitem')
    late.setAttribute('aria-haspopup', 'menu')
    late.setAttribute('data-testid', 'parent-late')
    ctx.leaf2.after(late)

    ctx.parent.focus()
    await fireEvent.keyDown(ctx.menu, { key: 'ArrowRight' })
    expect(late).toHaveAttribute('aria-expanded', 'false')
  })

  it('ArrowRight on a parent item opens the flyout and focuses its first item', async () => {
    const { parent, sub, sub1, onClose } = await openFlyout()
    expect(parent).toHaveAttribute('aria-expanded', 'true')
    expect(sub).toHaveClass('is-open')
    expect(sub.getAttribute('role')).toBe('menu')
    expect(sub.getAttribute('aria-labelledby')).toBe(parent.id)
    expect(parent.id).toBeTruthy()
    expect(document.activeElement).toBe(sub1)
    expect(onClose).not.toHaveBeenCalled()
  })

  it('Enter and Space on a parent item also open the flyout', async () => {
    const a = await setupSubmenuMenu()
    a.parent.focus()
    await fireEvent.keyDown(a.menu, { key: 'Enter' })
    expect(a.parent).toHaveAttribute('aria-expanded', 'true')
    expect(document.activeElement).toBe(a.sub1)

    // Close again, then reopen with Space.
    await fireEvent.keyDown(a.sub1, { key: 'ArrowLeft' })
    expect(a.parent).toHaveAttribute('aria-expanded', 'false')
    a.parent.focus()
    await fireEvent.keyDown(a.menu, { key: ' ' })
    expect(a.parent).toHaveAttribute('aria-expanded', 'true')
    expect(document.activeElement).toBe(a.sub1)
  })

  it('ArrowLeft inside the flyout closes it and refocuses the parent item', async () => {
    const { parent, sub, sub2 } = await openFlyout()
    sub2.focus()
    await fireEvent.keyDown(sub2, { key: 'ArrowLeft' })
    expect(parent).toHaveAttribute('aria-expanded', 'false')
    expect(sub).not.toHaveClass('is-open')
    expect(document.activeElement).toBe(parent)
  })

  it('Escape inside the flyout closes only that level, not the whole menu', async () => {
    const { onClose, parent, sub, sub2 } = await openFlyout()
    sub2.focus()
    await fireEvent.keyDown(sub2, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
    expect(parent).toHaveAttribute('aria-expanded', 'false')
    expect(sub).not.toHaveClass('is-open')
    expect(document.activeElement).toBe(parent)

    // A second Escape at root level closes everything.
    await fireEvent.keyDown(parent, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('Tab while a flyout is open closes the whole menu', async () => {
    const { onClose, sub1, sub } = await openFlyout()
    await fireEvent.keyDown(sub1, { key: 'Tab' })
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(sub).not.toHaveClass('is-open')
  })

  it('ArrowRight on a leaf item does nothing', async () => {
    const { menu, leaf1, sub, parent, onClose } = await setupSubmenuMenu()
    leaf1.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    expect(sub).not.toHaveClass('is-open')
    expect(parent.getAttribute('aria-expanded')).toBeNull()
    expect(onClose).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(leaf1)
  })

  it('ArrowLeft at root level does nothing', async () => {
    const { menu, leaf1, onClose } = await setupSubmenuMenu()
    leaf1.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowLeft' })
    expect(onClose).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(leaf1)
  })

  it('scopes roving arrows to the root level while the flyout is hidden', async () => {
    const { menu, leaf1, parent } = await setupSubmenuMenu()
    leaf1.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowDown' })
    // Lands on the parent item, not on the hidden flyout's first item.
    expect(document.activeElement).toBe(parent)
  })

  it('scopes arrows, Home and End to the flyout level while it is open', async () => {
    const { sub1, sub2, leaf2 } = await openFlyout()
    // Inside the flyout: wrap without touching root items.
    sub2.focus()
    await fireEvent.keyDown(sub2, { key: 'ArrowDown' })
    expect(document.activeElement).toBe(sub1)
    await fireEvent.keyDown(sub1, { key: 'End' })
    expect(document.activeElement).toBe(sub2)
    await fireEvent.keyDown(sub2, { key: 'Home' })
    expect(document.activeElement).toBe(sub1)
    expect(document.activeElement).not.toBe(leaf2)
  })

  it('clicking a parent item toggles the flyout open and closed', async () => {
    const { parent, sub, sub1 } = await setupSubmenuMenu()
    await fireEvent.click(parent)
    expect(parent).toHaveAttribute('aria-expanded', 'true')
    expect(sub).toHaveClass('is-open')
    expect(document.activeElement).toBe(sub1)

    await fireEvent.click(parent)
    expect(parent).toHaveAttribute('aria-expanded', 'false')
    expect(sub).not.toHaveClass('is-open')
  })

  it('a disabled parent item does not open its flyout', async () => {
    const { menu, parent, sub } = await setupSubmenuMenu()
    parent.setAttribute('disabled', '')
    parent.setAttribute('aria-disabled', 'true')
    parent.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    expect(sub).not.toHaveClass('is-open')
    expect(parent.getAttribute('aria-expanded')).toBeNull()
  })

  it('positions the flyout beside the parent item via clampToViewport', async () => {
    const { menu, parent, sub } = await setupSubmenuMenu()
    vi.spyOn(parent, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      right: 300,
      top: 200,
      width: 200,
      height: 28,
      x: 100,
      y: 200,
      toJSON: () => ({})
    } as DOMRect)
    // Rendered where intended (viewport containing block): the correction
    // pass must be a no-op.
    vi.spyOn(sub, 'getBoundingClientRect').mockReturnValue({
      left: 300,
      top: 200,
      width: 0,
      height: 0,
      x: 300,
      y: 200,
      toJSON: () => ({})
    } as DOMRect)
    parent.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    // Right side has room: flyout's left edge at the item right edge, top
    // aligned with the item (clamp keeps both as-is inside 1920×1080).
    expect(sub.style.left).toBe('300px')
    expect(sub.style.top).toBe('200px')
  })

  it('flips the flyout to the left of the parent item when there is no room right', async () => {
    const { menu, parent, sub } = await setupSubmenuMenu()
    Object.defineProperty(sub, 'offsetWidth', { value: 300 })
    Object.defineProperty(sub, 'offsetHeight', { value: 80 })
    vi.spyOn(parent, 'getBoundingClientRect').mockReturnValue({
      left: 1700,
      right: 1900,
      top: 100,
      width: 200,
      height: 28,
      x: 1700,
      y: 100,
      toJSON: () => ({})
    } as DOMRect)
    vi.spyOn(sub, 'getBoundingClientRect').mockReturnValue({
      left: 1400,
      top: 100,
      width: 300,
      height: 80,
      x: 1400,
      y: 100,
      toJSON: () => ({})
    } as DOMRect)
    parent.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    // 1900 + 300 > 1920 - 8 → flip: left edge = 1700 - 300 = 1400.
    expect(sub.style.left).toBe('1400px')
    expect(sub.style.top).toBe('100px')
  })

  it('corrects flyout coords for a backdrop-filtered containing block', async () => {
    // jsdom has no layout, so the gBCR chain is mocked to model a real
    // browser: the card sits at (400, 320) and, because of its
    // backdrop-filter, is the flyout's containing block — provisional
    // viewport coords render offset by the card's origin.
    const { menu, parent, sub } = await setupSubmenuMenu()
    vi.spyOn(parent, 'getBoundingClientRect').mockReturnValue({
      left: 100,
      right: 300,
      top: 200,
      width: 200,
      height: 28,
      x: 100,
      y: 200,
      toJSON: () => ({})
    } as DOMRect)
    vi.spyOn(sub, 'getBoundingClientRect').mockReturnValue({
      left: 300 + 400,
      top: 200 + 320,
      width: 0,
      height: 0,
      x: 700,
      y: 520,
      toJSON: () => ({})
    } as DOMRect)
    parent.focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    // Intended viewport pos (300, 200) rendered at (700, 520): the fixed
    // coords shift by the delta into containing-block space, so the flyout
    // lands at (300, 200) on screen again.
    expect(sub.style.left).toBe('-100px')
    expect(sub.style.top).toBe('-120px')
  })

  it('initial focus stays on root items when a nested menu has no parent item', async () => {
    const onClose = vi.fn()
    const { rerender } = render(ContextMenu, {
      props: { open: true, anchor: { x: 100, y: 100 }, onClose }
    })
    ;(
      document.querySelector('[data-context-menu-root]') as HTMLElement
    ).innerHTML = [
      '<button type="button" role="menuitem" data-testid="leaf-1">Leaf 1</button>',
      '<div role="menu" data-testid="orphan-sub">',
      '<button type="button" role="menuitem" data-testid="orphan-item">Orphan</button>',
      '</div>',
      '<button type="button" role="menuitem" data-testid="leaf-2">Leaf 2</button>'
    ].join('')
    await rerender({ open: true, anchor: { x: 120, y: 120 }, onClose })
    await tick()
    // First ROOT item is focused, and the orphan nested menu stays hidden.
    expect(document.activeElement).toBe(screen.getByTestId('leaf-1'))
    expect(screen.getByTestId('orphan-sub')).not.toHaveClass('is-open')
  })
})
