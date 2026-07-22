<script lang="ts">
  import { onMount, tick } from 'svelte'

  /**
   * Shared confirmation dialog (vault-style a11y). Backdrop blur, Esc +
   * click-away cancel, Tab focus trap. Does not autofocus confirm — the
   * dialog container receives focus so destructive actions need a deliberate
   * choice (#531).
   */
  interface Props {
    title: string
    message: string
    confirmLabel?: string
    cancelLabel?: string
    destructive?: boolean
    onConfirm: () => void
    onCancel: () => void
    dataTestId?: string
  }

  let {
    title,
    message,
    confirmLabel = 'Confirm',
    cancelLabel = 'Cancel',
    destructive = false,
    onConfirm,
    onCancel,
    dataTestId
  }: Props = $props()

  let dialogRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null

  const FOCUSABLE =
    'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!dialogRef) return []
    return Array.from(dialogRef.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onCancel()
      return
    }
    if (e.key === 'Tab' && dialogRef) {
      const els = focusableEls()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !dialogRef.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else if (active === last || !dialogRef.contains(active)) {
        e.preventDefault()
        first.focus()
      }
    }
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown, true)
    tick().then(() => dialogRef?.focus())
    return () => {
      window.removeEventListener('keydown', handleKeydown, true)
      previouslyFocused?.focus?.()
    }
  })
</script>

<div
  class="fixed inset-0 z-[200] flex items-center justify-center bg-black/40 backdrop-blur-[2px]"
  data-focus-trap
>
  <button
    type="button"
    tabindex="-1"
    aria-label={cancelLabel}
    class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    onclick={onCancel}
  ></button>
  <div
    bind:this={dialogRef}
    role="dialog"
    aria-modal="true"
    aria-label={title}
    tabindex="-1"
    data-testid={dataTestId}
    class="dialog-surface relative w-full max-w-sm glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden"
  >
    <div class="px-5 py-4 border-b border-surface-modal-border">
      <h2 class="font-headline-md text-headline-md text-text-primary">
        {title}
      </h2>
      <p class="text-text-muted text-type-sm font-body-md mt-1">{message}</p>
    </div>
    <div class="flex items-center justify-end gap-2 px-5 py-3">
      <button
        type="button"
        onclick={onCancel}
        data-testid={dataTestId ? `${dataTestId}-cancel` : undefined}
        class="px-4 py-2 rounded-lg text-text-muted hover:text-text-primary font-label-sm-bold transition-colors border-none bg-transparent cursor-pointer"
      >
        {cancelLabel}
      </button>
      <button
        type="button"
        onclick={onConfirm}
        data-testid={dataTestId ? `${dataTestId}-confirm` : undefined}
        class="px-4 py-2 rounded-lg font-label-sm-bold transition-all cursor-pointer border {destructive
          ? 'bg-status-danger/20 border-status-danger/40 text-status-danger hover:brightness-110'
          : 'bg-accent-primary-start/20 border-accent-primary-start/40 text-text-primary hover:brightness-110'}"
      >
        {confirmLabel}
      </button>
    </div>
  </div>
</div>

<style>
  .dialog-surface:focus-visible {
    outline: 2px solid var(--color-border-focus);
    outline-offset: 2px;
  }
</style>
