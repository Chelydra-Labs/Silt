import { afterEach, describe, expect, it } from 'vitest'
import {
  aiChatDrawer,
  resetAIChatDrawer,
  toggleAIChatDrawer
} from './drawer.svelte'

describe('unified AI chat drawer', () => {
  afterEach(resetAIChatDrawer)

  it('opens and closes through the titlebar toggle', () => {
    expect(aiChatDrawer.open).toBe(false)

    toggleAIChatDrawer()
    expect(aiChatDrawer.open).toBe(true)

    toggleAIChatDrawer()
    expect(aiChatDrawer.open).toBe(false)
  })
})
