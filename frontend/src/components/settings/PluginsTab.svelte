<script lang="ts">
  import { onMount } from 'svelte'
  import { SvelteSet } from 'svelte/reactivity'
  import { fade } from 'svelte/transition'
  import {
    ListPlugins,
    UninstallPlugin,
    EnablePlugin,
    DisablePlugin,
    GetGrantedCapabilities,
    GetPluginSecurityStats,
    CheckPluginUpdate
  } from '../../../bindings/silt/app.js'
  import { Events } from '@wailsio/runtime'
  import { EventName } from '../../generated/enums'
  import { loadPlugins, teardownPlugin } from '../../plugins/loader'
  import { firstPartyPlugins } from '../../plugins/registry'
  import { loadedPlugins } from '../../plugins/store.svelte'
  import { getSurfaces } from '../../plugins/surfaces'
  import { settings, saveConfig } from '../../settings/store.svelte'
  import { aiProviderNeedsSetup } from '../../settings/ai-setup'
  import SettingsForm from './SettingsForm.svelte'
  import NetworkAuditViewer from './NetworkAuditViewer.svelte'
  import PluginInstallFlow from './plugins/PluginInstallFlow.svelte'
  import CapabilityGrantList from './plugins/CapabilityGrantList.svelte'
  import SecurityBadge from './plugins/SecurityBadge.svelte'
  import type { Card, SecurityStats } from './plugins/types'
  import type { SettingSchema } from '../../plugins/sdk'

  interface Props {
    activeNotebook: string
    activeSection: string
    activePage: string
    onSwitchTab?: (tabId: string) => void
  }
  let { activeNotebook, activeSection, activePage, onSwitchTab }: Props =
    $props()

  let cards = $state<Card[]>([])
  let loading = $state(true)
  let expanded = $state<string | null>(null)
  /** pluginId → session security stats (#518). Collection stays tab-owned
   *  (one EventSecurityEvent subscription in onMount); the badge markup is
   *  delegated to SecurityBadge. */
  let securityByPlugin = $state<Record<string, SecurityStats>>({})
  let actionError = $state('')
  let checkingUpdates = $state(false)
  let updateCheckSummary = $state('')

  async function refreshSecurityStats() {
    try {
      const rows = ((await GetPluginSecurityStats()) ?? []) as SecurityStats[]
      const next: Record<string, SecurityStats> = {}
      for (const row of rows) {
        if (row?.pluginId) next[row.pluginId] = row
      }
      securityByPlugin = next
    } catch {
      // Non-fatal: badge is best-effort observability.
      securityByPlugin = {}
    }
  }

  async function refresh() {
    loading = true
    actionError = ''
    try {
      const disk = (await ListPlugins()) ?? []
      const fps = firstPartyPlugins()
      const fpIds = new Set(fps.map((p) => p.manifest.id))
      const errs = loadedPlugins.errors
      // v2 capability grants (#113): pluginID → cap → qualifier. First-party
      // plugins are not surfaced here (they are implicitly granted).
      const grants = (await GetGrantedCapabilities()) ?? {}
      await refreshSecurityStats()

      const merged: Card[] = []

      // First-party plugins (disableable via config, never uninstallable).
      const fpDisabled = new Set<string>(
        settings.config?.plugins?.disabled ?? []
      )
      for (const fp of fps) {
        const m = fp.manifest
        // First-party AI modules are managed under Settings → AI (#632).
        const managedAI = m.id.startsWith('silt-ai-')
        merged.push({
          id: m.id,
          name: m.name || m.id,
          version: m.version || '—',
          author: m.author || 'Silt',
          description: managedAI
            ? `${m.description || ''} Managed in Settings → AI.`
            : m.description || '',
          icon: m.icon || 'extension',
          source: 'first-party',
          disabled: managedAI ? false : fpDisabled.has(m.id),
          hasIndex: true,
          requestedCapabilities: m.capabilities,
          grantedCapabilities: m.capabilities
            ? Object.fromEntries(
                Object.keys(m.capabilities).map((c) => [c, 'granted'])
              )
            : undefined,
          loadError: errs.find((e) => e.id === m.id)?.message,
          settingsSchema: m.settings,
          managedInAI: managedAI
        })
      }
      // On-disk plugins (skip any shadowed by a first-party id).
      type DiskPlugin = {
        id: string
        name?: string
        version?: string
        author?: string
        description?: string
        icon?: string
        disabled?: boolean
        has_index?: boolean
        capabilities?: Record<string, true | string>
        settings?: SettingSchema[]
        update_url?: string
      }
      for (const p of disk as DiskPlugin[]) {
        if (fpIds.has(p.id)) continue
        merged.push({
          id: p.id,
          name: p.name || p.id,
          version: p.version || '—',
          author: p.author || '',
          description: p.description || '',
          icon: p.icon || 'extension',
          source: 'disk',
          disabled: !!p.disabled,
          hasIndex: !!p.has_index,
          requestedCapabilities: p.capabilities,
          grantedCapabilities: grants[p.id],
          loadError: errs.find((e) => e.id === p.id)?.message,
          settingsSchema: p.settings,
          updateUrl: p.update_url || undefined
        })
      }
      merged.sort((a, b) => a.name.localeCompare(b.name))
      cards = merged
    } catch (e) {
      actionError = e instanceof Error ? e.message : String(e)
      cards = []
    } finally {
      loading = false
    }
  }

  // A single hung CheckPluginUpdate (network stall, backend deadlock) must not
  // pin checkingUpdates forever. Each call is raced against this deadline; a
  // timeout lands in the call's catch as a normal failure so the existing
  // failed/summary paths count it.
  const UPDATE_CHECK_TIMEOUT_MS = 8000
  // Bound concurrent update checks (#813) so N simultaneously-hung plugins
  // don't stack their per-call deadlines (n * 8s worst case). A small cap
  // keeps total wall-clock near one deadline regardless of plugin count.
  const UPDATE_CHECK_CONCURRENCY = 4

  // Resolve `fn(item)` for every item with at most `limit` in flight, settling
  // every call (one rejection never aborts siblings — the allSettled shape the
  // sequential loop already used, just parallelized). Preserves input order in
  // the result array so callers can re-join with the targets snapshot by index.
  async function mapWithConcurrency<T, R>(
    items: T[],
    limit: number,
    fn: (item: T) => Promise<R>
  ): Promise<PromiseSettledResult<R>[]> {
    const results: PromiseSettledResult<R>[] = new Array(items.length)
    let cursor = 0
    async function worker() {
      while (cursor < items.length) {
        const i = cursor++
        try {
          results[i] = { status: 'fulfilled', value: await fn(items[i]) }
        } catch (e) {
          results[i] = { status: 'rejected', reason: e }
        }
      }
    }
    await Promise.all(
      Array.from({ length: Math.min(limit, items.length) }, worker)
    )
    return results
  }

  // Race an IPC promise against a deadline. The underlying promise keeps
  // settling after the race resolves, so its eventual rejection is swallowed
  // (it surfaces as neither an unhandled rejection nor a second failure), and
  // the timer is cleared on settle so a fast success never arms a late
  // timeout rejection.
  function withDeadline<T>(p: Promise<T>, ms: number): Promise<T> {
    let timer!: ReturnType<typeof setTimeout>
    const timeout = new Promise<never>((_, reject) => {
      timer = setTimeout(() => reject(new Error('update check timed out')), ms)
    })
    p.catch(() => {})
    return Promise.race([p, timeout]).finally(() => clearTimeout(timer))
  }

  async function checkForUpdates() {
    if (checkingUpdates) return
    checkingUpdates = true
    updateCheckSummary = ''
    actionError = ''
    try {
      // Snapshot targets by id so a mid-check refresh() cannot detach mutations.
      const targets = cards
        .filter((c) => c.updateUrl && c.source === 'disk')
        .map((c) => ({
          id: c.id,
          name: c.name,
          version: c.version,
          updateUrl: c.updateUrl!
        }))
      let found = 0
      let failed = 0
      const availableIds = new SvelteSet<string>()
      const failedIds = new SvelteSet<string>()
      // Parallel + concurrency-capped (#813): a hung plugin only costs one
      // deadline regardless of plugin count. Order is preserved so the settled
      // array re-joins with `targets` by index.
      const settled = await mapWithConcurrency(
        targets,
        UPDATE_CHECK_CONCURRENCY,
        (t) =>
          withDeadline(
            CheckPluginUpdate(t.id, t.version, t.updateUrl),
            UPDATE_CHECK_TIMEOUT_MS
          )
      )
      targets.forEach((t, i) => {
        const r = settled[i]
        if (r.status === 'fulfilled' && r.value?.updateAvailable) {
          availableIds.add(t.id)
          found++
        } else if (r.status === 'rejected') {
          // best-effort — network errors / timeouts are non-fatal for update checks
          failedIds.add(t.id)
          failed++
        }
      })
      // Rewrite flags from successful checks so a later "no updates" pass
      // clears stale badges. Failed checks keep the prior "Update available"
      // badge — a flaky network must not erase a previously confirmed update —
      // but flip a separate "Check failed" chip (#810) so the user can see
      // WHICH plugin failed without scanning the card list.
      const checkedIds = new SvelteSet(targets.map((t) => t.id))
      cards = cards.map((c) => {
        if (!checkedIds.has(c.id)) return c
        if (failedIds.has(c.id)) {
          return { ...c, updateCheckFailed: true }
        }
        return {
          ...c,
          updateAvailable: availableIds.has(c.id),
          updateCheckFailed: false
        }
      })
      const n = targets.length
      if (n === 0) {
        updateCheckSummary = 'No plugins support update checks'
      } else if (failed === n) {
        updateCheckSummary = `Couldn't check ${n} plugin${n === 1 ? '' : 's'} for updates`
      } else if (found === 0) {
        updateCheckSummary =
          failed > 0
            ? `Checked ${n - failed} of ${n} plugins — no updates (${failed} failed)`
            : `Checked ${n} plugins — no updates`
      } else {
        const failNote =
          failed > 0
            ? ` (${failed} check${failed === 1 ? '' : 's'} failed)`
            : ''
        updateCheckSummary = `Checked ${n} plugins — ${found} update${found === 1 ? '' : 's'} available${failNote}`
      }
      // Name the failed plugins (#810) so the user can locate the culprit(s)
      // without scanning the card list. Skipped when EVERY check failed (the
      // "Couldn't check N…" line already conveys it and the per-card chips
      // cover identity). List up to three by name; beyond that, name the first
      // and summarize the rest.
      if (failed > 0 && failed < n) {
        const failedNames = targets
          .filter((t) => failedIds.has(t.id))
          .map((t) => t.name)
        const named = failedNames.slice(0, 3)
        const rest = failedNames.length - named.length
        const namePart =
          rest > 0 ? `${named.join(', ')} +${rest} more` : named.join(', ')
        updateCheckSummary += ` — failed: ${namePart}`
      }
    } finally {
      checkingUpdates = false
    }
  }

  // #447: nudge when an enabled AI-capable plugin has no chat model configured.
  // Every AI plugin needs a chat model (the primary surface); embedding is
  // feature-specific, and the AI Provider tab the badge links to configures
  // both — so a single chat.model check keeps the nudge accurate without
  // false positives on chat-only plugins.
  // Unified with the AI Provider tab's nudge via aiProviderNeedsSetup (#450)
  // so clicking this badge always lands on a visible nudge there. The Plugins
  // tab reads SystemConfig, where API keys are scrubbed — has_key is omitted so
  // the predicate is model-gated here (a genuinely-missing key surfaces as a
  // retryable call-time error in the plugin, not a stale badge).
  function needsAISetup(card: Card): boolean {
    if (card.disabled) return false
    if (!card.requestedCapabilities?.ai) return false
    // Don't nudge before config has loaded — otherwise the badge flashes on
    // first paint and disappears once settings.config resolves.
    if (!settings.config) return false
    return aiProviderNeedsSetup(settings.config.ai?.chat)
  }

  async function reloadAll() {
    await loadPlugins(activeNotebook, activeSection, activePage)
    await refresh()
  }

  async function toggle(card: Card) {
    actionError = ''
    try {
      if (card.source === 'first-party') {
        // First-party plugins are disabled via the config disabled list
        // (there's no on-disk folder for a .disabled sentinel).
        const cfg = settings.config!
        // Defensive: the Go backend always populates cfg.plugins, but the
        // wails-generated SystemConfig class returns undefined for missing
        // keys, and a hand-edited config.yaml could omit the section.
        if (!cfg.plugins) {
          cfg.plugins = { active: [], disabled: [], plugin_settings: {} }
        }
        // eslint-disable-next-line svelte/prefer-svelte-reactivity -- non-reactive local/helper
        const disabled = new Set(cfg.plugins.disabled ?? [])
        if (card.disabled) {
          disabled.delete(card.id)
        } else {
          disabled.add(card.id)
          // Tearing down before reload drops the plugin's event-bus
          // subscriptions + lifecycle hooks (#106) so they don't linger.
          teardownPlugin(card.id)
        }
        cfg.plugins.disabled = [...disabled]
        await saveConfig(cfg)
        await reloadAll()
      } else {
        // Disk plugins use the .disabled sentinel file.
        if (card.disabled) {
          await EnablePlugin(card.id)
        } else {
          await DisablePlugin(card.id)
          teardownPlugin(card.id)
        }
        await reloadAll()
      }
    } catch (e) {
      actionError = e instanceof Error ? e.message : String(e)
    }
  }

  async function uninstall(card: Card) {
    actionError = ''
    if (
      !window.confirm(
        `Uninstall plugin "${card.name}"? This removes it from .system/plugins/.`
      )
    ) {
      return
    }
    try {
      // Tear down the plugin's host surface (lifecycle hooks + event-bus
      // subscriptions) BEFORE removing the folder + reloading (#106).
      teardownPlugin(card.id)
      await UninstallPlugin(card.id)
      if (expanded === card.id) expanded = null
      await reloadAll()
    } catch (e) {
      actionError = e instanceof Error ? e.message : String(e)
    }
  }

  function pluginSettings(id: string): Record<string, unknown> | undefined {
    return settings.config?.plugins.plugin_settings?.[id]
  }

  // #214: Hoist the surface lookup so the per-card check is O(1), not O(N²).
  // A single $derived set of pluginIDs that have a registered settings-panel
  // surface, recomputed only when the surfaces list changes.
  let settingsPanelPluginIDs = $derived(
    new Set(getSurfaces('settings-panel').map((s) => s.pluginID))
  )

  // #214: hasBespokeSettings reports whether a plugin renders its settings via a
  // dedicated tab (first-party settingsPageComponent or a registered
  // 'settings-panel' surface) rather than the generic schema form. When true,
  // the card shows a redirect note instead of the generic form (either/or).
  function hasBespokeSettings(id: string): boolean {
    if (loadedPlugins.plugins.get(id)?.settingsPageComponent) return true
    return settingsPanelPluginIDs.has(id)
  }

  function openPluginView(id: string) {
    // First-party view ids map to activeView (silt-tasks → tasks).
    const viewId = id.replace(/^silt-/, '')
    window.dispatchEvent(new CustomEvent('switch-view', { detail: viewId }))
  }

  onMount(() => {
    void refresh()
    // Live refresh when Go emits a denial / rate-limit aggregate (#518).
    // Debounced: a misbehaving plugin can emit a burst of denials, and each
    // event would otherwise fire a GetPluginSecurityStats round-trip.
    let timer: ReturnType<typeof setTimeout> | null = null
    const off = Events.On(EventName.EventSecurityEvent, () => {
      if (timer) clearTimeout(timer)
      timer = setTimeout(() => void refreshSecurityStats(), 250)
    })
    return () => {
      off?.()
      if (timer) clearTimeout(timer)
    }
  })
