import { untrack } from 'svelte'
import { SvelteSet } from 'svelte/reactivity'
import {
  GetNavigationPreferences,
  ListNavigation,
  SetNavigationSectionExpanded,
  SetSidebarView,
  SetFavoritePage
} from '../../../bindings/silt/app.js'
import { NavOrderManager } from './navOrder'
import { normalizeNavigationTree, reconcileActive } from './navTree'
import {
  EMPTY_NAVIGATION_PREFERENCES,
  expandActiveAncestors,
  expandedPathsForNotebook,
  locatorKey,
  reconcilePageRefs
} from './navigationPreferences'
import type {
  NavigationPreferences,
  NavigationTree,
  RecentPageRef
} from './types'

/** Active triple read from the host component's bindable props. */
export interface ActiveTriple {
  notebook: string
  section: string
  page: string
}

export interface UseNavLoaderDeps {
  getActive: () => ActiveTriple
  /** Patch the host's bindable active props. Only the provided fields change. */
  setActive: (patch: Partial<ActiveTriple>) => void
  onSelectNotebook: (notebook: string) => void
  onNavigationLoaded?: (tree: NavigationTree) => void
  onNavigationPreferencesLoaded?: (preferences: NavigationPreferences) => void
  onNavigationStatus?: (loading: boolean, error: string) => void
}

/**
 * Owns navigation tree + preference loading, sidebar-view (tab) switching,
 * section-expansion persistence, and the nav-order manager.
 *
 * State atoms and deriveds are returned on the object and must be read as
 * properties (`loader.tree`) — not destructured — so Svelte 5 reactivity is
 * preserved across the module boundary.
 */
