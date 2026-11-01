<script lang="ts">
  /**
   * External-edit save refusal banner (#1005).
   *
   * Shown by the surfaces that host autosave/save errors when the backend
   * refused a whole-page save because the file changed on disk since it was
   * opened. Non-blocking by design: the refusal state is the existing save
   * error phase, and the three actions settle it — the losing version always
   * survives as a `.conflict-<timestamp>` sibling page.
   */
  import {
    CONFLICT_RESOLUTIONS,
    type ExternalConflictResolution
  } from '../../lib/editor/externalConflict'

  interface Props {
    /** Page name (without .md) — names the file in the message. */
    page: string
    /** True while a resolution IPC is in flight; disables the actions. */
    busy?: boolean
    /** Set when a resolution attempt failed; the actions stay offered. */
    error?: string | null
    onResolve: (resolution: ExternalConflictResolution) => void
  }

  let { page, busy = false, error = null, onResolve }: Props = $props()
</script>

<div
  class="border border-error-border bg-error-bg text-error rounded-lg px-4 py-3 flex flex-col gap-2 my-2"
  role="alert"
  data-testid="save-conflict-banner"
>
  <p class="font-body-md">
    “{page}.md” changed on disk since it was opened, so the save was held to
    avoid overwriting it. Choose which version to keep — the other is saved as
    a .conflict copy.
  </p>
  {#if error}
    <p class="text-type-sm">
      Resolution failed: {error}. Try again, or recover both versions from the
      files on disk.
    </p>
  {/if}
  <div class="flex flex-wrap gap-2" role="group" aria-label="Resolve external edit">
    {#each CONFLICT_RESOLUTIONS as action (action.key)}
      <button
        type="button"
        class="px-4 py-1.5 rounded-lg bg-error/20 border border-error-border text-error font-label-sm-bold hover:brightness-110 transition-all cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
        onclick={() => onResolve(action.key)}
        disabled={busy}
        title={action.hint}
      >
        {action.label}
      </button>
    {/each}
  </div>
</div>
