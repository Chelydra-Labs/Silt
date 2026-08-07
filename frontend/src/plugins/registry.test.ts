// Parity test: the frontend first-party registry MUST mirror the Go-side
// FirstPartyPluginIDs roster (backend/plugins/first_party.go). The Go test
// (first_party_test.go) pins the other direction. silt-tasks was once added
// here without updating the Go set, which broke task creation (#407); this
// test fails if the two sides drift apart again.
import { describe, expect, it, vi } from 'vitest'

import { firstPartyPlugins, getFirstParty } from './registry'
import type { PluginContext } from './sdk'
import { resetTasksSettings } from './first-party/silt-tasks/settings'

describe('first-party registry parity with Go FirstPartyPluginIDs (#407)', () => {
  // This roster MUST match backend/plugins/first_party.go's FirstPartyPluginIDs.
  // When you add a bundled plugin, add its id to BOTH files.
  const GO_FIRST_PARTY_IDS = [
    'silt-attachments',
    'silt-tasks',
    'silt-ai-summary',
    'silt-ai-qa',
    'silt-ai-assistant',
    'silt-ai-agent'
  ]

  it('frontend registry ids match the Go FirstPartyPluginIDs roster', () => {
    const frontendIds = firstPartyPlugins()
      .map((p) => p.manifest.id)
      .sort()
    const goIds = [...GO_FIRST_PARTY_IDS].sort()
    expect(frontendIds).toEqual(goIds)
  })

  it('silt-tasks is registered and declares content-mutate', () => {
    const tasks = firstPartyPlugins().find(
      (p) => p.manifest.id === 'silt-tasks'
    )
    expect(tasks).toBeDefined()
    expect(tasks?.manifest.capabilities?.['content-mutate']).toBe(true)
  })

  it('every first-party plugin with a content-mutate need declares it', () => {
    // silt-tasks creates/mutates tasks via gated bindings; it must declare
    // content-mutate so the Plugins UI surfaces the "trusted" label
    // consistently.
    for (const id of ['silt-tasks']) {
      const p = firstPartyPlugins().find((plugin) => plugin.manifest.id === id)
      expect(
        p?.manifest.capabilities?.['content-mutate'],
        `${id} must declare content-mutate`
      ).toBe(true)
    }
  })

  it('registers the unified AI providers without standalone view components', () => {
    for (const id of ['silt-ai-qa', 'silt-ai-assistant', 'silt-ai-agent']) {
      const plugin = firstPartyPlugins().find(
        (entry) => entry.manifest.id === id
      )
      expect(plugin?.component, `${id} should be headless`).toBeUndefined()
    }
  })

  it('does not reject when a task settings preload becomes stale during load', async () => {
    let resolveSettings!: (settings: Record<string, unknown>) => void
    const pendingSettings = new Promise<Record<string, unknown>>(
      (resolve) => (resolveSettings = resolve)
    )
    const ctx = {
      getPluginSettings: vi.fn(() => pendingSettings),
      updatePluginSetting: vi.fn()
    } as unknown as PluginContext
    const tasks = getFirstParty('silt-tasks')

    expect(tasks?.onVaultOpen).toBeDefined()
    resetTasksSettings()
    const opening = tasks!.onVaultOpen!(ctx)
    resetTasksSettings()
    resolveSettings({ week_start: 'monday' })

    await expect(opening).resolves.toBeUndefined()
  })

  it('still rejects when the task settings read itself fails', async () => {
    const tasks = getFirstParty('silt-tasks')
    const error = new Error('settings IPC failed')
    const ctx = {
      getPluginSettings: vi.fn().mockRejectedValue(error),
      updatePluginSetting: vi.fn()
    } as unknown as PluginContext

    resetTasksSettings()
    await expect(tasks!.onVaultOpen!(ctx)).rejects.toThrow(error)
    resetTasksSettings()
  })
})
