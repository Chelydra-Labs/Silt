// Coverage for the plugin event bus (#106): subscribe/unsubscribe, fan-out,
// per-plugin cleanup, and host-event lazy attach/detach. Mocks the Wails
// runtime so no real IPC is involved.

import { describe, expect, it, beforeEach, vi } from 'vitest'

// Hoisted mock state for the Wails runtime. EventsOn/EventsOff are tracked so
// the bus's lazy attach/detach can be asserted.
const mocks = vi.hoisted(() => ({
  listeners: new Map<string, ((payload: unknown) => void)[]>(),
  eventsOn: vi.fn(),
  eventsOff: vi.fn()
}))

vi.mock('../../wailsjs/runtime/runtime.js', () => ({
  EventsOn: (event: string, cb: (payload: unknown) => void) => {
    mocks.eventsOn(event, cb)
    if (!mocks.listeners.has(event)) mocks.listeners.set(event, [])
    mocks.listeners.get(event)!.push(cb)
  },
  EventsOff: (event: string) => {
    mocks.eventsOff(event)
    mocks.listeners.delete(event)
  }
}))

// Import AFTER the mock so the bus uses the stubbed runtime.
import {
  subscribe,
  dispatch,
  cleanupPlugin,
  clearAllSubscribers,
  subscriberCount
} from './events'

function emitHost(event: string, payload: unknown) {
  for (const cb of mocks.listeners.get(event) ?? []) cb(payload)
}

describe('plugin event bus (#106)', () => {
  beforeEach(() => {
    // Tear down live subscribers FIRST (it calls EventsOff for active host
    // listeners), THEN clear the mock call records so per-test assertions
    // only see calls made during the test itself.
    clearAllSubscribers()
    mocks.listeners.clear()
    mocks.eventsOn.mockClear()
    mocks.eventsOff.mockClear()
  })

  it('delivers an in-process dispatch to a subscriber', () => {
    const cb = vi.fn()
    const off = subscribe('p1', 'active-notebook:changed', cb)
    dispatch('active-notebook:changed', {
      notebook: 'Work',
      section: '',
      page: ''
    })
    expect(cb).toHaveBeenCalledTimes(1)
    expect(cb).toHaveBeenCalledWith({ notebook: 'Work', section: '', page: '' })
    off()
  })

  it('unsubscribe stops further delivery', () => {
    const cb = vi.fn()
    const off = subscribe('p1', 'active-notebook:changed', cb)
    off()
    dispatch('active-notebook:changed', {
      notebook: 'X',
      section: '',
      page: ''
    })
    expect(cb).not.toHaveBeenCalled()
    expect(subscriberCount('p1', 'active-notebook:changed')).toBe(0)
  })

  it('fans out a host block:changed event to multiple plugins', () => {
    const cb1 = vi.fn()
    const cb2 = vi.fn()
    subscribe('p1', 'block:changed', cb1)
    subscribe('p2', 'block:changed', cb2)
    const payload = {
      id: 'abc',
      notebook: 'Work',
      section: '',
      page: 'Daily',
      file_date: '2026-06-16'
    }
    emitHost('block:changed', payload)
    expect(cb1).toHaveBeenCalledWith(payload)
    expect(cb2).toHaveBeenCalledWith(payload)
  })

  it('lazily attaches a single Wails listener for host events and detaches on last unsubscribe', () => {
    expect(mocks.eventsOn).not.toHaveBeenCalled()
    const off1 = subscribe('p1', 'block:changed', vi.fn())
    expect(mocks.eventsOn).toHaveBeenCalledWith(
      'block:changed',
      expect.any(Function)
    )
    // A second subscriber does NOT add a second listener.
    const off2 = subscribe('p2', 'block:changed', vi.fn())
    expect(mocks.eventsOn).toHaveBeenCalledTimes(1)
    off1()
    expect(mocks.eventsOff).not.toHaveBeenCalled() // still one subscriber
    off2()
    expect(mocks.eventsOff).toHaveBeenCalledWith('block:changed')
  })

  it('cleanupPlugin removes every subscription for a plugin across events', () => {
    subscribe('p1', 'block:changed', vi.fn())
    subscribe('p1', 'active-notebook:changed', vi.fn())
    subscribe('p2', 'block:changed', vi.fn())
    expect(subscriberCount('p1')).toBe(2)
    cleanupPlugin('p1')
    expect(subscriberCount('p1')).toBe(0)
    expect(subscriberCount('p2')).toBe(1) // untouched
  })

  it('a throwing callback does not break sibling subscribers', () => {
    const ok = vi.fn()
    subscribe('p1', 'block:changed', () => {
      throw new Error('boom')
    })
    subscribe('p2', 'block:changed', ok)
    // Silence the expected console.error from the bus.
    const spy = vi.spyOn(console, 'error').mockImplementation(() => {})
    emitHost('block:changed', {
      id: 'x',
      notebook: '',
      section: '',
      page: '',
      file_date: ''
    })
    spy.mockRestore()
    expect(ok).toHaveBeenCalledTimes(1)
  })

  it('an unsubscribe during dispatch does not corrupt iteration', () => {
    let off2: () => void = () => {}
    const cb1 = vi.fn(() => off2()) // unsubscribes the second mid-dispatch
    const cb2 = vi.fn()
    subscribe('p1', 'block:changed', cb1)
    off2 = subscribe('p2', 'block:changed', cb2)
    emitHost('block:changed', {
      id: 'x',
      notebook: '',
      section: '',
      page: '',
      file_date: ''
    })
    expect(cb1).toHaveBeenCalledTimes(1)
    // cb2 was registered before the snapshot, so it still fires this round.
    expect(cb2).toHaveBeenCalledTimes(1)
  })
})
