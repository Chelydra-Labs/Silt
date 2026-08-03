<script lang="ts">
  // Inline meta pill — the glanceability layer that lives in the page-header
  // (breadcrumb) row. One component covers the three page-type states:
  //
  //   • Typed (resolved type) — accent-tinted split-button: body opens the
  //     bottom properties panel; caret opens "View all {Type}" → dashboard.
  //   • Raw (unrecognized `type:` ref) — subdued chip; click opens the panel.
  //   • Untyped — hover/focus-revealed dashed chip with a [+] Type affordance;
  //     click opens the panel AND arms its type menu (mirrors `/type` slash).
  //
  // The untyped pill stays in the DOM (focusable, in the tab order) but is
  // visually subdued at rest so the page-header invariant holds: untyped
  // pages are the majority, and a clean header reads as a clean canvas.
  // opacity:0 (not display:none) keeps it in the tab order — required for
  // a11y. `.breadcrumb-row` is the class the host (PageBreadcrumb) sets on
  // the wrapper that contains the meta slot; referenced via :global() so the
  // hover/focus-within reveal resolves across the component boundary.
  import type { PageTypeInfo } from './types'

  interface Props {
    info: PageTypeInfo
    heroValue: string
    onOpen: () => void
    onViewAll?: () => void
    /** Untyped-state click: arms the panel's type menu (mirrors the /type
     *  slash command — App.svelte composes this with `onOpen` so the
     *  untyped click opens the panel AND focuses its type picker). */
    onOpenWithTypeMenu?: () => void
  }

  let { info, heroValue, onOpen, onViewAll, onOpenWithTypeMenu }: Props =
    $props()

  let isTyped = $derived(info.isSet)
  let isRaw = $derived(!info.isSet && info.rawType.length > 0)
  let isUntyped = $derived(!info.isSet && info.rawType.length === 0)
  let label = $derived(
    info.isSet ? info.type.name || info.type.id || info.rawType : info.rawType
  )
  // Only a resolvable typed page offers a "View all [Type]" dashboard target.
  let canViewAll = $derived(info.isSet && !!onViewAll)
  let menuOpen = $state(false)

  function closeMenu(): void {
    menuOpen = false
  }

  // Untyped mirrors the /type slash command: open the panel AND arm its type
  // menu so the picker is focused on arrival. Typed/raw just open the panel.
  function handleBodyClick(): void {
    if (isUntyped) {
      onOpen()
      onOpenWithTypeMenu?.()
    } else {
      onOpen()
    }
  }

  // Esc closes the caret menu (defers nothing — the menu is the only popover).
  $effect(() => {
    if (!menuOpen) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') closeMenu()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  })
</script>

<div class="strip-wrap" class:with-caret={canViewAll}>
  <button
    type="button"
    class="type-strip"
    class:raw={isRaw}
    class:pill-untyped={isUntyped}
    onclick={handleBodyClick}
    aria-label={isUntyped
      ? 'Assign a page type'
      : isRaw
        ? `Unrecognized page type: ${info.rawType}. Open properties.`
        : `Page type: ${label}. Open properties.`}
    title={isUntyped ? 'Assign a page type' : 'Open properties'}
  >
    {#if isUntyped}
      <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
        >add</span
      >
      <span class="type-name">Type</span>
    {:else}
      {#if isTyped && info.type.icon}
        <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
          >{info.type.icon}</span
        >
      {/if}
      <span class="type-name">{label}</span>
      {#if isTyped && heroValue}
        <span class="hero" aria-hidden="true">{heroValue}</span>
      {/if}
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
      border-color 120ms var(--transition-standard),
      opacity 120ms var(--transition-standard);
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
  /* Untyped pill: dashed affordance, subdued at rest, hover/focus-revealed.
     Lives in the DOM (focusable) at all times so keyboard users can reach it
     via Tab; opacity:0 (NOT display:none) keeps it in the tab order. */
  .type-strip.pill-untyped {
    border: 1px dashed var(--color-surface-panel-border);
    background: transparent;
    color: var(--color-text-muted);
    opacity: 0;
    pointer-events: none;
  }
  /* The host (PageBreadcrumb) sets .breadcrumb-row on the row containing the
     meta slot. Reveal the untyped pill on header interaction (hover, any
     focus within the row, or direct keyboard focus on the pill itself). */
  :global(.breadcrumb-row):hover .pill-untyped,
  :global(.breadcrumb-row):focus-within .pill-untyped,
  .pill-untyped:focus-visible {
    opacity: 1;
    pointer-events: auto;
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
