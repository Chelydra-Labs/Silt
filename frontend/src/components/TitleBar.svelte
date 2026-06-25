<script lang="ts">
  import { onMount } from 'svelte'
  import logo from '../assets/logo.svg'
  import {
    WindowMinimise,
    WindowToggleMaximise,
    WindowIsMaximised,
    Quit
  } from '../../wailsjs/runtime/runtime.js'

  interface Props {
    activeView: string
    sidebarCollapsed: boolean
    sidebarWidth?: number
    onSearchClick: () => void
    onOpenSettings: (tab?: string) => void
  }

  let {
    activeView = $bindable(),
    sidebarCollapsed = $bindable(),
    sidebarWidth = 256,
    onSearchClick,
    onOpenSettings
  }: Props = $props()

  const views: { id: string; label: string; icon: string }[] = [
    { id: 'notes', label: 'Notes', icon: 'description' },
    { id: 'agenda', label: 'Agenda', icon: 'event_repeat' },
    { id: 'tags', label: 'Tags', icon: 'label' },
    { id: 'calendar', label: 'Calendar', icon: 'calendar_month' }
    // Kanban returns when a first-party Kanban plugin ships.
  ]

  let maximised = $state(false)

  // Platform detection (#61): on macOS, Wails auto-injects the native
  // traffic-light buttons, so we hide our in-app controls and reserve a
  // left inset for them. Detection via navigator.userAgent is safe (the
  // guard only activates on Mac; detection failure → show controls).
  let isMac = $state(false)

  async function syncMaximised() {
    try {
      maximised = await WindowIsMaximised()
    } catch {
      // runtime not ready (e.g. during SSR/check); leave as-is
    }
  }

  let viewButtons = $state<Record<string, HTMLButtonElement>>({})
  let pillLeft = $state(0)
  let pillWidth = $state(0)
  let pillOpacity = $state(0)

  function updatePill() {
    const el = viewButtons[activeView]
    if (el) {
      const parent = el.parentElement
      if (parent) {
        const parentRect = parent.getBoundingClientRect()
        const rect = el.getBoundingClientRect()
        pillLeft = rect.left - parentRect.left
        pillWidth = rect.width
        pillOpacity = 1
      }
    } else {
      pillOpacity = 0
    }
  }

  // Update pill whenever activeView changes
  $effect(() => {
    activeView
    // Wait for the DOM to update so button dimensions are correct
    setTimeout(updatePill, 0)
  })

  onMount(() => {
    syncMaximised()
    isMac = /mac/i.test(navigator.platform || navigator.userAgent)
    // Maximize/restore triggers a viewport resize; re-sync the icon then.
    const onResize = () => {
      syncMaximised()
      updatePill()
    }
    window.addEventListener('resize', onResize)
    // Initial sync
    setTimeout(updatePill, 100)
    return () => window.removeEventListener('resize', onResize)
  })

  function selectView(id: string) {
    activeView = id
  }

  function handleToggleMax() {
    WindowToggleMaximise()
    // Optimistic flip; resize listener will correct it if the platform
    // refuses the toggle.
    maximised = !maximised
  }
</script>

<header
  class="drag-region bg-void flex justify-between items-center h-14 w-full z-50 fixed top-0 border-b border-border-muted select-none"
