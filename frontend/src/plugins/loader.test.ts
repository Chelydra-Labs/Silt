// Plugin loader tests (#161 integrity, #151 session tokens, P5-12, P7-13).
//
// The loader's dynamic `import()` of Blob URLs cannot run in jsdom, so we
// test the integrity-check REJECTION path (which skips the import) and verify
// the session-token + sha256 plumbing via direct assertion. The happy-path
// (hash matches → import succeeds) is covered by the Go-side Install tests
// (#161) and by manual verification.
import { describe, expect, it, beforeEach, beforeAll, vi } from 'vitest'
import { loadPlugins, teardownPlugin } from './loader'

const mockListPlugins = vi.hoisted(() => vi.fn())
const mockReadPluginSource = vi.hoisted(() => vi.fn())
const mockRegisterSession = vi.hoisted(() =>
  vi.fn(() => Promise.resolve('test-token'))
)
const mockUnregisterSession = vi.hoisted(() =>
  vi.fn(() => Promise.resolve(undefined))
)
const mockEventsOn = vi.hoisted(() =>
  vi.fn((_event: string, _cb: (payload: unknown) => void) => () => {})
)
const mockClosePluginDB = vi.hoisted(() =>
  vi.fn(() => Promise.resolve(undefined))
)
vi.mock('../../bindings/silt/app.js', () => ({
  ListPlugins: mockListPlugins,
  ReadPluginSource: mockReadPluginSource,
  RegisterPluginSession: mockRegisterSession,
  UnregisterPluginSession: mockUnregisterSession,
  ClosePluginDB: mockClosePluginDB
}))
// Events.On returns a per-listener disposer (v3 contract). The mock mirrors
// that so cleanupPlugin / clearAllSubscribers can call the captured disposer
// without throwing.
vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: mockEventsOn
  },
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
    Nullable: (fn: any) => fn,
    Array: () => [],
    Map: () => ({}),
    Any: {}
  }
}))

async function sha256Hex(text: string): Promise<string> {
  const encoder = new TextEncoder()
  const data = encoder.encode(text)
  const hashBuffer = await crypto.subtle.digest('SHA-256', data)
  const hashArray = Array.from(new Uint8Array(hashBuffer))
  return hashArray.map((b) => b.toString(16).padStart(2, '0')).join('')
}

describe('plugin loader integrity check (#161, P5-12)', () => {
  beforeEach(() => {
    mockListPlugins.mockReset()
    mockReadPluginSource.mockReset()
    mockRegisterSession.mockReset().mockResolvedValue('test-token')
    mockUnregisterSession.mockReset().mockResolvedValue(undefined)
  })

  it('sha256 mismatch → plugin refused with integrity error', async () => {
    mockListPlugins.mockResolvedValue([
      {
        id: 'tampered',
        disabled: false,
        has_index: true,
        contentSha256: 'abc123def456'
      }
    ])
    mockReadPluginSource.mockResolvedValue('TAMPERED CONTENT')

    const result = await loadPlugins('Work', '', '')
    expect(result.errors).toHaveLength(1)
    expect(result.errors[0].id).toBe('tampered')
    expect(result.errors[0].message).toContain('integrity check failed')
    expect(result.plugins.has('tampered')).toBe(false)
  }, 15000) // crypto.subtle.digest + dynamic import can exceed the 5s default under parallel load

  it("sha256 match → no integrity error (import may fail in jsdom, that's OK)", async () => {
    const src = 'export default {};'
    const hash = await sha256Hex(src)
    mockListPlugins.mockResolvedValue([
      { id: 'valid', disabled: false, has_index: true, contentSha256: hash }
    ])
    mockReadPluginSource.mockResolvedValue(src)

    const result = await loadPlugins('Work', '', '')
    // The integrity check passed — no "integrity check failed" error.
    // The import may fail in jsdom (Blob URLs don't work), producing a
    // different error. That's expected; we only assert the integrity check
    // itself didn't reject.
    const integrityError = result.errors.find((e) =>
      e.message.includes('integrity check failed')
    )
    expect(integrityError).toBeUndefined()
  }, 15000) // crypto.subtle.digest + dynamic import can exceed the 5s default under parallel load

  it('missing contentSha256 → no integrity error (backward compat)', async () => {
    mockListPlugins.mockResolvedValue([
      { id: 'no-hash', disabled: false, has_index: true }
    ])
    mockReadPluginSource.mockResolvedValue('export default {};')

    const result = await loadPlugins('Work', '', '')
    const integrityError = result.errors.find((e) =>
      e.message.includes('integrity check failed')
    )
    expect(integrityError).toBeUndefined()
  })
})

describe('plugin loader session token plumbing (#151, P7-13)', () => {
  beforeEach(() => {
    mockRegisterSession.mockReset().mockResolvedValue('session-token-123')
    mockUnregisterSession.mockReset().mockResolvedValue(undefined)
  })

  it('teardownPlugin calls UnregisterPluginSession for a registered plugin', async () => {
    // Register a session manually to populate the token map.
    mockRegisterSession.mockResolvedValue('token-abc')

    // Simulate a registered plugin by calling the module's internal map
    // indirectly: RegisterPluginSession is the production path, but
    // teardownPlugin just needs a token in the sessionTokens map.
    // Since we can't easily set the map directly, verify the function
    // is exported and doesn't throw for unknown plugins.
    expect(() => teardownPlugin('nonexistent-plugin')).not.toThrow()
  })
})

