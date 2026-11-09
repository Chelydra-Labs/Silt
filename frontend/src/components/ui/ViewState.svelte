<script lang="ts">
  import type { Snippet } from 'svelte'
  import Button from './Button.svelte'

  interface Props {
    kind: 'loading' | 'error' | 'empty'
    compact?: boolean
    icon?: string
    title?: string
    description?: string
    retryLabel?: string
    onRetry?: () => void
    dataTestId?: string
    class?: string
    actions?: Snippet
    children?: Snippet
  }

  let {
    kind,
    compact = false,
    icon,
    title,
    description,
    retryLabel = 'Retry',
    onRetry,
    dataTestId,
    class: className = '',
    actions,
    children
  }: Props = $props()
</script>

{#if kind === 'error'}
  <div
    role="alert"
    data-testid={dataTestId}
    class="flex flex-col items-center justify-center text-center {compact
      ? 'py-4 px-2'
      : 'py-8 px-4'} {className}"
  >
    <span
      class="material-symbols-outlined text-status-danger {compact
        ? 'text-icon-lg mb-1'
        : 'text-icon-2xl mb-2'}"
      aria-hidden="true"
    >
      {icon ?? 'error'}
    </span>
    {#if title}
      <h3
        class="{compact
          ? 'text-type-sm font-semibold'
          : 'font-headline-md text-type-lg font-semibold'} text-text-primary m-0"
      >
        {title}
      </h3>
    {/if}
    {#if description}
      <p
        class="{compact
          ? 'text-type-xs mt-1'
          : 'text-type-sm mt-1.5'} text-text-muted max-w-md m-0"
      >
        {description}
      </p>
    {/if}
    {#if onRetry}
      <div class="mt-3">
        <Button
          variant="secondary"
          size={compact ? 'sm' : 'md'}
          onclick={onRetry}
        >
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">refresh</span
          >
          {retryLabel}
        </Button>
      </div>
    {/if}
    {#if actions}
      <div class="flex flex-wrap items-center justify-center gap-2 mt-3">
        {@render actions()}
      </div>
    {/if}
    {#if children}
      {@render children()}
    {/if}
  </div>
{:else if kind === 'loading'}
  <div
    role="status"
    aria-live="polite"
    data-testid={dataTestId}
    class="flex flex-col items-center justify-center text-center {compact
      ? 'py-4 px-2'
      : 'py-8 px-4'} {className}"
  >
    <div class="flex items-center gap-2 text-text-muted">
      <span
        class="material-symbols-outlined animate-spin {compact
          ? 'text-icon-md'
          : 'text-icon-xl'}"
        aria-hidden="true"
      >
        {icon ?? 'progress_activity'}
      </span>
      <span
        class="{compact
          ? 'text-type-xs'
          : 'text-type-sm'} font-body-md animate-pulse"
      >
        {title ?? 'Loading…'}
      </span>
    </div>
    {#if description}
      <p class="text-type-xs text-text-muted mt-1 m-0">
        {description}
      </p>
    {/if}
    {#if children}
      {@render children()}
    {/if}
  </div>
{:else}
  <div
    data-testid={dataTestId}
    class="flex flex-col items-center justify-center text-center {compact
      ? 'py-6 px-3'
      : 'py-12 px-4'} {className}"
  >
    {#if icon}
      <span
        class="material-symbols-outlined text-text-muted opacity-40 {compact
          ? 'text-icon-xl mb-1'
          : 'text-icon-2xl mb-2'}"
        aria-hidden="true"
      >
        {icon}
      </span>
    {/if}
    {#if title}
      <h3
        class="{compact
          ? 'text-type-sm font-semibold'
          : 'font-headline-md text-type-lg font-semibold'} text-text-primary m-0"
      >
        {title}
      </h3>
    {/if}
    {#if description}
      <p
        class="{compact
          ? 'text-type-xs mt-1'
          : 'text-type-sm mt-1.5'} text-text-muted max-w-md m-0"
      >
        {description}
      </p>
    {/if}
    {#if actions}
      <div class="flex flex-wrap items-center justify-center gap-2 mt-4">
        {@render actions()}
      </div>
    {/if}
    {#if children}
      {@render children()}
    {/if}
  </div>
{/if}
