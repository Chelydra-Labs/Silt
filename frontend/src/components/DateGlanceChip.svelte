<script lang="ts">
  // Status-bar chip that opens the Date Glance popover (#730). One of three
  // openers (chip, global hotkey, /calendar). The chip owns the popover's
  // anchor element — it registers itself on mount so every opener surfaces the
  // popover in the same place.
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
    setDateGlanceAnchor(chipEl)
    return () => {
      // Clear the anchor on unmount so the popover doesn't position against
      // a detached node (Fast Refresh, conditional render, etc.).
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
    openDateGlance(capturedEditor ?? getLastActiveEditor())
    capturedEditor = null
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
