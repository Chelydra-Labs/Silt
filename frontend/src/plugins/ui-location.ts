// UI location snapshot for plugins (#680).
//
// App-owned ephemeral state (active page, focused/selected block, open tabs)
// that is not vault content. The agent captures a snapshot at run start so
// deictic phrases ("this page", "here", "open tabs") resolve without dumping
// page bodies into the system prompt.

import { getActiveLocation } from './location.svelte'

/** One open editor tab as seen by the agent (identifiers only). */
export interface UiLocationTab {
  notebook: string
  section: string
  page: string
  /** True when the tab is the transient preview slot. */
  preview?: boolean
  /** True when this tab is the active editor tab. */
  active: boolean
}

/** Structured UI location — paths/ids only, never full page bodies. */
export interface UiLocationSnapshot {
  notebook: string
  section: string
  page: string
  /** Focused/selected block id when present; omitted when none. */
  blockId?: string
  openTabs: UiLocationTab[]
}

export type OpenTabsProvider = () => UiLocationTab[]

let openTabsProvider: OpenTabsProvider | null = null

/** Last selection:changed payload that carried a block id (may be stale). */
let lastSelection: {
  notebook: string
  section: string
  page: string
  blockId?: string
} | null = null

/**
 * Register the host open-tabs provider (App.svelte). Pass null on vault close.
 */
export function setOpenTabsProvider(provider: OpenTabsProvider | null): void {
  openTabsProvider = provider
}

/**
 * Record the latest editor selection/focus block. Called from the shell when
 * selection:changed fires (or tests). Cleared when the user navigates away
 * from that page or on vault close.
 */
export function recordSelectionFocus(payload: {
  notebook: string
  section: string
  page: string
  blockId?: string
}): void {
  lastSelection = {
    notebook: payload.notebook || '',
    section: payload.section || '',
    page: payload.page || '',
    blockId: payload.blockId || undefined
  }
}

/** Drop focused-block memory (vault close / explicit reset). */
export function clearSelectionFocus(): void {
  lastSelection = null
}

/**
 * Capture the current UI location. Safe to call with no provider registered
 * (returns empty tabs + active location from location.svelte.ts).
 */
export function captureUiLocation(): UiLocationSnapshot {
  const loc = getActiveLocation()
  const notebook = loc.notebook || ''
  const section = loc.section || ''
  const page = loc.page || ''

  let blockId: string | undefined
  if (
    lastSelection?.blockId &&
    lastSelection.notebook === notebook &&
    lastSelection.section === section &&
    lastSelection.page === page
  ) {
    blockId = lastSelection.blockId
  }

  const openTabs = openTabsProvider ? openTabsProvider() : []

  return {
    notebook,
    section,
    page,
    ...(blockId ? { blockId } : {}),
    openTabs
  }
}

/** Format a snapshot for the agent system prompt (identifiers only). */
export function formatUiLocationForPrompt(loc: UiLocationSnapshot): string {
  const pagePath =
    loc.notebook && loc.page
      ? loc.section
        ? `${loc.notebook}/${loc.section}/${loc.page}`
        : `${loc.notebook}/${loc.page}`
      : '(none)'

  const blockLine = loc.blockId
    ? `Focused block id: ${loc.blockId}`
    : 'Focused block id: (none)'

  let tabsBlock: string
  if (loc.openTabs.length === 0) {
    tabsBlock = 'Open tabs: (none)'
  } else {
    const lines = loc.openTabs.map((t) => {
      const path = t.section
        ? `${t.notebook}/${t.section}/${t.page}`
        : `${t.notebook}/${t.page}`
      const flags = [t.active ? 'active' : null, t.preview ? 'preview' : null]
        .filter(Boolean)
        .join(', ')
      return flags ? `  - ${path} (${flags})` : `  - ${path}`
    })
    tabsBlock = ['Open tabs:', ...lines].join('\n')
  }

  return [
    'UI LOCATION (identifiers only — use tools to load content):',
    `Current page: ${pagePath}`,
    `Active notebook: ${loc.notebook || '(none)'}`,
    `Active section: ${loc.section || '(none)'}`,
    `Active page: ${loc.page || '(none)'}`,
    blockLine,
    tabsBlock,
    'Resolve "this page", "here", and "open tabs" from this snapshot.',
    'Mid-run navigation is ignored for this turn; the snapshot is fixed at run start.'
  ].join('\n')
}
