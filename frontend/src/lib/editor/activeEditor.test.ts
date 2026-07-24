import { afterEach, describe, expect, it } from 'vitest'
import {
  setActiveEditor,
  getActiveEditor,
  getLastActiveEditor,
  clearActiveEditorState
} from './activeEditor.svelte'

afterEach(() => clearActiveEditorState())

describe('activeEditor store', () => {
  it('starts null for both focused and lastFocused', () => {
    expect(getActiveEditor()).toBeNull()
    expect(getLastActiveEditor()).toBeNull()
  })

  it('setActiveEditor stores the editor and getActiveEditor returns it', () => {
    const fake = { isDestroyed: false } as never
    setActiveEditor(fake)
    // $state wraps the value in a reactive proxy, so compare by structure.
    expect(getActiveEditor()).toEqual(fake)
  })

  it('setActiveEditor(null) clears focused but keeps lastFocused (blur path)', () => {
    const fake = { isDestroyed: false } as never
    setActiveEditor(fake)
    setActiveEditor(null)
    expect(getActiveEditor()).toBeNull()
    // lastFocused survives blur so keyboard-only openers (Tab to chip + Enter)
    // can recover the editor the user was just typing in.
    expect(getLastActiveEditor()).toEqual(fake)
  })

  it('getLastActiveEditor tracks the most recently focused editor', () => {
    const first = { isDestroyed: false } as never
    const second = { isDestroyed: false } as never
    setActiveEditor(first)
    setActiveEditor(null)
    setActiveEditor(second)
    setActiveEditor(null)
    expect(getLastActiveEditor()).toEqual(second)
  })

  it('clearActiveEditorState clears both focused and lastFocused', () => {
    const fake = { isDestroyed: false } as never
    setActiveEditor(fake)
    setActiveEditor(null)
    clearActiveEditorState()
    expect(getActiveEditor()).toBeNull()
    expect(getLastActiveEditor()).toBeNull()
  })
})
