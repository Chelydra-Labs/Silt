<script lang="ts">
  // AISummarySettings — the bespoke settings page for silt-ai-summary (#223).
  // Mounted by PluginSettingsPanel as a dynamic Settings tab when the plugin
  // is registered with settingsPageComponent. Controls live-update the vault
  // config (plugin_settings.silt-ai-summary via ctx.updatePluginSetting) so a
  // change applies on the next summary without a reload; enable/disable goes
  // through the shared plugins.disabled path (mirrors the Plugins tab) so the
  // loader tears down / re-mounts the plugin's lifecycle hooks consistently.
  import { untrack } from 'svelte'
  import type { PluginContext } from '../../sdk'
  import type { PluginManifest } from '../../sdk'
  import { settings, saveConfig } from '../../../settings/store.svelte'
  import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
  import { loadPlugins, teardownPlugin } from '../../loader'
  import { resolveSettings, DEFAULT_SETTINGS } from './settings'
  import type { SummarySettings } from './types'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
    activeNotebook?: string
    activeSection?: string
    activePage?: string
  }
  let { ctx, manifest, activeNotebook, activeSection, activePage }: Props =
    $props()

  const PLUGIN_ID = 'silt-ai-summary'

  // Enabled state derives from plugins.disabled (same source the Plugins tab
  // edits). Toggling it reloads the plugin so onVaultOpen/onVaultClose rebind.
  let enabled = $derived(
    !(settings.config?.plugins?.disabled ?? []).includes(PLUGIN_ID)
  )
  // The chat-provider readiness nudge (mirrors the Plugins-tab badge + the
  // banner's unconfigured state — all via the shared aiProviderNeedsSetup).
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
    const raw = (await ctx.getPluginSettings()) as Record<string, unknown>
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
      await ctx.updatePluginSetting(key, value as never)
    } catch {
      /* best-effort: the config:changed round-trip will resync */
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
    // Re-mount the plugin so its lifecycle hooks reflect the new state.
    await loadPlugins(
      activeNotebook ?? '',
      activeSection ?? '',
      activePage ?? ''
    )
  }

  function writeFacet(key: keyof SummarySettings['facets'], value: boolean) {
    const facets = { ...draft.facets, [key]: value }
    void write('facets', facets)
  }
</script>

