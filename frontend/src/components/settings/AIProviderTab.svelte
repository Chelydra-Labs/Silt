<script lang="ts">
  // Settings → AI Provider tab.
  //
  // Thin view over the reactive controller in
  // ./ai/aiProviderController.svelte.ts, which owns config state, the
  // chat/embedding sync fan-out, API-key management, the live connection
  // probe, model discovery, and the audit log. The IPC bindings live in the
  // controller so this view never touches IPC directly. See the controller
  // for behavior; this file is layout + a11y only.
  import { onMount, tick } from 'svelte'
  import {
    GetCloseToTray,
    GetLocalMCPConfig,
    GetLocalMCPInstallHint,
    GetLocalMCPStatus,
    GetLocalMCPToken,
    SetCloseToTray,
    SetLocalMCPConfig
  } from '../../../bindings/silt/app.js'
  import {
    createAIProviderController,
    LOCAL_DEFAULT,
    PROVIDER_TYPES,
    supportsEmbeddings,
    supportsReasoningEffort,
    type ProviderType,
    type Which
  } from './ai/aiProviderController.svelte'
  import PresetControl from './PresetControl.svelte'
  import InfoTooltip from './InfoTooltip.svelte'
  import { getEmbeddingCapabilities } from '../../settings/modelCapabilities'
  import { getQAController } from '../../plugins/first-party/silt-ai-qa/state.svelte'
  import { makePluginContext } from '../../plugins/context'
  import { getSessionToken } from '../../plugins/loader'
  import QASettings from '../../plugins/first-party/silt-ai-qa/QASettings.svelte'
  import AssistantSettings from '../../plugins/first-party/silt-ai-assistant/AssistantSettings.svelte'
  import AISummarySettings from '../../plugins/first-party/silt-ai-summary/AISummarySettings.svelte'

  interface Props {
    /** Settings search jump target — selects the matching segment. */
    ringAnchor?: string | null
  }
  let { ringAnchor = null }: Props = $props()

  const ai = createAIProviderController() as ReturnType<
    typeof createAIProviderController
  > & {
    featuresSaving: boolean
    featuresError: string | null
    ragNeedsEmbeddingSetup: boolean
    updateFeatures: (patch: {
      enabled?: boolean
      rag_enabled?: boolean
      summaries_enabled?: boolean
    }) => Promise<void>
  }

  // Local MCP host (#687) — vault-scoped; independent of chat provider setup.
  let mcpEnabled = $state(false)
  let mcpWrite = $state(false)
  let mcpHttp = $state(true)
  let mcpPort = $state(17887)
  let mcpStatus = $state<{
    state?: string
    message?: string
    endpoint?: string
    write_enabled?: boolean
  } | null>(null)
  let mcpSaving = $state(false)
  let mcpError = $state('')
  let mcpHint = $state('')
  let mcpTokenVisible = $state(false)
  let mcpToken = $state('')
  let mcpTrayPrompt = $state(false)

  async function refreshMCP() {
    try {
      const [cfg, st, hint] = await Promise.all([
        GetLocalMCPConfig(),
        GetLocalMCPStatus(),
        GetLocalMCPInstallHint()
      ])
      mcpEnabled = !!(cfg as { enabled?: boolean })?.enabled
      mcpWrite = !!(cfg as { write_enabled?: boolean })?.write_enabled
      mcpHttp = (cfg as { http_enabled?: boolean })?.http_enabled !== false
      const p = (cfg as { http_port?: number })?.http_port
      if (typeof p === 'number' && p > 0) mcpPort = p
      mcpStatus = st as typeof mcpStatus
      mcpHint = typeof hint === 'string' ? hint : ''
    } catch (e) {
      console.error('Local MCP status failed', e)
    }
  }

  async function saveMCP(next: {
    enabled?: boolean
    write?: boolean
    http?: boolean
    port?: number
  }) {
    if (mcpSaving) return
    mcpSaving = true
    mcpError = ''
    const enabled = next.enabled ?? mcpEnabled
    const write = next.write ?? mcpWrite
    const http = next.http ?? mcpHttp
    const port = next.port ?? mcpPort
    try {
      if (enabled && !mcpEnabled) {
        // Prompt close-to-tray so MCP survives window close (user may decline).
        try {
          const tray = await GetCloseToTray()
          if (!tray) mcpTrayPrompt = true
        } catch {
          /* ignore */
        }
      }
      await SetLocalMCPConfig(enabled, http, write, port)
      mcpEnabled = enabled
      mcpWrite = write
      mcpHttp = http
      mcpPort = port
      await refreshMCP()
    } catch (e) {
      mcpError = 'Could not save local MCP settings.'
      console.error(e)
      await refreshMCP()
    } finally {
      mcpSaving = false
    }
  }

  async function acceptTrayForMCP() {
    try {
      await SetCloseToTray(true)
    } catch (e) {
      console.error(e)
    }
    mcpTrayPrompt = false
  }

  async function revealMCPToken() {
    try {
      mcpToken = (await GetLocalMCPToken()) || ''
      mcpTokenVisible = true
    } catch (e) {
      console.error(e)
    }
  }

  async function copyMCPToken() {
    try {
      if (!mcpToken) mcpToken = (await GetLocalMCPToken()) || ''
      if (!mcpToken) return
      await navigator.clipboard.writeText(mcpToken)
      mcpTokenVisible = true
    } catch (e) {
      console.error(e)
    }
  }

  async function copyMCPHint() {
    try {
      await navigator.clipboard.writeText(
        mcpHint || (await GetLocalMCPInstallHint())
      )
    } catch (e) {
      console.error(e)
    }
  }

  function clientInstallNotes(): { title: string; body: string }[] {
    const ep = mcpStatus?.endpoint || `http://127.0.0.1:${mcpPort}`
    const bin = 'silt' // path to Silt binary / `silt mcp`
    return [
      {
        title: 'OpenCode',
        body:
          mcpHint ||
          JSON.stringify(
            {
              mcp: {
                silt: {
                  type: 'local',
                  command: [bin, 'mcp'],
                  enabled: true
                }
              }
            },
            null,
            2
          )
      },
      {
        title: 'Claude Desktop',
        body: `1. Enable Local MCP here and copy the auth token.\n2. Install the MCPB from integrations/claude-desktop (or add a stdio server):\n   command: ${bin}\n   args: ["mcp"]\n3. Copy integrations/silt-agent/SKILL.md into your Claude skills folder.\n4. Endpoint (HTTP clients): ${ep}\nSee docs/LOCAL_MCP.md.`
      },
      {
        title: 'ChatGPT Desktop / Codex',
        body: `Configure a local MCP server with command "${bin} mcp" (stdio). Paste the bearer token only if the client uses HTTP to ${ep}. Install the Silt skill from integrations/silt-agent/SKILL.md. See docs/LOCAL_MCP.md.`
      }
    ]
  }

  // Degraded semantic index / hybrid search signals (#630).
  const qaStaleReason = $derived.by(() => {
    try {
      return getQAController()?.settings?.stale_reason ?? null
    } catch {
      return null
    }
  })
  const qaSearchDegrade = $derived.by(() => {
    try {
      return getQAController()?.searchDegradeReason ?? null
    } catch {
      return null
    }
  })

  // Features live on AIPublicConfig after Phase 2; cast for partial type caches.
  const features = $derived(
    (
      ai.config as {
        features?: {
          enabled?: boolean
          rag_enabled?: boolean
          summaries_enabled?: boolean
        }
      } | null
    )?.features ?? {
      enabled: false,
      rag_enabled: false,
      summaries_enabled: false
    }
  )

  // Fine-tuning modules embed on this page when their feature flag is on.
  // Contexts use the live session token when the plugin is loaded.
  const writingCtx = $derived.by(() => {
    if (!features.enabled) return null
    return makePluginContext(
      'silt-ai-assistant',
      getSessionToken('silt-ai-assistant')
    )
  })
  const searchCtx = $derived.by(() => {
    if (!features.enabled || !features.rag_enabled) return null
    return makePluginContext('silt-ai-qa', getSessionToken('silt-ai-qa'))
  })
  const summaryCtx = $derived.by(() => {
    if (!features.enabled || !features.summaries_enabled) return null
    return makePluginContext(
      'silt-ai-summary',
      getSessionToken('silt-ai-summary')
    )
  })

  type AiSegmentId = 'ai-setup' | 'ai-capabilities' | 'ai-advanced'

  const showCapabilities = $derived(
    Boolean(features.enabled && (writingCtx || searchCtx || summaryCtx))
  )

  const segments = $derived.by(() => {
    const list: { id: AiSegmentId; label: string }[] = [
      { id: 'ai-setup', label: 'Setup' }
    ]
    if (showCapabilities) {
      list.push({ id: 'ai-capabilities', label: 'Capabilities' })
    }
    list.push({ id: 'ai-advanced', label: 'Advanced' })
    return list
  })

  let activeSegment = $state<AiSegmentId>('ai-setup')
  // Roving-tabindex refs for the Setup / Capabilities / Advanced segment bar.
  let segmentTabRefs: HTMLButtonElement[] = $state([])

  function segmentForAnchor(
    anchor: string | null | undefined
  ): AiSegmentId | null {
    if (!anchor) return null
    if (
      anchor === 'ai-setup' ||
      anchor === 'ai-features' ||
      anchor === 'ai-embedding-section' ||
      anchor === 'ai-local-mcp'
    ) {
      return 'ai-setup'
    }
    if (
      anchor === 'ai-capabilities' ||
      anchor === 'ai-writing-tuning' ||
      anchor === 'ai-search-tuning' ||
      anchor === 'ai-summary-tuning'
    ) {
      return 'ai-capabilities'
    }
    if (anchor === 'ai-advanced') return 'ai-advanced'
    return null
  }

  async function selectSegment(id: AiSegmentId) {
    if (id === 'ai-capabilities' && !showCapabilities) {
      activeSegment = 'ai-setup'
      return
    }
    activeSegment = id
    await tick()
    const idx = segments.findIndex((s) => s.id === id)
    segmentTabRefs[idx]?.focus()
  }

  // WAI-ARIA tabs: Arrow/Home/End move selection within the segment tablist
  // (inactive tabs use tabindex=-1 so Tab leaves the group).
  function handleSegmentKeydown(e: KeyboardEvent) {
    const list = segments
    if (list.length === 0) return
    const idx = list.findIndex((s) => s.id === activeSegment)
    const cur = idx >= 0 ? idx : 0
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      void selectSegment(list[(cur + 1) % list.length].id)
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      void selectSegment(list[(cur - 1 + list.length) % list.length].id)
    } else if (e.key === 'Home') {
      e.preventDefault()
      void selectSegment(list[0].id)
    } else if (e.key === 'End') {
      e.preventDefault()
      void selectSegment(list[list.length - 1].id)
    }
  }

  /** Jump to the visible embedding model control (sync card or split section). */
  async function focusEmbeddingSetup() {
    await selectSegment('ai-setup')
    if (ai.syncProviders) {
      document
        .getElementById('ai-embedding-model')
        ?.scrollIntoView({ behavior: 'smooth', block: 'center' })
      document.getElementById('ai-embedding-model')?.focus?.()
    } else {
      ai.activeRole = 'embedding'
      await tick()
      document
        .getElementById('ai-embedding-section')
        ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
    }
  }

  // Drop Capabilities selection when the feature is turned off.
  $effect(() => {
    if (!showCapabilities && activeSegment === 'ai-capabilities') {
      activeSegment = 'ai-setup'
    }
  })

  // Settings search / deep-link: switch segment to match the anchor.
  $effect(() => {
    const seg = segmentForAnchor(ringAnchor)
    if (seg) selectSegment(seg)
  })

  function ringClass(id: string): string {
    return ringAnchor === id
      ? 'ring-2 ring-accent-primary-start/50 ring-offset-2 ring-offset-surface-app'
      : ''
  }

  onMount(() => {
    void ai.reload()
    void refreshMCP()
  })

  // Audit lazy-load: the controller is a plain module (no component context),
  // so the <details> open → loadAudit effect lives here. The state machine in
  // the controller prevents a refire loop after a failed probe.
  $effect(() => {
    if (
      activeSegment === 'ai-advanced' &&
      ai.auditOpen &&
      ai.auditState === 'idle'
    ) {
      void ai.loadAudit()
    }
  })
