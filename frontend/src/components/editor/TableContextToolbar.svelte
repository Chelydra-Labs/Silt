<script lang="ts">
  import type { Editor } from '@tiptap/core'
  import { nearestEnabledIndex } from '../../lib/editor/rovingTabindex'
  import { flipOrClamp } from '../../lib/editor/popoverPositioning'
  import { settings } from '../../settings/store.svelte'
  import { resolveHotkeyDisplay } from '../../settings/hotkeys'

  // Floating contextual toolbar for GFM tables (#688 / #172). Anchors near the
  // active cell, groups row vs column ops with visible labels, and keeps
  // destructive deletes distinct. Merge cells is omitted — GFM can't represent
  // spans.
  let { editor }: { editor: Editor } = $props()

  type Op = {
    id: string
    icon: string
    /** Short visible label so insert-before/after is not arrow-only. */
    shortLabel: string
    label: string
    /** Config hotkey action name (e.g. 'table_insert_row_above'). */
    hotkey?: string
    danger?: boolean
    can: () => boolean
    run: () => void
  }

  type Group = {
    id: string
    label: string
    ops: Op[]
  }

  // Each op's disabled state depends on the live editor selection.
  // `editor.can()` is not reactive, so a tick on selection/transaction keeps
  // disabled attributes honest while the cursor stays in the table.
  let selTick = $state(0)
  $effect(() => {
    const handler = (): void => {
      selTick++
    }
    editor.on('selectionUpdate', handler)
    editor.on('transaction', handler)
    return () => {
      editor.off('selectionUpdate', handler)
      editor.off('transaction', handler)
    }
  })

  const groups = $derived.by<Group[]>(() => {
    void selTick
    return [
      {
        id: 'rows',
        label: 'Rows',
        ops: [
          {
            id: 'row-above',
            icon: 'keyboard_arrow_up',
            shortLabel: 'Row ↑',
            label: 'Insert row above',
            hotkey: 'table_insert_row_above',
            can: () => !!editor.can().addRowBefore?.(),
            run: () => editor.chain().focus().addRowBefore().run()
          },
          {
            id: 'row-below',
            icon: 'keyboard_arrow_down',
            shortLabel: 'Row ↓',
            label: 'Insert row below',
            hotkey: 'table_insert_row_below',
            can: () => !!editor.can().addRowAfter?.(),
            run: () => editor.chain().focus().addRowAfter().run()
          },
          {
            id: 'del-row',
            icon: 'delete',
            shortLabel: 'Del row',
            label: 'Delete row',
            danger: true,
            can: () => !!editor.can().deleteRow?.(),
            run: () => editor.chain().focus().deleteRow().run()
          }
        ]
      },
      {
        id: 'columns',
        label: 'Columns',
        ops: [
          {
            id: 'col-left',
            icon: 'keyboard_arrow_left',
            shortLabel: 'Col ←',
            label: 'Insert column left',
            hotkey: 'table_insert_col_left',
            can: () => !!editor.can().addColumnBefore?.(),
            run: () => editor.chain().focus().addColumnBefore().run()
          },
          {
            id: 'col-right',
            icon: 'keyboard_arrow_right',
            shortLabel: 'Col →',
            label: 'Insert column right',
            hotkey: 'table_insert_col_right',
            can: () => !!editor.can().addColumnAfter?.(),
            run: () => editor.chain().focus().addColumnAfter().run()
          },
          {
            id: 'del-col',
            icon: 'delete_outline',
            shortLabel: 'Del col',
            label: 'Delete column',
            danger: true,
            can: () => !!editor.can().deleteColumn?.(),
            run: () => editor.chain().focus().deleteColumn().run()
          }
        ]
      }
    ]
  })

  const flatOps = $derived(groups.flatMap((g) => g.ops))

  // Roving tabindex: one Tab stop for the toolbar; arrows move between
  // enabled buttons. Disabled controls are skipped.
  let rovingIdx = $state(0)
  let toolbarEl: HTMLElement | null = $state(null)
  let pos = $state({ left: 0, top: 0 })
  let anchorInvalid = $state(false)

  $effect(() => {
    const currentOps = flatOps
    if (rovingIdx >= currentOps.length || !currentOps[rovingIdx].can()) {
      const disabled = currentOps.map((op) => !op.can())
      const next = nearestEnabledIndex(disabled, rovingIdx, 1)
      if (next !== rovingIdx) rovingIdx = next
    }
  })

  let hotkeys = $derived(settings.config?.hotkeys ?? {})
  function hk(action: string | undefined): string {
    return action ? resolveHotkeyDisplay(action, hotkeys) : ''
  }

  /** Resolve the active cell (or table) DOM for placement. */
  function resolveAnchorRect(): DOMRect | null {
    if (!editor || editor.isDestroyed) return null
    try {
      const { view, state } = editor
      const from = state.selection.$from
      let cellPos: number | null = null
      let tablePos: number | null = null
      for (let d = from.depth; d > 0; d--) {
        const name = from.node(d).type.name
        if (
          (name === 'tableCell' || name === 'tableHeader') &&
          cellPos === null
        ) {
          cellPos = from.before(d)
        }
        if (name === 'table' && tablePos === null) {
          tablePos = from.before(d)
        }
      }
      const anchorPos = cellPos ?? tablePos
      if (anchorPos == null) return null
      const dom = view.nodeDOM(anchorPos)
      if (!(dom instanceof HTMLElement)) return null
      return dom.getBoundingClientRect()
    } catch {
      return null
    }
  }

  function updatePosition(): void {
    const rect = resolveAnchorRect()
    if (!rect || (rect.width === 0 && rect.height === 0)) {
      anchorInvalid = true
      return
    }
    anchorInvalid = false
    const el = toolbarEl
    const width = el?.offsetWidth || 320
    const height = el?.offsetHeight || 44
    // Prefer above the cell; flipOrClamp flips below when needed and clamps.
    pos = flipOrClamp(
      { top: rect.top, bottom: rect.bottom, left: rect.left },
      { width, height },
      { width: window.innerWidth, height: window.innerHeight }
    )
  }

  // Reposition on selection ticks, measured size, scroll, and resize.
  $effect(() => {
    void selTick
    // Read toolbarEl so a post-bind measure updates placement after first paint.
    void toolbarEl
    updatePosition()
  })

  $effect(() => {
    const onScrollOrResize = (): void => {
      updatePosition()
    }
    document.addEventListener('scroll', onScrollOrResize, {
      capture: true,
      passive: true
    })
    window.addEventListener('resize', onScrollOrResize, { passive: true })
    return () => {
      document.removeEventListener('scroll', onScrollOrResize, {
        capture: true
      })
      window.removeEventListener('resize', onScrollOrResize)
    }
  })

  function handleKeydown(e: KeyboardEvent): void {
    const btns = toolbarEl?.querySelectorAll<HTMLButtonElement>('[data-tb]')
    if (!btns || btns.length === 0) return
    const disabled = Array.from(btns, (b) => b.disabled)
    let next: number
    if (e.key === 'ArrowRight' || e.key === 'ArrowDown') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, rovingIdx, 1)
    } else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, rovingIdx, -1)
    } else if (e.key === 'Home') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, -1, 1)
    } else if (e.key === 'End') {
      e.preventDefault()
      next = nearestEnabledIndex(disabled, disabled.length, -1)
    } else if (e.key === 'Escape') {
      e.preventDefault()
      editor.chain().focus().run()
      return
    } else {
      return
    }
    rovingIdx = next
    btns[next]?.focus()
  }
