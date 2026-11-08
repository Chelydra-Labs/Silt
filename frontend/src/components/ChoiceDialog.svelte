<script lang="ts">
  import Button from './ui/Button.svelte'
  import Dialog from './ui/Dialog.svelte'

  /**
   * Multi-action confirmation dialog (#664). Primary + secondary actions plus
   * cancel. Shared Dialog focusTrap, Esc cancel, no autofocus on destructive paths.
   */
  interface Props {
    title: string
    message: string
    primaryLabel: string
    secondaryLabel: string
    cancelLabel?: string
    /** Prefer this over mount-time activeElement for programmatic open. */
    returnFocusTo?: HTMLElement | null
    onPrimary: () => void
    onSecondary: () => void
    onCancel: () => void
    dataTestId?: string
  }

  let {
    title,
    message,
    primaryLabel,
    secondaryLabel,
    cancelLabel = 'Cancel',
    returnFocusTo = null,
    onPrimary,
    onSecondary,
    onCancel,
    dataTestId
  }: Props = $props()
</script>

<Dialog
  ariaLabel={title}
  backdropLabel={cancelLabel}
  maxWidth="max-w-md"
  {returnFocusTo}
  {dataTestId}
  onClose={onCancel}
>
  <div class="px-5 py-4 border-b border-surface-modal-border">
    <h2 class="font-headline-md text-headline-md text-text-primary">
      {title}
    </h2>
    <p class="text-text-muted text-type-sm font-body-md mt-1">{message}</p>
  </div>
  <div class="flex flex-wrap items-center justify-end gap-2 px-5 py-3">
    <Button
      variant="ghost"
      onclick={onCancel}
      dataTestId={dataTestId ? `${dataTestId}-cancel` : undefined}
    >
      {cancelLabel}
    </Button>
    <Button
      variant="secondary"
      onclick={onSecondary}
      dataTestId={dataTestId ? `${dataTestId}-secondary` : undefined}
    >
      {secondaryLabel}
    </Button>
    <Button
      variant="primary"
      onclick={onPrimary}
      dataTestId={dataTestId ? `${dataTestId}-primary` : undefined}
    >
      {primaryLabel}
    </Button>
  </div>
</Dialog>
