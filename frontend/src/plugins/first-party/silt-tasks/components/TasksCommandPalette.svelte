<script lang="ts">
  // Hub-scoped Tasks command palette (#436). Ctrl+K (when focus is not an
  // editor/input) opens this overlay over the Tasks hub: switch display mode,
  // group-by, sort, activate a saved view, find task, or add task. Fuzzy
  // filter + arrow/enter keyboard nav; ARIA combobox pattern.
  import { tick } from 'svelte'
  import {
    getTaskHubState,
    type DisplayMode,
    type GroupBy,
    type SortMode,
    type SavedView
  } from '../state.svelte'
  import { settings } from '../../../../settings/store.svelte'
  import { resolveHotkeyDisplay } from '../../../../settings/hotkeys'

  interface Props {
    open: boolean
    onClose: () => void
    onDisplayMode: (mode: DisplayMode) => void
    onGroupBy: (g: GroupBy) => void
    onSort: (s: SortMode) => void
    onApplyView: (view: SavedView) => void
    onFindTask: () => void
    onAddTask: () => void
  }

  let {
    open,
    onClose,
    onDisplayMode,
    onGroupBy,
    onSort,
    onApplyView,
    onFindTask,
    onAddTask
  }: Props = $props()

  interface PaletteCommand {
    id: string
    label: string
    subtitle?: string
    icon: string
    run: () => void
  }

  const MODES: { value: DisplayMode; label: string; icon: string }[] = [
    { value: 'list', label: 'List', icon: 'checklist' },
    { value: 'board', label: 'Board', icon: 'view_kanban' },
    { value: 'calendar', label: 'Calendar', icon: 'calendar_month' }
  ]

  const GROUP_OPTIONS: { value: GroupBy; label: string; icon: string }[] = [
    { value: 'none', label: 'None', icon: 'grid_off' },
    { value: 'status', label: 'Status', icon: 'check_circle' },
    { value: 'owner', label: 'Owner', icon: 'person' },
    { value: 'priority', label: 'Priority', icon: 'flag' },
    { value: 'dueDate', label: 'Due date', icon: 'schedule' },
    { value: 'tag', label: 'Tag', icon: 'label' },
    { value: 'notebook', label: 'Notebook', icon: 'book' },
    { value: 'section', label: 'Section', icon: 'tag' },
    { value: 'page', label: 'Page', icon: 'description' }
  ]

  const SORT_OPTIONS: { value: SortMode; label: string; icon: string }[] = [
    { value: 'manual', label: 'Manual', icon: 'drag_indicator' },
    { value: 'dueDate', label: 'Due date', icon: 'schedule' },
    { value: 'priority', label: 'Priority', icon: 'flag' },
    { value: 'title', label: 'Title', icon: 'title' },
    { value: 'created', label: 'Created', icon: 'calendar_today' },
    { value: 'owner', label: 'Owner', icon: 'person' },
    { value: 'modified', label: 'Recently Modified', icon: 'update' },
    { value: 'estimate', label: 'Estimate', icon: 'timer' }
  ]

  let query = $state('')
  let selectedIdx = $state(0)
  let inputEl = $state<HTMLInputElement | null>(null)
  let panelEl = $state<HTMLDivElement | null>(null)
  let listEl = $state<HTMLUListElement | null>(null)
  let previouslyFocused: HTMLElement | null = null

  const LISTBOX_ID = 'tasks-command-palette-listbox'
  const INPUT_ID = 'tasks-command-palette-input'

  let hotkeys = $derived(settings.config?.hotkeys ?? {})
  let openSearchHint = $derived(
    resolveHotkeyDisplay('open_search', hotkeys) || ''
  )
  let newTaskHint = $derived(resolveHotkeyDisplay('new_task', hotkeys) || '')

  // Case-insensitive includes OR subsequence match on the label.
  function fuzzyMatch(label: string, q: string): boolean {
    const l = label.toLowerCase()
    const needle = q.toLowerCase().trim()
    if (!needle) return true
    if (l.includes(needle)) return true
    let i = 0
    for (const ch of l) {
      if (ch === needle[i]) i++
      if (i === needle.length) return true
    }
    return false
  }

  let allCommands = $derived.by((): PaletteCommand[] => {
    // Track saved views so the list rebuilds when views change while open.
    const views = getTaskHubState().savedViews
    const cmds: PaletteCommand[] = []

    for (const m of MODES) {
      cmds.push({
        id: `mode-${m.value}`,
        label: `Switch to ${m.label}`,
        icon: m.icon,
        run: () => onDisplayMode(m.value)
      })
    }
    for (const g of GROUP_OPTIONS) {
      cmds.push({
        id: `group-${g.value}`,
        label: `Group by ${g.label}`,
        icon: g.icon,
        run: () => onGroupBy(g.value)
      })
    }
    for (const s of SORT_OPTIONS) {
      cmds.push({
        id: `sort-${s.value}`,
        label: `Sort by ${s.label}`,
        icon: s.icon,
        run: () => onSort(s.value)
      })
    }
    for (const v of views) {
      cmds.push({
        id: `view-${v.id}`,
        label: `Activate saved view: ${v.name}`,
        subtitle: v.system ? 'System' : undefined,
        icon: 'bookmark',
        run: () => onApplyView(v)
      })
    }
    cmds.push({
      id: 'find-task',
      label: 'Find task…',
      subtitle: openSearchHint || undefined,
      icon: 'search',
      run: () => onFindTask()
    })
    cmds.push({
      id: 'add-task',
      label: 'Add task…',
      subtitle: newTaskHint || undefined,
      icon: 'add_task',
      run: () => onAddTask()
    })
    return cmds
  })

  let filtered = $derived.by(() => {
    const q = query
    return allCommands.filter((c) => fuzzyMatch(c.label, q))
  })

  let activeOptionId = $derived(
    filtered.length > 0 && selectedIdx >= 0 && selectedIdx < filtered.length
      ? `tasks-cmd-${filtered[selectedIdx].id}`
      : undefined
  )

  let liveMsg = $derived.by(() => {
    if (!open) return ''
    if (filtered.length === 0) return 'No matching commands'
    const active = filtered[selectedIdx]
    if (!active) return `${filtered.length} commands`
    return `${active.label}${active.subtitle ? `, ${active.subtitle}` : ''}. ${selectedIdx + 1} of ${filtered.length}`
  })

  // Reset query + selection when the palette opens; focus the input.
  $effect(() => {
    if (!open) return
    query = ''
    selectedIdx = 0
    previouslyFocused = document.activeElement as HTMLElement | null
    void tick().then(() => inputEl?.focus())
  })

  // Clamp selection when the filter shrinks.
  $effect(() => {
    const n = filtered.length
    if (n === 0) {
      selectedIdx = 0
      return
    }
    if (selectedIdx >= n) selectedIdx = n - 1
  })

  // Scroll the active option into view.
  $effect(() => {
    if (!open || !listEl) return
    void selectedIdx
    const el = listEl.querySelector<HTMLElement>('[data-active="true"]')
    el?.scrollIntoView?.({ block: 'nearest' })
  })

  function runSelected() {
    const cmd = filtered[selectedIdx]
    if (!cmd) return
    cmd.run()
    onClose()
  }

  function runAt(idx: number) {
    const cmd = filtered[idx]
    if (!cmd) return
    selectedIdx = idx
    cmd.run()
    onClose()
  }

  const FOCUSABLE =
    'button:not([disabled]), input:not([disabled]), [tabindex]:not([tabindex="-1"])'

  function focusableEls(): HTMLElement[] {
    if (!panelEl) return []
    return Array.from(panelEl.querySelectorAll<HTMLElement>(FOCUSABLE))
  }

  function onPanelKeydown(e: KeyboardEvent) {
    if (e.key === 'Escape') {
      e.preventDefault()
      e.stopPropagation()
      onClose()
      return
    }
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      e.stopPropagation()
      if (filtered.length === 0) return
      selectedIdx = (selectedIdx + 1) % filtered.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      e.stopPropagation()
      if (filtered.length === 0) return
      selectedIdx = (selectedIdx - 1 + filtered.length) % filtered.length
      return
    }
    if (e.key === 'Enter') {
      e.preventDefault()
      e.stopPropagation()
      runSelected()
      return
    }
    // Focus trap: Tab cycles within the panel.
    if (e.key === 'Tab' && panelEl) {
      const els = focusableEls()
      if (els.length === 0) return
      const first = els[0]
      const last = els[els.length - 1]
      const active = document.activeElement as HTMLElement | null
      if (e.shiftKey) {
        if (active === first || !panelEl.contains(active)) {
          e.preventDefault()
          last.focus()
        }
      } else if (active === last || !panelEl.contains(active)) {
        e.preventDefault()
        first.focus()
      }
    }
  }

  // Capture-phase Escape so a nested input doesn't swallow close; also
  // restore focus when the overlay unmounts.
  $effect(() => {
    if (!open) return
    function onWinKey(e: KeyboardEvent) {
      if (e.key === 'Escape') {
        e.preventDefault()
        e.stopPropagation()
        onClose()
      }
    }
    window.addEventListener('keydown', onWinKey, true)
    return () => {
      window.removeEventListener('keydown', onWinKey, true)
      previouslyFocused?.focus?.()
    }
  })
