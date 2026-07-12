import { beforeEach, describe, expect, it, vi } from 'vitest'
import { v2CtxStubs } from '../../test-helpers'
import type { PluginContext } from '../../sdk'

const mocks = vi.hoisted(() => ({
  registerSlashCommand: vi.fn(() => () => {})
}))

import plugin from './index'
import { getAssistantController } from './state.svelte'

describe('silt-ai-assistant plugin entry', () => {
  beforeEach(() => {
    mocks.registerSlashCommand.mockClear()
    plugin.onVaultClose?.()
  })

  it('exports Writing Assistant manifest with required caps', () => {
    expect(plugin.manifest.id).toBe('silt-ai-assistant')
    expect(plugin.manifest.name).toBe('Writing Assistant')
    expect(plugin.manifest.capabilities?.ai).toBe(true)
    expect(plugin.manifest.capabilities?.['content-mutate']).toBe(true)
  })

  it('registers slash commands on vault open', () => {
    const ctx = {
      ...v2CtxStubs,
      registerSlashCommand: mocks.registerSlashCommand,
      activeNotebook: '',
      activeSection: '',
      activePage: '',
      today: '2026-07-12',
      on: () => () => {},
      sqliteQuery: async () => ({ rows: [], truncated: false }),
      getPluginSettings: async () => ({})
    } as unknown as PluginContext

    plugin.onVaultOpen?.(ctx)
    expect(getAssistantController()).not.toBeNull()
    // Six curated actions by default
    expect(mocks.registerSlashCommand).toHaveBeenCalledTimes(6)
    const ids = (
      mocks.registerSlashCommand.mock.calls as unknown as Array<
        [{ id: string }]
      >
    ).map((c) => c[0].id)
    expect(ids).toContain('draft-expand')
    expect(ids).toContain('suggest-related')
  })
})
