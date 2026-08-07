import { afterEach, describe, expect, it, vi } from 'vitest'
import { motionDuration } from './motion'

const originalMatchMedia = window.matchMedia

afterEach(() => {
  window.matchMedia = originalMatchMedia
})

describe('task motion', () => {
  it('removes transition duration when reduced motion is requested', () => {
    window.matchMedia = vi.fn().mockReturnValue({ matches: true })
    expect(motionDuration(200)).toBe(0)
  })

  it('keeps the intended duration otherwise', () => {
    window.matchMedia = vi.fn().mockReturnValue({ matches: false })
    expect(motionDuration(200)).toBe(200)
  })
})