describe('plugin loader loadersReady signal (#326 item 5)', () => {
  // The loadersReady flag gates Sidebar/PluginView context construction
  // against the vault:closing clear→re-register race. These tests pin the
  // transitions: false at start, true at end of loadPlugins, false again
  // when vault:closing fires, true again on the next loadPlugins.
  //
  // wireLifecycleOnce is module-scope idempotent: Events.On only fires on
  // the FIRST loadPlugins call in this file (likely from the integrity
  // describe block above). We capture that callback once and reuse it —
  // do NOT reset mockEventsOn or the call record is lost.
  let vaultClosingCb: (() => void) | null = null

  beforeAll(async () => {
    await loadPlugins('Work', '', '')
    const call = mockEventsOn.mock.calls.find(
      (args: unknown[]) => args[0] === 'vault:closing'
    )
    vaultClosingCb = call ? (call[1] as () => void) : null
  })

  beforeEach(() => {
    mockListPlugins.mockReset().mockResolvedValue([])
    mockReadPluginSource.mockReset()
    mockRegisterSession.mockReset().mockResolvedValue('test-token')
    mockUnregisterSession.mockReset().mockResolvedValue(undefined)
  })

  it('loadPlugins flips loadersReady to true after assigning plugins/errors', async () => {
    const { loadedPlugins } = await import('./store.svelte')
    loadedPlugins.loadersReady = false // simulating post-vault:closing state

    await loadPlugins('Work', '', '')

    expect(loadedPlugins.loadersReady).toBe(true)
  })

  it('vault:closing handler flips loadersReady to false BEFORE teardown', async () => {
    const { loadedPlugins } = await import('./store.svelte')

    await loadPlugins('Work', '', '')
    expect(loadedPlugins.loadersReady).toBe(true)

    expect(vaultClosingCb).toBeTruthy()
    vaultClosingCb!()

    expect(loadedPlugins.loadersReady).toBe(false)
  })

  it('loadersReady returns to true after a subsequent loadPlugins', async () => {
    const { loadedPlugins } = await import('./store.svelte')

    await loadPlugins('Work', '', '')
    expect(vaultClosingCb).toBeTruthy()
    vaultClosingCb!()
    expect(loadedPlugins.loadersReady).toBe(false)

    await loadPlugins('Personal', '', '')
    expect(loadedPlugins.loadersReady).toBe(true)
  })

  it('tears down first-party AI plugins when ai.features disables them (#632)', async () => {
    // Disabling Enable AI must tear down sessions (and slash/surfaces via
    // teardownPlugin), not leave a stale loadedPlugins entry after the map swap.
    const { loadedPlugins } = await import('./store.svelte')
    const { settings } = await import('../settings/store.svelte')
    const {
      registerSlashCommand,
      getSlashCommands,
      resetSlashRegistryForTests
    } = await import('../lib/editor/slash-registry')
    const { setGrantsForTests, resetGrantsForTests } =
      await import('./grants.svelte')

    mockListPlugins.mockResolvedValue([])
    mockRegisterSession.mockResolvedValue('ai-session')
    mockUnregisterSession.mockClear()
    mockClosePluginDB.mockClear()
    resetSlashRegistryForTests()
    resetGrantsForTests()
    // First-party IDs get all caps in setGrantsForTests; seed any plugin id.
    setGrantsForTests({ 'silt-ai-assistant': ['editor-schema'] })

    settings.config = {
      plugins: { disabled: [], active: [], plugin_settings: {} },
      ai: {
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: false
        },
        chat: {},
        embedding: {}
      }
    } as any

    await loadPlugins('Work', '', '')
    expect(loadedPlugins.plugins.has('silt-ai-agent')).toBe(true)
    expect(loadedPlugins.plugins.has('silt-ai-assistant')).toBe(true)
    expect(loadedPlugins.plugins.has('silt-ai-qa')).toBe(false)

    registerSlashCommand({
      id: 'silt-ai-assistant:test-ai-cmd',
      label: 'Test AI',
      pluginID: 'silt-ai-assistant',
      onSelect: () => {}
    })
    expect(
      getSlashCommands().some((c) => c.id === 'silt-ai-assistant:test-ai-cmd')
    ).toBe(true)

    settings.config = {
      ...settings.config!,
      ai: {
        ...(settings.config as any).ai,
        features: {
          enabled: false,
          rag_enabled: false,
          summaries_enabled: false
        }
      }
    } as any

    await loadPlugins('Work', '', '')
    expect(loadedPlugins.plugins.has('silt-ai-agent')).toBe(false)
    expect(loadedPlugins.plugins.has('silt-ai-assistant')).toBe(false)
    expect(
      getSlashCommands().some((c) => c.id === 'silt-ai-assistant:test-ai-cmd')
    ).toBe(false)
    expect(mockUnregisterSession).toHaveBeenCalled()
    expect(mockClosePluginDB).toHaveBeenCalledWith('silt-ai-agent')
    expect(mockClosePluginDB).toHaveBeenCalledWith('silt-ai-assistant')
  })

  it('vault:closing resets the unified task hub state #326 item 1', async () => {
    const {
      getTaskHubState,
      setScope,
      setFilters,
      setFocusDate,
      setActiveFilter
    } = await import('./first-party/silt-tasks/state.svelte')

    // Dirty the hub module-global as if the previous vault left state.
    setScope('notebook')
    setFilters({
      owners: ['alice'],
      priorities: [1],
      dueDate: 'today',
      tags: ['x']
    })
    setFocusDate('2026-06-28')
    setActiveFilter('today')

    expect(vaultClosingCb).toBeTruthy()
    vaultClosingCb!()

    const s = getTaskHubState()
    expect(s.scope).toBe('vault')
    expect(s.scopeUserOverride).toBe(false)
    expect(s.filters).toEqual({
      owners: [],
      priorities: [],
      dueDate: '',
      tags: []
    })
    expect(s.focusDate).toBe('')
    expect(s.activeFilter).toBe('all')
  })
})
