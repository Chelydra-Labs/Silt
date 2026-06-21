import { describe, it, expect, vi } from 'vitest'
import { render, fireEvent } from '@testing-library/svelte'
import CommandPalette from './CommandPalette.svelte'

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
    const { getByText } = render(CommandPalette, {
      props: { onSelect, onClose, query: 'nonexistentcommand' }
    })

    expect(getByText('No matching commands')).toBeTruthy()
  })

  it('ranks label matches higher than description matches', () => {
    const onSelect = vi.fn()
    const onClose = vi.fn()
    const { container } = render(CommandPalette, {
      props: { onSelect, onClose, query: 'h' }
    })

    const buttons = container.querySelectorAll('button')
    const labels = Array.from(buttons).map((btn) => {
      const span = btn.querySelector('.font-label-sm-bold')
      return span ? span.textContent : ''
    })

    // "Heading 1" starts with "h", so it should rank higher than "Italic" (whose description contains "h")
    const h1Index = labels.indexOf('Heading 1')
    const italicIndex = labels.indexOf('Italic')

    expect(h1Index).toBeGreaterThan(-1)
    expect(italicIndex).toBeGreaterThan(-1)
    expect(h1Index).toBeLessThan(italicIndex)
  })
})
