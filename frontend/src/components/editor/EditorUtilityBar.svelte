<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import {
    OPEN_TASKS_FOR_PAGE_EVENT,
    type OpenTasksForPageDetail
  } from './openTasksForPage'
  import { OPEN_PAGE_HISTORY_EVENT } from './openPageHistory'
  import FormatToolbar from './FormatToolbar.svelte'
  import { settings } from '../../settings/store.svelte'
  import { isSystemDark } from '../../lib/systemTheme.svelte'

  // EditorUtilityBar — extracted from VirtualScrollContainer (#202).
  // FormatToolbar + page actions. Note page zoom lives in the bottom status pill.

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

  function openPageHistory(): void {
    if (!pageLocator) return
    window.dispatchEvent(
      new CustomEvent(OPEN_PAGE_HISTORY_EVENT, {
        detail: {
          notebook: pageLocator.notebook,
          section: pageLocator.section,
          page: pageLocator.page,
          nonce: freshNonce()
        }
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
    <div class="page-actions" class:solo={!showFormatting}>
      <button
        type="button"
        class="page-action font-label-sm text-type-sm"
        onclick={openTasksForPage}
        aria-label="Open tasks on this page"
        title="Open tasks on this page"
      >
        <span class="material-symbols-outlined" aria-hidden="true"
          >checklist</span
        >
        <span>Page tasks</span>
      </button>
      <button
        type="button"
        class="page-action font-label-sm text-type-sm"
        onclick={openPageHistory}
        aria-label="Open page history"
        title="Page history"
      >
        <span class="material-symbols-outlined" aria-hidden="true">history</span
        >
        <span>Page history</span>
      </button>
    </div>
  {/if}
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

  .page-actions {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    flex: 0 0 auto;
  }

  .page-actions.solo {
    margin-left: auto;
  }

  .page-action:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .page-action .material-symbols-outlined {
    font-size: 18px;
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
