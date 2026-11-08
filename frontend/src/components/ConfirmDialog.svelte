<script lang="ts">
  import Button from './ui/Button.svelte'
  import Dialog from './ui/Dialog.svelte'

  /**
   * Shared confirmation dialog (vault-style a11y). Backdrop blur, Esc +
   * click-away cancel, Tab focus trap via shared Dialog primitive. Does not
   * autofocus confirm — the dialog container receives focus so destructive
   * actions need a deliberate choice (#531).
   */
  interface Props {
    title: string
    message: string
    confirmLabel?: string
    cancelLabel?: string
    destructive?: boolean
    busy?: boolean
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
    busy = false,
    onConfirm,
    onCancel,
    dataTestId
  }: Props = $props()
</script>

<Dialog
  ariaLabel={title}
  backdropLabel={cancelLabel}
  {busy}
  {dataTestId}
  onClose={() => {
    if (!busy) onCancel()
  }}
>
  <div class="px-5 py-4 border-b border-surface-modal-border">
    <h2 class="font-headline-md text-headline-md text-text-primary">
      {title}
    </h2>
    <p class="text-text-muted text-type-sm font-body-md mt-1">{message}</p>
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
      variant={destructive ? 'danger' : 'primary'}
      onclick={() => {
        if (!busy) onConfirm()
      }}
      disabled={busy}
      dataTestId={dataTestId ? `${dataTestId}-confirm` : undefined}
    >
      {confirmLabel}
    </Button>
  </div>
</Dialog>
