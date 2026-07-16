<script lang="ts">
  // Bespoke settings page for silt-ai-qa. Enablement is Settings → AI →
  // Semantic search (ai.features); this page is fine-tuning only (#632).
  import { untrack } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import {
    aiProviderNeedsSetup,
    embeddingProviderNeedsSetup
  } from '../../../settings/ai-setup'
  import { settings } from '../../../settings/store.svelte'
  import PresetControl from '../../../components/settings/PresetControl.svelte'
  import InfoTooltip from '../../../components/settings/InfoTooltip.svelte'
  import { DEFAULT_SETTINGS, resolveSettings } from './settings'
  import type { QASettings } from './types'
  import { getQAController } from './state.svelte'
  import {
    SEARCH_BALANCE_PRESETS,
    CONTEXT_BREADTH_PRESETS,
    matchContextBreadth,
    contextBreadthFromKey
  } from './searchPresets'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    activeNotebook?: string
    activeSection?: string
    activePage?: string
  }
  // Location props are part of the settings-page surface contract; unused here.
  let { ctx, manifest }: Props = $props()

  let local = $state<QASettings>({ ...DEFAULT_SETTINGS })
  let loaded = $state(false)
  let rebuildBusy = $state(false)

  const chatUnconfigured = $derived(
    aiProviderNeedsSetup(settings.config?.ai?.chat as any)
  )
  const embedUnconfigured = $derived(
    embeddingProviderNeedsSetup(settings.config?.ai?.embedding as any)
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

  async function saveKeys(patch: Partial<QASettings>) {
    local = { ...local, ...patch }
    try {
      for (const [k, v] of Object.entries(patch)) {
        await ctx.updatePluginSetting(k, v as never)
      }
      ctl?.setSettings(resolveSettings({ ...local } as any))
    } catch {
      /* best-effort */
    }
  }

  const contextBreadthKey = $derived(
    matchContextBreadth(local.top_k, local.max_context_chars)
  )

  function onContextBreadthChange(key: string) {
    const preset = contextBreadthFromKey(key)
    if (!preset) return
    void saveKeys({
      top_k: preset.top_k,
      max_context_chars: preset.max_context_chars
    })
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
  <h2 class="title">{manifest?.name ?? 'Semantic search'}</h2>
  <p class="lede">
    {manifest?.description ??
      'Tune search balance and the note index. Turn semantic search on under Settings → AI → Features.'}
  </p>

  <section class="card" aria-label="Managed enablement">
    <p class="hint">
      Enablement is managed under
      <button
        type="button"
        class="link"
        onclick={() => ctx.openSettings?.('ai' as any)}
      >
        Settings → AI → Features
      </button>
      (Semantic search). This page is fine-tuning only.
    </p>
  </section>

  <section class="card">
    <h3>Models</h3>
    <p class="hint">
      Chat and search models are configured on the
      <button
        type="button"
        class="link"
        onclick={() => ctx.openSettings?.('ai' as any)}
      >
        Settings → AI
      </button>
      page. The search model powers the search index; chat answers questions. See
      BRING_YOUR_OWN_MODEL in the docs.
    </p>
    <ul class="status-list">
      <li>Chat: {chatUnconfigured ? 'not configured' : 'ready'}</li>
      <li>Search model: {embedUnconfigured ? 'not configured' : 'ready'}</li>
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
      <span>Auto-update search index on save</span>
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
    {#if ctl?.showStaleBanner}
      <div class="stale-banner" role="status">
        <strong>Search index needs updating</strong>
        <p>{ctl.settings.stale_reason}. Rebuild for accurate results.</p>
        <div class="stale-actions">
          <button
            type="button"
            class="primary warn"
            disabled={rebuildBusy || embedUnconfigured}
            onclick={() => void onRebuild()}
          >
            Rebuild now
          </button>
          <button
            type="button"
            class="ghost"
            onclick={() => ctl.dismissStaleBanner()}>Later</button
          >
        </div>
      </div>
    {/if}
    {#if ctl?.searchDegradeReason}
      <div class="stale-banner" role="status">
        <strong>Search running in degraded mode</strong>
        <p>{ctl.searchDegradeReason}</p>
      </div>
    {/if}
    <div class="index-status" role="status">
      {#if ctl?.progress}
        Status: {ctl.progress.status}
        {#if ctl.progress.chunkCount != null}
          · {ctl.progress.chunkCount} notes indexed
        {/if}
        {#if ctl.progress.model}
          · search model {ctl.progress.model}
        {/if}
        {#if ctl.progress.dimensions}
          · {ctl.progress.dimensions}d
        {/if}
        {#if ctl.progress.lastError}
          · error: {ctl.progress.lastError}
        {/if}
      {:else}
        Status: idle
      {/if}
    </div>
    <button
      type="button"
      class="primary"
      class:warn={Boolean(local.stale_reason)}
      disabled={rebuildBusy || embedUnconfigured}
      onclick={() => void onRebuild()}
    >
      {rebuildBusy ? 'Updating…' : 'Update search index'}
    </button>
  </section>

  <section class="card">
    <h3>Search</h3>
    {#if loaded}
      <PresetControl
        label="Search Balance"
        tooltipText="How should your search work? Keyword search finds notes containing your exact words. Semantic search finds notes with similar meaning even if the words are different."
        tooltipTechnical="Technical: Hybrid weight in Reciprocal Rank Fusion. 0.0 = pure keyword, 1.0 = pure semantic. Fine-tune under Advanced below."
        options={[...SEARCH_BALANCE_PRESETS]}
        value={local.hybrid_weight}
        onchange={(v) => void saveKey('hybrid_weight', v)}
      />
      <PresetControl
        label="Context Breadth"
        tooltipText="How many of your notes should the AI read before answering? More notes means broader synthesis but slower responses."
        tooltipTechnical="Technical: Top-K retrieval count + max context characters (character budget in the prompt). No hard ceiling — scales to your model's context window."
        options={CONTEXT_BREADTH_PRESETS.map((p) => ({
          value: p.value,
          label: p.label,
          description: p.description
        }))}
        value={contextBreadthKey === '__custom__' ? '' : contextBreadthKey}
        onchange={(v) => onContextBreadthChange(String(v))}
      />
      <!-- Single Advanced disclosure for Search Balance + Context Breadth (#626).
           Search Balance already exposes Advanced via PresetControl; Context
           Breadth custom limits share one details here so the page is not two
           nested Advanced sections. -->
      <details class="adv-details">
        <summary>Advanced</summary>
        <label class="field">
          <span>Hybrid weight (Search Balance)</span>
          <input
            type="number"
            min="0"
            max="1"
            step="0.05"
            value={local.hybrid_weight}
            onchange={(e) =>
              void saveKey(
                'hybrid_weight',
                Number((e.currentTarget as HTMLInputElement).value)
              )}
          />
        </label>
        <label class="field">
          <span>Notes to retrieve (top-k)</span>
          <input
            type="number"
            min="1"
            max="100"
            value={local.top_k}
            onchange={(e) =>
              void saveKey(
                'top_k',
                Number((e.currentTarget as HTMLInputElement).value)
              )}
          />
        </label>
        <label class="field">
          <span>Context budget (characters)</span>
          <input
            type="number"
            min="1000"
            value={local.max_context_chars}
            onchange={(e) =>
              void saveKey(
                'max_context_chars',
                Number((e.currentTarget as HTMLInputElement).value)
              )}
          />
        </label>
      </details>
      <label class="row">
        <input
          type="checkbox"
          checked={local.rerank_enabled}
          onchange={(e) =>
            void saveKey(
              'rerank_enabled',
              (e.currentTarget as HTMLInputElement).checked
            )}
        />
        <span>Smart Re-ranking</span>
        <InfoTooltip
          text="After finding matching notes, re-evaluates and re-orders them for higher accuracy. Improves answer quality but adds a brief delay."
          technical="Technical: Cross-encoder reranking on the top-N fused candidates before context injection."
          label="What is Smart Re-ranking?"
        />
      </label>
    {/if}
  </section>

  <section class="card">
    <h3>Privacy</h3>
    <p class="hint">
      Note content is sent to your configured search and chat endpoints when you
      build the search index or ask a question. Local (Ollama) endpoints keep
      data on this machine. Cloud endpoints process content per that provider's
      policy. The search index lives only in the plugin database and is deleted
      on uninstall.
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
  .adv-details {
    font-size: 0.8rem;
  }
  .adv-details summary {
    cursor: pointer;
    opacity: 0.8;
    margin-bottom: 0.35rem;
  }
  .primary.warn {
    background: color-mix(in srgb, var(--color-status-warn, #fbbf24) 85%, #000);
    color: #1a1a1a;
  }
  .stale-banner {
    border: 1px solid
      color-mix(in srgb, var(--color-status-warn, #fbbf24) 50%, transparent);
    background: color-mix(
      in srgb,
      var(--color-status-warn, #fbbf24) 12%,
      transparent
    );
    border-radius: 0.4rem;
    padding: 0.65rem 0.75rem;
    display: flex;
    flex-direction: column;
    gap: 0.4rem;
  }
  .stale-banner p {
    margin: 0;
    font-size: 0.85rem;
    opacity: 0.9;
  }
  .stale-actions {
    display: flex;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .ghost {
    border: 1px solid var(--surface-panel-border, #2a2a30);
    background: transparent;
    border-radius: 0.4rem;
    padding: 0.35rem 0.75rem;
    cursor: pointer;
    color: inherit;
  }
</style>
