// Unit tests for the spellcheck-menu controller (#769). Covers openSpellMenuAt
// (hit/miss/fallback), the spellMenu getter/setter, and dispose(). Uses the
// $effect.root harness because the factory owns $state + a listener $effect.
import { describe, it, expect, beforeEach, vi } from 'vitest'
import type { Editor } from 'svelte-tiptap'
import { createSpellcheckMenuHarness } from './useSpellcheckMenuHarness.svelte'

const spellFns = vi.hoisted(() => ({
  findMisspellingAt: vi.fn(),
  findMisspellingAtOrAfter: vi.fn()
}))

vi.mock('../../../lib/editor/spellcheck/SpellcheckExtension', () => ({
  findMisspellingAt: spellFns.findMisspellingAt,
  findMisspellingAtOrAfter: spellFns.findMisspellingAtOrAfter
}))

vi.mock('../../../settings/store.svelte', () => ({
  settings: { config: { editor: { spellcheck_enabled: true } } }
}))

interface MockEditor {
  isDestroyed: boolean
}

function makeEditor(destroyed = false): MockEditor {
  return { isDestroyed: destroyed }
}

describe('createSpellcheckMenu', () => {
  let harness: ReturnType<typeof createSpellcheckMenuHarness> | null = null

  beforeEach(() => {
    vi.clearAllMocks()
    harness?.destroy()
    // No editor by default → the listener $effect bails without touching DOM.
    harness = createSpellcheckMenuHarness(() => null)
  })

  it('opens the menu at a misspelled word under the cursor', () => {
    const editor = makeEditor() as unknown as Editor
    spellFns.findMisspellingAt.mockReturnValue({
      word: 'teh',
      from: 1,
      to: 4
    })

    harness!.controller.openSpellMenuAt(editor, 2, { x: 10, y: 20 })

    expect(harness!.controller.spellMenu).toEqual({
      word: 'teh',
      range: { from: 1, to: 4 },
      anchor: { x: 10, y: 20 }
    })
  })

  it('does not open when there is no misspelling at the position (no fallback)', () => {
    const editor = makeEditor() as unknown as Editor
    spellFns.findMisspellingAt.mockReturnValue(null)

    harness!.controller.openSpellMenuAt(editor, 2, { x: 10, y: 20 })

    expect(harness!.controller.spellMenu).toBeNull()
    expect(spellFns.findMisspellingAtOrAfter).not.toHaveBeenCalled()
  })

  it('falls forward to the next misspelling when useFallback is true', () => {
    const editor = makeEditor() as unknown as Editor
    spellFns.findMisspellingAt.mockReturnValue(null)
    spellFns.findMisspellingAtOrAfter.mockReturnValue({
      word: 'recieve',
      from: 8,
      to: 15
    })

    harness!.controller.openSpellMenuAt(editor, 2, { x: 10, y: 20 }, true)

    expect(spellFns.findMisspellingAtOrAfter).toHaveBeenCalledWith(editor, 2)
    expect(harness!.controller.spellMenu).toEqual({
      word: 'recieve',
      range: { from: 8, to: 15 },
      anchor: { x: 10, y: 20 }
    })
  })

  it('does not open even with fallback when no misspelling exists', () => {
    const editor = makeEditor() as unknown as Editor
    spellFns.findMisspellingAt.mockReturnValue(null)
    spellFns.findMisspellingAtOrAfter.mockReturnValue(null)

    harness!.controller.openSpellMenuAt(editor, 2, { x: 10, y: 20 }, true)

    expect(harness!.controller.spellMenu).toBeNull()
  })

  it('spellMenu setter writes back (backs the SpellcheckMenu onClose)', () => {
    harness!.controller.spellMenu = {
      word: 'x',
      range: { from: 0, to: 1 },
      anchor: { x: 0, y: 0 }
    }
    expect(harness!.controller.spellMenu).not.toBeNull()
    harness!.controller.spellMenu = null
    expect(harness!.controller.spellMenu).toBeNull()
  })

  it('dispose clears an open menu', () => {
    const editor = makeEditor() as unknown as Editor
    spellFns.findMisspellingAt.mockReturnValue({
      word: 'teh',
      from: 1,
      to: 4
    })
    harness!.controller.openSpellMenuAt(editor, 2, { x: 10, y: 20 })
    expect(harness!.controller.spellMenu).not.toBeNull()

    harness!.controller.dispose()

    expect(harness!.controller.spellMenu).toBeNull()
  })
})
