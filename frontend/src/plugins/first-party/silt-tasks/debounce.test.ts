import { describe, expect, it, vi, afterEach } from 'vitest'
import { trailingDebounce } from './debounce'

afterEach(() => {
  vi.useRealTimers()
})

describe('trailingDebounce', () => {
  it('fires fn once after ms of quiet following rapid trigger() calls', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = trailingDebounce(fn, 200)

    d.trigger()
    d.trigger()
    d.trigger()

    expect(fn).not.toHaveBeenCalled()
    vi.advanceTimersByTime(199)
    expect(fn).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('reschedules on each trigger (trailing edge tracks the last call)', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = trailingDebounce(fn, 100)

    d.trigger()
    vi.advanceTimersByTime(90)
    d.trigger() // reschedule — clock resets
    vi.advanceTimersByTime(90)
    expect(fn).not.toHaveBeenCalled()
    vi.advanceTimersByTime(10)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('cancel() prevents the pending invocation', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = trailingDebounce(fn, 100)

    d.trigger()
    expect(d.pending()).toBe(true)
    d.cancel()
    expect(d.pending()).toBe(false)
    vi.advanceTimersByTime(500)
    expect(fn).not.toHaveBeenCalled()
  })

  it('cancel() is idempotent and trigger() works again after cancel', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = trailingDebounce(fn, 50)

    d.cancel()
    d.cancel()
    d.trigger()
    vi.advanceTimersByTime(50)
    expect(fn).toHaveBeenCalledTimes(1)
  })

  it('pending() reflects whether a trailing invocation is scheduled', () => {
    vi.useFakeTimers()
    const fn = vi.fn()
    const d = trailingDebounce(fn, 100)

    expect(d.pending()).toBe(false)
    d.trigger()
    expect(d.pending()).toBe(true)
    vi.advanceTimersByTime(100)
    expect(d.pending()).toBe(false)
  })
})
