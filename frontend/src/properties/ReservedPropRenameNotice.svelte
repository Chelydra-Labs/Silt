<script lang="ts">
  // ReservedPropRenameNotice — one-time banner after the #900 vault-open
  // migration renames type properties that collided with core metadata
  // (created/aliases). Go stamps reserved_prop_rename_v1_notice into
  // ui.dismissed_tips and stores the rename list under
  // plugins.plugin_settings._reserved_prop_renames_v1. Dismissal appends the
  // ack stamp via appendDismissedTip (add-only). Presentational + prop-driven.
  // role="status" aria-live="polite" so screen readers announce it.
  import type { ReservedPropRename } from './types'

  interface Props {
    dismissed: boolean
    renames: ReservedPropRename[]
    onDismiss: () => void
  }

  let { dismissed, renames, onDismiss }: Props = $props()

  let summary = $derived.by(() => {
    if (renames.length === 0) {
      return 'Some type properties were renamed because created and aliases are now core page fields.'
    }
    const parts = renames.map((r) => {
      const typeLabel = r.type_name || r.type_id || 'type'
      return `${typeLabel}: ${r.from} → ${r.to}`
    })
    const shown = parts.slice(0, 4)
    const extra = parts.length - shown.length
    let text =
      'Type properties were renamed because created and aliases are now core page fields — ' +
      shown.join('; ')
    if (extra > 0) text += `; +${extra} more`
    return text + '. Review types in the type editor if needed.'
  })
</script>

{#if !dismissed}
  <div
    class="flex items-start gap-2 p-3 rounded-lg bg-accent-primary-start/10 border border-accent-primary-start/30 text-accent-primary-start text-type-sm font-body-md"
    role="status"
    aria-live="polite"
    data-testid="reserved-prop-rename-notice"
  >
    <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
      >drive_file_rename_outline</span
    >
    <span class="flex-1">{summary}</span>
    <button
      type="button"
      onclick={onDismiss}
      aria-label="Dismiss notice"
      class="font-label-sm-bold underline hover:brightness-110 bg-transparent border-none cursor-pointer text-accent-primary-start shrink-0"
    >
      Got it
    </button>
  </div>
{/if}
