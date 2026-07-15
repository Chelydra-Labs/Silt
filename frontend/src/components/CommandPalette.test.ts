import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import CommandPalette from './CommandPalette.svelte'

// Helper: flush Svelte 5 $effect runs after a render / event so assertions on
// derived DOM (e.g. aria-activedescendant projection) are deterministic.
const flush = () => new Promise((r) => setTimeout(r, 0))

describe('CommandPalette', () => {
  it('renders commands matching the query', () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    const { queryByText } = render(CommandPalette, {
      props: { onSelect, onClose, query: 'Heading 1' }
    })

    expect(queryByText('Heading 1')).toBeTruthy()
    expect(queryByText('Italic')).toBeNull()
  })

  it('navigates with keyboard and selects a command', async () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    render(CommandPalette, {
      props: { onSelect, onClose, query: 'Heading 1' }
    })

    await fireEvent.keyDown(window, { key: 'Enter' })
    expect(onSelect).toHaveBeenCalledWith('h1')
  })

  // #585: after arrowing down, typing (narrowing the query) must reset the
  // active option to the new top-ranked match — not leave the highlight on a
  // stale index that Enter would then execute.
  it('resets the active option to the top match when the query changes', async () => {
    const { container, rerender } = render(CommandPalette, {
      props: { onSelect: vi.fn(), onClose: vi.fn(), query: 'h' }
    })
    await flush()

    // Several labels start with 'h' (Heading 1/2/3, Highlight). Move off top.
    await fireEvent.keyDown(window, { key: 'ArrowDown' })
    await flush()
    const activeAfterArrow = container.querySelector(
      'button[role="option"][aria-selected="true"]'
    ) as HTMLElement
    expect(activeAfterArrow.id).not.toBe('silt-slash-palette-opt-0')

    // Narrowing the query resets the active option to the top-ranked match.
    await rerender({ onSelect: vi.fn(), onClose: vi.fn(), query: 'heading' })
    await flush()
    const activeAfterQuery = container.querySelector(
      'button[role="option"][aria-selected="true"]'
    ) as HTMLElement
    expect(activeAfterQuery.id).toBe('silt-slash-palette-opt-0')
  })

  it('closes on Escape key press', async () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    render(CommandPalette, {
      props: { onSelect, onClose }
    })

    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalled()
  })

  it('shows no matching commands when query matches nothing', () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    const { container } = render(CommandPalette, {
      props: { onSelect, onClose, query: 'nonexistentcommand' }
    })

    // The visible empty state lives inside the listbox (the separate live
    // region echoes it for screen readers).
    const listbox = container.querySelector('[role="listbox"]')
    expect(listbox?.textContent ?? '').toContain('No matching commands')
  })

  it('includes label matches and gates short-query description matches', () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    const { container } = render(CommandPalette, {
      props: { onSelect, onClose, query: 'h' }
    })

    const buttons = container.querySelectorAll('button[role="option"]')
    const labels = Array.from(buttons).map((btn) => {
      const span = btn.querySelector('.font-label-sm-bold')
      return span ? span.textContent : ''
    })

    // "Heading 1" starts with "h" → label prefix match, included.
    expect(labels).toContain('Heading 1')
    // 'h' is a single character, below the description-match gate, so a
    // command that would match only by description (Italic — "the
    // selection") is excluded rather than swamping the list (#585).
    expect(labels).not.toContain('Italic')
  })

  // --- #584: listbox semantics ------------------------------------------------

  it('exposes listbox + option roles with a stable palette id', () => {
    const { container } = render(CommandPalette, {
      props: { onSelect: vi.fn(), onClose: vi.fn() }
    })
    const listbox = container.querySelector('[role="listbox"]')
    expect(listbox).toBeTruthy()
    expect(listbox?.id).toBe('silt-slash-palette')
    expect(listbox?.getAttribute('aria-label')).toBe('Slash commands')

    const options = container.querySelectorAll('button[role="option"]')
    expect(options.length).toBeGreaterThan(0)
    // Exactly the active option carries aria-selected="true".
    const selected = container.querySelectorAll(
      'button[role="option"][aria-selected="true"]'
    )
    expect(selected.length).toBe(1)
    expect(selected[0].id).toBe('silt-slash-palette-opt-0')
  })

  it('projects listbox control onto the editor textbox while open', async () => {
    const textbox = document.createElement('div')
    render(CommandPalette, {
      props: { onSelect: vi.fn(), onClose: vi.fn(), textboxEl: textbox }
    })
    await flush()

    expect(textbox.getAttribute('aria-controls')).toBe('silt-slash-palette')
    expect(textbox.getAttribute('aria-expanded')).toBe('true')
    expect(textbox.getAttribute('aria-autocomplete')).toBe('list')
    // The active option (index 0) is referenced.
    expect(textbox.getAttribute('aria-activedescendant')).toBe(
      'silt-slash-palette-opt-0'
    )
  })

  it('announces the match count via a polite status live region', () => {
    const { container } = render(CommandPalette, {
      props: { onSelect: vi.fn(), onClose: vi.fn(), query: 'Heading 1' }
    })
    const status = container.querySelector('[role="status"]')
    expect(status).toBeTruthy()
    expect(status?.getAttribute('aria-live')).toBe('polite')
    expect(status?.textContent ?? '').toMatch(/matching command/)
  })

  it('consumes Enter when there are no matches (no fallthrough to editor)', () => {
    const onSelect = vi.fn()
    render(CommandPalette, {
      props: { onSelect, onClose: vi.fn(), query: 'nonexistentcommand' }
    })

    const event = new KeyboardEvent('keydown', {
      key: 'Enter',
      bubbles: true,
      cancelable: true
    })
    window.dispatchEvent(event)

    expect(event.defaultPrevented).toBe(true)
    expect(onSelect).not.toHaveBeenCalled()
  })

  // --- Click-outside guard contract (#584 decoupling) -------------------------
  // The guard keys off the dedicated data-slash-palette marker, NOT the
  // .glass-palette visual class, so restyling the glass treatment cannot break
  // dismissal. The palette still carries the glass class for its frosted look.
  it('root carries the data-slash-palette marker for the click-outside guard', () => {
    const { container } = render(CommandPalette, {
      props: { onSelect: vi.fn(), onClose: vi.fn() }
    })
    const root = container.firstElementChild as HTMLElement
    expect(root.hasAttribute('data-slash-palette')).toBe(true)
    expect(root.classList.contains('glass-palette')).toBe(true)
  })

  it('document click inside the palette does not trigger dismissal', () => {
    const { container } = render(CommandPalette, {
      props: { onSelect: vi.fn(), onClose: vi.fn() }
    })

    let dismissed = false
    const onDocumentClick = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null
      if (!target) return
      if (
        target.closest('.ProseMirror') ||
        target.closest('.selection-bubble') ||
        target.closest('[data-slash-palette]')
      )
        return
      dismissed = true
    }
    document.addEventListener('click', onDocumentClick)

    const btn = container.querySelector('button') as HTMLButtonElement
    btn.click()

    expect(dismissed).toBe(false)

    document.removeEventListener('click', onDocumentClick)
  })
})
