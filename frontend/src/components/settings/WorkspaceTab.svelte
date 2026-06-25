<script lang="ts">
  import { settings, reloadFromBackend } from '../../settings/store.svelte'
  import VaultActionModal from './VaultActionModal.svelte'
  import VaultArchiveModal from './VaultArchiveModal.svelte'

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
</script>

<svelte:window onclick={handleWindowClick} />

{#if !settings.config}
  <div class="p-8 text-text-muted font-body-md">No configuration loaded.</div>
{:else}
  <div class="flex-1 overflow-y-auto p-6 space-y-6 custom-scrollbar h-full">
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
    <section class="max-w-xl">
      <h3
        class="font-label-sm-bold text-text-muted uppercase tracking-widest text-[10px] mb-3"
      >
        Workspace
      </h3>
      <div
        class="flex items-center gap-2 bg-surface border border-border-muted rounded-lg px-3 py-2.5"
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
              class="absolute right-0 top-full mt-1 z-10 w-44 bg-panel border border-border-zinc rounded-lg shadow-xl py-1"
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
              <div class="my-1 border-t border-border-muted"></div>
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
            </div>
          {/if}
        </div>
      </div>
      <p class="text-text-muted text-[11px] font-label-sm mt-1.5">
        Move, copy, back up, or migrate this workspace from the actions menu.
      </p>
    </section>
  </div>
{/if}

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
