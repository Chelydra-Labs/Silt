// SearchResultLike mirrors SearchModalResult's shape. Defined locally (not
// imported from the SearchModal.svelte component) to avoid a .ts -> .svelte
// type dependency; adaptSearchNavigation reads only the navigation fields.
export interface SearchResultLike {
  id: string
  source: string
  notebook: string
  section: string
  page: string
  file_date: string
  clean_content: string
  status?: string
  snippet?: string
}

export interface RecentPageRef {
  notebook: string
  section: string
  page: string
}

// createRecentPageRecorder builds a debounced recent-page persistence recorder.
// Batches rapid activations into one nav refresh; an invalidate() generation
// bump drops in-flight callbacks so a tab switch can't write a stale entry.
export function createRecentPageRecorder(
  persist: (ref: RecentPageRef) => Promise<unknown>,
  refresh: () => void,
  onError: (error: unknown) => void,
  delay = 250
) {
  let refreshTimer: ReturnType<typeof setTimeout> | null = null
  let generation = 0
  let pending = 0
  let refreshNeeded = false

  function scheduleRefresh(): void {
    if (refreshTimer) clearTimeout(refreshTimer)
    refreshTimer = setTimeout(() => {
      refreshTimer = null
      refreshNeeded = false
      refresh()
    }, delay)
  }

  return {
    record(ref: RecentPageRef): void {
      const requestGeneration = generation
      pending += 1
      if (refreshTimer) clearTimeout(refreshTimer)
      refreshTimer = null
      void persist(ref)
        .then(() => {
          if (requestGeneration !== generation) return
          refreshNeeded = true
        })
        .catch(onError)
        .finally(() => {
          if (requestGeneration !== generation) return
          pending -= 1
          if (pending === 0 && refreshNeeded) scheduleRefresh()
        })
    },
    invalidate(): void {
      generation += 1
      pending = 0
      refreshNeeded = false
      if (refreshTimer) clearTimeout(refreshTimer)
      refreshTimer = null
    }
  }
}

// resolveBreadcrumbSectionSelection keeps the active page selected when it
// lives within the chosen section, otherwise clears the page so the section
// itself is the selection target.
export function resolveBreadcrumbSectionSelection(
  currentSection: string,
  currentPage: string,
  selectedSection: string
): { section: string; page: string } {
  const pageIsWithinSelection =
    !!currentPage &&
    (currentSection === selectedSection ||
      currentSection.startsWith(`${selectedSection}/`))
  return {
    section: selectedSection,
    page: pageIsWithinSelection ? currentPage : ''
  }
}

export interface SourceNavigationRef extends RecentPageRef {
  source?: string
}

export interface SearchNavigationJump {
  locator: SourceNavigationRef
  date: string
  blockId: string
}

// adaptSearchNavigation projects a search-modal result into the navigation
// jump shape the routing layer consumes.
export function adaptSearchNavigation(
  result: SearchResultLike
): SearchNavigationJump {
  return {
    locator: {
      source: result.source,
      notebook: result.notebook,
      section: result.section,
      page: result.page
    },
    date: result.file_date,
    blockId: result.id
  }
}

// resolveSourceNavigationTarget resolves a target ref against the navigation
// catalog, falling back to the target itself when no match is found. Source
// defaults to 'vault' so legacy refs without an explicit source still resolve.
export function resolveSourceNavigationTarget<T extends SourceNavigationRef>(
  catalog: readonly T[],
  target: SourceNavigationRef
): SourceNavigationRef {
  if (!target.source) return target
  const source = target.source || 'vault'
  return (
    catalog.find(
      (item) =>
        (item.source || 'vault') === source &&
        item.notebook === target.notebook &&
        item.section === target.section &&
        item.page === target.page
    ) ?? target
  )
}

/** A dashboard row's locator (carries source, unlike the tab PageRef). */
export interface DashboardPageLocator {
  source: string
  notebook: string
  section: string
  page: string
}

/**
 * Resolve a dashboard row open against the tab system. The tab identity model
 * keys on notebook/section/page only — it has no source field — so a
 * linked-notebook row whose path collides with a vault page would activate the
 * wrong tab. Until tabs carry source, gate linked-source opens with a clear
 * message instead of risking the wrong page. Vault rows open by path.
 */
export function resolveDashboardOpenTarget(
  row: DashboardPageLocator
): { kind: 'open'; ref: RecentPageRef } | { kind: 'blocked'; reason: string } {
  if (row.source && row.source !== 'vault') {
    return {
      kind: 'blocked',
      reason:
        'Opening linked-notebook pages from the dashboard is not supported yet.'
    }
  }
  return {
    kind: 'open',
    ref: { notebook: row.notebook, section: row.section, page: row.page }
  }
}
