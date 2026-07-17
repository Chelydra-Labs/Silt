import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import SelectionBubble from './SelectionBubble.svelte'

const coords = { left: 100, top: 100, bottom: 120 }

describe('SelectionBubble', () => {
  it('does not render when selection is empty', () => {
    const { container } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: true,
        selectionCoords: null
      }
    })
    expect(container.querySelector('.selection-bubble')).toBeNull()
  })

  it('does not render when coords are null', () => {
    const { container } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: null
      }
    })
    expect(container.querySelector('.selection-bubble')).toBeNull()
  })

  it('renders when selection is non-empty with coords', () => {
    const { container } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    expect(container.querySelector('.selection-bubble')).toBeTruthy()
  })

  it('renders 7 quick format buttons', () => {
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    for (const label of [
      'Bold',
      'Italic',
      'Strikethrough',
      'Code',
      'Highlight',
      'Underline',
      'Link'
    ]) {
      expect(getByLabelText(label)).toBeTruthy()
    }
  })

  it('reflects active marks via aria-checked', () => {
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(['bold']),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    expect(getByLabelText('Bold').getAttribute('aria-checked')).toBe('true')
    expect(getByLabelText('Italic').getAttribute('aria-checked')).toBe('false')
  })

  it('navigates with ArrowRight and activates with Enter when focused (#643)', async () => {
    const run = vi.fn()
    const toggleMark = vi.fn().mockReturnValue({ run })
    const chain = vi.fn(() => ({
      focus: () => ({ toggleMark, unsetLink: () => ({ run }) })
    }))
    const editor = {
      isActive: vi.fn(() => false),
      chain
    } as any

    const { getByLabelText, getByRole } = render(SelectionBubble, {
      props: {
        editor,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })

    const menu = getByRole('menu', { name: 'Format selection' })
    // Explicit focus (Tab/click) — bubble must not steal focus on show.
    getByLabelText('Bold').focus()
    await fireEvent.keyDown(menu, { key: 'ArrowRight' })
    await fireEvent.keyDown(menu, { key: 'Enter' })
    expect(toggleMark).toHaveBeenCalledWith('italic')
  })

  it('does not auto-focus a bubble button when selection appears', () => {
    const editor = {
      isActive: vi.fn(() => false),
      chain: vi.fn()
    } as any
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    // Focus stays outside the bubble until the user Tabs/clicks in.
    expect(document.activeElement).not.toBe(getByLabelText('Bold'))
  })

  it('Esc returns focus to the editor when menu has focus (#643)', async () => {
    const run = vi.fn()
    const editor = {
      isActive: vi.fn(() => false),
      chain: vi.fn(() => ({
        focus: () => ({ run, toggleMark: () => ({ run }) })
      }))
    } as any

    const { getByLabelText, getByRole } = render(SelectionBubble, {
      props: {
        editor,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    getByLabelText('Bold').focus()
    const menu = getByRole('menu', { name: 'Format selection' })
    await fireEvent.keyDown(menu, { key: 'Escape' })
    expect(run).toHaveBeenCalled()
  })
})
