import { describe, expect, it } from 'vitest'
import { hiddenTabIds } from './tabOverflow'

describe('hiddenTabIds', () => {
  it('returns only tabs outside either visible edge', () => {
    expect(
      hiddenTabIds({ left: 10, right: 210 }, [
        { id: 'left', left: 0, right: 80 },
        { id: 'visible', left: 20, right: 180 },
        { id: 'right', left: 180, right: 250 }
      ])
    ).toEqual(['left', 'right'])
  })
})
