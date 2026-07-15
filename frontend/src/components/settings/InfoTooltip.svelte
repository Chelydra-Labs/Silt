<script lang="ts" module>
  // Only one tooltip open at a time so hover transitions stay clean.
  let activeId: string | null = $state(null)
  let nextId = 0
  function allocId(): string {
    nextId += 1
    return `info-tooltip-${nextId}`
  }
</script>

<script lang="ts">
  interface Props {
    /** Plain-language explanation. */
    text: string
    /** Technical term + range, shown italicized. */
    technical?: string
    /** aria-label for the icon button (e.g. "What is Answer Style?"). */
    label: string
  }

  let { text, technical, label }: Props = $props()

  const id = allocId()
  const contentId = `${id}-content`
  let open = $derived(activeId === id)
  let rootEl: HTMLSpanElement | undefined = $state()
  // When pinned by click, hover-exit should not close the tooltip.
  let pinned = false

  function show() {
    pinned = false
    activeId = id
  }

  function hide() {
    if (pinned) return
    if (activeId === id) activeId = null
  }

  function forceHide() {
    pinned = false
    if (activeId === id) activeId = null
  }

  function toggle(e: MouseEvent) {
    e.preventDefault()
    e.stopPropagation()
    if (open) {
      forceHide()
    } else {
      pinned = true
      activeId = id
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape' && open) {
      e.preventDefault()
      e.stopPropagation()
      forceHide()
      ;(e.currentTarget as HTMLElement)?.blur()
    }
  }

  function onDocPointerDown(e: PointerEvent) {
    if (!open || !rootEl) return
    if (rootEl.contains(e.target as Node)) return
    forceHide()
  }

  $effect(() => {
    if (!open) return
    document.addEventListener('pointerdown', onDocPointerDown, true)
    return () =>
      document.removeEventListener('pointerdown', onDocPointerDown, true)
  })
</script>

<span class="info-tooltip relative inline-flex align-middle" bind:this={rootEl}>
  <button
    type="button"
    class="info-tooltip-btn inline-flex items-center justify-center rounded-full text-text-muted hover:text-text-primary focus:outline-none focus-visible:ring-2 focus-visible:ring-accent-primary-start/60 p-0.5"
    aria-label={label}
    aria-describedby={open ? contentId : undefined}
    aria-expanded={open}
    onmouseenter={show}
    onmouseleave={hide}
    onfocus={show}
    onblur={forceHide}
    onclick={toggle}
    onkeydown={onKeydown}
  >
    <span class="material-symbols-outlined text-icon-sm" aria-hidden="true"
      >info</span
    >
  </button>
  {#if open}
    <span
      id={contentId}
      role="tooltip"
      class="info-tooltip-popover absolute left-1/2 top-full z-50 mt-1.5 w-max max-w-xs -translate-x-1/2 rounded-lg border border-surface-panel-border bg-surface-panel px-3 py-2 text-left shadow-lg"
    >
      <span
        class="block text-type-xs text-text-primary font-label-sm leading-snug"
        >{text}</span
      >
      {#if technical}
        <span
          class="block mt-1 text-type-2xs text-text-muted italic leading-snug"
          >{technical}</span
        >
      {/if}
    </span>
  {/if}
</span>

<style>
  .info-tooltip-btn :global(.material-symbols-outlined) {
    font-size: 1rem;
    line-height: 1;
  }
</style>
