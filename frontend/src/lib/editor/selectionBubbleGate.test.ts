import { describe, it, expect } from 'vitest'
import { gateBubbleCoords } from './selectionBubbleGate'

const c = (left: number) => ({ left, top: 10, bottom: 20 })

describe('gateBubbleCoords', () => {
  it('withholds published coords while pointer is down', () => {
    const r = gateBubbleCoords(true, null, c(1))
    expect(r.published).toBeNull()
    expect(r.pending).toEqual(c(1))
  })

  it('updates pending while pointer stays down', () => {
    const r = gateBubbleCoords(true, c(1), c(2))
    expect(r.published).toBeNull()
    expect(r.pending).toEqual(c(2))
  })

  it('publishes fresh coords on pointer release', () => {
    const r = gateBubbleCoords(false, c(1), c(3))
    expect(r.published).toEqual(c(3))
    expect(r.pending).toBeNull()
  })

  it('falls back to pending when release has no fresh coords', () => {
    const r = gateBubbleCoords(false, c(9), null)
    expect(r.published).toEqual(c(9))
    expect(r.pending).toBeNull()
  })

  it('clears both when selection collapses after release', () => {
    const r = gateBubbleCoords(false, null, null)
    expect(r.published).toBeNull()
    expect(r.pending).toBeNull()
  })
})
