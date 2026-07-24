// HotkeyCaptureInput: capture mode, clear, Escape cancel (#519 / #521 review).
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import HotkeyCaptureInput from './HotkeyCaptureInput.svelte'

describe('HotkeyCaptureInput (#519)', () => {
  afterEach(() => {
    cleanup()
  })

  it('shows the current binding and enters capture on click (not bare focus)', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: 'Ctrl+K', label: 'Quick Search', onchange }
    })
    const input = screen.getByLabelText('Quick Search') as HTMLInputElement
    expect(input.value).toBe('Ctrl+K')
    // Focus alone must not start capture (tab-through keyboard UX).
    await fireEvent.focus(input)
    expect(input.getAttribute('data-capturing')).toBe('false')
    await fireEvent.click(input)
    expect(input.getAttribute('data-capturing')).toBe('true')
    expect(input.value).toBe('Press a shortcut…')
    expect(screen.getByText(/Escape cancels/i)).toBeTruthy()
  })

  it('starts capture on Enter when idle', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: '', label: 'Action', onchange }
    })
    const input = screen.getByLabelText('Action')
    await fireEvent.keyDown(input, { key: 'Enter', bubbles: true })
    expect(input.getAttribute('data-capturing')).toBe('true')
  })

  it('formats a keydown combo and calls onchange', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: '', label: 'Action', onchange }
    })
    const input = screen.getByLabelText('Action')
    await fireEvent.click(input)
    await fireEvent.keyDown(input, {
      key: '9',
      ctrlKey: true,
      shiftKey: true,
      bubbles: true
    })
    expect(onchange).toHaveBeenCalledWith('Ctrl+Shift+9')
    expect(input.getAttribute('data-capturing')).toBe('false')
  })

  it('captures Ctrl+Shift+ArrowUp (editor navigation chords)', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: '', label: 'Move Up', onchange }
    })
    const input = screen.getByLabelText('Move Up')
    await fireEvent.click(input)
    await fireEvent.keyDown(input, {
      key: 'ArrowUp',
      ctrlKey: true,
      shiftKey: true,
      bubbles: true
    })
    expect(onchange).toHaveBeenCalledWith('Ctrl+Shift+ArrowUp')
  })

  it('captures Ctrl+Space (does not silently no-op)', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: '', label: 'Autocomplete', onchange }
    })
    const input = screen.getByLabelText('Autocomplete')
    await fireEvent.click(input)
    await fireEvent.keyDown(input, {
      key: ' ',
      ctrlKey: true,
      bubbles: true
    })
    expect(onchange).toHaveBeenCalledWith('Ctrl+Space')
  })

  it('Escape alone cancels capture without changing the value', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: 'Ctrl+B', label: 'Bold', onchange }
    })
    const input = screen.getByLabelText('Bold') as HTMLInputElement
    await fireEvent.click(input)
    await fireEvent.keyDown(input, { key: 'Escape', bubbles: true })
    expect(onchange).not.toHaveBeenCalled()
    expect(input.getAttribute('data-capturing')).toBe('false')
    expect(input.value).toBe('Ctrl+B')
  })

  it('Ctrl+Escape binds instead of cancelling', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: '', label: 'Chord', onchange }
    })
    const input = screen.getByLabelText('Chord')
    await fireEvent.click(input)
    await fireEvent.keyDown(input, {
      key: 'Escape',
      ctrlKey: true,
      bubbles: true
    })
    expect(onchange).toHaveBeenCalledWith('Ctrl+Escape')
  })

  it('clear button disables the binding (empty string)', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: 'Ctrl+Q', label: 'Quote', onchange }
    })
    const clear = screen.getByRole('button', { name: /Clear Quote shortcut/i })
    await fireEvent.click(clear)
    expect(onchange).toHaveBeenCalledWith('')
  })
})
