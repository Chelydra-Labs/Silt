<script lang="ts">
  // Writing Assistant fine-tuning. Master enablement is Settings → AI (#632).
  import { untrack } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import {
    aiProviderNeedsSetup,
    embeddingProviderNeedsSetup
  } from '../../../settings/ai-setup'
  import { settings } from '../../../settings/store.svelte'
  import { ACTION_CATALOG } from './catalog'
  import { DEFAULT_SETTINGS, resolveSettings } from './settings'
  import type { ActionId, AssistantSettings as Settings } from './types'

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

  let draft = $state<Settings>({
    ...DEFAULT_SETTINGS,
    actions_enabled: { ...DEFAULT_SETTINGS.actions_enabled },
    prompt_overrides: { ...DEFAULT_SETTINGS.prompt_overrides }
  })
  let loaded = $state(false)

  const chatUnconfigured = $derived(
    aiProviderNeedsSetup(settings.config?.ai?.chat)
  )
  const embedUnconfigured = $derived(
    embeddingProviderNeedsSetup(settings.config?.ai?.embedding)
  )

  async function refresh() {
    try {
      const raw = (await ctx.getPluginSettings()) as Record<string, unknown>
      draft = resolveSettings(raw)
    } catch {
      draft = resolveSettings(null)
    }
    loaded = true
  }
  untrack(() => {
    refresh().catch(() => {
      loaded = true
    })
  })

  async function write<K extends keyof Settings>(key: K, value: Settings[K]) {
    draft = { ...draft, [key]: value } as Settings
    try {
      await ctx.updatePluginSetting(key as string, value as never)
    } catch {
      /* best-effort */
    }
  }

  async function toggleAction(id: ActionId, on: boolean) {
    const actions_enabled = { ...draft.actions_enabled, [id]: on }
    await write('actions_enabled', actions_enabled)
  }

  async function writeOverride(id: ActionId, value: string) {
    const prompt_overrides = { ...draft.prompt_overrides }
    if (value.trim()) prompt_overrides[id] = value
    else delete prompt_overrides[id]
    await write('prompt_overrides', prompt_overrides)
  }
</script>

<div
  class:p-6={!embedded}
  class="space-y-6 {embedded ? 'w-full' : 'max-w-4xl mx-auto w-full'}"
