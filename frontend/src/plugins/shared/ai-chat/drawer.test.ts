import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  aiChatDrawer,
  closeAIChatDrawer,
  registerAIChatController,
  resetAIChatDrawer,
  toggleAIChatDrawer
} from './drawer.svelte'

describe('unified AI chat drawer', () => {
  afterEach(() => {
    registerAIChatController(null)
    aiChatDrawer.open = false
  })

  it('opens and closes through the titlebar toggle', () => {
    expect(aiChatDrawer.open).toBe(false)

    toggleAIChatDrawer()
    expect(aiChatDrawer.open).toBe(true)

    toggleAIChatDrawer()
    expect(aiChatDrawer.open).toBe(false)
  })

  it('stops an in-flight run when the drawer closes (keep transcript)', () => {
    const stop = vi.fn()
    const clear = vi.fn()
    registerAIChatController({ stop, clear } as unknown as Parameters<
      typeof registerAIChatController
    >[0])

    aiChatDrawer.open = true
    closeAIChatDrawer()

    expect(aiChatDrawer.open).toBe(false)
    expect(stop).toHaveBeenCalledOnce()
    // Closing keeps the transcript for reopen.
    expect(clear).not.toHaveBeenCalled()
  })

  it('stops and clears the controller on vault teardown (reset)', () => {
    const stop = vi.fn()
    const clear = vi.fn()
    registerAIChatController({ stop, clear } as unknown as Parameters<
      typeof registerAIChatController
    >[0])

    aiChatDrawer.open = true
    resetAIChatDrawer()

    expect(aiChatDrawer.open).toBe(false)
    expect(stop).toHaveBeenCalledOnce()
    expect(clear).toHaveBeenCalledOnce()
  })
})
