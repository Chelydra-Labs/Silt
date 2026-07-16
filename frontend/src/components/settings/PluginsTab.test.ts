// PluginsTab regression: cfg.plugins may be undefined when a hand-edited
// config.yaml omits the section. Without the guard added in the Sprint 4
// PR review, the toggle path would throw a TypeError on
// `cfg.plugins.disabled`. This test ensures the disabled-first-party flow
// is defensive against that schema drift.

import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'

const mocks = vi.hoisted(() => ({
  listPlugins: vi.fn(),
  loadPlugins: vi.fn(),
  // First-party list mirrors what the real registry exports (a getter).
  firstPartyPluginsFn: vi.fn(() => [
    {
      manifest: {
        id: 'silt-tasks',
        name: 'Tasks',
        version: '1.0.0',
        author: 'Silt',
        description: '',
        icon: 'checklist'
      }
    }
  ]),
  loadedPlugins: {
    plugins: new Map(),
    errors: [] as { id: string; message: string }[]
  },
  // Mutable config (no `plugins` key) to exercise the guard.
  configNoPlugins: {} as any,
  saveConfig: vi.fn(),
  getGrantedCapabilities: vi.fn().mockResolvedValue({}),
  teardownPlugin: vi.fn(),
  // PluginsTab.svelte imports these but the disable-guard test never
  // exercises them; they're mocked so module resolution is total.
  checkPluginUpdate: vi.fn(),
  getNetworkAudit: vi.fn(),
  getPluginSecurityStats: vi.fn().mockResolvedValue([]),
  // Matches the real Events.On signature: (eventName, callback) => cancel.
  // Declaring the arity here keeps mockImplementation callers type-safe.
  eventsOn: vi.fn((_name: string, _cb: () => void) => () => {}),
  setConfig: (next: any) => {
    mocks.configNoPlugins = next
  }
}))

vi.mock('../../../bindings/silt/app.js', () => ({
  ListPlugins: mocks.listPlugins,
  ValidatePluginArchive: vi.fn(),
  InstallPlugin: vi.fn(),
  UninstallPlugin: vi.fn(),
  EnablePlugin: vi.fn(),
  DisablePlugin: vi.fn(),
  PickPluginArchive: vi.fn(),
  RequestCapability: vi.fn(),
  RevokeCapability: vi.fn(),
  GetGrantedCapabilities: mocks.getGrantedCapabilities,
  CheckPluginUpdate: mocks.checkPluginUpdate,
  GetNetworkAudit: mocks.getNetworkAudit,
  GetPluginSecurityStats: mocks.getPluginSecurityStats
}))

vi.mock('@wailsio/runtime', () => ({
  Events: {
    On: mocks.eventsOn
  }
}))

vi.mock('../../plugins/loader', () => ({
  loadPlugins: mocks.loadPlugins,
  teardownPlugin: mocks.teardownPlugin
}))

vi.mock('../../plugins/registry', () => ({
  firstPartyPlugins: mocks.firstPartyPluginsFn
}))

vi.mock('../../plugins/store.svelte', () => ({
  loadedPlugins: mocks.loadedPlugins
}))

vi.mock('../../settings/store.svelte', () => ({
  settings: {
    get config() {
      return mocks.configNoPlugins
    }
  },
  saveConfig: mocks.saveConfig
}))

