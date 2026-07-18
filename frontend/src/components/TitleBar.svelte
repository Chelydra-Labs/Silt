<script lang="ts">
  import { onMount } from 'svelte'
  import logo from '../assets/logo.svg'
  import { settings } from '../settings/store.svelte'
  import { resolveHotkeyDisplay } from '../settings/hotkeys'
  import { shortcutBinding } from '../settings/shortcutActions'
  import { Window } from '@wailsio/runtime'
  import { RequestClose } from '../../bindings/silt/app.js'

  interface Props {
    sidebarCollapsed: boolean
    sidebarWidth?: number
    onSearchClick: () => void
    onSwitcherClick?: () => void
    onShortcutHelpClick?: () => void
    /** When set, show the unified AI toggle (an AI provider is available). */
    onAIClick?: () => void
    aiOpen?: boolean
    children?: import('svelte').Snippet
  }

  let {
    sidebarCollapsed = $bindable(),
    sidebarWidth = 256,
    onSearchClick,
    onSwitcherClick,
    onShortcutHelpClick,
    onAIClick,
    aiOpen = false,
    children
  }: Props = $props()

  let maximised = $state(false)

  // Platform detection (#61): on macOS, Wails auto-injects the native
  // traffic-light buttons, so we hide our in-app controls and reserve a
  // left inset for them. Detection via navigator.userAgent is safe (the
  // guard only activates on Mac; detection failure → show controls).
  let isMac = $state(false)

  async function syncMaximised() {
    try {
      maximised = await Window.IsMaximised()
    } catch {
      // runtime not ready (e.g. during SSR/check); leave as-is
    }
  }

  onMount(() => {
    syncMaximised()
    isMac = /mac/i.test(navigator.platform || navigator.userAgent)
    // Maximize/restore triggers a viewport resize; re-sync the icon then.
    const onResize = () => {
      syncMaximised()
    }
    window.addEventListener('resize', onResize)
    return () => window.removeEventListener('resize', onResize)
  })

  // macOS reserves 80px for traffic lights (collapsed or not); other platforms
  // use 48px. Hoisted so the inner ternary isn't evaluated twice.
  let trafficPx = $derived(isMac ? 80 : 48)
  let brandZoneWidth = $derived(
    sidebarCollapsed ? trafficPx : trafficPx + sidebarWidth
  )

  async function handleToggleMax() {
    await Window.ToggleMaximise()
    await syncMaximised()
  }
</script>

<header
  class="drag-region bg-surface-titlebar flex justify-between items-center h-12 w-full z-50 fixed top-0 border-b border-surface-titlebar-border select-none"
