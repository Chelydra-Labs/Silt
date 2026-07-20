import { fireEvent, render, waitFor } from '@testing-library/svelte'
import { createRawSnippet } from 'svelte'
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
  it('keeps stable option IDs, exposes the active option, and politely announces it', async () => {
    const { getAllByRole, getByRole, rerender } = render(SuggestPopup, {
      props: props()
    })

    const listbox = getByRole('listbox', { name: 'Task metadata' })
    const options = getByRole('option', { name: /owner/i })
    const optionIds = getAllByRole('option').map((option) => option.id)
    const popup = listbox.parentElement
    expect(popup?.getAttribute('style')).toContain('--suggest-popup-left: 42px')
    expect(popup?.getAttribute('style')).toContain('--suggest-popup-top: 84px')
    expect(optionIds.every(Boolean)).toBe(true)
    expect(listbox.getAttribute('aria-activedescendant')).toBe(options.id)
    expect(options.getAttribute('aria-selected')).toBe('true')
    const status = getByRole('status')
    expect(status.getAttribute('aria-live')).toBe('polite')
    expect(status.textContent).toContain('owner, Owner / assignee, 2 of 2')

    await rerender(props({ selected: 0 }))
    expect(getAllByRole('option').map((option) => option.id)).toEqual(optionIds)
    expect(listbox.getAttribute('aria-activedescendant')).toBe(optionIds[0])
    expect(status.textContent).toContain('due, Due date, 1 of 2')
  })

  it('scrolls the newly active option into the visible options area', async () => {
    const scrollIntoView = vi.fn()
    Object.defineProperty(HTMLElement.prototype, 'scrollIntoView', {
      configurable: true,
      value: scrollIntoView
    })
    const { getAllByRole, rerender } = render(SuggestPopup, {
      props: props({ selected: 0 })
    })
    await waitFor(() => expect(scrollIntoView).toHaveBeenCalled())
    scrollIntoView.mockClear()

    await rerender(props({ selected: 1 }))

    const activeOption = getAllByRole('option')[1]
    await waitFor(() =>
      expect(scrollIntoView).toHaveBeenCalledWith({ block: 'nearest' })
    )
    expect(scrollIntoView.mock.instances[0]).toBe(activeOption)
    delete (HTMLElement.prototype as { scrollIntoView?: unknown })
      .scrollIntoView
  })

  it('keeps the footer outside the viewport-constrained options region', () => {
    const footer = createRawSnippet(() => ({
      render: () => '<span>Keyboard help</span>'
    }))
    const { getByRole, getByText } = render(SuggestPopup, {
      props: props({ footer })
    })
    const listbox = getByRole('listbox')
    const popup = listbox.parentElement as HTMLElement
    const footerElement = getByText('Keyboard help').parentElement

    expect(popup.classList).toContain('suggest-popup')
    expect(listbox.classList).toContain('suggest-popup-options')
    expect(footerElement?.classList).toContain('suggest-popup-footer')
    expect(footerElement?.parentElement).toBe(popup)
    expect(listbox.contains(footerElement)).toBe(false)
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
