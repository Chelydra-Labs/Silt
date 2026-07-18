import { describe, expect, it, vi } from 'vitest'
import { createRecentSaveTracker } from './recentSaveTracker'

describe('recent save tracker', () => {
  it('records one recent only after a confirmed dirty save', () => {
    const record = vi.fn()
    const track = createRecentSaveTracker(record)
    track('tab-a', { phase: 'pending', dirty: true })
    track('tab-a', { phase: 'saving', dirty: true })
    track('tab-a', { phase: 'saved', dirty: false })
    track('tab-a', { phase: 'saved', dirty: false })
    expect(record).toHaveBeenCalledOnce()
    expect(record).toHaveBeenCalledWith('tab-a')
  })

  it('does not record failed or clean save notifications', () => {
    const record = vi.fn()
    const track = createRecentSaveTracker(record)
    track('tab-a', { phase: 'saved', dirty: false })
    track('tab-b', { phase: 'saving', dirty: true })
    track('tab-b', { phase: 'error', dirty: false })
    track('tab-b', { phase: 'saved', dirty: false })
    expect(record).not.toHaveBeenCalled()
  })
})
