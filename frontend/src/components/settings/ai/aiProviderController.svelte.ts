// Reactive controller for the AI Provider settings tab. Owns the provider
// config state, the per-role (chat/embedding) UI state maps, the sync fan-out
// logic, API-key management, the live connection probe, model discovery, and
// the lazy-loaded audit log. The component is a thin view over this surface;
// the IPC bindings are imported here so the view never touches IPC directly.
//
// Follows the repo's .svelte.ts state-module pattern (theme/editor/workingCopy,
// silt-tasks/state). The one $effect that needs component context (the audit
// lazy-load on <details> expand) stays in the component and calls loadAudit().
import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
import { getEmbeddingCapabilities } from '../../../settings/modelCapabilities'
import { updatePluginSetting } from '../../../settings/store.svelte'
import { getQAController } from '../../../plugins/first-party/silt-ai-qa/state.svelte'
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
} from '../../../../bindings/silt/app.js'
import type * as main from '../../../../bindings/silt/models.js'
import type * as aiTypes from '../../../../bindings/silt/backend/ai/models.js'

export type Which = 'chat' | 'embedding'
export type ProviderType =
  'local' | 'openai-compatible' | 'google' | 'anthropic'
type TestOutcome = { ok: boolean; message?: string }
type AuditState = 'idle' | 'loading' | 'loaded' | 'error'
type PersistResult = { ok: true } | { ok: false; message: string }

// Backend default endpoints. Provider-type switching snaps base_url to these
// unless the user typed a custom endpoint.
export const LOCAL_DEFAULT = 'http://localhost:11434'
export const OPENAI_DEFAULT = 'https://api.openai.com/v1'
export const GOOGLE_DEFAULT = 'https://generativelanguage.googleapis.com'
export const ANTHROPIC_DEFAULT = 'https://api.anthropic.com'

// Anthropic has no native embeddings endpoint.
export function supportsEmbeddings(type: string): boolean {
  return type !== 'anthropic'
}

// Providers that accept a reasoning_effort-style field on chat completions.
export function supportsReasoningEffort(type: string): boolean {
  return type === 'openai-compatible' || type === 'google' || type === 'local'
}

function presetLabel(
  value: number | string | undefined | null,
  options: { value: number | string; label: string }[],
  fallback: string
): string {
  if (value === undefined || value === null || value === '') return fallback
  const hit = options.find((o) => o.value === value)
  return hit?.label ?? 'Custom'
}

const TEMP_PRESETS = [
  { value: 0.2, label: 'Precise' },
  { value: 0.5, label: 'Natural' },
  { value: 0.9, label: 'Creative' }
]
const REASONING_PRESETS = [
  { value: 'none', label: 'Quick' },
  { value: 'medium', label: 'Standard' },
  { value: 'high', label: 'Deep' }
]
const TOKENS_PRESETS = [
  { value: 512, label: 'Concise' },
  { value: 2048, label: 'Standard' },
  { value: 4096, label: 'Detailed' }
]
const DIM_PRESETS = [
  { value: 0, label: 'Auto' },
  { value: 768, label: 'Compact' },
  { value: 1024, label: 'Balanced' }
]