import PluginsTab from './PluginsTab.svelte'

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('PluginsTab first-party disable guard', () => {
  beforeEach(() => {
    mocks.listPlugins.mockReset()
    mocks.loadPlugins.mockReset()
    mocks.saveConfig.mockReset()
    mocks.getGrantedCapabilities.mockReset()
    mocks.getPluginSecurityStats.mockReset()
    mocks.listPlugins.mockResolvedValue([])
    mocks.loadPlugins.mockResolvedValue(undefined)
    mocks.saveConfig.mockResolvedValue(true)
    mocks.getGrantedCapabilities.mockResolvedValue({})
    mocks.getPluginSecurityStats.mockResolvedValue([])
    mocks.configNoPlugins = {} // no `plugins` key
  })

  afterEach(() => {
    cleanup()
  })

  it('does not throw when toggling a first-party plugin and cfg.plugins is missing', async () => {
    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily'
    })
    await flush()

    // Locate the Tasks card (the only first-party plugin in the mock).
    const kanbanCard = screen.getByText('Tasks').closest('div')
    expect(kanbanCard).toBeTruthy()

    // The Disable toggle is a button with an aria-label of "<plugin>:
    // Disable" (prefixed with the plugin name so SR users can attribute it
    // among many cards). Click it; without the guard, this throws
    // "Cannot read properties of undefined (reading 'disabled')".
    const disableBtn = screen.getByRole('button', { name: /: Disable$/ })

    await expect(fireEvent.click(disableBtn)).resolves.not.toThrow()

    // saveConfig must have been called with a normalized config that
    // includes the disabled plugin id.
    expect(mocks.saveConfig).toHaveBeenCalledTimes(1)
    const saved = mocks.saveConfig.mock.calls[0][0]
    expect(saved.plugins).toBeTruthy()
    expect(saved.plugins.disabled).toContain('silt-tasks')
  })
})

// #632: first-party AI modules are managed under Settings → AI, not via
// independent Plugins enable toggles.
describe('PluginsTab first-party AI managed enablement', () => {
  beforeEach(() => {
    mocks.listPlugins.mockReset()
    mocks.loadPlugins.mockReset()
    mocks.saveConfig.mockReset()
    mocks.getGrantedCapabilities.mockReset()
    mocks.getPluginSecurityStats.mockReset()
    mocks.checkPluginUpdate.mockReset()
    mocks.getNetworkAudit.mockReset()
    mocks.getPluginSecurityStats.mockResolvedValue([])
    mocks.listPlugins.mockResolvedValue([])
    mocks.loadPlugins.mockResolvedValue(undefined)
    mocks.saveConfig.mockResolvedValue(true)
    mocks.getGrantedCapabilities.mockResolvedValue({})
    mocks.configNoPlugins = {
      ai: {
        features: {
          enabled: true,
          rag_enabled: false,
          summaries_enabled: false
        },
        chat: { model: 'm', provider_type: 'local' }
      },
      plugins: { disabled: [], plugin_settings: {} }
    }
    mocks.firstPartyPluginsFn.mockReturnValue([
      {
        manifest: {
          id: 'silt-ai-agent',
          name: 'Silt AI Agent',
          version: '1.0.0',
          author: 'Silt',
          description: 'Agent tools',
          icon: 'smart_toy'
        }
      },
      {
        manifest: {
          id: 'silt-tasks',
          name: 'Tasks',
          version: '1.0.0',
          author: 'Silt',
          description: '',
          icon: 'checklist'
        }
      }
    ])
  })

  afterEach(() => {
    cleanup()
    mocks.firstPartyPluginsFn.mockReturnValue([
      {
        manifest: {
          id: 'silt-tasks',
          name: 'Tasks',
          version: '1.0.0',
          author: 'Silt',
          description: '',
          icon: 'checklist'
        }
      }
    ])
  })

  it('shows Managed in Settings → AI instead of an enable toggle for silt-ai-*', async () => {
    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab: vi.fn()
    })
    await flush()

    expect(
      screen.getByRole('button', {
        name: /Silt AI Agent: managed in AI settings/i
      })
    ).toBeTruthy()
    expect(
      screen.queryByRole('button', { name: /Silt AI Agent: (Enable|Disable)/i })
    ).toBeNull()
    // Non-AI first-party still has enable/disable.
    expect(
      screen.getByRole('button', { name: /Tasks: (Enable|Disable)/i })
    ).toBeTruthy()
  })
})