>
  <!-- Left: brand zone (matches sidebar width) + sidebar toggle at the boundary -->
  <div class="flex items-center min-w-0 h-full">
    <!-- Brand strip aligns over the sidebar; collapses when sidebar does -->
    <div
      class="flex items-center gap-2 h-full flex-shrink-0 transition-all duration-200 ease-out overflow-hidden"
      style:width={sidebarCollapsed ? '0px' : sidebarWidth + 'px'}
      style:padding-left={isMac && !sidebarCollapsed ? '80px' : undefined}
      class:px-4={!sidebarCollapsed && !isMac}
      class:px-3={sidebarCollapsed}
    >
      <div
        class="relative logo-container flex items-center gap-2 group cursor-pointer"
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
        <span
          class="font-headline-md text-headline-md text-text-primary font-bold tracking-tight whitespace-nowrap group-hover:text-accent-primary-start transition-colors duration-300"
          >Silt</span
        >
      </div>
    </div>

    <div class="w-px h-6 bg-border-muted mx-1 flex-shrink-0"></div>

    <!-- View switcher (segmented control) -->
    <nav
      class="flex items-center gap-0.5 p-0.5 bg-panel border border-border-muted rounded-lg relative min-w-0 mx-2"
    >
      <!-- Sliding pill indicator -->
      <div
        class="absolute top-0.5 bottom-0.5 bg-hover rounded-md transition-all duration-200 ease-[var(--transition-standard)] pointer-events-none"
        style:left="{pillLeft}px"
        style:width="{pillWidth}px"
        style:opacity={pillOpacity}
      ></div>

      {#each views as v (v.id)}
        <button
          bind:this={viewButtons[v.id]}
          onclick={() => selectView(v.id)}
          class="relative flex items-center gap-1.5 px-3 py-1 rounded-md font-label-sm text-label-sm transition-colors border-none bg-transparent cursor-pointer focus:outline-none whitespace-nowrap z-10"
          class:text-accent-primary-start={activeView === v.id}
          class:text-text-muted={activeView !== v.id}
          aria-pressed={activeView === v.id}
        >
          <span class="material-symbols-outlined text-[18px]">{v.icon}</span>
          <span class="hidden lg:inline">{v.label}</span>
        </button>
      {/each}
    </nav>
  </div>

  <!-- Right: search + window controls -->
  <div class="flex items-center gap-2 flex-shrink-0 h-full pr-2">
    <button
      onclick={onSearchClick}
      class="bg-surface border border-border-muted rounded-lg pl-3 pr-8 py-1.5 items-center gap-2 cursor-pointer text-text-muted hover:border-accent-primary-start transition-all duration-200 hidden sm:flex w-72"
    >
      <span class="material-symbols-outlined text-[18px]">search</span>
      <span class="text-[12px] font-label-sm whitespace-nowrap"
        >Search… (Ctrl+P)</span
      >
    </button>

    <div class="w-px h-6 bg-border-muted mx-1"></div>

    <!-- Settings + plugin manager shortcuts (open the settings shell) -->
    <button
      onclick={() => onOpenSettings('plugins')}
      aria-label="Plugin manager"
      title="Plugin manager"
      class="h-9 w-9 flex items-center justify-center text-text-muted hover:text-accent-primary-start transition-colors border-none bg-transparent cursor-pointer focus:outline-none rounded-md hover:bg-hover"
    >
      <span class="material-symbols-outlined text-[20px]">extension</span>
    </button>
    <button
      onclick={() => onOpenSettings('general')}
      aria-label="Settings"
      title="Settings"
      class="h-9 w-9 flex items-center justify-center text-text-muted hover:text-accent-primary-start transition-colors border-none bg-transparent cursor-pointer focus:outline-none rounded-md hover:bg-hover"
    >
      <span class="material-symbols-outlined text-[20px]">settings</span>
    </button>

    <div class="w-px h-6 bg-border-muted mx-1"></div>

    <!-- Window controls (hidden on macOS — Wails injects native traffic lights) -->
    {#if !isMac}
      <div class="flex items-center h-full">
        <button
          onclick={() => WindowMinimise()}
          aria-label="Minimize"
          title="Minimize"
          class="h-full w-11 flex items-center justify-center text-text-muted hover:text-text-primary hover:bg-hover transition-colors border-none bg-transparent cursor-pointer focus:outline-none"
        >
          <span class="material-symbols-outlined text-[18px]">remove</span>
        </button>
        <button
          onclick={handleToggleMax}
          aria-label={maximised ? 'Restore' : 'Maximize'}
          title={maximised ? 'Restore' : 'Maximize'}
          class="h-full w-11 flex items-center justify-center text-text-muted hover:text-text-primary hover:bg-hover transition-colors border-none bg-transparent cursor-pointer focus:outline-none"
        >
          <span class="material-symbols-outlined text-[18px]"
            >{maximised ? 'fullscreen_exit' : 'crop_square'}</span
          >
        </button>
        <button
          onclick={() => Quit()}
          aria-label="Close"
          title="Close"
          class="h-full w-11 flex items-center justify-center text-text-muted hover:bg-error hover:text-white transition-colors border-none bg-transparent cursor-pointer focus:outline-none"
        >
          <span class="material-symbols-outlined text-[18px]">close</span>
        </button>
      </div>
    {/if}
  </div>
</header>

<style>
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
