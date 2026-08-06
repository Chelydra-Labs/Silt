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
  //
  // The tab-chord text is derived from the resolved hotkey map rather than
  // hardcoded (#863): on Linux/macOS the v1 migration does NOT rewrite
  // next_tab/prev_tab (Ctrl+Tab works there), so claiming "tab navigation moved
  // to Ctrl+Alt+Arrow" would contradict the table on every non-Windows vault.
  // Passing the resolved map in keeps the notice consistent with the
  // ShortcutHelp table and the HotkeysTab grid, which all derive from the same
  // source.
  import { shortcutBinding } from '../../settings/shortcutActions'

  interface Props {
    dismissed: boolean
    onDismiss: () => void
    hotkeys: Record<string, string | undefined>
  }

  let { dismissed, onDismiss, hotkeys }: Props = $props()

  let nextTabChord = $derived(shortcutBinding('next_tab', hotkeys))
  let prevTabChord = $derived(shortcutBinding('prev_tab', hotkeys))
  let closeTabChord = $derived(shortcutBinding('close_tab', hotkeys))
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
      bold everywhere (sidebar is now Ctrl+\), tab navigation uses
      {nextTabChord} / {prevTabChord}, close with {closeTabChord}, and the
      view-mode toggle moved to Ctrl+Alt+R. Review or remap them below.
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
