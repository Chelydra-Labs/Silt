import { describe, expect, it, vi, afterEach } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

import ColumnHeader from './ColumnHeader.svelte'

// ColumnHeader is presentational: every callback is a spy and the menu is
// driven through the `menu` anchor prop, exactly what BoardView feeds it.
function baseProps(overrides: Record<string, unknown> = {}) {
  return {
    colKey: 'status-TODO',
    colLabel: 'To Do',
    colValue: 'status-TODO',
    cardCount: 2,
    estimateSum: 0,
    canManage: true,
    wipLimit: null,
    overWip: false,
    dndEnabled: true,
    renaming: false,
    menu: null as { x: number; y: number } | null,
    wipEditing: false,
    wipEditError: '',
    renameValue: '',
    wipDraft: '',
    onOpenMenu: vi.fn(),
    onCloseMenu: vi.fn(),
    onStartRename: vi.fn(),
    onCommitRename: vi.fn(),
    onCancelRename: vi.fn(),
    onRemoveColumn: vi.fn(),
    onStartWipEdit: vi.fn(),
    onApplyWipLimit: vi.fn(),
    onClearWipLimit: vi.fn(),
    onWipEscape: vi.fn(),
    onColDragStart: vi.fn(),
    onColDragOver: vi.fn(),
    onColDrop: vi.fn(),
    onColDragEnd: vi.fn(),
    ...overrides
  }
}

/** Pin an element's rect so trigger coordinates are assertable in jsdom. */
function mockRect(
  el: Element,
  rect: { left: number; top: number; bottom: number }
) {
  el.getBoundingClientRect = () =>
    ({
      left: rect.left,
      top: rect.top,
      bottom: rect.bottom,
      right: rect.left + 28,
      width: 28,
      height: rect.bottom - rect.top,
      x: rect.left,
      y: rect.top,
      toJSON() {
        return this
      }
    }) as DOMRect
}

function headerEl(): HTMLElement {
  return screen.getByRole('group', { name: 'To Do column header' })
}

function menuButton(): HTMLElement {
  return screen.getByRole('button', { name: 'Column actions' })
}

async function openMenu() {
  const onOpenMenu = vi.fn()
  const { rerender } = render(ColumnHeader, {
    props: baseProps({ onOpenMenu })
  })
  await tick()
  await rerender(baseProps({ onOpenMenu, menu: { x: 10, y: 10 } }))
  await tick()
  return { onOpenMenu, rerender }
}

afterEach(cleanup)

