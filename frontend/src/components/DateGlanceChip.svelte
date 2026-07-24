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
    setDateGlanceAnchor
  } from '../lib/dateGlanceState.svelte'
  import { getActiveEditor } from '../lib/editor/activeEditor.svelte'
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
  })

  function onPointerDown(): void {
    capturedEditor = getActiveEditor()
  }

  function onClick(): void {
    openDateGlance(capturedEditor)
    capturedEditor = null
  }
</script>

<button
  type="button"
  bind:this={chipEl}
  onpointerdown={onPointerDown}
  onclick={onClick}
  aria-label="Open date glance ({label})"
  title="Date glance"
  class="flex items-center gap-1.5 rounded-md border border-transparent px-2 py-0.5 text-type-xs text-text-muted hover:bg-hover hover:text-text-primary focus-visible:ring-2 focus-visible:ring-accent-primary-start focus-visible:outline-none cursor-pointer"
>
  <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
    >calendar_month</span
  >
  <span>{label}</span>
</button>
