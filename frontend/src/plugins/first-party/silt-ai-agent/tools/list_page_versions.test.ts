import { describe, expect, it, vi } from 'vitest'
import type { PluginContext } from '../../../sdk'
import {
  handleListPageVersions,
  listPageVersionsToolDef
} from './list_page_versions'

describe('list_page_versions', () => {
  it('lists newest-first rows via PluginContext', async () => {
    const listPageVersions = vi.fn(async () => [
      {
        id: 'v-new',
        timestamp: '2026-08-16T18:00:00Z',
        source: 'editor',
        bytes: 120
      }
    ])
    const ctx = { listPageVersions } as unknown as PluginContext
    const res = await handleListPageVersions(ctx, {
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily'
    })
    expect(res.error).toBeUndefined()
    expect(listPageVersions).toHaveBeenCalledWith('Work', 'Journal', 'Daily')
    expect(res.content).toContain('v-new')
    expect(res.content).toContain('Work > Journal > Daily')
  })

  it('returns an empty-list message, not an error', async () => {
    const ctx = {
      listPageVersions: vi.fn(async () => [])
    } as unknown as PluginContext
    const res = await handleListPageVersions(ctx, {
      notebook: 'Work',
      page: 'Daily'
    })
    expect(res.error).toBeUndefined()
    expect(res.content).toContain('No versions')
  })

  it('requires notebook and page', async () => {
    const ctx = {
      listPageVersions: vi.fn()
    } as unknown as PluginContext
    const res = await handleListPageVersions(ctx, { notebook: '', page: '' })
    expect(res.error).toMatch(/required/)
    expect(listPageVersionsToolDef.name).toBe('list_page_versions')
  })
})
