import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'
import SelectionBubble from './SelectionBubble.svelte'

const mocks = vi.hoisted(() => ({
  flipOrClamp: vi.fn().mockReturnValue({ left: 80, top: 60 }),
  openURL: vi.fn()
}))

vi.mock('../../lib/editor/popoverPositioning', () => ({
  flipOrClamp: mocks.flipOrClamp,
  clampToViewport: vi.fn((r) => ({ left: r.x, top: r.y }))
}))

vi.mock('@wailsio/runtime', () => ({
  Browser: { OpenURL: mocks.openURL }
}))

const coords = { left: 100, top: 100, bottom: 120 }

function makeEditor(
  opts: {
    linkActive?: boolean
    href?: string
  } = {}
) {
  const run = vi.fn()
  const toggleMark = vi.fn().mockReturnValue({ run })
  const unsetLink = vi.fn().mockReturnValue({ run })
  const chain = vi.fn(() => ({
    focus: () => ({ toggleMark, unsetLink, run })
  }))
  return {
    isActive: vi.fn((mark: string) =>
      mark === 'link' ? !!opts.linkActive : false
    ),
    getAttributes: vi.fn(() => ({ href: opts.href ?? '' })),
    chain,
    _run: run,
    _toggleMark: toggleMark,
    _unsetLink: unsetLink
  }
}

