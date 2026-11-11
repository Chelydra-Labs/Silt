import { copyPageReference, type PageActionRef } from './pageActions'

/**
 * Shared page-locator context-menu vocabulary (#1120): the standard item set
 * — Open / Open in new tab / Copy page reference / Reveal in sidebar /
 * Toggle favorite — every locator-bearing surface offers, with the identical
 * labels, icons, ordering, chrome (the `ContextMenu` primitive), dismissal,
 * and keyboard behavior. A surface injects only the actions that apply to
 * it; uninjected actions are absent, not disabled.
 */

export type PageLocatorItemId =
  | 'open'
  | 'open-in-new-tab'
  | 'copy-page-reference'
  | 'reveal-in-sidebar'
  | 'toggle-favorite'

export interface PageLocatorMenuItem {
  id: PageLocatorItemId
  /** Material Symbols icon name (surfaces own the glyph rendering). */
  icon: string
  label: string
  /** Runs the action: closes the menu, then dispatches. */
  run: () => void
}

/** Menu presentation state; `null` = closed. */
export interface PageLocatorMenuState<T extends PageActionRef = PageActionRef> {
  /** Shallow snapshot of the row locator captured at open (by value). */
  locator: T
  x: number
  y: number
  /** Element owning the menu (scroll-dismiss scope + focus restore). */
  anchorEl: HTMLElement | null
  /** Favorite state at open time — the Unpin/Pin label is frozen with it. */
  favorite: boolean
}

export interface UsePageLocatorMenuDeps<
  T extends PageActionRef = PageActionRef
> {
  /** Open the page in the current/preview tab (single-click parity). */
  onOpen?: (locator: T) => void
  /** Open the page in a pinned tab (double/middle-click parity). */
  onOpenInNewTab?: (locator: T) => void
  /** Reveal the page through the surface's existing navigate path. */
  onReveal?: (locator: T) => void
  /** Toggle the Quick Access favorite. */
  onToggleFavorite?: (locator: T) => void
  /** Current favorite state, read once at open for the item label. */
  isFavorite?: (locator: T) => boolean
}

/**
 * Headless controller for the page-locator menu: owns open/close + the item
 * list for the open menu. The state is captured by value at open — item
 * activations close the menu first, so handlers never read a re-evaluated
 * closed state (the same lesson the task + day-cell menus bake in).
 *
 * Clipboard actions (Copy page reference) are owned here, not injected —
 * `lib/pageActions` is the single source for the `[[shortest]]` resolution.
 */
export function usePageLocatorMenu<T extends PageActionRef = PageActionRef>(
  deps: UsePageLocatorMenuDeps<T>
) {
  let menu = $state<PageLocatorMenuState<T> | null>(null)

  function openFromEvent(e: MouseEvent, locator: T): void {
    e.preventDefault()
    const target = e.target instanceof HTMLElement ? e.target : null
    // Keyboard invocation dispatches the synthetic contextmenu at the focused
    // control itself, so the focused element wins; mouse invocation anchors
    // to the handler's element. Either way the anchor is the element the
    // focus restore should land back on.
    const anchorEl =
      target && target === document.activeElement
        ? target
        : e.currentTarget instanceof HTMLElement
          ? e.currentTarget
          : target
    menu = {
      locator: { ...locator },
      x: e.clientX,
      y: e.clientY,
      anchorEl,
      favorite: deps.isFavorite?.(locator) ?? false
    }
  }

  function close(): void {
    menu = null
  }

  /** Items for the open menu — only the actions this surface injected. */
  const items = $derived.by(() => {
    if (!menu) return []
    const { locator, favorite } = menu
    const run = (action: () => void) => () => {
      close()
      action()
    }
    const list: PageLocatorMenuItem[] = []
    if (deps.onOpen) {
      list.push({
        id: 'open',
        icon: 'description',
        label: 'Open',
        run: run(() => deps.onOpen?.(locator))
      })
    }
    if (deps.onOpenInNewTab) {
      list.push({
        id: 'open-in-new-tab',
        icon: 'open_in_new',
        label: 'Open in new tab',
        run: run(() => deps.onOpenInNewTab?.(locator))
      })
    }
    list.push({
      id: 'copy-page-reference',
      icon: 'link',
      label: 'Copy page reference',
      // Fire-and-forget: resolution failures fall back to the full path and
      // clipboard failures toast, both inside copyPageReference.
      run: run(() => void copyPageReference(locator))
    })
    if (deps.onReveal) {
      list.push({
        id: 'reveal-in-sidebar',
        icon: 'find_in_page',
        label: 'Reveal in sidebar',
        run: run(() => deps.onReveal?.(locator))
      })
    }
    if (deps.onToggleFavorite) {
      list.push({
        id: 'toggle-favorite',
        icon: 'push_pin',
        label: favorite ? 'Unpin' : 'Pin to Quick Access',
        run: run(() => deps.onToggleFavorite?.(locator))
      })
    }
    return list
  })

  return {
    get menu() {
      return menu
    },
    get items() {
      return items
    },
    openFromEvent,
    close
  }
}

export type PageLocatorMenuController = ReturnType<typeof usePageLocatorMenu>
