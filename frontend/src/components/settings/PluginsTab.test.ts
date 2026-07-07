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
  setConfig: (next: any) => {
    mocks.configNoPlugins = next
  }
}))

vi.mock('../../../wailsjs/go/main/App.js', () => ({
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
  GetNetworkAudit: mocks.getNetworkAudit
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
    mocks.listPlugins.mockResolvedValue([])
    mocks.loadPlugins.mockResolvedValue(undefined)
    mocks.saveConfig.mockResolvedValue(true)
    mocks.getGrantedCapabilities.mockResolvedValue({})
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

    // The Disable toggle is a button with aria-label="Disable" inside the
    // first-party card row. Click it; without the guard, this throws
    // "Cannot read properties of undefined (reading 'disabled')".
    const disableBtn = screen.getByRole('button', { name: 'Disable' })

    await expect(fireEvent.click(disableBtn)).resolves.not.toThrow()

    // saveConfig must have been called with a normalized config that
    // includes the disabled plugin id.
    expect(mocks.saveConfig).toHaveBeenCalledTimes(1)
    const saved = mocks.saveConfig.mock.calls[0][0]
    expect(saved.plugins).toBeTruthy()
    expect(saved.plugins.disabled).toContain('silt-tasks')
  })
})

// #447: when an enabled AI-capable plugin has no chat model configured,
// the Plugins tab surfaces an "AI setup needed" nudge that links to the
// AI Provider tab. These tests cover the render condition, the click
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
    mocks.checkPluginUpdate.mockReset()
    mocks.getNetworkAudit.mockReset()
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
