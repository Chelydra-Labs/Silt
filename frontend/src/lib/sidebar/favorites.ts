import {
  GetNavigationPreferences,
  SetFavoritePage
} from '../../../bindings/silt/app.js'
import type { NavigationPageRef } from './types'

/**
 * Single favorites write path (#1120), shared by the sidebar's nav loader
 * and the page-locator menus on the type dashboards. The write goes through
 * the same SetFavoritePage IPC the nav loader has always used, then the
 * window broadcast lets every surface holding navigation-preferences state
 * (sidebar nav loader / Quick Access, dashboard favorite mirrors) reload —
 * the same resync signal autosave's recents bump dispatches.
 */
export async function persistFavorite(
  ref: Pick<NavigationPageRef, 'notebook' | 'section' | 'page'>,
  favorite: boolean
): Promise<void> {
  await SetFavoritePage(ref.notebook, ref.section, ref.page, favorite)
  window.dispatchEvent(new CustomEvent('navigation-preferences-changed'))
}

/**
 * Narrow favorites read for surfaces that only need the pinned set (the
 * dashboard menus' Pin/Unpin label). The nav loader keeps reading the full
 * preferences document for its own state.
 */
export async function fetchFavoritePages(): Promise<NavigationPageRef[]> {
  const prefs = await GetNavigationPreferences()
  return prefs?.favorites ?? []
}
