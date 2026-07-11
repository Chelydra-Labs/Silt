<script lang="ts">
  import { onMount } from 'svelte'
  import { GetCloseToTray, SetCloseToTray } from '../../../bindings/silt/app.js'
  import { settings, reloadFromBackend } from '../../settings/store.svelte'
  import { themeState } from '../../theme/store.svelte'
  import { loadedPlugins } from '../../plugins/store.svelte'
  import { aiProviderNeedsSetup } from '../../settings/ai-setup'
  import VaultActionModal from './VaultActionModal.svelte'
  import VaultArchiveModal from './VaultArchiveModal.svelte'

  // Close-to-tray is a user-global window behaviour (#501). Its state lives on
  // the tab rather than in the vault-scoped config store — the window exists
  // before any vault opens, so this section renders regardless of config load.
  let closeToTray = $state(false)
  let closeToTrayInflight = $state(false)
  let closeToTrayError = $state('')

  onMount(async () => {
    // Hydrate the toggle from the persisted preference. If the user flipped
    // it before this resolved (closeToTrayInflight), their explicit choice
    // wins — don't clobber it with the stale on-disk value. Default-off on
    // any read failure matches the backend's nil-pointer semantics.
    try {
      const v = await GetCloseToTray()
      if (!closeToTrayInflight) closeToTray = !!v
    } catch {
      closeToTray = false
    }
  })

  // Optimistic flip + revert-on-failure. The inflight guard stops a second
  // rapid click from racing the pending write and leaving the toggle out of
  // sync with disk.
  async function setCloseToTray(on: boolean) {
    if (closeToTrayInflight) return
    closeToTrayInflight = true
    closeToTrayError = ''
    closeToTray = on
    try {
      await SetCloseToTray(on)
    } catch (e) {
      closeToTray = !on
      closeToTrayError = 'Could not save the close-to-tray preference.'
      console.error('SetCloseToTray failed:', e)
    } finally {
      closeToTrayInflight = false
    }
  }

  // Workspace relocation + portable-archive menu (#141, #143).
  // Provides options to move, copy, export, or import the vault.
  let vaultMenuOpen = $state(false)
  let vaultAction = $state<'move' | 'copy' | 'export' | 'import' | null>(null)
  let menuItemRefs: HTMLButtonElement[] = $state([])
  let menuWrapper = $state<HTMLDivElement | null>(null)
  let triggerBtn = $state<HTMLButtonElement | null>(null)

  function toggleMenu() {
    vaultMenuOpen = !vaultMenuOpen
  }

  function openAction(action: 'move' | 'copy' | 'export' | 'import') {
    vaultAction = action
    vaultMenuOpen = false
  }

  function handleWindowClick(e: MouseEvent) {
    if (
      vaultMenuOpen &&
      menuWrapper &&
      !menuWrapper.contains(e.target as Node)
    ) {
      vaultMenuOpen = false
    }
  }

  function handleMenuTriggerKeydown(e: KeyboardEvent) {
    if (e.key === 'ArrowDown' || e.key === 'Enter' || e.key === ' ') {
      e.preventDefault()
      vaultMenuOpen = true
      queueMicrotask(() => menuItemRefs[0]?.focus())
    }
  }

  function handleMenuItemKeydown(e: KeyboardEvent, index: number) {
    const items = menuItemRefs
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      items[(index + 1) % items.length]?.focus()
    } else if (e.key === 'ArrowUp') {
      e.preventDefault()
      items[(index - 1 + items.length) % items.length]?.focus()
    } else if (e.key === 'Home') {
      e.preventDefault()
      items[0]?.focus()
    } else if (e.key === 'End') {
      e.preventDefault()
      items[items.length - 1]?.focus()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      vaultMenuOpen = false
      triggerBtn?.focus()
    }
  }

  // Landing summary strip ("home base"): active vault, theme/mode, AI status,
  // and plugin count. Each chip jumps to its section via the shared
  // silt:settings-jump event (handled by App.svelte → settingsSection).
  function jumpTo(sectionId: string) {
    window.dispatchEvent(
      new CustomEvent('silt:settings-jump', { detail: { section: sectionId } })
    )
  }

  // Theme name for the summary chip. Falls back to the id when the human
  // name hasn't loaded yet.
  let themeName = $derived(themeState.name || themeState.id || '—')
  let pluginCount = $derived(loadedPlugins.plugins.size)
  // AI needs-setup mirrors the AI Provider + Plugins tabs' predicate so the
  // chip reflects the same "configure me" state those pages surface.
  let aiNeedsSetup = $derived(
    settings.config ? aiProviderNeedsSetup(settings.config.ai?.chat) : true
  )
