// Reactive controller for the Tasks Board status-column config (#421/#437).
// Owns the user-managed status columns (rename/add/remove/WIP-limit editor),
// column drag-reorder, persistence + hub-state mirroring, and the rendered-
// column snapshot/revert helpers the DnD engine uses for optimistic moves.
//
// Lifted from BoardView.svelte following the repo's `.svelte.ts` rune-class
// pattern (aiProviderController / localMcpController). The rendered `columns`
// model + the `rebin` derivation stay in BoardView (the render surface owns
// them) and are injected as accessors; `statusColumns` (the persisted config)
// moves here because this controller is the sole owner/mutator of it.
import {
  cloneColumns,
  columnNames,
  type BoardColumn as StatusColumn
} from '../../columns'
import { laneLabel } from '../../types'
import { loadColumns, persistColumns } from '../../settings'
// Alias to avoid colliding with the deps `setColumns` accessor below: this
// one mirrors the status-column config into hub state (saved-view dirty
// flag), the deps one writes the rendered Lane[] model.
import { setColumns as setHubColumns } from '../../state.svelte'
import type { Lane } from './useBoardDnd.svelte'

export interface StatusColumnsDeps {
  // Shared render model (BoardView's $state `columns`).
  getColumns: () => Lane[]
  setColumns: (next: Lane[]) => void
  // Re-derive the rendered columns after a status-column config change
  // (BoardView owns the derivation since it owns rows/groupBy/today).
  rebin: () => void
}