<section class="aisettings" aria-labelledby="aisettings-title">
  <header class="head">
    <h2 id="aisettings-title" class="title">
      {manifest?.name ?? 'AI Summary'}
    </h2>
    <p class="desc">
      {manifest?.description ??
        'A dismissible highlight at the top of each note with a summary plus new tasks, risks, and decisions.'}
    </p>
  </header>

  {#if unconfigured}
    <div class="note unconfigured" role="status">
      <span class="material-symbols-outlined" aria-hidden="true">info</span>
      <span>
        No AI provider is configured yet — summaries need a chat model. Add one
        via your own local or OpenAI-compatible endpoint.
      </span>
      <button type="button" class="cta" onclick={() => ctx.openSettings('ai')}>
        Open AI Provider settings
      </button>
    </div>
  {/if}

  <fieldset class="control">
    <legend class="label">Enabled</legend>
    <label class="row">
      <input
        type="checkbox"
        class="toggle"
        checked={enabled}
        onchange={toggleEnabled}
      />
      <span
        >Generate summaries for notes (the banner appears at the top of each
        note).</span
      >
    </label>
  </fieldset>

  <p class="privacy">
    <span class="material-symbols-outlined" aria-hidden="true">shield</span>
    <span>
      Note content is sent only to your configured AI endpoint — local or remote
      — to generate the summary. No other note data is sent. See <strong
        >Settings &rarr; AI Provider &rarr; Recent AI activity</strong
      > for the call log.
    </span>
  </p>

  {#if loaded}
    <fieldset class="control">
      <legend class="label">When to generate</legend>
      <label class="row"
        ><input
          type="radio"
          name="when"
          value="auto"
          checked={draft.auto_on_open && !draft.on_demand_only}
          onchange={() => {
            void write('auto_on_open', true)
            void write('on_demand_only', false)
          }}
        />
        <span>Automatically on note open</span></label
      >
      <label class="row"
        ><input
          type="radio"
          name="when"
          value="ondemand"
          checked={draft.on_demand_only}
          onchange={() => {
            void write('on_demand_only', true)
            void write('auto_on_open', false)
          }}
        />
        <span>Only when I click Regenerate (on-demand)</span></label
      >
    </fieldset>

    <fieldset class="control">
      <legend class="label">Summary length</legend>
      <select
        class="select"
        aria-label="Summary length"
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
    </fieldset>

    <fieldset class="control">
      <legend class="label">Facets to show</legend>
      <label class="row"
        ><input
          type="checkbox"
          checked={draft.facets.tasks}
          onchange={(e) => writeFacet('tasks', e.currentTarget.checked)}
        /><span>Tasks</span></label
      >
      <label class="row"
        ><input
          type="checkbox"
          checked={draft.facets.risks}
          onchange={(e) => writeFacet('risks', e.currentTarget.checked)}
        /><span>Risks</span></label
      >
      <label class="row"
        ><input
          type="checkbox"
          checked={draft.facets.decisions}
          onchange={(e) => writeFacet('decisions', e.currentTarget.checked)}
        /><span>Decisions</span></label
      >
    </fieldset>

    <fieldset class="control">
      <legend class="label">Regenerate debounce (ms after save)</legend>
      <input
        type="number"
        class="number"
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
    </fieldset>

    <fieldset class="control">
      <legend class="label">Max note size (characters)</legend>
      <input
        type="number"
        class="number"
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
      <p class="hint">
        Notes larger than this are skipped (chunking is a future enhancement).
        Lower it to cap compute on long notes.
      </p>
    </fieldset>
  {/if}
</section>

<style>
  .aisettings {
    display: flex;
    flex-direction: column;
    gap: 18px;
    padding: 1.5rem;
    max-width: 46rem;
    color: var(--color-text-primary);
    font-size: 0.875rem;
  }
  .head {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .title {
    margin: 0;
    font-size: 1.1rem;
    font-weight: 600;
  }
  .desc {
    margin: 0;
    color: var(--color-text-muted);
    line-height: 1.5;
  }
  .note {
    display: flex;
    align-items: flex-start;
    gap: 8px;
    margin: 0;
    padding: 8px 12px;
    border-radius: 8px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 8%,
      var(--color-surface-card)
    );
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-glow) 22%, transparent);
    color: var(--color-text-primary);
    line-height: 1.5;
  }
  /* First Silt plugin that sends note content externally — the notice sits
     above the tuning controls and reads as a callout, not fineprint: stronger
     accent tint + a 3px left rail (the established .silt-callout idiom). */
  .privacy {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    margin: 0;
    padding: 10px 14px;
    border-radius: 8px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 14%,
      var(--color-surface-card)
    );
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-glow) 30%, transparent);
    border-left: 3px solid var(--color-accent-primary-start);
    color: var(--color-text-primary);
    font-size: 0.85rem;
    line-height: 1.5;
  }
  .note .material-symbols-outlined {
    color: var(--color-accent-primary-start);
    font-size: 18px;
    flex-shrink: 0;
  }
  .unconfigured {
    flex-wrap: wrap;
    align-items: center;
  }
  /* CTA — same accent-primary-start recipe as SummaryBanner's .inline-cta
     so every AI-summary deep-link reads as the same affordance kind. */
  .cta {
    margin-left: auto;
    padding: 4px 12px;
    border-radius: 6px;
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-start) 35%, transparent);
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 10%,
      transparent
    );
    color: var(--color-text-primary);
    font-size: 0.85rem;
    font-weight: 500;
    cursor: pointer;
    white-space: nowrap;
    transition:
      background 0.12s ease,
      color 0.12s ease;
  }
  .cta:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 18%,
      transparent
    );
  }
  .cta:active {
    filter: brightness(0.92);
  }
  .cta:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .privacy .material-symbols-outlined {
    color: var(--color-text-muted);
    font-size: 18px;
    flex-shrink: 0;
  }
  .control {
    display: flex;
    flex-direction: column;
    gap: 8px;
    border: none;
    padding: 0;
    margin: 0;
  }
  .label {
    font-size: 0.8rem;
    font-weight: 600;
    color: var(--color-text-muted);
    padding: 0;
    margin-bottom: 2px;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--color-text-primary);
    cursor: pointer;
  }
  .row input[type='checkbox'],
  .row input[type='radio'] {
    accent-color: var(--color-accent-primary-start);
  }
  .select,
  .number {
    padding: 6px 8px;
    border-radius: 6px;
    border: 1px solid
      var(--color-border-active, var(--color-surface-panel-border, #444));
    background: var(--color-surface-card);
    color: var(--color-text-primary);
    font-size: 0.85rem;
    max-width: 26rem;
  }
  .number {
    max-width: 10rem;
  }
  .hint {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-text-muted);
    line-height: 1.4;
  }
</style>
