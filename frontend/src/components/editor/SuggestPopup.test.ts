import { fireEvent, render } from '@testing-library/svelte'
import { describe, expect, it, vi } from 'vitest'
import SuggestPopup from './SuggestPopup.svelte'
import { popupCoordsAt } from '../../lib/editor/suggestPopupCoords'

const items = [
  { id: 'due', label: 'due', hint: 'Due date' },
  { id: 'owner', label: 'owner', hint: 'Owner / assignee' }
]

function props(overrides = {}) {
  return {
    items,
    selected: 1,
    coords: { left: 42, top: 84 },
    emptyLabel: 'No matching metadata keys',
    onPick: vi.fn(),
    onHover: vi.fn(),
    ariaLabel: 'Task metadata',
    ...overrides
  }
}

describe('SuggestPopup', () => {
  it('renders a positioned listbox with the selected option as active', () => {
    const { getByRole } = render(SuggestPopup, { props: props() })

    const listbox = getByRole('listbox', { name: 'Task metadata' })
    const options = getByRole('option', { name: /owner/i })
    expect(listbox.getAttribute('style')).toContain('left: 42px')
    expect(listbox.getAttribute('style')).toContain('top: 84px')
    expect(listbox.getAttribute('aria-activedescendant')).toBe(options.id)
    expect(options.getAttribute('aria-selected')).toBe('true')
  })

  it('reports pointer hover and picking by item index', async () => {
    const popupProps = props()
    const { getByRole } = render(SuggestPopup, { props: popupProps })
    const due = getByRole('option', { name: /due date/i })

    await fireEvent.mouseEnter(due)
    await fireEvent.click(due)

    expect(popupProps.onHover).toHaveBeenCalledWith(0)
    expect(popupProps.onPick).toHaveBeenCalledWith(0)
  })

  it('leaves keyboard navigation to the editor host', async () => {
    const popupProps = props()
    const { getByRole } = render(SuggestPopup, { props: popupProps })
    const listbox = getByRole('listbox')
    const activeBefore = listbox.getAttribute('aria-activedescendant')

    await fireEvent.keyDown(listbox, { key: 'ArrowDown' })

    expect(listbox.getAttribute('aria-activedescendant')).toBe(activeBefore)
    expect(popupProps.onHover).not.toHaveBeenCalled()
    expect(popupProps.onPick).not.toHaveBeenCalled()
  })

  it('announces an empty result without exposing a stale active option', () => {
    const { getByRole, getByText, queryAllByRole } = render(SuggestPopup, {
      props: props({ items: [], selected: 0 })
    })

    const empty = getByText('No matching metadata keys')
    expect(empty.getAttribute('aria-live')).toBe('polite')
    expect(getByRole('listbox').hasAttribute('aria-activedescendant')).toBe(
      false
    )
    expect(queryAllByRole('option')).toHaveLength(0)
  })

  it('does not react to outside clicks or Escape', async () => {
    const popupProps = props()
    render(SuggestPopup, { props: popupProps })

    await fireEvent.click(document.body)
    await fireEvent.keyDown(document, { key: 'Escape' })

    expect(popupProps.onPick).not.toHaveBeenCalled()
    expect(popupProps.onHover).not.toHaveBeenCalled()
  })
})

describe('popupCoordsAt', () => {
  it('purely maps coordsAtPos to the popup anchor below the caret', () => {
    const coordsAtPos = vi.fn().mockReturnValue({
      left: 18,
      right: 19,
      top: 30,
      bottom: 46
    })
    const editor = { view: { coordsAtPos } } as never

    expect(popupCoordsAt(editor, 7)).toEqual({ left: 18, top: 46 })
    expect(coordsAtPos).toHaveBeenCalledWith(7)
  })
})
