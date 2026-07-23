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
    /** When true, render as a section of Settings → AI (no page chrome). */
    embedded?: boolean
  }
  // Location props are part of the settings-page surface contract; unused here.
  let { ctx, manifest, embedded = false }: Props = $props()

  let local = $state<QASettings>({ ...DEFAULT_SETTINGS })
  let loaded = $state(false)
  let rebuildBusy = $state(false)

  const chatUnconfigured = $derived(
    aiProviderNeedsSetup(settings.config?.ai?.chat)
  )
  const embedUnconfigured = $derived(
    embeddingProviderNeedsSetup(settings.config?.ai?.embedding)
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
      ctl?.setSettings(resolveSettings({ ...local } as never))
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
      ctl?.setSettings(resolveSettings({ ...local } as never))
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

<div
  class:p-6={!embedded}
  class="space-y-6 {embedded ? 'w-full' : 'max-w-4xl mx-auto w-full'}"
>
  {#if !embedded}
    <header class="space-y-1">
      <h2 class="text-text-primary text-type-xl font-bold m-0">
        {manifest?.name ?? 'Semantic search'}
      </h2>
      <p class="text-text-muted text-type-md font-body-md leading-relaxed m-0">
        {manifest?.description ??
          'Tune search balance and the note index. Turn semantic search on under Settings → AI → Features.'}
      </p>
    </header>

    <section
      class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-4"
      aria-label="Managed enablement"
    >
      <p class="text-text-muted text-type-sm font-body-md m-0 leading-relaxed">
        Enablement is managed under
        <button
          type="button"
          class="text-accent-primary-start underline bg-transparent border-none p-0 cursor-pointer font-inherit"
          onclick={() => ctx.openSettings?.('ai')}
        >
          Settings → AI → Features
        </button>
        (Semantic search). This page is fine-tuning only.
      </p>
    </section>
  {/if}

  <!-- Models readiness -->
  <section
    class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
    aria-labelledby="qa-models-heading"
  >
    <div class="space-y-0.5">
      <h3
        id="qa-models-heading"
        class="text-text-primary text-type-md font-semibold m-0"
      >
        Models
      </h3>
      <p class="text-text-muted text-type-xs font-label-sm m-0">
        Chat and embedding models are configured on
        <button
          type="button"
          class="text-accent-primary-start underline bg-transparent border-none p-0 cursor-pointer font-inherit"
          onclick={() => ctx.openSettings?.('ai')}
        >
          Settings → AI
        </button>
        . The embedding model powers the search index; chat answers questions.
      </p>
    </div>

    <ul class="list-none m-0 p-0 space-y-2" aria-label="Model readiness">
      <li
        class="flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg bg-surface-panel/40 border border-surface-panel-border/60"
      >
        <span class="flex items-center gap-2 text-text-primary text-type-sm">
          <span
            class="material-symbols-outlined text-icon-md text-text-muted"
            aria-hidden="true">chat</span
          >
          Chat
        </span>
        {#if chatUnconfigured}
          <span
            class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-status-warn/10 border border-status-warn/30 text-status-warn text-type-2xs font-label-sm-bold uppercase tracking-wide"
          >
            Not configured
          </span>
        {:else}
          <span
            class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-accent-primary-glow/30 border border-accent-primary-start/30 text-accent-primary-start text-type-2xs font-label-sm-bold uppercase tracking-wide"
          >
            Ready
          </span>
        {/if}
      </li>
      <li
        class="flex items-center justify-between gap-3 px-3 py-2.5 rounded-lg bg-surface-panel/40 border border-surface-panel-border/60"
      >
        <span class="flex items-center gap-2 text-text-primary text-type-sm">
          <span
            class="material-symbols-outlined text-icon-md text-text-muted"
            aria-hidden="true">travel_explore</span
          >
          Embedding (search index)
        </span>
        {#if embedUnconfigured}
          <span
            class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-status-warn/10 border border-status-warn/30 text-status-warn text-type-2xs font-label-sm-bold uppercase tracking-wide"
          >
            Not configured
          </span>
        {:else}
          <span
            class="inline-flex items-center gap-1 px-2 py-0.5 rounded-md bg-accent-primary-glow/30 border border-accent-primary-start/30 text-accent-primary-start text-type-2xs font-label-sm-bold uppercase tracking-wide"
          >
            Ready
          </span>
        {/if}
      </li>
    </ul>

    {#if chatUnconfigured || embedUnconfigured}
      <button
        type="button"
        class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-accent-primary-start text-text-on-accent font-label-sm-bold text-type-xs hover:brightness-110 transition-all cursor-pointer border-none"
        onclick={() => ctx.openSettings?.('ai')}
      >
        Open AI settings
      </button>
    {/if}
  </section>

  <!-- Index -->
  <section
    class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
    aria-labelledby="qa-index-heading"
  >
    <div class="space-y-0.5">
      <h3
        id="qa-index-heading"
        class="text-text-primary text-type-md font-semibold m-0"
      >
        Search index
      </h3>
      <p class="text-text-muted text-type-xs font-label-sm m-0">
        Rebuild after changing the embedding model or when results look stale.
      </p>
    </div>

    <label
      class="flex items-start justify-between gap-4 cursor-pointer select-none"
      for="qa-auto-reembed"
    >
      <span class="min-w-0 space-y-0.5">
        <span
          class="text-text-primary text-type-sm font-semibold block"
          id="qa-auto-reembed-label"
        >
          Auto-update on save
        </span>
        <span class="text-text-muted text-type-xs font-label-sm block">
          Re-index a note when you save it. Off = manual rebuild only.
        </span>
      </span>
      <span class="flex items-center flex-shrink-0">
        <input
          id="qa-auto-reembed"
          type="checkbox"
          class="keyring-switch peer sr-only"
          aria-labelledby="qa-auto-reembed-label"
          checked={local.auto_reembed}
          disabled={!loaded}
          onchange={(e) =>
            void saveKey(
              'auto_reembed',
              (e.currentTarget as HTMLInputElement).checked
            )}
        />
        <span
          aria-hidden="true"
          class="keyring-switch-track"
          class:on={local.auto_reembed}
          class:disabled={!loaded}
        ></span>
      </span>
    </label>

    <label class="flex flex-col gap-1.5" for="qa-notebook-scope">
      <span
        class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
      >
        Notebook scope
      </span>
      <input
        id="qa-notebook-scope"
        type="text"
        class="w-full max-w-md rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-type-sm text-text-primary outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start disabled:opacity-50"
        placeholder="Empty = all notebooks"
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
      <span class="text-text-muted text-type-2xs font-label-sm">
        Comma-separated notebook names. Leave empty to index the whole vault.
      </span>
    </label>

    {#if ctl?.showStaleBanner}
      <div
        class="flex items-start gap-3 p-3.5 rounded-lg bg-status-warn/5 border border-status-warn/30"
        role="alert"
      >
        <span
          class="material-symbols-outlined text-status-warn text-icon-md flex-shrink-0 mt-0.5"
          aria-hidden="true">warning</span
        >
        <div class="flex-1 min-w-0 space-y-2">
          <div>
            <strong class="text-text-primary text-type-sm font-semibold block">
              Search index needs updating
            </strong>
            <p
              class="text-text-muted text-type-xs font-body-md m-0 mt-0.5 leading-relaxed"
            >
              {ctl.settings.stale_reason}. Rebuild for accurate results.
            </p>
          </div>
          <div class="flex flex-wrap gap-2">
            <button
              type="button"
              class="inline-flex items-center gap-1 px-3 py-1.5 rounded-lg border border-status-warn/40 bg-status-warn/15 text-status-warn font-label-sm-bold text-type-xs cursor-pointer hover:brightness-110 disabled:opacity-50 disabled:cursor-not-allowed"
              disabled={rebuildBusy || embedUnconfigured}
              onclick={() => void onRebuild()}
            >
              Rebuild now
            </button>
            <button
              type="button"
              class="inline-flex items-center px-3 py-1.5 rounded-lg border border-surface-panel-border bg-transparent text-text-primary font-label-sm-bold text-type-xs cursor-pointer hover:bg-surface-panel/40"
              onclick={() => ctl.dismissStaleBanner()}
            >
              Later
            </button>
          </div>
        </div>
      </div>
    {/if}

    {#if ctl?.searchDegradeReason}
      <div
        class="flex items-start gap-3 p-3.5 rounded-lg bg-status-warn/5 border border-status-warn/30"
        role="alert"
      >
        <span
          class="material-symbols-outlined text-status-warn text-icon-md flex-shrink-0 mt-0.5"
          aria-hidden="true">warning</span
        >
        <div class="min-w-0">
          <strong class="text-text-primary text-type-sm font-semibold block">
            Search running in degraded mode
          </strong>
          <p
            class="text-text-muted text-type-xs font-body-md m-0 mt-0.5 leading-relaxed"
          >
            {ctl.searchDegradeReason}
          </p>
        </div>
      </div>
    {/if}

    <div
      class="flex flex-wrap items-center gap-x-3 gap-y-1 text-type-xs font-label-sm text-text-muted"
      role="status"
      aria-live="polite"
    >
      {#if ctl?.progress}
        <span class="inline-flex items-center gap-1">
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">database</span
          >
          Status: {ctl.progress.status}
        </span>
        {#if ctl.progress.chunkCount != null}
          <span>· {ctl.progress.chunkCount} notes indexed</span>
        {/if}
        {#if ctl.progress.model}
          <span>· model {ctl.progress.model}</span>
        {/if}
        {#if ctl.progress.dimensions}
          <span>· {ctl.progress.dimensions}d</span>
        {/if}
        {#if ctl.progress.lastError}
          <span class="text-status-danger"
            >· error: {ctl.progress.lastError}</span
          >
        {/if}
      {:else}
        <span>Status: idle</span>
      {/if}
    </div>

    <button
      type="button"
      class="inline-flex items-center gap-1.5 self-start px-3.5 py-2 rounded-lg font-label-sm-bold text-type-xs cursor-pointer transition-all disabled:opacity-50 disabled:cursor-not-allowed {local.stale_reason
        ? 'border border-status-warn/40 bg-status-warn/15 text-status-warn hover:brightness-110'
        : 'border-none bg-accent-primary-start text-text-on-accent hover:brightness-110'}"
      disabled={rebuildBusy || embedUnconfigured}
      onclick={() => void onRebuild()}
    >
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >{rebuildBusy ? 'progress_activity' : 'sync'}</span
      >
      {rebuildBusy ? 'Updating…' : 'Update search index'}
    </button>
  </section>

  <!-- Search tuning -->
  <section
    class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-5"
    aria-labelledby="qa-search-heading"
  >
    <div class="space-y-0.5">
      <h3
        id="qa-search-heading"
        class="text-text-primary text-type-md font-semibold m-0"
      >
        Search
      </h3>
      <p class="text-text-muted text-type-xs font-label-sm m-0">
        Balance keyword vs meaning, and how much context the model sees.
      </p>
    </div>

    {#if loaded}
      <div class="space-y-5">
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

        <!-- Single Advanced disclosure for Search Balance + Context Breadth (#626). -->
        <details
          class="group bg-surface-panel/10 border border-surface-panel-border rounded-xl"
        >
          <summary
            class="flex items-center justify-between p-3.5 cursor-pointer select-none list-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
          >
            <span class="flex items-center gap-2">
              <span
                class="material-symbols-outlined text-icon-md text-text-muted"
                aria-hidden="true">tune</span
              >
              <span class="text-type-sm font-semibold text-text-primary"
                >Advanced</span
              >
            </span>
            <span
              class="material-symbols-outlined text-icon-md text-text-muted transition-transform group-open:rotate-180"
              aria-hidden="true">expand_more</span
            >
          </summary>
          <div
            class="px-3.5 pb-4 border-t border-surface-panel-border/30 pt-4 space-y-3"
          >
            <label
              class="flex flex-col gap-1.5 max-w-xs"
              for="qa-hybrid-weight"
            >
              <span
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >
                Hybrid weight (Search Balance)
              </span>
              <input
                id="qa-hybrid-weight"
                type="number"
                min="0"
                max="1"
                step="0.05"
                class="w-full rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-type-sm text-text-primary outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start"
                value={local.hybrid_weight}
                onchange={(e) =>
                  void saveKey(
                    'hybrid_weight',
                    Number((e.currentTarget as HTMLInputElement).value)
                  )}
              />
            </label>
            <label class="flex flex-col gap-1.5 max-w-xs" for="qa-top-k">
              <span
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >
                Notes to retrieve (top-k)
              </span>
              <input
                id="qa-top-k"
                type="number"
                min="1"
                max="100"
                class="w-full rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-type-sm text-text-primary outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start"
                value={local.top_k}
                onchange={(e) =>
                  void saveKey(
                    'top_k',
                    Number((e.currentTarget as HTMLInputElement).value)
                  )}
              />
            </label>
            <label class="flex flex-col gap-1.5 max-w-xs" for="qa-max-context">
              <span
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >
                Context budget (characters)
              </span>
              <input
                id="qa-max-context"
                type="number"
                min="1000"
                class="w-full rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-type-sm text-text-primary outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start"
                value={local.max_context_chars}
                onchange={(e) =>
                  void saveKey(
                    'max_context_chars',
                    Number((e.currentTarget as HTMLInputElement).value)
                  )}
              />
            </label>
          </div>
        </details>

        <label
          class="flex items-start justify-between gap-4 cursor-pointer select-none"
          for="qa-rerank"
        >
          <span class="min-w-0 space-y-0.5">
            <span
              class="flex items-center gap-1.5 text-text-primary text-type-sm font-semibold"
              id="qa-rerank-label"
            >
              Smart re-ranking
              <InfoTooltip
                text="After finding matching notes, re-evaluates and re-orders them for higher accuracy. Improves answer quality but adds a brief delay."
                technical="Technical: Cross-encoder reranking on the top-N fused candidates before context injection."
                label="What is Smart Re-ranking?"
              />
            </span>
            <span class="text-text-muted text-type-xs font-label-sm block">
              Re-score retrieved notes by similarity to your question.
            </span>
          </span>
          <span class="flex items-center flex-shrink-0">
            <input
              id="qa-rerank"
              type="checkbox"
              class="keyring-switch peer sr-only"
              aria-labelledby="qa-rerank-label"
              checked={local.rerank_enabled}
              onchange={(e) =>
                void saveKey(
                  'rerank_enabled',
                  (e.currentTarget as HTMLInputElement).checked
                )}
            />
            <span
              aria-hidden="true"
              class="keyring-switch-track"
              class:on={local.rerank_enabled}
            ></span>
          </span>
        </label>
      </div>
    {:else}
      <p class="text-text-muted text-type-sm m-0" role="status">
        Loading search settings…
      </p>
    {/if}
  </section>

  {#if !embedded}
    <section aria-label="Privacy information">
      <div
        class="flex items-start gap-3 p-4 rounded-xl bg-surface-panel/10 border border-surface-panel-border border-l-4 border-l-accent-primary-start"
      >
        <span
          class="material-symbols-outlined text-text-muted text-icon-lg flex-shrink-0 mt-0.5"
          aria-hidden="true">shield</span
        >
        <p
          class="text-text-primary text-type-sm font-body-md leading-relaxed m-0"
        >
          Note content is sent to your configured search and chat endpoints when
          you build the search index or ask a question. Local (Ollama) endpoints
          keep data on this machine. Cloud endpoints process content per that
          provider's policy. The search index lives only in the plugin database
          and is deleted on uninstall. See
          <strong class="text-accent-primary-start"
            >Settings &rarr; AI &rarr; Plugin AI calls</strong
          >
          for the call log.
        </p>
      </div>
    </section>
  {/if}
</div>

<style>
  /* Switch track — same contract as Settings → AI (AIProviderTab). */
  .keyring-switch-track {
    width: 36px;
    height: 20px;
    border-radius: 9999px;
    background: var(--color-surface-panel-border);
    position: relative;
    flex-shrink: 0;
    margin-top: 2px;
    transition: background-color 0.15s ease;
  }
  .keyring-switch-track.on {
    background: var(--color-accent-primary-start);
  }
  .keyring-switch-track.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .keyring-switch-track::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 9999px;
    background: var(--color-surface-app);
    transition: transform 0.15s ease;
  }
  .keyring-switch-track.on::after {
    transform: translateX(16px);
  }
  .keyring-switch:focus-visible + .keyring-switch-track {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }
  details > summary::-webkit-details-marker {
    display: none;
  }
  details > summary {
    list-style: none;
  }
</style>
