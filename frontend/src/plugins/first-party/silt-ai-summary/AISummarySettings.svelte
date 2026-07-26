<script lang="ts">
  // AISummarySettings — fine-tuning for silt-ai-summary. Master enablement is
  // Settings → AI → Note summaries (ai.features); not plugins.disabled (#632).
  import { untrack } from 'svelte'
  import type { PluginContext } from '../../sdk'
  import type { PluginManifest } from '../../sdk'
  import { settings } from '../../../settings/store.svelte'
  import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
  import { resolveSettings, DEFAULT_SETTINGS } from './settings'
  import type { SummarySettings } from './types'

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
  let {
    ctx,
    manifest,
    embedded = false,
    activeNotebook: _activeNotebook,
    activeSection: _activeSection,
    activePage: _activePage
  }: Props = $props()

  // Chat-provider readiness nudge (shared with Plugins badge + banner).
  let unconfigured = $derived(aiProviderNeedsSetup(settings.config?.ai?.chat))

  // Tuning is read from plugin_settings.<id> (resolved over defaults) and
  // written back per-control. A local draft mirrors the resolved settings so
  // the form is reactive while writes round-trip through config.yaml.
  let draft = $state<SummarySettings>({
    ...DEFAULT_SETTINGS,
    facets: { ...DEFAULT_SETTINGS.facets }
  })
  let loaded = $state(false)

  async function refresh() {
    const raw = await ctx.getPluginSettings()
    const resolved = resolveSettings(raw)
    draft = { ...resolved, facets: { ...resolved.facets } }
    loaded = true
  }
  // Load once on mount (the ctx is stable for the plugin).
  untrack(() => {
    refresh().catch(() => {
      loaded = true // degrade to defaults on error rather than hanging
    })
  })

  async function write<K extends keyof SummarySettings>(
    key: K,
    value: SummarySettings[K]
  ) {
    draft[key] = value
    try {
      await ctx.updatePluginSetting(key, value)
    } catch {
      /* best-effort: the config:changed round-trip will resync */
    }
  }

  function writeFacet(key: keyof SummarySettings['facets'], value: boolean) {
    const facets = { ...draft.facets, [key]: value }
    void write('facets', facets)
  }
</script>

<div
  class:p-6={!embedded}
  class="space-y-6 {embedded ? 'w-full' : 'max-w-4xl mx-auto w-full'}"
