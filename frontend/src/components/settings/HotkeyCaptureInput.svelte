<script lang="ts">
  import {
    formatHotkey,
    hotkeyFromKeyboardEvent,
    parseHotkey
  } from '../../settings/hotkeys'

  interface Props {
    value: string
    /** Accessible name for the binding field (action label). */
    label: string
    onchange: (next: string) => void
  }
  let { value, label, onchange }: Props = $props()

  let capturing = $state(false)
  let inputEl: HTMLInputElement | null = $state(null)

  function startCapture() {
    capturing = true
  }

  function cancelCapture() {
    capturing = false
  }

  function clearBinding() {
    onchange('')
    capturing = false
  }

  function onKeyDown(e: KeyboardEvent) {
    if (!capturing) return

    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      cancelCapture()
      return
    }

    // Backspace/Delete with no modifiers clears (disable binding).
    if (
      (e.key === 'Backspace' || e.key === 'Delete') &&
      !e.ctrlKey &&
      !e.altKey &&
      !e.metaKey &&
      !e.shiftKey
    ) {
      e.preventDefault()
      e.stopPropagation()
      clearBinding()
      return
    }

    const parsed = hotkeyFromKeyboardEvent(e)
    if (!parsed) {
      // Still holding modifiers — wait for a real key.
      e.preventDefault()
      return
    }

    e.preventDefault()
    e.stopPropagation()
    const next = formatHotkey(parsed)
    // Only accept parseable combos (format should always re-parse).
    if (parseHotkey(next)) {
      onchange(next)
    }
    capturing = false
  }

  function onBlur() {
    if (capturing) cancelCapture()
  }

  let display = $derived(
    capturing ? 'Press a shortcut…' : value.trim() === '' ? '' : value
  )
  let isValid = $derived(
    value === undefined || value.trim() === '' || parseHotkey(value) !== null
  )
</script>

<div class="flex items-center gap-1">
  <input
    bind:this={inputEl}
    type="text"
    readonly
    value={display}
    aria-label={label}
    aria-invalid={!isValid}
    aria-pressed={capturing}
    placeholder="Click, then press keys"
    onfocus={startCapture}
    onclick={startCapture}
    onkeydown={onKeyDown}
    onblur={onBlur}
    class="bg-surface-panel border rounded-lg px-3 py-1.5 text-text-primary font-label-sm font-mono outline-none transition-colors w-full cursor-pointer
      {capturing
      ? 'border-accent-primary-start ring-1 ring-accent-primary-start'
      : isValid
        ? 'border-surface-panel-border focus:border-accent-primary-start'
        : 'border-error'}"
  />
  {#if value.trim() !== ''}
    <button
      type="button"
      onclick={clearBinding}
      aria-label="Clear {label} shortcut"
      title="Clear (disable)"
      class="flex-shrink-0 p-1.5 rounded-md text-text-muted hover:text-text-primary hover:bg-hover border-none bg-transparent cursor-pointer"
    >
      <span class="material-symbols-outlined text-sm" aria-hidden="true"
        >close</span
      >
    </button>
  {/if}
</div>
{#if capturing}
  <span class="sr-only" role="status" aria-live="polite"
    >Press a keyboard shortcut for {label}. Escape cancels. Backspace clears.</span
  >
{/if}
