import { describe, expect, it, beforeEach, afterEach, vi } from 'vitest'
import { tick } from 'svelte'
import { render, screen, cleanup } from '@testing-library/svelte'
import type { RegisteredPlugin } from '../plugins/sdk'

// PluginView reads from loadedPlugins (plugins/store.svelte) and builds a
// PluginContext via makePluginContext (plugins/context). We mock both to
// avoid real Wails IPC. loadersReady defaults to true so existing tests
// exercise the happy path; the suspend test flips it to false (#326 item 5).
const mocks = vi.hoisted(() => ({
  loadedPlugins: {
    plugins: new Map<string, RegisteredPlugin>(),
    errors: [] as { id: string; message: string }[],
    loadersReady: true
  }
}))

// Spy on the ctx-construction path so the suspend test can assert it is
// skipped entirely when loadersReady is false (#326 item 5).
const mockMakePluginContext = vi.hoisted(() =>
  vi.fn(() => ({ pluginID: 'test', sessionToken: 'tok' }))
)
const mockGetSessionToken = vi.hoisted(() => vi.fn(() => 'tok'))

vi.mock('../../../wailsjs/go/main/App.js', () => ({
  PluginRawQuery: vi.fn(),
  PluginMutateBlock: vi.fn(),
  PluginUpdateBlockState: vi.fn()
}))
vi.mock('../plugins/store.svelte', () => ({
  loadedPlugins: mocks.loadedPlugins
}))
vi.mock('../plugins/context', () => ({
  makePluginContext: mockMakePluginContext
}))
vi.mock('../plugins/loader', () => ({
  getSessionToken: mockGetSessionToken
}))

import PluginView from './PluginView.svelte'

// A dummy component that just renders its manifest name, so we can tell
// PluginView successfully resolved and rendered the plugin.
function DummyPlugin() {}
DummyPlugin.prototype = Object.create(HTMLElement.prototype)

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('PluginView', () => {
  beforeEach(() => {
    mocks.loadedPlugins.plugins = new Map()
    mocks.loadedPlugins.errors = []
    mocks.loadedPlugins.loadersReady = true
  })

  afterEach(() => {
    cleanup()
  })

  it('shows the not-registered empty state for an unknown plugin id', async () => {
    render(PluginView, {
      props: {
        pluginId: 'silt-nonexistent',
        activeNotebook: 'Work',
        activeSection: '',
        activePage: ''
      }
    })
    await flush()

    expect(screen.getByText(/plugin not registered/i)).toBeInTheDocument()
  })

  it('shows a load-error message when the plugin has an error', async () => {
    mocks.loadedPlugins.errors = [
      { id: 'silt-broken', message: 'Syntax error in index.js' }
    ]
    render(PluginView, {
      props: {
        pluginId: 'silt-broken',
        activeNotebook: 'Work',
        activeSection: '',
        activePage: ''
      }
    })
    await flush()

    expect(screen.getByText(/Plugin failed to load/i)).toBeInTheDocument()
    expect(screen.getByText(/Syntax error in index.js/)).toBeInTheDocument()
  })

  it('renders the registered plugin component on the happy path', async () => {
    // Register a mock plugin that renders a distinctive marker.
    mocks.loadedPlugins.plugins.set('silt-test', {
      manifest: { id: 'silt-test', name: 'Test Plugin', version: '1.0.0' },
      component: DummyPlugin,
      source: 'first-party'
    })
    render(PluginView, {
      props: {
        pluginId: 'silt-test',
        activeNotebook: 'Work',
        activeSection: 'Journal',
        activePage: 'Daily'
      }
    })
    await flush()

    // No error or empty-state shown.
    expect(screen.queryByText(/plugin not registered/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/Plugin failed to load/i)).not.toBeInTheDocument()
  })

  it('loadersReady=false suspends ctx construction (no makePluginContext) (#326 item 5)', async () => {
    // During vault:closing's clear→re-register window, getSessionToken
    // would return undefined. The gate makes the derived return null until
    // loadersReady flips back to true, so makePluginContext is never called
    // with an empty token.
    mocks.loadedPlugins.plugins.set('silt-test', {
      manifest: { id: 'silt-test', name: 'Test Plugin', version: '1.0.0' },
      component: DummyPlugin,
      source: 'first-party'
    })
    mocks.loadedPlugins.loadersReady = false
    mockMakePluginContext.mockClear()
    mockGetSessionToken.mockClear()

    render(PluginView, {
      props: {
        pluginId: 'silt-test',
        activeNotebook: 'Work',
        activeSection: '',
        activePage: ''
      }
    })
    await flush()

    // The plugin exists but its ctx is suspended — neither constructor ran.
    expect(mockMakePluginContext).not.toHaveBeenCalled()
    expect(mockGetSessionToken).not.toHaveBeenCalled()
    // No error / empty-state either; the plugin simply doesn't render.
    expect(screen.queryByText(/plugin not registered/i)).not.toBeInTheDocument()
    expect(screen.queryByText(/Plugin failed to load/i)).not.toBeInTheDocument()
  })
})
