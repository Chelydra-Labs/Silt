// Parity test: the frontend first-party registry MUST mirror the Go-side
// FirstPartyPluginIDs roster (backend/plugins/first_party.go). The Go test
// (first_party_test.go) pins the other direction. silt-tasks was once added
// here without updating the Go set, which broke task creation (#407); this
// test fails if the two sides drift apart again.
import { describe, expect, it } from 'vitest'

import { firstPartyPlugins } from './registry'

describe('first-party registry parity with Go FirstPartyPluginIDs (#407)', () => {
  // This roster MUST match backend/plugins/first_party.go's FirstPartyPluginIDs.
  // When you add a bundled plugin, add its id to BOTH files.
  const GO_FIRST_PARTY_IDS = [
    'silt-agenda',
    'silt-calendar',
    'silt-kanban',
    'silt-attachments',
    'silt-tasks'
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
    // silt-calendar, silt-kanban, and silt-tasks all create/mutate tasks via
    // gated bindings; each must declare content-mutate so the Plugins UI
    // surfaces the "trusted" label consistently.
    for (const id of ['silt-calendar', 'silt-kanban', 'silt-tasks']) {
      const p = firstPartyPlugins().find((plugin) => plugin.manifest.id === id)
      expect(
        p?.manifest.capabilities?.['content-mutate'],
        `${id} must declare content-mutate`
      ).toBe(true)
    }
  })
})
