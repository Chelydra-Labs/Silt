<script lang="ts">
  // Provider configuration card: provider-type selector, base URL + API key,
  // model selector(s), and the live connection probe. Extracted verbatim
  // from AIProviderTab's providerCard/modelSelector snippets — the view is a
  // thin layer over the shared reactive controller (no logic here).
  import {
    LOCAL_DEFAULT,
    PROVIDER_TYPES,
    supportsEmbeddings
  } from './aiProviderController.svelte'
  import type {
    Which,
    AIProviderController
  } from './aiProviderController.svelte'

  interface Props {
    which: Which
    ai: AIProviderController
  }
  let { which, ai }: Props = $props()

  // Mirrors the snippet's {@const} block. {@const} may not sit directly
  // under a plain element, so these become component-scoped deriveds.
  const b = $derived(ai.config![which])
  const idPrefix = $derived(`ai-${which}`)
  const typeLabel = $derived(
    which === 'chat' ? 'Chat Provider Type' : 'Embedding Provider Type'
  )
  const isLocal = $derived(b.provider_type === 'local')
  const embedUnsupported = $derived(
    which === 'embedding' && !supportsEmbeddings(b.provider_type)
  )
  const testingNow = $derived(ai.testing[which])
  const result = $derived(ai.testResult[which])
</script>