describe('SelectionBubble', () => {
  beforeEach(() => {
    mocks.flipOrClamp.mockClear()
    mocks.flipOrClamp.mockReturnValue({ left: 80, top: 60 })
    mocks.openURL.mockClear()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

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

  it('exposes primary Bold, Italic, Underline, Link, Inline code and a More control', () => {
    const { getByLabelText, queryByLabelText } = render(SelectionBubble, {
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
      'Underline',
      'Link',
      'Inline code'
    ]) {
      expect(getByLabelText(label)).toBeTruthy()
    }
    expect(getByLabelText('More formatting')).toBeTruthy()
    // Lower-frequency marks are not top-level.
    expect(queryByLabelText('Strikethrough')).toBeNull()
    expect(queryByLabelText('Highlight')).toBeNull()
  })

  it('reveals lower-frequency marks under More', async () => {
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await fireEvent.click(getByLabelText('More formatting'))
    expect(getByLabelText('Strikethrough')).toBeTruthy()
    expect(getByLabelText('Highlight')).toBeTruthy()
  })

  it('uses toolbar + aria-pressed toggle semantics', () => {
    const { getByRole, getByLabelText } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(['bold']),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    expect(getByRole('toolbar', { name: 'Format selection' })).toBeTruthy()
    expect(getByLabelText('Bold').getAttribute('aria-pressed')).toBe('true')
    expect(getByLabelText('Italic').getAttribute('aria-pressed')).toBe('false')
  })

  it('uses bubble-btn hit targets sized for touch (≥32×32 in CSS)', () => {
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    // jsdom has no layout; contract is the shared .bubble-btn class with
    // min-width/min-height 32px in the component stylesheet.
    expect(getByLabelText('Bold').classList.contains('bubble-btn')).toBe(true)
    expect(getByLabelText('Link').classList.contains('bubble-btn')).toBe(true)
    expect(
      getByLabelText('More formatting').classList.contains('bubble-btn')
    ).toBe(true)
  })

  it('positions via flipOrClamp so the bubble stays in viewport', async () => {
    const { container } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await tick()
    expect(mocks.flipOrClamp).toHaveBeenCalled()
    const bubble = container.querySelector('.selection-bubble') as HTMLElement
    expect(bubble.style.left).toBe('80px')
    expect(bubble.style.top).toBe('60px')
  })

  it('dismisses on scroll (#594)', async () => {
    const { container } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await tick()
    expect(container.querySelector('.selection-bubble')).toBeTruthy()
    document.dispatchEvent(new Event('scroll', { bubbles: true }))
    await tick()
    expect(container.querySelector('.selection-bubble')).toBeNull()
  })

  it('dismisses on resize', async () => {
    const { container } = render(SelectionBubble, {
      props: {
        editor: null,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await tick()
    window.dispatchEvent(new Event('resize'))
    await tick()
    expect(container.querySelector('.selection-bubble')).toBeNull()
  })

  it('opens link input for a new link without unsetting', async () => {
    const editor = makeEditor({ linkActive: false })
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await fireEvent.click(getByLabelText('Link'))
    expect(editor._unsetLink).not.toHaveBeenCalled()
    const evt = dispatchSpy.mock.calls
      .map((c) => c[0] as Event)
      .find((e) => e.type === 'silt:open-link-input') as CustomEvent
    expect(evt).toBeTruthy()
    dispatchSpy.mockRestore()
  })

  it('shows Edit/Open/Copy/Remove for an existing link instead of unsetting', async () => {
    const editor = makeEditor({
      linkActive: true,
      href: 'https://example.com'
    })
    const { getByLabelText, queryByLabelText } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(['link']),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await fireEvent.click(getByLabelText('Link'))
    expect(editor._unsetLink).not.toHaveBeenCalled()
    expect(getByLabelText('Edit link')).toBeTruthy()
    expect(getByLabelText('Open link')).toBeTruthy()
    expect(getByLabelText('Copy link')).toBeTruthy()
    expect(getByLabelText('Remove link')).toBeTruthy()
    expect(queryByLabelText('Edit link')?.textContent).toContain('Edit')
  })

  it('Edit dispatches open-link-input with the current href', async () => {
    const editor = makeEditor({
      linkActive: true,
      href: 'https://example.com/path'
    })
    const dispatchSpy = vi.spyOn(window, 'dispatchEvent')
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(['link']),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await fireEvent.click(getByLabelText('Link'))
    await fireEvent.click(getByLabelText('Edit link'))
    const evt = dispatchSpy.mock.calls
      .map((c) => c[0] as CustomEvent)
      .find((e) => e.type === 'silt:open-link-input')
    expect(evt?.detail?.href).toBe('https://example.com/path')
    dispatchSpy.mockRestore()
  })

  it('Open uses Browser.OpenURL with the current href', async () => {
    const editor = makeEditor({
      linkActive: true,
      href: 'https://open.me'
    })
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(['link']),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await fireEvent.click(getByLabelText('Link'))
    await fireEvent.click(getByLabelText('Open link'))
    expect(mocks.openURL).toHaveBeenCalledWith('https://open.me')
  })

  it('Remove unsets the link only when chosen explicitly', async () => {
    const editor = makeEditor({
      linkActive: true,
      href: 'https://example.com'
    })
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(['link']),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    await fireEvent.click(getByLabelText('Link'))
    expect(editor._unsetLink).not.toHaveBeenCalled()
    await fireEvent.click(getByLabelText('Remove link'))
    expect(editor._unsetLink).toHaveBeenCalled()
  })

  it('navigates with ArrowRight and activates with Enter when focused (#643)', async () => {
    const editor = makeEditor()
    const { getByLabelText, getByRole } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })

    const toolbar = getByRole('toolbar', { name: 'Format selection' })
    getByLabelText('Bold').focus()
    await fireEvent.keyDown(toolbar, { key: 'ArrowRight' })
    await fireEvent.keyDown(toolbar, { key: 'Enter' })
    expect(editor._toggleMark).toHaveBeenCalledWith('italic')
  })

  it('does not auto-focus a bubble button when selection appears', () => {
    const editor = makeEditor()
    const { getByLabelText } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    expect(document.activeElement).not.toBe(getByLabelText('Bold'))
  })

  it('Esc returns focus to the editor when toolbar has focus (#643)', async () => {
    const editor = makeEditor()
    const { getByLabelText, getByRole } = render(SelectionBubble, {
      props: {
        editor: editor as never,
        activeMarks: new Set<string>(),
        selectionEmpty: false,
        selectionCoords: coords
      }
    })
    getByLabelText('Bold').focus()
    const toolbar = getByRole('toolbar', { name: 'Format selection' })
    await fireEvent.keyDown(toolbar, { key: 'Escape' })
    expect(editor._run).toHaveBeenCalled()
  })

  it('Esc closes the More menu before returning to the editor', async () => {
    const editor = makeEditor()
    const { getByLabelText, getByRole, queryByLabelText } = render(
      SelectionBubble,
      {
        props: {
          editor: editor as never,
          activeMarks: new Set<string>(),
          selectionEmpty: false,
          selectionCoords: coords
        }
      }
    )
    await fireEvent.click(getByLabelText('More formatting'))
    expect(getByLabelText('Highlight')).toBeTruthy()
    const toolbar = getByRole('toolbar', { name: 'Format selection' })
    await fireEvent.keyDown(toolbar, { key: 'Escape' })
    expect(queryByLabelText('Highlight')).toBeNull()
    expect(editor._run).not.toHaveBeenCalled()
  })
})
