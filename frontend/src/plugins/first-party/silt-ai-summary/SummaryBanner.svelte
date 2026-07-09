<script lang="ts">
  /**
   * SummaryBanner — dismissible AI summary highlight for silt-ai-summary.
   *
   * State machine (mutually exclusive — evaluated top-down):
   *   1. unconfigured  — aiProviderNeedsSetup(chat) is true. Muted nudge to
   *                      configure a provider. No spinner, no retry, no facets.
   *   2. loading       — pageState.status === 'loading' OR no pageState yet
   *                      while a generation is expected (the brief race before
   *                      the controller seeds state for a fresh generation).
   *                      Skeleton + announced "Summarizing…". Regenerate disabled.
   *   3. stale         — pageState.status === 'ready' && pageState.stale. The
   *                      prior summary + facets stay readable while a regen runs;
   *                      a subtle "Updating…" line + spinning refresh overlay it.
   *   4. ready         — pageState.status === 'ready' && result.ok. Renders the
   *                      summary sentence(s) + facet lists. Empty summary AND no
   *                      facet items → muted "Nothing to highlight for this note."
   *   5. error         — pageState.status === 'error' OR result is an error
   *                      outcome. Inline, non-blocking. Error text + Retry.
   *
   * Dismissal is keyed by `${pageId}:${contentHash}` (#455). Editing the note
   * changes its content hash, so the next open re-shows the banner with the
   * new summary. The controller regenerates on content change regardless;
   * re-showing on the next open after an edit is the safe UX default. Falls
   * back to bare `pageId` when no hash is available (error states where the
   * content never reached the hasher).
   */

  import { getController, BANNER_SURFACE_ID } from './index'
  import type { PageState } from './state.svelte'
  import type { PluginContext } from '../../sdk'
  import type { SummaryResult, FacetDiff, SummaryError } from './types'
  import { DEFAULT_SETTINGS } from './settings'
  import { settings as appSettings } from '../../../settings/store.svelte'
  import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
  import { untrack } from 'svelte'

  interface Props {
    ctx: PluginContext
    onDismiss: () => void
  }
  let { ctx, onDismiss }: Props = $props()

  // The close button's data-banner-close MUST match the surface id the
  // orchestrator registers the banner under (BANNER_SURFACE_ID). The host's
  // cross-banner focus management queries that attribute after a dismiss to
  // forward focus to the next banner; a mismatch silently drops focus.
  const FACET_PREVIEW_LIMIT = 3
  // Bound the dismissed list so config.yaml doesn't grow without limit. The
  // list is keyed by pageId, so dropping the oldest entry only re-shows a
  // banner for a note dismissed long ago — an acceptable trade vs. unbounded
  // config growth. 500 covers any realistic vault; the host's RMW write can't
  // race with a second summary dismissal anyway (only one banner surface exists).
  const MAX_DISMISSED = 500

  type FacetKey = 'tasks' | 'risks' | 'decisions'
  interface FacetMeta {
    key: FacetKey
    label: string
    icon: string
  }
  const FACETS: readonly FacetMeta[] = [
    { key: 'tasks', label: 'Tasks', icon: 'task_alt' },
    { key: 'risks', label: 'Risks', icon: 'warning' },
    { key: 'decisions', label: 'Decisions', icon: 'gavel' }
  ]

  // pageId mirrors the controller's key: `${notebook}/${section}/${page}`.
  const pageId = $derived(
    `${ctx.activeNotebook}/${ctx.activeSection}/${ctx.activePage}`
  )
  const controller = getController()
  const pageState = $derived<PageState | undefined>(controller?.state[pageId])
  const settings = $derived(controller?.getSettings() ?? DEFAULT_SETTINGS)
  const unconfigured = $derived(
    aiProviderNeedsSetup(appSettings.config?.ai?.chat)
  )

  // --- State machine (mutually exclusive) -----------------------------------
  const result = $derived(
    pageState?.result?.ok ? pageState.result.result : undefined
  )
  const errorOutcome = $derived(
    pageState?.result?.ok === false ? pageState.result.error : undefined
  )
  const isError = $derived(
    pageState?.status === 'error' || errorOutcome !== undefined
  )
  // Loading: explicit loading status, OR no state yet while configured + not
  // erroring (the brief race before the controller seeds state).
  const isLoading = $derived(
    pageState?.status === 'loading' || (!pageState && !unconfigured && !isError)
  )
  const isStale = $derived(
    pageState?.status === 'ready' &&
      pageState.stale === true &&
      pageState.result?.ok === true
  )
  const isReady = $derived(
    pageState?.status === 'ready' && pageState.result?.ok === true
  )
  const summaryText = $derived((result?.summary ?? '').trim())
  // Empty: ready state with no summary text. The spec gates the "Nothing to
  // highlight" muted line on summary === '' and renders it INSTEAD of facets
  // (the empty-note case). In practice the extractor returns no facets when
  // the summary is empty, so the summary-only gate matches the spec literally.
  const isEmpty = $derived(isReady && summaryText === '')

  // --- Per-facet "Show more" expand state -----------------------------------
  // Reset on page switch so a stale expand from the prior note doesn't bleed
  // into the new one. `lastPageId` snapshots the initial pageId outside
  // reactivity (untrack) so it acts as a stable "previous value" tracker; the
  // effect then reads pageId reactively and resets expandedFacets on change.
  let expandedFacets = $state<Record<FacetKey, boolean>>({
    tasks: false,
    risks: false,
    decisions: false
  })
  let lastPageId = untrack(() => pageId)
  $effect(() => {
    if (pageId !== lastPageId) {
      lastPageId = pageId
      expandedFacets = { tasks: false, risks: false, decisions: false }
      reshowDone = false
      reshowHint = false
    }
  })

  // Edit-triggered re-show detection (#455). When the banner re-appears
  // because the note was edited since the last dismissal, the dismissed_notes
  // list still holds `${pageId}:<oldHash>` while pageState.contentHash is now
  // different. Surface a one-time hint so the un-asked-for re-show isn't
  // confusing. `reshowDone` is a plain guard (not $state) so it doesn't
  // re-trigger the effect; reset on page switch alongside expandedFacets.
  let reshowHint = $state(false)
  let reshowDone = false
  $effect(() => {
    if (reshowDone) return
    if (!isReady || !pageState?.contentHash) return
    reshowDone = true
    const dismissed = settings.dismissed_notes ?? []
    const prefix = `${pageId}:`
    if (
      dismissed.some(
        (e) => e.startsWith(prefix) && e !== `${prefix}${pageState.contentHash}`
      )
    ) {
      reshowHint = true
      const t = setTimeout(() => (reshowHint = false), 5000)
      return () => clearTimeout(t)
    }
  })

  function visibleItems(r: SummaryResult, key: FacetKey): string[] {
    const all = r[key] ?? []
    return expandedFacets[key] ? all : all.slice(0, FACET_PREVIEW_LIMIT)
  }
  function hiddenCount(r: SummaryResult, key: FacetKey): number {
    const all = r[key] ?? []
    return expandedFacets[key]
      ? 0
      : Math.max(0, all.length - FACET_PREVIEW_LIMIT)
  }
  function isNewItem(r: SummaryResult, key: FacetKey, item: string): boolean {
    const diff: FacetDiff = r.newItems ?? {
      tasks: [],
      risks: [],
      decisions: []
    }
    return (diff[key] ?? []).includes(item)
  }
  function newCount(r: SummaryResult, key: FacetKey): number {
    const items = r[key] ?? []
    const news = r.newItems?.[key] ?? []
    return items.filter((i) => news.includes(i)).length
  }
  function visibleFacets(): FacetMeta[] {
    if (!result) return []
    return FACETS.filter(
      (f) => settings.facets[f.key] && (result[f.key] ?? []).length > 0
    )
  }

  // Polite live-region announcement. Empty only when the visible content
  // alone is enough (loading-skeleton first paint, unconfigured). Ready-with-
  // content always speaks — either new counts or a bare "Summary ready." so a
  // screen-reader user gets an audible completion signal even when nothing is
  // new (the common first-open case where the diff is empty because there's no
  // prior snapshot to diff against). Inline IIFE so the derivation reads other
  // runes (unconfigured, isError, isLoading, isStale, isReady, result)
  // naturally and stays reactive.
  const announcement = $derived.by(() => {
    if (unconfigured) return ''
    if (isError) {
      return errorOutcome?.code === 'unconfigured'
        ? 'AI provider not configured.'
        : `Summary error: ${errorOutcome?.message ?? ''}`
    }
    if (isLoading && !isStale) return 'Summarizing…'
    if (isStale) return 'Updating summary…'
    if (isReady && result) {
      const parts: string[] = []
      for (const f of visibleFacets()) {
        const n = newCount(result, f.key)
        if (n > 0) parts.push(`${n} new ${f.label.toLowerCase()}`)
      }
      if (parts.length) return parts.join(', ') + '.'
      return summaryText ? 'Summary ready.' : ''
    }
    return ''
  })

  // --- Actions --------------------------------------------------------------
  async function handleRegenerate() {
    // Block while ANY generation is in flight — fresh loading OR a stale
    // update. The generation counter protects state correctness, but without
    // this gate rapid clicks during isStale would stack wasted LLM calls
    // (local models are slow; each force-click burns a full completion).
    if (isLoading || isStale) return
    await controller?.generateFor(ctx, pageId, { force: true })
  }

  async function handleClose() {
    // Dismissal key is `${pageId}:${contentHash}` (#455) — an edit changes
    // the hash, so the next open re-shows the banner for the new content.
    // Falls back to bare `pageId` when no hash is available (oversized /
    // fetch-failed error states where the content never reached the hasher).
    // ctx.updatePluginSetting is the SDK's 2-arg (key, value) form — the
    // pluginID is captured in the ctx closure.
    const cur = settings.dismissed_notes ?? []
    const key = pageState?.contentHash
      ? `${pageId}:${pageState.contentHash}`
      : pageId
    // Compute the bounded list up front (reads settings/pageId while the
    // component is still mounted), then dismiss visually FIRST so the IPC
    // round-trip doesn't add perceptible lag to the close interaction. The
    // host's dismiss path is idempotent and persistence is best-effort (the
    // catch below), so there's no correctness reason to gate the visual on
    // the write.
    const bounded = cur.includes(key)
      ? cur
      : (() => {
          const next = [...cur, key]
          return next.length > MAX_DISMISSED
            ? next.slice(next.length - MAX_DISMISSED)
            : next
        })()
    onDismiss()
    if (!cur.includes(key)) {
      try {
        await ctx.updatePluginSetting('dismissed_notes', bounded)
      } catch {
        /* best-effort — the banner is already torn down via onDismiss */
      }
    }
  }

  function errorMessage(e: SummaryError | undefined): string {
    if (!e) return "Couldn't generate a summary."
    if (e.code === 'unconfigured')
      return 'Configure an AI provider in Settings → AI Provider to generate summaries.'
    if (e.code === 'oversized')
      // The note hasn't shrunk, so a retry deterministically fails the same
      // way — point at the setting by name instead of implying retryability.
      return 'This note exceeds the "Max note size" limit (Settings → AI Summary). Split the note or raise the limit.'
    if (e.code === 'fetch-failed')
      return "Couldn't read this note's content. The vault may be busy — try again."
    return `Couldn't generate a summary. ${e.message ?? ''}`.trim()
  }

  // Minimal relative-time formatter for the freshness line. The repo has no
  // shared time util, so keep this local and dependency-free. Output is
  // computed at render time only — no ticking timer; a banner left open for an
  // hour simply reads "5m ago" until something triggers a re-render.
  function formatFreshness(iso: string): string {
    const ts = new Date(iso).getTime()
    if (!Number.isFinite(ts)) return ''
    const diffSec = Math.max(0, (Date.now() - ts) / 1000)
    if (diffSec < 60) return 'Generated just now'
    if (diffSec < 3600) return `Generated ${Math.floor(diffSec / 60)}m ago`
    if (diffSec < 86400) return `Generated ${Math.floor(diffSec / 3600)}h ago`
    if (diffSec < 604800) return `Generated ${Math.floor(diffSec / 86400)}d ago`
    return `Generated ${new Date(iso).toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric'
    })}`
  }
