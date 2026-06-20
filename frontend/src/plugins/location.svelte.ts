/**
 * Module-scoped reactive location state for the plugin context (#69).
 *
 * This is the single source of truth for the active notebook/section/page
 * that plugins read via `ctx.activeNotebook` etc. It is a `$state` object
 * so plugins reading these values inside a Svelte reactive context (template,
 * `$derived`, `$effect`) automatically re-render when the user navigates.
 *
 * Plugins that read `ctx.active*` inside `init()` and cache the value see a
 * stale snapshot — that is an inherent limitation of destructuring. Plugins
 * that read `ctx.activeNotebook` at query time always see the live value.
 *
 * Each navigation also fans out an 'active-notebook:changed' event through the
 * plugin event bus (#106) so headless (non-reactive) plugins can react.
 */
import { dispatch } from './events'

let activeLocation = $state({
  notebook: '',
  section: '',
  page: ''
})

export function setActiveLocation(
  notebook: string,
  section: string,
  page: string
): void {
  const changed =
    activeLocation.notebook !== notebook ||
    activeLocation.section !== section ||
    activeLocation.page !== page
  activeLocation.notebook = notebook
  activeLocation.section = section
  activeLocation.page = page
  // Fan out the typed event so non-reactive (headless) plugins can react to
  // navigation (#106). Reactive plugins already track via the $state getters.
  if (changed) {
    dispatch('active-notebook:changed', { notebook, section, page })
  }
}

export function getActiveLocation() {
  return activeLocation
}