describe('ColumnHeader — column action menu (#1121)', () => {
  // --- triggers ------------------------------------------------------------

  it('⋯ button opens the menu at the button rect (SavedViews shape)', async () => {
    const props = baseProps()
    render(ColumnHeader, { props })
    await tick()
    const btn = menuButton()
    mockRect(btn, { left: 100, top: 40, bottom: 50 })

    await fireEvent.click(btn)

    expect(props.onOpenMenu).toHaveBeenCalledWith(100, 52)
    expect(props.onCloseMenu).not.toHaveBeenCalled()
  })

  it('⋯ button toggles the menu closed when it is already open', async () => {
    const props = baseProps({ menu: { x: 5, y: 5 } })
    render(ColumnHeader, { props })
    await tick()

    await fireEvent.click(menuButton())

    expect(props.onCloseMenu).toHaveBeenCalledTimes(1)
    expect(props.onOpenMenu).not.toHaveBeenCalled()
  })

  it('right-click on the header opens the menu at the pointer', async () => {
    const props = baseProps()
    render(ColumnHeader, { props })
    await tick()

    const e = await fireEvent.contextMenu(headerEl(), {
      clientX: 220,
      clientY: 140
    })

    // fireEvent returns false when a cancelable event was default-prevented —
    // here that's exactly the contract (native menu suppressed).
    expect(e).toBe(false)
    expect(props.onOpenMenu).toHaveBeenCalledWith(220, 140)
  })

  it('right-click suppresses the native browser menu', async () => {
    const props = baseProps()
    render(ColumnHeader, { props })
    await tick()

    const evt = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true,
      clientX: 8,
      clientY: 9
    })
    headerEl().dispatchEvent(evt)

    expect(evt.defaultPrevented).toBe(true)
  })

  it('context-menu key (Shift+F10 / Menu) on the ⋯ button opens the menu at the button', async () => {
    const props = baseProps()
    render(ColumnHeader, { props })
    await tick()
    const btn = menuButton()
    mockRect(btn, { left: 30, top: 12, bottom: 40 })

    await fireEvent.keyDown(btn, { key: 'F10', shiftKey: true })
    expect(props.onOpenMenu).toHaveBeenCalledWith(30, 40)

    await fireEvent.keyDown(btn, { key: 'ContextMenu' })
    expect(props.onOpenMenu).toHaveBeenCalledTimes(2)
  })

  it('canManage=false renders no ⋯ button and leaves right-click to the browser', async () => {
    const props = baseProps({
      canManage: false,
      menu: { x: 1, y: 1 }
    })
    render(ColumnHeader, { props })
    await tick()

    expect(screen.queryByRole('button', { name: 'Column actions' })).toBeNull()
    // No menu even if a stale anchor somehow arrives.
    expect(screen.queryByRole('menu')).toBeNull()

    const evt = new MouseEvent('contextmenu', {
      bubbles: true,
      cancelable: true
    })
    headerEl().dispatchEvent(evt)
    expect(evt.defaultPrevented).toBe(false)
    expect(props.onOpenMenu).not.toHaveBeenCalled()
  })

  // --- menu contents through the shared primitive --------------------------

  it('renders Rename / WIP limit… / Remove through the primitive and focuses the first item', async () => {
    render(ColumnHeader, { props: baseProps({ menu: { x: 10, y: 10 } }) })
    await tick()

    const menu = screen.getByRole('menu')
    expect(menu).toHaveAttribute('aria-label', 'Actions for To Do column')
    const rename = screen.getByRole('menuitem', { name: 'Rename' })
    const wip = screen.getByRole('menuitem', { name: 'WIP limit…' })
    const remove = screen.getByRole('menuitem', { name: 'Remove' })
    // DOM order: Rename, WIP limit…, Remove (comparePosition, not query order).
    expect(
      rename.compareDocumentPosition(wip) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(
      wip.compareDocumentPosition(remove) & Node.DOCUMENT_POSITION_FOLLOWING
    ).toBeTruthy()
    expect(rename).toHaveFocus()
  })

  it('aria-expanded on the ⋯ button tracks the menu state', async () => {
    const props = baseProps()
    const { rerender } = render(ColumnHeader, { props })
    await tick()
    expect(menuButton()).toHaveAttribute('aria-expanded', 'false')

    await rerender(baseProps({ menu: { x: 0, y: 0 } }))
    await tick()
    expect(menuButton()).toHaveAttribute('aria-expanded', 'true')
  })

  it('Rename and Remove items fire their callbacks', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 } })
    render(ColumnHeader, { props })
    await tick()

    await fireEvent.click(screen.getByRole('menuitem', { name: 'Rename' }))
    expect(props.onStartRename).toHaveBeenCalledTimes(1)

    await fireEvent.click(screen.getByRole('menuitem', { name: 'Remove' }))
    expect(props.onRemoveColumn).toHaveBeenCalledTimes(1)
  })

  // --- primitive dismissal + focus return ----------------------------------

  it('Escape and backdrop click dismiss via the primitive', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 } })
    render(ColumnHeader, { props })
    await tick()

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(props.onCloseMenu).toHaveBeenCalledTimes(1)

    await fireEvent.click(screen.getByLabelText('Close context menu'))
    expect(props.onCloseMenu).toHaveBeenCalledTimes(2)
  })

  it('scroll (anchor scope fallback) dismisses the menu', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 } })
    render(ColumnHeader, { props })
    await tick()

    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    expect(props.onCloseMenu).toHaveBeenCalledTimes(1)
  })

  it('closing the menu returns focus to the ⋯ button (anchorEl)', async () => {
    const { rerender } = await openMenu()
    const btn = menuButton()
    expect(document.activeElement).not.toBe(btn)

    await rerender(baseProps({ menu: null }))
    await tick()
    expect(document.activeElement).toBe(btn)
  })

  it('Arrow keys rove between the three items', async () => {
    render(ColumnHeader, { props: baseProps({ menu: { x: 10, y: 10 } }) })
    await tick()
    const [rename, wip, remove] = screen.getAllByRole('menuitem')

    await fireEvent.keyDown(rename, { key: 'ArrowDown' })
    expect(wip).toHaveFocus()
    await fireEvent.keyDown(wip, { key: 'ArrowDown' })
    expect(remove).toHaveFocus()
    await fireEvent.keyDown(remove, { key: 'ArrowUp' })
    expect(wip).toHaveFocus()
  })

  // --- inline WIP editor ---------------------------------------------------

  it('WIP limit… item starts the editor; the editor input takes focus', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 } })
    render(ColumnHeader, { props })
    await tick()

    await fireEvent.click(screen.getByRole('menuitem', { name: 'WIP limit…' }))
    expect(props.onStartWipEdit).toHaveBeenCalledTimes(1)
  })

  it('WIP editor: typing reaches the draft and Enter applies without closing the menu from the input', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 }, wipEditing: true })
    render(ColumnHeader, { props })
    await tick()

    const input = screen.getByLabelText('WIP limit (empty = unlimited)')
    expect(input).toHaveFocus()

    await fireEvent.input(input, { target: { value: '3' } })
    expect((input as HTMLInputElement).value).toBe('3')

    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(props.onApplyWipLimit).toHaveBeenCalledTimes(1)
    // The input owns Enter — the primitive's menu handler must not see it.
    expect(props.onCloseMenu).not.toHaveBeenCalled()
  })

  it('WIP editor: arrows in the input stay in the input (menu roving does not steal focus)', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 }, wipEditing: true })
    render(ColumnHeader, { props })
    await tick()
    const input = screen.getByLabelText('WIP limit (empty = unlimited)')

    await fireEvent.keyDown(input, { key: 'ArrowDown' })
    await fireEvent.keyDown(input, { key: 'ArrowUp' })
    await fireEvent.keyDown(input, { key: 'Home' })

    expect(input).toHaveFocus()
    expect(props.onCloseMenu).not.toHaveBeenCalled()
  })

  it('WIP editor: Escape closes only the editor, not the whole menu', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 }, wipEditing: true })
    render(ColumnHeader, { props })
    await tick()
    const input = screen.getByLabelText('WIP limit (empty = unlimited)')

    await fireEvent.keyDown(input, { key: 'Escape' })

    expect(props.onWipEscape).toHaveBeenCalledTimes(1)
    expect(props.onCloseMenu).not.toHaveBeenCalled()
    // The menu (and its backdrop) is still rendered.
    expect(screen.getByRole('menu')).toBeInTheDocument()
  })

  it('WIP editor: Apply and Clear buttons keep their actions', async () => {
    const props = baseProps({ menu: { x: 10, y: 10 }, wipEditing: true })
    render(ColumnHeader, { props })
    await tick()

    await fireEvent.click(screen.getByTestId('board-wip-apply-status-TODO'))
    expect(props.onApplyWipLimit).toHaveBeenCalledTimes(1)

    await fireEvent.click(screen.getByTestId('board-wip-clear-status-TODO'))
    expect(props.onClearWipLimit).toHaveBeenCalledTimes(1)
  })

  // --- unchanged header affordances ----------------------------------------

  it('inline rename: Enter commits, Escape cancels (unchanged)', async () => {
    const props = baseProps({ renaming: true })
    render(ColumnHeader, { props })
    await tick()
    const input = screen.getByLabelText('Rename column')

    await fireEvent.keyDown(input, { key: 'Enter' })
    expect(props.onCommitRename).toHaveBeenCalledTimes(1)

    await fireEvent.keyDown(input, { key: 'Escape' })
    expect(props.onCancelRename).toHaveBeenCalledTimes(1)
  })

  it('column drag affordances stay wired (canManage gating)', async () => {
    const props = baseProps()
    const { unmount } = render(ColumnHeader, { props })
    await tick()
    const header = headerEl()
    expect(header).toHaveAttribute('draggable', 'true')

    await fireEvent.dragStart(header)
    expect(props.onColDragStart).toHaveBeenCalledTimes(1)

    unmount()
    const viewOnly = baseProps({ canManage: false })
    render(ColumnHeader, { props: viewOnly })
    await tick()
    expect(headerEl()).not.toHaveAttribute('draggable')
  })
})
