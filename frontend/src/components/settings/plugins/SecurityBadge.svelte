<script lang="ts">
  // Session-security badge for a plugin card row: surfaces capability denials
  // and rate-limit hits aggregated by GetPluginSecurityStats (#518). Extracted
  // from PluginsTab (#765) as presentational markup; the stats *collection*
  // (securityByPlugin + refreshSecurityStats + the EventSecurityEvent
  // subscription) stays tab-owned because it is one onMount listener shared
  // across all cards. This component receives the per-card stats as a prop and
  // renders nothing when there is nothing to report.
  import type { SecurityStats } from './types'

  interface Props {
    stats?: SecurityStats
  }
  let { stats }: Props = $props()

  function securityTitle(st: SecurityStats): string {
    const parts: string[] = []
    if (st.denials > 0) {
      parts.push(
        `${st.denials} capability denial${st.denials === 1 ? '' : 's'}${st.lastCapability ? ` (last: ${st.lastCapability})` : ''}`
      )
    }
    if (st.rateLimited > 0) {
      parts.push(
        `${st.rateLimited} rate-limit hit${st.rateLimited === 1 ? '' : 's'}`
      )
    }
    // Unreachable fallback intentionally omitted: the {#if} guard below
    // ensures at least one count is > 0, so `parts` is always non-empty here.
    return parts.join('; ')
  }
</script>

{#if stats && (stats.denials > 0 || stats.rateLimited > 0)}
  <span
    role="status"
    title={securityTitle(stats)}
    aria-label={securityTitle(stats)}
    class="inline-flex items-center gap-0.5 text-type-3xs text-status-warn bg-status-warn/10 border border-status-warn/30 rounded px-1.5 py-0.5 uppercase tracking-wider"
  >
    <span class="material-symbols-outlined text-type-xs" aria-hidden="true"
      >shield</span
    >
    {#if stats.denials > 0 && stats.rateLimited > 0}
      {stats.denials} denied · {stats.rateLimited} limited
    {:else if stats.denials > 0}
      {stats.denials} denied
    {:else}
      {stats.rateLimited} limited
    {/if}
  </span>
{/if}