</script>

{#if open}
  <div
    class="fixed inset-0 z-[200] flex items-start justify-center pt-[15vh] bg-black/40 backdrop-blur-[2px]"
    role="presentation"
    data-testid="tasks-command-palette-scrim"
    onclick={(e) => {
      if (e.target === e.currentTarget) onClose()
    }}
  >
    <div
      bind:this={panelEl}
      class="w-full max-w-lg glass-palette glass-palette-strong border border-surface-modal-border rounded-xl shadow-2xl overflow-hidden"
      role="dialog"
      aria-modal="true"
      aria-label="Tasks command palette"
      tabindex="-1"
      data-testid="tasks-command-palette"
      onkeydown={onPanelKeydown}
    >
      <div class="px-3 pt-3 pb-2 border-b border-surface-modal-border">
        <label class="sr-only" for={INPUT_ID}>Filter commands</label>
        <input
          bind:this={inputEl}
          bind:value={query}
          id={INPUT_ID}
          type="text"
          role="combobox"
          aria-expanded="true"
          aria-controls={LISTBOX_ID}
          aria-autocomplete="list"
          aria-activedescendant={activeOptionId}
          aria-haspopup="listbox"
          placeholder="Type a command…"
          data-testid="tasks-command-palette-input"
          class="w-full px-3 py-2 rounded-lg bg-surface-panel border border-surface-panel-border text-text-primary text-type-sm font-body-md outline-none focus:border-accent-primary-start"
          oninput={() => {
            selectedIdx = 0
          }}
        />
      </div>

      <ul
        bind:this={listEl}
        id={LISTBOX_ID}
        role="listbox"
        aria-label="Commands"
        data-testid="tasks-command-palette-list"
        class="max-h-80 overflow-y-auto py-1 custom-scrollbar"
      >
        {#if filtered.length === 0}
          <li
            class="px-4 py-3 text-type-sm text-text-muted text-center select-none"
            role="presentation"
          >
            No matching commands
          </li>
        {:else}
          {#each filtered as cmd, idx (cmd.id)}
            <!-- Keyboard activation is handled on the dialog (Arrow/Enter);
                 pointer users click the option. role=option + listbox is the
                 WAI-ARIA combobox pattern — not a button. -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <li
              id={`tasks-cmd-${cmd.id}`}
              role="option"
              aria-selected={idx === selectedIdx}
              data-active={idx === selectedIdx ? 'true' : undefined}
              data-testid={`tasks-command-palette-item-${cmd.id}`}
              class="flex items-center gap-3 px-4 py-2 cursor-pointer transition-colors {idx ===
              selectedIdx
                ? 'bg-accent-primary-glow text-accent-primary-start'
                : 'text-text-primary hover:bg-hover'}"
              onmouseenter={() => (selectedIdx = idx)}
              onclick={() => runAt(idx)}
            >
              <span
                class="material-symbols-outlined text-icon-md select-none"
                aria-hidden="true">{cmd.icon}</span
              >
              <div class="flex-1 flex flex-col min-w-0">
                <span class="font-label-sm text-type-sm truncate"
                  >{cmd.label}</span
                >
                {#if cmd.subtitle}
                  <span class="text-type-2xs text-text-muted truncate"
                    >{cmd.subtitle}</span
                  >
                {/if}
              </div>
            </li>
          {/each}
        {/if}
      </ul>

      <div
        class="sr-only"
        aria-live="polite"
        data-testid="tasks-command-palette-live"
      >
        {liveMsg}
      </div>
    </div>
  </div>
{/if}
