<script lang="ts">
  import { onMount, tick } from 'svelte'
  import type { TabEntry } from '../lib/tabs'
  import { fade, fly } from 'svelte/transition'
  import ContextMenu from './ContextMenu.svelte'
  import { copyPagePath, copyPageReference } from '../lib/pageActions'
  import { isDevMode, openInspect } from '../lib/devModeInspect'
  import { hiddenTabIds } from '../lib/tabOverflow'

  interface Props {
    tabs: TabEntry[]
    activeTabId: string
    onSelectTab: (id: string) => void
    onCloseTab: (id: string) => void
    onPromoteTab: (id: string) => void
    onReorderTab: (fromId: string, toId: string, before: boolean) => void
    /** When true (default), show per-tab dirty/save-failed glyphs (#167). */
    showDirtyIndicators?: boolean
  }

  let {
    tabs,
    activeTabId,
    onSelectTab,
    onCloseTab,
    onPromoteTab,
    onReorderTab,
    showDirtyIndicators = true
  }: Props = $props()

  let contextMenu = $state<{
    open: boolean
    anchor: { x: number; y: number } | null
    anchorEl: HTMLElement | null
    tab: TabEntry | null
  }>({
    open: false,
    anchor: null,
    anchorEl: null,
    tab: null
  })

  let wrapperEl = $state<HTMLDivElement | null>(null)
  let stripEl = $state<HTMLDivElement | null>(null)
  let hiddenIds = $state<string[]>([])
  let overflowButton = $state<HTMLButtonElement | null>(null)
  let overflowMenuOpen = $state(false)
  let measureFrame: number | null = null
  let hiddenTabs = $derived(tabs.filter((tab) => hiddenIds.includes(tab.id)))

  function measureOverflow() {
    if (!stripEl) return
    const bounds = stripEl.getBoundingClientRect()
    if (stripEl.scrollWidth <= stripEl.clientWidth + 1) {
      hiddenIds = []
      overflowMenuOpen = false
      return
    }
    hiddenIds = hiddenTabIds(
      { left: bounds.left, right: bounds.right - 40 },
      tabs.flatMap((tab, index) => {
        const rect = tabRefs[index]?.getBoundingClientRect()
        return rect ? [{ id: tab.id, left: rect.left, right: rect.right }] : []
      })
    )
  }

  function scheduleOverflowMeasure() {
    if (measureFrame !== null) cancelAnimationFrame(measureFrame)
    measureFrame = requestAnimationFrame(() => {
      measureFrame = null
      measureOverflow()
    })
  }

  function selectOverflowTab(id: string) {
    overflowMenuOpen = false
    onSelectTab(id)
    void tick().then(() => {
      const index = tabs.findIndex((tab) => tab.id === id)
      tabRefs[index]?.scrollIntoView?.({ block: 'nearest', inline: 'nearest' })
      scheduleOverflowMeasure()
    })
  }

  function closeOverflowTab(id: string) {
    overflowMenuOpen = false
    onCloseTab(id)
    void tick().then(scheduleOverflowMeasure)
  }

  onMount(() => {
    void tick().then(scheduleOverflowMeasure)
    const observer =
      typeof ResizeObserver === 'undefined'
        ? null
        : new ResizeObserver(scheduleOverflowMeasure)
    if (wrapperEl) observer?.observe(wrapperEl)
    if (stripEl) observer?.observe(stripEl)
    return () => {
      observer?.disconnect()
      if (measureFrame !== null) cancelAnimationFrame(measureFrame)
    }
  })

  $effect(() => {
    void tabs
    void tick().then(scheduleOverflowMeasure)
  })

  function handleTabContextMenu(e: MouseEvent, tab: TabEntry): void {
    e.preventDefault()
    e.stopPropagation()
    contextMenu = {
      open: true,
      anchor: { x: e.clientX, y: e.clientY },
      // Pass the tab button so ContextMenu can restore focus on Escape/close.
      anchorEl: e.currentTarget as HTMLElement,
      tab
    }
  }

  function closeContextMenu(): void {
    contextMenu = { open: false, anchor: null, anchorEl: null, tab: null }
  }

  function handleCloseOtherTabs(targetTabId: string): void {
    const toClose = tabs.filter((t) => t.id !== targetTabId)
    closeContextMenu()
    for (const t of toClose) {
      onCloseTab(t.id)
    }
  }

  function handleCloseTabsToRight(targetTabId: string): void {
    const idx = tabs.findIndex((t) => t.id === targetTabId)
    closeContextMenu()
    if (idx !== -1) {
      const toClose = tabs.slice(idx + 1)
      for (const t of toClose) {
        onCloseTab(t.id)
      }
    }
  }

  // Plain vault path — human-readable path only (sibling of wiki-link copy).
  async function handleCopyPagePath(tab: TabEntry): Promise<void> {
    closeContextMenu()
    await copyPagePath(tab)
  }

  // Wiki-link reference [[shortest-unique-path]] via ResolvePageLink (#545).
  async function handleCopyPageReference(tab: TabEntry): Promise<void> {
    closeContextMenu()
    await copyPageReference(tab)
  }

  // Roving tabindex: the active tab (or the first tab if none active) is the
  // only tab in the tab sequence. Arrow keys move focus between tabs without
  // consuming Tab (which the browser uses to leave the tablist).
  let focusedIndex = $state(0)

  // Keep focusedIndex in bounds and synced to the active tab.
  $effect(() => {
    const idx = tabs.findIndex((t) => t.id === activeTabId)
    if (idx !== -1) {
      focusedIndex = idx
    } else if (tabs.length > 0 && focusedIndex >= tabs.length) {
      focusedIndex = 0
    }
  })

  function tabTooltip(tab: TabEntry): string {
    const parts = [tab.notebook]
    if (tab.section) parts.push(tab.section)
    parts.push(tab.page)
    let tip = parts.join(' › ')
    if (tab.saveError) tip += ' — save failed'
    else if (tab.savePhase === 'saving') tip += ' — saving…'
    else if (tab.dirty) tip += ' — unsaved edits'
    return tip
  }

  function handleTablistKeydown(e: KeyboardEvent): void {
    if (tabs.length === 0) return
    switch (e.key) {
      case 'ArrowRight':
        e.preventDefault()
        focusedIndex = (focusedIndex + 1) % tabs.length
        focusTab(focusedIndex)
        break
      case 'ArrowLeft':
        e.preventDefault()
        focusedIndex = (focusedIndex - 1 + tabs.length) % tabs.length
        focusTab(focusedIndex)
        break
      case 'Home':
        e.preventDefault()
        focusedIndex = 0
        focusTab(0)
        break
      case 'End':
        e.preventDefault()
        focusedIndex = tabs.length - 1
        focusTab(focusedIndex)
        break
      case 'Enter':
      case ' ': {
        e.preventDefault()
        const tab = tabs[focusedIndex]
        if (tab) onSelectTab(tab.id)
        break
      }
      case 'Delete': {
        e.preventDefault()
        const tab = tabs[focusedIndex]
        if (tab) onCloseTab(tab.id)
        break
      }
    }
  }

  function focusTab(index: number): void {
    const el = tabRefs[index]
    if (el) el.focus()
  }

  // Refs for each tab button, for roving-tabindex focus management.
  let tabRefs: HTMLButtonElement[] = $state([])

  function handleAuxClick(e: MouseEvent, tab: TabEntry): void {
    // Middle-click (button 1) closes the tab — industry-standard parity.
    if (e.button === 1) {
      e.preventDefault()
      onCloseTab(tab.id)
    }
  }

  function handleDblClick(tab: TabEntry): void {
    // Double-click promotes a PREVIEW tab only; pinned tabs are no-ops.
    if (tab.preview) onPromoteTab(tab.id)
  }

  // --- Tab drag-to-reorder (#175) ---
  // dragTabId: the id of the tab being dragged. dropTabTarget: the tab
  // currently under the cursor + whether the drop indicator should show on
  // its left (before) or right (after) edge.
  let dragTabId = $state<string | null>(null)
  let dropTabTarget = $state<{ id: string; before: boolean } | null>(null)

  function handleTabDragStart(e: DragEvent, tab: TabEntry): void {
    // Don't start a drag if the user grabbed the close button — the close
    // span is a mouse-only convenience; dragging from it would be confusing.
    const target = e.target as HTMLElement
    if (target.closest('.tab-close')) {
      e.preventDefault()
      return
    }
    dragTabId = tab.id
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', tab.id)
    }
  }

  function handleTabDragOver(e: DragEvent, tab: TabEntry): void {
    if (!dragTabId || dragTabId === tab.id) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
    const rect = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const before = e.clientX < rect.left + rect.width / 2
    dropTabTarget = { id: tab.id, before }
  }

  function handleTabDragLeave(e: DragEvent): void {
    const tabEl = e.currentTarget as HTMLElement
    if (e.relatedTarget && tabEl.contains(e.relatedTarget as Node)) return
    dropTabTarget = null
  }

  function handleTabDrop(e: DragEvent, tab: TabEntry): void {
    e.preventDefault()
    e.stopPropagation()
    if (dragTabId && dragTabId !== tab.id && dropTabTarget) {
      onReorderTab(dragTabId, tab.id, dropTabTarget.before)
    }
    dragTabId = null
    dropTabTarget = null
  }

  function handleTabDragEnd(): void {
    dragTabId = null
    dropTabTarget = null
  }
