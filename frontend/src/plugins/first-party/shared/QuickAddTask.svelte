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
  import type { PluginContext, TaskStatus } from '../../sdk'

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
  }

  let {
    ctx,
    createTask,
    dueDate,
    status = 'TODO',
    placeholder = 'Add a task…',
    onCreated,
    onCancel,
    keepOpenAfterCreate = true
  }: Props = $props()

  let title = $state('')
  let busy = $state(false)
  let errorMsg = $state('')
  let inputEl = $state<HTMLInputElement | null>(null)

  // Autofocus on mount so the user can type immediately from any entry point.
  $effect(() => {
    inputEl?.focus()
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
      onCancel?.()
    }
  }

  function onBlur() {
    // If the input is empty on blur, treat as cancel so a stray click-away
    // dismisses the affordance. A non-empty draft stays for the user to resume.
    if (!title.trim()) onCancel?.()
  }
</script>

<input
  bind:this={inputEl}
  bind:value={title}
  type="text"
  {placeholder}
  disabled={busy}
  onkeydown={onKeydown}
  onblur={onBlur}
  aria-label={placeholder}
  aria-invalid={!!errorMsg}
  data-testid="quick-add-task-input"
  class="w-full px-2 py-1 rounded border border-accent-primary-start/40 bg-surface-panel text-text-primary placeholder:text-text-muted focus:outline-none focus:ring-1 focus:ring-accent-primary-start/40 text-[12px] disabled:opacity-60"
/>
{#if errorMsg}
  <div class="text-error text-[11px] mt-1" role="alert">
    {errorMsg}
  </div>
{/if}
