// Per-page reactive state + controller for silt-ai-summary.
//
// The pure logic (extract / diff / cache / summarize) is in sibling modules;
// this is the reactive shell the banner (Phase 3) binds to. It owns:
//   - the per-page status/result map (Svelte $state, so the banner re-renders),
//   - the configured-provider snapshot read (the Phase 2 stub of the
//     aiProviderNeedsSetup gate — refined in Phase 3, #450),
//   - a per-page debounce for editor:save-triggered regeneration (#222),
//   - note-switch / vault-close cleanup.
//
// Lifecycle: created in onVaultOpen, disposed in onVaultClose. The banner
// reads `controller.state.get(pageId)` and calls `generateFor` / `regenerate`.

import type { PluginContext } from '../../sdk'
import { settings } from '../../../settings/store.svelte'
import { aiProviderNeedsSetup } from '../../../settings/ai-setup'
import { fetchNoteContent } from './content'
import { resolveSettings } from './settings'
import { summarize } from './summarize'
import type { SummaryOutcome, SummarySettings } from './types'

export type PageStatus = 'idle' | 'loading' | 'ready' | 'error'

export interface PageState {
  status: PageStatus
  result?: SummaryOutcome
  /** True when a (re)generation is in flight and a stale result is still
   *  shown underneath — the banner renders a subtle progress affordance. */
  stale?: boolean
}

export interface ProviderInfo {
  isConfigured: boolean
  configuredModel: string
}

/** Read provider readiness from the live app settings store, via the SAME
 *  aiProviderNeedsSetup predicate the Plugins-tab badge, the AI Provider tab
 *  nudge, and the SummaryBanner's unconfigured state all consume (#450 single
 *  source of truth). The controller's gate and the banner's gate therefore
 *  agree by construction — no second predicate to drift. */
export function readProviderInfo(): ProviderInfo {
  const chat = settings.config?.ai?.chat
  return {
    isConfigured: !aiProviderNeedsSetup(chat),
    configuredModel: chat?.model ?? ''
  }
}

export interface SummaryController {
  /** Reactive pageId → state map (Svelte $state). */
  readonly state: Map<string, PageState>
  /** Resolved settings, re-read each call so a settings change applies live. */
  getSettings(): SummarySettings
  /** Generate (or regenerate) for a page. Fetches the page's content when
   *  `content` is omitted. Updates state for the page. Returns the outcome. */
  generateFor(
    ctx: PluginContext,
    pageId: string,
    opts?: {
      content?: string
      force?: boolean
      notebook?: string
      section?: string
      page?: string
    }
  ): Promise<SummaryOutcome>
  /** Debounce a generation for a page (editor:save path). Cancels any pending
   *  generation already queued for that page. */
  scheduleGenerate(
    ctx: PluginContext,
    pageId: string,
    delayMs: number,
    loc: { notebook: string; section: string; page: string }
  ): void
  /** Cancel a pending debounced generation for a page (note switch). */
  cancelPending(pageId: string): void
  /** Drop state for one page (or all when omitted). */
  clear(pageId?: string): void
  /** Release timers + clear all state (onVaultClose). */
  dispose(): void
}

/** Create the per-vault controller. `providerInfo` is injectable so tests can
 *  pin the configured/unconfigured branch without the settings store. */
