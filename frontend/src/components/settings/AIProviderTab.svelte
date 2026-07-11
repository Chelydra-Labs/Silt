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
  import { aiProviderNeedsSetup } from '../../settings/ai-setup'
  import {
    GetAIProviderConfig,
    UpdateAIProviderConfig,
    SetAIAPIKey,
    CopyAIAPIKey,
    ClearAIAPIKey,
    SetUseKeyring,
    TestAIConnection,
    ListModels,
    GetAIAudit,
    ClearAIAudit
  } from '../../../bindings/silt/app.js'
  import type * as main from '../../../bindings/silt/models.js'
  import type * as aiTypes from '../../../bindings/silt/backend/ai/models.js'

  type Which = 'chat' | 'embedding'

  type Props = Record<string, never>
  let {}: Props = $props()

  let config = $state<main.AIPublicConfig | null>(null)
  let loadError = $state<string | null>(null)
  let loading = $state(true)
  let syncProviders = $state(true)

  // Split-mode tab state: tracks which role card is actively shown to the user.
  // The inactive card is hidden via CSS to keep the DOM queryable for Vitest.
  let activeRole = $state<Which>('chat')

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

  // Model discovery state. The dropdown polls ListModels (cached server-side);
  // this holds the client-side copy for rendering. manualModel tracks whether
  // the user opted into the free-text input (or the poll failed/returned empty,
  // which auto-falls-back). When true the <select> is hidden and a text input
  // is shown, preserving the current model value.
  let modelLists = $state<Record<Which, aiTypes.AIModel[]>>({
    chat: [],
    embedding: []
  })
  let modelLoading = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })
  let modelError = $state<Record<Which, string | null>>({
    chat: null,
    embedding: null
  })
  let manualModel = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
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
  const GOOGLE_DEFAULT = 'https://generativelanguage.googleapis.com'
  const ANTHROPIC_DEFAULT = 'https://api.anthropic.com'

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
      if (config) {
        // Sync-by-default if providers match type, url, and key status.
        const sameType =
          config.chat.provider_type === config.embedding.provider_type
        const sameUrl = config.chat.base_url === config.embedding.base_url
        const sameKey = config.chat.has_key === config.embedding.has_key
        syncProviders = sameType && sameUrl && sameKey
      }
      // Load cached model lists (non-forced: no network call on cold start).
      // Best-effort — a failure leaves the dropdown empty with manual fallback.
      void loadModels('chat')
      void loadModels('embedding')
    } catch (e) {
      loadError = e instanceof Error ? e.message : String(e)
    } finally {
      loading = false
    }
  }

  // loadModels reads the server-side cache (no network when cached). On cold
  // start with no cache it returns empty — the dropdown falls back to free-text.
  async function loadModels(which: Which) {
    try {
      const models = toPlain(await ListModels(which, false))
      modelLists[which] = models ?? []
      if (modelLists[which].length === 0) {
        manualModel[which] = true
      } else {
        manualModel[which] = false // Auto-show dropdown if models are cached
      }
    } catch {
      // No cache yet — silent; user can click Refresh to poll.
    }
  }

  // refreshModels forces a server-side poll (bypasses cache) and updates the
  // dropdown. Automatically switches to dropdown mode if models are found,
  // or manual input if empty, eliminating unnecessary pick-from-list clicks.
  async function refreshModels(which: Which) {
    if (modelLoading[which]) return
    modelLoading[which] = true
    modelError[which] = null
    try {
      const models = toPlain(await ListModels(which, true))
      modelLists[which] = models ?? []
      if (modelLists[which].length === 0) {
        manualModel[which] = true
      } else {
        manualModel[which] = false // Auto-switch to dropdown once models land
      }
    } catch (e) {
      modelError[which] = e instanceof Error ? e.message : String(e)
      manualModel[which] = true
    } finally {
      modelLoading[which] = false
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
    const fields: (
      'temperature' | 'max_tokens' | 'timeout_ms' | 'dimensions'
    )[] =
      which === 'embedding'
        ? ['temperature', 'max_tokens', 'timeout_ms', 'dimensions']
        : ['temperature', 'max_tokens', 'timeout_ms']
    return fields.some((f) => advancedFieldError(which, f) !== null)
  }

  type PersistResult = { ok: true } | { ok: false; message: string }

  async function persistProvider(which: Which): Promise<PersistResult> {
    if (!config)
      return { ok: false, message: 'AI provider settings are not loaded.' }
    if (hasAdvancedErrors(which)) {
      return {
        ok: false,
        message: 'Fix invalid advanced settings before testing the connection.'
      }
    }
    const b = config[which]
    const patch: main.AIProviderPatch = {
      provider_type: b.provider_type,
      base_url: b.base_url,
      model: b.model,
      temperature: b.temperature,
      max_tokens: b.max_tokens,
      reasoning_effort: b.reasoning_effort,
      timeout_ms: b.timeout_ms,
      dimensions: b.dimensions
    }
    try {
      await UpdateAIProviderConfig(which, patch)
      return { ok: true }
    } catch (e) {
      console.error('UpdateAIProviderConfig failed:', e)
      return {
        ok: false,
        message: `Failed to save provider settings: ${e instanceof Error ? e.message : String(e)}`
      }
    }
  }

  async function persistProviderWithSync(which: Which): Promise<PersistResult> {
    if (!config) return { ok: false, message: 'No config loaded' }
    if (syncProviders && which === 'chat') {
      config.embedding.provider_type = config.chat.provider_type
      config.embedding.base_url = config.chat.base_url

      const resChat = await persistProvider('chat')
      if (!resChat.ok) return resChat

      if (supportsEmbeddings(config.chat.provider_type)) {
        const resEmbed = await persistProvider('embedding')
        if (!resEmbed.ok) return resEmbed
      }
      return { ok: true }
    } else {
      return persistProvider(which)
    }
  }

  type ProviderType = 'local' | 'openai-compatible' | 'google' | 'anthropic'

  // Switching provider type snaps base_url to that type's canonical default
  // unless the user has a custom endpoint, so flipping between types doesn't
  // clobber a typed URL. Also drops the cached model list (different endpoint).
  function selectProviderType(which: Which, type: ProviderType) {
    if (!config) return

    const updateOne = async (w: Which, t: ProviderType) => {
      const b = config![w]
      // No-op when re-selecting the current type: avoids wiping the cached
      // model list and firing an unnecessary ListModels network poll against
      // the same endpoint.
      if (b.provider_type === t) return true
      const oldDefault = providerDefaultURL(b.provider_type)
      b.provider_type = t
      const nativeTarget = t === 'google' || t === 'anthropic'
      if (nativeTarget || b.base_url === oldDefault || !b.base_url) {
        b.base_url = providerDefaultURL(t)
      }
      modelLists[w] = []
      modelError[w] = null
      manualModel[w] = false

      const persisted = await persistProvider(w)
      if (!persisted.ok) {
        modelError[w] = persisted.message
        manualModel[w] = true
        return false
      }
      void refreshModels(w)
      return true
    }

    if (syncProviders && which === 'chat') {
      void (async () => {
        const ok = await updateOne('chat', type)
        if (ok) {
          const embedType = supportsEmbeddings(type) ? type : 'local'
          await updateOne('embedding', embedType as ProviderType)
        }
      })()
    } else {
      void updateOne(which, type)
    }
  }

  function providerDefaultURL(type: string): string {
    switch (type) {
      case 'local':
        return LOCAL_DEFAULT
      case 'google':
        return GOOGLE_DEFAULT
      case 'anthropic':
        return ANTHROPIC_DEFAULT
      default:
        return OPENAI_DEFAULT
    }
  }

  // Anthropic has no native embeddings endpoint — the embedding block should
  // surface this and disable the model field when anthropic is selected.
  function supportsEmbeddings(type: string): boolean {
    return type !== 'anthropic'
  }

  // --- API key save / clear --------------------------------------------

  async function saveKey(which: Which) {
    const key = apiKeyInputs[which].trim()
    if (!key || savingKey[which]) return
    savingKey[which] = true
    try {
      if (syncProviders && which === 'chat') {
        await SetAIAPIKey('chat', key)
        if (config) {
          config.chat.has_key = true
          if (supportsEmbeddings(config.chat.provider_type)) {
            await SetAIAPIKey('embedding', key)
            config.embedding.has_key = true
          }
        }
        apiKeyInputs.chat = ''
        showKey.chat = false
        keySavedFlash.chat = true
        setTimeout(() => {
          keySavedFlash.chat = false
        }, 3500)
      } else {
        await SetAIAPIKey(which, key)
        if (config) config[which].has_key = true
        apiKeyInputs[which] = ''
        showKey[which] = false
        keySavedFlash[which] = true
        setTimeout(() => {
          keySavedFlash[which] = false
        }, 3500)
      }
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
      if (syncProviders && which === 'chat') {
        await ClearAIAPIKey('chat')
        await ClearAIAPIKey('embedding')
        if (config) {
          config.chat.has_key = false
          config.embedding.has_key = false
        }
        keySavedFlash.chat = false
      } else {
        await ClearAIAPIKey(which)
        if (config) config[which].has_key = false
        keySavedFlash[which] = false
      }
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
    testResult[which] = null
    try {
      const persisted = await persistProvider(which)
      if (!persisted.ok) {
        testResult[which] = { ok: false, message: persisted.message }
        return
      }
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

  async function runTestUnified() {
    if (!config) return
    const testChat = runTest('chat')
    let testEmbed = Promise.resolve()
    if (supportsEmbeddings(config.chat.provider_type)) {
      testEmbed = runTest('embedding')
    }
    await Promise.all([testChat, testEmbed])
  }

  // --- Keyring ----------------------------------------------------------

  async function toggleKeyring(on: boolean) {
    if (!config || config.use_keyring === on) return
    try {
      await SetUseKeyring(on)
      config.use_keyring = on
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

  async function toggleSyncProviders(on: boolean) {
    if (!on) {
      syncProviders = false
      return
    }
    if (!config) {
      syncProviders = true
      return
    }
    // Flip sync on optimistically so the toggle tracks the user's click
    // immediately; rolled back below if the persist or key copy fails.
    syncProviders = true
    // Sync the embedding slot to chat. Anthropic has no embeddings endpoint,
    // so embedding falls back to local (mirrors selectProviderType) rather than
    // persisting a provider_type that can never serve embeddings.
    const chatSupportsEmbed = supportsEmbeddings(config.chat.provider_type)
    config.embedding.provider_type = chatSupportsEmbed
      ? config.chat.provider_type
      : ('local' as ProviderType)
    config.embedding.base_url = chatSupportsEmbed
      ? config.chat.base_url
      : providerDefaultURL('local')

    modelLists['embedding'] = []
    modelError['embedding'] = null
    manualModel['embedding'] = false

    const persisted = await persistProvider('embedding')
    if (!persisted.ok) {
      // Roll back the optimistic toggle and surface the failure — every other
      // failure path in this tab surfaces its error; swallowing it left the UI
      // claiming sync was on while the backend embedding config was stale.
      syncProviders = false
      loadError = persisted.message
      return
    }

    // Share chat's existing key with embedding server-side. The frontend can
    // only see has_key (never the value), so this goes through a backend copy
    // binding rather than re-entering the secret. Skipped for the local
    // fallback (Ollama is keyless) and when chat has no key.
    if (chatSupportsEmbed && config.chat.has_key) {
      try {
        await CopyAIAPIKey('chat', 'embedding')
        config.embedding.has_key = true
      } catch (e) {
        // Copy failed (e.g. keyring unavailable): don't lie about the key.
        // Roll back and surface it so the user knows to re-enter the key.
        syncProviders = false
        loadError = `Failed to share the API key: ${e instanceof Error ? e.message : String(e)}`
        return
      }
    } else {
      config.embedding.has_key = config.chat.has_key && chatSupportsEmbed
    }

    void refreshModels('embedding')
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

  $effect(() => {
    if (auditOpen && auditState === 'idle') {
      void loadAudit()
    }
  })

  // --- Derived + helpers ------------------------------------------------

  function formatAuditTime(iso: string): string {
    const d = new Date(iso)
    if (Number.isNaN(d.getTime())) return iso
    return new Intl.DateTimeFormat(undefined, {
      month: 'short',
      day: 'numeric',
      hour: '2-digit',
      minute: '2-digit'
    }).format(d)
  }

  let needsSetup = $derived.by(() => {
    if (!config) return false
    return aiProviderNeedsSetup(config.chat)
  })

  function keyringFellBack(which: Which): boolean {
    if (!config) return false
    return (config.keyring_unusable_for ?? []).includes(which)
  }

  // Dynamic summaries for Accordion headers to show details at a glance.
  let tuningSummary = $derived.by(() => {
    if (!config) return ''
    if (syncProviders) {
      const chatModel = config.chat.model || 'none'
      const embedModel = config.embedding.model || 'none'
      return `Chat: ${chatModel} (Temp ${config.chat.temperature ?? 'default'}) · Embed: ${embedModel}`
    } else {
      const b = config[activeRole]
      const model = b.model || 'none'
      if (activeRole === 'chat') {
        return `Chat Model: ${model} (Temp ${b.temperature ?? 'default'})`
      } else {
        return `Embedding Model: ${model} (${b.dimensions ?? 'default'} dims)`
      }
    }
  })

  let keyringSummary = $derived.by(() => {
    if (!config) return ''
    if (!config.keyring_available)
      return 'OS Keyring unavailable (fallback active)'
    return config.use_keyring
      ? 'Secure OS Keychain storage enabled'
      : 'Stored in vault configuration'
  })

  let auditSummary = $derived.by(() => {
    if (auditState === 'idle') return 'Click to view log'
    if (auditState === 'loading') return 'Loading logs…'
    if (auditState === 'error') return 'Error loading logs'
    return `${audit.length} call${audit.length === 1 ? '' : 's'} recorded`
  })
</script>

<div class="p-6 max-w-6xl mx-auto w-full space-y-6">
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
      <span class="flex-1">Failed to load AI configuration: {loadError}</span>
      <button
        type="button"
        onclick={() => void reload()}
        class="text-[11px] font-label-sm-bold underline bg-transparent border-none cursor-pointer text-error"
      >
        Retry
      </button>
    </div>
  {:else if config}
    <!-- Intro & nudge banner -->
    <section aria-label="AI provider overview">
      <p class="text-text-primary text-[13px] font-body-md leading-relaxed">
        Connect Silt to an AI model to power smart features like note
        summarization, semantic vault search, and task tracking. Choose a setup
        mode below to get started.
      </p>
      {#if needsSetup}
        <div
          class="mt-4 bg-accent-primary-glow/20 border border-accent-primary-start/30 rounded-xl p-4 flex items-start gap-3"
        >
          <span
            class="material-symbols-outlined text-accent-primary-start text-[20px] mt-0.5 flex-shrink-0"
            aria-hidden="true">lightbulb</span
          >
          <div
            class="flex-1 text-[12px] font-body-md text-text-primary leading-relaxed"
          >
            <strong class="text-accent-primary-start"
              >Set up an AI provider.</strong
            >
            Leave on <em>Local</em> if you are running Ollama on localhost, or
            switch to a cloud provider like <em>Google AI</em> or
            <em>OpenAI</em> for remote API access.
          </div>
        </div>
      {/if}
    </section>

    <!-- Setup Mode & Sync toggle (Pill switch layout) -->
    <section aria-label="Configuration Mode" class="space-y-3">
      <div
        class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-4 flex flex-col sm:flex-row sm:items-center justify-between gap-4"
      >
        <div class="space-y-0.5">
          <span
            id="sync-providers-label"
            class="text-text-primary text-[13px] font-semibold block"
          >
            Sync chat and embedding providers
          </span>
          <span class="text-text-muted text-[11px] font-label-sm block">
            Recommended. Share the same credentials, provider type, and base URL
            for both roles.
          </span>
        </div>
        <label
          class="flex items-center cursor-pointer select-none"
          for="sync-providers-toggle"
        >
          <input
            id="sync-providers-toggle"
            type="checkbox"
            class="keyring-switch peer sr-only"
            aria-labelledby="sync-providers-label"
            checked={syncProviders}
            onchange={(e) => void toggleSyncProviders(e.currentTarget.checked)}
          />
          <span
            aria-hidden="true"
            class="keyring-switch-track"
            class:on={syncProviders}
          ></span>
        </label>
      </div>

      <!-- Split Role switcher (only visible in split mode) -->
      {#if !syncProviders}
        <div
          class="flex p-1 rounded-xl bg-surface-panel/40 border border-surface-panel-border/80 max-w-xs"
          role="tablist"
          aria-label="AI Role Switcher"
        >
          <button
            type="button"
            role="tab"
            aria-selected={activeRole === 'chat'}
            onclick={() => (activeRole = 'chat')}
            class="flex-1 py-1.5 px-3 rounded-lg text-[11px] font-label-sm-bold transition-all cursor-pointer {activeRole ===
            'chat'
              ? 'bg-accent-primary-start text-surface-app shadow-md'
              : 'text-text-muted hover:text-text-primary'}"
          >
            Chat Model
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={activeRole === 'embedding'}
            onclick={() => (activeRole = 'embedding')}
            class="flex-1 py-1.5 px-3 rounded-lg text-[11px] font-label-sm-bold transition-all cursor-pointer {activeRole ===
            'embedding'
              ? 'bg-accent-primary-start text-surface-app shadow-md'
              : 'text-text-muted hover:text-text-primary'}"
          >
            Embedding Model
          </button>
        </div>
      {/if}
    </section>

    {#snippet providerCard(which: Which)}
      {@const b = config![which]}
      {@const idPrefix = `ai-${which}`}
      {@const typeLabel =
        which === 'chat' ? 'Chat Provider Type' : 'Embedding Provider Type'}
      {@const isLocal = b.provider_type === 'local'}
      {@const embedUnsupported =
        which === 'embedding' && !supportsEmbeddings(b.provider_type)}
      {@const testingNow = testing[which]}
      {@const result = testResult[which]}
      {@const providerTypes = [
        {
          value: 'local' as ProviderType,
          icon: 'dns',
          label: 'Local (Ollama)'
        },
        {
          value: 'openai-compatible' as ProviderType,
          icon: 'cloud',
          label: 'OpenAI-compatible'
        },
        {
          value: 'google' as ProviderType,
          icon: 'auto_awesome',
          label: 'Google AI'
        },
        {
          value: 'anthropic' as ProviderType,
          icon: 'psychology',
          label: 'Anthropic'
        }
      ]}
      <div
        class="bg-surface-panel/10 border border-surface-panel-border/50 rounded-xl p-5 space-y-5"
      >
        <!-- Provider type -->
        <div>
          <span
            id="{idPrefix}-type-label"
            class="text-text-muted text-[10px] font-semibold uppercase tracking-wider block mb-2"
          >
            {typeLabel}
          </span>
          <div
            role="radiogroup"
            aria-labelledby="{idPrefix}-type-label"
            class="grid grid-cols-2 sm:grid-cols-4 gap-2"
          >
            {#each providerTypes as pt (pt.value)}
              {@const selected = b.provider_type === pt.value}
              <button
                type="button"
                role="radio"
                aria-checked={selected}
                onclick={() => selectProviderType(which, pt.value)}
                class="flex items-center gap-2 px-3 py-2 rounded-lg border transition-all duration-150 cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 {selected
                  ? 'bg-accent-primary-glow/15 border-accent-primary-start text-accent-primary-start shadow-sm'
                  : 'bg-surface-panel/40 border-surface-panel-border text-text-muted hover:border-border-active hover:text-text-primary'}"
              >
                <span
                  class="material-symbols-outlined text-[16px]"
                  aria-hidden="true">{pt.icon}</span
                >
                <span class="font-label-sm-bold text-[11px]">{pt.label}</span>
              </button>
            {/each}
          </div>

          <!-- Privacy notice -->
          <p
            class="text-[11px] font-label-sm mt-3 flex items-center gap-1.5 {isLocal
              ? 'text-text-muted'
              : 'text-text-primary'}"
          >
            <span
              class="material-symbols-outlined text-[14px]"
              aria-hidden="true"
            >
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

        <!-- Balanced URL & Key Grid Row -->
        <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
          <!-- Base URL -->
          <div class="flex flex-col gap-1.5">
            <label
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              for="{idPrefix}-base-url"
            >
              Base URL
            </label>
            <input
              id="{idPrefix}-base-url"
              type="url"
              bind:value={b.base_url}
              onblur={() => void persistProviderWithSync(which)}
              autocomplete="off"
              spellcheck="false"
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
            />
            {#if isLocal}
              <p class="text-text-muted text-[9px] font-label-sm mt-0.5">
                Ollama default is <code class="font-mono text-[9px]"
                  >{LOCAL_DEFAULT}</code
                >.
              </p>
            {/if}
          </div>

          <!-- API Key input -->
          <div class="flex flex-col gap-1.5">
            <label
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              for="{idPrefix}-key"
            >
              API key
            </label>

            <div class="relative w-full">
              <input
                id="{idPrefix}-key"
                type={showKey[which] ? 'text' : 'password'}
                bind:value={apiKeyInputs[which]}
                autocomplete="off"
                spellcheck="false"
                placeholder={b.has_key
                  ? '••••••••••••••••••••••••••••••••'
                  : isLocal
                    ? 'Optional — local servers usually need no key'
                    : 'sk-…'}
                onkeydown={(e: KeyboardEvent) => {
                  if (e.key === 'Enter') {
                    e.preventDefault()
                    void saveKey(which)
                  }
                }}
                class="w-full bg-surface-panel border border-surface-panel-border rounded-lg pl-3 pr-24 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
              />

              <!-- Inline Action Controls -->
              <div
                class="absolute right-1 top-1/2 -translate-y-1/2 flex items-center gap-0.5"
              >
                <button
                  type="button"
                  onclick={() => (showKey[which] = !showKey[which])}
                  aria-pressed={showKey[which]}
                  aria-label={showKey[which]
                    ? `Hide ${which} API key`
                    : `Show ${which} API key`}
                  title={showKey[which] ? 'Hide' : 'Show'}
                  class="p-1 text-text-muted hover:text-text-primary bg-transparent border-none cursor-pointer"
                >
                  <span
                    class="material-symbols-outlined text-[16px]"
                    aria-hidden="true"
                  >
                    {showKey[which] ? 'visibility_off' : 'visibility'}
                  </span>
                </button>

                <!-- Save key button (visually hidden when empty, keeps tests passing) -->
                <button
                  type="button"
                  onclick={() => void saveKey(which)}
                  disabled={!apiKeyInputs[which].trim() || savingKey[which]}
                  aria-label="Save key"
                  class="px-2 py-1 bg-accent-primary-start text-surface-app rounded-md font-label-sm-bold text-[10px] hover:brightness-110 transition-all cursor-pointer"
                  class:hidden={!apiKeyInputs[which].trim()}
                >
                  Save
                </button>

                <!-- Clear key button -->
                {#if b.has_key}
                  <button
                    type="button"
                    onclick={() => void clearKey(which)}
                    disabled={clearingKey[which]}
                    aria-label="Clear key"
                    class="px-2 py-1 bg-surface-panel border border-surface-panel-border text-text-muted hover:text-error hover:border-error/30 rounded-md font-label-sm-bold text-[10px] transition-all cursor-pointer"
                    class:hidden={apiKeyInputs[which].trim()}
                  >
                    Clear
                  </button>
                {/if}
              </div>
            </div>

            {#if b.has_key && !apiKeyInputs[which].trim()}
              <p
                class="text-[10px] font-label-sm text-accent-primary-start flex items-center gap-0.5 mt-0.5"
              >
                <span
                  class="material-symbols-outlined text-[12px]"
                  aria-hidden="true">check_circle</span
                >
                Key configured
              </p>
            {/if}
            {#if keyringFellBack(which) && b.has_key}
              <p
                class="text-[10px] font-label-sm text-status-warn flex items-center gap-0.5 mt-0.5"
              >
                <span
                  class="material-symbols-outlined text-[12px]"
                  aria-hidden="true">warning</span
                >
                The keyring was unreachable; this key was saved to config.yaml instead.
              </p>
            {/if}
            {#if keySavedFlash[which]}
              <p
                class="text-[10px] font-label-sm text-accent-primary-start mt-0.5 font-semibold"
                role="status"
              >
                Key saved.
              </p>
            {/if}
          </div>
        </div>

        <!-- Model Selectors -->
        {#if syncProviders && which === 'chat'}
          <!-- Render both selectors in sync mode -->
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-4 pt-1">
            <!-- Chat Model -->
            {@render modelSelector('chat', 'Chat Model')}
            <!-- Embedding Model -->
            <div class="flex flex-col gap-1.5">
              {#if !supportsEmbeddings(b.provider_type)}
                <span
                  class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
                  >Embedding Model</span
                >
                <div
                  class="flex items-start gap-2 px-3 py-2 rounded-lg bg-status-warn/5 border border-status-warn/30 text-status-warn text-[12px] font-body-md"
                  role="note"
                >
                  <span
                    class="material-symbols-outlined text-[16px] mt-0.5 flex-shrink-0"
                    aria-hidden="true">block</span
                  >
                  <span class="flex-1"
                    >Anthropic does not offer embeddings. Switch to split
                    settings to configure a separate embedding provider.</span
                  >
                </div>
              {:else}
                {@render modelSelector('embedding', 'Embedding Model')}
              {/if}
            </div>
          </div>
        {:else if !syncProviders}
          <!-- Render single selector in split mode -->
          <div class="pt-1">
            {#if embedUnsupported}
              <span
                class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
                >Model</span
              >
              <div
                class="flex items-start gap-2 px-3 py-2 rounded-lg bg-status-warn/5 border border-status-warn/30 text-status-warn text-[12px] font-body-md"
                role="note"
              >
                <span
                  class="material-symbols-outlined text-[16px] mt-0.5 flex-shrink-0"
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
            {#if syncProviders && which === 'chat'}
              <div class="space-y-1">
                {#if testResult.chat?.ok}
                  <p
                    class="text-[12px] font-body-md text-accent-primary-start flex items-start gap-1.5"
                    role="status"
                  >
                    <span
                      class="material-symbols-outlined text-[14px] mt-0.5"
                      aria-hidden="true">check_circle</span
                    >
                    <span
                      >Connected (Chat){testResult.chat.message
                        ? ` · ${testResult.chat.message}`
                        : ''}</span
                    >
                  </p>
                {/if}
                {#if testResult.chat && !testResult.chat.ok}
                  <p
                    class="text-[12px] font-body-md text-error flex items-start gap-1.5"
                    role="alert"
                  >
                    <span
                      class="material-symbols-outlined text-[14px] mt-0.5"
                      aria-hidden="true">error</span
                    >
                    <span
                      >Connection failed (Chat){testResult.chat.message
                        ? ` · ${testResult.chat.message}`
                        : ''}</span
                    >
                  </p>
                {/if}

                {#if supportsEmbeddings(b.provider_type)}
                  {#if testResult.embedding?.ok}
                    <p
                      class="text-[12px] font-body-md text-accent-primary-start flex items-start gap-1.5"
                      role="status"
                    >
                      <span
                        class="material-symbols-outlined text-[14px] mt-0.5"
                        aria-hidden="true">check_circle</span
                      >
                      <span
                        >Connected (Embedding){testResult.embedding.message
                          ? ` · ${testResult.embedding.message}`
                          : ''}</span
                      >
                    </p>
                  {/if}
                  {#if testResult.embedding && !testResult.embedding.ok}
                    <p
                      class="text-[12px] font-body-md text-error flex items-start gap-1.5"
                      role="alert"
                    >
                      <span
                        class="material-symbols-outlined text-[14px] mt-0.5"
                        aria-hidden="true">error</span
                      >
                      <span
                        >Connection failed (Embedding){testResult.embedding
                          .message
                          ? ` · ${testResult.embedding.message}`
                          : ''}</span
                      >
                    </p>
                  {/if}
                {/if}
              </div>
            {:else}
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
                    >Connected{result.message
                      ? ` · ${result.message}`
                      : ''}</span
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
            {/if}
          </div>

          <button
            type="button"
            onclick={() => {
              if (syncProviders && which === 'chat') {
                void runTestUnified()
              } else {
                void runTest(which)
              }
            }}
            disabled={testingNow ||
              (syncProviders && (testing.chat || testing.embedding))}
            class="flex-shrink-0 inline-flex items-center gap-1.5 px-4 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary font-label-sm-bold hover:border-accent-primary-start hover:text-accent-primary-start transition-all cursor-pointer disabled:opacity-60"
          >
            {#if testingNow || (syncProviders && which === 'chat' && (testing.chat || testing.embedding))}
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
        </div>
      </div>
    {/snippet}

    {#snippet modelSelector(w: Which, label: string)}
      {@const b = config![w]}
      {@const idPrefix = `ai-${w}`}

      <div class="flex flex-col gap-1.5">
        <span
          class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
          >{label}</span
        >
        <div class="flex items-center gap-2">
          <div class="flex-1 relative min-w-0">
            {#if manualModel[w] || modelLists[w].length === 0}
              <!-- Free-text input -->
              <input
                id="{idPrefix}-model"
                type="text"
                bind:value={b.model}
                onblur={() => void persistProvider(w)}
                autocomplete="off"
                spellcheck="false"
                placeholder={w === 'chat'
                  ? 'gemini-2.0-flash, claude-3-5-sonnet-latest, llama3.1'
                  : 'text-embedding-3-small, nomic-embed-text'}
                class="w-full bg-surface-panel border border-surface-panel-border rounded-lg pl-3 pr-8 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
              />
              {#if modelLists[w].length > 0}
                <button
                  type="button"
                  onclick={() => (manualModel[w] = false)}
                  title="Pick from list"
                  aria-label="Pick from list"
                  class="absolute right-2 top-1/2 -translate-y-1/2 text-text-muted hover:text-text-primary bg-transparent border-none cursor-pointer p-0"
                >
                  <span class="material-symbols-outlined text-[16px]">list</span
                  >
                </button>
              {/if}
            {:else}
              <!-- Dropdown select -->
              <select
                id="{idPrefix}-model"
                value={b.model}
                onchange={(e) => {
                  const val = (e.currentTarget as HTMLSelectElement).value
                  if (val === '__custom__') {
                    manualModel[w] = true
                  } else {
                    b.model = val
                    void persistProvider(w)
                  }
                }}
                class="w-full bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all cursor-pointer appearance-none pr-8"
              >
                {#if !modelLists[w].some((m) => m.id === b.model)}
                  <option value={b.model}>{b.model || 'Select a model…'}</option
                  >
                {/if}
                {#each modelLists[w] as m (m.id)}
                  <option value={m.id}>{m.display_name}</option>
                {/each}
                <option value="__custom__">+ Type model name manually...</option
                >
              </select>
              <span
                class="material-symbols-outlined text-[16px] text-text-muted absolute right-2.5 top-1/2 -translate-y-1/2 pointer-events-none"
                aria-hidden="true"
              >
                arrow_drop_down
              </span>
            {/if}
          </div>

          <!-- Refresh models button -->
          <button
            type="button"
            onclick={() => void refreshModels(w)}
            disabled={modelLoading[w]}
            title="Refresh models"
            aria-label="Refresh models"
            class="flex-shrink-0 flex items-center justify-center p-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted hover:text-text-primary hover:border-border-active transition-all cursor-pointer disabled:opacity-40"
          >
            <span
              class="material-symbols-outlined text-[16px]"
              class:animate-spin={modelLoading[w]}
            >
              {modelLoading[w] ? 'progress_activity' : 'refresh'}
            </span>
          </button>
        </div>

        {#if modelError[w]}
          <p
            class="text-[10px] font-label-sm text-error flex items-center gap-1 mt-0.5"
            role="alert"
          >
            <span
              class="material-symbols-outlined text-[12px]"
              aria-hidden="true">error</span
            >
            {modelError[w]}
          </p>
        {/if}
      </div>
    {/snippet}

    {#snippet advancedTuningGrid(w: Which)}
      {@const b = config![w]}
      {@const idPrefix = `ai-${w}`}
      <div class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {#if w === 'chat'}
          <label class="flex flex-col gap-1.5" for="{idPrefix}-temperature">
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
              onblur={() => void persistProvider(w)}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
            />
            {#if advancedFieldError(w, 'temperature')}
              <span class="text-error text-[10px] font-label-sm" role="alert"
                >{advancedFieldError(w, 'temperature')}</span
              >
            {/if}
          </label>

          <label class="flex flex-col gap-1.5" for="{idPrefix}-max-tokens">
            <span
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              >Max tokens</span
            >
            <input
              id="{idPrefix}-max-tokens"
              type="number"
              min="1"
              bind:value={b.max_tokens}
              onblur={() => void persistProvider(w)}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
            />
            {#if advancedFieldError(w, 'max_tokens')}
              <span class="text-error text-[10px] font-label-sm" role="alert"
                >{advancedFieldError(w, 'max_tokens')}</span
              >
            {/if}
          </label>

          <label class="flex flex-col gap-1.5" for="{idPrefix}-reasoning">
            <span
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              >Reasoning effort</span
            >
            <select
              id="{idPrefix}-reasoning"
              value={b.reasoning_effort ?? ''}
              onchange={(e) => {
                const v = (e.currentTarget as HTMLSelectElement).value
                b.reasoning_effort = v || undefined
                void persistProvider(w)
              }}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all cursor-pointer"
            >
              <option value="">Default</option>
              <option value="none">None</option>
              <option value="minimal">Minimal</option>
              <option value="low">Low</option>
              <option value="medium">Medium</option>
              <option value="high">High</option>
              <option value="xhigh">xHigh</option>
              <option value="max">Max</option>
            </select>
          </label>
        {/if}

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
            onblur={() => void persistProvider(w)}
            class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
          />
          {#if advancedFieldError(w, 'timeout_ms')}
            <span class="text-error text-[10px] font-label-sm" role="alert"
              >{advancedFieldError(w, 'timeout_ms')}</span
            >
          {/if}
        </label>

        {#if w === 'embedding'}
          <label class="flex flex-col gap-1.5" for="{idPrefix}-dimensions">
            <span
              class="text-text-muted text-[10px] font-semibold uppercase tracking-wider"
              >Dimensions</span
            >
            <input
              id="{idPrefix}-dimensions"
              type="number"
              min="1"
              bind:value={b.dimensions}
              onblur={() => void persistProvider(w)}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-[13px] font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
            />
            {#if advancedFieldError(w, 'dimensions')}
              <span class="text-error text-[10px] font-label-sm" role="alert"
                >{advancedFieldError(w, 'dimensions')}</span
              >
            {/if}
          </label>
        {/if}
      </div>
    {/snippet}

    <!-- Main Config Area -->
    <div class="space-y-4">
      <!-- Sync Mode: renders the chat card representing both configurations -->
      <!-- Split Mode: renders both cards, using CSS 'hidden' on the inactive one so Vitest can query them -->
      <section
        aria-labelledby="chat-heading"
        class:hidden={!syncProviders && activeRole !== 'chat'}
      >
        {#if !syncProviders}
          <h3
            id="chat-heading"
            class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
          >
            Chat model
          </h3>
        {:else}
          <h3 id="chat-heading" class="sr-only">Chat model</h3>
        {/if}
        {@render providerCard('chat')}
      </section>

      <section
        aria-labelledby="embedding-heading"
        class:hidden={syncProviders || activeRole !== 'embedding'}
      >
        <h3
          id="embedding-heading"
          class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
        >
          Embedding model
        </h3>
        {@render providerCard('embedding')}
      </section>
    </div>

    <!-- Accordion Stack of Collapsible Secondary Panels (Tuning, Keyring, Audit Log) -->
    <div class="space-y-3 pt-4 border-t border-surface-panel-border/30">
      <!-- Section 1: Advanced Options -->
      <details
        class="group bg-surface-panel/10 border border-surface-panel-border rounded-xl"
      >
        <summary
          class="flex items-center justify-between p-4 cursor-pointer select-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
        >
          <div class="flex items-center gap-2.5">
            <span
              class="material-symbols-outlined text-[18px] text-text-muted"
              aria-hidden="true">tune</span
            >
            <div class="text-left">
              <span class="text-[12px] font-semibold text-text-primary block"
                >Advanced Options</span
              >
              <span class="text-[10px] text-text-muted block mt-0.5"
                >{tuningSummary}</span
              >
            </div>
          </div>
          <span
            class="material-symbols-outlined text-[20px] text-text-muted transition-transform group-open:rotate-180"
            aria-hidden="true">expand_more</span
          >
        </summary>
        <div class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4">
          <div class="space-y-5">
            {#if syncProviders}
              <div>
                <h4 class="text-[11px] font-semibold text-text-primary mb-3">
                  Chat Tuning
                </h4>
                {@render advancedTuningGrid('chat')}
              </div>
              {#if supportsEmbeddings(config.chat.provider_type)}
                <div class="border-t border-surface-panel-border/30 pt-4">
                  <h4 class="text-[11px] font-semibold text-text-primary mb-3">
                    Embedding Tuning
                  </h4>
                  {@render advancedTuningGrid('embedding')}
                </div>
              {/if}
            {:else}
              {@render advancedTuningGrid(activeRole)}
            {/if}
          </div>
        </div>
      </details>

      <!-- Section 2: Key Storage -->
      <details
        class="group bg-surface-panel/10 border border-surface-panel-border rounded-xl"
      >
        <summary
          class="flex items-center justify-between p-4 cursor-pointer select-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
        >
          <div class="flex items-center gap-2.5">
            <span
              class="material-symbols-outlined text-[18px] text-text-muted"
              aria-hidden="true">vpn_key</span
            >
            <div class="text-left">
              <span class="text-[12px] font-semibold text-text-primary block"
                >Key storage</span
              >
              <span class="text-[10px] text-text-muted block mt-0.5"
                >{keyringSummary}</span
              >
            </div>
          </div>
          <span
            class="material-symbols-outlined text-[20px] text-text-muted transition-transform group-open:rotate-180"
            aria-hidden="true">expand_more</span
          >
        </summary>
        <div
          class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4 space-y-3"
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
      </details>

      <!-- Section 3: Recent Activity -->
      <details
        bind:open={auditOpen}
        class="group bg-surface-panel/10 border border-surface-panel-border rounded-xl"
      >
        <summary
          class="flex items-center justify-between p-4 cursor-pointer select-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
        >
          <div class="flex items-center gap-2.5">
            <span
              class="material-symbols-outlined text-[18px] text-text-muted"
              aria-hidden="true">history</span
            >
            <div class="text-left">
              <!-- summaryEl in test queries exact text 'Plugin AI calls' -->
              <span class="text-[12px] font-semibold text-text-primary block"
                >Plugin AI calls</span
              >
              <span class="text-[10px] text-text-muted block mt-0.5"
                >{auditSummary}</span
              >
            </div>
          </div>
          <span
            class="material-symbols-outlined text-[20px] text-text-muted transition-transform group-open:rotate-180"
            aria-hidden="true">expand_more</span
          >
        </summary>
        <div class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4">
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
              <span class="flex-1">Failed to load audit log: {auditError}</span>
            </div>
          {:else if audit.length === 0}
            <p class="text-text-muted text-[12px] font-body-md py-3">
              No activity recorded yet.
            </p>
          {:else}
            <div class="overflow-x-auto">
              <table class="w-full text-[11px] font-body-md border-collapse">
                <caption class="sr-only"> Recent plugin AI calls </caption>
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
                      <td class="py-1.5 pr-3 whitespace-nowrap" title={entry.at}
                        >{formatAuditTime(entry.at)}</td
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
                            class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-accent-primary-glow/20 border border-accent-primary-start text-accent-primary-start font-label-sm-bold text-[10px]"
                          >
                            <span
                              class="material-symbols-outlined text-[10px]"
                              aria-hidden="true">check_circle</span
                            >
                            ok
                          </span>
                        {:else}
                          <span
                            class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-status-danger/10 text-status-danger font-label-sm-bold text-[10px]"
                          >
                            <span
                              class="material-symbols-outlined text-[10px]"
                              aria-hidden="true">error</span
                            >
                            {entry.status}
                          </span>
                        {/if}
                      </td>
                      <td class="py-1.5 pr-3 text-text-muted whitespace-nowrap">
                        {entry.total_tokens != null ? entry.total_tokens : '—'}
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
                class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted font-label-sm-bold hover:text-error hover:border-error/50 transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
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

    {#if loadError}
      <!-- Soft error banner -->
      <div
        class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-[12px] font-body-md"
        role="alert"
      >
        <span class="material-symbols-outlined text-[18px]" aria-hidden="true"
          >error</span
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

  /* Remove default details chevron */
  details > summary::-webkit-details-marker {
    display: none;
  }
  details > summary {
    list-style: none;
  }
</style>
