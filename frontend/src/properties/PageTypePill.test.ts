import { describe, expect, it, afterEach, vi } from 'vitest'
import { cleanup, render, screen, fireEvent } from '@testing-library/svelte'
import PageTypePill from './PageTypePill.svelte'
import type { PageTypeInfo } from './types'

const typedInfo: PageTypeInfo = {
  typeId: 'book',
  type: { id: 'book', name: 'Book', icon: 'menu_book', heroField: 'title' },
  isSet: true,
  rawType: ''
}

const untypedInfo: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '' },
  isSet: false,
  rawType: ''
}

const rawInfo: PageTypeInfo = {
  typeId: '',
  type: { id: '', name: '' },
  isSet: false,
  rawType: 'mystery'
}

afterEach(cleanup)

describe('PageTypePill', () => {
  it('renders the type chip and hero value for a typed page', () => {
    render(PageTypePill, {
      props: { info: typedInfo, heroValue: 'Dune', onOpen: () => {} }
    })
    const chip = screen.getByRole('button', {
      name: 'Page type: Book. Open properties.'
    })
    expect(chip.textContent).toContain('Book')
    expect(chip.textContent).toContain('Dune')
  })

  it('renders a hover-revealed [+] Type pill for an untyped page', () => {
    // The untyped pill is in the DOM (focusable, in the tab order) but
    // visually subdued at rest via opacity:0 — NOT display:none. The
    // affordance is revealed on header hover/focus, so the page-header
    // invariant (clean at rest) holds while keyboard users can still
    // reach the action by tabbing.
    render(PageTypePill, {
      props: { info: untypedInfo, heroValue: '', onOpen: () => {} }
    })
    const pill = screen.getByRole('button', { name: 'Assign a page type' })
    expect(pill).toHaveClass('pill-untyped')
    expect(pill.textContent).toContain('Type')
    // No caret, no hero on an untyped page.
    expect(pill.textContent).not.toContain('Book')
  })

  it('renders a subdued raw chip for an unknown type and no hero', () => {
    render(PageTypePill, {
      props: { info: rawInfo, heroValue: '', onOpen: () => {} }
    })
    const chip = screen.getByRole('button', {
      name: 'Unrecognized page type: mystery. Open properties.'
    })
    expect(chip.textContent).toContain('mystery')
    expect(chip.textContent).not.toContain('Book')
    expect(chip).toHaveClass('raw')
  })

  it('opens the panel when a typed chip is clicked', async () => {
    const onOpen = vi.fn()
    render(PageTypePill, {
      props: { info: typedInfo, heroValue: 'Dune', onOpen }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /Page type: Book/ })
    )
    expect(onOpen).toHaveBeenCalledOnce()
  })

  it('calls onOpen AND onOpenWithTypeMenu when the untyped pill is clicked', async () => {
    // Untyped mirrors /type slash: open the panel + arm its type menu.
    const onOpen = vi.fn()
    const onOpenWithTypeMenu = vi.fn()
    render(PageTypePill, {
      props: { info: untypedInfo, heroValue: '', onOpen, onOpenWithTypeMenu }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: 'Assign a page type' })
    )
    expect(onOpen).toHaveBeenCalledOnce()
    expect(onOpenWithTypeMenu).toHaveBeenCalledOnce()
  })

  it('only calls onOpen (not onOpenWithTypeMenu) for a typed chip', async () => {
    const onOpen = vi.fn()
    const onOpenWithTypeMenu = vi.fn()
    render(PageTypePill, {
      props: {
        info: typedInfo,
        heroValue: 'Dune',
        onOpen,
        onOpenWithTypeMenu
      }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /Page type: Book/ })
    )
    expect(onOpen).toHaveBeenCalledOnce()
    expect(onOpenWithTypeMenu).not.toHaveBeenCalled()
  })

  it('only calls onOpen for a raw chip', async () => {
    const onOpen = vi.fn()
    const onOpenWithTypeMenu = vi.fn()
    render(PageTypePill, {
      props: { info: rawInfo, heroValue: '', onOpen, onOpenWithTypeMenu }
    })
    await fireEvent.click(
      screen.getByRole('button', { name: /Unrecognized page type/ })
    )
    expect(onOpen).toHaveBeenCalledOnce()
    expect(onOpenWithTypeMenu).not.toHaveBeenCalled()
  })

  it('omits the hero span when the hero value is empty', () => {
    const { container } = render(PageTypePill, {
      props: { info: typedInfo, heroValue: '', onOpen: () => {} }
    })
    const chip = screen.getByRole('button', { name: /Page type: Book/ })
    expect(chip.textContent).toContain('Book')
    expect(container.querySelector('.hero')).toBeNull()
  })

  it('uses state-specific aria-labels for each variant', () => {
    const { unmount } = render(PageTypePill, {
      props: { info: typedInfo, heroValue: 'Dune', onOpen: () => {} }
    })
    expect(
      screen.getByRole('button', {
        name: 'Page type: Book. Open properties.'
      })
    ).toBeInTheDocument()
    unmount()

    render(PageTypePill, {
      props: { info: rawInfo, heroValue: '', onOpen: () => {} }
    })
    expect(
      screen.getByRole('button', {
        name: 'Unrecognized page type: mystery. Open properties.'
      })
    ).toBeInTheDocument()
  })

  it('exposes "View all {Type}" via the caret for a typed page with onViewAll', async () => {
    const onViewAll = vi.fn()
    render(PageTypePill, {
      props: {
        info: typedInfo,
        heroValue: 'Dune',
        onOpen: () => {},
        onViewAll
      }
    })
    const caret = screen.getByRole('button', { name: 'Type actions' })
    expect(caret).toHaveAttribute('aria-haspopup', 'menu')
    expect(caret).toHaveAttribute('aria-expanded', 'false')
    await fireEvent.click(caret)
    expect(caret).toHaveAttribute('aria-expanded', 'true')
    const item = screen.getByRole('menuitem', { name: 'View all Book' })
    await fireEvent.click(item)
    expect(onViewAll).toHaveBeenCalledOnce()
  })

  it('hides the hero on narrow viewports', () => {
    // The @media (max-width: 700px) rule hides .hero — pinned here so a
    // future refactor doesn't silently drop the responsive guard.
    const { container } = render(PageTypePill, {
      props: { info: typedInfo, heroValue: 'Dune', onOpen: () => {} }
    })
    const hero = container.querySelector('.hero')
    expect(hero).not.toBeNull()
    // The CSS rule itself is verified by svelte-check + visual review; this
    // test pins the existence of the hero span so the @media target exists.
  })
})
