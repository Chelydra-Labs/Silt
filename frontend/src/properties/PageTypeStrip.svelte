<script lang="ts">
  // Inline meta strip — the glanceability layer that lives in the page-header
  // (breadcrumb) row. Renders NOTHING for an untyped page (the majority case —
  // no visual noise), a subdued chip for an unrecognized `type:` ref, and the
  // type chip + hero value for a properly typed page. Clicking the chip opens
  // the bottom properties panel.
  import type { PageTypeInfo } from './types'

  interface Props {
    info: PageTypeInfo
    heroValue: string
    onOpen: () => void
  }

  let { info, heroValue, onOpen }: Props = $props()

  // Untyped + no raw ref → render nothing at all (clean page-header invariant).
  let show = $derived(info.isSet || info.rawType.length > 0)
  let label = $derived(
    info.isSet ? info.type.name || info.type.id || info.rawType : info.rawType
  )
</script>

{#if show}
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
{/if}

<style>
  .type-strip {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    flex: 0 0 auto;
    max-width: 22rem;
    min-width: 0;
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
  .type-strip:hover {
    background: var(--color-hover);
  }
  .type-strip:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
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
  /* Keep the hero out of the way on narrow viewports. */
  @media (max-width: 700px) {
    .hero {
      display: none;
    }
  }
</style>
