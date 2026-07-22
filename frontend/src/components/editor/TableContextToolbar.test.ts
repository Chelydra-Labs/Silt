import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'
import TableContextToolbar from './TableContextToolbar.svelte'

const mocks = vi.hoisted(() => ({
  flipOrClamp: vi.fn().mockReturnValue({ left: 48, top: 24 })
}))

vi.mock('../../lib/editor/popoverPositioning', () => ({
  flipOrClamp: mocks.flipOrClamp,
  clampToViewport: vi.fn((r) => ({ left: r.x, top: r.y }))
}))

// Minimal editor mock matching the surface TableContextToolbar uses.
function makeMockEditor(overrides: Record<string, boolean> = {}) {
  const canMap: Record<string, boolean> = {
    addRowBefore: true,
    addRowAfter: true,
    addColumnBefore: true,
    addColumnAfter: true,
    deleteRow: true,
    deleteColumn: true,
    ...overrides
  }
  const handlers: Record<string, Array<() => void>> = {}
  const focusReturnSpy = vi.fn()
  const cellEl = document.createElement('td')
  cellEl.getBoundingClientRect = () =>
    ({
      top: 200,
      bottom: 230,
      left: 100,
      right: 180,
      width: 80,
      height: 30,
      x: 100,
      y: 200,
      toJSON: () => ({})
    }) as DOMRect

  const $from = {
    depth: 3,
    node: (d: number) => {
      if (d === 3) return { type: { name: 'tableCell' } }
      if (d === 2) return { type: { name: 'tableRow' } }
      if (d === 1) return { type: { name: 'table' } }
      return { type: { name: 'doc' } }
    },
    before: (d: number) => d * 10
  }

  return {
    isDestroyed: false,
    on: vi.fn((event: string, handler: () => void) => {
      ;(handlers[event] ||= []).push(handler)
    }),
    off: vi.fn((event: string, handler: () => void) => {
      handlers[event] = (handlers[event] || []).filter((h) => h !== handler)
    }),
    can: () => ({
      addRowBefore: () => canMap.addRowBefore,
      addRowAfter: () => canMap.addRowAfter,
      addColumnBefore: () => canMap.addColumnBefore,
      addColumnAfter: () => canMap.addColumnAfter,
      deleteRow: () => canMap.deleteRow,
      deleteColumn: () => canMap.deleteColumn
    }),
    chain: () => ({
      focus: () => ({
        addRowBefore: () => ({ run: vi.fn() }),
        addRowAfter: () => ({ run: vi.fn() }),
        addColumnBefore: () => ({ run: vi.fn() }),
        addColumnAfter: () => ({ run: vi.fn() }),
        deleteRow: () => ({ run: vi.fn() }),
        deleteColumn: () => ({ run: vi.fn() }),
        run: focusReturnSpy
      })
    }),
    state: {
      selection: { $from }
    },
    view: {
      nodeDOM: vi.fn(() => cellEl)
    },
    _canMap: canMap,
    _cellEl: cellEl,
    _emit(event: string) {
      ;(handlers[event] || []).forEach((h) => h())
    },
    _focusReturnSpy: focusReturnSpy
  }
}

