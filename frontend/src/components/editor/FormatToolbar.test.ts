import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'
import FormatToolbar from './FormatToolbar.svelte'

// Mock editor with the minimal interface FormatToolbar uses.
function makeMockEditor(opts: { empty?: boolean; canMark?: boolean } = {}) {
  const marks = new Set<string>()
  const mockNode = {
    type: { name: 'noteBlock' },
    attrs: { depth: 0, align: 'left' }
  }
  const canMark = opts.canMark !== false
  return {
    isActive: vi.fn((mark: string) => marks.has(mark)),
    can: () => ({
      toggleMark: (m: string) => canMark && m !== '__never__'
    }),
    on: vi.fn(),
    off: vi.fn(),
    chain: () => ({
      focus: () => ({
        toggleMark: () => ({ run: () => {} }),
        unsetLink: () => ({ run: () => {} }),
        unsetAllMarks: () => ({ run: () => {} }),
        run: () => {}
      }),
      toggleMark: () => ({ run: () => {} }),
      unsetLink: () => ({ run: () => {} }),
      unsetAllMarks: () => ({ run: () => {} })
    }),
    state: {
      selection: {
        empty: opts.empty ?? false,
        $from: { depth: 1, node: () => mockNode }
      }
    },
    view: {
      dom: document.createElement('div'),
      coordsAtPos: () => ({ left: 10, top: 10, bottom: 20 })
    },
    _marks: marks
  }
}

const baseProps = {
  activeMarks: new Set<string>(),
  isDark: true,
  colorEnabled: true
}

