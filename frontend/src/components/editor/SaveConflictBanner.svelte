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
    /** True while a resolution IPC is in flight; disables the actions. */
    busy?: boolean
    /** Set when a resolution attempt failed; the actions stay offered. */
    error?: string | null
    onResolve: (resolution: ExternalConflictResolution) => void
  }

  let { busy = false, error = null, onResolve }: Props = $props()
</script>

<div
  class="border border-error-border bg-error-bg text-error rounded-lg px-4 py-3 flex flex-col gap-2 my-2"
  data-testid="save-conflict-banner"
>
  <!-- The assertive region carries ONLY the one-sentence refusal: screen
       readers announce a role="alert" element's entire text content on
       insertion, so the hints (already served on focus via
       aria-describedby), the error detail, and the busy text must live
       outside it or every hint gets force-read twice. -->
  <p class="font-body-md" role="alert">
    This page was changed by another app since Silt opened it, so your save was
    held. Pick what happens next — the version you don't keep is preserved as a
    separate page in the same folder.
  </p>
  <!-- Own alert semantics: inserted only on failure, so the announcement
       fires at the moment something actually went wrong. -->
  {#if error}
    <p class="text-type-sm" role="alert">
      Couldn't resolve the conflict. Nothing was written — both versions are
      safe. Try again.
    </p>
  {/if}
  <div
    class="flex flex-wrap gap-2 items-center"
    role="group"
    aria-label="Resolve external edit"
  >
    {#each CONFLICT_RESOLUTIONS as action (action.key)}
      <button
        type="button"
        class="px-4 py-1.5 rounded-lg bg-error/20 border border-error-border text-error font-label-sm-bold hover:brightness-110 transition-all cursor-pointer disabled:opacity-50 disabled:cursor-not-allowed"
        onclick={() => onResolve(action.key)}
        disabled={busy}
        title={action.hint}
        aria-describedby={`conflict-hint-${action.key}`}
      >
        {action.label}
      </button>
    {/each}
    {#if busy}
      <span class="text-type-sm" role="status">Resolving…</span>
    {/if}
  </div>
  {#each CONFLICT_RESOLUTIONS as action (action.key)}
    <span id={`conflict-hint-${action.key}`} class="sr-only">{action.hint}</span
    >
  {/each}
</div>