describe('TableContextToolbar', () => {
  beforeEach(() => {
    mocks.flipOrClamp.mockClear()
    mocks.flipOrClamp.mockReturnValue({ left: 48, top: 24 })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('renders six operation buttons with data-tb', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const buttons = container.querySelectorAll('[data-tb]')
    expect(buttons.length).toBe(6)
  })

  it('positions via flipOrClamp near the active cell', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    expect(mocks.flipOrClamp).toHaveBeenCalled()
    const anchor = mocks.flipOrClamp.mock.calls[0][0]
    expect(anchor.top).toBe(200)
    expect(anchor.bottom).toBe(230)
    expect(anchor.left).toBe(100)
    const toolbar = container.querySelector(
      '.table-context-toolbar'
    ) as HTMLElement
    expect(toolbar.style.left).toBe('48px')
    expect(toolbar.style.top).toBe('24px')
  })

  it('presents labelled Rows and Columns groups', async () => {
    const editor = makeMockEditor()
    const { container, getByText } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    expect(getByText('Rows')).toBeTruthy()
    expect(getByText('Columns')).toBeTruthy()
    const groups = container.querySelectorAll('[role="group"]')
    expect(groups.length).toBe(2)
    expect(groups[0].getAttribute('aria-labelledby')).toBe('tct-group-rows')
    expect(groups[1].getAttribute('aria-labelledby')).toBe('tct-group-columns')
  })

  it('shows short text labels so insert direction is not arrow-only', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const texts = Array.from(container.querySelectorAll('.tct-btn-text')).map(
      (el) => el.textContent
    )
    expect(texts).toEqual(
      expect.arrayContaining([
        'Row ↑',
        'Row ↓',
        'Col ←',
        'Col →',
        'Del row',
        'Del col'
      ])
    )
  })

  it('marks delete actions with danger styling and clear accessible names', async () => {
    const editor = makeMockEditor()
    const { getByLabelText } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const delRow = getByLabelText('Delete row') as HTMLButtonElement
    const delCol = getByLabelText('Delete column') as HTMLButtonElement
    expect(delRow.classList.contains('tct-btn-danger')).toBe(true)
    expect(delCol.classList.contains('tct-btn-danger')).toBe(true)
    expect(getByLabelText('Insert row above')).toBeTruthy()
    expect(getByLabelText('Insert column left')).toBeTruthy()
  })

  it('moves focus forward on Arrow Right', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const toolbar = container.querySelector('[role="toolbar"]')!
    const buttons = toolbar.querySelectorAll<HTMLButtonElement>('[data-tb]')
    fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
    await tick()
    expect(document.activeElement).toBe(buttons[1])
  })

  it('skips a disabled button on Arrow Right', async () => {
    const editor = makeMockEditor({ deleteRow: false })
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const toolbar = container.querySelector('[role="toolbar"]')!
    const buttons = toolbar.querySelectorAll<HTMLButtonElement>('[data-tb]')
    // row-above(0), row-below(1), del-row(2 disabled), col-left(3)...
    // From index 1, Arrow Right should skip del-row → col-left.
    fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
    await tick()
    expect(document.activeElement).toBe(buttons[1])
    fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
    await tick()
    expect(document.activeElement).toBe(buttons[3])
  })

  it('moves focus to last button on End', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const toolbar = container.querySelector('[role="toolbar"]')!
    const buttons = toolbar.querySelectorAll<HTMLButtonElement>('[data-tb]')
    fireEvent.keyDown(toolbar, { key: 'End' })
    await tick()
    expect(document.activeElement).toBe(buttons[5])
  })

  it('returns focus to editor on Escape', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const toolbar = container.querySelector('[role="toolbar"]')!
    fireEvent.keyDown(toolbar, { key: 'Escape' })
    expect(editor._focusReturnSpy).toHaveBeenCalled()
  })

  it('re-clamps tabindex off a button that becomes disabled', async () => {
    const editor = makeMockEditor()
    const { container } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    const toolbar = container.querySelector('[role="toolbar"]')!
    const buttons = toolbar.querySelectorAll<HTMLButtonElement>('[data-tb]')
    // Navigate to index 2 (del-row).
    for (let k = 0; k < 2; k++) {
      fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
      await tick()
    }
    expect(buttons[2].tabIndex).toBe(0)
    editor._canMap.deleteRow = false
    editor._emit('selectionUpdate')
    await tick()
    expect(buttons[2].tabIndex).toBe(-1)
    expect(buttons[3].tabIndex).toBe(0)
  })

  it('disables delete row when can() returns false', async () => {
    const editor = makeMockEditor({ deleteRow: false })
    const { getByLabelText } = render(TableContextToolbar, {
      props: { editor: editor as never }
    })
    await tick()
    expect((getByLabelText('Delete row') as HTMLButtonElement).disabled).toBe(
      true
    )
  })
})
