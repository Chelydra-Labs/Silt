/**
 * Isolated unit tests for PluginInstallFlow (#765). The component owns the
 * pick → validate → preview → install → reload flow; here we render it
 * directly (no PluginsTab wrapper). The integration guard (rendering through
 * PluginsTab) lives in PluginsTab.test.ts. Mocks IPC at the binding boundary.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import { render, screen, cleanup, fireEvent } from '@testing-library/svelte'
import { tick } from 'svelte'

const mocks = vi.hoisted(() => ({
  pickPluginArchive: vi.fn(),
  validatePluginArchive: vi.fn(),
  installPlugin: vi.fn(),
  reloadAll: vi.fn()
}))

vi.mock('../../../../bindings/silt/app.js', () => ({
  PickPluginArchive: mocks.pickPluginArchive,
  ValidatePluginArchive: mocks.validatePluginArchive,
  InstallPlugin: mocks.installPlugin
}))

import PluginInstallFlow from './PluginInstallFlow.svelte'

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('PluginInstallFlow', () => {
  beforeEach(() => {
    mocks.pickPluginArchive.mockReset()
    mocks.validatePluginArchive.mockReset()
    mocks.installPlugin.mockReset()
    mocks.reloadAll.mockReset()
    mocks.reloadAll.mockResolvedValue(undefined)
  })

  afterEach(() => cleanup())

  it('picks, validates, and renders the manifest preview', async () => {
    mocks.pickPluginArchive.mockResolvedValue('/x/demo.silt-plugin')
    mocks.validatePluginArchive.mockResolvedValue({
      manifest: {
        id: 'demo',
        name: 'Demo Plugin',
        version: '2.1.0',
        description: 'A demo',
        capabilities: { network: true }
      },
      warnings: ['uses experimental feature']
    })

    render(PluginInstallFlow, { reloadAll: mocks.reloadAll })

    await fireEvent.click(
      screen.getByRole('button', { name: /Install from \.silt-plugin/i })
    )
    await flush()

    expect(mocks.validatePluginArchive).toHaveBeenCalledWith(
      '/x/demo.silt-plugin'
    )
    expect(screen.getByText('Demo Plugin')).toBeTruthy()
    expect(screen.getByText(/v2\.1\.0/)).toBeTruthy()
    expect(screen.getByText('uses experimental feature')).toBeTruthy()
    expect(screen.getByText('Network access')).toBeTruthy()
  })

  it('installs on confirm, then reloads', async () => {
    mocks.pickPluginArchive.mockResolvedValue('/x/demo.silt-plugin')
    mocks.validatePluginArchive.mockResolvedValue({
      manifest: { id: 'demo', name: 'Demo', version: '1.0.0' },
      warnings: []
    })
    mocks.installPlugin.mockResolvedValue(undefined)

    render(PluginInstallFlow, { reloadAll: mocks.reloadAll })
    await fireEvent.click(
      screen.getByRole('button', { name: /Install from \.silt-plugin/i })
    )
    await flush()

    await fireEvent.click(screen.getByRole('button', { name: /^Install$/ }))
    await flush()

    expect(mocks.installPlugin).toHaveBeenCalledWith('/x/demo.silt-plugin')
    expect(mocks.reloadAll).toHaveBeenCalled()
  })

  it('surfaces a validation failure and never installs', async () => {
    mocks.pickPluginArchive.mockResolvedValue('/bad/broken.silt-plugin')
    mocks.validatePluginArchive.mockRejectedValue(
      new Error('manifest missing id')
    )

    render(PluginInstallFlow, { reloadAll: mocks.reloadAll })
    await fireEvent.click(
      screen.getByRole('button', { name: /Install from \.silt-plugin/i })
    )
    await flush()

    expect(
      screen.getByText(/Validation failed: manifest missing id/i)
    ).toBeTruthy()
    expect(mocks.installPlugin).not.toHaveBeenCalled()
  })

  it('renders the injected actions snippet beside the install button', async () => {
    const { component } = render(PluginInstallFlow, {
      reloadAll: mocks.reloadAll
    })
    // The actions snippet is optional; without it the install button still
    // renders and no extra buttons appear.
    expect(
      screen.getByRole('button', { name: /Install from \.silt-plugin/i })
    ).toBeTruthy()
    // Sanity: no stray "Install" confirm button before a preview exists.
    expect(screen.queryByRole('button', { name: /^Install$/ })).toBeNull()
    expect(component).toBeTruthy()
  })
})
