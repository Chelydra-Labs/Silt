<script lang="ts">
  // Inline meta strip — the glanceability layer that lives in the page-header
  // (breadcrumb) row. Renders NOTHING for an untyped page (the majority case —
  // no visual noise), a subdued chip for an unrecognized `type:` ref, and the
  // type chip + hero value for a properly typed page. The chip body opens the
  // bottom properties panel; the caret opens a small menu with a "View all"
  // action that jumps to the per-type dashboard.
  import type { PageTypeInfo } from './types'

  interface Props {
    info: PageTypeInfo
    heroValue: string
    onOpen: () => void
    onViewAll?: () => void
  }

  let { info, heroValue, onOpen, onViewAll }: Props = $props()

  // Untyped + no raw ref → render nothing at all (clean page-header invariant).
  let show = $derived(info.isSet || info.rawType.length > 0)
  let label = $derived(
    info.isSet ? info.type.name || info.type.id || info.rawType : info.rawType
  )
  // Only a resolvable typed page offers a "View all [Type]" dashboard target.
  let canViewAll = $derived(info.isSet && !!onViewAll)
  let menuOpen = $state(false)

  function closeMenu(): void {
    menuOpen = false
  }

  // Esc closes the menu (defers nothing — the menu is the only popover here).
  $effect(() => {
    if (!menuOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeMenu()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

{#if show}
  <div class="strip-wrap" class:with-caret={canViewAll}>
    <button
      type="button"
      class="type-strip"
      class:raw={!info.isSet}
      onclick={onOpen}
      aria-label="Page type {label}. Open properties."
      title="Open properties"
    >
      {#if info.isSet && info.type.icon}
        <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
          >{info.type.icon}</span
        >
      {/if}
      <span class="type-name">{label}</span>
      {#if info.isSet && heroValue}
        <span class="hero" aria-hidden="true">{heroValue}</span>
      {/if}
    </button>
    {#if canViewAll}
      <button
        type="button"
        class="caret"
        onclick={() => (menuOpen = !menuOpen)}
        aria-haspopup="menu"
        aria-expanded={menuOpen}
        aria-label="Type actions"
        title="Type actions"
      >
        <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
          >{menuOpen ? 'expand_less' : 'expand_more'}</span
        >
      </button>
      {#if menuOpen}
        <div class="menu" role="menu" aria-label="Type actions">
          <button
            type="button"
            role="menuitem"
            class="menu-item"
            onclick={() => {
              closeMenu()
              onViewAll?.()
            }}
          >
            <span
              class="material-symbols-outlined text-icon-sm"
              aria-hidden="true">table_view</span
            >
            <span>View all {label}</span>
          </button>
        </div>
      {/if}
      {#if menuOpen}
        <button
          type="button"
          class="backdrop"
          aria-hidden="true"
          tabindex="-1"
          onclick={closeMenu}
        ></button>
      {/if}
    {/if}
  </div>
{/if}

<style>
  .strip-wrap {
    position: relative;
    display: inline-flex;
    align-items: stretch;
    flex: 0 0 auto;
  }
  .type-strip {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    min-width: 0;
    max-width: 22rem;
    padding: 0.1rem 0.5rem;
    border-radius: 0.375rem;
    border: 1px solid var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    font-size: var(--text-type-xs);
    line-height: 1.4;
    cursor: pointer;
    transition:
      background 120ms var(--transition-standard),
      border-color 120ms var(--transition-standard);
  }
  /* Split-button shaping: when a caret follows, the chip rounds only its left
     side and drops its right border so the two read as one pill. */
  .strip-wrap.with-caret .type-strip {
    border-radius: 0.375rem 0 0 0.375rem;
    border-right: 0;
  }
  .type-strip:hover {
    background: var(--color-hover);
  }
  .type-strip:focus-visible,
  .caret:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .caret {
    display: inline-flex;
    align-items: center;
    padding: 0.1rem 0.2rem;
    border-radius: 0 0.375rem 0.375rem 0;
    border: 1px solid var(--color-accent-primary-start);
    background: var(--color-accent-primary-glow);
    color: var(--color-accent-primary-start);
    cursor: pointer;
  }
  .caret:hover {
    background: var(--color-hover);
  }
  /* Subdued treatment for an unrecognized raw type ref — no accent, no hero. */
  .type-strip.raw {
    border-color: var(--color-surface-panel-border);
    background: transparent;
    color: var(--color-text-muted);
  }
  .type-name {
    font-weight: 600;
    white-space: nowrap;
  }
  .hero {
    color: var(--color-text-primary);
    font-weight: 400;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    opacity: 0.85;
  }
  .menu {
    position: absolute;
    top: calc(100% + 0.2rem);
    right: 0;
    z-index: 50;
    min-width: 12rem;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 0.5rem;
    box-shadow: var(--shadow-lg);
    padding: 0.25rem;
  }
  .menu-item {
    display: flex;
    align-items: center;
    gap: 0.4rem;
    width: 100%;
    text-align: left;
    padding: 0.35rem 0.5rem;
    border: 0;
    background: transparent;
    color: var(--color-surface-popover-text);
    border-radius: 0.3rem;
    font-size: var(--text-type-sm);
    cursor: pointer;
  }
  .menu-item:hover {
    background: var(--color-hover);
  }
  .menu-item:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
  .backdrop {
    position: fixed;
    inset: 0;
    z-index: 40;
    background: transparent;
    border: 0;
    cursor: default;
    padding: 0;
  }
  /* Keep the hero out of the way on narrow viewports. */
  @media (max-width: 700px) {
    .hero {
      display: none;
    }
  }
</style>