</script>

<section
  class="summary-banner"
  class:is-loading={isLoading && !isStale}
  class:is-stale={isStale}
  class:is-error={isError}
  class:is-unconfigured={unconfigured}
  class:is-empty={isEmpty}
  aria-label="AI summary"
  onpointerdown={() => {
    if (reshowHint) reshowHint = false
  }}
>
  <header class="head">
    <span class="lead-icon material-symbols-outlined" aria-hidden="true"
      >auto_awesome</span
    >

    <div class="body">
      {#if unconfigured}
        <p class="line muted">
          Configure an AI provider in <strong
            >Settings &rarr; AI Provider</strong
          > to generate summaries.
        </p>
        <button
          type="button"
          class="inline-cta"
          onclick={() => ctx.openSettings('ai')}
        >
          Open AI Provider settings
        </button>
      {:else if isError}
        <div class="error-row">
          <p class="line error-text" role="status">
            {errorMessage(errorOutcome)}
          </p>
          {#if errorOutcome?.code === 'oversized'}
            <button
              type="button"
              class="inline-cta"
              onclick={() => ctx.openSettings('plugin:silt-ai-summary')}
            >
              Open AI Summary settings
            </button>
          {:else}
            <button
              type="button"
              class="inline-cta"
              onclick={handleRegenerate}
              aria-label="Retry"
            >
              <span class="material-symbols-outlined" aria-hidden="true"
                >refresh</span
              >
              Retry
            </button>
          {/if}
        </div>
      {:else if isLoading && !isStale}
        <div class="skeleton" aria-hidden="true">
          <div class="sk-line"></div>
          <div class="sk-line w70"></div>
        </div>
      {:else if isEmpty}
        <p class="line muted">Nothing to highlight for this note.</p>
      {:else if isReady}
        {#if summaryText}
          <p class="line summary-text">{summaryText}</p>
        {/if}
        {#if !isStale && result?.generatedAt}
          <p class="freshness-line">{formatFreshness(result.generatedAt)}</p>
        {/if}
        {#if reshowHint}
          <p class="reshow-hint">
            Re-showing — this note changed since you dismissed it.
          </p>
        {/if}
        {#if isStale}
          <p class="updating-line" role="status">
            <span class="dot" aria-hidden="true"></span>
            Updating…
          </p>
        {/if}
      {/if}
    </div>

    <div class="actions">
      {#if !unconfigured && !isError}
        <button
          type="button"
          class="action regenerate"
          onclick={handleRegenerate}
          disabled={isLoading || isStale}
          aria-label="Regenerate summary"
          title="Regenerate summary"
        >
          <span
            class="material-symbols-outlined"
            class:spinning={isStale}
            aria-hidden="true">refresh</span
          >
        </button>
      {/if}
      {#if !unconfigured}
        <button
          type="button"
          class="action close-btn"
          data-banner-close={BANNER_SURFACE_ID}
          onclick={handleClose}
          aria-label="Dismiss AI summary"
          title="Dismiss"
        >
          <span class="material-symbols-outlined" aria-hidden="true">close</span
          >
        </button>
      {/if}
    </div>
  </header>

  {#if isReady && !isEmpty && result && visibleFacets().length > 0}
    <div class="facets">
      {#each visibleFacets() as facet (facet.key)}
        {@const items = result[facet.key] ?? []}
        {@const hidden = hiddenCount(result, facet.key)}
        {@const news = newCount(result, facet.key)}
        <section
          class="facet"
          aria-labelledby="summary-facet-{facet.key}-label"
        >
          <h3 id="summary-facet-{facet.key}-label" class="facet-head">
            <span
              class="material-symbols-outlined facet-icon {facet.key}"
              aria-hidden="true">{facet.icon}</span
            >
            <span class="facet-name">{facet.label}</span>
            <span class="facet-count">
              {items.length}
              {#if news > 0}
                <span class="count-sep" aria-hidden="true"> · </span><span
                  class="new-count">{news} new</span
                >
              {/if}
            </span>
          </h3>
          <ul id="summary-facet-{facet.key}-list" class="facet-items">
            {#each visibleItems(result, facet.key) as item (item)}
              <li
                class="facet-item"
                class:has-new={isNewItem(result, facet.key, item)}
              >
                <span class="item-text">{item}</span>
                {#if isNewItem(result, facet.key, item)}
                  <span class="new-pill">New</span>
                {/if}
              </li>
            {/each}
          </ul>
          {#if items.length > FACET_PREVIEW_LIMIT}
            <button
              type="button"
              class="show-more"
              onclick={() =>
                (expandedFacets[facet.key] = !expandedFacets[facet.key])}
              aria-expanded={expandedFacets[facet.key]}
              aria-controls="summary-facet-{facet.key}-list"
            >
              {expandedFacets[facet.key] ? 'Show less' : `Show ${hidden} more`}
            </button>
          {/if}
        </section>
      {/each}
    </div>
  {/if}

  <!-- Polite live region: announces state transitions + new facet counts.
       Visually hidden; sighted UX is carried by the skeleton, the "Updating…"
       line, the error text, and the facet "New" pills above. -->
  <span class="sr-only" aria-live="polite" aria-atomic="true"
    >{announcement}</span
  >
</section>

<style>
  /* Tinted highlight region — same 12% / 30% accent-glow mix as
     FormattingFirstRunTip and the third-party note-banner chrome, so every
     dismissible highlight in the editor reads as the same surface kind. */
  .summary-banner {
    --facet-tasks: var(--color-accent-primary-start);
    --facet-risks: var(--color-status-danger);
    --facet-decisions: var(--color-accent-secondary-start);

    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 8px 12px;
    border-radius: 8px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 12%,
      var(--color-surface-card)
    );
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-glow) 30%, transparent);
    color: var(--color-text-primary);
    font-size: 0.82rem;
    line-height: 1.45;
    position: relative;
  }

  .summary-banner.is-error {
    background: color-mix(
      in srgb,
      var(--color-status-danger) 8%,
      var(--color-surface-card)
    );
    border-color: color-mix(
      in srgb,
      var(--color-status-danger) 30%,
      transparent
    );
  }

  /* Muted, non-alarming: dim the accent so the nudge reads as informational,
     not as an error or a fresh result. */
  .summary-banner.is-unconfigured {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 5%,
      var(--color-surface-card)
    );
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-glow) 18%,
      transparent
    );
  }

  /* Stale: dim the prior-pass text + facets so they read as provisional while
     a regen runs. Actions stay full-opacity so the spinning refresh icon stays
     the primary "this is active" signal; the dimming is an additional cue. */
  .summary-banner.is-stale .body,
  .summary-banner.is-stale .facets {
    opacity: 0.6;
    transition: opacity 0.28s ease;
  }

  .head {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    min-width: 0;
  }

  .lead-icon {
    font-size: 18px;
    line-height: 1.4;
    color: var(--color-accent-primary-start);
    flex-shrink: 0;
    margin-top: 1px;
  }
  .is-error .lead-icon {
    color: var(--color-status-danger);
  }
  .is-unconfigured .lead-icon {
    color: var(--color-text-muted);
    opacity: 0.75;
  }

  .body {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    align-self: center;
  }

  .line {
    margin: 0;
    word-wrap: break-word;
    overflow-wrap: anywhere;
  }
  .summary-text {
    color: var(--color-text-primary);
  }
  .muted {
    color: var(--color-text-muted);
  }
  .error-text {
    color: var(--color-status-danger);
  }
  .line :global(strong) {
    color: var(--color-text-primary);
    font-weight: 600;
  }

  /* Inline error: message + Retry sit on one row, wrap on narrow widths. */
  .error-row {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
  }
  .error-row .error-text {
    flex: 1;
    min-width: 12em;
  }

  /* Inline CTA: shared by the error Retry button and the settings deep-links
     (unconfigured nudge, oversized error). Same chrome so every per-banner
     action reads as the same affordance kind. */
  .inline-cta {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    padding: 3px 9px;
    border: 1px solid
      color-mix(in srgb, var(--color-accent-primary-start) 35%, transparent);
    border-radius: 6px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 10%,
      transparent
    );
    color: var(--color-accent-primary-start);
    font-size: 0.74rem;
    cursor: pointer;
    transition:
      background 0.12s ease,
      color 0.12s ease;
  }
  .inline-cta:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 18%,
      transparent
    );
    color: var(--color-text-primary);
  }
  .inline-cta:active {
    filter: brightness(0.92);
  }
  .inline-cta:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }
  .inline-cta .material-symbols-outlined {
    font-size: 14px;
  }

  .updating-line {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    margin: 0;
    font-size: 0.7rem;
    color: var(--color-text-muted);
  }
  .updating-line .dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--color-accent-primary-start);
    animation: pulse 1.4s ease-in-out infinite;
  }

  /* Freshness stamp under the summary — muted like the updating line, but
     static. Lives in .body (read naturally with the section), not in the live
     region — the announcement handles the ready signal. */
  .freshness-line {
    margin: 0;
    font-size: 0.7rem;
    color: var(--color-text-muted);
  }

  /* Edit-triggered re-show nudge — explains why a dismissed banner came back.
     Same muted treatment as the freshness line. */
  .reshow-hint {
    margin: 0;
    font-size: 11px;
    color: var(--color-text-muted);
  }

  /* Loading skeleton — two shimmer lines stand in for the summary sentence. */
  .skeleton {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    max-width: 540px;
    padding: 2px 0;
  }
  .sk-line {
    height: 10px;
    border-radius: 4px;
    background: linear-gradient(
      90deg,
      color-mix(in srgb, var(--color-accent-primary-start) 14%, transparent),
      color-mix(in srgb, var(--color-accent-primary-start) 30%, transparent),
      color-mix(in srgb, var(--color-accent-primary-start) 14%, transparent)
    );
    background-size: 200% 100%;
    animation: shimmer 1.6s linear infinite;
  }
  .sk-line.w70 {
    width: 70%;
  }

  .actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-shrink: 0;
    align-self: flex-start;
  }

  .action {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 4px;
    border: none;
    border-radius: 6px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    transition:
      background 0.12s ease,
      color 0.12s ease;
    line-height: 0;
  }
  .action .material-symbols-outlined {
    font-size: 18px;
  }
  .action:hover:not(:disabled) {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 15%,
      transparent
    );
    color: var(--color-text-primary);
  }
  .action:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }
  .action:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .action.close-btn:hover:not(:disabled) {
    background: color-mix(in srgb, var(--color-text-muted) 18%, transparent);
    color: var(--color-text-primary);
  }

  /* Facets: auto-fit grid so 1/2/3 facets each get a balanced column. Indented
     under the body to align with the summary text, not the lead icon. Bounded
     height with internal scroll so a long facet list never blows out the editor
     (the host's 30vh cap is the outer backstop). */
  .facets {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(190px, 1fr));
    gap: 6px 16px;
    padding-left: 28px;
    max-height: 160px;
    overflow-y: auto;
    scrollbar-width: thin;
  }

  .facet {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 4px;
    animation: facet-in 0.28s ease both;
  }
  .facet:nth-child(2) {
    animation-delay: 0.06s;
  }
  .facet:nth-child(3) {
    animation-delay: 0.12s;
  }

  .facet-head {
    display: flex;
    align-items: center;
    gap: 5px;
    margin: 0;
    font-size: 0.7rem;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-text-muted);
  }
  .facet-icon {
    font-size: 14px;
  }
  .facet-icon.tasks {
    color: var(--facet-tasks);
  }
  .facet-icon.risks {
    color: var(--facet-risks);
  }
  .facet-icon.decisions {
    color: var(--facet-decisions);
  }
  .facet-name {
    color: var(--color-text-primary);
  }
  .facet-count {
    font-weight: 400;
    text-transform: none;
    letter-spacing: 0;
    font-size: 0.7rem;
    color: var(--color-text-muted);
  }
  .new-count {
    color: var(--color-accent-primary-start);
    font-weight: 600;
  }

  .facet-items {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: 3px;
  }
  .facet-item {
    display: flex;
    align-items: baseline;
    gap: 6px;
    font-size: 0.8rem;
    line-height: 1.4;
    color: var(--color-text-primary);
    /* Reserve a 2px rail so the accent border on has-new doesn't shift the
       item relative to non-new siblings in the same list. */
    border-left: 2px solid transparent;
    padding-left: 6px;
  }
  /* Tiny bullet marker via ::before so it survives list-style: none and stays
     aligned with the first text line. */
  .facet-item::before {
    content: '';
    flex-shrink: 0;
    width: 3px;
    height: 3px;
    border-radius: 50%;
    background: var(--color-text-muted);
    transform: translateY(-2px);
  }
  .facet-item.has-new::before {
    background: var(--color-accent-primary-start);
    box-shadow: 0 0 0 2px
      color-mix(in srgb, var(--color-accent-primary-start) 22%, transparent);
  }
  /* Glanceable left-rail signal — the established callout idiom (3px on
     .silt-callout, trimmed to 2px here for the denser facet-item density). */
  .facet-item.has-new {
    border-left-color: var(--color-accent-primary-start);
  }
  .item-text {
    flex: 1;
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .new-pill {
    flex-shrink: 0;
    font-size: 0.7rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 1px 6px;
    border-radius: 999px;
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 22%,
      transparent
    );
    color: var(--color-accent-primary-start);
    line-height: 1.4;
  }

  .show-more {
    align-self: flex-start;
    margin-top: 1px;
    padding: 2px 4px;
    border: none;
    background: transparent;
    color: var(--color-text-muted);
    font-size: 0.7rem;
    cursor: pointer;
    border-radius: 4px;
    transition:
      background 0.1s ease,
      color 0.1s ease;
  }
  .show-more:hover {
    background: color-mix(
      in srgb,
      var(--color-accent-primary-start) 12%,
      transparent
    );
    color: var(--color-text-primary);
  }
  .show-more:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 1px;
  }

  /* Animations — all gated by prefers-reduced-motion at the bottom. */
  .spinning {
    animation: spin 1.1s linear infinite;
  }
  @keyframes shimmer {
    0% {
      background-position: 200% 0;
    }
    100% {
      background-position: -200% 0;
    }
  }
  @keyframes pulse {
    0%,
    100% {
      opacity: 0.4;
      transform: scale(0.8);
    }
    50% {
      opacity: 1;
      transform: scale(1.1);
    }
  }
  @keyframes spin {
    to {
      transform: rotate(360deg);
    }
  }
  @keyframes facet-in {
    from {
      opacity: 0;
      transform: translateY(4px);
    }
    to {
      opacity: 1;
      transform: translateY(0);
    }
  }

  /* Visually hidden but available to assistive tech (the live region). Same
     pattern PluginNoteBanners uses for its collapsed-stack announcement. */
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

  @media (prefers-reduced-motion: reduce) {
    .sk-line,
    .spinning,
    .updating-line .dot,
    .facet {
      animation: none !important;
    }
  }
</style>
