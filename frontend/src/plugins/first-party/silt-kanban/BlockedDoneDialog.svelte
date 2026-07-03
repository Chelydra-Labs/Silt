<script lang="ts">
  import { onMount, tick } from 'svelte'

  /**
   * Confirmation dialog shown when a user moves a blocked task to DONE (#302).
   * Lists the open prerequisite tasks so the user can make an informed choice,
   * then either persists the DONE transition (onConfirm) or reverts it
   * (onCancel). Glassy overlay + focus trap follow the SettingsShell pattern.
   */
  interface Blocker {
    id: string
    clean_content?: string
  }

  let {
    cardText,
    blockers,
    onConfirm,
    onCancel
  }: {
    cardText: string
    blockers: Blocker[]
    onConfirm: () => void
    onCancel: () => void
  } = $props()

  let dialogRef = $state<HTMLDivElement | null>(null)
  let previouslyFocused: HTMLElement | null = null

  const FOCUSABLE =
    'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!dialogRef) return []
    return Array.from(dialogRef.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      onCancel()
      return
    }
    if (e.key === 'Tab' && dialogRef) {
      const els = focusableEls()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !dialogRef.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else {
        if (active === last) {
          e.preventDefault()
          first.focus()
        }
      }
    }
  }

  onMount(() => {
    previouslyFocused = document.activeElement as HTMLElement | null
    window.addEventListener('keydown', handleKeydown)
    // Focus the confirm button once rendered so keyboard users land on the
    // affirmative action (the conventional default for a confirm dialog).
    tick().then(() => {
      const els = focusableEls()
      // Cancel is the first button in DOM order; focus the confirm (second)
      // so Enter confirms, matching native confirm() semantics.
      const confirmBtn = dialogRef?.querySelector<HTMLButtonElement>(
        '[data-action="confirm"]'
      )
      ;(confirmBtn ?? els[0])?.focus()
    })
    return () => {
      window.removeEventListener('keydown', handleKeydown)
      previouslyFocused?.focus?.()
    }
  })
</script>

<!-- Backdrop: a sibling button so the click is keyboard/AT reachable but
     excluded from the tab order. -->
<div
  class="fixed inset-0 z-[190] flex items-center justify-center bg-black/40 backdrop-blur-[2px]"
>
  <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
  <button
    tabindex="-1"
    aria-label="Cancel complete"
    class="absolute inset-0 cursor-default border-none bg-transparent p-0"
    onclick={onCancel}
  ></button>
  <div
    bind:this={dialogRef}
    role="alertdialog"
    aria-modal="true"
    aria-labelledby="blocked-done-title"
    aria-describedby="blocked-done-desc"
    tabindex="-1"
    class="relative w-full max-w-md rounded-xl border border-border-active shadow-2xl flex flex-col"
    style="backdrop-filter: blur(16px) saturate(140%); background: color-mix(in srgb, var(--color-panel) 92%, transparent);"
  >
    <div class="p-5 flex items-start gap-3">
      <span
        class="material-symbols-outlined text-status-warn text-[24px] mt-0.5"
        aria-hidden="true">lock</span
      >
      <div class="min-w-0 flex-1">
        <h2
          id="blocked-done-title"
          class="text-text-primary font-label-md text-base mb-1"
        >
          Complete blocked task?
        </h2>
        <p id="blocked-done-desc" class="text-text-muted text-sm font-body-md">
          “{cardText}” is blocked by {blockers.length}
          {blockers.length === 1 ? 'task' : 'tasks'} that {blockers.length === 1
            ? 'is'
            : 'are'} not done:
        </p>
      </div>
    </div>
    <ul class="px-5 pb-3 space-y-1 max-h-40 overflow-y-auto">
      {#each blockers as b}
        <li
          class="text-sm text-text-primary font-body-md flex items-center gap-1.5"
        >
          <span
            class="material-symbols-outlined text-status-warn text-[14px]"
            aria-hidden="true">lock</span
          >
          <span class="truncate">{b.clean_content ?? '(untitled task)'}</span>
        </li>
      {/each}
    </ul>
    <div class="flex justify-end gap-2 px-5 py-3 border-t border-border-muted">
      <button
        data-action="cancel"
        class="px-3 py-1.5 rounded-md text-sm font-label-sm text-text-primary border border-border-zinc hover:bg-hover transition-colors"
        onclick={onCancel}
      >
        Cancel
      </button>
      <button
        data-action="confirm"
        class="px-3 py-1.5 rounded-md text-sm font-label-sm text-void bg-status-warn hover:opacity-90 transition-opacity"
        onclick={onConfirm}
      >
        Complete anyway
      </button>
    </div>
  </div>
</div>
