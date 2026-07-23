// #580: vault:closing must unregister every loaded plugin's editor
// contributions (slash commands, surfaces, decorations) — mirroring
// teardownPlugin — so a vault switch leaves no ghost commands/surfaces/
// decorations in the registries.
//
// The loader's dynamic import() of Blob URLs can't run in jsdom, so we seed
// loadedPlugins.plugins directly and assert the unregister helpers are invoked
// for every loaded plugin when the captured vault:closing callback fires.
import { describe, expect, it, beforeAll, vi } from 'vitest'

const spyUnregisterSlash = vi.hoisted(() => vi.fn())
const spyUnregisterSurfaces = vi.hoisted(() => vi.fn())
const spyUnregisterDecorations = vi.hoisted(() => vi.fn())
const mockEventsOn = vi.hoisted(() =>
  vi.fn((_event: string, _cb: (...args: unknown[]) => void) => () => {})
)

vi.mock('../lib/editor/slash-registry', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('../lib/editor/slash-registry')>()
  return { ...actual, unregisterPluginSlashCommands: spyUnregisterSlash }
})
vi.mock('./surfaces', async (importOriginal) => {
  const actual = await importOriginal<typeof import('./surfaces')>()
  return { ...actual, unregisterPluginSurfaces: spyUnregisterSurfaces }
})
vi.mock('../lib/editor/decorations', async (importOriginal) => {
  const actual =
    await importOriginal<typeof import('../lib/editor/decorations')>()
  return { ...actual, unregisterPluginDecorations: spyUnregisterDecorations }
})
vi.mock('./grants.svelte', () => ({
  initGrants: vi.fn(),
  isGranted: vi.fn(() => true),
  refreshGrants: vi.fn(() => Promise.resolve()),
  resetGrantsForTests: vi.fn(),
  setGrantsForTests: vi.fn()
}))
vi.mock('../../bindings/silt/app.js', () => ({
  ListPlugins: vi.fn(() => Promise.resolve([])),
  ReadPluginSource: vi.fn(),
  RegisterPluginSession: vi.fn(() => Promise.resolve('t')),
  UnregisterPluginSession: vi.fn(() => Promise.resolve(undefined))
}))
vi.mock('@wailsio/runtime', () => ({
  Events: { On: mockEventsOn },
  Call: { ByID: vi.fn(), ByName: vi.fn() },
  CancellablePromise: class {
    then() {
      return this
    }
    catch() {
      return this
    }
    finally() {
      return this
    }
  },
  Create: {
    Nullable: (fn: unknown) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

import { loadPlugins } from './loader'
import { loadedPlugins } from './store.svelte'

describe('vault:closing purges plugin contributions (#580)', () => {
  let vaultClosingCb: (() => void) | null = null

  beforeAll(async () => {
    await loadPlugins('Work', '', '')
    const call = mockEventsOn.mock.calls.find(
      (args: unknown[]) => args[0] === 'vault:closing'
    )
    vaultClosingCb = call ? call[1] : null
  })

  it('captures the vault:closing handler', () => {
    expect(vaultClosingCb).toBeTruthy()
  })

  it('calls all three unregister helpers for every loaded plugin', () => {
    expect(vaultClosingCb).toBeTruthy()
    // Seed two loaded plugins directly into the reactive store.
    loadedPlugins.plugins = new Map([
      ['alpha', { manifest: { id: 'alpha' } } as never],
      ['beta', { manifest: { id: 'beta' } } as never]
    ])

    spyUnregisterSlash.mockClear()
    spyUnregisterSurfaces.mockClear()
    spyUnregisterDecorations.mockClear()

    vaultClosingCb!()

    for (const id of ['alpha', 'beta']) {
      expect(spyUnregisterSlash).toHaveBeenCalledWith(id)
      expect(spyUnregisterSurfaces).toHaveBeenCalledWith(id)
      expect(spyUnregisterDecorations).toHaveBeenCalledWith(id)
    }
  })
})
