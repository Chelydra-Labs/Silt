<script lang="ts">
  import { onMount, tick } from 'svelte'

  /**
   * Named prompt dialog for Save as / Rename (#531). Focuses the text field
   * on open; validates non-empty trim; Esc / backdrop cancel; Ctrl/Cmd+Enter
   * submits (#662).
   */
  interface Props {
    title: string
    label?: string
    initialValue?: string
    placeholder?: string
    confirmLabel?: string
    cancelLabel?: string
    /** Server/async error shown under the field (parent keeps dialog open). */
    errorMessage?: string
    busy?: boolean
    onConfirm: (value: string) => void
    onCancel: () => void
    dataTestId?: string
  }

  let {
    title,
    label = 'Name',
    initialValue = '',
    placeholder = '',
    confirmLabel = 'Save',
    cancelLabel = 'Cancel',
    errorMessage = '',
    busy = false,
    onConfirm,
    onCancel,
    dataTestId
  }: Props = $props()

  let dialogRef = $state<HTMLDivElement | null>(null)
  let inputRef = $state<HTMLInputElement | null>(null)
  // Seed once at mount — parent remounts the dialog when opening with a new name.
  // svelte-ignore state_referenced_locally
  let value = $state(initialValue)
  let error = $state<string | null>(null)
  let previouslyFocused: HTMLElement | null = null

  const FOCUSABLE =
    'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!dialogRef) return []
    return Array.from(dialogRef.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function submit() {
    const trimmed = value.trim()
    if (!trimmed) {
      error = 'Name is required'
      inputRef?.focus()
      return
    }
    error = null
    onConfirm(trimmed)
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onCancel()
      return
    }
    // Enter or Ctrl/Cmd+Enter submits (#662).
    if (
      e.key === 'Enter' &&
      (e.target === inputRef || e.ctrlKey || e.metaKey)
    ) {
      e.preventDefault()
      if (!busy) submit()
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
    tick().then(() => {
      inputRef?.focus()
      inputRef?.select()
    })
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
    class="relative w-full max-w-sm glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden"
  >
    <div class="px-5 py-4 border-b border-surface-modal-border">
      <h2 class="font-headline-md text-headline-md text-text-primary">
        {title}
      </h2>
      <label
        class="block mt-3 text-type-sm font-label-sm text-text-muted"
        for={dataTestId ? `${dataTestId}-input` : undefined}
      >
        {label}
      </label>
      <input
        bind:this={inputRef}
        id={dataTestId ? `${dataTestId}-input` : undefined}
        type="text"
        class="mt-1.5 w-full h-9 px-2.5 rounded-md bg-surface-panel border border-surface-panel-border text-text-primary font-body-md text-type-sm focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60"
        bind:value
        {placeholder}
        disabled={busy}
        aria-invalid={error || errorMessage ? true : undefined}
        aria-describedby={(error || errorMessage) && dataTestId
          ? `${dataTestId}-error`
          : undefined}
        data-testid={dataTestId ? `${dataTestId}-input` : undefined}
        oninput={() => {
          if (error) error = null
        }}
      />
      {#if error || errorMessage}
        <p
          id={dataTestId ? `${dataTestId}-error` : undefined}
          class="mt-1.5 text-type-2xs font-label-sm text-status-danger"
          role={errorMessage ? 'alert' : 'status'}
          aria-live={errorMessage ? 'assertive' : 'polite'}
        >
          {error || errorMessage}
        </p>
      {/if}
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
        onclick={submit}
        disabled={busy}
        data-testid={dataTestId ? `${dataTestId}-confirm` : undefined}
        class="px-4 py-2 rounded-lg font-label-sm-bold transition-all cursor-pointer border bg-accent-primary-start/20 border-accent-primary-start/40 text-text-primary hover:brightness-110 disabled:opacity-50 disabled:cursor-not-allowed"
      >
        {confirmLabel}
      </button>
    </div>
  </div>
</div>
