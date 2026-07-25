<script lang="ts">
  // Settings → AI Provider tab.
  //
  // Thin view over reactive controllers:
  // ./ai/aiProviderController.svelte.ts (provider/chat/embedding/audit)
  // ./ai/localMcpController.svelte.ts (Local MCP host)
  // IPC lives in controllers — this file is layout + a11y only.
  import { onMount, onDestroy, tick } from 'svelte'
  import {
    createAIProviderController,
    supportsEmbeddings
  } from './ai/aiProviderController.svelte'
  import { createLocalMcpController } from './ai/localMcpController.svelte'
  import ProviderCard from './ai/ProviderCard.svelte'
  import AdvancedTuningGrid from './ai/AdvancedTuningGrid.svelte'
  import { ringClass } from './SettingsSearch'
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

  // Local MCP (#687) — controller owns IPC; view binds to reactive getters.
  const mcp = createLocalMcpController()

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
    if (seg) void selectSegment(seg)
  })

  onMount(() => {
    void ai.reload()
    void mcp.refresh()
  })

  onDestroy(() => {
    mcp.destroy()
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
              'ai-local-mcp',
              ringAnchor
            )}"
            aria-busy={mcp.saving}
          >
            <div>
              <h3 class="text-text-primary text-type-md font-semibold m-0">
                Local MCP
              </h3>
              <p class="text-text-muted text-type-xs font-label-sm m-0 mt-0.5">
                Generic MCP server for any desktop agent. Read (and with grant,
                edit) this vault over loopback. Off by default.
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
                  checked={mcp.enabled}
                  disabled={mcp.saving}
                  onchange={(e) =>
                    void mcp.save({ enabled: e.currentTarget.checked })}
                />
                <span
                  aria-hidden="true"
                  class="keyring-switch-track"
                  class:on={mcp.enabled}
                ></span>
              </label>
            </div>

            {#if mcp.trayPrompt}
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
                    onclick={() => void mcp.acceptTray()}
                  >
                    Enable close to tray
                  </button>
                  <button
                    type="button"
                    class="px-3 py-1 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                    onclick={() => mcp.dismissTrayPrompt()}
                  >
                    Not now
                  </button>
                </div>
              </div>
            {/if}

            <div
              class="ml-3 pl-3 border-l border-surface-panel-border space-y-3"
              class:opacity-50={!mcp.enabled}
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
                  class:cursor-pointer={mcp.enabled}
                  class:cursor-not-allowed={!mcp.enabled}
                  for="mcp-write"
                >
                  <input
                    id="mcp-write"
                    type="checkbox"
                    class="keyring-switch peer sr-only"
                    aria-labelledby="mcp-write-label"
                    checked={mcp.write}
                    disabled={mcp.saving || !mcp.enabled}
                    onchange={(e) =>
                      void mcp.save({ write: e.currentTarget.checked })}
                  />
                  <span
                    aria-hidden="true"
                    class="keyring-switch-track"
                    class:on={mcp.write}
                    class:disabled={!mcp.enabled}
                  ></span>
                </label>
              </div>

              <p class="text-text-muted text-type-xs m-0" id="mcp-availability">
                MCP availability:
                <strong class="text-text-primary">
                  {mcp.status?.state ?? 'unknown'}
                </strong>
                {#if mcp.status?.message}
                  — {mcp.status.message}
                {/if}
                {#if mcp.status?.endpoint}
                  <br />
                  Endpoint:
                  <code class="text-text-primary">{mcp.status.endpoint}</code>
                {/if}
              </p>

              <div class="flex flex-wrap gap-2">
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer disabled:opacity-50"
                  disabled={mcp.saving}
                  onclick={() => void mcp.refresh()}
                >
                  Refresh status
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                  title="Shows the bearer in the UI for 30s, then clears it from memory"
                  onclick={() => void mcp.revealToken()}
                >
                  {mcp.tokenVisible ? 'Token shown' : 'Show auth token'}
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                  title="Copies the bearer to the clipboard. We try to clear the clipboard after 30s; clipboard history apps may retain a copy."
                  onclick={() => void mcp.copyToken()}
                >
                  {mcp.tokenCopied ? 'Copied' : 'Copy token'}
                </button>
                <button
                  type="button"
                  class="px-3 py-1.5 rounded-md bg-surface-panel text-text-primary text-type-xs border border-surface-panel-border cursor-pointer"
                  onclick={() => void mcp.copyHint()}
                >
                  Copy OpenCode snippet
                </button>
              </div>
              {#if mcp.tokenVisible && mcp.token}
                <p class="text-text-muted text-type-xs m-0 break-all">
                  Bearer token (OS keyring):
                  <code class="text-text-primary select-all">{mcp.token}</code>
                </p>
              {/if}

              <div class="space-y-2" aria-label="Client install notes">
                <p class="text-text-primary text-type-xs font-semibold m-0">
                  Client setup
                </p>
                {#each mcp.installNotes() as note (note.title)}
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

            {#if mcp.error}
              <p class="text-error text-type-xs m-0" role="alert">
                {mcp.error}
              </p>
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
              <ProviderCard which="chat" {ai} />
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
              <ProviderCard which="embedding" {ai} />
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
                    <AdvancedTuningGrid which="chat" {ai} />
                  </div>
                  {#if supportsEmbeddings(ai.config.chat.provider_type)}
                    <div class="border-t border-surface-panel-border/30 pt-4">
                      <h4
                        class="text-type-xs font-semibold text-text-primary mb-3"
                      >
                        Search Index
                      </h4>
                      <AdvancedTuningGrid which="embedding" {ai} />
                    </div>
                  {/if}
                {:else}
                  <AdvancedTuningGrid which={ai.activeRole} {ai} />
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
