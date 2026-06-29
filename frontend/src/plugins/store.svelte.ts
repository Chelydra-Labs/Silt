import type { LoadedPlugins } from './sdk'

// Reactive store of loaded plugins, shared between the loader and PluginView.
// Svelte 5 permits $state in .svelte.ts modules.
// `loadersReady` is false until the first loadPlugins completes, and is
// flipped back to false during vault:closing teardown so Sidebar/PluginView
// can suspend context construction against the clear→re-register race
// (#326 item 5).
export const loadedPlugins: LoadedPlugins = $state({
  plugins: new Map(),
  errors: [],
  loadersReady: false
})
