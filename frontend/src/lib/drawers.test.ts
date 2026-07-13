import { afterEach, describe, expect, it } from 'vitest'
import {
  aiSearchDrawer,
  closeAISearchDrawer,
  resetAISearchDrawer
} from '../plugins/first-party/silt-ai-qa/drawer.svelte'
import {
  writingAssistantDrawer,
  closeWritingAssistantDrawer,
  resetWritingAssistantDrawer
} from '../plugins/first-party/silt-ai-assistant/drawer.svelte'
import {
  openAISearchDrawerExclusive,
  toggleAISearchDrawerExclusive,
  openWritingAssistantDrawerExclusive,
  toggleWritingAssistantDrawerExclusive
} from './drawers.svelte'

// Reset both drawers between cases so order does not matter.
function resetBoth(): void {
  resetAISearchDrawer()
  resetWritingAssistantDrawer()
}

describe('AI drawer mutual exclusion (#542)', () => {
  afterEach(resetBoth)

  it('opening Writing Assistant closes an open AI Assistant drawer', () => {
    aiSearchDrawer.open = true
    expect(writingAssistantDrawer.open).toBe(false)

    openWritingAssistantDrawerExclusive()

    expect(writingAssistantDrawer.open).toBe(true)
    expect(aiSearchDrawer.open).toBe(false)
  })

  it('opening AI Assistant closes an open Writing Assistant drawer', () => {
    writingAssistantDrawer.open = true
    expect(aiSearchDrawer.open).toBe(false)

    openAISearchDrawerExclusive()

    expect(aiSearchDrawer.open).toBe(true)
    expect(writingAssistantDrawer.open).toBe(false)
  })

  it('toggle closes the other drawer only when opening', () => {
    aiSearchDrawer.open = true

    // Toggle WA from closed -> opens it, closes QA.
    toggleWritingAssistantDrawerExclusive()
    expect(writingAssistantDrawer.open).toBe(true)
    expect(aiSearchDrawer.open).toBe(false)

    // Toggle WA again (closing) leaves QA closed.
    toggleWritingAssistantDrawerExclusive()
    expect(writingAssistantDrawer.open).toBe(false)
    expect(aiSearchDrawer.open).toBe(false)
  })

  it('toggle AI Assistant closes Writing Assistant only when opening', () => {
    writingAssistantDrawer.open = true

    toggleAISearchDrawerExclusive()
    expect(aiSearchDrawer.open).toBe(true)
    expect(writingAssistantDrawer.open).toBe(false)

    toggleAISearchDrawerExclusive()
    expect(aiSearchDrawer.open).toBe(false)
    expect(writingAssistantDrawer.open).toBe(false)
  })

  it('opening the same drawer twice is idempotent and keeps the other closed', () => {
    openWritingAssistantDrawerExclusive()
    openWritingAssistantDrawerExclusive()
    expect(writingAssistantDrawer.open).toBe(true)
    expect(aiSearchDrawer.open).toBe(false)
  })

  it('never leaves both drawers open simultaneously', () => {
    // Drive every open/toggle path in adversarial sequences; the invariant
    // "at most one open" must hold after every operation. This is what makes
    // Escape coherent: only the active drawer's keydown guard passes.
    const ops = [
      openWritingAssistantDrawerExclusive,
      openAISearchDrawerExclusive,
      toggleWritingAssistantDrawerExclusive,
      toggleAISearchDrawerExclusive,
      closeWritingAssistantDrawer,
      closeAISearchDrawer
    ]
    for (const op of ops) {
      resetBoth()
      op()
      expect(writingAssistantDrawer.open && aiSearchDrawer.open).toBe(false)

      // Also interleave: start with the other open, then op.
      resetBoth()
      openAISearchDrawerExclusive()
      op()
      expect(writingAssistantDrawer.open && aiSearchDrawer.open).toBe(false)

      resetBoth()
      openWritingAssistantDrawerExclusive()
      op()
      expect(writingAssistantDrawer.open && aiSearchDrawer.open).toBe(false)
    }
  })
})
