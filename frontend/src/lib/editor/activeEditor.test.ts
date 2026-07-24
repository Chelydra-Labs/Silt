import { afterEach, describe, expect, it } from 'vitest'
import { setActiveEditor, getActiveEditor } from './activeEditor.svelte'

afterEach(() => setActiveEditor(null))

describe('activeEditor store', () => {
  it('starts null', () => {
    expect(getActiveEditor()).toBeNull()
  })

  it('setActiveEditor stores the editor and getActiveEditor returns it', () => {
    const fake = { isDestroyed: false } as never
    setActiveEditor(fake)
    // $state wraps the value in a reactive proxy, so compare by structure.
    expect(getActiveEditor()).toEqual(fake)
  })

  it('setActiveEditor(null) clears the stored editor (blur path)', () => {
    const fake = { isDestroyed: false } as never
    setActiveEditor(fake)
    setActiveEditor(null)
    expect(getActiveEditor()).toBeNull()
  })
})
