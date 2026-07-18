import type {
  NavigationPageRef,
  NavigationPreferences,
  NavigationTree
} from './types'
import { pageExists, sectionAncestors } from './navTree'

export const EMPTY_NAVIGATION_PREFERENCES: NavigationPreferences = {
  expanded_sections: [],
  recent_pages: [],
  favorites: []
}

export function locatorKey(ref: NavigationPageRef): string {
  return `${ref.notebook}\u0000${ref.section}\u0000${ref.page}`
}

export function expandedPathsForNotebook(
  preferences: NavigationPreferences,
  notebook: string
): Set<string> {
  return new Set(
    preferences.expanded_sections
      .filter((item) => item.notebook === notebook)
      .map((item) => item.path)
  )
}

export function expandActiveAncestors(
  current: ReadonlySet<string>,
  activeSection: string
): Set<string> {
  const next = new Set(current)
  for (const path of sectionAncestors(activeSection)) next.add(path)
  return next
}

export function reconcilePageRefs<T extends NavigationPageRef>(
  tree: NavigationTree,
  refs: readonly T[]
): { available: T[]; stale: T[] } {
  const available: T[] = []
  const stale: T[] = []
  for (const ref of refs) {
    ;(pageExists(tree, ref) ? available : stale).push(ref)
  }
  return { available, stale }
}

export function pagePathLabel(ref: NavigationPageRef): string {
  return [ref.notebook, ref.section, ref.page].filter(Boolean).join(' / ')
}