</script>

<svelte:window onclick={handleWindowClick} />

<div
  class="flex-1 overflow-y-auto p-6 space-y-6 custom-scrollbar h-full max-w-4xl mx-auto w-full"
>
  <!-- Landing summary strip: one-line "home base" with clickable chips that
       jump to each respective section. Gives the page an at-a-glance status
       without duplicating the controls those sections own. -->
  <div
    class="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-2.5"
    aria-label="Workspace summary"
  >
    <button
      type="button"
      onclick={() =>
        document
          .getElementById('general-workspace')
          ?.scrollIntoView({ block: 'start', behavior: 'smooth' })}
      class="flex items-center gap-2.5 px-3 py-2.5 rounded-lg bg-surface-panel/40 border border-surface-panel-border text-left transition-colors hover:border-border-active hover:bg-hover cursor-pointer"
    >
      <span class="material-symbols-outlined text-text-muted text-[18px]"
        >folder</span
      >
      <span class="min-w-0 flex-1">
        <span
          class="block text-[9px] uppercase tracking-widest text-text-muted font-label-sm-bold"
          >Vault</span
        >
        <span
          class="block text-[12px] text-text-primary font-body-md truncate"
          title={settings.config?.notebooks.path || 'No workspace'}
        >
          {settings.config?.notebooks.path
            ? settings.config.notebooks.path.replace(/.*[/\\]/, '')
            : 'No workspace'}
        </span>
      </span>
    </button>

    <button
      type="button"
      onclick={() => jumpTo('appearance')}
      class="flex items-center gap-2.5 px-3 py-2.5 rounded-lg bg-surface-panel/40 border border-surface-panel-border text-left transition-colors hover:border-border-active hover:bg-hover cursor-pointer"
    >
      <span class="material-symbols-outlined text-text-muted text-[18px]"
        >{themeState.mode === 'dark'
          ? 'dark_mode'
          : themeState.mode === 'light'
            ? 'light_mode'
            : 'desktop_windows'}</span
      >
      <span class="min-w-0 flex-1">
        <span
          class="block text-[9px] uppercase tracking-widest text-text-muted font-label-sm-bold"
          >Theme</span
        >
        <span class="block text-[12px] text-text-primary font-body-md truncate">
          {themeName}
        </span>
      </span>
    </button>

    <button
      type="button"
      onclick={() => jumpTo('ai')}
      class="flex items-center gap-2.5 px-3 py-2.5 rounded-lg bg-surface-panel/40 border border-surface-panel-border text-left transition-colors hover:border-border-active hover:bg-hover cursor-pointer"
    >
      <span
        class="material-symbols-outlined text-[18px] {aiNeedsSetup
          ? 'text-status-warn'
          : 'text-accent-primary-start'}">smart_toy</span
      >
      <span class="min-w-0 flex-1">
        <span
          class="block text-[9px] uppercase tracking-widest text-text-muted font-label-sm-bold"
          >AI</span
        >
        <span class="block text-[12px] text-text-primary font-body-md truncate">
          {aiNeedsSetup ? 'Setup needed' : 'Configured'}
        </span>
      </span>
    </button>

    <button
      type="button"
      onclick={() => jumpTo('plugins')}
      class="flex items-center gap-2.5 px-3 py-2.5 rounded-lg bg-surface-panel/40 border border-surface-panel-border text-left transition-colors hover:border-border-active hover:bg-hover cursor-pointer"
    >
      <span class="material-symbols-outlined text-text-muted text-[18px]"
        >extension</span
      >
      <span class="min-w-0 flex-1">
        <span
          class="block text-[9px] uppercase tracking-widest text-text-muted font-label-sm-bold"
          >Plugins</span
        >
        <span class="block text-[12px] text-text-primary font-body-md truncate">
          {pluginCount}
          {pluginCount === 1 ? 'plugin' : 'plugins'}
        </span>
      </span>
    </button>
  </div>

  <!-- Window: user-global, renders regardless of vault config. -->
  <section>
    <h3
      class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
    >
      Window
    </h3>
    <div
      class="bg-surface-panel border border-surface-panel-border rounded-lg px-4 py-3 space-y-2"
    >
      <label
        class="flex items-center justify-between gap-3 {closeToTrayInflight
          ? ''
          : 'cursor-pointer'}"
      >
        <span class="text-text-primary text-[12px] font-body-md">
          Close to tray
        </span>
        <button
          type="button"
          role="switch"
          aria-checked={closeToTray}
          aria-label="Close to tray"
          disabled={closeToTrayInflight}
          onclick={() => setCloseToTray(!closeToTray)}
          class="relative w-9 h-5 rounded-full transition-colors border-none {closeToTrayInflight
            ? 'cursor-wait opacity-60'
            : 'cursor-pointer'} {closeToTray
            ? 'bg-accent-primary-start'
            : 'bg-surface-panel-border'}"
        >
          <span
            class="absolute top-0.5 left-0.5 w-4 h-4 rounded-full bg-surface-panel transition-transform {closeToTray
              ? 'translate-x-4'
              : ''}"
          ></span>
        </button>
      </label>
      <p class="text-text-muted text-[11px] font-label-sm leading-relaxed">
        Closing the window hides Silt to the tray instead of quitting. Use Quit
        in the tray menu to exit.
      </p>
      {#if closeToTrayError}
        <p
          class="text-status-danger text-[12px] font-body-md flex items-center gap-1.5"
          role="alert"
        >
          <span class="material-symbols-outlined text-[16px]">error</span>
          {closeToTrayError}
        </p>
      {/if}
    </div>
  </section>

  <!-- Workspace: vault-scoped. Unavailable until config loads. -->
  {#if settings.config}
    <!-- External update notice -->
    {#if settings.pendingExternal}
      <div
        class="flex items-start gap-2 p-3 rounded-lg bg-accent-primary-start/10 border border-accent-primary-start/30 text-accent-primary-start text-[12px] font-body-md"
      >
        <span class="material-symbols-outlined text-[18px]">sync</span>
        <span class="flex-1"> Settings were updated externally. </span>
        <button
          onclick={async () => {
            settings.dirty = false
            await reloadFromBackend()
          }}
          class="font-label-sm-bold underline hover:brightness-110 bg-transparent border-none cursor-pointer text-accent-primary-start"
        >
          Reload
        </button>
      </div>
    {/if}

    <!-- Vault path + relocate menu -->
    <section id="general-workspace">
      <h3
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Workspace
      </h3>
      <div
        class="flex items-center gap-2 bg-surface-panel border border-surface-panel-border rounded-lg px-3 py-2.5"
      >
        <span class="material-symbols-outlined text-text-muted text-[18px]"
          >folder</span
        >
        <span
          class="text-text-primary text-[13px] font-body-md truncate flex-1"
          title={settings.config.notebooks.path || ''}
        >
          {settings.config.notebooks.path || '—'}
        </span>
        <div class="relative" bind:this={menuWrapper}>
          <button
            type="button"
            bind:this={triggerBtn}
            onclick={toggleMenu}
            onkeydown={handleMenuTriggerKeydown}
            aria-haspopup="menu"
            aria-expanded={vaultMenuOpen}
            aria-label="Vault actions"
            title="Vault actions"
            class="flex-shrink-0 p-1 rounded-md text-text-muted hover:text-text-primary hover:bg-hover border-none bg-transparent cursor-pointer transition-colors"
          >
            <span class="material-symbols-outlined text-[20px]">more_vert</span>
          </button>
          {#if vaultMenuOpen}
            <div
              role="menu"
              aria-label="Vault actions"
              class="absolute right-0 top-full mt-1 z-10 w-44 bg-surface-popover border border-surface-popover-border rounded-lg shadow-xl py-1"
            >
              <button
                type="button"
                bind:this={menuItemRefs[0]}
                role="menuitem"
                onclick={() => openAction('move')}
                onkeydown={(e) => handleMenuItemKeydown(e, 0)}
                class="flex items-center gap-2.5 w-full text-left px-3 py-2 text-text-primary text-[12px] font-body-md hover:bg-hover border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-[18px] text-text-muted"
                  >drive_file_move</span
                >
                Move vault…
              </button>
              <button
                type="button"
                bind:this={menuItemRefs[1]}
                role="menuitem"
                onclick={() => openAction('copy')}
                onkeydown={(e) => handleMenuItemKeydown(e, 1)}
                class="flex items-center gap-2.5 w-full text-left px-3 py-2 text-text-primary text-[12px] font-body-md hover:bg-hover border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-[18px] text-text-muted"
                  >content_copy</span
                >
                Copy vault…
              </button>
              <div class="my-1 border-t border-surface-popover-border"></div>
              <button
                type="button"
                bind:this={menuItemRefs[2]}
                role="menuitem"
                onclick={() => openAction('export')}
                onkeydown={(e) => handleMenuItemKeydown(e, 2)}
                class="flex items-center gap-2.5 w-full text-left px-3 py-2 text-text-primary text-[12px] font-body-md hover:bg-hover border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-[18px] text-text-muted"
                  >archive</span
                >
                Export vault…
              </button>
              <button
                type="button"
                bind:this={menuItemRefs[3]}
                role="menuitem"
                onclick={() => openAction('import')}
                onkeydown={(e) => handleMenuItemKeydown(e, 3)}
                class="flex items-center gap-2.5 w-full text-left px-3 py-2 text-text-primary text-[12px] font-body-md hover:bg-hover border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-[18px] text-text-muted"
                  >unarchive</span
                >
                Import vault…
              </button>
              <div class="my-1 border-t border-surface-popover-border"></div>
              <button
                type="button"
                bind:this={menuItemRefs[4]}
                role="menuitem"
                onclick={() => {
                  vaultMenuOpen = false
                  // Tear down this vault and return to the onboarding screen so
                  // the user can pick a different workspace folder. App.svelte
                  // owns the CloseVault flow and closes this settings overlay.
                  window.dispatchEvent(new CustomEvent('silt:change-vault'))
                }}
                onkeydown={(e) => handleMenuItemKeydown(e, 4)}
                class="flex items-center gap-2.5 w-full text-left px-3 py-2 text-text-primary text-[12px] font-body-md hover:bg-hover border-none bg-transparent cursor-pointer"
              >
                <span
                  class="material-symbols-outlined text-[18px] text-text-muted"
                  >swap_horiz</span
                >
                Switch vault…
              </button>
            </div>
          {/if}
        </div>
      </div>
      <p class="text-text-muted text-[11px] font-label-sm mt-1.5">
        Move, copy, back up, or migrate this workspace from the actions menu.
      </p>
    </section>
  {:else if settings.loading}
    <section>
      <h3
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Workspace
      </h3>
      <p class="text-text-muted text-[13px] font-body-md animate-pulse">
        Loading workspace…
      </p>
    </section>
  {:else if settings.error}
    <!-- Config failed to load: surface the backend's actual error so the
         user can act on it (e.g. malformed YAML) rather than the generic
         no-workspace copy. -->
    <section>
      <h3
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Workspace
      </h3>
      <div
        class="flex items-start gap-2 p-3 rounded-lg bg-surface-panel border border-surface-panel-border text-status-danger text-[12px] font-body-md"
        role="alert"
      >
        <span class="material-symbols-outlined text-[18px]">error</span>
        <span class="flex-1">
          Couldn't load workspace settings: {settings.error}
        </span>
      </div>
    </section>
  {:else}
    <section>
      <h3
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Workspace
      </h3>
      <div
        class="flex items-start gap-2 p-3 rounded-lg bg-surface-panel border border-surface-panel-border text-text-muted text-[12px] font-body-md"
      >
        <span class="material-symbols-outlined text-[18px]">folder_off</span>
        <span class="flex-1">
          No workspace configuration loaded. Open a vault to manage its path.
        </span>
      </div>
    </section>
  {/if}
</div>

{#if vaultAction}
  {#if vaultAction === 'move' || vaultAction === 'copy'}
    <VaultActionModal
      mode={vaultAction}
      currentPath={settings.config?.notebooks.path || ''}
      onClose={() => (vaultAction = null)}
    />
  {:else}
    <VaultArchiveModal
      mode={vaultAction}
      currentPath={settings.config?.notebooks.path || ''}
      onClose={() => (vaultAction = null)}
    />
  {/if}
{/if}
