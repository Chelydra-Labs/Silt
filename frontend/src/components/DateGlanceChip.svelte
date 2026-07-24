<script lang="ts">
  // Status-bar chip that opens the Date Glance popover. One of three
  // openers (chip, global hotkey, /calendar). The chip registers itself as the
  // persistent placement anchor only while its tab is active — every open tab
  // keeps a VirtualScrollContainer mounted (inactive ones are display:none),
  // and a hidden chip's getBoundingClientRect is 0×0 (popover → top-left).
  //
  // Chip clicks always pass this button as `element` so placement tracks the
  // control the user actually pressed, not whichever chip registered last.
  //
  // Editor capture timing: clicking the chip moves focus to the button, which
  // blurs the editor. pointerdown fires before that blur, so the editor is
  // captured there; the popover then opens on click using the captured value.
  import {
    openDateGlance,
    setDateGlanceAnchor,
    dateGlance
  } from '../lib/dateGlanceState.svelte'
  import {
    getActiveEditor,
    getLastActiveEditor
  } from '../lib/editor/activeEditor.svelte'
  import type { Editor } from '@tiptap/core'

  interface Props {
    /** False when this chip lives in an inactive (display:none) tab panel. */
    active?: boolean
  }

  let { active = true }: Props = $props()

  let chipEl = $state<HTMLElement | null>(null)
  // Captured on pointerdown (before blur); consumed on click.
  let capturedEditor: Editor | null = null

  let label = $derived(
    new Date().toLocaleDateString(undefined, {
      month: 'short',
      day: 'numeric'
    })
  )

  $effect(() => {
    // Only the active tab's chip may own the global fallback anchor (hotkey
    // path when caret coords are unavailable).
    if (active && chipEl) {
      setDateGlanceAnchor(chipEl)
    } else if (dateGlance.anchor === chipEl) {
      setDateGlanceAnchor(null)
    }
    return () => {
      // Clear on unmount / dependency change so we never leave a detached or
      // hidden node registered.
      if (dateGlance.anchor === chipEl) setDateGlanceAnchor(null)
    }
  })

  function onPointerDown(): void {
    capturedEditor = getActiveEditor()
  }

  function onClick(): void {
    // capturedEditor is set by pointerdown (mouse path). Keyboard activation
    // (Tab + Enter) never fires pointerdown, so fall back to the last editor
    // that had focus for a11y parity with the mouse path.
    const editor = capturedEditor ?? getLastActiveEditor()
    capturedEditor = null
    if (!chipEl) {
      console.error('[silt] date-glance chip click without element')
      return
    }
    // Pass this chip explicitly — do not use "last registered" global anchor
    // (another tab's hidden chip may own that slot).
    openDateGlance(editor, { element: chipEl })
  }
</script>

<button
  type="button"
  bind:this={chipEl}
  onpointerdown={onPointerDown}
  onclick={onClick}
  aria-label="Pick a date"
  title="Pick a date — {label}"
  class="h-8 w-8 flex items-center justify-center rounded-full transition-colors border-none bg-transparent cursor-pointer focus:outline-none hover:bg-hover text-text-muted focus-visible:ring-2 focus-visible:ring-accent-primary-start"
>
  <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
    >calendar_month</span
  >
</button>