>
  {#if !embedded}
    <header class="space-y-1">
      <h2
        id="aisettings-title"
        class="text-text-primary text-type-xl font-bold"
      >
        {manifest?.name ?? 'AI Summary'}
      </h2>
      <p class="text-text-muted text-type-md font-body-md leading-relaxed">
        {manifest?.description ??
          'A dismissible highlight at the top of each note with a summary plus new tasks, risks, and decisions.'}
      </p>
    </header>

    <section
      class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-4"
      aria-label="Managed enablement"
    >
      <p class="text-text-muted text-type-sm m-0">
        Enablement is managed under
        <button
          type="button"
          class="text-accent-primary-start underline bg-transparent border-none p-0 cursor-pointer font-inherit"
          onclick={() => ctx.openSettings('ai')}
        >
          Settings → AI → Features
        </button>
        (Note summaries). This page is fine-tuning only.
      </p>
    </section>
  {/if}

  {#if unconfigured}
    <div
      class="flex items-start gap-3 p-4 rounded-xl bg-accent-primary-glow/20 border border-accent-primary-start/30"
      role="status"
    >
      <span
        class="material-symbols-outlined text-accent-primary-start text-type-2xl"
        aria-hidden="true">info</span
      >
      <div class="flex-1 space-y-2.5">
        <p class="text-text-primary text-type-sm font-body-md leading-relaxed">
          No AI provider is configured yet — summaries need a chat model. Add
          one via your own local or OpenAI-compatible endpoint.
        </p>
        <button
          type="button"
          class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-accent-primary-start text-text-on-accent font-label-sm-bold text-type-xs hover:brightness-110 transition-all cursor-pointer"
          onclick={() => ctx.openSettings('ai')}
        >
          Open AI settings
        </button>
      </div>
    </div>
  {/if}

  {#if !embedded}
    <section aria-label="Privacy information">
      <div
        class="flex items-start gap-3 p-4 rounded-xl bg-surface-panel/10 border border-surface-panel-border border-l-4 border-l-accent-primary-start"
      >
        <span
          class="material-symbols-outlined text-text-muted text-icon-lg flex-shrink-0 mt-0.5"
          aria-hidden="true">shield</span
        >
        <p class="text-text-primary text-type-sm font-body-md leading-relaxed">
          Note content is sent only to your configured AI endpoint — local or
          remote — to generate the summary. No other note data is sent. See
          <strong class="text-accent-primary-start"
            >Settings &rarr; AI &rarr; Plugin AI calls</strong
          >
          for the call log.
        </p>
      </div>
    </section>
  {/if}

  {#if loaded}
    <div class="space-y-4">
      <!-- Generation settings card -->
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      >
        <h3
          id="ai-summary-trigger-label"
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        >
          Generation Trigger
        </h3>
        <div
          role="radiogroup"
          aria-labelledby="ai-summary-trigger-label"
          class="grid grid-cols-1 sm:grid-cols-2 gap-3"
        >
          <label
            class="flex items-start gap-3 p-3.5 rounded-lg border text-left transition-all duration-150 cursor-pointer select-none focus-within:ring-2 focus-within:ring-accent-primary-start/60 focus-within:outline-none {draft.auto_on_open &&
            !draft.on_demand_only
              ? 'bg-accent-primary-glow/20 border-accent-primary-start text-accent-primary-start'
              : 'bg-surface-panel border-surface-panel-border text-text-muted hover:border-border-active hover:text-text-primary'}"
          >
            <input
              type="radio"
              name="ai-summary-trigger"
              class="sr-only"
              checked={draft.auto_on_open && !draft.on_demand_only}
              onchange={() => {
                void write('auto_on_open', true)
                void write('on_demand_only', false)
              }}
            />
            <span
              class="material-symbols-outlined text-type-2xl flex-shrink-0 mt-0.5"
              >autorenew</span
            >
            <div>
              <span
                class="font-label-sm-bold text-type-xs uppercase tracking-wide block"
                >Automatically on open</span
              >
              <span
                class="text-type-xs font-label-sm block mt-0.5 text-text-muted"
                >Generates or refreshes a summary as soon as you open a note.</span
              >
            </div>
          </label>

          <label
            class="flex items-start gap-3 p-3.5 rounded-lg border text-left transition-all duration-150 cursor-pointer select-none focus-within:ring-2 focus-within:ring-accent-primary-start/60 focus-within:outline-none {draft.on_demand_only
              ? 'bg-accent-primary-glow/20 border-accent-primary-start text-accent-primary-start'
              : 'bg-surface-panel border-surface-panel-border text-text-muted hover:border-border-active hover:text-text-primary'}"
          >
            <input
              type="radio"
              name="ai-summary-trigger"
              class="sr-only"
              checked={draft.on_demand_only}
              onchange={() => {
                void write('on_demand_only', true)
                void write('auto_on_open', false)
              }}
            />
            <span
              class="material-symbols-outlined text-type-2xl flex-shrink-0 mt-0.5"
              >ads_click</span
            >
            <div>
              <span
                class="font-label-sm-bold text-type-xs uppercase tracking-wide block"
                >Only on-demand</span
              >
              <span
                class="text-type-xs font-label-sm block mt-0.5 text-text-muted"
                >Summaries are only generated when you manually click
                Regenerate.</span
              >
            </div>
          </label>
        </div>
      </div>

      <!-- Content tuning card -->
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      >
        <h3
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        >
          Summary Structure
        </h3>

        <!-- Summary length -->
        <div class="flex flex-col gap-1.5">
          <label
            class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
            for="summary-length-select">Summary length</label
          >
          <select
            id="summary-length-select"
            class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all cursor-pointer max-w-md"
            value={draft.summary_length}
            onchange={(e) =>
              void write(
                'summary_length',
                e.currentTarget.value as SummarySettings['summary_length']
              )}
          >
            <option value="short">Short (2 concise sentences)</option>
            <option value="medium">Medium (2–3 sentences)</option>
            <option value="long">Long (3–4 sentences)</option>
          </select>
        </div>

        <!-- Facets to show -->
        <div class="space-y-2">
          <span
            class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider block"
            >Facets to include</span
          >
          <div class="grid grid-cols-3 gap-2.5">
            {#each [['tasks', 'Tasks', 'done_all'], ['risks', 'Risks', 'warning'], ['decisions', 'Decisions', 'gavel']] as [key, label, icon] (key)}
              {@const active =
                draft.facets[key as keyof SummarySettings['facets']]}
              <label
                class="flex flex-col items-center justify-center p-3 rounded-lg border text-center transition-all duration-150 cursor-pointer select-none focus-within:ring-2 focus-within:ring-accent-primary-start/60 focus-within:outline-none {active
                  ? 'bg-accent-primary-glow/20 border-accent-primary-start text-accent-primary-start'
                  : 'bg-surface-panel border-surface-panel-border text-text-muted hover:border-border-active hover:text-text-primary'}"
              >
                <input
                  type="checkbox"
                  class="sr-only"
                  checked={active}
                  onchange={(e) =>
                    writeFacet(
                      key as keyof SummarySettings['facets'],
                      e.currentTarget.checked
                    )}
                />
                <span class="material-symbols-outlined text-icon-lg mb-1"
                  >{icon}</span
                >
                <span
                  class="font-label-sm-bold text-type-xs uppercase tracking-wide"
                  >{label}</span
                >
              </label>
            {/each}
          </div>
        </div>
      </div>

      <!-- Limits & performance card -->
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      >
        <h3
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        >
          Limits & Performance
        </h3>

        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Regenerate debounce (ms)</span
            >
            <input
              type="number"
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
              aria-label="Regenerate debounce milliseconds after save"
              min="0"
              step="500"
              value={draft.regenerate_debounce_ms}
              onchange={(e) =>
                void write(
                  'regenerate_debounce_ms',
                  Math.max(0, Number(e.currentTarget.value) || 0)
                )}
            />
          </label>

          <label class="flex flex-col gap-1.5">
            <span
              class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
              >Max note size (chars)</span
            >
            <input
              type="number"
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
              aria-label="Max note size characters"
              min="1000"
              step="1000"
              value={draft.max_note_chars}
              onchange={(e) =>
                void write(
                  'max_note_chars',
                  Math.max(1000, Number(e.currentTarget.value) || 0)
                )}
            />
          </label>
        </div>

        <p
          class="text-text-muted text-type-xs font-label-sm leading-relaxed mt-1"
        >
          Notes larger than the max size will be skipped. Lowering this caps
          computation overhead on long note vaults.
        </p>
      </div>
    </div>
  {/if}
</div>
