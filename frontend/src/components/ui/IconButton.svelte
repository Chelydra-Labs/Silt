<script lang="ts">
  import type { Snippet } from 'svelte'
  import type { HTMLButtonAttributes } from 'svelte/elements'

  /**
   * Accessible icon-only button primitive. Requires an accessible name via
   * ariaLabel (or aria-label) and renders either a Material Symbols glyph
   * (`icon`) or custom children snippet.
   */
  interface Props extends Omit<HTMLButtonAttributes, 'type' | 'class'> {
    ariaLabel?: string
    title?: string
    icon?: string
    size?: 'sm' | 'md'
    variant?: 'ghost' | 'secondary' | 'danger'
    active?: boolean
    disabled?: boolean
    type?: 'button' | 'submit' | 'reset'
    dataTestId?: string
    class?: string
    onclick?: (e: MouseEvent) => void
    ref?: HTMLButtonElement | null
    children?: Snippet
  }

  let {
    ariaLabel,
    title,
    icon,
    size = 'md',
    variant = 'ghost',
    active = false,
    disabled = false,
    type = 'button',
    dataTestId,
    class: className = '',
    onclick,
    ref = $bindable(null),
    children,
    ...rest
  }: Props = $props()

  const resolvedAriaLabel = $derived(
    ariaLabel ??
      (rest['aria-label'] as string | undefined) ??
      title ??
      undefined
  )

  const variantClasses: Record<NonNullable<Props['variant']>, string> = {
    ghost:
      'bg-transparent hover:bg-hover border-transparent text-text-muted hover:text-text-primary',
    secondary:
      'bg-surface-panel hover:bg-hover border-surface-panel-border text-text-primary',
    danger:
      'bg-status-danger/20 hover:bg-status-danger/30 border-status-danger/40 text-status-danger'
  }

  const sizeClasses: Record<NonNullable<Props['size']>, string> = {
    sm: 'w-7 h-7 rounded-md',
    md: 'w-8 h-8 rounded-lg'
  }

  const iconSizeClasses: Record<NonNullable<Props['size']>, string> = {
    sm: 'text-icon-md',
    md: 'text-icon-lg'
  }
</script>

<button
  bind:this={ref}
  {...rest}
  {type}
  {disabled}
  {onclick}
  aria-label={resolvedAriaLabel}
  aria-pressed={active
    ? true
    : (rest['aria-pressed'] as
        boolean | 'false' | 'mixed' | 'true' | undefined)}
  title={title ?? resolvedAriaLabel}
  data-testid={dataTestId ??
    ((rest as Record<string, unknown>)['data-testid'] as string | undefined)}
  class="ui-icon-btn inline-flex items-center justify-center border transition-colors cursor-pointer disabled:cursor-not-allowed disabled:opacity-50 {sizeClasses[
    size
  ]} {active
    ? 'bg-active border-border-active text-text-primary'
    : variantClasses[variant]} {className}"
>
  {#if children}
    {@render children()}
  {:else if icon}
    <span
      class="material-symbols-outlined select-none leading-none {iconSizeClasses[
        size
      ]}"
      aria-hidden="true">{icon}</span
    >
  {/if}
</button>

<style>
  .ui-icon-btn:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 1px;
  }
</style>
