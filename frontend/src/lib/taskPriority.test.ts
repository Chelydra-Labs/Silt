import { describe, expect, it } from 'vitest'

import { nextPriority } from './taskPriority'

describe('nextPriority', () => {
  it('cycles Low→Normal→High and wraps back to Low', () => {
    expect(nextPriority(3)).toBe(2)
    expect(nextPriority(2)).toBe(1)
    expect(nextPriority(1)).toBe(3)
  })

  it('bootstraps an unprioritized task to Low', () => {
    expect(nextPriority(0)).toBe(3)
  })

  it('treats legacy out-of-range values as unset', () => {
    expect(nextPriority(4)).toBe(3)
    expect(nextPriority(-1)).toBe(3)
  })
})
