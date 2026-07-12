<script lang="ts">
  // Bespoke settings page for silt-ai-qa (#228).
  import { untrack } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import {
    aiProviderNeedsSetup,
    embeddingProviderNeedsSetup
  } from '../../../settings/ai-setup'
  import { settings, saveConfig } from '../../../settings/store.svelte'
  import { loadPlugins, teardownPlugin } from '../../loader'
  import { DEFAULT_SETTINGS, resolveSettings } from './settings'
  import type { QASettings } from './types'
  import { getQAController } from './state.svelte'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    activeNotebook?: string
    activeSection?: string
    activePage?: string
  }
  let { ctx, manifest, activeNotebook, activeSection, activePage }: Props =
    $props()

  const PLUGIN_ID = 'silt-ai-qa'

  let local = $state<QASettings>({ ...DEFAULT_SETTINGS })
  let loaded = $state(false)
  let rebuildBusy = $state(false)

  const chatUnconfigured = $derived(
    aiProviderNeedsSetup(settings.config?.ai?.chat as any)
  )
  const embedUnconfigured = $derived(
    embeddingProviderNeedsSetup(settings.config?.ai?.embedding as any)
  )
  const enabled = $derived(
    !(settings.config?.plugins?.disabled ?? []).includes(PLUGIN_ID)
  )
  const ctl = $derived(getQAController())

  async function refresh() {
    try {
      const raw = (await ctx.getPluginSettings()) as Record<string, unknown>
      local = resolveSettings(raw)
    } catch {
      local = resolveSettings(null)
    }
    loaded = true
  }
  untrack(() => {
    refresh().catch(() => {
      loaded = true
    })
  })

  async function saveKey<K extends keyof QASettings>(
    key: K,
    value: QASettings[K]
  ) {
    local = { ...local, [key]: value }
    try {
      await ctx.updatePluginSetting(key as string, value as never)
      ctl?.setSettings(resolveSettings({ ...local } as any))
    } catch {
      /* best-effort */
    }
  }

  async function toggleEnabled() {
    const cfg = settings.config
    if (!cfg) return
    if (!cfg.plugins) {
      cfg.plugins = { active: [], disabled: [], plugin_settings: {} }
    }
    const disabled = new Set(cfg.plugins.disabled ?? [])
    if (enabled) {
      disabled.add(PLUGIN_ID)
      teardownPlugin(PLUGIN_ID)
    } else {
      disabled.delete(PLUGIN_ID)
    }
    cfg.plugins.disabled = [...disabled]
    await saveConfig(cfg)
    await loadPlugins(
      activeNotebook ?? '',
      activeSection ?? '',
      activePage ?? ''
    )
  }

  async function onRebuild() {
    if (!ctl) return
    rebuildBusy = true
    try {
      await ctl.rebuild(ctx)
    } finally {
      rebuildBusy = false
    }
  }
</script>

