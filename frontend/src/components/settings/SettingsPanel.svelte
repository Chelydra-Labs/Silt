<script lang="ts">
  // The active settings section's content — the `role="tabpanel"` half of the
  // WAI-ARIA tabs pattern (#511 rework). Rendered in the main content area;
  // its matching `role="tablist"` (SettingsNav) lives in the sidebar. The two
  // are linked by stable ids, so aria-labelledby on this panel resolves to the
  // active tab in the sidebar even though they are separate components.
  import { onMount } from 'svelte'
  import GeneralTab from './GeneralTab.svelte'
  import EditorTab from './EditorTab.svelte'
  import HotkeysTab from './HotkeysTab.svelte'
  import AppearanceTab from './AppearanceTab.svelte'
  import AIProviderTab from './AIProviderTab.svelte'
  import AboutTab from './AboutTab.svelte'
  import PluginsTab from './PluginsTab.svelte'
  import DevTab from './DevTab.svelte'
  import PluginSettingsPanel from './PluginSettingsPanel.svelte'
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
</script>

<div class="flex-1 min-w-0 flex flex-col overflow-hidden">
  <div
    class="flex items-center justify-between px-6 py-4 border-b border-surface-sidebar-border flex-shrink-0"
  >
    <h2 class="font-headline-md text-headline-md text-text-primary capitalize">
      {sections.find((s) => s.id === section)?.label}
    </h2>
  </div>

  <!-- aria-labelledby resolves to the active tab in SettingsNav (sidebar)
       via the shared silt-settings-tab-<id> id. -->
  <div
    id="silt-settings-panel"
    role="tabpanel"
    aria-labelledby="silt-settings-tab-{section}"
    tabindex="0"
    class="flex-1 min-h-0 focus:outline-none"
    class:overflow-y-auto={['appearance', 'ai', 'plugins', 'about'].includes(
      section
    ) || section.startsWith('plugin:')}
    class:custom-scrollbar={['appearance', 'ai', 'plugins', 'about'].includes(
      section
    ) || section.startsWith('plugin:')}
    class:flex={['general', 'editor', 'hotkeys'].includes(section)}
    class:flex-col={['general', 'editor', 'hotkeys'].includes(section)}
    class:overflow-hidden={['general', 'editor', 'hotkeys'].includes(section)}
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
      <EditorTab />
    {:else if section === 'appearance'}
      <AppearanceTab />
    {:else if section === 'ai'}
      <AIProviderTab />
    {:else if section === 'hotkeys'}
      <HotkeysTab />
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
