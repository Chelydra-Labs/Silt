import { beforeEach, describe, expect, it, vi } from 'vitest'

// The five IPC actions backing the dialog confirm/decline/unlink/relink flows.
const mocks = vi.hoisted(() => ({
  ConfirmSettingsChange: vi.fn().mockResolvedValue(undefined),
  ConfirmGrantsMigration: vi.fn().mockResolvedValue(undefined),
  DeclineGrantsMigration: vi.fn().mockResolvedValue(undefined),
  PickLinkedNotebook: vi.fn().mockResolvedValue(undefined),
  UnlinkNotebook: vi.fn().mockResolvedValue(undefined),
  pushNotification: vi.fn()
}))

vi.mock('$silt-app', () =>
  createAppIpcMocks({
    ConfirmSettingsChange: mocks.ConfirmSettingsChange,
    ConfirmGrantsMigration: mocks.ConfirmGrantsMigration,
    DeclineGrantsMigration: mocks.DeclineGrantsMigration,
    PickLinkedNotebook: mocks.PickLinkedNotebook,
    UnlinkNotebook: mocks.UnlinkNotebook
  })
)

// pushNotification is imported directly by the controller; stub it so error
// paths are observable without the real notification store.
vi.mock('../notifications/store.svelte', () => ({
  pushNotification: mocks.pushNotification
}))

import { createSettingsDialogs } from './useSettingsDialogs.svelte'

describe('useSettingsDialogs (#768)', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('open/close mutators toggle the settings-mismatch flag', () => {
    const d = createSettingsDialogs()
    expect(d.showSettingsMismatch).toBe(false)
    d.openSettingsMismatch()
    expect(d.showSettingsMismatch).toBe(true)
    d.closeSettingsMismatch()
    expect(d.showSettingsMismatch).toBe(false)
  })

  it('openGrantsMigration stashes the legacy grants payload + opens', () => {
    const d = createSettingsDialogs()
    const grants = { 'silt-journal': { 'file:read': '*' } }
    d.openGrantsMigration(grants)
    expect(d.showGrantsMigration).toBe(true)
    expect(d.pendingLegacyGrants).toEqual(grants)
  })

  it('confirmSettingsMismatch calls ConfirmSettingsChange then closes', async () => {
    const d = createSettingsDialogs()
    d.openSettingsMismatch()
    await d.confirmSettingsMismatch()
    expect(mocks.ConfirmSettingsChange).toHaveBeenCalledOnce()
    expect(d.showSettingsMismatch).toBe(false)
    expect(mocks.pushNotification).not.toHaveBeenCalled()
  })

  it('confirmSettingsMismatch surfaces a notification on IPC failure', async () => {
    mocks.ConfirmSettingsChange.mockRejectedValueOnce(new Error('locked'))
    const d = createSettingsDialogs()
    d.openSettingsMismatch()
    await d.confirmSettingsMismatch()
    // Stays open on failure.
    expect(d.showSettingsMismatch).toBe(true)
    expect(mocks.pushNotification).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'error' })
    )
  })

  it('declineGrantsMigration persists the decline and closes even on error', async () => {
    mocks.DeclineGrantsMigration.mockRejectedValueOnce(new Error('boom'))
    const d = createSettingsDialogs()
    d.openGrantsMigration({})
    await d.declineGrantsMigration()
    expect(mocks.DeclineGrantsMigration).toHaveBeenCalledOnce()
    // Close is unconditional — a non-fatal decline must not strand the modal.
    expect(d.showGrantsMigration).toBe(false)
  })

  it('confirmGrantsMigration forwards the pending grants + closes', async () => {
    const d = createSettingsDialogs()
    const grants = { p: { 'file:read': '*' } }
    d.openGrantsMigration(grants)
    await d.confirmGrantsMigration()
    expect(mocks.ConfirmGrantsMigration).toHaveBeenCalledWith(grants)
    expect(d.showGrantsMigration).toBe(false)
  })

  it('handleUnlinkNotebook calls UnlinkNotebook + drops the entry', async () => {
    const d = createSettingsDialogs()
    d.setQuarantinedLinks([
      { id: 'a', display_name: 'A', root_path: '/a' },
      { id: 'b', display_name: 'B', root_path: '/b' }
    ])
    await d.handleUnlinkNotebook('a')
    expect(mocks.UnlinkNotebook).toHaveBeenCalledWith('a')
    expect(d.quarantinedLinks.map((l) => l.id)).toEqual(['b'])
  })

  it('handleRelinkNotebook unlinks then re-picks + drops the entry', async () => {
    const d = createSettingsDialogs()
    d.setQuarantinedLinks([{ id: 'a', display_name: 'A', root_path: '/a' }])
    await d.handleRelinkNotebook('a')
    expect(mocks.UnlinkNotebook).toHaveBeenCalledWith('a')
    expect(mocks.PickLinkedNotebook).toHaveBeenCalledOnce()
    expect(d.quarantinedLinks).toHaveLength(0)
  })

  it('handleRelinkNotebook surfaces a notification on failure', async () => {
    mocks.UnlinkNotebook.mockRejectedValueOnce(new Error('fs'))
    const d = createSettingsDialogs()
    d.setQuarantinedLinks([{ id: 'a', display_name: 'A', root_path: '/a' }])
    await d.handleRelinkNotebook('a')
    expect(mocks.pushNotification).toHaveBeenCalledWith(
      expect.objectContaining({ kind: 'error' })
    )
    // Entry stays on failure so the user can retry.
    expect(d.quarantinedLinks).toHaveLength(1)
  })
})
