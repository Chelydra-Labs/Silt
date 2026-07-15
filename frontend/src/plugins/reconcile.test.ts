import { describe, it, expect, beforeEach, vi } from 'vitest'

// grants is mocked so the reconcile pass can control capabilities per-test.
vi.mock('./grants.svelte', () => ({
  isGranted: vi.fn(() => true),
  initGrants: vi.fn(),
  refreshGrants: vi.fn(),
  resetGrantsForTests: vi.fn(),
  setGrantsForTests: vi.fn()
}))

import { isGranted } from './grants.svelte'
import { revokeRevokedContributions } from './reconcile'
import {
  registerSlashCommand,
  resetSlashRegistryForTests,
  getSlashCommands
} from '../lib/editor/slash-registry'
import { registerSurface, resetSurfacesForTests, getSurfaces } from './surfaces'
import {
  registerDecorationProvider,
  resetDecorationsForTests,
  getDecorationProviderPluginIDs
} from '../lib/editor/decorations'

describe('revokeRevokedContributions (#582)', () => {
  beforeEach(() => {
    resetSlashRegistryForTests()
    resetSurfacesForTests()
    resetDecorationsForTests()
    vi.mocked(isGranted).mockReturnValue(true)
  })

  it('removes slash commands whose plugin lost editor-schema', () => {
    registerSlashCommand({ id: 'p:cmd', label: 'X', pluginID: 'p' })
    expect(getSlashCommands().some((c) => c.id === 'p:cmd')).toBe(true)

    // Revoke all capabilities for p, then reconcile.
    vi.mocked(isGranted).mockReturnValue(false)
    revokeRevokedContributions()

    expect(getSlashCommands().some((c) => c.id === 'p:cmd')).toBe(false)
  })

  it('removes surfaces whose plugin lost ui-surface', () => {
    registerSurface({
      id: 'p:surf',
      pluginID: 'p',
      kind: 'status-bar-item',
      label: 'S'
    })
    // grant ui-surface at registration, then revoke for the pass
    vi.mocked(isGranted).mockImplementation((_pid, cap) => {
      // registration already happened granted; now revoke ui-surface only
      return cap !== 'ui-surface'
    })
    revokeRevokedContributions()
    expect(getSurfaces().some((s) => s.id === 'p:surf')).toBe(false)
  })

  it('removes decoration providers whose plugin lost editor-schema', () => {
    // registerDecorationProvider checks isGranted at registration, so grant first.
    vi.mocked(isGranted).mockReturnValue(true)
    registerDecorationProvider('d', 'p', () => [])
    expect(getDecorationProviderPluginIDs()).toContain('p')

    vi.mocked(isGranted).mockReturnValue(false)
    revokeRevokedContributions()
    expect(getDecorationProviderPluginIDs()).not.toContain('p')
  })

  it('leaves contributions whose plugin still holds the capability', () => {
    registerSlashCommand({ id: 'p:cmd', label: 'X', pluginID: 'p' })
    registerSurface({
      id: 'p:surf',
      pluginID: 'p',
      kind: 'status-bar-item',
      label: 'S'
    })
    registerDecorationProvider('d', 'p', () => [])

    // Still granted → nothing removed.
    vi.mocked(isGranted).mockReturnValue(true)
    revokeRevokedContributions()

    expect(getSlashCommands().some((c) => c.id === 'p:cmd')).toBe(true)
    expect(getSurfaces().some((s) => s.id === 'p:surf')).toBe(true)
    expect(getDecorationProviderPluginIDs()).toContain('p')
  })

  it('is a no-op when nothing is registered', () => {
    vi.mocked(isGranted).mockReturnValue(false)
    expect(() => revokeRevokedContributions()).not.toThrow()
  })
})