<div class="qa-settings">
  <h2 class="title">{manifest?.name ?? 'AI Search'}</h2>
  <p class="lede">
    {manifest?.description ??
      'Ask natural-language questions of your vault with cited answers. Off by default.'}
  </p>

  <section class="card">
    <h3>Enable</h3>
    <label class="row">
      <input
        type="checkbox"
        checked={enabled}
        onchange={() => void toggleEnabled()}
      />
      <span>Enable AI Search plugin</span>
    </label>
  </section>

  <section class="card">
    <h3>Models</h3>
    <p class="hint">
      Chat and embedding models are configured on the
      <button
        type="button"
        class="link"
        onclick={() => ctx.openSettings?.('ai' as any)}
      >
        AI Provider
      </button>
      page. Embeddings power the index; chat answers questions. See BRING_YOUR_OWN_MODEL
      in the docs.
    </p>
    <ul class="status-list">
      <li>Chat: {chatUnconfigured ? 'not configured' : 'ready'}</li>
      <li>Embedding: {embedUnconfigured ? 'not configured' : 'ready'}</li>
    </ul>
  </section>

  <section class="card">
    <h3>Index</h3>
    <label class="row">
      <input
        type="checkbox"
        checked={local.auto_reembed}
        disabled={!loaded}
        onchange={(e) =>
          void saveKey(
            'auto_reembed',
            (e.currentTarget as HTMLInputElement).checked
          )}
      />
      <span>Auto re-embed on save</span>
    </label>
    <label class="field">
      <span>Notebook scope (comma-separated; empty = all)</span>
      <input
        type="text"
        value={local.notebook_scope.join(', ')}
        disabled={!loaded}
        onchange={(e) => {
          const v = (e.currentTarget as HTMLInputElement).value
          const scope = v
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean)
          void saveKey('notebook_scope', scope)
        }}
      />
    </label>
    <div class="index-status" role="status">
      {#if ctl?.progress}
        Status: {ctl.progress.status}
        {#if ctl.progress.chunkCount != null}
          · {ctl.progress.chunkCount} chunks
        {/if}
        {#if ctl.progress.model}
          · model {ctl.progress.model}
        {/if}
        {#if ctl.progress.dimensions}
          · {ctl.progress.dimensions}d
        {/if}
        {#if ctl.progress.lastError}
          · error: {ctl.progress.lastError}
        {/if}
      {:else}
        Status: unknown (enable plugin to track)
      {/if}
    </div>
    <button
      type="button"
      class="primary"
      disabled={rebuildBusy || embedUnconfigured || !enabled}
      onclick={() => void onRebuild()}
    >
      {rebuildBusy ? 'Rebuilding…' : 'Rebuild index'}
    </button>
  </section>

  <section class="card">
    <h3>Retrieval</h3>
    <label class="field">
      <span>Hybrid weight (vector) — {local.hybrid_weight.toFixed(2)}</span>
      <input
        type="range"
        min="0"
        max="1"
        step="0.05"
        value={local.hybrid_weight}
        disabled={!loaded}
        onchange={(e) =>
          void saveKey(
            'hybrid_weight',
            Number((e.currentTarget as HTMLInputElement).value)
          )}
      />
      <span class="hint">0 = keyword only · 1 = semantic only</span>
    </label>
    <label class="field">
      <span>Top-k passages</span>
      <input
        type="number"
        min="1"
        max="50"
        value={local.top_k}
        disabled={!loaded}
        onchange={(e) =>
          void saveKey(
            'top_k',
            Number((e.currentTarget as HTMLInputElement).value)
          )}
      />
    </label>
  </section>

  <section class="card">
    <h3>Privacy</h3>
    <p class="hint">
      Note content is sent to your configured embedding and chat endpoints when
      you build the index or ask a question. Local (Ollama) endpoints keep data
      on this machine. Cloud endpoints process content per that provider's
      policy. Vectors live only in the plugin database and are deleted on
      uninstall.
    </p>
  </section>
</div>

<style>
  .qa-settings {
    max-width: 40rem;
    display: flex;
    flex-direction: column;
    gap: 1rem;
    padding: 0.25rem 0 2rem;
  }
  .title {
    margin: 0;
    font-size: 1.1rem;
  }
  .lede {
    margin: 0;
    opacity: 0.85;
    line-height: 1.45;
  }
  .card {
    border: 1px solid var(--surface-panel-border, #2a2a30);
    border-radius: 0.5rem;
    padding: 0.85rem 1rem;
    display: flex;
    flex-direction: column;
    gap: 0.65rem;
  }
  .card h3 {
    margin: 0;
    font-size: 0.9rem;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }
  .field input[type='text'],
  .field input[type='number'] {
    padding: 0.35rem 0.5rem;
    border-radius: 0.35rem;
    border: 1px solid var(--surface-panel-border, #2a2a30);
    background: var(--surface-input, #121216);
    color: inherit;
  }
  .hint {
    font-size: 0.8rem;
    opacity: 0.75;
    margin: 0;
    line-height: 1.4;
  }
  .primary {
    align-self: flex-start;
    border: none;
    border-radius: 0.4rem;
    padding: 0.4rem 0.85rem;
    background: var(--accent-primary-start, #6366f1);
    color: #fff;
    font-weight: 600;
    cursor: pointer;
  }
  .primary:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .status-list {
    margin: 0;
    padding-left: 1.1rem;
  }
  .index-status {
    font-size: 0.8rem;
    opacity: 0.85;
  }
  .link {
    background: none;
    border: none;
    color: var(--accent-primary-start, #6366f1);
    cursor: pointer;
    padding: 0;
    font: inherit;
    text-decoration: underline;
  }
</style>
