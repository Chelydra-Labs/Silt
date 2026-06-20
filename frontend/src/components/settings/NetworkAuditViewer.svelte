<script lang="ts">
  // Network activity viewer for a plugin (#115). Shows the host+status audit
  // log entries for this plugin (never the body). Loads on expand and can be
  // refreshed.
  import { GetNetworkAudit } from '../../../wailsjs/go/main/App.js'
  import { onMount } from 'svelte'

  interface Props {
    pluginID: string
  }
  let { pluginID }: Props = $props()

  interface AuditEntry {
    plugin: string
    host: string
    status: number
    method: string
    at: string
  }

  let entries = $state<AuditEntry[]>([])
  let loading = $state(false)

  async function load() {
    loading = true
    try {
      const all = (await GetNetworkAudit()) as AuditEntry[]
      entries = (all ?? []).filter((e) => e.plugin === pluginID)
    } catch {
      entries = []
    } finally {
      loading = false
    }
  }

  onMount(load)
</script>

{#if loading}
  <p class="text-text-muted text-[10px] font-body-md">Loading…</p>
{:else if entries.length === 0}
  <p class="text-text-muted text-[10px] font-body-md italic">
    No network activity recorded.
  </p>
{:else}
  <div class="max-h-32 overflow-y-auto custom-scrollbar space-y-0.5">
    {#each entries.slice(-50) as entry}
      <div class="flex items-center gap-2 text-[10px] font-body-md">
        <span
          class={entry.status >= 200 && entry.status < 300
            ? 'text-accent-primary-start'
            : 'text-status-warn'}
        >
          {entry.method}
          {entry.status}
        </span>
        <span class="text-text-primary truncate flex-1">{entry.host}</span>
        <span class="text-text-muted whitespace-nowrap">
          {new Date(entry.at).toLocaleTimeString()}
        </span>
      </div>
    {/each}
  </div>
  <button
    onclick={load}
    class="mt-1 text-text-muted hover:text-text-primary text-[10px] font-label-sm-bold bg-transparent border border-border-muted rounded px-2 py-0.5 cursor-pointer"
  >
    Refresh
  </button>
{/if}
