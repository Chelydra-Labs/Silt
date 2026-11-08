<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { HTMLButtonAttributes } from 'svelte/elements'

  /**
   * Shared token-based button primitive for dialogs, forms, headers, and CTAs.
   * Uses named theme tokens from index.css @theme and exposes a consistent
   * keyboard focus ring.
   */
  interface Props extends Omit<HTMLButtonAttributes, 'type' | 'class'> {
    variant?: 'primary' | 'secondary' | 'ghost' | 'danger'
    size?: 'sm' | 'md'
    type?: 'button' | 'submit' | 'reset'
    disabled?: boolean
    ariaLabel?: string
    title?: string
    id?: string
    dataTestId?: string
    class?: string
    onclick?: (e: MouseEvent) => void
    ref?: HTMLButtonElement | null
    children?: Snippet
  }

  let {
    variant = 'secondary',
    size = 'md',
    type = 'button',
    disabled = false,
    ariaLabel,
    title,
    id,
    dataTestId,
    class: className = '',
    onclick,
    ref = $bindable(null),
    children,
    ...rest
  }: Props = $props()

  const variantClasses: Record<NonNullable<Props['variant']>, string> = {
    primary:
      'bg-accent-primary-start/20 hover:bg-accent-primary-start/30 border-accent-primary-start/40 text-text-primary hover:brightness-110',
    secondary:
      'bg-surface-panel hover:bg-hover border-surface-panel-border text-text-primary hover:brightness-110',
    ghost:
      'bg-transparent hover:bg-hover border-transparent text-text-muted hover:text-text-primary',
    danger:
      'bg-status-danger/20 hover:bg-status-danger/30 border-status-danger/40 text-status-danger hover:brightness-110'
  }

  const sizeClasses: Record<NonNullable<Props['size']>, string> = {
    sm: 'px-2.5 py-1.5 text-type-xs rounded-md',
    md: 'px-4 py-2 text-type-sm rounded-lg'
  }
</script>

<button
  bind:this={ref}
  {...rest}
  {type}
  {id}
  {disabled}
  {title}
  {onclick}
  aria-label={ariaLabel ?? rest['aria-label']}
  data-testid={dataTestId ??
    ((rest as Record<string, unknown>)['data-testid'] as string | undefined)}
  class="ui-btn inline-flex items-center justify-center gap-1.5 font-label-sm-bold border transition-all cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 {variantClasses[
    variant
  ]} {sizeClasses[size]} {className}"
>
  {#if children}
    {@render children()}
  {/if}
</button>

<style>
  .ui-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
