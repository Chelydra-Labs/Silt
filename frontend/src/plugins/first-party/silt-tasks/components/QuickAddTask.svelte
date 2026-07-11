<script lang="ts">
  // QuickAddTask — a compact title-only input that creates a standalone task
  // (#368). Shared by the calendar (day-cell + toolbar), the kanban
  // (per-column + inbox), and the global Ctrl+Shift+N overlay.
  //
  // Title-only is the v1 surface: sensible defaults (status TODO, no due date
  // unless the surface supplied one) match the issue's acceptance criteria.
  // Rich editing stays on the existing task/note editor.
  //
  // A plugin surface passes `ctx` (uses ctx.createTask). The app-shell global
  // overlay has no plugin ctx, so it passes `createTask` directly — a thin
  // shim over the app-level CreateStandaloneTask binding. One component, one
  // source of truth for the Enter/Escape/busy/error behavior.
  import type { PluginContext, TaskStatus } from '../../../sdk'
  import ErrorBanner from './ErrorBanner.svelte'

  interface Props {
    /** Plugin context (plugin surfaces). Mutually exclusive with createTask. */
    ctx?: PluginContext
    /** App-level create shim (global overlay). Mutually exclusive with ctx. */
    createTask?: (opts: {
      title: string
      dueDate?: string
      status?: TaskStatus
    }) => Promise<string>
    /** Prefilled due date (YYYY-MM-DD); undefined = no due date. */
    dueDate?: string
    /** Prefilled status; defaults to TODO. */
    status?: TaskStatus
    /** Placeholder text for the input. */
    placeholder?: string
    /** Called with the new block id after a successful create. */
    onCreated?: (id: string) => void
    /** Called when the user cancels (Escape) or blurs with an empty title. */
    onCancel?: () => void
    /**
     * When true (default), the input stays open + focused after a create so a
     * user can capture several tasks in a row. Set false for one-shot surfaces
     * that close on submit.
     */
    keepOpenAfterCreate?: boolean
    /**
     * When true (default), the input grabs focus on mount so the user can type
     * immediately. Set false for persistent (always-mounted) instances like the
     * Tasks view's bottom bar, where stealing focus on every view-entry would
     * pull the cursor away from the list the user came to interact with.
     */
    autofocus?: boolean
    /**
     * When true, Escape clears the draft text instead of calling onCancel.
     * For persistent instances (Tasks bottom bar) that have no onCancel, this
     * makes Escape mean "discard this draft" rather than being a dead key.
     * Default false — toggle surfaces (Kanban) pass onCancel to collapse
     * instead, so Escape hides the input via the cancel path.
     */
    clearOnEscape?: boolean
  }

  let {
    ctx,
    createTask,
    dueDate,
    status = 'TODO',
    placeholder = 'Add a task — Enter to add',
    onCreated,
    onCancel,
    keepOpenAfterCreate = true,
    autofocus = true,
    clearOnEscape = false
  }: Props = $props()

  let title = $state('')
  let busy = $state(false)
  let errorMsg = $state('')
  let inputEl = $state<HTMLInputElement | null>(null)

  // Autofocus on mount so the user can type immediately from any entry point.
  // Gated on the autofocus prop so persistent instances don't steal focus.
  $effect(() => {
    if (autofocus) inputEl?.focus()
  })

  // Resolve the create fn at call time: the explicit app-level shim wins,
  // else the plugin ctx. Read inside submit (not a top-level const) so Svelte
  // doesn't flag a stale-capture of the reactive props.
  async function submit() {
    const t = title.trim()
    if (!t || busy) return
    busy = true
    errorMsg = ''
    const fn =
      createTask ??
      ((opts: { title: string; dueDate?: string; status?: TaskStatus }) =>
        ctx!.createTask(opts))
    try {
      const id = await fn({
        title: t,
        dueDate,
        status
      })
      onCreated?.(id)
      if (keepOpenAfterCreate) {
        title = ''
        // block:changed listener on each view repaints; keep focus for rapid entry.
        inputEl?.focus()
      }
    } catch (err) {
      // Surface the failure inline so the user knows the create did not land
      // (disk full / vault reloading / locked file / canceled session). The
      // text stays in place for an easy retry.
      errorMsg = err instanceof Error ? err.message : String(err)
    } finally {
      busy = false
    }
  }

  function onKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') {
      e.preventDefault()
      e.stopPropagation()
      void submit()
    } else if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      if (clearOnEscape) {
        title = ''
      } else {
        onCancel?.()
      }
    }
  }

  function onBlur() {
    // If the input is empty on blur, treat as cancel so a stray click-away
    // dismisses the affordance. A non-empty draft stays for the user to resume.
    if (!title.trim()) onCancel?.()
  }
</script>

<div class="relative">
  <input
    bind:this={inputEl}
    bind:value={title}
    type="text"
    {placeholder}
    maxlength={500}
    disabled={busy}
    onkeydown={onKeydown}
    onblur={onBlur}
    aria-label={placeholder}
    aria-invalid={!!errorMsg}
    aria-busy={busy}
    data-testid="quick-add-task-input"
    class="w-full px-2 py-1 rounded border border-accent-primary-start/40 bg-surface-panel text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 text-type-sm disabled:opacity-60 {busy
      ? 'pr-7'
      : ''}"
  />
  {#if busy}
    <!-- Busy spinner: progress_activity is Material's dedicated spin glyph;
         animate-spin matches the convention used across the app (#461). -->
    <span
      class="absolute right-2 top-1/2 -translate-y-1/2 pointer-events-none text-text-muted"
      aria-hidden="true"
    >
      <span class="material-symbols-outlined text-icon-sm animate-spin"
        >progress_activity</span
      >
    </span>
  {/if}
</div>
{#if errorMsg}
  <div class="mt-1">
    <ErrorBanner message={errorMsg} compact dataTestId="quick-add-error" />
  </div>
{/if}