>
  {#if !embedded}
    <header class="space-y-1">
      <h2 class="text-text-primary text-type-xl font-bold m-0">
        {manifest?.name ?? 'Writing Assistant'}
      </h2>
      <p class="text-text-muted text-type-md font-body-md leading-relaxed m-0">
        {manifest?.description ??
          'Curated AI writing actions with accept/reject before anything is written.'}
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
          onclick={() => ctx.openSettings('ai')}
        >
          Settings → AI → Features
        </button>
        (Enable AI). This page is action catalog fine-tuning only. AI never writes
        until you accept a proposal.
      </p>
    </section>
  {/if}

  <!-- Models readiness -->
  <section
    class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
    aria-labelledby="wa-models-heading"
  >
    <div class="space-y-0.5">
      <h3
        id="wa-models-heading"
        class="text-text-primary text-type-md font-semibold m-0"
      >
        Models
      </h3>
      <p class="text-text-muted text-type-xs font-label-sm m-0">
        Writing actions use the chat model; related-note suggestions need an
        embedding model. Configure both on
        <button
          type="button"
          class="text-accent-primary-start underline bg-transparent border-none p-0 cursor-pointer font-inherit"
          onclick={() => ctx.openSettings('ai')}
        >
          Settings → AI
        </button>
        .
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
          Embedding (related notes)
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
      <div
        class="flex items-start gap-3 p-3.5 rounded-lg bg-accent-primary-glow/20 border border-accent-primary-start/30"
        role="status"
      >
        <span
          class="material-symbols-outlined text-accent-primary-start text-icon-md flex-shrink-0 mt-0.5"
          aria-hidden="true">info</span
        >
        <div class="flex-1 min-w-0 space-y-2">
          <p
            class="text-text-primary text-type-xs font-body-md m-0 leading-relaxed"
          >
            {#if chatUnconfigured && embedUnconfigured}
              Chat and embedding models are not configured yet.
            {:else if chatUnconfigured}
              Chat model not configured — most writing actions need it.
            {:else}
              Embedding model not configured — related-note suggestions need it.
            {/if}
          </p>
          <button
            type="button"
            class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-accent-primary-start text-text-on-accent font-label-sm-bold text-type-xs hover:brightness-110 transition-all cursor-pointer border-none"
            onclick={() => ctx.openSettings('ai')}
          >
            Open AI settings
          </button>
        </div>
      </div>
    {/if}
  </section>

  {#if loaded}
    <!-- Actions catalog -->
    <section
      class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      aria-labelledby="wa-actions-heading"
    >
      <div class="space-y-0.5">
        <h3
          id="wa-actions-heading"
          class="text-text-primary text-type-md font-semibold m-0"
        >
          Actions
        </h3>
        <p class="text-text-muted text-type-xs font-label-sm m-0">
          Enable or hide individual writing actions in the chat drawer and slash
          menu.
        </p>
      </div>

      <ul class="list-none m-0 p-0 space-y-2" aria-label="Writing actions">
        {#each ACTION_CATALOG as a (a.id)}
          {@const enabled = draft.actions_enabled[a.id] !== false}
          {@const needsSetup =
            (a.needsChat && chatUnconfigured) ||
            (a.needsEmbed && embedUnconfigured)}
          <li
            class="flex items-start justify-between gap-4 p-3.5 rounded-lg border transition-all {enabled
              ? 'bg-surface-panel/40 border-surface-panel-border'
              : 'bg-surface-panel/10 border-surface-panel-border/50 opacity-80'}"
          >
            <div class="flex items-start gap-3 min-w-0">
              <span
                class="material-symbols-outlined text-icon-lg flex-shrink-0 mt-0.5 {enabled
                  ? 'text-accent-primary-start'
                  : 'text-text-muted'}"
                aria-hidden="true">{a.icon}</span
              >
              <div class="min-w-0 space-y-1">
                <div class="flex flex-wrap items-center gap-2">
                  <span
                    class="text-text-primary text-type-sm font-semibold"
                    id="wa-action-{a.id}-label"
                  >
                    {a.label}
                  </span>
                  {#if a.needsEmbed}
                    <span
                      class="inline-flex items-center px-1.5 py-0.5 rounded bg-surface-panel border border-surface-panel-border text-text-muted text-type-3xs font-label-sm-bold uppercase tracking-wide"
                    >
                      Needs embedding
                    </span>
                  {/if}
                  {#if needsSetup}
                    <span
                      class="inline-flex items-center px-1.5 py-0.5 rounded bg-status-warn/10 border border-status-warn/30 text-status-warn text-type-3xs font-label-sm-bold uppercase tracking-wide"
                    >
                      Setup needed
                    </span>
                  {/if}
                </div>
                <p
                  class="text-text-muted text-type-xs font-label-sm m-0 leading-relaxed"
                >
                  {a.description}
                </p>
              </div>
            </div>
            <label
              class="flex items-center cursor-pointer select-none flex-shrink-0"
              for="wa-action-{a.id}"
            >
              <input
                id="wa-action-{a.id}"
                type="checkbox"
                class="keyring-switch peer sr-only"
                aria-labelledby="wa-action-{a.id}-label"
                checked={enabled}
                onchange={(e) =>
                  void toggleAction(
                    a.id,
                    (e.currentTarget as HTMLInputElement).checked
                  )}
              />
              <span
                aria-hidden="true"
                class="keyring-switch-track"
                class:on={enabled}
              ></span>
            </label>
          </li>
        {/each}
      </ul>
    </section>

    <!-- Tag suggestions -->
    <section
      class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      aria-labelledby="wa-tags-heading"
    >
      <div class="space-y-0.5">
        <h3
          id="wa-tags-heading"
          class="text-text-primary text-type-md font-semibold m-0"
        >
          Tag suggestions
        </h3>
        <p class="text-text-muted text-type-xs font-label-sm m-0">
          Controls for the Suggest tags action.
        </p>
      </div>

      <label
        class="flex items-start justify-between gap-4 cursor-pointer select-none"
        for="wa-vocab-only"
      >
        <span class="min-w-0 space-y-0.5">
          <span
            class="text-text-primary text-type-sm font-semibold block"
            id="wa-vocab-only-label"
          >
            Existing vocabulary only
          </span>
          <span class="text-text-muted text-type-xs font-label-sm block">
            Prefer tags already used in your vault over inventing new ones.
          </span>
        </span>
        <span class="flex items-center flex-shrink-0">
          <input
            id="wa-vocab-only"
            type="checkbox"
            class="keyring-switch peer sr-only"
            aria-labelledby="wa-vocab-only-label"
            checked={draft.existing_vocab_only}
            onchange={(e) =>
              void write(
                'existing_vocab_only',
                (e.currentTarget as HTMLInputElement).checked
              )}
          />
          <span
            aria-hidden="true"
            class="keyring-switch-track"
            class:on={draft.existing_vocab_only}
          ></span>
        </span>
      </label>

      <label class="flex flex-col gap-1.5 max-w-xs" for="wa-max-tags">
        <span
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        >
          Max tag suggestions
        </span>
        <input
          id="wa-max-tags"
          type="number"
          min="1"
          max="50"
          class="w-full rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-type-sm text-text-primary outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start"
          value={draft.max_tag_suggestions}
          onchange={(e) =>
            void write(
              'max_tag_suggestions',
              Number((e.currentTarget as HTMLInputElement).value) || 8
            )}
        />
      </label>
    </section>

    <!-- Limits -->
    <section
      class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      aria-labelledby="wa-limits-heading"
    >
      <div class="space-y-0.5">
        <h3
          id="wa-limits-heading"
          class="text-text-primary text-type-md font-semibold m-0"
        >
          Limits
        </h3>
        <p class="text-text-muted text-type-xs font-label-sm m-0">
          Cap how much note text is sent with each writing action.
        </p>
      </div>

      <label class="flex flex-col gap-1.5 max-w-xs" for="wa-max-input">
        <span
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        >
          Max input characters
        </span>
        <input
          id="wa-max-input"
          type="number"
          min="1000"
          max="100000"
          class="w-full rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-type-sm text-text-primary outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start"
          value={draft.max_input_chars}
          onchange={(e) =>
            void write(
              'max_input_chars',
              Number((e.currentTarget as HTMLInputElement).value) || 12000
            )}
        />
        <span class="text-text-muted text-type-2xs font-label-sm">
          Longer notes are truncated before being sent to the model.
        </span>
      </label>
    </section>

    <!-- Advanced prompt overrides -->
    <details
      class="group bg-surface-panel/20 border border-surface-panel-border rounded-xl"
    >
      <summary
        class="flex items-center justify-between p-4 cursor-pointer select-none list-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
      >
        <div class="flex items-center gap-2.5 min-w-0">
          <span
            class="material-symbols-outlined text-icon-lg text-text-muted"
            aria-hidden="true">tune</span
          >
          <div class="text-left min-w-0">
            <span class="text-type-sm font-semibold text-text-primary block"
              >Advanced prompt overrides</span
            >
            <span class="text-type-2xs text-text-muted block mt-0.5">
              Optional system prompts per action — leave blank for defaults
            </span>
          </div>
        </div>
        <span
          class="material-symbols-outlined text-icon-lg text-text-muted transition-transform group-open:rotate-180 flex-shrink-0"
          aria-hidden="true">expand_more</span
        >
      </summary>
      <div
        class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4 space-y-4"
      >
        <p
          class="text-text-muted text-type-xs font-body-md m-0 leading-relaxed"
        >
          Optional system prompt per chat-based action. Leave blank to use the
          built-in prompts tuned for small models.
        </p>
        {#each ACTION_CATALOG.filter((a) => a.needsChat) as a (a.id)}
          <label class="flex flex-col gap-1.5" for="wa-prompt-{a.id}">
            <span
              class="flex items-center gap-2 text-text-primary text-type-sm font-medium"
            >
              <span
                class="material-symbols-outlined text-icon-sm text-text-muted"
                aria-hidden="true">{a.icon}</span
              >
              {a.label}
            </span>
            <textarea
              id="wa-prompt-{a.id}"
              rows="3"
              class="w-full rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-2 text-text-primary font-mono text-type-sm outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start resize-y min-h-[4.5rem]"
              placeholder="Built-in default"
              value={draft.prompt_overrides[a.id] ?? ''}
              onchange={(e) =>
                void writeOverride(
                  a.id,
                  (e.currentTarget as HTMLTextAreaElement).value
                )}></textarea>
          </label>
        {/each}
      </div>
    </details>
  {:else}
    <p class="text-text-muted text-type-sm m-0" role="status">
      Loading writing settings…
    </p>
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
        <p
          class="text-text-primary text-type-sm font-body-md leading-relaxed m-0"
        >
          Note content and selections are sent only to your configured AI
          endpoint when you run an action. Results are proposals — nothing is
          written until you accept. See
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
  /* Switch track — same contract as Settings → AI / Semantic search. */
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