</script>

<div class="p-6 max-w-6xl mx-auto w-full">
  <!-- Install flow (delegates to PluginInstallFlow). The "Check for updates"
       button stays here — it iterates the card list and mutates update flags —
       and is injected beside the install button via the actions snippet. -->
  <section class="mb-6">
    <PluginInstallFlow {reloadAll} onError={(m) => (actionError = m)}>
      {#snippet actions()}
        <button
          type="button"
          onclick={checkForUpdates}
          disabled={checkingUpdates}
          class="ml-2 text-text-muted hover:text-accent-primary-start text-type-xs font-label-sm-bold bg-transparent border border-surface-panel-border rounded px-2 py-1 cursor-pointer transition-colors disabled:opacity-60 disabled:cursor-default"
        >
          {checkingUpdates ? 'Checking…' : 'Check for updates'}
        </button>
      {/snippet}
    </PluginInstallFlow>
    {#if updateCheckSummary}
      <p
        class="mt-2 text-type-xs text-text-muted font-body-md"
        role="status"
        aria-live="polite"
      >
        {updateCheckSummary}
      </p>
    {/if}
  </section>

  {#if actionError}
    <div
      class="flex items-start gap-2 p-3 mb-4 rounded-lg bg-error/10 border border-error/30 text-error text-type-sm font-body-md"
      role="alert"
    >
      <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
        >error</span
      >
      <span class="flex-1">{actionError}</span>
    </div>
  {/if}

  <!-- Plugin list -->
  <h3
    class="font-label-sm-bold text-text-muted uppercase tracking-widest text-type-2xs mb-2"
  >
    Plugins
  </h3>

  {#if loading}
    <div class="text-text-muted py-4 animate-pulse font-body-md">Loading…</div>
  {:else if cards.length === 0}
    <div class="text-text-muted py-4 font-body-md text-type-md">
      No plugins installed. First-party plugins (Agenda, Calendar, Kanban,
      Attachments) are bundled.
    </div>
  {:else}
    <div class="space-y-2">
      {#each cards as card (card.id)}
        <div
          class="rounded-lg border border-surface-panel-border bg-surface-panel/50 overflow-hidden"
        >
          <!-- Card row -->
          <div class="flex items-center gap-3 px-4 py-3">
            <span
              class="material-symbols-outlined text-accent-primary-start/80 text-icon-xl"
              aria-hidden="true"
            >
              {card.icon || 'extension'}
            </span>
            <div class="flex-1 min-w-0">
              <div class="flex items-center gap-2 flex-wrap">
                <span class="font-body-md text-text-primary truncate"
                  >{card.name}</span
                >
                <span class="text-type-2xs text-text-muted"
                  >v{card.version}</span
                >
                {#if card.updateAvailable}
                  <span
                    role="status"
                    class="text-type-3xs text-accent-primary-start bg-accent-primary-glow border border-accent-primary-start/30 rounded px-1.5 py-0.5 uppercase tracking-wider"
                  >
                    Update available
                  </span>
                {/if}
                {#if card.updateCheckFailed}
                  <span
                    role="status"
                    class="text-type-3xs text-error bg-error/10 border border-error/30 rounded px-1.5 py-0.5 uppercase tracking-wider"
                  >
                    Check failed
                  </span>
                {/if}
                {#if card.author}
                  <span class="text-type-2xs text-text-muted truncate"
                    >· {card.author}</span
                  >
                {/if}
                <span
                  class={'text-type-3xs rounded px-1.5 py-0.5 uppercase tracking-wider border ' +
                    (card.source === 'first-party'
                      ? 'text-accent-primary-start border-accent-primary-start/40'
                      : 'text-text-muted border-surface-panel-border')}
                >
                  {card.source === 'first-party' ? 'Bundled' : 'Installed'}
                </span>
                {#if card.disabled}
                  <span
                    class="text-type-3xs text-text-muted bg-surface-panel border border-surface-panel-border rounded px-1.5 py-0.5 uppercase tracking-wider"
                    >disabled</span
                  >
                {/if}
                {#if card.loadError}
                  <span
                    class="text-type-3xs text-error bg-error/10 border border-error/30 rounded px-1.5 py-0.5 uppercase tracking-wider"
                    >error</span
                  >
                {/if}
                <SecurityBadge stats={securityByPlugin[card.id]} />
                {#if needsAISetup(card)}
                  <button
                    type="button"
                    onclick={() => onSwitchTab?.('ai')}
                    disabled={!onSwitchTab}
                    title="Open AI settings"
                    class="inline-flex items-center gap-0.5 text-type-3xs text-accent-primary-start bg-accent-primary-glow border border-accent-primary-start/30 rounded px-1.5 py-0.5 uppercase tracking-wider hover:bg-accent-primary-start/20 hover:border-accent-primary-start/60 transition-all motion-reduce:transition-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:cursor-default disabled:opacity-70"
                  >
                    AI setup needed
                    <span
                      class="material-symbols-outlined text-type-xs"
                      aria-hidden="true">arrow_forward</span
                    >
                  </button>
                {/if}
              </div>
              <div class="text-type-2xs text-text-muted truncate font-label-sm">
                {card.id}
              </div>
            </div>

            <!-- Expand details -->
            <button
              onclick={() => (expanded = expanded === card.id ? null : card.id)}
              aria-expanded={expanded === card.id}
              aria-label={`${card.name}: ${expanded === card.id ? 'Collapse' : 'Details'}`}
              title={expanded === card.id ? 'Collapse' : 'Details'}
              class="text-text-muted hover:text-text-primary border-none bg-transparent cursor-pointer p-1.5 rounded transition-colors"
            >
              <span
                class="material-symbols-outlined text-icon-lg"
                aria-hidden="true"
              >
                {expanded === card.id ? 'expand_less' : 'expand_more'}
              </span>
            </button>

            {#if card.managedInAI}
              <button
                type="button"
                onclick={() => onSwitchTab?.('ai')}
                disabled={!onSwitchTab}
                title="Managed in Settings → AI"
                aria-label={`${card.name}: managed in AI settings`}
                class="text-type-2xs font-label-sm-bold text-accent-primary-start border border-accent-primary-start/30 rounded px-2 py-1 bg-accent-primary-glow/30 hover:bg-accent-primary-start/20 cursor-pointer disabled:opacity-70 disabled:cursor-default"
              >
                AI settings
              </button>
            {:else}
              <button
                type="button"
                onclick={() => toggle(card)}
                title={card.disabled ? 'Enable' : 'Disable'}
                aria-label={`${card.name}: ${card.disabled ? 'Enable' : 'Disable'}`}
                class="text-text-muted hover:text-accent-primary-start border-none bg-transparent cursor-pointer p-1.5 rounded transition-colors"
              >
                <span
                  class="material-symbols-outlined text-icon-lg"
                  aria-hidden="true"
                >
                  {card.disabled ? 'toggle_off' : 'toggle_on'}
                </span>
              </button>
            {/if}
            {#if card.source === 'disk'}
              <button
                type="button"
                onclick={() => uninstall(card)}
                title="Uninstall"
                aria-label={`${card.name}: Uninstall`}
                class="text-text-muted hover:text-error border-none bg-transparent cursor-pointer p-1.5 rounded transition-colors"
              >
                <span
                  class="material-symbols-outlined text-icon-lg"
                  aria-hidden="true">delete</span
                >
              </button>
            {/if}
          </div>

          <!-- Inline load error -->
          {#if card.loadError}
            <div
              class="px-4 pb-2 -mt-1 text-error text-type-xs font-body-md flex items-center gap-1.5"
            >
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true">error</span
              >
              {card.loadError}
            </div>
          {/if}

          <!-- Detail panel -->
          {#if expanded === card.id}
            <div
              transition:fade={{ duration: 120 }}
              class="px-4 py-3 border-t border-surface-panel-border bg-surface-panel/40 space-y-2"
            >
              {#if card.description}
                <p class="text-text-muted text-type-sm font-body-md">
                  {card.description}
                </p>
              {/if}
              <dl
                class="grid grid-cols-auto-fr gap-x-4 gap-y-1 text-type-xs font-label-sm"
              >
                <dt class="text-text-muted">ID</dt>
                <dd class="text-text-primary">{card.id}</dd>
                <dt class="text-text-muted">Version</dt>
                <dd class="text-text-primary">{card.version}</dd>
                {#if card.author}
                  <dt class="text-text-muted">Author</dt>
                  <dd class="text-text-primary">{card.author}</dd>
                {/if}
                <dt class="text-text-muted">Source</dt>
                <dd class="text-text-primary capitalize">
                  {card.source === 'first-party'
                    ? 'First-party (bundled)'
                    : 'Third-party (.silt-plugin)'}
                </dd>
                <dt class="text-text-muted">Status</dt>
                <dd class="text-text-primary">
                  {#if card.loadError}
                    Error
                  {:else if card.disabled}
                    Disabled
                  {:else}
                    Active
                  {/if}
                </dd>
              </dl>

              {#if hasBespokeSettings(card.id) && !card.managedInAI}
                <!-- #214: dedicated settings tab (not the unified AI tab). -->
                <div>
                  <h5
                    class="text-text-muted text-type-2xs font-label-sm-bold uppercase tracking-widest mt-2 mb-1"
                  >
                    Plugin settings
                  </h5>
                  {#if onSwitchTab}
                    <button
                      type="button"
                      class="text-type-sm text-accent-primary-start hover:underline bg-transparent border-none cursor-pointer p-0 font-body-md"
                      onclick={() => onSwitchTab(`plugin:${card.id}`)}
                    >
                      Open the {card.name} settings tab
                    </button>
                  {:else}
                    <p class="text-type-sm text-text-muted font-body-md">
                      This plugin has a dedicated settings page — open the
                      <strong>{card.name}</strong> tab on the left.
                    </p>
                  {/if}
                </div>
              {:else if card.settingsSchema && card.settingsSchema.length > 0 && !card.managedInAI}
                <div>
                  <h5
                    class="text-text-muted text-type-2xs font-label-sm-bold uppercase tracking-widest mt-2 mb-1"
                  >
                    Plugin settings
                  </h5>
                  <SettingsForm
                    pluginID={card.id}
                    schema={card.settingsSchema}
                    values={pluginSettings(card.id) ?? {}}
                  />
                </div>
              {:else if pluginSettings(card.id)}
                <div>
                  <h5
                    class="text-text-muted text-type-2xs font-label-sm-bold uppercase tracking-widest mt-2 mb-1"
                  >
                    Plugin settings
                  </h5>
                  <pre
                    class="text-type-2xs text-text-primary bg-surface-panel/60 border border-surface-panel-border rounded p-2 overflow-x-auto">{JSON.stringify(
                      pluginSettings(card.id),
                      null,
                      2
                    )}</pre>
                </div>
              {/if}

              <CapabilityGrantList
                {card}
                onRefresh={refresh}
                onError={(m) => (actionError = m)}
              />

              {#if card.source === 'first-party'}
                <button
                  type="button"
                  onclick={() => openPluginView(card.id)}
                  class="mt-1 text-accent-primary-start text-type-xs font-label-sm-bold hover:brightness-110 bg-transparent border-none cursor-pointer flex items-center gap-1"
                >
                  Open {card.name} view
                  <span
                    class="material-symbols-outlined text-icon-sm"
                    aria-hidden="true">arrow_forward</span
                  >
                </button>
              {/if}

              {#if card.grantedCapabilities?.network}
                <div>
                  <h5
                    class="text-text-muted text-type-2xs font-label-sm-bold uppercase tracking-widest mt-2 mb-1"
                  >
                    Network activity
                  </h5>
                  <NetworkAuditViewer pluginID={card.id} />
                </div>
              {/if}
            </div>
          {/if}
        </div>
      {/each}
    </div>
  {/if}
</div>