export function createSummaryController(
  providerInfo: () => ProviderInfo = readProviderInfo
): SummaryController {
  // $state deep-proxies the Map AND its PageState values, so a component
  // reading `controller.state.get(pageId)` (via $derived) re-runs when the
  // controller mutates a page's status/result/stale. Without $state the banner
  // would render once and never update as the LLM call resolves. (Pending
  // timers + generation counters are internal bookkeeping — not reactive.)
  const state = $state(new Map<string, PageState>())
  const pending = new Map<string, ReturnType<typeof setTimeout>>()
  // Track the most-recent generation per page so a stale completion (note
  // switched mid-generation) doesn't overwrite the newer state.
  const generations = new Map<string, number>()

  function nextGen(pageId: string): number {
    const n = (generations.get(pageId) ?? 0) + 1
    generations.set(pageId, n)
    return n
  }

  function setStatus(pageId: string, status: PageStatus, stale?: boolean) {
    const cur = state.get(pageId)
    if (cur) {
      cur.status = status
      cur.stale = stale
    } else {
      state.set(pageId, { status, stale })
    }
  }

  async function run(
    ctx: PluginContext,
    pageId: string,
    content: string,
    force: boolean,
    gen: number
  ): Promise<SummaryOutcome> {
    const info = providerInfo()
    const outcome = await summarize(ctx, {
      pageId,
      cleanContent: content,
      settings: resolveFromStore(ctx),
      configuredModel: info.configuredModel,
      isConfigured: info.isConfigured,
      force
    })
    // A newer generation for this page has started (note switched back + edit);
    // drop this stale completion so the banner shows the freshest result.
    if (generations.get(pageId) !== gen) return outcome
    const cur = state.get(pageId)
    if (cur) {
      cur.result = outcome
      cur.status = outcome.ok ? 'ready' : 'error'
      cur.stale = false
    } else {
      state.set(pageId, {
        status: outcome.ok ? 'ready' : 'error',
        result: outcome
      })
    }
    return outcome
  }

  function resolveFromStore(ctx: PluginContext): SummarySettings {
    // Plugin settings are stored under plugin_settings.silt-ai-summary; read
    // them via the SDK so a linked-notebook override layer applies (#133).
    // The fetch is async in general, but the orchestrator needs settings
    // synchronously for the debounce path — so resolveSettings is fed the
    // already-cached settings.config snapshot (same source the settings page
    // edits). This keeps the gate synchronous + testable.
    void ctx
    const raw = settings.config?.plugins?.plugin_settings?.[
      'silt-ai-summary'
    ] as Record<string, unknown> | undefined
    return resolveSettings(raw)
  }

  return {
    state,
    getSettings() {
      return resolveSettings(
        settings.config?.plugins?.plugin_settings?.['silt-ai-summary'] as
          Record<string, unknown> | undefined
      )
    },
    async generateFor(ctx, pageId, opts = {}) {
      const gen = nextGen(pageId)
      // Show loading only when there's no existing result to show as stale.
      const existing = state.get(pageId)
      if (existing?.result?.ok) {
        existing.stale = true
      } else {
        setStatus(pageId, 'loading')
      }
      let content = opts.content
      if (content === undefined) {
        try {
          content = await fetchNoteContent(
            ctx,
            opts.notebook ?? ctx.activeNotebook,
            opts.section ?? ctx.activeSection,
            opts.page ?? ctx.activePage
          )
        } catch {
          // A content-read failure (locked vault, disk error, query failure) is
          // NOT an empty note. Surfacing it as 'fetch-failed' lets the banner
          // show Retry instead of the muted "Nothing to highlight" — the user
          // keeps a retryable error signal rather than a silent mislabel.
          const outcome: SummaryOutcome = {
            ok: false,
            error: {
              code: 'fetch-failed',
              message:
                "Couldn't read this note's content. The vault may be busy or the note unavailable."
            }
          }
          if (generations.get(pageId) === gen) {
            setStatus(pageId, 'error')
            const cur = state.get(pageId)
            if (cur) cur.result = outcome
            else state.set(pageId, { status: 'error', result: outcome })
          }
          return outcome
        }
      }
      return run(ctx, pageId, content, !!opts.force, gen)
    },
    scheduleGenerate(ctx, pageId, delayMs, loc) {
      const existing = pending.get(pageId)
      if (existing) clearTimeout(existing)
      pending.set(
        pageId,
        setTimeout(() => {
          pending.delete(pageId)
          // generateFor without explicit content → fetches fresh content at
          // fire time, so the debounce always summarizes the LATEST save.
          void this.generateFor(ctx, pageId, loc)
        }, delayMs)
      )
    },
    cancelPending(pageId) {
      const t = pending.get(pageId)
      if (t) {
        clearTimeout(t)
        pending.delete(pageId)
      }
    },
    clear(pageId) {
      if (pageId === undefined) {
        state.clear()
        return
      }
      state.delete(pageId)
    },
    dispose() {
      for (const t of pending.values()) clearTimeout(t)
      pending.clear()
      state.clear()
      generations.clear()
    }
  }
}
