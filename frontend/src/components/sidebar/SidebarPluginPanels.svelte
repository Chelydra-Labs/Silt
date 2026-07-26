<script lang="ts">
  // Primary-sidebar view switch (#321/#511). Routes the active view to its
  // sidebar surface: built-in tags/backlinks/settings panels, a plugin's
  // registered sidebarComponent, or — via the children snippet — the host's
  // notebook navigation tree. Mirrors PluginView's resolution for the main
  // view: read the live plugin entry, build ctx with the loader's session
  // token.
  //
  // Gating on loadedPlugins.loadersReady re-runs the ctx derivation after the
  // vault:closing clear→re-register cycle so getSessionToken captures the
  // fresh token, not an empty one captured mid-teardown (#326 item 5).
  import type { Snippet } from 'svelte'
  import TagSidebarPanel from '../TagSidebarPanel.svelte'
  import BacklinksSidebarPanel from '../BacklinksSidebarPanel.svelte'
  import SettingsNav from '../settings/SettingsNav.svelte'
  import type { PluginContext, PluginManifest } from '../../plugins/sdk'
  import {
    getPluginSidebar,
    pluginIdForView
  } from '../../plugins/getPluginSidebar'
  import { getSessionToken } from '../../plugins/loader'
  import { makePluginContext } from '../../plugins/context'
  import { loadedPlugins } from '../../plugins/store.svelte'

  interface Props {
    activeView: string
    activeNotebook: string
    activeSection: string
    activePage: string
    selectedTag?: string
    settingsSection?: string
    children: Snippet
  }

  let {
    activeView,
    activeNotebook,
    activeSection,
    activePage,
    selectedTag = $bindable(''),
    settingsSection = $bindable('general'),
    children
  }: Props = $props()

  let pluginSidebarEntry = $derived(getPluginSidebar(activeView))
  let SidebarCmp = $derived(pluginSidebarEntry?.sidebarComponent)
  let pluginSidebarCtx: PluginContext | null = $derived.by(() => {
    if (!loadedPlugins.loadersReady) return null
    const id = pluginIdForView(activeView)
    if (!id) return null
    return makePluginContext(id, getSessionToken(id))
  })
  let pluginSidebarManifest: PluginManifest | null = $derived(
    pluginSidebarEntry?.manifest ?? null
  )
</script>

{#if activeView === 'tags'}
  <TagSidebarPanel bind:selectedTag />
{:else if activeView === 'backlinks'}
  <BacklinksSidebarPanel
    notebook={activeNotebook}
    section={activeSection}
    page={activePage}
  />
{:else if activeView === 'settings'}
  <!-- Settings view: the sidebar IS the section nav (#511 rework). The
       matching panel lives in the content area (SettingsPanel). -->
  <SettingsNav bind:section={settingsSection} />
{:else if SidebarCmp && pluginSidebarCtx}
  <!-- Plugin-provided primary sidebar (#321). The active view's plugin owns
       the entire sidebar slot when it registers a sidebarComponent; the
       notebook selector + page tree are skipped because the plugin is
       responsible for any navigation affordance it wants to expose. -->
  <SidebarCmp ctx={pluginSidebarCtx} manifest={pluginSidebarManifest} />
{:else}
  {@render children?.()}
{/if}