// #447: when an enabled AI-capable plugin has no chat model configured,
// the Plugins tab surfaces an "AI setup needed" nudge that links to the
// AI settings tab. These tests cover the render condition, the click
// handler, and the graceful-degradation path when onSwitchTab is absent.
describe('PluginsTab AI setup nudge', () => {
  // Disk plugin shapes used across the tests. `grantsFor` is what
  // GetGrantedCapabilities returns for the plugin id map.
  const aiPlugin = {
    id: 'ai-plugin',
    name: 'AI Helper',
    version: '1.0.0',
    author: 'Test',
    description: '',
    icon: 'extension',
    capabilities: { ai: true }
  }
  const networkPlugin = {
    id: 'net-plugin',
    name: 'Net Helper',
    version: '1.0.0',
    author: 'Test',
    description: '',
    icon: 'extension',
    capabilities: { network: true }
  }

  beforeEach(() => {
    mocks.listPlugins.mockReset()
    mocks.loadPlugins.mockReset()
    mocks.saveConfig.mockReset()
    mocks.getGrantedCapabilities.mockReset()
    mocks.getPluginSecurityStats.mockReset()
    mocks.checkPluginUpdate.mockReset()
    mocks.getNetworkAudit.mockReset()
    mocks.getPluginSecurityStats.mockResolvedValue([])
    mocks.listPlugins.mockResolvedValue([])
    mocks.loadPlugins.mockResolvedValue(undefined)
    mocks.saveConfig.mockResolvedValue(true)
    mocks.getGrantedCapabilities.mockResolvedValue({})
    mocks.configNoPlugins = {}
  })

  afterEach(() => {
    cleanup()
  })

  it('renders the badge when an enabled AI-capable plugin has no chat model', async () => {
    mocks.listPlugins.mockResolvedValue([aiPlugin])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'ai-plugin': { ai: 'granted' }
    })
    mocks.setConfig({
      ai: { chat: { model: '' }, embedding: { model: '' } }
    })

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab: vi.fn()
    })
    await flush()

    expect(
      screen.getByRole('button', { name: /AI setup needed/i })
    ).toBeTruthy()
  })

  it('calls onSwitchTab("ai") when the badge is clicked', async () => {
    mocks.listPlugins.mockResolvedValue([aiPlugin])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'ai-plugin': { ai: 'granted' }
    })
    mocks.setConfig({
      ai: { chat: { model: '' }, embedding: { model: '' } }
    })

    const onSwitchTab = vi.fn()
    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab
    })
    await flush()

    const badge = screen.getByRole('button', { name: /AI setup needed/i })
    await fireEvent.click(badge)

    expect(onSwitchTab).toHaveBeenCalledWith('ai')
  })

  it('hides the badge once a chat model is configured', async () => {
    mocks.listPlugins.mockResolvedValue([aiPlugin])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'ai-plugin': { ai: 'granted' }
    })
    mocks.setConfig({
      ai: { chat: { model: 'llama3.1' }, embedding: { model: '' } }
    })

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab: vi.fn()
    })
    await flush()

    expect(
      screen.queryByRole('button', { name: /AI setup needed/i })
    ).toBeNull()
  })

  it('hides the badge for a disabled AI-capable plugin', async () => {
    mocks.listPlugins.mockResolvedValue([{ ...aiPlugin, disabled: true }])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'ai-plugin': { ai: 'granted' }
    })
    mocks.setConfig({
      ai: { chat: { model: '' }, embedding: { model: '' } }
    })

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab: vi.fn()
    })
    await flush()

    expect(
      screen.queryByRole('button', { name: /AI setup needed/i })
    ).toBeNull()
  })

  it('hides the badge for a plugin without the ai capability', async () => {
    mocks.listPlugins.mockResolvedValue([networkPlugin])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'net-plugin': { network: 'granted' }
    })
    mocks.setConfig({
      ai: { chat: { model: '' }, embedding: { model: '' } }
    })

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab: vi.fn()
    })
    await flush()

    expect(
      screen.queryByRole('button', { name: /AI setup needed/i })
    ).toBeNull()
  })

  it('renders the badge disabled (non-interactive) when onSwitchTab is absent', async () => {
    mocks.listPlugins.mockResolvedValue([aiPlugin])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'ai-plugin': { ai: 'granted' }
    })
    mocks.setConfig({
      ai: { chat: { model: '' }, embedding: { model: '' } }
    })

    // No onSwitchTab prop: graceful degradation, badge is discoverable but
    // non-operable (native disabled).
    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily'
    })
    await flush()

    const badge = screen.getByRole('button', {
      name: /AI setup needed/i
    }) as HTMLButtonElement
    expect(badge.disabled).toBe(true)
  })

  it('does not render the badge while config is unloaded (null)', async () => {
    mocks.listPlugins.mockResolvedValue([aiPlugin])
    mocks.getGrantedCapabilities.mockResolvedValue({
      'ai-plugin': { ai: 'granted' }
    })
    // settings.config is null before initial load completes — the badge must
    // not flash in spuriously (#447 hardening).
    mocks.setConfig(null as any)

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily',
      onSwitchTab: vi.fn()
    })
    await flush()

    expect(
      screen.queryByRole('button', { name: /AI setup needed/i })
    ).toBeNull()
  })
})