{#snippet modelSelector(w: Which, label: string)}
  {@const b = ai.config![w]}
  {@const idPrefix = `ai-${w}`}

  <div class="flex flex-col gap-1.5">
    <span
      class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
      >{label}</span
    >
    <div class="flex items-center gap-2">
      <div class="flex-1 relative min-w-0">
        {#if ai.manualModel[w] || ai.modelLists[w].length === 0}
          <!-- Free-text input -->
          <input
            id="{idPrefix}-model"
            type="text"
            bind:value={b.model}
            onblur={() => void ai.persistModelOnBlur(w)}
            autocomplete="off"
            spellcheck="false"
            placeholder={w === 'chat'
              ? 'gemini-2.0-flash, claude-3-5-sonnet-latest, llama3.1'
              : 'text-embedding-3-small, nomic-embed-text'}
            class="w-full bg-surface-panel border border-surface-panel-border rounded-lg pl-3 pr-8 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
          />
          {#if ai.modelLists[w].length > 0}
            <button
              type="button"
              onclick={() => (ai.manualModel[w] = false)}
              title="Pick from list"
              aria-label="Pick from list"
              class="absolute right-2 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-primary bg-transparent border-none cursor-pointer p-0"
            >
              <span class="material-symbols-outlined text-icon-md">list</span>
            </button>
          {/if}
        {:else}
          <!-- Dropdown select -->
          <select
            id="{idPrefix}-model"
            value={b.model}
            onchange={(e) => {
              const val = e.currentTarget.value
              if (val === '__custom__') {
                ai.manualModel[w] = true
              } else {
                b.model = val
                void ai.persistProvider(w)
              }
            }}
            class="w-full bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all cursor-pointer appearance-none pr-8"
          >
            {#if !ai.modelLists[w].some((m) => m.id === b.model)}
              <option value={b.model}>{b.model || 'Select a model…'}</option>
            {/if}
            {#each ai.modelLists[w] as m (m.id)}
              <option value={m.id}>{m.display_name}</option>
            {/each}
            <option value="__custom__">+ Type model name manually...</option>
          </select>
          <span
            class="material-symbols-outlined text-icon-md text-text-muted absolute right-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
            aria-hidden="true"
          >
            arrow_drop_down
          </span>
        {/if}
      </div>

      <!-- Refresh models button -->
      <button
        type="button"
        onclick={() => void ai.refreshModels(w)}
        disabled={ai.modelLoading[w]}
        title="Refresh models"
        aria-label="Refresh models"
        class="flex-shrink-0 flex items-center justify-center p-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted hover:text-text-primary hover:border-border-active transition-all cursor-pointer disabled:opacity-40"
      >
        <span
          class="material-symbols-outlined text-icon-md"
          class:animate-spin={ai.modelLoading[w]}
        >
          {ai.modelLoading[w] ? 'progress_activity' : 'refresh'}
        </span>
      </button>
    </div>

    {#if ai.modelError[w]}
      <p
        class="text-type-2xs font-label-sm text-error flex items-center gap-1 mt-0.5"
        role="alert"
      >
        <span class="material-symbols-outlined text-type-sm" aria-hidden="true"
          >error</span
        >
        {ai.modelError[w]}
      </p>
    {/if}
  </div>
{/snippet}

<div
  class="bg-surface-panel/10 border border-surface-panel-border/50 rounded-xl p-5 space-y-5"
>
  <!-- Provider type -->
  <div>
    <span
      id="{idPrefix}-type-label"
      class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider block mb-2"
    >
      {typeLabel}
    </span>
    <div
      role="radiogroup"
      aria-labelledby="{idPrefix}-type-label"
      class="grid grid-cols-2 sm:grid-cols-4 gap-2"
    >
      {#each PROVIDER_TYPES as pt (pt.value)}
        {@const selected = b.provider_type === pt.value}
        <button
          type="button"
          role="radio"
          aria-checked={selected}
          onclick={() => ai.selectProviderType(which, pt.value)}
          class="flex items-center gap-2 px-3 py-2 rounded-lg border transition-all duration-150 cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 {selected
            ? 'bg-accent-primary-glow/15 border-accent-primary-start text-accent-primary-start shadow-sm'
            : 'bg-surface-panel/40 border-surface-panel-border text-text-muted hover:border-border-active hover:text-text-primary'}"
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">{pt.icon}</span
          >
          <span class="font-label-sm-bold text-type-xs">{pt.label}</span>
        </button>
      {/each}
    </div>

    <!-- Privacy notice -->
    <p
      class="text-type-xs font-label-sm mt-3 flex items-center gap-1.5 {isLocal
        ? 'text-text-muted'
        : 'text-text-primary'}"
    >
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true">
        {isLocal ? 'shield' : 'arrow_outward'}
      </span>
      {#if isLocal}
        Runs on your machine — content sent to this provider doesn't leave this
        device.
      {:else}
        Content sent to this endpoint leaves your machine.
      {/if}
    </p>
  </div>

  <!-- Balanced URL & Key Grid Row -->
  <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
    <!-- Base URL -->
    <div class="flex flex-col gap-1.5">
      <label
        class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        for="{idPrefix}-base-url"
      >
        Base URL
      </label>
      <input
        id="{idPrefix}-base-url"
        type="url"
        bind:value={b.base_url}
        onblur={() => void ai.persistUrlOnBlur(which)}
        autocomplete="off"
        spellcheck="false"
        class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
      />
      {#if isLocal}
        <p class="text-text-muted text-type-3xs font-label-sm mt-0.5">
          Ollama default is <code class="font-mono text-type-3xs"
            >{LOCAL_DEFAULT}</code
          >.
        </p>
      {/if}
    </div>

    <!-- API Key input -->
    <div class="flex flex-col gap-1.5">
      <label
        class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
        for="{idPrefix}-key"
      >
        API key
      </label>

      <div class="relative w-full">
        <input
          id="{idPrefix}-key"
          type={ai.showKey[which] ? 'text' : 'password'}
          bind:value={ai.apiKeyInputs[which]}
          autocomplete="off"
          spellcheck="false"
          placeholder={b.has_key
            ? '•••••••••••••••••••••••••••••••'
            : isLocal
              ? 'Optional — local servers usually need no key'
              : 'sk-…'}
          onkeydown={(e: KeyboardEvent) => {
            if (e.key === 'Enter') {
              e.preventDefault()
              void ai.saveKey(which)
            }
          }}
          class="w-full bg-surface-panel border border-surface-panel-border rounded-lg pl-3 pr-24 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
        />

        <!-- Inline Action Controls -->
        <div
          class="absolute right-1 top-1/2 -translate-y-1/2 flex items-center gap-0.5"
        >
          <button
            type="button"
            onclick={() => (ai.showKey[which] = !ai.showKey[which])}
            aria-pressed={ai.showKey[which]}
            aria-label={ai.showKey[which]
              ? `Hide ${which} API key`
              : `Show ${which} API key`}
            title={ai.showKey[which] ? 'Hide' : 'Show'}
            class="p-1 text-text-muted hover:text-text-primary bg-transparent border-none cursor-pointer"
          >
            <span
              class="material-symbols-outlined text-icon-md"
              aria-hidden="true"
            >
              {ai.showKey[which] ? 'visibility_off' : 'visibility'}
            </span>
          </button>

          <!-- Save key button (visually hidden when empty, keeps tests passing) -->
          <button
            type="button"
            onclick={() => void ai.saveKey(which)}
            disabled={!ai.apiKeyInputs[which].trim() || ai.savingKey[which]}
            aria-label="Save key"
            class="px-2 py-1 bg-accent-primary-start text-text-on-accent rounded-md font-label-sm-bold text-type-2xs hover:brightness-110 transition-all cursor-pointer"
            class:hidden={!ai.apiKeyInputs[which].trim()}
          >
            Save
          </button>

          <!-- Clear key button -->
          {#if b.has_key}
            <button
              type="button"
              onclick={() => void ai.clearKey(which)}
              disabled={ai.clearingKey[which]}
              aria-label="Clear key"
              class="px-2 py-1 bg-surface-panel border border-surface-panel-border text-text-muted hover:text-error hover:border-error/30 rounded-md font-label-sm-bold text-type-2xs transition-all cursor-pointer"
              class:hidden={ai.apiKeyInputs[which].trim()}
            >
              Clear
            </button>
          {/if}
        </div>
      </div>

      {#if b.has_key && !ai.apiKeyInputs[which].trim()}
        <p
          class="text-type-2xs font-label-sm text-accent-primary-start flex items-center gap-0.5 mt-0.5"
        >
          <span
            class="material-symbols-outlined text-type-sm"
            aria-hidden="true">check_circle</span
          >
          Key configured
        </p>
      {/if}
      {#if ai.keyringFellBack(which) && b.has_key}
        <p
          class="text-type-2xs font-label-sm text-status-warn flex items-center gap-0.5 mt-0.5"
        >
          <span
            class="material-symbols-outlined text-type-sm"
            aria-hidden="true">warning</span
          >
          The keyring was unreachable; this key was saved to config.yaml instead.
        </p>
      {/if}
      {#if ai.keySavedFlash[which]}
        <p
          class="text-type-2xs font-label-sm text-accent-primary-start mt-0.5 font-semibold"
          role="status"
        >
          Key saved.
        </p>
      {/if}
    </div>
  </div>

  <!-- Model Selectors -->
  {#if ai.syncProviders && which === 'chat'}
    <!-- Render both selectors in sync mode -->
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-1">
      <!-- Chat Model -->
      {@render modelSelector('chat', 'Chat Model')}
      <!-- Embedding Model -->
      <div class="flex flex-col gap-1.5">
        {#if !supportsEmbeddings(b.provider_type)}
          <span
            class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
            >Embedding Model</span
          >
          <div
            class="flex items-start gap-2 px-3 py-2 rounded-lg bg-status-warn/5 border border-status-warn/30 text-status-warn text-type-sm font-body-md"
            role="note"
          >
            <span
              class="material-symbols-outlined text-icon-md mt-0.5 flex-shrink-0"
              aria-hidden="true">block</span
            >
            <span class="flex-1"
              >Anthropic does not offer embeddings. Switch to split settings to
              configure a separate embedding provider.</span
            >
          </div>
        {:else}
          {@render modelSelector('embedding', 'Embedding Model')}
        {/if}
      </div>
    </div>
  {:else if !ai.syncProviders}
    <!-- Render single selector in split mode -->
    <div class="pt-1">
      {#if embedUnsupported}
        <span
          class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
          >Model</span
        >
        <div
          class="flex items-start gap-2 px-3 py-2 rounded-lg bg-status-warn/5 border border-status-warn/30 text-status-warn text-type-sm font-body-md"
          role="note"
        >
          <span
            class="material-symbols-outlined text-icon-md mt-0.5 flex-shrink-0"
            aria-hidden="true">block</span
          >
          <span class="flex-1"
            >Anthropic doesn't offer embeddings. Switch to Local or
            OpenAI-compatible for the embedding model.</span
          >
        </div>
      {:else}
        {@render modelSelector(which, 'Model')}
      {/if}
    </div>
  {/if}

  <!-- Test connection button (shows test status for both when synced) -->
  <div
    class="flex flex-col sm:flex-row sm:items-center justify-between gap-3 pt-4 border-t border-surface-panel-border/30"
  >
    <div class="flex-1 min-w-0">
      {#if ai.syncProviders && which === 'chat'}
        <div class="space-y-1">
          {#if ai.testResult.chat?.ok}
            <p
              class="text-type-sm font-body-md text-accent-primary-start flex items-start gap-1.5"
              role="status"
            >
              <span
                class="material-symbols-outlined text-icon-sm mt-0.5"
                aria-hidden="true">check_circle</span
              >
              <span
                >Connected (Chat){ai.testResult.chat.message
                  ? ` · ${ai.testResult.chat.message}`
                  : ''}</span
              >
            </p>
          {/if}
          {#if ai.testResult.chat && !ai.testResult.chat.ok}
            <p
              class="text-type-sm font-body-md text-error flex items-start gap-1.5"
              role="alert"
            >
              <span
                class="material-symbols-outlined text-icon-sm mt-0.5"
                aria-hidden="true">error</span
              >
              <span
                >Connection failed (Chat){ai.testResult.chat.message
                  ? ` · ${ai.testResult.chat.message}`
                  : ''}</span
              >
            </p>
          {/if}

          {#if supportsEmbeddings(b.provider_type)}
            {#if ai.testResult.embedding?.ok}
              <p
                class="text-type-sm font-body-md text-accent-primary-start flex items-start gap-1.5"
                role="status"
              >
                <span
                  class="material-symbols-outlined text-icon-sm mt-0.5"
                  aria-hidden="true">check_circle</span
                >
                <span
                  >Connected (Embedding){ai.testResult.embedding.message
                    ? ` · ${ai.testResult.embedding.message}`
                    : ''}</span
                >
              </p>
            {/if}
            {#if ai.testResult.embedding && !ai.testResult.embedding.ok}
              <p
                class="text-type-sm font-body-md text-error flex items-start gap-1.5"
                role="alert"
              >
                <span
                  class="material-symbols-outlined text-icon-sm mt-0.5"
                  aria-hidden="true">error</span
                >
                <span
                  >Connection failed (Embedding){ai.testResult.embedding.message
                    ? ` · ${ai.testResult.embedding.message}`
                    : ''}</span
                >
              </p>
            {/if}
          {/if}
        </div>
      {:else}
        {#if result?.ok}
          <p
            class="text-type-sm font-body-md text-accent-primary-start flex items-start gap-1.5"
            role="status"
          >
            <span
              class="material-symbols-outlined text-icon-sm mt-0.5"
              aria-hidden="true">check_circle</span
            >
            <span>Connected{result.message ? ` · ${result.message}` : ''}</span>
          </p>
        {/if}
        {#if result && !result.ok}
          <p
            class="text-type-sm font-body-md text-error flex items-start gap-1.5"
            role="alert"
          >
            <span
              class="material-symbols-outlined text-icon-sm mt-0.5"
              aria-hidden="true">error</span
            >
            <span
              >Connection failed{result.message
                ? ` · ${result.message}`
                : ''}</span
            >
          </p>
        {/if}
      {/if}
    </div>

    <button
      type="button"
      onclick={() => {
        if (ai.syncProviders && which === 'chat') {
          void ai.runTestUnified()
        } else {
          void ai.runTest(which)
        }
      }}
      disabled={testingNow ||
        (ai.syncProviders && (ai.testing.chat || ai.testing.embedding))}
      class="flex-shrink-0 inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-accent-primary-start hover:text-accent-primary-start transition-all cursor-pointer disabled:opacity-60"
    >
      {#if testingNow || (ai.syncProviders && which === 'chat' && (ai.testing.chat || ai.testing.embedding))}
        <span
          class="material-symbols-outlined text-icon-md animate-spin"
          aria-hidden="true">progress_activity</span
        >
        Testing…
      {:else}
        <span class="material-symbols-outlined text-icon-md" aria-hidden="true"
          >bolt</span
        >
        Test connection
      {/if}
    </button>
  </div>
</div>
