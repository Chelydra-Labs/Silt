// Shared domain types for the Plugins settings surface. Consumed by PluginsTab
// (the orchestrator) and by the extracted children (CapabilityGrantList,
// SecurityBadge), so neither child depends on the parent for its types.
import type { SettingSchema } from '../../../plugins/sdk'

/** Session security aggregate from GetPluginSecurityStats (#518). */
export interface SecurityStats {
  pluginId: string
  denials: number
  rateLimited: number
  lastDenialAt?: number
  lastRateAt?: number
  lastCapability?: string
}

/** A merged first-party + on-disk plugin row in the Plugins tab. */
export interface Card {
  id: string
  name: string
  version: string
  author: string
  description: string
  icon: string
  source: 'first-party' | 'disk'
  disabled: boolean // disk plugins only
  hasIndex: boolean
  loadError?: string
  /** Capabilities requested by the manifest (#113): cap id → qualifier (true | "notebook" | "vault"). */
  requestedCapabilities?: Record<string, true | string>
  /** Capabilities currently granted to this plugin (cap id → qualifier).
   *  Values are `string | undefined` because the Wails v3 binding generates
   *  Go map[string]string values as optional. */
  grantedCapabilities?: Record<string, string | undefined>
  /** Declarative settings schema (#103), read from the manifest. */
  settingsSchema?: SettingSchema[]
  /** Optional update URL for distribution-v2 update checks (#111). */
  updateUrl?: string
  /** True when a newer version is available (#111). */
  updateAvailable?: boolean
  /** First-party AI modules: enablement lives under Settings → AI (#632). */
  managedInAI?: boolean
}
