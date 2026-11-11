import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import {
  usePageLocatorMenu,
  type UsePageLocatorMenuDeps
} from './pageLocatorMenu.svelte'
import type { NavigationPageRef } from './sidebar/types'

// copyPageReference resolves the [[shortest]] form through the ResolvePageLink
// binding; pageActions imports the bindings module, which the $silt-app alias
// resolves to — one mock covers both import specifiers.
const mocks = vi.hoisted(() => ({
  resolvePageLink: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ResolvePageLink: mocks.resolvePageLink
  })
)

const baseRef: NavigationPageRef = {
  notebook: 'Work',
  section: 'Journal',
  page: 'Daily'
}

function contextMenuEvent(clientX = 10, clientY = 20): MouseEvent {
  return new MouseEvent('contextmenu', {
    bubbles: true,
    cancelable: true,
    clientX,
    clientY
  })
}

/**
 * Host the composable's handler on the row the way a surface template would
 * (oncontextmenu → openFromEvent), then dispatch a real contextmenu so
 * currentTarget/target anchor exactly like production.
 */
function dispatchOnRow(
  row: HTMLElement,
  locator: NavigationPageRef,
  menu: ReturnType<typeof usePageLocatorMenu<NavigationPageRef>>,
  x = 10,
  y = 20
): MouseEvent {
  const listener = (e: MouseEvent) => menu.openFromEvent(e, locator)
  row.addEventListener('contextmenu', listener)
  const e = contextMenuEvent(x, y)
  row.dispatchEvent(e)
  row.removeEventListener('contextmenu', listener)
  return e
}

let row: HTMLButtonElement
let clipboardWrite: ReturnType<typeof vi.fn>

beforeEach(() => {
  row = document.createElement('button')
  document.body.appendChild(row)
  clipboardWrite = vi.fn().mockResolvedValue(undefined)
  Object.assign(navigator, { clipboard: { writeText: clipboardWrite } })
  mocks.resolvePageLink.mockReset()
  mocks.resolvePageLink.mockResolvedValue({
    exists: true,
    shortest: 'Daily'
  })
})

afterEach(() => {
  row.remove()
})

function allDeps() {
  return {
    onOpen: vi.fn<(locator: NavigationPageRef) => void>(),
    onOpenInNewTab: vi.fn<(locator: NavigationPageRef) => void>(),
    onReveal: vi.fn<(locator: NavigationPageRef) => void>(),
    onToggleFavorite: vi.fn<(locator: NavigationPageRef) => void>(),
    isFavorite: vi
      .fn<(locator: NavigationPageRef) => boolean>()
      .mockReturnValue(false)
  } satisfies UsePageLocatorMenuDeps<NavigationPageRef>
}

describe('usePageLocatorMenu — item subsets per injected actions', () => {
  it('yields only copy-page-reference when no actions are injected', () => {
    const menu = usePageLocatorMenu<NavigationPageRef>({})
    expect(menu.items).toEqual([])

    dispatchOnRow(row, baseRef, menu)
    expect(menu.items.map((i) => i.id)).toEqual(['copy-page-reference'])
    expect(menu.items[0].label).toBe('Copy page reference')
    expect(menu.items[0].icon).toBe('link')
  })

  it('yields the full standard vocabulary in canonical order', () => {
    const deps = allDeps()
    const menu = usePageLocatorMenu<NavigationPageRef>(deps)
    expect(menu.items).toEqual([])

    dispatchOnRow(row, baseRef, menu)
    expect(menu.items.map((i) => i.id)).toEqual([
      'open',
      'open-in-new-tab',
      'copy-page-reference',
      'reveal-in-sidebar',
      'toggle-favorite'
    ])
    expect(menu.items.map((i) => i.label)).toEqual([
      'Open',
      'Open in new tab',
      'Copy page reference',
      'Reveal in sidebar',
      'Pin to Quick Access'
    ])
  })

  it('each injected subset appears without the absent actions', () => {
    const menu = usePageLocatorMenu<NavigationPageRef>({
      onOpenInNewTab: vi.fn(),
      onToggleFavorite: vi.fn()
    })
    dispatchOnRow(row, baseRef, menu)
    expect(menu.items.map((i) => i.id)).toEqual([
      'open-in-new-tab',
      'copy-page-reference',
      'toggle-favorite'
    ])
  })
})