>
  <!-- Left: brand zone (matches sidebar width) + sidebar toggle at the boundary -->
  <div class="flex items-center min-w-0 h-full flex-grow">
    <!-- Brand strip aligns over the sidebar; collapses when sidebar does -->
    <div
      class="flex items-center gap-2 h-full flex-shrink-0 transition-all duration-200 ease-out overflow-hidden"
      style:width={brandZoneWidth + 'px'}
      style:padding-left={isMac && !sidebarCollapsed ? '80px' : undefined}
      class:px-4={!sidebarCollapsed && !isMac}
      class:px-3={sidebarCollapsed && !isMac}
    >
      {#if !isMac || !sidebarCollapsed}
        <div
          class="relative logo-container flex items-center gap-2 group cursor-pointer"
          class:justify-center={sidebarCollapsed}
          class:w-full={sidebarCollapsed}
        >
          <div
            class="relative logo-shimmer flex-shrink-0 w-6 h-6 rounded-md overflow-hidden"
          >
            <img
              src={logo}
              alt="Silt"
              class="w-full h-full logo-img transition-all duration-300"
            />
            <div
              class="absolute inset-0 logo-shimmer-sweep pointer-events-none"
            ></div>
          </div>
          {#if !sidebarCollapsed}
            <span
              class="font-headline-md text-headline-md text-surface-titlebar-text font-bold tracking-tight whitespace-nowrap group-hover:text-accent-primary-start transition-colors duration-300"
              >Silt</span
            >
          {/if}
        </div>
      {/if}
    </div>

    {#if children}
      <div class="h-full flex items-end flex-grow min-w-0">
        {@render children()}
      </div>
    {/if}
  </div>

  <!-- Right: standard search + unified AI drawer + window controls -->
  <div class="flex items-center gap-2 flex-shrink-0 h-full pr-2">
    {#if onSwitcherClick}
      <button
        type="button"
        onclick={onSwitcherClick}
        aria-label="Switch page"
        title={`Switch page${shortcutBinding('open_quick_switcher', settings.config?.hotkeys ?? {}) ? ` (${shortcutBinding('open_quick_switcher', settings.config?.hotkeys ?? {})})` : ''}`}
        class="flex items-center justify-center h-9 w-9 rounded-lg text-surface-titlebar-text-muted hover:text-surface-titlebar-text hover:bg-hover transition-colors cursor-pointer border-none bg-transparent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
      >
        <span class="material-symbols-outlined text-type-2xl" aria-hidden="true"
          >quick_reference_all</span
        >
      </button>
    {/if}
    {#if onShortcutHelpClick}
      <button
        type="button"
        onclick={onShortcutHelpClick}
        aria-label="Keyboard shortcuts"
        title={`Keyboard shortcuts${shortcutBinding('open_shortcuts_help', settings.config?.hotkeys ?? {}) ? ` (${shortcutBinding('open_shortcuts_help', settings.config?.hotkeys ?? {})})` : ''}`}
        class="flex items-center justify-center h-9 w-9 rounded-lg text-surface-titlebar-text-muted hover:text-surface-titlebar-text hover:bg-hover transition-colors cursor-pointer border-none bg-transparent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
      >
        <span class="material-symbols-outlined text-type-2xl" aria-hidden="true"
          >keyboard</span
        >
      </button>
    {/if}
    <button
      type="button"
      onclick={onSearchClick}
      aria-label="Search"
      title={`Search${(() => {
        const h = resolveHotkeyDisplay(
          'open_search',
          settings.config?.hotkeys ?? {}
        )
        return h ? ` (${h})` : ''
      })()}`}
      class="flex items-center justify-center h-9 w-9 rounded-lg text-surface-titlebar-text-muted hover:text-surface-titlebar-text hover:bg-hover transition-colors cursor-pointer border-none bg-transparent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
    >
      <span class="material-symbols-outlined text-type-2xl">search</span>
    </button>

    {#if onAIClick}
      <button
        type="button"
        onclick={onAIClick}
        aria-label="Silt AI"
        aria-expanded={aiOpen}
        aria-controls="silt-ai-drawer"
        title="Silt AI"
        class="flex items-center justify-center h-9 w-9 rounded-lg transition-colors cursor-pointer border-none bg-transparent focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        class:text-accent-primary-start={aiOpen}
        class:text-surface-titlebar-text-muted={!aiOpen}
        class:hover:text-surface-titlebar-text={!aiOpen}
        class:hover:bg-hover={true}
      >
        <span class="material-symbols-outlined text-type-2xl">auto_awesome</span
        >
      </button>
    {/if}

    <div class="w-px h-6 bg-surface-titlebar-border mx-1"></div>

    <!-- Window controls (hidden on macOS — Wails injects native traffic lights) -->
    {#if !isMac}
      <div class="flex items-center h-full">
        <button
          type="button"
          onclick={async () => {
            await Window.Minimise()
          }}
          aria-label="Minimize"
          title="Minimize"
          class="h-full w-11 flex items-center justify-center text-surface-titlebar-text-muted hover:text-surface-titlebar-text hover:bg-hover transition-colors border-none bg-transparent cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        >
          <span
            class="material-symbols-outlined text-icon-sm window-control-icon"
            >remove</span
          >
        </button>
        <button
          type="button"
          onclick={handleToggleMax}
          aria-label={maximised ? 'Restore' : 'Maximize'}
          title={maximised ? 'Restore' : 'Maximize'}
          class="h-full w-11 flex items-center justify-center text-surface-titlebar-text-muted hover:text-surface-titlebar-text hover:bg-hover transition-colors border-none bg-transparent cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        >
          <span
            class="material-symbols-outlined text-icon-sm window-control-icon"
            >{maximised ? 'fullscreen_exit' : 'crop_square'}</span
          >
        </button>
        <button
          type="button"
          onclick={async () => {
            await RequestClose()
          }}
          aria-label="Close"
          title="Close"
          class="h-full w-11 flex items-center justify-center text-surface-titlebar-text-muted hover:bg-error hover:text-white transition-colors border-none bg-transparent cursor-pointer focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        >
          <span
            class="material-symbols-outlined text-icon-sm window-control-icon"
            >close</span
          >
        </button>
      </div>
    {/if}
  </div>
</header>

<style>
  .window-control-icon {
    font-weight: 300;
  }
  .drag-region {
    --wails-draggable: drag;
  }
  /* Interactive children stay clickable while empty header space drags the window. */
  .drag-region :global(button),
  .drag-region :global(nav),
  .drag-region :global(input),
  .drag-region :global(a) {
    --wails-draggable: no-drag;
  }

  .logo-container:hover .logo-img {
    filter: drop-shadow(0 0 6px var(--color-accent-primary-start))
      brightness(1.1);
    transform: scale(1.05);
  }

  .logo-shimmer-sweep {
    background: linear-gradient(
      90deg,
      transparent,
      rgba(255, 255, 255, 0.25),
      transparent
    );
    left: -150%;
    width: 50%;
    height: 100%;
    transform: skewX(-20deg);
    transition: none;
  }

  .logo-container:hover .logo-shimmer-sweep {
    animation: logo-sweep 1.2s cubic-bezier(0.16, 1, 0.3, 1);
  }

  @keyframes logo-sweep {
    0% {
      left: -150%;
    }
    100% {
      left: 150%;
    }
  }
</style>
