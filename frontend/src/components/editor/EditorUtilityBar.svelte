<script module lang="ts">
  export const OPEN_TASKS_FOR_PAGE_EVENT = 'silt:open-tasks-for-page' as const

  export interface OpenTasksForPageDetail {
    source: string
    notebook: string
    section: string
    page: string
    nonce: string
  }

  declare global {
    interface WindowEventMap {
      'silt:open-tasks-for-page': CustomEvent<OpenTasksForPageDetail>
    }
  }
</script>

<script lang="ts">
  import type { Editor } from 'svelte-tiptap'
  import FormatToolbar from './FormatToolbar.svelte'
  import { settings } from '../../settings/store.svelte'
  import { isSystemDark } from '../../lib/systemTheme.svelte'

  // EditorUtilityBar — extracted from VirtualScrollContainer (#202).
  // Now simply acts as a container for FormatToolbar since action controls
  // (View Mode, Zen Mode, Focus Mode) have been relocated to the TabStrip.

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

  @container editor-utility-bar (max-width: 680px) {
    .page-action span:last-child {
      display: none;
    }

    .page-action {
      padding-inline: 6px;
    }
  }
</style>
