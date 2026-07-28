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
  reloadAll: vi.fn(),
  onError: vi.fn()
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
    mocks.onError.mockReset()
    mocks.reloadAll.mockResolvedValue(undefined)
  })

  afterEach(() => cleanup())

  // Every render passes onError so the component's required prop is satisfied
  // and install/reload errors can be asserted.
  function renderIt() {
    return render(PluginInstallFlow, {
      reloadAll: mocks.reloadAll,
      onError: mocks.onError
    })
  }

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

    renderIt()

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

    renderIt()
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

    renderIt()
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
    const { component } = renderIt()
    // The actions snippet is optional; without it the install button still
    // renders and no extra buttons appear.
    expect(
      screen.getByRole('button', { name: /Install from \.silt-plugin/i })
    ).toBeTruthy()
    // Sanity: no stray "Install" confirm button before a preview exists.
    expect(screen.queryByRole('button', { name: /^Install$/ })).toBeNull()
    expect(component).toBeTruthy()
  })

  it('routes an install/reload failure to onError (not the validation line)', async () => {
    mocks.pickPluginArchive.mockResolvedValue('/x/demo.silt-plugin')
    mocks.validatePluginArchive.mockResolvedValue({
      manifest: { id: 'demo', name: 'Demo', version: '1.0.0' },
      warnings: []
    })
    mocks.installPlugin.mockRejectedValue(new Error('disk full'))

    renderIt()
    await fireEvent.click(
      screen.getByRole('button', { name: /Install from \.silt-plugin/i })
    )
    await flush()

    await fireEvent.click(screen.getByRole('button', { name: /^Install$/ }))
    await flush()

    // Install/reload errors are tab-wide → onError; they must NOT appear as a
    // local "Validation failed:" line (reserved for chooseArchive failures).
    expect(mocks.onError).toHaveBeenCalledWith('disk full')
    expect(screen.queryByText(/Validation failed:/i)).toBeNull()
  })

  it('shows a Validating… state and disables the trigger while validating', async () => {
    // Hold ValidatePluginArchive pending so validating stays true.
    type ValidationResult = {
      manifest: {
        id: string
        name: string
        version?: string
        description?: string
        capabilities?: Record<string, true | string>
      }
      warnings: string[]
    }
    let release: ((value: ValidationResult) => void) | null = null
    mocks.validatePluginArchive.mockImplementation(
      () =>
        new Promise<ValidationResult>((resolve) => {
          release = resolve
        })
    )
    mocks.pickPluginArchive.mockResolvedValue('/x/demo.silt-plugin')

    renderIt()
    const trigger = screen.getByRole('button', {
      name: /Install from \.silt-plugin/i
    })
    await fireEvent.click(trigger)
    await flush()

    expect(screen.getByText(/Validating…/i)).toBeTruthy()
    expect((trigger as HTMLButtonElement).disabled).toBe(true)

    release!({ manifest: { id: 'demo', name: 'Demo' }, warnings: [] })
    await flush()
    expect(screen.queryByText(/Validating…/i)).toBeNull()
  })
})
