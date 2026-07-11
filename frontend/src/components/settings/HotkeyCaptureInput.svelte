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
  const helpId = $derived(
    `hotkey-help-${label.replace(/\s+/g, '-').toLowerCase()}`
  )

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

<div class="flex flex-col gap-1">
  <div class="flex items-center gap-1">
    <input
      type="text"
      readonly
      value={display}
      aria-label={label}
      aria-invalid={!isValid}
      aria-describedby={helpId}
      placeholder="Click, then press keys"
      onfocus={startCapture}
      onclick={startCapture}
      onkeydown={onKeyDown}
      onblur={onBlur}
      data-capturing={capturing ? 'true' : 'false'}
      class="bg-surface-panel border rounded-lg px-3 py-1.5 text-text-primary text-type-sm font-mono outline-none transition-colors w-full cursor-pointer
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
        <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
          >close</span
        >
      </button>
    {/if}
  </div>
  <p
    id={helpId}
    class="text-type-3xs text-text-muted {capturing ? '' : 'sr-only'}"
    role="status"
    aria-live="polite"
  >
    {#if capturing}
      Press a shortcut for {label}. Escape cancels. Backspace clears.
    {:else}
      Click the field, then press a keyboard shortcut. Leave empty to disable.
    {/if}
  </p>
</div>