describe('PluginsTab security stats badge (#518)', () => {
  beforeEach(() => {
    mocks.listPlugins.mockReset()
    mocks.getGrantedCapabilities.mockReset()
    mocks.getPluginSecurityStats.mockReset()
    mocks.checkPluginUpdate.mockReset()
    mocks.listPlugins.mockResolvedValue([
      {
        id: 'noisy',
        name: 'Noisy Plugin',
        version: '1.0.0',
        author: 'X',
        description: '',
        icon: 'extension',
        capabilities: { network: true }
      }
    ])
    mocks.getGrantedCapabilities.mockResolvedValue({})
    mocks.getPluginSecurityStats.mockResolvedValue([])
    mocks.configNoPlugins = {
      plugins: { active: [], disabled: [], plugin_settings: {} }
    }
  })

  afterEach(() => {
    cleanup()
  })

  it('shows a denial/rate-limit badge when security stats are non-zero', async () => {
    mocks.getPluginSecurityStats.mockResolvedValue([
      {
        pluginId: 'noisy',
        denials: 3,
        rateLimited: 2,
        lastCapability: 'network'
      }
    ])

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily'
    })
    await flush()

    const badge = screen.getByRole('status', {
      name: /3 capability denials.*2 rate-limit hits/i
    })
    expect(badge).toBeTruthy()
    expect(badge.textContent).toMatch(/3 denied/i)
    expect(badge.textContent).toMatch(/2 limited/i)
  })

  it('hides the badge when stats are empty', async () => {
    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily'
    })
    await flush()

    expect(screen.queryByRole('status', { name: /denied|limited/i })).toBeNull()
  })

  it('subscribes to security:event and refreshes the badge live', async () => {
    let securityHandler: (() => void) | undefined
    mocks.eventsOn.mockImplementation((name: string, cb: () => void) => {
      if (name === 'security:event') securityHandler = cb
      return () => {}
    })
    mocks.getPluginSecurityStats.mockResolvedValue([])

    render(PluginsTab, {
      activeNotebook: 'Work',
      activeSection: 'Journal',
      activePage: 'Daily'
    })
    await flush()

    expect(mocks.eventsOn).toHaveBeenCalledWith(
      'security:event',
      expect.any(Function)
    )
    expect(securityHandler).toBeTypeOf('function')
    expect(screen.queryByRole('status', { name: /denied|limited/i })).toBeNull()

    mocks.getPluginSecurityStats.mockResolvedValue([
      {
        pluginId: 'noisy',
        denials: 1,
        rateLimited: 0,
        lastCapability: 'network'
      }
    ])
    securityHandler!()
    await flush()

    expect(
      screen.getByRole('status', { name: /1 capability denial/i })
    ).toBeTruthy()
  })
})
