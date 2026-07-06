<script lang="ts">
  // Settings → AI Provider tab.
  //
  // Configures the local + cloud AI providers (chat completions and
  // embeddings) that plugins call through ctx.ai.complete / ctx.ai.embed.
  // The page also manages API key storage (OS keyring when available,
  // plaintext config.yaml fallback otherwise), runs a live connection
  // probe, and surfaces a recent-activity audit log.
  //
  // Provider config fields (provider_type / base_url / model / tuning)
  // are bound directly to a locally-tracked AIPublicConfig and persisted
  // on blur via UpdateAIProviderConfig. The API key is treated as a
  // separate explicit-write surface: the input field is the only place
  // the secret ever lives in the DOM, and it is cleared the instant
  // Save lands so the value is never left client-side.
  import { onMount } from 'svelte'
  import {
    GetAIProviderConfig,
    UpdateAIProviderConfig,
    SetAIAPIKey,
    ClearAIAPIKey,
    SetUseKeyring,
    TestAIConnection,
    GetAIAudit,
    ClearAIAudit
  } from '../../../wailsjs/go/main/App.js'
  import type { main } from '../../../wailsjs/go/models'

  type Which = 'chat' | 'embedding'

  type Props = Record<string, never>
  let {}: Props = $props()

  let config = $state<main.AIPublicConfig | null>(null)
  let loadError = $state<string | null>(null)
  let loading = $state(true)

  // Per-provider UI state. Provider config fields live inside `config`
  // (bound directly to inputs); these maps hold the transient UI state
  // that doesn't belong in the saved config — the API-key input field
  // (cleared after every save), the show/hide mask toggle, the in-flight
  // test probe state, and the post-action status flashes.
  let apiKeyInputs = $state<Record<Which, string>>({
    chat: '',
    embedding: ''
  })
  let showKey = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })
  let savingKey = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })
  let clearingKey = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })
  let testing = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })
  type TestOutcome = { ok: boolean; message?: string }
  let testResult = $state<Record<Which, TestOutcome | null>>({
    chat: null,
    embedding: null
  })
  let keySavedFlash = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })

  // Element refs. Wrapped in $state so Svelte 5's bind:this doesn't
  // warn about non-reactive bindings; we only use these to focus the
  // input on the "Replace" affordance click, never to render from.
  let keyInputRefs = $state<Record<Which, HTMLInputElement | null>>({
    chat: null,
    embedding: null
  })

  // Audit log state. Loaded lazily on first expand of the audit
  // <details>; the open binding + $effect below trigger the fetch.
  // `auditState` is a small state machine so the lazy-load effect
  // doesn't refire after a failed probe (auditLoading flips back to
  // false on error, which would otherwise re-trigger loadAudit and
  // loop forever).
  let audit = $state<main.AIAuditEntry[]>([])
  let auditOpen = $state(false)
  type AuditState = 'idle' | 'loading' | 'loaded' | 'error'
  let auditState = $state<AuditState>('idle')
  let auditError = $state<string | null>(null)

  // Backend constants.
  const LOCAL_DEFAULT = 'http://localhost:11434'
  const OPENAI_DEFAULT = 'https://api.openai.com/v1'

  // Plain-object round-trip so Svelte 5's deep proxy can track nested
  // field mutations — Wails returns class instances, which $state does
  // not recursively wrap (so bind:value mutations on instance fields
  // would silently not re-render).
  function toPlain<T>(o: T): T {
    return JSON.parse(JSON.stringify(o))
  }

  onMount(() => {
    void reload()
  })

  async function reload() {
    loading = true
    loadError = null
    try {
      config = toPlain(await GetAIProviderConfig())
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // --- Provider config persistence --------------------------------------

  // validateAdvancedField returns an error message for an out-of-range tuning
  // value, or null when the value is empty/valid. Empty values are fine — they
  // mean "use the provider default" (omitempty on the Go side).
  function advancedFieldError(
    which: Which,
    field: 'temperature' | 'max_tokens' | 'timeout_ms' | 'dimensions'
  ): string | null {
    if (!config) return null
    const v = config[which][field]
    if (v === undefined || v === null || Number.isNaN(v)) return null
    switch (field) {
      case 'temperature':
        if (v < 0 || v > 2) return 'Must be 0–2'
        break
      case 'max_tokens':
        if (v < 1) return 'Must be ≥ 1'
        break
      case 'timeout_ms':
        if (v < 1000) return 'Must be ≥ 1000 ms'
        break
      case 'dimensions':
        if (v < 1) return 'Must be ≥ 1'
        break
    }
    return null
  }

  function hasAdvancedErrors(which: Which): boolean {
    const fields: ('temperature' | 'max_tokens' | 'timeout_ms' | 'dimensions')[] =
      which === 'embedding'
        ? ['temperature', 'max_tokens', 'timeout_ms', 'dimensions']
        : ['temperature', 'max_tokens', 'timeout_ms']
    return fields.some((f) => advancedFieldError(which, f) !== null)
  }

  function persistProvider(which: Which) {
    if (!config) return
    if (hasAdvancedErrors(which)) return // don't persist invalid tuning values
    const b = config[which]
    const patch: main.AIProviderPatch = {
      provider_type: b.provider_type,
      base_url: b.base_url,
      model: b.model,
      temperature: b.temperature,
      max_tokens: b.max_tokens,
      timeout_ms: b.timeout_ms,
      dimensions: b.dimensions
    }
    return UpdateAIProviderConfig(which, patch).catch((e) => {
      console.error('UpdateAIProviderConfig failed:', e)
    })
  }

  // Switching provider type snaps base_url to that type's default
  // unless the user has already typed a custom endpoint, so flipping
  // openai→local and back doesn't clobber an OpenRouter URL.
  function selectProviderType(
    which: Which,
    type: 'local' | 'openai-compatible'
  ) {
    if (!config) return
    const b = config[which]
    if (b.provider_type === type) return
    b.provider_type = type
    if (type === 'local') {
      b.base_url = LOCAL_DEFAULT
    } else if (!b.base_url || b.base_url === LOCAL_DEFAULT) {
      b.base_url = OPENAI_DEFAULT
    }
    void persistProvider(which)
  }

  // --- API key save / clear --------------------------------------------

  async function saveKey(which: Which) {
    const key = apiKeyInputs[which].trim()
    if (!key || savingKey[which]) return
    savingKey[which] = true
    try {
      await SetAIAPIKey(which, key)
      // Mirror has_key locally — GetAIProviderConfig only returns the
      // boolean, not the value, and a full reload would discard any
      // unsaved edits in the other provider's tuning fields.
      if (config) config[which].has_key = true
      // Never leave the secret in the DOM after the save lands.
      apiKeyInputs[which] = ''
      showKey[which] = false
      keySavedFlash[which] = true
      setTimeout(() => {
        keySavedFlash[which] = false
      }, 3500)
    } catch (e) {
      testResult[which] = {
        ok: false,
        message: `Failed to save key: ${e instanceof Error ? e.message : String(e)}`
      }
    } finally {
      savingKey[which] = false
    }
  }

  async function clearKey(which: Which) {
    if (clearingKey[which]) return
    clearingKey[which] = true
    try {
      await ClearAIAPIKey(which)
      if (config) config[which].has_key = false
      keySavedFlash[which] = false
    } catch (e) {
      testResult[which] = {
        ok: false,
        message: `Failed to clear key: ${e instanceof Error ? e.message : String(e)}`
      }
    } finally {
      clearingKey[which] = false
    }
  }

  // --- Test connection --------------------------------------------------

  async function runTest(which: Which) {
    if (testing[which]) return
    testing[which] = true
    // Clear the previous result so the live region re-announces the
    // fresh one rather than appearing unchanged to AT.
    testResult[which] = null
    try {
      const result = await TestAIConnection(which)
      testResult[which] = {
        ok: result.ok,
        message: result.message ?? undefined
      }
    } catch (e) {
      testResult[which] = {
        ok: false,
        message: e instanceof Error ? e.message : String(e)
      }
    } finally {
      testing[which] = false
    }
  }

  // --- Keyring ----------------------------------------------------------

  async function toggleKeyring(on: boolean) {
    if (!config || config.use_keyring === on) return
    try {
      await SetUseKeyring(on)
      config.use_keyring = on
      // The backend opportunistically migrates plaintext keys into the
      // keyring on enable; refresh so keyring_unusable_for and has_key
      // reflect the post-migration state.
      if (on) {
        try {
          config = toPlain(await GetAIProviderConfig())
        } catch {
          // Migration refresh is best-effort; the toggle itself succeeded.
        }
      }
    } catch (e) {
      loadError = `Failed to update key storage: ${e instanceof Error ? e.message : String(e)}`
    }
  }

  // --- Audit log --------------------------------------------------------

  async function loadAudit() {
    if (auditState === 'loading') return
    auditState = 'loading'
    auditError = null
    try {
      audit = toPlain(await GetAIAudit())
      auditState = 'loaded'
    } catch (e) {
      auditError = e instanceof Error ? e.message : String(e)
      auditState = 'error'
    }
  }

  async function clearAudit() {
    try {
      await ClearAIAudit()
      audit = []
    } catch (e) {
      auditError = e instanceof Error ? e.message : String(e)
      auditState = 'error'
    }
  }

  // Audit panel lazy-loads on first open. bind:open on <details> keeps
  // state and DOM in sync; this effect triggers the fetch the first
  // time the user expands the panel. Guarded by `auditState === 'idle'`
  // so a failed probe doesn't loop.
  $effect(() => {
    if (auditOpen && auditState === 'idle') {
      void loadAudit()
    }
  })

  // --- Derived + helpers ------------------------------------------------

  // Show the setup nudge only when both providers are still on their
  // local defaults and no key has been entered for either — i.e. the
  // user has made no move yet. Once they touch anything we get out of
  // their way.
  let needsSetup = $derived.by(() => {
    if (!config) return false
    const stillLocal = (b: main.AIPublicProvider) =>
      b.provider_type === 'local' &&
      (!b.base_url || b.base_url === LOCAL_DEFAULT)
    return (
      stillLocal(config.chat) &&
      stillLocal(config.embedding) &&
      !config.chat.has_key &&
      !config.embedding.has_key
    )
  })

  function keyringFellBack(which: Which): boolean {
    if (!config) return false
    return (config.keyring_unusable_for ?? []).includes(which)
  }
</script>

<div class="p-6 max-w-3xl space-y-8">
  {#if loading}
    <div
      class="text-text-muted text-[12px] font-body-md animate-pulse py-8 text-center"
    >
      Loading AI configuration…
    </div>
  {:else if loadError && !config}
    <div
      class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-[12px] font-body-md"
      role="alert"
    >
      <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
        >error</span
      >
      <span class="flex-1"
        >Failed to load AI configuration: {loadError}</span
      >
      <button
        type="button"
        onclick={() => void reload()}
        class="text-[11px] font-label-sm-bold underline bg-transparent border-none cursor-pointer text-error"
      >
        Retry
      </button>
    </div>
  {:else if config}
    <!-- Intro -->
    <section aria-labelledby="ai-intro-heading">
      <h3
        id="ai-intro-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-2"
      >
        AI Provider
      </h3>
      <p class="text-text-muted text-[12px] font-body-md">
        Plugins call
        <code class="font-mono text-[11px]">ctx.ai.complete()</code> for chat
        and <code class="font-mono text-[11px]">ctx.ai.embed()</code> for
        embeddings. Configure which model server handles those calls here.
      </p>
      {#if needsSetup}
        <div
          class="mt-3 bg-accent-primary-glow border border-accent-primary-start/30 rounded-lg p-3 flex items-start gap-2.5"
        >
          <span
            class="material-symbols-outlined text-accent-primary-start text-[18px] mt-0.5 flex-shrink-0"
            aria-hidden="true">lightbulb</span
          >
          <div class="flex-1 text-[12px] font-body-md text-text-primary">
            <strong class="text-accent-primary-start"
              >Set up an AI provider.</strong
            >
            Stay on <em>Local</em> if you run Ollama at the default URL, or
            switch to <em>OpenAI-compatible</em> and add an API key to use a
            cloud endpoint.
          </div>
        </div>
      {/if}
    </section>

    {#snippet providerCard(which: Which)}
      {@const b = config![which]}
      {@const idPrefix = `ai-${which}`}
      {@const typeLabel =
        which === 'chat' ? 'Chat provider type' : 'Embedding provider type'}
      {@const isLocal = b.provider_type === 'local'}
      {@const testingNow = testing[which]}
      {@const result = testResult[which]}
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-4"
      >
        <!-- Provider type -->
        <div>
          <span
            id="{idPrefix}-type-label"
            class="text-text-muted text-[10px] font-semibold uppercase tracking-wider block mb-1.5"
          >
            {typeLabel}
          </span>
          <div
            role="radiogroup"
            aria-labelledby="{idPrefix}-type-label"
            class="inline-flex bg-surface-panel border border-surface-panel-border rounded-lg p-1 gap-1"
          >
            <button
              type="button"
              role="radio"
              aria-checked={isLocal}
              onclick={() => selectProviderType(which, 'local')}
              class="flex items-center gap-1.5 px-3 py-1.5 rounded-md font-label-sm text-label-sm motion-reduce:transition-none transition-colors border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
              class:bg-hover={isLocal}
              class:text-accent-primary-start={isLocal}
              class:text-text-muted={!isLocal}
              class:hover:text-text-primary={!isLocal}
              class:ring-1={isLocal}
              class:ring-accent-primary-start={isLocal}
            >
              <span
                class="material-symbols-outlined text-[16px]"
                aria-hidden="true">dns</span
              >
              Local (Ollama)
            </button>
            <button
              type="button"
              role="radio"
              aria-checked={!isLocal}
              onclick={() => selectProviderType(which, 'openai-compatible')}
              class="flex items-center gap-1.5 px-3 py-1.5 rounded-md font-label-sm text-label-sm motion-reduce:transition-none transition-colors border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
              class:bg-hover={!isLocal}
              class:text-accent-primary-start={!isLocal}
              class:text-text-muted={isLocal}
              class:hover:text-text-primary={isLocal}
              class:ring-1={!isLocal}
              class:ring-accent-primary-start={!isLocal}
            >
              <span
                class="material-symbols-outlined text-[16px]"
                aria-hidden="true">cloud</span
              >
              OpenAI-compatible
            </button>
          </div>
          <!-- Privacy notice: local stays on-device; cloud sends content out. -->
          <p
            class="text-[11px] font-label-sm mt-2 flex items-center gap-1.5 {isLocal
              ? 'text-text-muted'
              : 'text-text-primary'}"
          >
            <span class="material-symbols-outlined text-[14px]" aria-hidden="true">
              {isLocal ? 'shield' : 'arrow_outward'}
            </span>
            {#if isLocal}
              Runs on your machine — content sent to this provider doesn't leave
              this device.
            {:else}
              Content sent to this endpoint leaves your machine.
            {/if}
          </p>
        </div>

        <!-- Base URL + Model -->
        <div class="grid grid-cols-1 sm:grid-cols-2 gap-4">
          <label class="flex flex-col gap-1.5" for="{idPrefix}-base-url">
            <span
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              >Base URL</span
            >
            <input
              id="{idPrefix}-base-url"
              type="url"
              bind:value={b.base_url}
              onblur={() => void persistProvider(which)}
              autocomplete="off"
              spellcheck="false"
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
          </label>
          <label class="flex flex-col gap-1.5" for="{idPrefix}-model">
            <span
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              >Model</span
            >
            <input
              id="{idPrefix}-model"
              type="text"
              bind:value={b.model}
              onblur={() => void persistProvider(which)}
              autocomplete="off"
              spellcheck="false"
              placeholder={which === 'chat'
                ? 'llama3.1, gpt-4o'
                : 'text-embedding-3-small, nomic-embed-text'}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
          </label>
        </div>
        {#if isLocal}
          <p class="text-text-muted text-[11px] font-label-sm -mt-2">
            Ollama's default is
            <code class="font-mono text-[11px]">{LOCAL_DEFAULT}</code>. Local
            servers usually don't need a key.
          </p>
        {/if}

        <!-- API key -->
        <div class="space-y-1.5">
          <div class="flex items-center justify-between gap-3">
            <label
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              for="{idPrefix}-key"
            >
              API key
            </label>
            {#if b.has_key}
              <span class="flex items-center gap-2 flex-wrap justify-end">
                <span
                  class="inline-flex items-center gap-1 text-[11px] font-label-sm-bold text-accent-primary-start"
                >
                  <span
                    class="material-symbols-outlined text-[14px]"
                    aria-hidden="true">check_circle</span
                  >
                  Key set
                </span>
                <button
                  type="button"
                  onclick={() => keyInputRefs[which]?.focus()}
                  class="text-[11px] font-label-sm-bold underline bg-transparent border-none cursor-pointer text-text-muted hover:text-text-primary p-0"
                >
                  Replace
                </button>
                <button
                  type="button"
                  onclick={() => void clearKey(which)}
                  disabled={clearingKey[which]}
                  class="text-[11px] font-label-sm-bold underline bg-transparent border-none cursor-pointer text-text-muted hover:text-error disabled:opacity-40 disabled:cursor-not-allowed disabled:no-underline p-0"
                >
                  {clearingKey[which] ? 'Clearing…' : 'Clear key'}
                </button>
              </span>
            {/if}
          </div>
          <div class="flex items-center gap-2">
            <input
              id="{idPrefix}-key"
              type={showKey[which] ? 'text' : 'password'}
              bind:value={apiKeyInputs[which]}
              bind:this={keyInputRefs[which]}
              autocomplete="off"
              spellcheck="false"
              placeholder={b.has_key
                ? 'Enter new key to replace the stored one'
                : isLocal
                  ? 'Optional — local servers usually need no key'
                  : 'sk-…'}
              onkeydown={(e: KeyboardEvent) => {
                if (e.key === 'Enter') {
                  e.preventDefault()
                  void saveKey(which)
                }
              }}
              class="flex-1 bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
            />
            <button
              type="button"
              onclick={() => (showKey[which] = !showKey[which])}
              aria-pressed={showKey[which]}
              aria-label={showKey[which]
                ? `Hide ${which} API key`
                : `Show ${which} API key`}
              title={showKey[which] ? 'Hide' : 'Show'}
              class="flex-shrink-0 inline-flex items-center justify-center w-9 h-9 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted hover:text-text-primary hover:border-accent-primary-start transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
            >
              <span
                class="material-symbols-outlined text-[18px]"
                aria-hidden="true"
              >
                {showKey[which] ? 'visibility_off' : 'visibility'}
              </span>
            </button>
            <button
              type="button"
              onclick={() => void saveKey(which)}
              disabled={!apiKeyInputs[which].trim() || savingKey[which]}
              class="flex-shrink-0 inline-flex items-center gap-1.5 px-3 py-2 rounded-lg bg-accent-primary-start/20 border border-accent-primary-start/40 text-accent-primary-start font-label-sm-bold hover:brightness-110 motion-reduce:transition-none transition-all cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-40 disabled:cursor-not-allowed"
            >
              {#if savingKey[which]}
                <span
                  class="material-symbols-outlined text-[16px] animate-spin"
                  aria-hidden="true">progress_activity</span
                >
                Saving…
              {:else}
                Save key
              {/if}
            </button>
          </div>
          {#if keyringFellBack(which) && b.has_key}
            <p
              class="text-[11px] font-label-sm text-status-warn flex items-center gap-1"
            >
              <span
                class="material-symbols-outlined text-[14px]"
                aria-hidden="true">warning</span
              >
              The keyring was unreachable; this key was saved to
              config.yaml instead.
            </p>
          {/if}
          {#if keySavedFlash[which]}
            <p
              class="text-[11px] font-label-sm text-accent-primary-start"
              role="status"
            >
              Key saved.
            </p>
          {/if}
        </div>

        <!-- Test connection -->
        <div class="space-y-1.5">
          <button
            type="button"
            onclick={() => void runTest(which)}
            disabled={testingNow}
            class="inline-flex items-center gap-1.5 px-3 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-accent-primary-start motion-reduce:transition-none transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 disabled:opacity-60 disabled:cursor-not-allowed"
          >
            {#if testingNow}
              <span
                class="material-symbols-outlined text-[16px] animate-spin"
                aria-hidden="true">progress_activity</span
              >
              Testing…
            {:else}
              <span
                class="material-symbols-outlined text-[16px]"
                aria-hidden="true">bolt</span
              >
              Test connection
            {/if}
          </button>
          <!-- Live regions: success in role=status (polite), failure in
               role=alert (assertive). Both are conditional so the
               region only renders when there is a result to convey;
               clearing testResult before each probe guarantees AT
               re-announces the new outcome. -->
          {#if result?.ok}
            <p
              class="text-[12px] font-body-md text-accent-primary-start flex items-start gap-1.5"
              role="status"
            >
              <span
                class="material-symbols-outlined text-[14px] mt-0.5"
                aria-hidden="true">check_circle</span
              >
              <span
                >Connected{result.message ? ` · ${result.message}` : ''}</span
              >
            </p>
          {/if}
          {#if result && !result.ok}
            <p
              class="text-[12px] font-body-md text-error flex items-start gap-1.5"
              role="alert"
            >
              <span
                class="material-symbols-outlined text-[14px] mt-0.5"
                aria-hidden="true">error</span
              >
              <span
                >Connection failed{result.message
                  ? ` · ${result.message}`
                  : ''}</span
              >
            </p>
          {/if}
        </div>

        <!-- Advanced (progressive disclosure) -->
        <details class="advanced-details">
          <summary
            class="cursor-pointer text-text-muted text-[11px] font-label-sm-bold uppercase tracking-wider hover:text-text-primary select-none focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded"
          >
            Advanced
          </summary>
          <div
            class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4 mt-3"
          >
            <label
              class="flex flex-col gap-1.5"
              for="{idPrefix}-temperature"
            >
              <span
                class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
                >Temperature</span
              >
              <input
                id="{idPrefix}-temperature"
                type="number"
                min="0"
                max="2"
                step="0.1"
                bind:value={b.temperature}
                onblur={() => void persistProvider(which)}
                class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
              />
              {#if advancedFieldError(which, 'temperature')}
                <span class="text-error text-[10px] font-label-sm" role="alert">{advancedFieldError(which, 'temperature')}</span>
              {/if}
            </label>
            <label
              class="flex flex-col gap-1.5"
              for="{idPrefix}-max-tokens"
            >
              <span
                class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
                >Max tokens</span
              >
              <input
                id="{idPrefix}-max-tokens"
                type="number"
                min="1"
                bind:value={b.max_tokens}
                onblur={() => void persistProvider(which)}
                class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
              />
              {#if advancedFieldError(which, 'max_tokens')}
                <span class="text-error text-[10px] font-label-sm" role="alert">{advancedFieldError(which, 'max_tokens')}</span>
              {/if}
            </label>
            <label class="flex flex-col gap-1.5" for="{idPrefix}-timeout">
              <span
                class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
                >Timeout (ms)</span
              >
              <input
                id="{idPrefix}-timeout"
                type="number"
                min="1000"
                step="500"
                bind:value={b.timeout_ms}
                onblur={() => void persistProvider(which)}
                class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
              />
              {#if advancedFieldError(which, 'timeout_ms')}
                <span class="text-error text-[10px] font-label-sm" role="alert">{advancedFieldError(which, 'timeout_ms')}</span>
              {/if}
            </label>
            {#if which === 'embedding'}
              <label
                class="flex flex-col gap-1.5"
                for="{idPrefix}-dimensions"
              >
                <span
                  class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
                  >Dimensions</span
                >
                <input
                  id="{idPrefix}-dimensions"
                  type="number"
                  min="1"
                  bind:value={b.dimensions}
                  onblur={() => void persistProvider(which)}
                  class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start transition-colors"
                />
                {#if advancedFieldError(which, 'dimensions')}
                  <span class="text-error text-[10px] font-label-sm" role="alert">{advancedFieldError(which, 'dimensions')}</span>
                {/if}
              </label>
            {/if}
          </div>
        </details>
      </div>
    {/snippet}

    <!-- Chat provider -->
    <section aria-labelledby="chat-heading">
      <h3
        id="chat-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Chat model
      </h3>
      {@render providerCard('chat')}
    </section>

    <!-- Embedding provider -->
    <section aria-labelledby="embedding-heading">
      <h3
        id="embedding-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Embedding model
      </h3>
      {@render providerCard('embedding')}
    </section>

    <!-- Key storage -->
    <section aria-labelledby="keyring-heading">
      <h3
        id="keyring-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Key storage
      </h3>
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-5 space-y-3"
      >
        {#if !config.keyring_available}
          <div
            class="flex items-start gap-2 p-3 rounded-lg bg-status-warn/5 border border-status-warn/30 text-status-warn text-[12px] font-body-md"
            role="alert"
          >
            <span
              class="material-symbols-outlined text-[18px] mt-0.5 flex-shrink-0"
              aria-hidden="true">warning</span
            >
            <span class="flex-1">
              No OS keyring was found on this system. Keys will be stored in
              <code class="font-mono text-[11px]">config.yaml</code> regardless
              of this setting.
            </span>
          </div>
        {/if}
        <label
          class="flex items-start gap-3 cursor-pointer select-none"
          for="ai-keyring-toggle"
        >
          <input
            id="ai-keyring-toggle"
            type="checkbox"
            class="keyring-switch peer sr-only"
            checked={config.use_keyring}
            disabled={!config.keyring_available}
            onchange={(e: Event) =>
              void toggleKeyring(
                (e.currentTarget as HTMLInputElement).checked
              )}
          />
          <span
            aria-hidden="true"
            class="keyring-switch-track"
            class:on={config.use_keyring && config.keyring_available}
            class:disabled={!config.keyring_available}
          ></span>
          <span class="flex-1">
            <span class="text-text-primary text-[13px] font-body-md block">
              Store API keys in the OS keyring
            </span>
            <span
              class="text-text-muted text-[11px] font-label-sm block mt-0.5"
            >
              When on, keys live in the OS keyring instead of the vault's
              <code class="font-mono text-[11px]">config.yaml</code>, so they
              don't travel when the vault syncs. Turning this off leaves
              existing keyring entries in place until you clear or re-enter
              each key.
            </span>
          </span>
        </label>
      </div>
    </section>

    <!-- Recent activity -->
    <section aria-labelledby="audit-heading">
      <h3
        id="audit-heading"
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Recent AI activity
      </h3>
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl"
      >
        <details bind:open={auditOpen} class="group">
          <summary
            class="cursor-pointer list-none flex items-center justify-between gap-2 px-4 py-3 select-none hover:bg-hover/40 motion-reduce:transition-none transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 rounded-xl"
          >
            <span
              class="flex items-center gap-2 text-text-primary text-[13px] font-body-md"
            >
              <span
                class="material-symbols-outlined text-[16px] text-text-muted group-open:rotate-90 motion-reduce:transition-none transition-transform"
                aria-hidden="true">chevron_right</span
              >
              {auditState === 'loaded'
                ? `${audit.length} ${audit.length === 1 ? 'entry' : 'entries'} recorded`
                : 'Plugin AI calls'}
            </span>
            <span
              class="inline-flex items-center justify-center min-w-[20px] h-5 px-1.5 rounded-full bg-surface-panel border border-surface-panel-border text-text-muted text-[10px] font-label-sm-bold"
              aria-hidden="true"
            >
              {audit.length}
            </span>
          </summary>
          <div class="px-4 pb-4 pt-1">
            {#if auditState === 'loading'}
              <div
                class="text-text-muted text-[12px] font-body-md animate-pulse py-3"
              >
                Loading audit log…
              </div>
            {:else if auditError}
              <div
                class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-[12px] font-body-md"
                role="alert"
              >
                <span
                  class="material-symbols-outlined text-[18px]"
                  aria-hidden="true">error</span
                >
                <span class="flex-1"
                  >Failed to load audit log: {auditError}</span
                >
              </div>
            {:else if audit.length === 0}
              <p class="text-text-muted text-[12px] font-body-md py-3">
                No activity recorded yet.
              </p>
            {:else}
              <div class="overflow-x-auto">
                <table
                  class="w-full text-[11px] font-body-md border-collapse"
                >
                  <caption class="sr-only">
                    Recent plugin AI calls
                  </caption>
                  <thead>
                    <tr
                      class="text-left text-text-muted border-b border-surface-panel-border"
                    >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >When</th
                      >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >Plugin</th
                      >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >Kind</th
                      >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >Host</th
                      >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >Model</th
                      >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >Status</th
                      >
                      <th
                        scope="col"
                        class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-[10px]"
                        >Tokens</th
                      >
                    </tr>
                  </thead>
                  <tbody>
                    {#each audit as entry, i (`${entry.at}:${i}`)}
                      <tr
                        class="border-b border-surface-panel-border/50 text-text-primary"
                      >
                        <td class="py-1.5 pr-3 whitespace-nowrap"
                          >{entry.at}</td
                        >
                        <td class="py-1.5 pr-3">{entry.plugin}</td>
                        <td class="py-1.5 pr-3 capitalize">{entry.kind}</td>
                        <td class="py-1.5 pr-3 truncate max-w-[180px]"
                          >{entry.host}</td
                        >
                        <td class="py-1.5 pr-3 truncate max-w-[160px]"
                          >{entry.model}</td
                        >
                        <td class="py-1.5 pr-3">
                          {#if entry.status === 'ok'}
                            <span
                              class="inline-flex items-center gap-1 text-accent-primary-start"
                            >
                              <span
                                class="material-symbols-outlined text-[12px]"
                                aria-hidden="true">check_circle</span
                              >
                              ok
                            </span>
                          {:else}
                            <span
                              class="inline-flex items-center gap-1 text-error"
                            >
                              <span
                                class="material-symbols-outlined text-[12px]"
                                aria-hidden="true">error</span
                              >
                              {entry.status}
                            </span>
                          {/if}
                        </td>
                        <td class="py-1.5 pr-3 text-text-muted whitespace-nowrap">
                          {entry.total_tokens != null
                            ? entry.total_tokens
                            : '—'}
                        </td>
                      </tr>
                    {/each}
                  </tbody>
                </table>
              </div>
              <div class="mt-3 flex justify-end">
                <button
                  type="button"
                  onclick={() => void clearAudit()}
                  class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted font-label-sm-bold hover:text-error hover:border-error/50 motion-reduce:transition-none transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                >
                  <span
                    class="material-symbols-outlined text-[16px]"
                    aria-hidden="true">delete_sweep</span
                  >
                  Clear log
                </button>
              </div>
            {/if}
          </div>
        </details>
      </div>
    </section>

    {#if loadError}
      <!-- Soft (non-blocking) error banner shown when an action like
           toggling keyring fails after the initial config load. -->
      <div
        class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-[12px] font-body-md"
        role="alert"
      >
        <span
          class="material-symbols-outlined text-[18px]"
          aria-hidden="true">error</span
        >
        <span class="flex-1">{loadError}</span>
        <button
          type="button"
          onclick={() => {
            loadError = null
            void reload()
          }}
          class="text-[11px] font-label-sm-bold underline bg-transparent border-none cursor-pointer text-error"
        >
          Retry
        </button>
      </div>
    {/if}
  {/if}
</div>

<style>
  /* Visually hidden but available to assistive tech. Matches the
     locally-scoped .sr-only in AppearanceTab.svelte (no global utility
     exists). */
  .sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  }

  /* Switch visual for the keyring toggle. The native checkbox is
     visually-hidden (sr-only) but keyboard-focusable; the styled track
     is the next sibling and reflects state via the .on class. The
     peer:focus-visible selector below gives the track a visible ring
     when the underlying checkbox has focus. */
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
    background: #ffffff;
    transition: transform 0.15s ease;
  }
  .keyring-switch-track.on::after {
    transform: translateX(16px);
  }
  .keyring-switch:focus-visible + .keyring-switch-track {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }
</style>
