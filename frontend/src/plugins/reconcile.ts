// Capability reconciliation for plugin editor contributions (#582).
//
// The three contribution registries (slash commands, surfaces, decorations)
// check `isGranted` only at REGISTRATION time. When a capability is revoked
// later (or a plugin disabled) via Settings, already-registered contributions
// stayed live until a reload. This pass walks each registry after the grant
// cache is refreshed and removes any entry whose plugin no longer holds the
// capability its registry requires.
//
// Lives in its own module (not grants.svelte.ts) to avoid a circular import:
// the registries import `isGranted` from grants, so grants cannot import them
// back. App.svelte calls this right after `await refreshGrants()`.

import { isGranted } from './grants.svelte'
import {
  getSlashCommands,
  unregisterSlashCommand
} from '../lib/editor/slash-registry'
import { getSurfaces, unregisterSurface } from './surfaces'
import {
  getDecorationProviderPluginIDs,
  unregisterPluginDecorations
} from '../lib/editor/decorations'

/** Remove every registered contribution whose plugin lacks the capability its
 *  registry requires. Idempotent; safe to call when nothing is registered. */
export function revokeRevokedContributions(): void {
  // Slash commands require editor-schema.
  for (const cmd of getSlashCommands()) {
    if (cmd.pluginID && !isGranted(cmd.pluginID, 'editor-schema')) {
      unregisterSlashCommand(cmd.id)
    }
  }
  // Surfaces require ui-surface.
  for (const surf of getSurfaces()) {
    if (!isGranted(surf.pluginID, 'ui-surface')) {
      unregisterSurface(surf.id)
    }
  }
  // Decoration providers require editor-schema (same gate as slash commands).
  for (const pid of getDecorationProviderPluginIDs()) {
    if (!isGranted(pid, 'editor-schema')) {
      unregisterPluginDecorations(pid)
    }
  }
}
