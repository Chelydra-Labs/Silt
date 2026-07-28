<script lang="ts">
  // Per-plugin capability grant/revoke list, rendered inside a card's expanded
  // detail panel. Extracted from PluginsTab (#765).
  //
  // `grantBusy` is local to each instance (one CapabilityGrantList per expanded
  // card), so it tracks just the in-flight capability id rather than the
  // tab-wide "<pluginId>:<cap>" key it was before. Grant/revoke call the
  // parent's `onRefresh` (full card-list refresh) after the IPC; errors flow
  // back through `onError` (empty string clears, mirroring the prior
  // optimistic-clear-at-start behavior).
  import {
    RequestCapability,
    RevokeCapability
  } from '../../../../bindings/silt/app.js'
  import { capabilityLabels, qualifierLabel } from './capabilityLabels'
  import type { Card } from './types'

  interface Props {
    card: Card
    onRefresh: () => Promise<void>
    onError: (msg: string) => void
  }
  let { card, onRefresh, onError }: Props = $props()

  let grantBusy = $state<string>('')

  function isGranted(cap: string): boolean {
    return !!card.grantedCapabilities?.[cap]
  }

  async function grant(cap: string) {
    grantBusy = cap
    onError('')
    try {
      const qual = card.requestedCapabilities?.[cap]
      const qualStr = typeof qual === 'string' ? qual : ''
      await RequestCapability(card.id, cap, qualStr)
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e))
      return
    } finally {
      grantBusy = ''
    }
    // Grant succeeded; refresh to reflect the new state. A refresh failure is
    // non-fatal to the grant itself — report it distinctly so the user knows
    // the capability was granted even if the list didn't update.
    try {
      await onRefresh()
    } catch (e) {
      onError(
        `Granted, but the list didn't refresh: ${e instanceof Error ? e.message : String(e)}`
      )
    }
  }

  async function revoke(cap: string) {
    grantBusy = cap
    onError('')
    try {
      await RevokeCapability(card.id, cap)
    } catch (e) {
      onError(e instanceof Error ? e.message : String(e))
      return
    } finally {
      grantBusy = ''
    }
    try {
      await onRefresh()
    } catch (e) {
      onError(
        `Revoked, but the list didn't refresh: ${e instanceof Error ? e.message : String(e)}`
      )
    }
  }
</script>

{#if card.requestedCapabilities && Object.keys(card.requestedCapabilities).length > 0}
  <div>
    <h5
      class="text-text-muted text-type-2xs font-label-sm-bold uppercase tracking-widest mt-2 mb-1"
      id="caps-{card.id}"
    >
      Capabilities
    </h5>
    <ul
      class="text-type-xs font-body-md space-y-1"
      aria-labelledby="caps-{card.id}"
    >
      {#each Object.keys(card.requestedCapabilities) as cap (cap)}
        <li class="flex items-center gap-2">
          <span
            class="material-symbols-outlined text-icon-sm text-text-muted"
            aria-hidden="true"
          >
            {isGranted(cap) ? 'lock_open' : 'lock'}
          </span>
          <span class="flex-1 text-text-primary">
            {capabilityLabels[cap] ?? cap}{qualifierLabel(
              card.requestedCapabilities[cap]
            )}
          </span>
          {#if card.source === 'first-party'}
            <span class="text-type-2xs text-text-muted italic">trusted</span>
          {:else if isGranted(cap)}
            <button
              type="button"
              onclick={() => revoke(cap)}
              disabled={grantBusy === cap}
              class="text-text-muted hover:text-error text-type-2xs font-label-sm-bold bg-transparent border border-surface-panel-border rounded px-2 py-0.5 cursor-pointer disabled:opacity-50"
              aria-label="Revoke {capabilityLabels[cap] ?? cap}"
            >
              {grantBusy === cap ? 'Revoking…' : 'Revoke'}
            </button>
          {:else}
            <button
              type="button"
              onclick={() => grant(cap)}
              disabled={grantBusy === cap}
              class="text-accent-primary-start hover:brightness-110 text-type-2xs font-label-sm-bold bg-transparent border border-accent-primary-start/40 rounded px-2 py-0.5 cursor-pointer disabled:opacity-50"
              aria-label="Grant {capabilityLabels[cap] ?? cap}"
            >
              {grantBusy === cap ? 'Granting…' : 'Grant'}
            </button>
          {/if}
        </li>
      {/each}
    </ul>
  </div>
{/if}
