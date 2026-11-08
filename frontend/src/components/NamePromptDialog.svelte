<script lang="ts">
  import { onMount, tick } from 'svelte'
  import Button from './ui/Button.svelte'
  import Dialog from './ui/Dialog.svelte'
  import Input from './ui/Input.svelte'

  /**
   * Named prompt dialog for Save as / Rename (#531). Focuses the text field
   * on open; validates non-empty trim; Esc / backdrop cancel; Ctrl/Cmd+Enter
   * submits (#662). Tab trap via shared Dialog primitive.
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

  let inputRef = $state<HTMLInputElement | null>(null)
  // Seed once at mount — parent remounts the dialog when opening with a new name.
  // svelte-ignore state_referenced_locally
  let value = $state(initialValue)
  let error = $state<string | null>(null)

  const inputId = $derived(dataTestId ? `${dataTestId}-input` : undefined)
  const errorId = $derived(dataTestId ? `${dataTestId}-error` : undefined)
  const activeError = $derived(error || errorMessage || null)

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
    // Enter or Ctrl/Cmd+Enter submits (#662).
    if (
      e.key === 'Enter' &&
      (e.target === inputRef || e.ctrlKey || e.metaKey)
    ) {
      e.preventDefault()
      if (!busy) submit()
    }
  }

  onMount(() => {
    void tick().then(() => {
      inputRef?.focus()
      inputRef?.select()
    })
  })
</script>

<Dialog
  ariaLabel={title}
  backdropLabel={cancelLabel}
  {busy}
  {dataTestId}
  initialFocus={() => inputRef}
  onClose={() => {
    if (!busy) onCancel()
  }}
  onKeydown={handleKeydown}
>
  <div class="px-5 py-4 border-b border-surface-modal-border">
    <h2 class="font-headline-md text-headline-md text-text-primary">
      {title}
    </h2>
    <label
      class="block mt-3 text-type-sm font-label-sm text-text-muted"
      for={inputId}
    >
      {label}
    </label>
    <Input
      bind:ref={inputRef}
      id={inputId}
      type="text"
      class="mt-1.5"
      bind:value
      {placeholder}
      disabled={busy}
      error={activeError}
      {errorId}
      errorRole={errorMessage ? 'alert' : 'status'}
      errorLive={errorMessage ? 'assertive' : 'polite'}
      dataTestId={inputId}
      oninput={() => {
        if (error) error = null
      }}
    />
  </div>
  <div class="flex items-center justify-end gap-2 px-5 py-3">
    <Button
      variant="ghost"
      onclick={() => {
        if (!busy) onCancel()
      }}
      disabled={busy}
      dataTestId={dataTestId ? `${dataTestId}-cancel` : undefined}
    >
      {cancelLabel}
    </Button>
    <Button
      variant="primary"
      onclick={submit}
      disabled={busy}
      dataTestId={dataTestId ? `${dataTestId}-confirm` : undefined}
    >
      {confirmLabel}
    </Button>
  </div>
</Dialog>
