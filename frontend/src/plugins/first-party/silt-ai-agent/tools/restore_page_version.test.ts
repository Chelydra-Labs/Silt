import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { handleRestorePageVersion } from './restore_page_version'

describe('restore_page_version', () => {
  it('restores via PluginContext', async () => {
    const restorePageVersion = vi.fn(async () => true)
    const ctx = { restorePageVersion } as unknown as PluginContext
    const res = await handleRestorePageVersion(ctx, {
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      version_id: 'v-old'
    })
    expect(res.error).toBeUndefined()
    expect(restorePageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-old'
    )
    expect(res.content).toContain('Restored')
    expect(res.content).toContain('v-old')
    expect(res.content).not.toMatch(/was kept\.$/)
  })

  it('surfaces restore errors', async () => {
    const ctx = {
      restorePageVersion: vi.fn(async () => {
        throw new Error(
          JSON.stringify({
            code: 'navigation_not_found',
            message: 'page version not found'
          })
        )
      })
    } as unknown as PluginContext
    const res = await handleRestorePageVersion(ctx, {
      notebook: 'Work',
      page: 'Daily',
      version_id: 'missing'
    })
    expect(res.error).toBe('page version not found')
  })
})