</script>

<div class="max-w-4xl mx-auto w-full h-full min-h-0 flex flex-col">
  {#if ai.loading}
    <div
      class="p-6 text-text-muted text-type-sm font-body-md animate-pulse py-8 text-center"
    >
      Loading AI configuration…
    </div>
  {:else if ai.loadError && !ai.config}
    <div
      class="m-6 flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-type-sm font-body-md"
      role="alert"
    >
      <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
        >error</span
      >
      <span class="flex-1">Failed to load AI configuration: {ai.loadError}</span
      >
      <button
        type="button"
        onclick={() => void ai.reload()}
        class="text-type-xs font-label-sm-bold underline bg-transparent border-none cursor-pointer text-error"
      >
        Retry
      </button>
    </div>
  {:else if ai.config}
    <!-- Fixed segment bar (parent panel does not scroll this chrome). -->
    <nav
      class="flex-shrink-0 px-6 pt-4 pb-3 bg-surface-app border-b border-surface-panel-border"
      aria-label="AI settings sections"
    >
      <div
        class="flex flex-wrap gap-1 p-1 rounded-xl bg-surface-panel/40 border border-surface-panel-border/80 max-w-md"
        role="tablist"
        aria-orientation="horizontal"
        tabindex="-1"
        onkeydown={handleSegmentKeydown}
      >
        {#each segments as seg, i (seg.id)}
          <button
            type="button"
            role="tab"
            id="ai-seg-tab-{seg.id}"
            bind:this={segmentTabRefs[i]}
            aria-selected={activeSegment === seg.id}
            aria-controls="{seg.id}-panel"
            tabindex={activeSegment === seg.id ? 0 : -1}
            class="flex-1 px-3 py-1.5 rounded-lg text-type-xs font-label-sm-bold transition-all border-none cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 {activeSegment ===
            seg.id
              ? 'bg-surface-app text-accent-primary-start shadow-sm'
              : 'bg-transparent text-text-muted hover:text-text-primary'}"
            onclick={() => void selectSegment(seg.id)}
          >
            {seg.label}
          </button>
        {/each}
      </div>
    </nav>

    <div
      class="flex-1 min-h-0 overflow-y-auto custom-scrollbar px-6 pb-6 pt-6 space-y-6"
    >
      {#if activeSegment === 'ai-setup'}
        <div
          id="ai-setup-panel"
          role="tabpanel"
          aria-labelledby="ai-seg-tab-ai-setup"
          class="space-y-6"
        >
          <!-- Features first: enablement before plumbing (#632). -->
          <section
            aria-label="AI features"
            class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-4 space-y-4"
            aria-busy={ai.featuresSaving}
          >
            <div class="flex flex-wrap items-start justify-between gap-2">
              <div>
                <h3 class="text-text-primary text-type-md font-semibold m-0">
                  Features
                </h3>
                <p
                  class="text-text-muted text-type-xs font-label-sm m-0 mt-0.5"
                >
                  Turn AI on once, then choose optional capabilities.
                </p>
              </div>
              {#if ai.featuresSaving}
                <span
                  class="inline-flex items-center gap-1.5 text-text-muted text-type-xs font-label-sm"
                  role="status"
                  aria-live="polite"
                >
                  <span
                    class="material-symbols-outlined text-icon-sm animate-spin"
                    aria-hidden="true">progress_activity</span
                  >
                  Saving…
                </span>
              {/if}
            </div>

            <div
              class="flex flex-col sm:flex-row sm:items-start justify-between gap-4"
            >
              <div class="space-y-0.5 min-w-0">
                <span
                  id="ai-enable-label"
                  class="text-text-primary text-type-md font-semibold block"
                >
                  Enable AI
                </span>
                <span class="text-text-muted text-type-xs font-label-sm block">
                  Chat with your vault, writing help, and tools. Uses your chat
                  model.
                </span>
              </div>
              <label
                class="flex items-center cursor-pointer select-none"
                for="ai-enable"
              >
                <input
                  id="ai-enable"
                  type="checkbox"
                  class="keyring-switch peer sr-only"
                  aria-labelledby="ai-enable-label"
                  checked={features.enabled === true}
                  disabled={ai.featuresSaving}
                  onchange={(e) =>
                    void ai.updateFeatures({
                      enabled: e.currentTarget.checked
                    })}
                />
                <span
                  aria-hidden="true"
                  class="keyring-switch-track"
                  class:on={features.enabled === true}
                ></span>
              </label>
            </div>

            <div
              class="ml-3 pl-3 border-l border-surface-panel-border space-y-3"
              class:opacity-50={features.enabled !== true}
            >
              <div
                class="flex flex-col sm:flex-row sm:items-start justify-between gap-4"
              >
                <div class="space-y-0.5 min-w-0">
                  <span
                    id="ai-rag-label"
                    class="text-text-primary text-type-sm font-semibold block"
                  >
                    Semantic search
                  </span>
                  <span
                    class="text-text-muted text-type-xs font-label-sm block"
                  >
                    Find notes by meaning. Needs an embedding model.
                  </span>
                </div>
                <label
                  class="flex items-center select-none"
                  class:cursor-pointer={features.enabled === true}
                  class:cursor-not-allowed={features.enabled !== true}
                  for="ai-rag"
                  title={features.enabled !== true
                    ? 'Enable AI first'
                    : undefined}
                >
                  <input
                    id="ai-rag"
                    type="checkbox"
                    class="keyring-switch peer sr-only"
                    aria-labelledby="ai-rag-label"
                    checked={features.rag_enabled === true}
                    disabled={ai.featuresSaving || features.enabled !== true}
                    onchange={(e) =>
                      void ai.updateFeatures({
                        rag_enabled: e.currentTarget.checked
                      })}
                  />
                  <span
                    aria-hidden="true"
                    class="keyring-switch-track"
                    class:on={features.rag_enabled === true}
                    class:disabled={features.enabled !== true}
                  ></span>
                </label>
              </div>

              {#if ai.ragNeedsEmbeddingSetup}
                <div
                  class="bg-accent-primary-glow/20 border border-accent-primary-start/30 rounded-lg p-3 flex items-start gap-2"
                  role="status"
                >
                  <span
                    class="material-symbols-outlined text-accent-primary-start text-icon-md flex-shrink-0"
                    aria-hidden="true">link</span
                  >
                  <div class="text-type-xs font-body-md text-text-primary">
                    Semantic search needs an embedding model.
                    <button
                      type="button"
                      class="text-accent-primary-start underline font-label-sm-bold ml-1 bg-transparent border-none cursor-pointer p-0"
                      onclick={() => void focusEmbeddingSetup()}
                    >
                      Set up embedding
                    </button>
                  </div>
                </div>
              {:else if features.rag_enabled === true && qaStaleReason}
                <div
                  class="bg-accent-primary-glow/20 border border-accent-primary-start/30 rounded-lg p-3 flex items-start gap-2"
                  role="alert"
                >
                  <span
                    class="material-symbols-outlined text-accent-primary-start text-icon-md flex-shrink-0"
                    aria-hidden="true">warning</span
                  >
                  <div class="text-type-xs font-body-md text-text-primary">
                    Semantic search index may be out of date: {qaStaleReason}.
                    Rebuild from Search settings for accurate results.
                  </div>
                </div>
              {:else if features.rag_enabled === true && qaSearchDegrade}
                <div
                  class="bg-accent-primary-glow/20 border border-accent-primary-start/30 rounded-lg p-3 flex items-start gap-2"
                  role="alert"
                >
                  <span
                    class="material-symbols-outlined text-accent-primary-start text-icon-md flex-shrink-0"
                    aria-hidden="true">warning</span
                  >
                  <div class="text-type-xs font-body-md text-text-primary">
                    Search is degraded: {qaSearchDegrade}
                  </div>
                </div>
              {/if}

              <div
                class="flex flex-col sm:flex-row sm:items-start justify-between gap-4"
              >
                <div class="space-y-0.5 min-w-0">
                  <span
                    id="ai-summaries-label"
                    class="text-text-primary text-type-sm font-semibold block"
                  >
                    Note summaries
                  </span>
                  <span
                    class="text-text-muted text-type-xs font-label-sm block"
                  >
                    Show a short summary banner on notes.
                  </span>
                </div>
                <label
                  class="flex items-center select-none"
                  class:cursor-pointer={features.enabled === true}
                  class:cursor-not-allowed={features.enabled !== true}
                  for="ai-summaries"
                  title={features.enabled !== true
                    ? 'Enable AI first'
                    : undefined}
                >
                  <input
                    id="ai-summaries"
                    type="checkbox"
                    class="keyring-switch peer sr-only"
                    aria-labelledby="ai-summaries-label"
                    checked={features.summaries_enabled === true}
                    disabled={ai.featuresSaving || features.enabled !== true}
                    onchange={(e) =>
                      void ai.updateFeatures({
                        summaries_enabled: e.currentTarget.checked
                      })}
                  />
                  <span
                    aria-hidden="true"
                    class="keyring-switch-track"
                    class:on={features.summaries_enabled === true}
                    class:disabled={features.enabled !== true}
                  ></span>
                </label>
              </div>
            </div>

            {#if ai.featuresError}
              <p class="text-error text-type-xs m-0" role="alert">
                {ai.featuresError}
              </p>
            {/if}
          </section>

          <!-- Local MCP (#687) -->
          <section
            id="ai-local-mcp"
            aria-label="Local MCP"
            class="bg-surface-panel/20 border border-surface-panel-border rounded-xl p-4 space-y-4 {ringClass(
              'ai-local-mcp'
            )}"
            aria-busy={mcpSaving}
          >
            <div>
              <h3 class="text-text-primary text-type-md font-semibold m-0">
                Local MCP
              </h3>
              <p class="text-text-muted text-type-xs font-label-sm m-0 mt-0.5">
                Let desktop agents (Claude Desktop, OpenCode, Codex) read and —
                with grant — edit this vault over loopback. Off by default.
              </p>
            </div>

            <div
              class="flex flex-col sm:flex-row sm:items-start justify-between gap-4"
            >
              <div class="space-y-0.5 min-w-0">
                <span
                  id="mcp-enable-label"
                  class="text-text-primary text-type-sm font-semibold block"
                >
                  Enable local AI integration
                </span>
                <span class="text-text-muted text-type-xs font-label-sm block">
                  Starts when a vault is open. Close-to-tray keeps MCP running;
                  Quit stops it.
                </span>
              </div>
              <label
                class="flex items-center cursor-pointer select-none"
                for="mcp-enable"
              >
                <input
                  id="mcp-enable"
                  type="checkbox"
                  class="keyring-switch peer sr-only"
                  aria-labelledby="mcp-enable-label"
                  checked={mcpEnabled}
                  disabled={mcpSaving}
                  onchange={(e) =>
                    void saveMCP({ enabled: e.currentTarget.checked })}
                />
                <span
                  aria-hidden="true"
                  class="keyring-switch-track"
                  class:on={mcpEnabled}
                ></span>
              </label>
            </div>

            {#if mcpTrayPrompt}
              <div
                class="rounded-lg border border-accent-primary-start/30 bg-accent-primary-glow/15 p-3 space-y-2"
                role="status"
              >
                <p class="text-text-primary text-type-xs m-0">
                  Enable <strong>Close to tray</strong> so agents can keep using MCP
                  after you close the window? You can decline and quit will still
                  stop MCP.
                </p>
                <div class="flex flex-wrap gap-2">
                  <button
                    type="button"
                    class="px-3 py-1 rounded-md bg-accent-primary-start text-surface-app text-type-xs border-none cursor-pointer"
                    onclick={() => void acceptTrayForMCP()}
                  >
                    Enable close to tray
                  </button>
                  <button
                    type="button"
                    class="px-3 py-1 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                    onclick={() => (mcpTrayPrompt = false)}
                  >
                    Not now
                  </button>
                </div>
              </div>
            {/if}

            <div
              class="ml-3 pl-3 border-l border-surface-panel-border space-y-3"
              class:opacity-50={!mcpEnabled}
            >
              <div
                class="flex flex-col sm:flex-row sm:items-start justify-between gap-4"
              >
                <div class="space-y-0.5 min-w-0">
                  <span
                    id="mcp-write-label"
                    class="text-text-primary text-type-sm font-semibold block"
                  >
                    Allow write tools
                  </span>
                  <span
                    class="text-text-muted text-type-xs font-label-sm block"
                  >
                    create_page and update_blocks. Read tools stay available
                    without this.
                  </span>
                </div>
                <label
                  class="flex items-center select-none"
                  class:cursor-pointer={mcpEnabled}
                  class:cursor-not-allowed={!mcpEnabled}
                  for="mcp-write"
                >
                  <input
                    id="mcp-write"
                    type="checkbox"
                    class="keyring-switch peer sr-only"
                    aria-labelledby="mcp-write-label"
                    checked={mcpWrite}
                    disabled={mcpSaving || !mcpEnabled}
                    onchange={(e) =>
                      void saveMCP({ write: e.currentTarget.checked })}
                  />
                  <span
                    aria-hidden="true"
                    class="keyring-switch-track"
                    class:on={mcpWrite}
                    class:disabled={!mcpEnabled}
                  ></span>
                </label>
              </div>

              <p class="text-text-muted text-type-xs m-0" id="mcp-availability">
                MCP availability:
                <strong class="text-text-primary">
                  {mcpStatus?.state ?? 'unknown'}
                </strong>
                {#if mcpStatus?.message}
                  — {mcpStatus.message}
                {/if}
                {#if mcpStatus?.endpoint}
                  <br />
                  Endpoint:
                  <code class="text-text-primary">{mcpStatus.endpoint}</code>
                {/if}
              </p>

              <div class="flex flex-wrap gap-2">
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer disabled:opacity-50"
                  disabled={mcpSaving}
                  onclick={() => void refreshMCP()}
                >
                  Refresh status
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                  onclick={() => void revealMCPToken()}
                >
                  {mcpTokenVisible ? 'Token shown' : 'Show auth token'}
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                  onclick={() => void copyMCPToken()}
                >
                  Copy token
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                  onclick={() => void copyMCPHint()}
                >
                  Copy OpenCode snippet
                </button>
              </div>
              {#if mcpTokenVisible && mcpToken}
                <p class="text-text-muted text-type-xs m-0 break-all">
                  Bearer token (OS keyring):
                  <code class="text-text-primary select-all">{mcpToken}</code>
                </p>
              {/if}

              <div class="space-y-2" aria-label="Client install notes">
                <p class="text-text-primary text-type-xs font-semibold m-0">
                  Client setup
                </p>
                {#each clientInstallNotes() as note (note.title)}
                  <details
                    class="rounded-md border border-surface-panel-border bg-surface-panel/30 p-2"
                  >
                    <summary
                      class="text-text-primary text-type-xs font-semibold cursor-pointer"
                    >
                      {note.title}
                    </summary>
                    <pre
                      class="text-text-muted text-type-2xs m-0 mt-2 whitespace-pre-wrap break-words font-mono">{note.body}</pre>
                    <button
                      type="button"
                      class="mt-2 px-2 py-1 rounded-md bg-surface-panel text-text-primary text-type-2xs border border-surface-panel-border cursor-pointer"
                      onclick={() =>
                        void navigator.clipboard.writeText(note.body)}
                    >
                      Copy {note.title} notes
                    </button>
                  </details>
                {/each}
              </div>
            </div>

            {#if mcpError}
              <p class="text-error text-type-xs m-0" role="alert">{mcpError}</p>
            {/if}
          </section>

          <!-- Intro & nudge banner -->
          <section aria-label="AI provider overview">
            <p
              class="text-text-primary text-type-md font-body-md leading-relaxed"
            >
              Connect Silt to an AI model to power chat, writing help, semantic
              search, and note summaries. Choose a setup mode below to get
              started.
            </p>
            {#if ai.needsSetup}
              <div
                class="mt-4 bg-accent-primary-glow/20 border border-accent-primary-start/30 rounded-xl p-4 flex items-start gap-3"
              >
                <span
                  class="material-symbols-outlined text-accent-primary-start text-icon-lg mt-0.5 flex-shrink-0"
                  aria-hidden="true">lightbulb</span
                >
                <div
                  class="flex-1 text-type-sm font-body-md text-text-primary leading-relaxed"
                >
                  <strong class="text-accent-primary-start"
                    >Set up an AI provider.</strong
                  >
                  Leave on <em>Local</em> if you are running Ollama on
                  localhost, or switch to a cloud provider like
                  <em>Google AI</em>
                  or
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
                  class="text-text-primary text-type-md font-semibold block"
                >
                  Sync chat and embedding providers
                </span>
                <span class="text-text-muted text-type-xs font-label-sm block">
                  Recommended. Share the same credentials, provider type, and
                  base URL for both roles.
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
                  checked={ai.syncProviders}
                  onchange={(e) =>
                    void ai.toggleSyncProviders(e.currentTarget.checked)}
                />
                <span
                  aria-hidden="true"
                  class="keyring-switch-track"
                  class:on={ai.syncProviders}
                ></span>
              </label>
            </div>

            <!-- Split Role switcher (only visible in split mode) -->
            {#if !ai.syncProviders}
              <div
                class="flex p-1 rounded-xl bg-surface-panel/40 border border-surface-panel-border/80 max-w-xs"
                role="tablist"
                aria-label="AI Role Switcher"
              >
                <button
                  type="button"
                  role="tab"
                  aria-selected={ai.activeRole === 'chat'}
                  onclick={() => (ai.activeRole = 'chat')}
                  class="flex-1 py-1.5 px-3 rounded-lg text-type-xs font-label-sm-bold transition-all cursor-pointer {ai.activeRole ===
                  'chat'
                    ? 'bg-accent-primary-start text-text-on-accent shadow-md'
                    : 'text-text-muted hover:text-text-primary'}"
                >
                  Chat Model
                </button>
                <button
                  type="button"
                  role="tab"
                  aria-selected={ai.activeRole === 'embedding'}
                  onclick={() => (ai.activeRole = 'embedding')}
                  class="flex-1 py-1.5 px-3 rounded-lg text-type-xs font-label-sm-bold transition-all cursor-pointer {ai.activeRole ===
                  'embedding'
                    ? 'bg-accent-primary-start text-text-on-accent shadow-md'
                    : 'text-text-muted hover:text-text-primary'}"
                >
                  Embedding Model
                </button>
              </div>
            {/if}
          </section>
        </div>
      {/if}
      <!-- /Setup view (features + intro + sync). Snippets stay at config root. -->

      {#snippet providerCard(which: Which)}
        {@const b = ai.config![which]}
        {@const idPrefix = `ai-${which}`}
        {@const typeLabel =
          which === 'chat' ? 'Chat Provider Type' : 'Embedding Provider Type'}
        {@const isLocal = b.provider_type === 'local'}
        {@const embedUnsupported =
          which === 'embedding' && !supportsEmbeddings(b.provider_type)}
        {@const testingNow = ai.testing[which]}
        {@const result = ai.testResult[which]}
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
                  <span class="font-label-sm-bold text-type-xs">{pt.label}</span
                  >
                </button>
              {/each}
            </div>

            <!-- Privacy notice -->
            <p
              class="text-type-xs font-label-sm mt-3 flex items-center gap-1.5 {isLocal
                ? 'text-text-muted'
                : 'text-text-primary'}"
            >
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true"
              >
                {isLocal ? 'shield' : 'arrow_outward'}
              </span>
              {#if isLocal}
                Runs on your machine — content sent to this provider doesn't
                leave this device.
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
                    disabled={!ai.apiKeyInputs[which].trim() ||
                      ai.savingKey[which]}
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
                      >Anthropic does not offer embeddings. Switch to split
                      settings to configure a separate embedding provider.</span
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
                          >Connection failed (Embedding){ai.testResult.embedding
                            .message
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
                    <span
                      >Connected{result.message
                        ? ` · ${result.message}`
                        : ''}</span
                    >
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
                <span
                  class="material-symbols-outlined text-icon-md"
                  aria-hidden="true">bolt</span
                >
                Test connection
              {/if}
            </button>
          </div>
        </div>
      {/snippet}

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
                    <span class="material-symbols-outlined text-icon-md"
                      >list</span
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
                      ai.manualModel[w] = true
                    } else {
                      b.model = val
                      void ai.persistProvider(w)
                    }
                  }}
                  class="w-full bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all cursor-pointer appearance-none pr-8"
                >
                  {#if !ai.modelLists[w].some((m) => m.id === b.model)}
                    <option value={b.model}
                      >{b.model || 'Select a model…'}</option
                    >
                  {/if}
                  {#each ai.modelLists[w] as m (m.id)}
                    <option value={m.id}>{m.display_name}</option>
                  {/each}
                  <option value="__custom__"
                    >+ Type model name manually...</option
                  >
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
              <span
                class="material-symbols-outlined text-type-sm"
                aria-hidden="true">error</span
              >
              {ai.modelError[w]}
            </p>
          {/if}
        </div>
      {/snippet}

      {#snippet advancedTuningGrid(w: Which)}
        {@const b = ai.config![w]}
        {@const idPrefix = `ai-${w}`}
        {@const embedCaps =
          w === 'embedding' ? getEmbeddingCapabilities(b.model ?? '') : null}
        <div class="flex flex-col gap-5">
          {#if w === 'chat'}
            <PresetControl
              label="Answer Style"
              tooltipText="How predictable or creative should the AI's answers be? Lower means more consistent and factual. Higher means more varied and exploratory."
              tooltipTechnical="Technical: Temperature (0.0-2.0)."
              options={[
                {
                  value: 0.2,
                  label: 'Precise',
                  description:
                    'Consistent, factual answers. Best for research and facts.'
                },
                {
                  value: 0.5,
                  label: 'Natural',
                  description:
                    'Conversational, natural responses. Good for most questions.'
                },
                {
                  value: 0.9,
                  label: 'Creative',
                  description:
                    'Varied, exploratory answers. Best for brainstorming.'
                }
              ]}
              value={b.temperature ?? 0.5}
              customLabel="Temperature"
              customMin={0}
              customMax={2}
              customStep={0.1}
              onchange={(v) => {
                b.temperature = v
                void ai.persistProvider(w)
              }}
            />

            {#if supportsReasoningEffort(b.provider_type)}
              <PresetControl
                label="Thinking Depth"
                tooltipText="How much the AI works through a problem before answering. Deeper thinking produces more thorough answers but takes longer."
                tooltipTechnical="Technical: Reasoning effort (none-max). Not all models support this."
                options={[
                  {
                    value: 'none',
                    label: 'Quick',
                    description: 'Fast responses with light reasoning.'
                  },
                  {
                    value: 'medium',
                    label: 'Standard',
                    description: 'Balanced reasoning for everyday questions.'
                  },
                  {
                    value: 'high',
                    label: 'Deep',
                    description:
                      'Thorough analysis before answering. Slower but more complete.'
                  }
                ]}
                value={b.reasoning_effort ?? 'medium'}
                customLabel="Reasoning effort"
                customSelectOptions={[
                  { value: 'none', label: 'none' },
                  { value: 'minimal', label: 'minimal' },
                  { value: 'low', label: 'low' },
                  { value: 'medium', label: 'medium' },
                  { value: 'high', label: 'high' },
                  { value: 'xhigh', label: 'xhigh' },
                  { value: 'max', label: 'max' }
                ]}
                onchange={(v) => {
                  b.reasoning_effort = v
                  void ai.persistProvider(w)
                }}
              />
            {/if}

            <PresetControl
              label="Answer Length"
              tooltipText="How long should the AI's answer be? Shorter answers are faster."
              tooltipTechnical="Technical: Maximum output tokens."
              options={[
                {
                  value: 512,
                  label: 'Concise',
                  description: 'Short, to-the-point answers.'
                },
                {
                  value: 2048,
                  label: 'Standard',
                  description: 'Moderate length with enough detail.'
                },
                {
                  value: 4096,
                  label: 'Detailed',
                  description: 'In-depth answers with full explanations.'
                }
              ]}
              value={b.max_tokens ?? 2048}
              customLabel="Max tokens"
              customMin={1}
              customStep={1}
              customSuffix="tokens"
              onchange={(v) => {
                b.max_tokens = v
                void ai.persistProvider(w)
              }}
            />
          {/if}

          {#if w === 'embedding'}
            {#if embedCaps?.supportsTruncation === false}
              <div class="flex flex-col gap-1.5">
                <span
                  class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
                  >Index Density</span
                >
                <span
                  class="inline-flex items-center gap-1.5 self-start rounded-lg border border-surface-panel-border bg-surface-panel/40 px-3 py-1.5 text-type-xs text-text-muted"
                >
                  Fixed{embedCaps.nativeDimensions
                    ? ` at ${embedCaps.nativeDimensions} dimensions`
                    : ' dimensions'} (this model doesn't support truncation)
                </span>
              </div>
            {:else}
              <PresetControl
                label="Index Density"
                tooltipText="How detailed each search entry is. Higher means more precise search but more storage. Compact uses truncated dimensions to save space with minimal quality loss."
                tooltipTechnical="Technical: Embedding output dimensions (Matryoshka Representation Learning truncation). Only supported by some models."
                options={[
                  {
                    value: 0,
                    label: 'Auto',
                    description:
                      "Uses the model's recommended setting. Best for most users."
                  },
                  {
                    value: 768,
                    label: 'Compact',
                    description:
                      'Smaller index, faster search. Slight quality tradeoff.'
                  },
                  {
                    value: 1024,
                    label: 'Balanced',
                    description: 'Good middle ground for large vaults.'
                  }
                ]}
                value={b.dimensions ?? 0}
                customLabel="Dimensions"
                customMin={1}
                customStep={1}
                customSuffix="dimensions"
                onchange={(v) => {
                  b.dimensions = v === 0 ? undefined : v
                  void ai.persistProvider(w)
                }}
              />
              {#if embedCaps?.supportsTruncation === undefined}
                <p class="text-type-2xs text-text-muted m-0">
                  If this model doesn't support truncation, the API will reject
                  it — fall back to Auto.
                </p>
              {/if}
            {/if}
          {/if}

          <div class="flex flex-col gap-1.5 max-w-xs">
            <div class="flex items-center gap-1.5">
              <label
                class="text-text-muted text-type-2xs font-semibold uppercase tracking-wider"
                for="{idPrefix}-timeout">Timeout</label
              >
              <InfoTooltip
                text="How long to wait before giving up on a response. Increase this if you use a slow model."
                technical="Technical: Request timeout in milliseconds."
                label="What is Timeout?"
              />
            </div>
            <input
              id="{idPrefix}-timeout"
              type="number"
              min="1000"
              step="500"
              bind:value={b.timeout_ms}
              onblur={() => void ai.persistProvider(w)}
              class="bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2 text-text-primary text-type-md font-body-md outline-none focus:border-accent-primary-start focus:ring-1 focus:ring-accent-primary-start transition-all"
            />
            {#if ai.advancedFieldError(w, 'timeout_ms')}
              <span class="text-error text-type-2xs font-label-sm" role="alert"
                >{ai.advancedFieldError(w, 'timeout_ms')}</span
              >
            {/if}
          </div>
        </div>
      {/snippet}

      {#if activeSegment === 'ai-setup'}
        <!-- Provider cards (same Setup view; panel chrome is above snippets). -->
        <div class="space-y-4" aria-labelledby="ai-seg-tab-ai-setup">
          <!-- Sync Mode: chat card represents both configs. Split: hide inactive via CSS. -->
          <section
            aria-labelledby="chat-heading"
            class:hidden={!ai.syncProviders && ai.activeRole !== 'chat'}
          >
            {#if !ai.syncProviders}
              <h3
                id="chat-heading"
                class="font-label-sm-bold text-text-muted uppercase tracking-widest text-type-2xs mb-3"
              >
                Chat model
              </h3>
            {:else}
              <h3 id="chat-heading" class="sr-only">Chat model</h3>
            {/if}
            <div
              class="ml-3 pl-3 border-l border-surface-panel-border"
              class:ml-0={ai.syncProviders}
              class:pl-0={ai.syncProviders}
              class:border-l-0={ai.syncProviders}
            >
              {@render providerCard('chat')}
            </div>
          </section>

          <section
            id="ai-embedding-section"
            aria-labelledby="embedding-heading"
            class:hidden={ai.syncProviders || ai.activeRole !== 'embedding'}
          >
            <h3
              id="embedding-heading"
              class="font-label-sm-bold text-text-muted uppercase tracking-widest text-type-2xs mb-3"
            >
              Embedding model
            </h3>
            <div class="ml-3 pl-3 border-l border-surface-panel-border">
              {@render providerCard('embedding')}
            </div>
          </section>
        </div>
      {/if}
      <!-- /Setup provider cards -->

      <!-- Capabilities view -->
      {#if activeSegment === 'ai-capabilities' && showCapabilities}
        <div
          id="ai-capabilities-panel"
          class="space-y-6"
          role="tabpanel"
          aria-labelledby="ai-seg-tab-ai-capabilities"
          aria-label="AI feature fine-tuning"
        >
          {#if writingCtx}
            <section id="ai-writing-tuning" class="space-y-3">
              <div class="flex items-center gap-2">
                <span
                  class="material-symbols-outlined text-icon-lg text-text-muted"
                  aria-hidden="true">ink_pen</span
                >
                <div>
                  <h3 class="text-text-primary text-type-md font-semibold m-0">
                    Writing Assistant
                  </h3>
                  <p class="text-text-muted text-type-xs font-label-sm m-0">
                    Action catalog and limits. Proposals never write until you
                    accept.
                  </p>
                </div>
              </div>
              <div class="ml-3 pl-3 border-l border-surface-panel-border">
                <AssistantSettings ctx={writingCtx} embedded />
              </div>
            </section>
          {/if}

          {#if searchCtx}
            <section id="ai-search-tuning" class="space-y-3">
              <div class="flex items-center gap-2">
                <span
                  class="material-symbols-outlined text-icon-lg text-text-muted"
                  aria-hidden="true">manage_search</span
                >
                <div>
                  <h3 class="text-text-primary text-type-md font-semibold m-0">
                    Semantic search
                  </h3>
                  <p class="text-text-muted text-type-xs font-label-sm m-0">
                    Index, search balance, and context breadth.
                  </p>
                </div>
              </div>
              <div class="ml-3 pl-3 border-l border-surface-panel-border">
                <QASettings ctx={searchCtx} embedded />
              </div>
            </section>
          {/if}

          {#if summaryCtx}
            <section id="ai-summary-tuning" class="space-y-3">
              <div class="flex items-center gap-2">
                <span
                  class="material-symbols-outlined text-icon-lg text-text-muted"
                  aria-hidden="true">notes</span
                >
                <div>
                  <h3 class="text-text-primary text-type-md font-semibold m-0">
                    Note summaries
                  </h3>
                  <p class="text-text-muted text-type-xs font-label-sm m-0">
                    Banner length, auto-on-open, and facets.
                  </p>
                </div>
              </div>
              <div class="ml-3 pl-3 border-l border-surface-panel-border">
                <AISummarySettings ctx={summaryCtx} embedded />
              </div>
            </section>
          {/if}
        </div>
      {/if}
      <!-- /Capabilities view -->

      <!-- Advanced view -->
      {#if activeSegment === 'ai-advanced'}
        <div
          id="ai-advanced-panel"
          class="space-y-3"
          role="tabpanel"
          aria-labelledby="ai-seg-tab-ai-advanced"
          aria-label="Advanced AI settings"
        >
          <!-- Section 1: Advanced Options -->
          <details
            class="group bg-surface-panel/10 border border-surface-panel-border rounded-xl"
          >
            <summary
              class="flex items-center justify-between p-4 cursor-pointer select-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
            >
              <div class="flex items-center gap-2.5">
                <span
                  class="material-symbols-outlined text-icon-lg text-text-muted"
                  aria-hidden="true">tune</span
                >
                <div class="text-left">
                  <span
                    class="text-type-sm font-semibold text-text-primary block"
                    >Advanced Options</span
                  >
                  <span class="text-type-2xs text-text-muted block mt-0.5"
                    >{ai.tuningSummary}</span
                  >
                </div>
              </div>
              <span
                class="material-symbols-outlined text-icon-lg text-text-muted transition-transform group-open:rotate-180"
                aria-hidden="true">expand_more</span
              >
            </summary>
            <div class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4">
              <div class="space-y-5">
                {#if ai.syncProviders}
                  <div>
                    <h4
                      class="text-type-xs font-semibold text-text-primary mb-3"
                    >
                      AI Assistant
                    </h4>
                    {@render advancedTuningGrid('chat')}
                  </div>
                  {#if supportsEmbeddings(ai.config.chat.provider_type)}
                    <div class="border-t border-surface-panel-border/30 pt-4">
                      <h4
                        class="text-type-xs font-semibold text-text-primary mb-3"
                      >
                        Search Index
                      </h4>
                      {@render advancedTuningGrid('embedding')}
                    </div>
                  {/if}
                {:else}
                  {@render advancedTuningGrid(ai.activeRole)}
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
                  class="material-symbols-outlined text-icon-lg text-text-muted"
                  aria-hidden="true">vpn_key</span
                >
                <div class="text-left">
                  <span
                    class="text-type-sm font-semibold text-text-primary block"
                    >Key storage</span
                  >
                  <span class="text-type-2xs text-text-muted block mt-0.5"
                    >{ai.keyringSummary}</span
                  >
                </div>
              </div>
              <span
                class="material-symbols-outlined text-icon-lg text-text-muted transition-transform group-open:rotate-180"
                aria-hidden="true">expand_more</span
              >
            </summary>
            <div
              class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4 space-y-3"
            >
              {#if !ai.config.keyring_available}
                <div
                  class="flex items-start gap-2 p-3 rounded-lg bg-status-warn/5 border border-status-warn/30 text-status-warn text-type-sm font-body-md"
                  role="alert"
                >
                  <span
                    class="material-symbols-outlined text-icon-lg mt-0.5 flex-shrink-0"
                    aria-hidden="true">warning</span
                  >
                  <span class="flex-1">
                    No OS keyring was found on this system. Keys will be stored
                    in
                    <code class="font-mono text-type-xs">config.yaml</code> regardless
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
                  checked={ai.config.use_keyring}
                  disabled={!ai.config.keyring_available}
                  onchange={(e: Event) =>
                    void ai.toggleKeyring(
                      (e.currentTarget as HTMLInputElement).checked
                    )}
                />
                <span
                  aria-hidden="true"
                  class="keyring-switch-track"
                  class:on={ai.config.use_keyring &&
                    ai.config.keyring_available}
                  class:disabled={!ai.config.keyring_available}
                ></span>
                <span class="flex-1">
                  <span
                    class="text-text-primary text-type-md font-body-md block"
                  >
                    Store API keys in the OS keyring
                  </span>
                  <span
                    class="text-text-muted text-type-xs font-label-sm block mt-0.5"
                  >
                    When on, keys live in the OS keyring instead of the vault's
                    <code class="font-mono text-type-xs">config.yaml</code>, so
                    they don't travel when the vault syncs. Turning this off
                    leaves existing keyring entries in place until you clear or
                    re-enter each key.
                  </span>
                </span>
              </label>
            </div>
          </details>

          <!-- Section 3: Recent Activity -->
          <details
            bind:open={ai.auditOpen}
            class="group bg-surface-panel/10 border border-surface-panel-border rounded-xl"
          >
            <summary
              class="flex items-center justify-between p-4 cursor-pointer select-none focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-primary-start rounded-xl"
            >
              <div class="flex items-center gap-2.5">
                <span
                  class="material-symbols-outlined text-icon-lg text-text-muted"
                  aria-hidden="true">history</span
                >
                <div class="text-left">
                  <!-- summaryEl in test queries exact text 'Plugin AI calls' -->
                  <span
                    class="text-type-sm font-semibold text-text-primary block"
                    >Plugin AI calls</span
                  >
                  <span class="text-type-2xs text-text-muted block mt-0.5"
                    >{ai.auditSummary}</span
                  >
                </div>
              </div>
              <span
                class="material-symbols-outlined text-icon-lg text-text-muted transition-transform group-open:rotate-180"
                aria-hidden="true">expand_more</span
              >
            </summary>
            <div class="px-4 pb-4 border-t border-surface-panel-border/30 pt-4">
              {#if ai.auditState === 'loading'}
                <div
                  class="text-text-muted text-type-sm font-body-md animate-pulse py-3"
                >
                  Loading audit log…
                </div>
              {:else if ai.auditError}
                <div
                  class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-type-sm font-body-md"
                  role="alert"
                >
                  <span
                    class="material-symbols-outlined text-icon-lg"
                    aria-hidden="true">error</span
                  >
                  <span class="flex-1"
                    >Failed to load audit log: {ai.auditError}</span
                  >
                  <button
                    type="button"
                    onclick={() => void ai.loadAudit()}
                    class="text-type-xs font-label-sm-bold underline bg-transparent border-none cursor-pointer text-error"
                  >
                    Retry
                  </button>
                </div>
              {:else if ai.audit.length === 0}
                <p class="text-text-muted text-type-sm font-body-md py-3">
                  No activity recorded yet.
                </p>
              {:else}
                <div class="overflow-x-auto">
                  <table
                    class="w-full text-type-xs font-body-md border-collapse"
                  >
                    <caption class="sr-only"> Recent plugin AI calls </caption>
                    <thead>
                      <tr
                        class="text-left text-text-muted border-b border-surface-panel-border"
                      >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >When</th
                        >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >Plugin</th
                        >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >Kind</th
                        >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >Host</th
                        >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >Model</th
                        >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >Status</th
                        >
                        <th
                          scope="col"
                          class="py-2 pr-3 font-label-sm-bold uppercase tracking-wider text-type-2xs"
                          >Tokens</th
                        >
                      </tr>
                    </thead>
                    <tbody>
                      {#each ai.audit as entry, i (`${entry.at}:${i}`)}
                        <tr
                          class="border-b border-surface-panel-border/50 text-text-primary"
                        >
                          <td
                            class="py-1.5 pr-3 whitespace-nowrap"
                            title={entry.at}>{ai.formatAuditTime(entry.at)}</td
                          >
                          <td class="py-1.5 pr-3">{entry.plugin}</td>
                          <td class="py-1.5 pr-3 capitalize">{entry.kind}</td>
                          <td class="py-1.5 pr-3 truncate max-w-44"
                            >{entry.host}</td
                          >
                          <td class="py-1.5 pr-3 truncate max-w-40"
                            >{entry.model}</td
                          >
                          <td class="py-1.5 pr-3">
                            {#if entry.status === 'ok'}
                              <span
                                class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-accent-primary-glow/20 border border-accent-primary-start text-accent-primary-start font-label-sm-bold text-type-2xs"
                              >
                                <span
                                  class="material-symbols-outlined text-type-2xs"
                                  aria-hidden="true">check_circle</span
                                >
                                ok
                              </span>
                            {:else}
                              <span
                                class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded bg-status-danger/10 text-status-danger font-label-sm-bold text-type-2xs"
                              >
                                <span
                                  class="material-symbols-outlined text-type-2xs"
                                  aria-hidden="true">error</span
                                >
                                {entry.status}
                              </span>
                            {/if}
                          </td>
                          <td
                            class="py-1.5 pr-3 text-text-muted whitespace-nowrap"
                          >
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
                    onclick={() => void ai.clearAudit()}
                    class="inline-flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted font-label-sm-bold hover:text-error hover:border-error/50 transition-colors cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
                  >
                    <span
                      class="material-symbols-outlined text-icon-md"
                      aria-hidden="true">delete_sweep</span
                    >
                    Clear log
                  </button>
                </div>
              {/if}
            </div>
          </details>
        </div>
      {/if}
      <!-- /Advanced view -->

      {#if ai.loadError}
        <!-- Soft error banner -->
        <div
          class="flex items-start gap-2 p-3 rounded-lg bg-error-bg border border-error-border text-error text-type-sm font-body-md"
          role="alert"
        >
          <span
            class="material-symbols-outlined text-icon-lg"
            aria-hidden="true">error</span
          >
          <span class="flex-1">{ai.loadError}</span>
          <button
            type="button"
            onclick={() => {
              ai.loadError = null
              void ai.reload()
            }}
            class="text-type-xs font-label-sm-bold underline bg-transparent border-none cursor-pointer text-error"
          >
            Retry
          </button>
        </div>
      {/if}
    </div>
    <!-- /scroll body -->
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