</script>

{#if tabs.length > 0}
  <div
    class="tab-strip-wrapper"
    bind:this={wrapperEl}
    class:has-overflow={hiddenTabs.length > 0}
  >
    <div
      bind:this={stripEl}
      class="tab-strip"
      role="tablist"
      aria-label="Open pages"
      aria-orientation="horizontal"
      tabindex="-1"
      onkeydown={handleTablistKeydown}
      onscroll={scheduleOverflowMeasure}
    >
      {#each tabs as tab, i (tab.id)}
        <button
          in:fly={{ duration: 150, x: -8 }}
          out:fade={{ duration: 100 }}
          bind:this={tabRefs[i]}
          role="tab"
          id="silt-tab-{tab.id}"
          aria-selected={tab.id === activeTabId}
          aria-controls="silt-tabpanel"
          aria-label={tabTooltip(tab)}
          aria-haspopup="menu"
          aria-expanded={contextMenu.open && contextMenu.tab?.id === tab.id}
          tabindex={i === focusedIndex ? 0 : -1}
          title={tabTooltip(tab)}
          class="tab-button group"
          class:active={tab.id === activeTabId}
          class:preview={tab.preview}
          class:tab-drop-before={dropTabTarget?.id === tab.id &&
            dropTabTarget.before}
          class:tab-drop-after={dropTabTarget?.id === tab.id &&
            !dropTabTarget.before}
          draggable="true"
          ondragstart={(e) => handleTabDragStart(e, tab)}
          ondragover={(e) => handleTabDragOver(e, tab)}
          ondragleave={handleTabDragLeave}
          ondrop={(e) => handleTabDrop(e, tab)}
          ondragend={handleTabDragEnd}
          onclick={() => onSelectTab(tab.id)}
          onfocus={() => (focusedIndex = i)}
          onauxclick={(e) => handleAuxClick(e, tab)}
          ondblclick={() => handleDblClick(tab)}
          oncontextmenu={(e) => handleTabContextMenu(e, tab)}
        >
          {#if tab.id === activeTabId}
            <div class="active-tab-indicator"></div>
          {/if}
          <span class="tab-label" class:italic={tab.preview}>{tab.page}</span>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <!-- Close is keyboard-accessible via the parent tab's Delete and
               Ctrl+W handlers; this span is a mouse-only convenience and
               MUST NOT have role="button" (that would nest interactive
               elements inside the <button role="tab"> — HTML spec violation). -->
          <div class="tab-action-slot">
            {#if showDirtyIndicators && tab.saveError}
              <span class="tab-save-state error" aria-hidden="true">
                <span class="material-symbols-outlined text-icon-xs">error</span
                >
              </span>
            {:else if showDirtyIndicators && tab.savePhase === 'saving'}
              <span class="dirty-dot saving" aria-hidden="true"></span>
            {:else if showDirtyIndicators && tab.dirty}
              <span class="dirty-dot" aria-hidden="true"></span>
            {/if}
            <span
              aria-label="Close tab"
              title="Close tab"
              class="tab-close"
              class:has-indicator={showDirtyIndicators &&
                (tab.dirty || tab.saveError || tab.savePhase === 'saving')}
              onclick={(e) => {
                e.stopPropagation()
                onCloseTab(tab.id)
              }}
            >
              <span
                class="material-symbols-outlined text-icon-sm"
                aria-hidden="true">close</span
              >
            </span>
          </div>
        </button>
      {/each}
    </div>
    {#if hiddenTabs.length > 0}
      <button
        bind:this={overflowButton}
        type="button"
        class="tab-overflow-button"
        aria-label={`${hiddenTabs.length} hidden ${hiddenTabs.length === 1 ? 'tab' : 'tabs'}`}
        aria-haspopup="menu"
        aria-expanded={overflowMenuOpen}
        aria-controls="tab-overflow-menu"
        title={`${hiddenTabs.length} hidden ${hiddenTabs.length === 1 ? 'tab' : 'tabs'}`}
        onclick={() => (overflowMenuOpen = !overflowMenuOpen)}
      >
        <span class="material-symbols-outlined text-icon-lg" aria-hidden="true"
          >more_horiz</span
        >
      </button>
    {/if}
  </div>

  <ContextMenu
    open={overflowMenuOpen && hiddenTabs.length > 0}
    anchor={overflowButton
      ? {
          x: overflowButton.getBoundingClientRect().right,
          y: overflowButton.getBoundingClientRect().bottom
        }
      : null}
    anchorEl={overflowButton}
    onClose={() => (overflowMenuOpen = false)}
    ariaLabel="Hidden tabs"
    menuId="tab-overflow-menu"
  >
    {#each hiddenTabs as tab (tab.id)}
      {@const tabPath = `${tab.notebook}${tab.section ? ` / ${tab.section}` : ''} / ${tab.page}`}
      {@const tabStatus = tab.saveError
        ? 'save failed'
        : tab.dirty
          ? 'unsaved'
          : tab.preview
            ? 'preview'
            : 'pinned'}
      <div role="none" class="overflow-tab-row">
        <button
          type="button"
          role="menuitem"
          aria-label={`Switch to ${tabPath} — ${tabStatus}`}
          onclick={() => selectOverflowTab(tab.id)}
        >
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true"
          >
            {tab.saveError
              ? 'error'
              : tab.dirty
                ? 'circle'
                : tab.preview
                  ? 'visibility'
                  : 'description'}
          </span>
          <span class:italic={tab.preview} class="truncate">{tab.page}</span>
        </button>
        <button
          type="button"
          role="menuitem"
          class="overflow-close"
          aria-label={`Close ${tabPath} — ${tabStatus}`}
          onclick={() => closeOverflowTab(tab.id)}
        >
          <span
            class="material-symbols-outlined text-icon-sm"
            aria-hidden="true">close</span
          >
        </button>
      </div>
    {/each}
  </ContextMenu>

  <ContextMenu
    open={contextMenu.open}
    anchor={contextMenu.anchor}
    anchorEl={contextMenu.anchorEl}
    onClose={closeContextMenu}
    ariaLabel="Tab actions"
    menuId="tab-context-menu"
  >
    {#if contextMenu.tab}
      {@const targetTab = contextMenu.tab}
      {@const targetIdx = tabs.findIndex((t) => t.id === targetTab.id)}
      {@const canCloseOthers = tabs.length > 1}
      {@const canCloseToRight = targetIdx !== -1 && targetIdx < tabs.length - 1}
      <button
        type="button"
        role="menuitem"
        onclick={() => {
          // Capture id before close — closeContextMenu nulls contextMenu.tab
          // and can invalidate the {@const} binding mid-handler.
          const id = targetTab.id
          closeContextMenu()
          onCloseTab(id)
        }}
      >
        <span class="material-symbols-outlined text-icon-md">close</span>
        Close Tab
      </button>

      <button
        type="button"
        role="menuitem"
        disabled={!canCloseOthers}
        aria-disabled={!canCloseOthers}
        onclick={() => {
          if (!canCloseOthers) return
          handleCloseOtherTabs(targetTab.id)
        }}
      >
        <span class="material-symbols-outlined text-icon-md"
          >tab_unselected</span
        >
        Close Other Tabs
      </button>

      <button
        type="button"
        role="menuitem"
        disabled={!canCloseToRight}
        aria-disabled={!canCloseToRight}
        onclick={() => {
          if (!canCloseToRight) return
          handleCloseTabsToRight(targetTab.id)
        }}
      >
        <span class="material-symbols-outlined text-icon-md"
          >tab_close_right</span
        >
        Close Tabs to Right
      </button>

      <div class="context-menu-separator"></div>

      {#if targetTab.preview}
        <button
          type="button"
          role="menuitem"
          onclick={() => {
            const id = targetTab.id
            closeContextMenu()
            onPromoteTab(id)
          }}
        >
          <span class="material-symbols-outlined text-icon-md">push_pin</span>
          Pin Tab
        </button>
        <div class="context-menu-separator"></div>
      {/if}

      <button
        type="button"
        role="menuitem"
        onclick={() => handleCopyPagePath(targetTab)}
      >
        <span class="material-symbols-outlined text-icon-md">content_copy</span>
        Copy Page Path
      </button>
      <button
        type="button"
        role="menuitem"
        onclick={() => handleCopyPageReference(targetTab)}
      >
        <span class="material-symbols-outlined text-icon-md">link</span>
        Copy Page Reference
      </button>
      {#if isDevMode()}
        <div class="context-menu-separator" aria-hidden="true"></div>
        <button
          type="button"
          role="menuitem"
          onclick={() => {
            closeContextMenu()
            void openInspect()
          }}
        >
          <span
            class="material-symbols-outlined text-icon-md"
            aria-hidden="true">bug_report</span
          >
          Inspect
        </button>
      {/if}
    {/if}
  </ContextMenu>
{/if}

<style>
  .tab-strip-wrapper {
    display: flex;
    align-items: stretch;
    justify-content: space-between;
    height: 36px;
    min-height: 36px;
    background: transparent;
    flex: 1;
    min-width: 0;
    position: relative;
    z-index: 10;
    top: 1px;
  }
  .tab-strip-wrapper.has-overflow::after {
    content: '';
    position: absolute;
    right: 2.35rem;
    top: 0;
    bottom: 0;
    width: 2rem;
    pointer-events: none;
    background: linear-gradient(
      to right,
      transparent,
      var(--color-surface-titlebar)
    );
  }
  .tab-overflow-button {
    position: absolute;
    right: 0.25rem;
    top: 0.25rem;
    z-index: 3;
    width: 2rem;
    height: 2rem;
    border: 1px solid var(--color-surface-titlebar-border);
    border-radius: 0.5rem;
    background: color-mix(
      in srgb,
      var(--color-surface-titlebar) 94%,
      transparent
    );
    color: var(--color-surface-titlebar-text-muted);
    cursor: pointer;
    display: grid;
    place-items: center;
  }
  .tab-overflow-button:hover,
  .tab-overflow-button:focus-visible {
    color: var(--color-accent-primary-start);
    outline: 2px solid
      color-mix(in srgb, var(--color-accent-primary-start) 55%, transparent);
  }
  .overflow-tab-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
  }
  :global([data-context-menu-root] .overflow-tab-row .overflow-close) {
    width: auto;
    padding-inline: 0.45rem;
  }

  .tab-strip {
    flex: 1;
    display: flex;
    align-items: stretch;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none; /* Hide scrollbar Firefox */
    padding: 0 12px;
    gap: 0;
    /* Mask to fade out scroll edges */
    -webkit-mask-image: linear-gradient(
      to right,
      transparent,
      black 12px,
      black calc(100% - 12px),
      transparent
    );
    mask-image: linear-gradient(
      to right,
      transparent,
      black 12px,
      black calc(100% - 12px),
      transparent
    );
  }

  .tab-strip::-webkit-scrollbar {
    display: none; /* Hide scrollbar Webkit */
  }

  .tab-button {
    display: flex;
    align-items: center;
    gap: 4px;
    padding: 0 8px 0 12px;
    min-width: 100px;
    max-width: 200px;
    height: calc(100% - 4px);
    margin-top: 4px;
    border: none;
    background: transparent;
    color: var(--color-surface-sidebar-text-muted);
    font-family: var(--font-body, inherit);
    font-size: 12px;
    cursor: pointer;
    transition:
      background-color 120ms ease,
      color 120ms ease,
      height 120ms ease,
      margin-top 120ms ease;
    white-space: nowrap;
    position: relative;
    border-radius: 6px 6px 0 0;
  }

  /* Subtle vertical divider between tabs */
  .tab-button::before {
    content: '';
    position: absolute;
    left: 0;
    top: 25%;
    height: 50%;
    width: 1px;
    background: var(--color-surface-sidebar-border);
    transition: opacity 120ms ease;
  }

  /* Hide the divider for the active tab, the tab immediately following it, or hovered tabs */
  .tab-button.active::before,
  .tab-button.active + .tab-button::before,
  .tab-button:hover::before,
  .tab-button:hover + .tab-button::before {
    opacity: 0;
  }

  .tab-button:hover {
    background: var(--color-hover);
    color: var(--color-surface-sidebar-text);
  }

  .tab-button:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: -2px;
  }

  .tab-button.active {
    color: var(--color-accent-primary-start);
    background: var(--color-surface-editor);
    height: 100%;
    margin-top: 0;
    border: 1px solid var(--color-surface-editor-border);
    border-bottom: none;
    z-index: 2;
  }

  .tab-label {
    overflow: hidden;
    text-overflow: ellipsis;
    flex: 1;
  }

  .tab-button.preview .tab-label {
    font-style: italic;
  }

  .tab-action-slot {
    position: relative;
    width: 18px;
    height: 18px;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
  }

  .dirty-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background-color: var(--color-accent-primary-start);
    transition:
      transform 120ms ease,
      opacity 120ms ease;
  }

  /* In-flight save: the dot pulses so an actively-writing tab is visibly
     distinct from a merely-dirty (debouncing) one (#546). */
  .dirty-dot.saving {
    animation: silt-tab-saving-pulse 1.1s ease-in-out infinite;
  }

  @keyframes silt-tab-saving-pulse {
    0%,
    100% {
      opacity: 1;
      transform: scale(1);
    }
    50% {
      opacity: 0.35;
      transform: scale(0.7);
    }
  }

  .tab-close {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 4px;
    color: inherit;
    cursor: pointer;
    transition:
      opacity 120ms ease,
      background-color 120ms ease,
      transform 120ms ease;
    flex-shrink: 0;
  }

  /* Default state: if tab has dirty/error indicator, hide close button and show indicator */
  .tab-button .tab-close.has-indicator {
    opacity: 0;
    pointer-events: none;
    transform: scale(0.6);
  }

  /* Preview tabs: hide close button by default if not dirty */
  .tab-button.preview .tab-close:not(.has-indicator) {
    opacity: 0;
    pointer-events: none;
  }

  /* Pinned tabs: show close button by default if not dirty */
  .tab-button:not(.preview) .tab-close:not(.has-indicator) {
    opacity: 0.5;
    pointer-events: auto;
  }

  /* Hover state on the tab button */
  .tab-button:hover .dirty-dot {
    opacity: 0;
    transform: scale(0);
  }

  .tab-button:hover .tab-save-state.error {
    opacity: 0;
    transform: scale(0);
  }

  .tab-button:hover .tab-close {
    opacity: 0.5;
    pointer-events: auto;
    transform: scale(1);
  }

  /* Hover state directly on the close button */
  .tab-close:hover {
    opacity: 1 !important;
    background: var(--color-hover);
  }

  /* Tab drag-to-reorder drop indicators (#175). A vertical accent line at
     the left/right edge of the hovered tab, matching the sidebar's
     drag-over-top/bottom style for visual consistency. */
  .tab-button.tab-drop-before::before {
    content: '';
    position: absolute;
    left: -1px;
    top: 4px;
    bottom: 4px;
    width: 2px;
    background: var(--color-accent-primary-start);
    border-radius: 1px;
    z-index: 10;
    opacity: 1 !important;
    height: auto;
  }

  .tab-button.tab-drop-after::after {
    content: '';
    position: absolute;
    right: -1px;
    top: 4px;
    bottom: 4px;
    width: 2px;
    background: var(--color-accent-primary-start);
    border-radius: 1px;
    z-index: 10;
  }

  /* Per-tab dirty/save-state indicators (#167). The dirty dot uses CSS shapes;
     the error glyph uses --status-danger. Both are accessible via tooltips/aria-labels. */
  .tab-save-state {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    flex-shrink: 0;
    line-height: 1;
    transition:
      transform 120ms ease,
      opacity 120ms ease;
  }

  .tab-save-state.error {
    color: var(--color-status-danger);
  }

  .active-tab-indicator {
    position: absolute;
    top: 0;
    left: 0;
    right: 0;
    height: 2px;
    background: linear-gradient(
      90deg,
      var(--color-accent-primary-start),
      var(--color-accent-primary-end)
    );
    border-radius: 6px 6px 0 0;
  }
</style>
