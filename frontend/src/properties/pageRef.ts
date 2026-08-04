// Relation-ref helpers for the page-link typeahead. The backend stores `page`
// values as a path-or-name string and `pages` as a list of the same; this module
// builds/normalizes those refs against a flat navigation list so the typeahead
// can offer real pages and flag dangling (deleted-target) values.
import type { NavigationCatalogItem } from '../lib/navigationCatalog'

/**
 * Build the canonical relation-ref the backend stores and validates. A ref with
 * a "/" is parsed by the backend as notebook / ...section / page; a bare name is
 * resolved by leaf. We always emit the full path form so a stored value is
 * unambiguous even when several pages share a leaf.
 */
export function toRef(notebook: string, section: string, page: string): string {
  const segs = [notebook, ...section.split('/').filter(Boolean), page].filter(
    Boolean
  )
  return segs.join('/')
}

/** O(1) lookup index over a flat navigation list. */
export interface NavIndex {
  byRef: Map<string, NavigationCatalogItem>
  byLeaf: Map<string, NavigationCatalogItem>
  refs: NavigationCatalogItem[]
}

export function indexNav(items: NavigationCatalogItem[]): NavIndex {
  const byRef = new Map<string, NavigationCatalogItem>()
  const byLeaf = new Map<string, NavigationCatalogItem>()
  for (const it of items) {
    const ref = toRef(it.notebook, it.section, it.page)
    byRef.set(ref, it)
    // First leaf wins — mirrors the backend's FindPageByLeaf resolver, so a
    // stored bare name resolves to the same page on both sides.
    if (!byLeaf.has(it.page)) byLeaf.set(it.page, it)
  }
  return { byRef, byLeaf, refs: items }
}

/**
 * Resolve a stored ref to a live nav item, or null when it points at a page the
 * index no longer knows (a dangling target the backend treats as inert).
 * Mirrors parseRelationRef: "/" → exact path; bare name → leaf lookup.
 */
export function resolveRef(
  ref: string,
  idx: NavIndex
): NavigationCatalogItem | null {
  const r = ref.trim()
  if (!r) return null
  if (r.includes('/')) return idx.byRef.get(r) ?? null
  return idx.byLeaf.get(r) ?? null
}
