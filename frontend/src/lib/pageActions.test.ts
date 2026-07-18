import { beforeEach, describe, expect, it, vi } from 'vitest'
import {
  copyPagePath,
  copyPageReference,
  pagePath,
  shortestPageReference
} from './pageActions'
import {
  _resetForTests,
  notificationsState
} from '../notifications/store.svelte'

const mocks = vi.hoisted(() => ({ resolvePageLink: vi.fn() }))
vi.mock('../../bindings/silt/app.js', () => ({
  ResolvePageLink: mocks.resolvePageLink
}))

const ref = { notebook: 'Work', section: 'Projects/Active', page: 'Plan' }

describe('shared page actions', () => {
  beforeEach(() => {
    _resetForTests()
    mocks.resolvePageLink.mockReset().mockResolvedValue({
      exists: true,
      shortest: 'Active/Plan'
    })
  })

  it('builds the same canonical plain path used by every surface', () => {
    expect(pagePath(ref)).toBe('Work/Projects/Active/Plan')
    expect(pagePath({ ...ref, section: '' })).toBe('Work/Plan')
  })

  it('copies the shortest unique wiki reference', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.assign(navigator, { clipboard: { writeText } })
    expect(await shortestPageReference(ref)).toBe('[[Active/Plan]]')
    expect(await copyPageReference(ref)).toBe(true)
    expect(writeText).toHaveBeenCalledWith('[[Active/Plan]]')
  })

  it('falls back to the full reference when resolution fails', async () => {
    mocks.resolvePageLink.mockRejectedValueOnce(new Error('unavailable'))
    expect(await shortestPageReference(ref)).toBe(
      '[[Work/Projects/Active/Plan]]'
    )
  })

  it('reports clipboard failure with shared grounded copy', async () => {
    Object.assign(navigator, {
      clipboard: { writeText: vi.fn().mockRejectedValue(new Error('denied')) }
    })
    expect(await copyPagePath(ref)).toBe(false)
    expect(notificationsState.items).toEqual([
      expect.objectContaining({
        kind: 'error',
        message: 'Could not copy to the clipboard.'
      })
    ])
  })
})