describe('usePageLocatorMenu — open/close state', () => {
  it('captures coordinates, anchor, and a by-value locator snapshot per open', () => {
    const menu = usePageLocatorMenu<NavigationPageRef>(allDeps())
    const mutable: NavigationPageRef = { ...baseRef }
    dispatchOnRow(row, mutable, menu)

    expect(menu.menu?.x).toBe(10)
    expect(menu.menu?.y).toBe(20)
    expect(menu.menu?.anchorEl).toBe(row)

    // Mutating the caller's object after open must not leak into the menu.
    mutable.page = 'Changed'
    expect(menu.menu?.locator.page).toBe('Daily')

    // A second open replaces the presentation entirely.
    const other = document.createElement('button')
    document.body.appendChild(other)
    try {
      dispatchOnRow(other, baseRef, menu, 30, 40)
      expect(menu.menu?.x).toBe(30)
      expect(menu.menu?.locator).toEqual(baseRef)
      expect(menu.menu?.anchorEl).toBe(other)
    } finally {
      other.remove()
    }
  })

  it('prevents the native menu on open; close nulls the state', () => {
    const menu = usePageLocatorMenu<NavigationPageRef>(allDeps())
    const e = dispatchOnRow(row, baseRef, menu)
    expect(e.defaultPrevented).toBe(true)
    expect(menu.menu).not.toBeNull()

    menu.close()
    expect(menu.menu).toBeNull()
    expect(menu.items).toEqual([])
  })

  it('anchors to the focused element for keyboard-invoked opens', () => {
    const menu = usePageLocatorMenu<NavigationPageRef>(allDeps())
    row.focus()
    dispatchOnRow(row, baseRef, menu)
    // target === document.activeElement → the focused control is the anchor.
    expect(menu.menu?.anchorEl).toBe(row)
  })
})

describe('usePageLocatorMenu — item activation', () => {
  it('closes the menu before dispatching, with the captured locator', () => {
    const deps = allDeps()
    const menu = usePageLocatorMenu<NavigationPageRef>(deps)
    dispatchOnRow(row, baseRef, menu)

    const openItem = menu.items.find((i) => i.id === 'open')!
    openItem.run()
    expect(menu.menu).toBeNull()
    expect(deps.onOpen).toHaveBeenCalledTimes(1)
    expect(deps.onOpen).toHaveBeenCalledWith(baseRef)
  })

  it('freezes the Unpin/Pin label at open time', () => {
    const deps = allDeps()
    deps.isFavorite.mockReturnValue(true)
    const menu = usePageLocatorMenu<NavigationPageRef>(deps)
    dispatchOnRow(row, baseRef, menu)
    expect(menu.items.at(-1)?.label).toBe('Unpin')

    // The favorite state flipping after open must not relabel the open menu.
    deps.isFavorite.mockReturnValue(false)
    expect(menu.items.at(-1)?.label).toBe('Unpin')
  })

  it('copy-page-reference resolves the shortest form and writes it', async () => {
    const menu = usePageLocatorMenu<NavigationPageRef>(allDeps())
    dispatchOnRow(row, baseRef, menu)

    menu.items.find((i) => i.id === 'copy-page-reference')!.run()
    expect(menu.menu).toBeNull()
    await vi.waitFor(() =>
      expect(mocks.resolvePageLink).toHaveBeenCalledWith('Work/Journal/Daily')
    )
    await vi.waitFor(() =>
      expect(clipboardWrite).toHaveBeenCalledWith('[[Daily]]')
    )
  })
})
