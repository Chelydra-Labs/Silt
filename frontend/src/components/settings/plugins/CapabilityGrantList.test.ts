/**
 * Isolated unit tests for CapabilityGrantList (#765). The component owns the
 * per-card grant/revoke flow; here we render it directly. The integration
 * guard (rendering through PluginsTab) lives in PluginsTab.test.ts.
 */
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import { tick } from 'svelte'
import type { ComponentProps } from 'svelte'
import CapabilityGrantList from './CapabilityGrantList.svelte'
import type { Card } from './types'

const mocks = vi.hoisted(() => ({
  requestCapability: vi.fn(),
  revokeCapability: vi.fn(),
  onRefresh: vi.fn(),
  onError: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    RequestCapability: mocks.requestCapability,
    RevokeCapability: mocks.revokeCapability
  })
)

type GrantProps = ComponentProps<typeof CapabilityGrantList>

const diskCard: Card = {
  id: 'cap-plugin',
  name: 'Cap Plugin',
  version: '1.0.0',
  author: 'Test',
  description: '',
  icon: 'extension',
  source: 'disk',
  disabled: false,
  hasIndex: true,
  requestedCapabilities: { network: true, 'read-files': true }
}

async function flush() {
  await tick()
  await new Promise((r) => setTimeout(r, 0))
}

describe('CapabilityGrantList', () => {
  beforeEach(() => {
    mocks.requestCapability.mockReset()
    mocks.revokeCapability.mockReset()
    mocks.onRefresh.mockReset()
    mocks.onError.mockReset()
    mocks.requestCapability.mockResolvedValue(undefined)
    mocks.revokeCapability.mockResolvedValue(undefined)
    mocks.onRefresh.mockResolvedValue(undefined)
  })

  afterEach(() => cleanup())

  function renderIt(overrides: Partial<GrantProps> = {}) {
    return render(CapabilityGrantList, {
      card: diskCard,
      onRefresh: mocks.onRefresh,
      onError: mocks.onError,
      ...overrides
    })
  }

  it('grants an ungranted capability via RequestCapability then refreshes', async () => {
    renderIt()

    // network is ungranted → Grant button present.
    await fireEvent.click(
      screen.getByRole('button', { name: 'Grant Network access' })
    )
    await flush()

    // qual `true` serializes to ''.
    expect(mocks.requestCapability).toHaveBeenCalledWith(
      'cap-plugin',
      'network',
      ''
    )
    expect(mocks.onRefresh).toHaveBeenCalled()
    // Optimistic clear at the start of the grant.
    expect(mocks.onError).toHaveBeenCalledWith('')
  })

  it('revokes a granted capability via RevokeCapability', async () => {
    renderIt({
      card: {
        ...diskCard,
        grantedCapabilities: { network: 'granted' }
      }
    })

    await fireEvent.click(
      screen.getByRole('button', { name: 'Revoke Network access' })
    )
    await flush()

    expect(mocks.revokeCapability).toHaveBeenCalledWith('cap-plugin', 'network')
    expect(mocks.onRefresh).toHaveBeenCalled()
  })

  it('shows "trusted" (no Grant/Revoke) for first-party plugins', async () => {
    renderIt({
      card: { ...diskCard, source: 'first-party' }
    })

    // First-party plugins render "trusted" for every requested capability
    // (two here: network + read-files) and no Grant/Revoke controls.
    expect(screen.getAllByText('trusted').length).toBe(2)
    expect(screen.queryByRole('button', { name: /Grant/i })).toBeNull()
    expect(screen.queryByRole('button', { name: /Revoke/i })).toBeNull()
  })

  it('disables the in-flight capability button while a grant is pending', async () => {
    let releaseGrant: () => void = () => {}
    mocks.requestCapability.mockImplementation(
      () =>
        new Promise<void>((resolve) => {
          releaseGrant = resolve
        })
    )

    renderIt()
    const grantBtn = screen.getByRole('button', {
      name: 'Grant Network access'
    }) as HTMLButtonElement

    await fireEvent.click(grantBtn)
    await tick()

    // In-flight: the network Grant button is disabled; the read-files Grant
    // button (different cap) is still enabled because grantBusy is per-cap.
    expect(grantBtn.disabled).toBe(true)
    const readFilesBtn = screen.getByRole('button', {
      name: 'Grant Read notebook files'
    }) as HTMLButtonElement
    expect(readFilesBtn.disabled).toBe(false)

    releaseGrant()
    await waitFor(() => {
      expect(grantBtn.disabled).toBe(false)
    })
  })

  it('reports a grant failure through onError', async () => {
    mocks.requestCapability.mockRejectedValue(new Error('permission denied'))

    renderIt()
    await fireEvent.click(
      screen.getByRole('button', { name: 'Grant Network access' })
    )
    await flush()

    expect(mocks.onError).toHaveBeenLastCalledWith('permission denied')
  })

  it('renders nothing when the card has no requested capabilities', async () => {
    renderIt({
      card: { ...diskCard, requestedCapabilities: undefined }
    })
    await tick()
    expect(screen.queryByRole('list')).toBeNull()
  })
})
