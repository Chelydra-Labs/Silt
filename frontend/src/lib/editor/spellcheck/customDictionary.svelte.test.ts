/**
 * Regression tests for #818: "Add to dictionary" must clear the misspelling.
 *
 * Root cause: Go self-writes (AddCustomDictionaryWord / Remove / Import)
 * suppress the fsnotify event that would otherwise emit `config:changed`, so
 * the live config snapshot was never refreshed by the hot-reload path — the
 * spellcheck `$effect` in TipTapEditor (keyed off
 * `config.editor.custom_dictionary`) never re-ran, and the just-added word
 * stayed underlined. The fix mirrors the resolved word list into `settings`
 * locally after each IPC (same discipline as toggleFormatToolbar). These tests
 * lock in that mirror by asserting the observable the fix produces.
 */
import { describe, it, expect, beforeEach, vi } from 'vitest'

const mocks = vi.hoisted(() => ({
  AddCustomDictionaryWord: vi.fn(),
  RemoveCustomDictionaryWord: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    AddCustomDictionaryWord: mocks.AddCustomDictionaryWord,
    RemoveCustomDictionaryWord: mocks.RemoveCustomDictionaryWord
  })
)

vi.mock('@wailsio/runtime', () => ({
  Events: { On: vi.fn(() => () => {}) },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import { settings, type SystemConfig } from '../../../settings/store.svelte'
import { customDictionary } from './customDictionary.svelte'

describe('custom dictionary mirrors into the live config (#818)', () => {
  beforeEach(() => {
    settings.config = {
      editor: { custom_dictionary: [] }
    } as unknown as SystemConfig
    settings.saving = false
    settings.error = ''
    mocks.AddCustomDictionaryWord.mockReset()
    mocks.RemoveCustomDictionaryWord.mockReset()
  })

  it('add mirrors the resolved list so the spellcheck $effect re-checks', async () => {
    mocks.AddCustomDictionaryWord.mockResolvedValue(['wortsila'])
    await customDictionary.add('wortsila')
    expect(settings.config?.editor?.custom_dictionary).toEqual(['wortsila'])
  })

  it('remove mirrors the resolved list', async () => {
    settings.config = {
      editor: { custom_dictionary: ['wortsila'] }
    } as unknown as SystemConfig
    mocks.RemoveCustomDictionaryWord.mockResolvedValue([])
    await customDictionary.remove('wortsila')
    expect(settings.config?.editor?.custom_dictionary).toEqual([])
  })

  it('does not throw when config is not loaded yet (no vault)', async () => {
    settings.config = null
    mocks.AddCustomDictionaryWord.mockResolvedValue(['wortsila'])
    await expect(customDictionary.add('wortsila')).resolves.toBeUndefined()
    expect(settings.config).toBeNull()
  })
})
