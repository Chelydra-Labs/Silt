import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'
import FormatToolbar from './FormatToolbar.svelte'

// Mock editor with the minimal interface FormatToolbar uses.
function makeMockEditor(
  opts: {
    empty?: boolean
    canMark?: boolean
    bullet?: string
    blockType?: string
  } = {}
) {
  const marks = new Set<string>()
  const blockName = opts.blockType ?? 'noteBlock'
  const mockNode = {
    type: { name: blockName },
    attrs: { depth: 0, align: 'left', bullet: opts.bullet ?? '' }
  }
  const canMark = opts.canMark !== false
  const empty = opts.empty ?? false
  return {
    isDestroyed: false,
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
        empty,
        from: 1,
        to: empty ? 1 : 5,
        $from: {
          depth: 1,
          node: (d?: number) => (d === undefined || d >= 1 ? mockNode : null),
          before: () => 0
        }
      },
      doc: {
        nodeAt: (pos: number) => (pos === 0 ? mockNode : null),
        nodesBetween: (
          _from: number,
          _to: number,
          f: (node: typeof mockNode, pos: number) => boolean | void
        ) => {
          f(mockNode, 0)
        }
      },
      tr: {
        setNodeMarkup: vi.fn(function (this: { docChanged?: boolean }) {
          this.docChanged = true
          return this
        }),
        docChanged: false,
        doc: {
          nodeAt: (pos: number) => (pos === 0 ? mockNode : null)
        }
      }
    },
    view: {
      dom: document.createElement('div'),
      coordsAtPos: () => ({ left: 10, top: 10, bottom: 20 }),
      dispatch: vi.fn()
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

  it('keeps block style, lists, Bold, Italic, Underline, Link, and Inline code directly available', () => {
    const editor = makeMockEditor()
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    expect(getByLabelText('Block type')).toBeTruthy()
    expect(getByLabelText('Bullet list')).toBeTruthy()
    expect(getByLabelText('Numbered list')).toBeTruthy()
    expect(getByLabelText('Bold')).toBeTruthy()
    expect(getByLabelText('Italic')).toBeTruthy()
    expect(getByLabelText('Underline')).toBeTruthy()
    expect(getByLabelText('Insert link')).toBeTruthy()
    expect(getByLabelText('Inline code')).toBeTruthy()
    // Direct primary buttons carry data-primary.
    expect(getByLabelText('Bold').hasAttribute('data-primary')).toBe(true)
    expect(getByLabelText('Bullet list').hasAttribute('data-primary')).toBe(
      true
    )
    expect(getByLabelText('Inline code').hasAttribute('data-primary')).toBe(
      true
    )
  })

  it('enables list controls when caret is in a noteBlock', () => {
    const editor = makeMockEditor({ empty: true })
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    expect((getByLabelText('Bullet list') as HTMLButtonElement).disabled).toBe(
      false
    )
    expect(
      (getByLabelText('Numbered list') as HTMLButtonElement).disabled
    ).toBe(false)
  })

  it('disables list controls when caret is not in a noteBlock', () => {
    const editor = makeMockEditor({ empty: true, blockType: 'headerBlock' })
    editor.state.doc.nodeAt = () => null
    editor.state.doc.nodesBetween = () => {}
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    expect((getByLabelText('Bullet list') as HTMLButtonElement).disabled).toBe(
      true
    )
    expect(
      (getByLabelText('Numbered list') as HTMLButtonElement).disabled
    ).toBe(true)
  })

  it('marks list buttons pressed when selection is already a list', async () => {
    const editor = makeMockEditor({ empty: true, bullet: '- ' })
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    await tick()
    expect(getByLabelText('Bullet list').getAttribute('aria-pressed')).toBe(
      'true'
    )
    expect(getByLabelText('Numbered list').getAttribute('aria-pressed')).toBe(
      'false'
    )
  })

  it('keeps list controls in the Paragraph group with Alignment', () => {
    const editor = makeMockEditor()
    const { getByLabelText, container } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    const paragraph = container.querySelector('[aria-label="Paragraph"]')
    expect(paragraph).toBeTruthy()
    expect(paragraph?.contains(getByLabelText('Bullet list'))).toBe(true)
    expect(paragraph?.contains(getByLabelText('Numbered list'))).toBe(true)
    expect(paragraph?.contains(getByLabelText('Alignment'))).toBe(true)
  })

  it('places advanced marks under the More formatting menu', async () => {
    const editor = makeMockEditor()
    const { getByLabelText, queryByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    // Not top-level.
    expect(queryByLabelText('Strikethrough')).toBeNull()
    expect(queryByLabelText('Highlight')).toBeNull()
    expect(queryByLabelText('Subscript')).toBeNull()
    expect(queryByLabelText('Superscript')).toBeNull()

    await fireEvent.click(getByLabelText('More formatting'))
    expect(getByLabelText('Strikethrough')).toBeTruthy()
    expect(getByLabelText('Highlight')).toBeTruthy()
    expect(getByLabelText('Subscript')).toBeTruthy()
    expect(getByLabelText('Superscript')).toBeTruthy()
    expect(getByLabelText('Check spelling')).toBeTruthy()
  })

  it('places alignment and insert actions in labelled menus', async () => {
    const editor = makeMockEditor()
    const { getByLabelText, queryByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
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

  it('renders link and a single clear-formatting button', () => {
    const editor = makeMockEditor()
    const { getByLabelText, getAllByLabelText, queryByLabelText } = render(
      FormatToolbar,
      {
        props: { editor: editor as never, ...baseProps }
      }
    )
    expect(getByLabelText('Insert link')).toBeTruthy()
    // Clear stays top-level only (not duplicated under More).
    expect(getAllByLabelText('Clear formatting')).toHaveLength(1)
    // Open More — clear must not appear there.
    void fireEvent.click(getByLabelText('More formatting'))
    expect(queryByLabelText('Check spelling')).toBeTruthy()
    expect(getAllByLabelText('Clear formatting')).toHaveLength(1)
  })

  it('hides color pickers when colorEnabled is false', () => {
    const editor = makeMockEditor()
    const { queryByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps, colorEnabled: false }
    })
    expect(queryByLabelText('Text color')).toBeNull()
    expect(queryByLabelText('Background color')).toBeNull()
  })

  it('reflects aria-pressed for active marks', () => {
    const editor = makeMockEditor()
    const { getByLabelText } = render(FormatToolbar, {
      props: {
        editor: editor as never,
        ...baseProps,
        activeMarks: new Set<string>(['bold'])
      }
    })
    const boldBtn = getByLabelText('Bold') as HTMLButtonElement
    expect(boldBtn.getAttribute('aria-pressed')).toBe('true')
  })

  it('has role=toolbar with tabindex for keyboard navigation', () => {
    const editor = makeMockEditor()
    const { getByRole } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    const toolbar = getByRole('toolbar')
    expect(toolbar).toBeTruthy()
    expect(toolbar.getAttribute('tabindex')).toBe('-1')
  })

  it('includes Heading and Color triggers in the unified data-tb focus set', () => {
    const editor = makeMockEditor()
    const { container, getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
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
    const editor = makeMockEditor()
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
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
    const editor = makeMockEditor({ empty: true })
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    expect((getByLabelText('Insert link') as HTMLButtonElement).disabled).toBe(
      true
    )
  })

  it('disables marks when TipTap can() returns false', () => {
    const editor = makeMockEditor({ canMark: false })
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    expect((getByLabelText('Bold') as HTMLButtonElement).disabled).toBe(true)
    expect((getByLabelText('Italic') as HTMLButtonElement).disabled).toBe(true)
  })

  it('disables marks when TipTap can() throws (fail closed)', () => {
    const editor = makeMockEditor()
    editor.can = () => {
      throw new Error('can boom')
    }
    const { getByLabelText } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    expect((getByLabelText('Bold') as HTMLButtonElement).disabled).toBe(true)
  })

  it('Esc closes an open overflow menu before returning to the editor', async () => {
    const editor = makeMockEditor()
    const { getByLabelText, getByRole, queryByLabelText } = render(
      FormatToolbar,
      {
        props: { editor: editor as never, ...baseProps }
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
    const editor = makeMockEditor()
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
      props: { editor: editor as never, ...baseProps }
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
    const editor = makeMockEditor()
    const { container, getByRole } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    const toolbar = getByRole('toolbar')
    const buttons = container.querySelectorAll<HTMLButtonElement>('[data-tb]')
    buttons[0].focus()
    await fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
    await tick()
    expect(document.activeElement).toBe(buttons[1])
  })

  it('ArrowDown roves focus inside an open overflow .toolbar-menu', async () => {
    const editor = makeMockEditor()
    const { getByLabelText, getByRole } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    await fireEvent.click(getByLabelText('More formatting'))
    await tick()
    const menu = getByRole('menu', { name: 'More formatting' })
    // menuitemcheckbox / menuitem / menuitemradio all participate in roving.
    const items = menu.querySelectorAll<HTMLButtonElement>('[role^="menuitem"]')
    expect(items.length).toBeGreaterThan(1)
    items[0].focus()
    const toolbar = getByRole('toolbar')
    await fireEvent.keyDown(toolbar, { key: 'ArrowDown' })
    await tick()
    expect(document.activeElement).toBe(items[1])
  })

  it('does not force horizontal overflow classes on the toolbar root', () => {
    const editor = makeMockEditor()
    const { getByRole } = render(FormatToolbar, {
      props: { editor: editor as never, ...baseProps }
    })
    const toolbar = getByRole('toolbar')
    // Contract for ≤600px: wrap + visible overflow, never a forced h-scroll rail.
    expect(toolbar.className).toContain('format-toolbar')
    const style = toolbar.getAttribute('style') || ''
    expect(style).not.toMatch(/overflow-x:\s*scroll/)
  })
})
