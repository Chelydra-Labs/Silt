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
  }
  // Location props are part of the settings-page surface contract; unused here.
  let { ctx, manifest }: Props = $props()

  let draft = $state<Settings>({
    ...DEFAULT_SETTINGS,
    actions_enabled: { ...DEFAULT_SETTINGS.actions_enabled },
    prompt_overrides: { ...DEFAULT_SETTINGS.prompt_overrides }
  })
  let loaded = $state(false)

  const chatUnconfigured = $derived(
    aiProviderNeedsSetup(settings.config?.ai?.chat as any)
  )
  const embedUnconfigured = $derived(
    embeddingProviderNeedsSetup(settings.config?.ai?.embedding as any)
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

<div class="p-6 max-w-3xl space-y-6">
  <header class="space-y-1">
    <h2 class="text-text-primary text-type-xl font-bold">
      {manifest?.name ?? 'Writing Assistant'}
    </h2>
    <p class="text-text-muted text-type-md leading-relaxed">
      {manifest?.description ??
        'Curated AI writing actions with accept/reject before anything is written.'}
    </p>
  </header>

  <section
    class="p-4 rounded-xl border border-surface-panel-border bg-surface-panel/10"
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
      (Enable AI). This page is action catalog fine-tuning only. AI never writes until
      you accept a proposal.
    </p>
  </section>

  {#if chatUnconfigured || embedUnconfigured}
    <div
      class="flex flex-col gap-2 p-4 rounded-xl border border-accent-primary-start/30 bg-accent-primary-glow/20"
      role="status"
    >
      <p class="text-text-primary text-type-md">
        {#if chatUnconfigured}
          Chat model not configured — writing actions need Settings → AI.
        {/if}
        {#if embedUnconfigured}
          Embedding model not configured — related-note suggestions need an
          embedding model.
        {/if}
      </p>
      <button
        type="button"
        class="self-start text-accent-primary-start underline text-type-sm"
        onclick={() => ctx.openSettings('ai')}
      >
        Open AI settings
      </button>
    </div>
  {/if}

  {#if loaded}
    <section class="space-y-3">
      <h3 class="text-text-primary font-semibold">Actions</h3>
      {#each ACTION_CATALOG as a (a.id)}
        <label class="flex items-start gap-3">
          <input
            type="checkbox"
            class="mt-1"
            checked={draft.actions_enabled[a.id] !== false}
            onchange={(e) =>
              void toggleAction(
                a.id,
                (e.currentTarget as HTMLInputElement).checked
              )}
          />
          <span>
            <span class="text-text-primary font-medium">{a.label}</span>
            <span class="block text-text-muted text-type-sm"
              >{a.description}</span
            >
          </span>
        </label>
      {/each}
    </section>

    <section class="space-y-3">
      <h3 class="text-text-primary font-semibold">Tag suggestions</h3>
      <label class="flex items-center gap-3">
        <input
          type="checkbox"
          checked={draft.existing_vocab_only}
          onchange={(e) =>
            void write(
              'existing_vocab_only',
              (e.currentTarget as HTMLInputElement).checked
            )}
        />
        <span class="text-text-primary">Existing vocabulary only</span>
      </label>
      <label class="flex flex-col gap-1 max-w-xs">
        <span class="text-text-muted text-type-sm">Max tag suggestions</span>
        <input
          type="number"
          min="1"
          max="50"
          class="rounded-lg border border-surface-panel-border bg-surface-app px-3 py-2 text-text-primary"
          value={draft.max_tag_suggestions}
          onchange={(e) =>
            void write(
              'max_tag_suggestions',
              Number((e.currentTarget as HTMLInputElement).value) || 8
            )}
        />
      </label>
    </section>

    <section class="space-y-3">
      <h3 class="text-text-primary font-semibold">Limits</h3>
      <label class="flex flex-col gap-1 max-w-xs">
        <span class="text-text-muted text-type-sm">Max input characters</span>
        <input
          type="number"
          min="1000"
          max="100000"
          class="rounded-lg border border-surface-panel-border bg-surface-app px-3 py-2 text-text-primary"
          value={draft.max_input_chars}
          onchange={(e) =>
            void write(
              'max_input_chars',
              Number((e.currentTarget as HTMLInputElement).value) || 12000
            )}
        />
      </label>
    </section>

    <section class="space-y-3">
      <h3 class="text-text-primary font-semibold">Advanced prompt overrides</h3>
      <p class="text-text-muted text-type-sm">
        Optional system prompt per action. Leave blank to use the built-in
        small-model prompts.
      </p>
      {#each ACTION_CATALOG.filter((a) => a.needsChat) as a (a.id)}
        <label class="flex flex-col gap-1">
          <span class="text-text-primary text-type-sm font-medium"
            >{a.label}</span
          >
          <textarea
            rows="3"
            class="rounded-lg border border-surface-panel-border bg-surface-app px-3 py-2 text-text-primary font-mono text-type-sm"
            value={draft.prompt_overrides[a.id] ?? ''}
            onchange={(e) =>
              void writeOverride(
                a.id,
                (e.currentTarget as HTMLTextAreaElement).value
              )}></textarea>
        </label>
      {/each}
    </section>
  {/if}
</div>
