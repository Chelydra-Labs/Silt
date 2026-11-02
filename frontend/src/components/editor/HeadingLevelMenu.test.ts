import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import HeadingLevelMenu from './HeadingLevelMenu.svelte'

function makeMockEditor(nodeType: string = 'noteBlock', depth: number = 0) {
  const mockNode = { type: { name: nodeType }, attrs: { depth, align: 'left' } }
  return {
    // Editable by default; the #1005 conflict-hold case flips it to false.
    isEditable: true,
    on: vi.fn(),
    off: vi.fn(),
    isActive: vi.fn(() => false),
    state: {
      selection: {
        $from: {
          depth: 1,
          node: () => mockNode
        }
      }
    }
  }
}

describe('HeadingLevelMenu', () => {
  it('renders the trigger button showing current block type', () => {
    const editor = makeMockEditor('noteBlock')
    const { getByRole } = render(HeadingLevelMenu, {
      props: { editor: editor as never }
    })
    const trigger = getByRole('button')
    expect(trigger.textContent).toContain('Note')
  })

  it('shows H1 label for a headerBlock with depth 1', () => {
    const editor = makeMockEditor('headerBlock', 1)
    const { getByRole } = render(HeadingLevelMenu, {
      props: { editor: editor as never }
    })
    const trigger = getByRole('button')
    expect(trigger.textContent).toContain('H1')
  })

  it('opens menu with H1–H6 plus Text and Task (#645)', async () => {
    const editor = makeMockEditor('noteBlock')
    const { getByRole, getAllByRole, getByText } = render(HeadingLevelMenu, {
      props: { editor: editor as never }
    })
    const trigger = getByRole('button')
    await fireEvent.click(trigger)
    const items = getAllByRole('menuitemradio')
    expect(items).toHaveLength(8)
    expect(getByText('Heading 4')).toBeInTheDocument()
    expect(getByText('Heading 6')).toBeInTheDocument()
  })

  it('dispatches silt:change-block-type on selection', async () => {
    const editor = makeMockEditor('noteBlock')
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const { getByRole, getByText } = render(HeadingLevelMenu, {
      props: { editor: editor as never }
    })
    await fireEvent.click(getByRole('button'))
    await fireEvent.click(getByText('Heading 2'))
    const lastCall = dispatchSpy.mock.calls[
      dispatchSpy.mock.calls.length - 1
    ][0] as CustomEvent
    expect(lastCall.type).toBe('silt:change-block-type')
    expect(lastCall.detail.type).toBe('headerBlock')
    expect(lastCall.detail.depth).toBe(2)
    dispatchSpy.mockRestore()
  })

  it('closes the menu on click outside', async () => {
    const editor = makeMockEditor('noteBlock')
    const { getByRole, queryAllByRole } = render(HeadingLevelMenu, {
      props: { editor: editor as never }
    })
    await fireEvent.click(getByRole('button'))
    expect(queryAllByRole('menuitemradio')).toHaveLength(8)

    await fireEvent.click(document.body)

    expect(queryAllByRole('menuitemradio')).toHaveLength(0)
  })
})

// #1005: a conflict-held editor is setEditable(false) — the change-block-type
// receiver refuses the dispatch, and the trigger must show that state instead
// of opening a menu of silent no-ops.
describe('HeadingLevelMenu conflict hold (#1005)', () => {
  it('disables the trigger on a non-editable editor', () => {
    const editor = makeMockEditor('noteBlock')
    editor.isEditable = false
    const { getByRole } = render(HeadingLevelMenu, {
      props: { editor: editor as never }
    })
    expect((getByRole('button') as HTMLButtonElement).disabled).toBe(true)
  })
})
