<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import {
    OPEN_TASKS_FOR_PAGE_EVENT,
    type OpenTasksForPageDetail
  } from './openTasksForPage'
  import FormatToolbar from './FormatToolbar.svelte'
  import { settings } from '../../settings/store.svelte'
  import { isSystemDark } from '../../lib/systemTheme.svelte'
  import {
    noteZoom,
    NOTE_ZOOM_MAX,
    NOTE_ZOOM_MIN
  } from '../../lib/noteZoom.svelte'

  // EditorUtilityBar — extracted from VirtualScrollContainer (#202).
  // FormatToolbar + page actions + note page zoom (#843).

  interface Props {
    editor: Editor | null
    activeMarks: Set<string>
    pageLocator?: Omit<OpenTasksForPageDetail, 'nonce'> | null
    showFormatting?: boolean
  }

  let {
    editor,
    activeMarks,
    pageLocator = null,
    showFormatting = true
  }: Props = $props()

  let isDark = $derived(isSystemDark())
  let colorEnabled = $derived(
    settings.config?.ui?.formatting?.color_enabled !== false
  )

  function freshNonce(): string {
    return typeof crypto !== 'undefined' && 'randomUUID' in crypto
      ? crypto.randomUUID()
      : `page-tasks-${Date.now()}-${Math.random().toString(36).slice(2)}`
  }

  function openTasksForPage(): void {
    if (!pageLocator) return
    window.dispatchEvent(
      new CustomEvent<OpenTasksForPageDetail>(OPEN_TASKS_FOR_PAGE_EVENT, {
        detail: { ...pageLocator, nonce: freshNonce() }
      })
    )
  }
</script>

<div class="unified-utility-bar">
  {#if showFormatting}
    <FormatToolbar {editor} {activeMarks} {isDark} {colorEnabled} />
  {/if}
  {#if pageLocator}
    {#if showFormatting}
      <span class="page-action-divider" aria-hidden="true"></span>
    {/if}
    <button
      type="button"
      class="page-action font-label-sm text-type-sm"
      class:solo={!showFormatting}
      onclick={openTasksForPage}
      aria-label="Open tasks on this page"
      title="Open tasks on this page"
    >
      <span class="material-symbols-outlined" aria-hidden="true">checklist</span
      >
      <span>Page tasks</span>
    </button>
  {/if}

  <div
    class="zoom-cluster"
    class:push-end={!pageLocator}
    role="group"
    aria-label="Page zoom"
  >
    <button
      type="button"
      class="zoom-btn"
      onclick={() => noteZoom.zoomOut()}
      disabled={noteZoom.factor <= NOTE_ZOOM_MIN}
      aria-label="Zoom out"
      title="Zoom out (Ctrl+scroll)"
    >
      <span class="material-symbols-outlined" aria-hidden="true">zoom_out</span>
    </button>
    <button
      type="button"
      class="zoom-percent font-label-sm text-type-sm"
      onclick={() => noteZoom.reset()}
      aria-label="Reset zoom to 100%"
      title="Reset zoom (Ctrl+scroll to zoom)"
    >
      {noteZoom.percent}%
    </button>
    <button
      type="button"
      class="zoom-btn"
      onclick={() => noteZoom.zoomIn()}
      disabled={noteZoom.factor >= NOTE_ZOOM_MAX}
      aria-label="Zoom in"
      title="Zoom in (Ctrl+scroll)"
    >
      <span class="material-symbols-outlined" aria-hidden="true">zoom_in</span>
    </button>
  </div>
</div>

<style>
  .unified-utility-bar {
    display: flex;
    align-items: center;
    min-height: 38px;
    height: auto;
    padding: 4px 16px;
    background: color-mix(in srgb, var(--color-surface-panel) 95%, transparent);
    backdrop-filter: blur(8px);
    border-bottom: 1px solid var(--color-surface-panel-border);
    flex-shrink: 0;
    z-index: 15;
    min-width: 0;
    max-width: 100%;
    overflow: visible;
    /* Enable container queries for FormatToolbar label collapse at ≤600px. */
    container-type: inline-size;
    container-name: editor-utility-bar;
  }

  .page-action-divider {
    width: 1px;
    height: 22px;
    margin-left: auto;
    background: var(--color-surface-panel-border);
    flex: 0 0 auto;
  }

  .page-action {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 28px;
    padding: 3px 9px;
    border: 1px solid var(--color-surface-panel-border);
    border-radius: 7px;
    background: transparent;
    color: var(--color-text-muted);
    white-space: nowrap;
    cursor: pointer;
    transition:
      color 120ms ease,
      background 120ms ease,
      border-color 120ms ease;
  }

  .page-action:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 36%,
      var(--color-surface-panel-border)
    );
  }

  .page-action.solo {
    margin-left: auto;
  }

  .page-action:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .page-action .material-symbols-outlined {
    font-size: 18px;
  }

  .zoom-cluster {
    display: inline-flex;
    align-items: center;
    gap: 2px;
    flex: 0 0 auto;
    margin-left: 8px;
  }

  /* No format toolbar / page tasks: park zoom on the trailing edge. */
  .zoom-cluster.push-end {
    margin-left: auto;
  }

  /* When page tasks already used margin-left:auto, keep zoom after it. */
  .page-action + .zoom-cluster,
  .page-action-divider + .page-action + .zoom-cluster {
    margin-left: 8px;
  }

  .zoom-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 28px;
    height: 28px;
    padding: 0;
    border: 1px solid transparent;
    border-radius: 7px;
    background: transparent;
    color: var(--color-text-muted);
    cursor: pointer;
    transition:
      color 120ms ease,
      background 120ms ease,
      border-color 120ms ease;
  }

  .zoom-btn:hover:not(:disabled) {
    color: var(--color-text-primary);
    background: var(--color-hover);
    border-color: color-mix(
      in srgb,
      var(--color-accent-primary-start) 36%,
      var(--color-surface-panel-border)
    );
  }

  .zoom-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }

  .zoom-btn:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .zoom-btn .material-symbols-outlined {
    font-size: 18px;
  }

  .zoom-percent {
    min-width: 3.25rem;
    min-height: 28px;
    padding: 3px 4px;
    border: 1px solid transparent;
    border-radius: 7px;
    background: transparent;
    color: var(--color-text-muted);
    text-align: center;
    cursor: pointer;
    transition:
      color 120ms ease,
      background 120ms ease;
  }

  .zoom-percent:hover {
    color: var(--color-text-primary);
    background: var(--color-hover);
  }

  .zoom-percent:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  @container editor-utility-bar (max-width: 680px) {
    .page-action span:last-child {
      display: none;
    }

    .page-action {
      padding-inline: 6px;
    }
  }
</style>