export function providerDefaultURL(type: string): string {
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

export const PROVIDER_TYPES: {
  value: ProviderType
  icon: string
  label: string
}[] = [
  { value: 'local', icon: 'dns', label: 'Local (Ollama)' },
  { value: 'openai-compatible', icon: 'cloud', label: 'OpenAI-compatible' },
  { value: 'google', icon: 'auto_awesome', label: 'Google AI' },
  { value: 'anthropic', icon: 'psychology', label: 'Anthropic' }
]

// Plain-object round-trip so Svelte 5's deep proxy can track nested field
// mutations — Wails returns class instances, which $state does not recursively
// wrap (so bind:value mutations on instance fields would silently not
// re-render).
function toPlain<T>(o: T): T {
  return JSON.parse(JSON.stringify(o))
}

export function createAIProviderController() {
  let config = $state<main.AIPublicConfig | null>(null)
  let loadError = $state<string | null>(null)
  let loading = $state(true)
  let syncProviders = $state(true)

  // Split-mode tab state: which role card is actively shown. The inactive
  // card is hidden via CSS to keep the DOM queryable for Vitest.
  let activeRole = $state<Which>('chat')

  let apiKeyInputs = $state<Record<Which, string>>({ chat: '', embedding: '' })
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
  let testResult = $state<Record<Which, TestOutcome | null>>({
    chat: null,
    embedding: null
  })
  let keySavedFlash = $state<Record<Which, boolean>>({
    chat: false,
    embedding: false
  })

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

  let audit = $state<main.AIAuditEntry[]>([])
  let auditOpen = $state(false)
  let auditState = $state<AuditState>('idle')
  let auditError = $state<string | null>(null)

  // Last values successfully written for embedding model/dimensions. Callers
  // mutate config in place before persistProvider, so a "pre-persist" read of
  // config.embedding is already the new value — compare against this instead.
  let lastPersistedEmbedModel = ''
  let lastPersistedEmbedDims: number | undefined

  function rememberPersistedEmbedding(cfg: main.AIPublicConfig) {
    lastPersistedEmbedModel = cfg.embedding.model ?? ''
    lastPersistedEmbedDims = cfg.embedding.dimensions ?? undefined
  }

  async function reload(): Promise<void> {
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
        rememberPersistedEmbedding(config)
      }
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
      manualModel[which] = modelLists[which].length === 0
    } catch {
      // No cache yet — silent; user can click Refresh to poll.
    }
  }

  async function refreshModels(which: Which) {
    if (modelLoading[which]) return
    modelLoading[which] = true
    modelError[which] = null
    try {
      const models = toPlain(await ListModels(which, true))
      modelLists[which] = models ?? []
      manualModel[which] = modelLists[which].length === 0
    } catch (e) {
      modelError[which] = e instanceof Error ? e.message : String(e)
      manualModel[which] = true
    } finally {
      modelLoading[which] = false
    }
  }

  // --- Provider config persistence --------------------------------------

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

  async function markSearchIndexStale(reason: string) {
    // Update the live QA controller (if loaded) so the amber banner appears
    // immediately — the controller owns the reactive showStaleBanner state.
    const ctl = getQAController()
    if (ctl) {
      await ctl.setStaleReason(reason)
      return
    }
    // Plugin not loaded yet — persist directly so the banner shows on next load.
    try {
      await updatePluginSetting('silt-ai-qa', 'stale_reason', reason)
    } catch (e) {
      console.warn('Failed to mark search index stale:', e)
    }
  }

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
    // Fixed-size models reject a dimensions override; clear any leftover
    // Compact/Balanced value when the control is hidden for that model.
    if (which === 'embedding') {
      const caps = getEmbeddingCapabilities(b.model ?? '')
      if (caps.supportsTruncation === false && b.dimensions != null) {
        b.dimensions = undefined
      }
    }
    const prevModel = lastPersistedEmbedModel
    const prevDims = lastPersistedEmbedDims
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
      if (which === 'embedding') {
        const nextModel = config.embedding.model ?? ''
        const nextDims = config.embedding.dimensions
        if (prevModel && nextModel && prevModel !== nextModel) {
          await markSearchIndexStale(
            `Search model changed from ${prevModel} to ${nextModel}`
          )
        } else if (prevDims !== nextDims && (prevDims || nextDims)) {
          await markSearchIndexStale(
            `Index Density changed from ${prevDims ?? 'Auto'} to ${nextDims ?? 'Auto'}`
          )
        }
        rememberPersistedEmbedding(config)
      }
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

  async function persistModelOnBlur(w: Which): Promise<void> {
    const r = await persistProvider(w)
    if (!r.ok) modelError[w] = r.message ?? null
  }
  async function persistUrlOnBlur(which: Which): Promise<void> {
    const r = await persistProviderWithSync(which)
    if (!r.ok) testResult[which] = { ok: false, message: r.message }
  }

  function selectProviderType(which: Which, type: ProviderType) {
    if (!config) return

    const updateOne = async (w: Which, t: ProviderType) => {
      const b = config![w]
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
    // Flip sync on optimistically; rolled back below if the persist or key
    // copy fails.
    syncProviders = true
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
      syncProviders = false
      loadError = persisted.message
      return
    }

    // Share chat's existing key with embedding server-side via a backend copy
    // binding (the frontend only sees has_key, never the value). Skipped for
    // the local fallback (Ollama is keyless) and when chat has no key.
    if (chatSupportsEmbed && config.chat.has_key) {
      try {
        await CopyAIAPIKey('chat', 'embedding')
        config.embedding.has_key = true
      } catch (e) {
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

  function keyringFellBack(which: Which): boolean {
    if (!config) return false
    return (config.keyring_unusable_for ?? []).includes(which)
  }

  const needsSetup = $derived.by(() => {
    if (!config) return false
    return aiProviderNeedsSetup(config.chat)
  })

  return {
    // read state (reactive via $state proxies)
    get config() {
      return config
    },
    get loadError() {
      return loadError
    },
    set loadError(v: string | null) {
      loadError = v
    },
    get loading() {
      return loading
    },
    get syncProviders() {
      return syncProviders
    },
    get activeRole() {
      return activeRole
    },
    set activeRole(v: Which) {
      activeRole = v
    },
    get apiKeyInputs() {
      return apiKeyInputs
    },
    get showKey() {
      return showKey
    },
    get savingKey() {
      return savingKey
    },
    get clearingKey() {
      return clearingKey
    },
    get testing() {
      return testing
    },
    get testResult() {
      return testResult
    },
    get keySavedFlash() {
      return keySavedFlash
    },
    get modelLists() {
      return modelLists
    },
    get modelLoading() {
      return modelLoading
    },
    get modelError() {
      return modelError
    },
    get manualModel() {
      return manualModel
    },
    get audit() {
      return audit
    },
    get auditOpen() {
      return auditOpen
    },
    set auditOpen(v: boolean) {
      auditOpen = v
    },
    get auditState() {
      return auditState
    },
    get auditError() {
      return auditError
    },
    // derived summaries
    get needsSetup() {
      return needsSetup
    },
    get tuningSummary(): string {
      if (!config) return ''
      const chat = config.chat
      const style = presetLabel(
        chat.temperature ?? 0.5,
        TEMP_PRESETS,
        'Default'
      )
      const depth = presetLabel(
        chat.reasoning_effort ?? 'medium',
        REASONING_PRESETS,
        'Default'
      )
      const length = presetLabel(
        chat.max_tokens ?? 2048,
        TOKENS_PRESETS,
        'Default'
      )
      const density = presetLabel(
        config.embedding.dimensions ?? 0,
        DIM_PRESETS,
        'Auto'
      )
      if (syncProviders) {
        return `${style}, ${depth}, ${length} · Index: ${density}`
      }
      if (activeRole === 'chat') {
        return `${style}, ${depth}, ${length}`
      }
      return `Index Density: ${density}`
    },
    get keyringSummary(): string {
      if (!config) return ''
      if (!config.keyring_available)
        return 'OS Keyring unavailable (fallback active)'
      return config.use_keyring
        ? 'Secure OS Keychain storage enabled'
        : 'Stored in vault configuration'
    },
    get auditSummary(): string {
      if (auditState === 'idle') return 'Click to view log'
      if (auditState === 'loading') return 'Loading logs…'
      if (auditState === 'error') return 'Error loading logs'
      return `${audit.length} call${audit.length === 1 ? '' : 's'} recorded`
    },
    // actions
    reload,
    loadModels,
    refreshModels,
    advancedFieldError,
    hasAdvancedErrors,
    persistProvider,
    persistProviderWithSync,
    persistModelOnBlur,
    persistUrlOnBlur,
    selectProviderType,
    saveKey,
    clearKey,
    runTest,
    runTestUnified,
    toggleKeyring,
    toggleSyncProviders,
    loadAudit,
    clearAudit,
    formatAuditTime,
    keyringFellBack
  }
}

export type AIProviderController = ReturnType<typeof createAIProviderController>