</script>

{#if !anchorInvalid}
  <div
    class="table-context-toolbar"
    role="toolbar"
    aria-label="Table actions"
    tabindex="-1"
    bind:this={toolbarEl}
    style="left: {pos.left}px; top: {pos.top}px"
    onkeydown={handleKeydown}
  >
    {#each groups as group, gi (group.id)}
      {@const baseIdx = groups
        .slice(0, gi)
        .reduce((n, g) => n + g.ops.length, 0)}
      <div
        class="tct-group"
        role="group"
        aria-labelledby="tct-group-{group.id}"
      >
        <span class="tct-group-label" id="tct-group-{group.id}"
          >{group.label}</span
        >
        {#each group.ops as op, i (op.id)}
          {@const idx = baseIdx + i}
          {@const hotkey = hk(op.hotkey)}
          <button
            type="button"
            class="tct-btn"
            class:tct-btn-danger={op.danger}
            data-tb
            data-op={op.id}
            disabled={!op.can()}
            aria-label={op.label}
            aria-keyshortcuts={hotkey || undefined}
            tabindex={rovingIdx === idx ? 0 : -1}
            title={hotkey ? `${op.label} (${hotkey})` : op.label}
            onclick={op.run}
            onfocus={() => (rovingIdx = idx)}
          >
            <span class="material-symbols-outlined" aria-hidden="true">
              {op.icon}
            </span>
            <span class="tct-btn-text" aria-hidden="true">{op.shortLabel}</span>
          </button>
        {/each}
      </div>
      {#if gi < groups.length - 1}
        <span class="tct-divider" aria-hidden="true"></span>
      {/if}
    {/each}
  </div>
{/if}
