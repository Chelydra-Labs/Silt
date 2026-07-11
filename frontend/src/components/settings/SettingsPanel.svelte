<script lang="ts">
  // The active settings section's content — the `role="tabpanel"` half of the
  // WAI-ARIA tabs pattern (#511 rework). Rendered in the main content area;
  // its matching `role="tablist"` (SettingsNav) lives in the sidebar. The two
  // are linked by stable ids, so aria-labelledby on this panel resolves to the
  // active tab in the sidebar even though they are separate components.
  //
  // The panel owns the shared section header (title + one-line description)
  // and the settings search box, so every section reads as one designed
  // surface. Content width is set by each tab's own root container
  // (form-style tabs center at max-w-4xl; grid/list tabs use max-w-6xl).
  import { onMount, onDestroy, tick } from 'svelte'
  import GeneralTab from './GeneralTab.svelte'
  import EditorTab from './EditorTab.svelte'
  import HotkeysTab from './HotkeysTab.svelte'
  import AppearanceTab from './AppearanceTab.svelte'
  import AIProviderTab from './AIProviderTab.svelte'
  import AboutTab from './AboutTab.svelte'
  import PluginsTab from './PluginsTab.svelte'
  import DevTab from './DevTab.svelte'
  import PluginSettingsPanel from './PluginSettingsPanel.svelte'
  import SettingsSearch from './SettingsSearch.svelte'
  import { loadConfig, settings } from '../../settings/store.svelte'
  import { loadPlugins } from '../../plugins/loader'
  import { getSettingsSections } from './settingsSections.svelte'

  interface Props {
    section?: string
    activeNotebook: string
    activeSection: string
    activePage: string
  }

  let {
    section = $bindable('general'),
    activeNotebook,
    activeSection,
    activePage
  }: Props = $props()

  let sections = $derived(getSettingsSections())
  let activeSectionMeta = $derived(
    sections.find((s) => s.id === section) ?? null
  )

  // Anchor-ring transient state: when search jumps to an anchorId, we briefly
  // add an accent ring to that element to confirm the landing. Cleared on a
  // timer so the affordance fades.
  let ringAnchor = $state<string | null>(null)
  let ringTimer: ReturnType<typeof setTimeout> | null = null

  onMount(() => {
    loadConfig().catch((e) => console.error('loadConfig failed:', e))
  })

  // Refresh the plugin registry when entering the Plugins tab so
  // installs/enables done elsewhere are reflected without a restart.
  let lastSection = ''
  $effect(() => {
    if (section === 'plugins' && lastSection !== 'plugins') {
      loadPlugins(activeNotebook, activeSection, activePage).catch((e) =>
        console.error('Plugin reload failed:', e)
      )
    }
    lastSection = section
  })

  // If the active plugin section disappears (plugin disabled/uninstalled or
  // its surface unregistered while its tab is open), fall back to Plugins so
  // the panel is never blank with an orphaned section id.
  $effect(() => {
    if (
      section.startsWith('plugin:') &&
      !sections.some((s) => s.id === section)
    ) {
      section = 'plugins'
    }
  })

  // Search jump: switch section, then scroll to + ring the anchor (if any).
  // Waits a tick so the target tab is mounted before we look up its element.
  async function handleJump(sectionId: string, anchorId?: string) {
    section = sectionId
    if (!anchorId) return
    await tick()
    const el = document.getElementById(anchorId)
    if (!el) return
    el.scrollIntoView({ block: 'center', behavior: 'smooth' })
    if (ringTimer) clearTimeout(ringTimer)
    ringAnchor = anchorId
    ringTimer = setTimeout(() => {
      ringAnchor = null
    }, 1600)
  }

  // Clear the anchor-ring timer on teardown so it can't write ringAnchor
  // ($state) after the component has unmounted.
  onDestroy(() => {
    if (ringTimer) clearTimeout(ringTimer)
  })
</script>

<div class="flex-1 min-w-0 flex flex-col overflow-hidden">
  <!-- Shared section header: title + one-line description + search. This
       replaces the bare <h2> so every section has a consistent home base. -->
  <div
    class="flex items-center justify-between gap-4 px-6 py-4 border-b border-surface-panel-border flex-shrink-0"
  >
    <div class="min-w-0">
      <h2
        class="font-headline-md text-headline-md text-text-primary capitalize truncate"
      >
        {activeSectionMeta?.label ?? section}
      </h2>
      {#if activeSectionMeta?.description}
        <p class="text-text-muted text-[12px] font-body-md mt-0.5 truncate">
          {activeSectionMeta.description}
        </p>
      {/if}
    </div>
    <SettingsSearch onJump={handleJump} />
  </div>

  <!-- aria-labelledby resolves to the active tab in SettingsNav (sidebar)
       via the shared silt-settings-tab-<id> id. Form-style tabs (editor,
       hotkeys) own their scroll + fixed footer, so the panel is a non-scroll
       flex column for them; everything else scrolls here. -->
  <div
    id="silt-settings-panel"
    role="tabpanel"
    aria-labelledby="silt-settings-tab-{section}"
    tabindex="0"
    class="flex-1 min-h-0 focus:outline-none custom-scrollbar"
    class:flex={['editor', 'hotkeys'].includes(section)}
    class:flex-col={['editor', 'hotkeys'].includes(section)}
    class:overflow-hidden={['editor', 'hotkeys'].includes(section)}
    class:overflow-y-auto={!['editor', 'hotkeys'].includes(section)}
  >
    {#if settings.loading && !settings.config && section !== 'general'}
      <div class="p-8 text-text-muted animate-pulse font-body-md">
        Loading settings…
      </div>
    {:else if !settings.config && settings.error && section !== 'general'}
      <div class="p-8">
        <div
          class="flex items-start gap-2 p-3 rounded-lg bg-error/10 border border-error/30 text-error text-[12px] font-body-md max-w-xl"
        >
          <span class="material-symbols-outlined text-[18px]">error</span>
          <span class="flex-1">{settings.error}</span>
        </div>
      </div>
    {:else if section === 'general'}
      <GeneralTab />
    {:else if section === 'editor'}
      <EditorTab {ringAnchor} />
    {:else if section === 'appearance'}
      <AppearanceTab />
    {:else if section === 'ai'}
      <AIProviderTab />
    {:else if section === 'hotkeys'}
      <HotkeysTab {ringAnchor} />
    {:else if section === 'plugins'}
      <PluginsTab
        {activeNotebook}
        {activeSection}
        {activePage}
        onSwitchTab={(id) => (section = id)}
      />
    {:else if section.startsWith('plugin:')}
      {@const pluginSection = sections.find((s) => s.id === section)}
      {#if pluginSection?.plugin}
        <PluginSettingsPanel
          plugin={pluginSection.plugin}
          {activeNotebook}
          {activeSection}
          {activePage}
        />
      {/if}
    {:else if section === 'dev'}
      <DevTab />
    {:else if section === 'about'}
      <AboutTab />
    {/if}
  </div>
</div>
