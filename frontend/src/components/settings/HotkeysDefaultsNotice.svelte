<script lang="ts">
  // HotkeysDefaultsNotice — a one-time banner shown in Settings -> Hotkeys after
  // the v1 default-keymap realignment (#868) migrated a vault's legacy chords.
  // The Go normalizer stamps `hotkeys_defaults_v1_notice` into ui.dismissed_tips
  // when it rewrites a legacy chord; this banner surfaces that change so a user
  // reviewing their shortcuts sees why chords moved (Ctrl+B -> bold, sidebar ->
  // Ctrl+\, tab/view-mode relocations). Acknowledgement appends a separate ack
  // stamp via the existing appendDismissedTip helper (add-only, atomic), so no
  // removal IPC is needed. Presentational + prop-driven for easy testing.
  // role="status" aria-live="polite" so screen readers announce it.

  interface Props {
    dismissed: boolean
    onDismiss: () => void
  }

  let { dismissed, onDismiss }: Props = $props()
</script>

{#if !dismissed}
  <div
    class="flex items-start gap-2 p-3 rounded-lg bg-accent-primary-start/10 border border-accent-primary-start/30 text-accent-primary-start text-type-sm font-body-md"
    role="status"
    aria-live="polite"
    data-testid="hotkeys-defaults-notice"
  >
    <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
      >keyboard</span
    >
    <span class="flex-1">
      Default shortcuts were updated to standard editor conventions — Ctrl+B is
      bold everywhere (sidebar is now Ctrl+\), tab navigation moved to
      Ctrl+Alt+Arrow / Ctrl+Shift+W, and the view-mode toggle moved to
      Ctrl+Alt+R. Review or remap them below.
    </span>
    <button
      type="button"
      onclick={onDismiss}
      aria-label="Dismiss notice"
      class="font-label-sm-bold underline hover:brightness-110 bg-transparent border-none cursor-pointer text-accent-primary-start"
    >
      Got it
    </button>
  </div>
{/if}
