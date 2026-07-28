// Settings / quarantine dialog controller (#768).
//
// Owns the three modal-overlay states + their IPC action handlers that lived
// inline in App.svelte: the settings fingerprint-mismatch confirm, the legacy
// grants migration confirm/decline, and the quarantined linked-notebook
// re-link/unlink flow. The dialog components themselves are presentational;
// this factory is the single source of truth for their open state and the
// backend calls each action makes.
//
// Extracted via the proven createX(deps) factory idiom: the $state runes move
// IN here and are exposed as getters (returning bare $state from a plain
// object would snapshot the initial value). The startup-events controller
// drives the open state through the open*/set* methods (live Events.On
// handlers + the replay path share them); App's markup binds the getters +
// close handlers to the dialog props.
import {
  ConfirmSettingsChange,
  ConfirmGrantsMigration,
  DeclineGrantsMigration,
  PickLinkedNotebook,
  UnlinkNotebook
} from '../../bindings/silt/app.js'
import { pushNotification } from '../notifications/store.svelte'

/** A quarantined linked notebook (root moved or tampered). */
export interface QuarantinedLink {
  id: string
  display_name: string
  root_path: string
}

/** Legacy grants block payload: pluginID → capability → qualifier. */
export type LegacyGrants = Record<string, Record<string, string>>

export interface SettingsDialogsController {
  // Open state (read by App markup; backs the dialog `open` props).
  get showSettingsMismatch(): boolean
  get showGrantsMigration(): boolean
  get pendingLegacyGrants(): LegacyGrants
  get quarantinedLinks(): QuarantinedLink[]
  // Open/close mutators. open* are driven by the startup-events controller
  // (live + replay); close*/clear* back the dialog onClose props.
  openSettingsMismatch(): void
  closeSettingsMismatch(): void
  openGrantsMigration(grants: LegacyGrants): void
  setQuarantinedLinks(links: QuarantinedLink[]): void
  clearQuarantinedLinks(): void
  // IPC action handlers (bound to the dialog confirm/decline/unlink/relink).
  confirmSettingsMismatch(): Promise<void>
  declineGrantsMigration(): Promise<void>
  confirmGrantsMigration(): Promise<void>
  handleUnlinkNotebook(id: string): Promise<void>
  handleRelinkNotebook(id: string): Promise<void>
}

/**
 * Build the settings/quarantine dialog controller. Called once at component
 * init; the $state lives for the component's lifetime.
 */
export function createSettingsDialogs(): SettingsDialogsController {
  // F20: trust-anchor fingerprint mismatch — backend detected vault_path or
  // trusted_publishers changed since last launch.
  let showSettingsMismatch = $state(false)
  // F4: legacy grants: block in this vault's config.yaml the host has never
  // seen. The modal asks the user to confirm moving grants to per-host storage.
  let showGrantsMigration = $state(false)
  let pendingLegacyGrants = $state<LegacyGrants>({})
  // F3: quarantined linked notebooks (root_path moved or tampered). The modal
  // offers re-link (PickLinkedNotebook) or unlink (UnlinkNotebook).
  let quarantinedLinks = $state<QuarantinedLink[]>([])

  async function confirmSettingsMismatch(): Promise<void> {
    try {
      await ConfirmSettingsChange()
      showSettingsMismatch = false
    } catch (e) {
      pushNotification({
        kind: 'error',
        message: `Failed to confirm settings change: ${String(e)}`
      })
    }
  }

  // Decline and Escape share the same path: persist the decline so the
  // sentinel clears, then close. A failure here is log-only (non-fatal).
  async function declineGrantsMigration(): Promise<void> {
    try {
      await DeclineGrantsMigration()
    } catch (e) {
      console.error('DeclineGrantsMigration failed:', e)
    }
    showGrantsMigration = false
  }

  async function confirmGrantsMigration(): Promise<void> {
    try {
      await ConfirmGrantsMigration(pendingLegacyGrants)
      showGrantsMigration = false
    } catch (e) {
      pushNotification({
        kind: 'error',
        message: `Failed to move plugin permissions: ${String(e)}`
      })
    }
  }

  async function handleUnlinkNotebook(id: string): Promise<void> {
    const q = quarantinedLinks.find((l) => l.id === id)
    try {
      await UnlinkNotebook(id)
      quarantinedLinks = quarantinedLinks.filter((l) => l.id !== id)
    } catch (e) {
      pushNotification({
        kind: 'error',
        message: `Failed to unlink ${q?.display_name ?? id}: ${String(e)}`
      })
    }
  }

  async function handleRelinkNotebook(id: string): Promise<void> {
    const q = quarantinedLinks.find((l) => l.id === id)
    try {
      await UnlinkNotebook(id)
      await PickLinkedNotebook()
      quarantinedLinks = quarantinedLinks.filter((l) => l.id !== id)
    } catch (e) {
      pushNotification({
        kind: 'error',
        message: `Failed to re-link ${q?.display_name ?? id}: ${String(e)}`
      })
    }
  }

  return {
    get showSettingsMismatch() {
      return showSettingsMismatch
    },
    get showGrantsMigration() {
      return showGrantsMigration
    },
    get pendingLegacyGrants() {
      return pendingLegacyGrants
    },
    get quarantinedLinks() {
      return quarantinedLinks
    },
    openSettingsMismatch: () => {
      showSettingsMismatch = true
    },
    closeSettingsMismatch: () => {
      showSettingsMismatch = false
    },
    openGrantsMigration: (grants: LegacyGrants) => {
      pendingLegacyGrants = grants
      showGrantsMigration = true
    },
    setQuarantinedLinks: (links: QuarantinedLink[]) => {
      quarantinedLinks = links
    },
    clearQuarantinedLinks: () => {
      quarantinedLinks = []
    },
    confirmSettingsMismatch,
    declineGrantsMigration,
    confirmGrantsMigration,
    handleUnlinkNotebook,
    handleRelinkNotebook
  }
}
