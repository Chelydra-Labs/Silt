import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import { handleGetPageVersion } from './get_page_version'

describe('get_page_version', () => {
  it('previews via PluginContext and does not restore', async () => {
    const getPageVersion = vi.fn(async () => '# older body')
    const restorePageVersion = vi.fn()
    const ctx = {
      getPageVersion,
      restorePageVersion
    } as unknown as PluginContext
    const res = await handleGetPageVersion(ctx, {
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      version_id: 'v-old'
    })
    expect(res.error).toBeUndefined()
    expect(getPageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-old'
    )
    expect(restorePageVersion).not.toHaveBeenCalled()
    expect(res.content).toContain('# older body')
    expect(res.content).toContain('read-only')
  })

  it('requires version_id', async () => {
    const ctx = {
      getPageVersion: vi.fn()
    } as unknown as PluginContext
    const res = await handleGetPageVersion(ctx, {
      notebook: 'Work',
      page: 'Daily',
      version_id: ''
    })
    expect(res.error).toMatch(/version_id/)
  })
})
