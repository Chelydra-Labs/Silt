import { describe, it, expect, beforeEach } from 'vitest'
import { findBarState } from './findBarState.svelte'

describe('findBarState', () => {
  // Module-scope runes state persists across tests; close() is the full reset.
  beforeEach(() => {
    findBarState.close()
  })

  it('setReplaceOpen(true) reveals replace without closing the bar', () => {
    findBarState.openFind()
    findBarState.setReplaceOpen(true)
    expect(findBarState.open).toBe(true)
    expect(findBarState.replaceOpen).toBe(true)
  })

  it('setReplaceOpen(false) hides replace without closing the bar', () => {
    findBarState.openReplace()
    findBarState.setReplaceOpen(false)
    expect(findBarState.open).toBe(true)
    expect(findBarState.replaceOpen).toBe(false)
  })

  it('openFind() after a reveal resets replace to hidden', () => {
    findBarState.openReplace()
    findBarState.openFind()
    expect(findBarState.open).toBe(true)
    expect(findBarState.replaceOpen).toBe(false)
  })

  it('openReplace() opens the bar with replace visible', () => {
    findBarState.openReplace()
    expect(findBarState.open).toBe(true)
    expect(findBarState.replaceOpen).toBe(true)
  })

  it('close() clears both flags', () => {
    findBarState.openReplace()
    findBarState.close()
    expect(findBarState.open).toBe(false)
    expect(findBarState.replaceOpen).toBe(false)
  })
})
