<script lang="ts">
  // Shared toggle switch used across Settings → AI and the AI plugins.
  //
  // The knob fills with var(--color-surface-app) so it reads correctly in both
  // light and dark themes. An earlier per-file copy hardcoded #ffffff, which
  // vanished against the light-on-dark track in dark mode; consolidating here
  // makes the theme-aware value the single source of truth.

  import type { Snippet } from 'svelte'

  interface Props {
    /** Two-way bindable checked state of the underlying checkbox. */
    checked?: boolean
    /** Disables the checkbox and dims the track. */
    disabled?: boolean
    /** Overrides the track's "on" look; falls back to `checked`. */
    trackOn?: boolean
    /** Checkbox id; one is generated when omitted. */
    id?: string
    /** Classes for the wrapping <label>. */
    labelClass?: string
    /** Tooltip on the wrapping <label>. */
    title?: string
    /** Forwarded to the underlying checkbox on each change. */
    onchange?: (
      event: Event & { currentTarget: EventTarget & HTMLInputElement }
    ) => void
    /** Rich label content (title, description, optional info affordance). */
    children?: Snippet
    [key: string]: unknown
  }

  let {
    checked = $bindable(false),
    disabled = false,
    trackOn,
    id,
    labelClass = '',
    title,
    onchange,
    children,
    ...rest
  }: Props = $props()

  // Stable per-instance fallback so <label>↔<input> association works even when
  // the caller omits id. All in-tree call sites pass an explicit id.
  const fallbackId = `toggle-switch-${crypto.randomUUID()}`
  const inputId = $derived(id ?? fallbackId)
  const onTrack = $derived(trackOn ?? checked)
</script>

<label class={labelClass} {title}>
  {@render children?.()}
  <input
    id={inputId}
    type="checkbox"
    class="toggle-switch sr-only"
    bind:checked
    {disabled}
    {onchange}
    {...rest}
  />
  <span
    aria-hidden="true"
    class="toggle-switch-track"
    class:on={onTrack}
    class:disabled
  ></span>
</label>

<style>
  .toggle-switch-track {
    width: 36px;
    height: 20px;
    border-radius: 9999px;
    background: var(--color-surface-panel-border);
    position: relative;
    flex-shrink: 0;
    margin-top: 2px;
    transition: background-color 0.15s ease;
  }
  .toggle-switch-track.on {
    background: var(--color-accent-primary-start);
  }
  .toggle-switch-track.disabled {
    opacity: 0.5;
    cursor: not-allowed;
  }
  .toggle-switch-track::after {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    border-radius: 9999px;
    background: var(--color-surface-app);
    transition: transform 0.15s ease;
  }
  .toggle-switch-track.on::after {
    transform: translateX(16px);
  }
  .toggle-switch:focus-visible + .toggle-switch-track {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }
</style>
