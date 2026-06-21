import { settings } from '../settings/store.svelte'

export type ViewMode = 'edit' | 'source'

// Soft cap on the session-sticky viewMode cache (#199). Decoupled from
// max_open_tabs because the viewMode cache is a different concern (a separate
// per-page preference memory), and we want it comfortably above the tab
// ceiling so a tab-switch flurry never thrashes it. 50 × ~100 bytes ≈ 5 KB.
const MAX_VIEW_MODES = 50

function pageKey(notebook: string, section: string, page: string): string {
  return `${notebook}/${section}/${page}`
}

// Module-scoped reactive record: page key → view mode. Uses a plain object
// (not Map) so Svelte 5's $state deeply tracks property reads/writes.
const viewModes = $state<Record<string, ViewMode>>({})

// Non-reactive sibling bookkeeping for LRU access-order tracking. Reactivity
// is unnecessary for LRU bookkeeping and would only add re-render overhead;
// mirror the TabEntry.lastActivatedAt pattern from lib/tabs.ts.
const lastUsed: Record<string, number> = {}

// Internal: drop the least-recently-used key from both the reactive cache and
// the access-order bookkeeping. Mirrors tabs.ts pickEvictionVictim's reduce.
function evictLRU(): void {
  let victim: string | undefined
  let oldest = Infinity
  for (const k in lastUsed) {
    if (lastUsed[k] < oldest) {
      oldest = lastUsed[k]
      victim = k
    }
  }
  if (victim !== undefined) {
    delete lastUsed[victim]
    delete viewModes[victim]
  }
}

export function getViewMode(
  notebook: string,
  section: string,
  page: string
): ViewMode {
  const key = pageKey(notebook, section, page)
  if (viewModes[key]) {
    // Sticky hit — bump access-order so the LRU keeps this page warm.
    lastUsed[key] = Date.now()
    return viewModes[key]
  }
  // Fall back to the per-vault default_view_mode config (#171).
  const configured = settings.config?.editor?.default_view_mode
  return configured === 'source' ? 'source' : 'edit'
}

export function setViewMode(
  notebook: string,
  section: string,
  page: string,
  mode: ViewMode
): void {
  const key = pageKey(notebook, section, page)
  viewModes[key] = mode
  lastUsed[key] = Date.now()
  if (Object.keys(viewModes).length > MAX_VIEW_MODES) evictLRU()
}

export function toggleViewMode(
  notebook: string,
  section: string,
  page: string
): void {
  const current = getViewMode(notebook, section, page)
  setViewMode(notebook, section, page, current === 'edit' ? 'source' : 'edit')
}

// Test seam: the LRU bookkeeping is module-scoped state. Tests reset both
// the reactive cache and the access-order map; not exported on the public
// surface (no consumer outside the test should reach for this).
export function __resetViewModesForTests(): void {
  for (const k of Object.keys(viewModes)) delete viewModes[k]
  for (const k of Object.keys(lastUsed)) delete lastUsed[k]
}