export function useNavLoader(deps: UseNavLoaderDeps) {
  let tree = $state<NavigationTree>({ notebooks: [] })
  let navigationLoading = $state(true)
  let navigationError = $state('')
  let preferences = $state<NavigationPreferences>(EMPTY_NAVIGATION_PREFERENCES)
  let preferencesLoading = $state(true)
  let preferencesError = $state('')
  /** Active sidebar view mode, persisted via SetSidebarView. */
  let sidebarTab = $state<'tree' | 'quick'>('tree')
  let sidebarTabHydrated = $state(false)

  // Expanded section names (within the active notebook). The active section is
  // always expanded so the active path stays visible (spatial memory).
  // SvelteSet is deeply reactive; reassignment still used for immutable-style updates.
  // eslint-disable-next-line svelte/no-unnecessary-state-wrap -- whole-set reassignment pattern
  let expandedSections = $state(new SvelteSet<string>())

  // Nav order for drag-to-reorder (#68). Explicit ordering from config.yaml;
  // items not in the map fall back to alphabetical.
  let navOrder = $state<{
    notebooks: string[]
    sections: Record<string, string[]>
    pages: Record<string, string[]>
  }>({
    notebooks: [],
    sections: {},
    pages: {}
  })

  const navOrderManager = new NavOrderManager({
    onStateChange: (s) => {
      navOrder = s
    }
  })

  const favoriteKeys = $derived(
    new SvelteSet(preferences.favorites.map((ref) => locatorKey(ref)))
  )
  const favoriteState = $derived(reconcilePageRefs(tree, preferences.favorites))
  const recentState = $derived(
    reconcilePageRefs(tree, preferences.recent_pages)
  )

  async function loadNavigation() {
    deps.onNavigationStatus?.(true, '')
    try {
      const data = await ListNavigation()
      if (!data) return
      tree = normalizeNavigationTree(data)
      deps.onNavigationLoaded?.(tree)
      navigationError = ''
      const active = deps.getActive()
      const next = reconcileActive(tree, active)
      if (next.notebook !== active.notebook) {
        deps.setActive({ notebook: next.notebook })
        deps.onSelectNotebook(next.notebook)
      }
      if (next.section !== active.section) {
        deps.setActive({ section: next.section })
      }
      if (next.page !== active.page) {
        deps.setActive({ page: next.page })
      }
    } catch (e) {
      navigationError =
        e instanceof Error ? e.message : 'Navigation could not be loaded.'
    } finally {
      navigationLoading = false
      deps.onNavigationStatus?.(false, navigationError)
    }
  }

  let preferenceLoadSequence = 0
  async function loadNavigationPreferences() {
    const sequence = ++preferenceLoadSequence
    preferencesLoading = true
    try {
      const loaded = await GetNavigationPreferences()
      if (sequence !== preferenceLoadSequence) return
      const sidebarView = loaded?.sidebar_view === 'quick' ? 'quick' : 'tree'
      preferences = {
        expanded_sections: loaded?.expanded_sections ?? [],
        recent_pages: loaded?.recent_pages ?? [],
        favorites: loaded?.favorites ?? [],
        sidebar_view: sidebarView
      }
      if (!sidebarTabHydrated) {
        sidebarTab = sidebarView
        sidebarTabHydrated = true
      }
      deps.onNavigationPreferencesLoaded?.(preferences)
      expandedSections = new SvelteSet(
        expandedPathsForNotebook(preferences, deps.getActive().notebook)
      )
      lastExpandedActive = ''
      preferencesError = ''
    } catch (e) {
      if (sequence !== preferenceLoadSequence) return
      preferencesError =
        e instanceof Error ? e.message : 'Sidebar view could not be loaded.'
    } finally {
      if (sequence === preferenceLoadSequence) preferencesLoading = false
    }
  }

  async function loadNavOrder() {
    await navOrderManager.load()
  }

  async function setSidebarTab(next: 'tree' | 'quick') {
    if (sidebarTab === next) return
    const previous = sidebarTab
    const previousView = preferences.sidebar_view
    sidebarTab = next
    preferences = { ...preferences, sidebar_view: next }
    try {
      await SetSidebarView(next)
      preferencesError = ''
    } catch (error) {
      sidebarTab = previous
      preferences = {
        ...preferences,
        sidebar_view: previousView
      }
      preferencesError =
        error instanceof Error
          ? error.message
          : 'Sidebar view preference could not be saved.'
    }
  }

  function setLocalExpansion(
    notebook: string,
    path: string,
    expanded: boolean
  ) {
    const without = preferences.expanded_sections.filter(
      (item) => !(item.notebook === notebook && item.path === path)
    )
    preferences = {
      ...preferences,
      expanded_sections: expanded ? [...without, { notebook, path }] : without
    }
  }

  function toggleSection(path: string) {
    const next = new SvelteSet(expandedSections)
    const expanded = !next.has(path)
    if (!expanded) {
      next.delete(path)
    } else {
      next.add(path)
    }
    expandedSections = next
    const active = deps.getActive()
    setLocalExpansion(active.notebook, path, expanded)
    void SetNavigationSectionExpanded(active.notebook, path, expanded).catch(
      () => {
        preferencesError = 'Expanded sections could not be saved.'
      }
    )
  }

  function setExpandedSections(next: SvelteSet<string>) {
    expandedSections = next
  }

  async function toggleFavorite(ref: {
    notebook: string
    section: string
    page: string
  }) {
    const wasFavorite = favoriteKeys.has(locatorKey(ref))
    try {
      await SetFavoritePage(ref.notebook, ref.section, ref.page, !wasFavorite)
      preferences = {
        ...preferences,
        favorites: wasFavorite
          ? preferences.favorites.filter(
              (item) => locatorKey(item) !== locatorKey(ref)
            )
          : [...preferences.favorites, ref]
      }
      preferencesError = ''
    } catch (e) {
      preferencesError =
        e instanceof Error ? e.message : 'Favorite could not be updated.'
    }
  }

  // Expand an active page's full ancestry once when its location changes.
  // A manual collapse remains respected until the user activates another page.
  let lastExpandedActive = ''
  $effect(() => {
    const active = deps.getActive()
    const notebook = active.notebook
    const section = active.section
    const page = active.page
    const ready = !preferencesLoading
    if (!notebook || !section || !ready) return
    const activeKey = `${notebook}\u0000${section}\u0000${page}`
    if (activeKey === lastExpandedActive) return
    lastExpandedActive = activeKey
    untrack(() => {
      const next = expandActiveAncestors(expandedSections, section)
      const added = [...next].filter((path) => !expandedSections.has(path))
      expandedSections = new SvelteSet(next)
      for (const path of added) {
        setLocalExpansion(notebook, path, true)
        void SetNavigationSectionExpanded(notebook, path, true).catch(() => {
          preferencesError = 'Expanded sections could not be saved.'
        })
      }
    })
  })

  return {
    // state
    get tree() {
      return tree
    },
    get preferences() {
      return preferences
    },
    get navigationLoading() {
      return navigationLoading
    },
    get navigationError() {
      return navigationError
    },
    get preferencesLoading() {
      return preferencesLoading
    },
    get preferencesError() {
      return preferencesError
    },
    get sidebarTab() {
      return sidebarTab
    },
    get expandedSections() {
      return expandedSections
    },
    get navOrder() {
      return navOrder
    },
    get favoriteKeys() {
      return favoriteKeys
    },
    get favoriteState() {
      return favoriteState as {
        available: typeof preferences.favorites
        stale: typeof preferences.favorites
      }
    },
    get recentState() {
      return recentState as {
        available: RecentPageRef[]
        stale: RecentPageRef[]
      }
    },
    navOrderManager,
    // actions
    loadNavigation,
    loadNavigationPreferences,
    loadNavOrder,
    setSidebarTab,
    toggleSection,
    setLocalExpansion,
    setExpandedSections,
    toggleFavorite
  }
}

export type NavLoader = ReturnType<typeof useNavLoader>
