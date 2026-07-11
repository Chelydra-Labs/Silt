<script lang="ts">
  /**
   * Shared inline error/warning banner for the Tasks hub surfaces.
   *
   * Replaces the per-view duplicated `<div role="alert">` blocks so every error
   * surface (List/Board/Calendar views, Sidebar, CommentThread, QuickAdd) gets
   * consistent semantics, tone, and optional retry/dismiss affordances.
   *
   * ARIA contract: `role="alert"` (implicit assertive live region) for errors
   * that need immediate announcement; `role="status"` + `aria-live="polite"`
   * for non-blocking warnings. The kind also drives the color tokens so error
   * (destructive / invalid input) and warning (recoverable / degraded) stay
   * visually distinct — mirroring the theme engine's `error` vs `status.warn`
   * split documented in ARCHITECTURE.md §4.4.
   *
   * `compact` swaps to the smaller inline style used by CommentThread /
   * QuickAdd / Sidebar (which render the banner inside a tight list/form, not
   * as a full-width view strip).
   */
  interface Props {
    message: string
    kind?: 'error' | 'warning'
    /** Dismiss callback. When omitted, no ✕ button renders (non-dismissable). */
    onDismiss?: () => void
    /** Retry callback. When omitted, no "Try again" button renders. */
    onRetry?: () => void
    /** Test hook — surfaces as `data-testid` on the banner root. */
    dataTestId?: string
    /** Smaller inline variant for forms / list rows (default: full strip). */
    compact?: boolean
  }

  let {
    message,
    kind = 'error',
    onDismiss,
    onRetry,
    dataTestId,
    compact = false
  }: Props = $props()

  const isError = $derived(kind === 'error')
  // Compute the wrapper classes in one place so the markup stays flat —
  // nested template literals inside a class="{...}" expression confuse the
  // Svelte parser. Error = destructive tone; warning = recoverable tone.
  const wrapperClass = $derived(
    [
      'flex items-center gap-2',
      isError ? 'bg-error-bg text-error' : 'bg-status-warn/10 text-status-warn',
      compact
        ? 'px-2 py-1 text-type-xs border-l-2 border-current rounded-sm'
        : `px-6 py-2 text-type-sm font-body-md border-b ${
            isError ? 'border-error-border' : 'border-status-warn/30'
          }`
    ].join(' ')
  )
  const iconClass = $derived(compact ? 'text-type-md' : 'text-icon-sm')
</script>

<div
  role={isError ? 'alert' : 'status'}
  aria-live={isError ? 'assertive' : 'polite'}
  aria-atomic="true"
  data-testid={dataTestId}
  class={wrapperClass}
>
  <span class="material-symbols-outlined {iconClass}" aria-hidden="true">
    {isError ? 'error' : 'warning'}
  </span>
  <span class="flex-1">{message}</span>
  {#if onRetry}
    <button
      type="button"
      onclick={onRetry}
      data-testid={dataTestId ? `${dataTestId}-retry` : undefined}
      class="underline font-label-sm hover:no-underline bg-transparent border-none cursor-pointer {compact
        ? 'text-type-xs'
        : 'text-type-sm'} focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-current"
    >
      Try again
    </button>
  {/if}
  {#if onDismiss}
    <button
      type="button"
      aria-label="Dismiss"
      onclick={onDismiss}
      data-testid={dataTestId ? `${dataTestId}-dismiss` : undefined}
      class="p-1 rounded hover:bg-hover {isError
        ? 'text-text-muted hover:text-error'
        : 'text-text-muted hover:text-status-warn'} border-none bg-transparent cursor-pointer focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-current"
    >
      <span class="material-symbols-outlined {iconClass}" aria-hidden="true"
        >close</span
      >
    </button>
  {/if}
</div>