describe('FormatToolbar', () => {
  beforeEach(() => {
    vi.restoreAllMocks()
  })

  it('keeps block style, Bold, Italic, Link, and Inline code directly available', () => {
    const editor = makeMockEditor() as any
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    expect(getByLabelText('Block type')).toBeTruthy()
    expect(getByLabelText('Bold')).toBeTruthy()
    expect(getByLabelText('Italic')).toBeTruthy()
    expect(getByLabelText('Insert link')).toBeTruthy()
    expect(getByLabelText('Inline code')).toBeTruthy()
    // Direct primary buttons carry data-primary.
    expect(getByLabelText('Bold').hasAttribute('data-primary')).toBe(true)
    expect(getByLabelText('Inline code').hasAttribute('data-primary')).toBe(
      true
    )
  })

  it('places advanced marks under the More formatting menu', async () => {
    const editor = makeMockEditor() as any
    const { getByLabelText, queryByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    // Not top-level.
    expect(queryByLabelText('Underline')).toBeNull()
    expect(queryByLabelText('Strikethrough')).toBeNull()
    expect(queryByLabelText('Highlight')).toBeNull()
    expect(queryByLabelText('Subscript')).toBeNull()
    expect(queryByLabelText('Superscript')).toBeNull()

    await fireEvent.click(getByLabelText('More formatting'))
    expect(getByLabelText('Underline')).toBeTruthy()
    expect(getByLabelText('Strikethrough')).toBeTruthy()
    expect(getByLabelText('Highlight')).toBeTruthy()
    expect(getByLabelText('Subscript')).toBeTruthy()
    expect(getByLabelText('Superscript')).toBeTruthy()
    expect(getByLabelText('Check spelling')).toBeTruthy()
  })

  it('places alignment and insert actions in labelled menus', async () => {
    const editor = makeMockEditor() as any
    const { getByLabelText, queryByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    expect(queryByLabelText('Align center')).toBeNull()
    expect(queryByLabelText('Callout')).toBeNull()

    await fireEvent.click(getByLabelText('Alignment'))
    expect(getByLabelText('Align left')).toBeTruthy()
    expect(getByLabelText('Align center')).toBeTruthy()
    expect(getByLabelText('Align right')).toBeTruthy()
    expect(getByLabelText('Align justify')).toBeTruthy()

    await fireEvent.click(getByLabelText('Insert'))
    expect(getByLabelText('Quote')).toBeTruthy()
    expect(getByLabelText('Code block')).toBeTruthy()
    expect(getByLabelText('Callout')).toBeTruthy()
    expect(getByLabelText('Foldable section')).toBeTruthy()
    expect(getByLabelText('Table')).toBeTruthy()
  })

  it('renders link and clear-formatting buttons', () => {
    const editor = makeMockEditor() as any
    const { getByLabelText, getAllByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    expect(getByLabelText('Insert link')).toBeTruthy()
    // Clear is both top-level and under More once More is open; top-level always present.
    expect(getAllByLabelText('Clear formatting').length).toBeGreaterThanOrEqual(
      1
    )
  })

  it('hides color pickers when colorEnabled is false', () => {
    const editor = makeMockEditor() as any
    const { queryByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps, colorEnabled: false }
    })
    expect(queryByLabelText('Text color')).toBeNull()
    expect(queryByLabelText('Background color')).toBeNull()
  })

  it('reflects aria-pressed for active marks', () => {
    const editor = makeMockEditor() as any
    const { getByLabelText } = render(FormatToolbar, {
      props: {
        editor,
        ...baseProps,
        activeMarks: new Set<string>(['bold'])
      }
    })
    const boldBtn = getByLabelText('Bold') as HTMLButtonElement
    expect(boldBtn.getAttribute('aria-pressed')).toBe('true')
  })

  it('has role=toolbar with tabindex for keyboard navigation', () => {
    const editor = makeMockEditor() as any
    const { getByRole } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    const toolbar = getByRole('toolbar')
    expect(toolbar).toBeTruthy()
    expect(toolbar.getAttribute('tabindex')).toBe('-1')
  })

  it('includes Heading and Color triggers in the unified data-tb focus set', () => {
    const editor = makeMockEditor() as any
    const { container, getByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    const tbs = container.querySelectorAll('[data-tb]')
    expect(tbs.length).toBeGreaterThanOrEqual(8)
    expect(getByLabelText('Block type').hasAttribute('data-tb')).toBe(true)
    expect(getByLabelText('Text color').hasAttribute('data-tb')).toBe(true)
    expect(getByLabelText('Background color').hasAttribute('data-tb')).toBe(
      true
    )
  })

  it('dispatches silt:set-block-align on alignment click', async () => {
    const editor = makeMockEditor() as any
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    await fireEvent.click(getByLabelText('Alignment'))
    await fireEvent.click(getByLabelText('Align center'))
    const lastCall = dispatchSpy.mock.calls[
      dispatchSpy.mock.calls.length - 1
    ][0] as CustomEvent
    expect(lastCall.type).toBe('silt:set-block-align')
    expect(lastCall.detail).toBe('center')
    dispatchSpy.mockRestore()
  })

  it('disables link when selection is empty and not in a link', () => {
    const editor = makeMockEditor({ empty: true }) as any
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    expect((getByLabelText('Insert link') as HTMLButtonElement).disabled).toBe(
      true
    )
  })

  it('disables marks when TipTap can() returns false', () => {
    const editor = makeMockEditor({ canMark: false }) as any
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    expect((getByLabelText('Bold') as HTMLButtonElement).disabled).toBe(true)
    expect((getByLabelText('Italic') as HTMLButtonElement).disabled).toBe(true)
  })

  it('disables marks when TipTap can() throws (fail closed)', () => {
    const editor = makeMockEditor() as any
    editor.can = () => {
      throw new Error('can boom')
    }
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    expect((getByLabelText('Bold') as HTMLButtonElement).disabled).toBe(true)
  })

  it('Esc closes an open overflow menu before returning to the editor', async () => {
    const editor = makeMockEditor() as any
    const { getByLabelText, getByRole, queryByLabelText } = render(
      FormatToolbar,
      {
        props: { editor, ...baseProps }
      }
    )
    await fireEvent.click(getByLabelText('More formatting'))
    expect(getByLabelText('Highlight')).toBeTruthy()
    const toolbar = getByRole('toolbar')
    await fireEvent.keyDown(toolbar, { key: 'Escape' })
    await tick()
    expect(queryByLabelText('Highlight')).toBeNull()
  })

  it('second Esc after menu close returns focus to the editor', async () => {
    const editor = makeMockEditor() as any
    const focusRun = vi.fn()
    editor.chain = vi.fn(() => ({
      focus: () => ({
        toggleMark: () => ({ run: () => {} }),
        unsetLink: () => ({ run: () => {} }),
        unsetAllMarks: () => ({ run: () => {} }),
        run: focusRun
      }),
      toggleMark: () => ({ run: () => {} }),
      unsetLink: () => ({ run: () => {} }),
      unsetAllMarks: () => ({ run: () => {} })
    }))
    const { getByLabelText, getByRole } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    await fireEvent.click(getByLabelText('More formatting'))
    const toolbar = getByRole('toolbar')
    await fireEvent.keyDown(toolbar, { key: 'Escape' })
    await tick()
    await fireEvent.keyDown(toolbar, { key: 'Escape' })
    await tick()
    expect(editor.chain).toHaveBeenCalled()
    expect(focusRun).toHaveBeenCalled()
  })

  it('ArrowRight moves focus across top-level toolbar controls', async () => {
    const editor = makeMockEditor() as any
    const { container, getByRole } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    const toolbar = getByRole('toolbar')
    const buttons = container.querySelectorAll<HTMLButtonElement>('[data-tb]')
    buttons[0].focus()
    await fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
    await tick()
    expect(document.activeElement).toBe(buttons[1])
  })

  it('does not force horizontal overflow classes on the toolbar root', () => {
    const editor = makeMockEditor() as any
    const { getByRole } = render(FormatToolbar, {
      props: { editor, ...baseProps }
    })
    const toolbar = getByRole('toolbar') as HTMLElement
    // Contract for ≤600px: wrap + visible overflow, never a forced h-scroll rail.
    expect(toolbar.className).toContain('format-toolbar')
    const style = toolbar.getAttribute('style') || ''
    expect(style).not.toMatch(/overflow-x:\s*scroll/)
  })
})
