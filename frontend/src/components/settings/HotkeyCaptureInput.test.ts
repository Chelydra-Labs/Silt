// HotkeyCaptureInput: capture mode, clear, Escape cancel (#519 harden-polish).
import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import HotkeyCaptureInput from './HotkeyCaptureInput.svelte'

describe('HotkeyCaptureInput (#519)', () => {
  afterEach(() => {
    cleanup()
  })

  it('shows the current binding and enters capture on focus', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: 'Ctrl+K', label: 'Quick Search', onchange }
    })
    const input = screen.getByLabelText('Quick Search') as HTMLInputElement
    expect(input.value).toBe('Ctrl+K')
    await fireEvent.focus(input)
    expect(input.getAttribute('data-capturing')).toBe('true')
    expect(input.value).toBe('Press a shortcut…')
    expect(screen.getByText(/Escape cancels/i)).toBeTruthy()
  })

  it('formats a keydown combo and calls onchange', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: '', label: 'Action', onchange }
    })
    const input = screen.getByLabelText('Action')
    await fireEvent.focus(input)
    await fireEvent.keyDown(input, {
      key: '9',
      ctrlKey: true,
      shiftKey: true,
      bubbles: true
    })
    expect(onchange).toHaveBeenCalledWith('Ctrl+Shift+9')
    expect(input.getAttribute('data-capturing')).toBe('false')
  })

  it('Escape cancels capture without changing the value', async () => {
    const onchange = vi.fn()
    render(HotkeyCaptureInput, {
      props: { value: 'Ctrl+B', label: 'Bold', onchange }
    })
    const input = screen.getByLabelText('Bold') as HTMLInputElement
    await fireEvent.focus(input)
    await fireEvent.keyDown(input, { key: 'Escape', bubbles: true })
    expect(onchange).not.toHaveBeenCalled()
    expect(input.getAttribute('data-capturing')).toBe('false')
    expect(input.value).toBe('Ctrl+B')
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
