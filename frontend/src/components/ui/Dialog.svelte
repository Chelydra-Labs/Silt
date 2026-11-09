<script lang="ts">
  import type { Snippet } from 'svelte'
  import { trapFocus } from '../../lib/focusTrap'

  /**
   * Shared modal overlay primitive per ADR 0009 (overlay <div> + trapFocus).
   * Standardizes backdrop (`bg-black/45 backdrop-blur-[2px]`), Tab/Shift+Tab
   * focus trap, Escape key dismissal, backdrop click dismissal, and focus
   * restoration to the previously focused element on close/unmount.
   */
  interface Props {
    open?: boolean
    onClose?: () => void
    closeOnEscape?: boolean
    closeOnBackdrop?: boolean
    busy?: boolean
    role?: 'dialog' | 'alertdialog'
    ariaLabel?: string
    labelledBy?: string
    describedBy?: string
    backdropLabel?: string
    zIndex?: string
    placement?: 'center' | 'top'
    maxWidth?: string
    surfaceClass?: string
    returnFocusTo?: HTMLElement | null
    initialFocus?: HTMLElement | null | (() => HTMLElement | null)
    onKeydown?: (e: KeyboardEvent) => void
    dataTestId?: string
    dialogRef?: HTMLDivElement | null
    children?: Snippet
  }

  let {
    open = true,
    onClose,
    closeOnEscape = true,
    closeOnBackdrop = true,
    busy = false,
    role = 'dialog',
    ariaLabel,
    labelledBy,
    describedBy,
    backdropLabel = 'Close dialog',
    zIndex = 'z-[200]',
    placement = 'center',
    maxWidth = 'max-w-sm',
    surfaceClass = '',
    returnFocusTo = null,
    initialFocus = null,
    onKeydown,
    dataTestId,
    dialogRef = $bindable(null),
    children
  }: Props = $props()

  function handleKeydown(e: KeyboardEvent) {
    if (!open) return
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      if (!busy && closeOnEscape) {
        onClose?.()
      }
      return
    }
    onKeydown?.(e)
  }

  function handleBackdropClick() {
    if (!busy && closeOnBackdrop) {
      onClose?.()
    }
  }

  $effect(() => {
    if (!open || !dialogRef) return

    const capturedFocus =
      returnFocusTo ?? (document.activeElement as HTMLElement | null)
    window.addEventListener('keydown', handleKeydown, true)
    const disposeTrap = trapFocus(dialogRef)

    if (!dialogRef.contains(document.activeElement)) {
      const target =
        typeof initialFocus === 'function' ? initialFocus() : initialFocus
      if (target) {
        target.focus()
      } else {
        dialogRef.focus()
      }
    }

    return () => {
      disposeTrap()
      window.removeEventListener('keydown', handleKeydown, true)
      const restoreTarget = returnFocusTo ?? capturedFocus
      if (restoreTarget?.isConnected) {
        restoreTarget.focus?.()
      }
    }
  })

  const placementClasses: Record<NonNullable<Props['placement']>, string> = {
    center: 'items-center justify-center',
    top: 'items-start justify-center pt-[12vh]'
  }
</script>

{#if open}
  <div
    class="fixed inset-0 {zIndex} flex {placementClasses[
      placement
    ]} bg-black/45 backdrop-blur-[2px]"
    data-focus-trap
  >
    <button
      type="button"
      tabindex="-1"
      aria-label={backdropLabel}
      class="absolute inset-0 cursor-default border-none bg-transparent p-0"
      onclick={handleBackdropClick}
    ></button>
    <div
      bind:this={dialogRef}
      {role}
      aria-modal="true"
      aria-label={ariaLabel}
      aria-labelledby={labelledBy}
      aria-describedby={describedBy}
      aria-busy={busy || undefined}
      tabindex="-1"
      data-testid={dataTestId}
      class="ui-dialog-surface relative w-full {maxWidth} glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden {surfaceClass}"
    >
      {#if children}
        {@render children()}
      {/if}
    </div>
  </div>
{/if}

<style>
  .ui-dialog-surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }
</style>