export function createStatusColumnsController({
  getColumns,
  setColumns,
  rebin
}: StatusColumnsDeps) {
  // Status columns are the ONLY user-managed columns (configured + persisted,
  // including soft WIP limits #437). Every other dimension derives its
  // columns from the loaded data.
  let statusColumns = $state<StatusColumn[]>(loadColumns())
  let configError = $state('')

  // --- Column-management UI state (status dimension only) -----------------
  let menuCol = $state<string | null>(null)
  let renamingColKey = $state<string | null>(null)
  let renameValue = $state('')
  let colDragIndex = $state<number | null>(null)
  // Inline WIP editor in the column menu (replaces window.prompt for a11y).
  let wipEditCol = $state<string | null>(null)
  let wipDraft = $state('')
  let wipEditError = $state('')

  function snapshotColumns(): Lane[] {
    return getColumns().map((c) => ({ ...c, items: [...c.items] }))
  }

  function revertTo(prev: Lane[]) {
    setColumns(prev)
  }

  /** Persist status columns + mirror into hub state (saved-view dirty flag). */
  function saveStatusColumns(next: StatusColumn[], prev: StatusColumn[]) {
    statusColumns = next
    setHubColumns(next)
    void persistColumns(next).then((ok) => {
      if (!ok) {
        configError = 'Failed to save columns'
        statusColumns = prev
        setHubColumns(prev)
        rebin()
      }
    })
    rebin()
  }

  // --- Column management (status dimension only) --------------------------
  function toggleColMenu(colKey: string) {
    const next = menuCol === colKey ? null : colKey
    menuCol = next
    if (!next || next !== colKey) {
      wipEditCol = null
      wipEditError = ''
    }
  }
  function startRename(statusName: string, colKey: string) {
    renamingColKey = colKey
    renameValue = statusName
    menuCol = null
  }
  function commitRename(oldStatus: string) {
    const v = renameValue.trim()
    renamingColKey = null
    const names = columnNames(statusColumns)
    if (!v || v === oldStatus || names.includes(v)) return
    const prev = cloneColumns(statusColumns)
    const next = statusColumns.map((c) =>
      c.name === oldStatus ? { ...c, name: v } : c
    )
    configError = ''
    saveStatusColumns(next, prev)
  }
  function cancelRename() {
    renamingColKey = null
  }
  function addColumn() {
    const name = window.prompt('New column name')?.trim()
    if (!name || columnNames(statusColumns).includes(name)) return
    const prev = cloneColumns(statusColumns)
    configError = ''
    saveStatusColumns([...statusColumns, { name }], prev)
  }
  function removeColumn(statusName: string) {
    menuCol = null
    if (
      !window.confirm(
        `Remove column "${laneLabel(statusName)}"? Cards keep their status.`
      )
    )
      return
    const prev = cloneColumns(statusColumns)
    configError = ''
    saveStatusColumns(
      statusColumns.filter((c) => c.name !== statusName),
      prev
    )
  }

  function startWipEdit(statusName: string) {
    const current = statusColumns.find((c) => c.name === statusName)
    wipEditCol = statusName
    wipEditError = ''
    wipDraft =
      current?.wipLimit != null && current.wipLimit >= 1
        ? String(current.wipLimit)
        : ''
  }

  function applyWipLimit(statusName: string) {
    const trimmed = wipDraft.trim()
    let wipLimit: number | null
    if (trimmed === '') {
      wipLimit = null
    } else {
      const n = Number(trimmed)
      if (!Number.isFinite(n) || n < 1) {
        wipEditError = 'Enter a whole number ≥ 1, or leave empty for unlimited'
        return
      }
      wipLimit = Math.floor(n)
    }
    const prev = cloneColumns(statusColumns)
    const next = statusColumns.map((c) => {
      if (c.name !== statusName) return c
      if (wipLimit == null) {
        const { wipLimit: _drop, ...rest } = c
        void _drop
        return rest
      }
      return { ...c, wipLimit }
    })
    configError = ''
    wipEditCol = null
    wipEditError = ''
    menuCol = null
    saveStatusColumns(next, prev)
  }

  function clearWipLimit(statusName: string) {
    const prev = cloneColumns(statusColumns)
    const next = statusColumns.map((c) => {
      if (c.name !== statusName) return c
      const { wipLimit: _drop, ...rest } = c
      void _drop
      return rest
    })
    configError = ''
    wipEditCol = null
    wipEditError = ''
    menuCol = null
    saveStatusColumns(next, prev)
  }

  function onColDragStart(e: DragEvent, i: number) {
    colDragIndex = i
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move'
      e.dataTransfer.setData('text/plain', `col:${i}`)
    }
  }
  function onColDragOver(e: DragEvent, _i: number) {
    if (colDragIndex === null) return
    e.preventDefault()
    if (e.dataTransfer) e.dataTransfer.dropEffect = 'move'
  }
  function onColDrop(e: DragEvent, i: number) {
    if (colDragIndex === null) return
    e.preventDefault()
    const from = colDragIndex
    colDragIndex = null
    if (from === i) return
    const prev = cloneColumns(statusColumns)
    const next = [...statusColumns]
    const [moved] = next.splice(from, 1)
    next.splice(i, 0, moved)
    configError = ''
    saveStatusColumns(next, prev)
  }

  // Clear the column-drag index when a card drag starts (the DnD controller
  // calls this so a card drag doesn't show the column-drag ghost).
  function clearColDragIndex() {
    colDragIndex = null
  }

  // Escape the inline WIP editor (the template's onWipEscape callback).
  function escapeWipEdit() {
    wipEditCol = null
    wipEditError = ''
  }

  return {
    // reactive state
    get statusColumns() {
      return statusColumns
    },
    get configError() {
      return configError
    },
    get menuCol() {
      return menuCol
    },
    set menuCol(v: string | null) {
      menuCol = v
    },
    get renamingColKey() {
      return renamingColKey
    },
    get renameValue() {
      return renameValue
    },
    set renameValue(v: string) {
      renameValue = v
    },
    get colDragIndex() {
      return colDragIndex
    },
    get wipEditCol() {
      return wipEditCol
    },
    get wipDraft() {
      return wipDraft
    },
    set wipDraft(v: string) {
      wipDraft = v
    },
    get wipEditError() {
      return wipEditError
    },
    // optimistic-column helpers shared with the DnD controller
    snapshotColumns,
    revertTo,
    // column config actions
    saveStatusColumns,
    toggleColMenu,
    startRename,
    commitRename,
    cancelRename,
    addColumn,
    removeColumn,
    startWipEdit,
    applyWipLimit,
    clearWipLimit,
    onColDragStart,
    onColDragOver,
    onColDrop,
    clearColDragIndex,
    escapeWipEdit
  }
}

export type StatusColumnsController = ReturnType<
  typeof createStatusColumnsController
>
