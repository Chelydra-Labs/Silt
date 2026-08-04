// Slash menu — `/` command palette detection, positioning, and the built-in +
// plugin command dispatch. Extracted from TipTapEditor.svelte unchanged in
// behaviour. The composable owns the palette's open/query/dismissed state and
// the measured palette size; the host still renders <CommandPalette> (it needs
// the editor DOM + formatting toggles) and feeds the result back through
// onSelect. Branches that open a host-owned popover/picker call back into the
// host so the slash logic stays decoupled from the editor's many UI surfaces.

import { SvelteDate } from 'svelte/reactivity'
import type { Editor } from 'svelte-tiptap'
import {
  convertToBlock,
  setBlockAlign,
  toggleBlockQuote,
  insertCallout,
  insertCodeBlock,
  insertDetails,
  insertTable,
  insertBlockMath
} from './keymaps'
import { getSlashCommands } from './slash-registry'
import { classifySlashCommand } from './builtinSlashCommands'
import { runPluginCommand } from './runPluginCommand'
import { flipOrClamp } from './popoverPositioning'
import { settings } from '../../settings/store.svelte'
import { pushNotification } from '../../notifications/store.svelte'
import { formatDate, resolveDateFormat } from '../dateFormat'
import { openDateGlanceNearEditor } from '../dateGlanceState.svelte'
import { openShortcutHelp } from '../shortcutHelpState.svelte'
import { ASSIGN_PAGE_TYPE_EVENT } from '../../shell/pageTypeEvents'

/** Shape of the LaTeX popover the `/math` branch opens (block mode). */
export interface SlashMathPopover {
  latex: string
  displayMode: boolean
  coords: { left: number; top: number }
  onCommit: (latex: string) => void
}

/** Cursor anchor rect the `/tableCustom` size picker flips/clamps against. */
export interface SlashTableSizeAnchor {
  top: number
  bottom: number
  left: number
}

export interface SlashMenuOptions {
  /** Live editor accessor — re-read so deferred callbacks (math onCommit) see
   *  the current editor after an edit↔source switch, not a stale capture. */
  getEditor: () => Editor | null | undefined
  /** `/math` — open the block-equation LaTeX popover. */
  onOpenMathPopover: (popover: SlashMathPopover) => void
  /** `/tableCustom` — open the custom table-size picker at the anchor. */
  onOpenTableSizePicker: (anchor: SlashTableSizeAnchor) => void
  /** `/color` — open the text/background color picker. */
  onOpenColorPicker: (markType: 'textColor' | 'highlight') => void
  /** `/embed` — open the block picker to insert an embed portal. */
  onShowEmbedPicker: () => void
  /** `/template` — open the template picker. */
  onShowTemplatePicker: () => void
}

export function createSlashMenu(opts: SlashMenuOptions) {
  let showSlashMenu = $state(false)
  let slashQuery = $state('')
  let slashMenuDismissed = $state(false)
  // Measured size of the rendered slash palette, used for the flip/clamp
  // decision so positioning reflects the real element rather than a fixed
  // 256×300 estimate (#590). Falls back to the estimate until measured.
  let paletteSize = $state({ width: 256, height: 300 })

  $effect(() => {
    if (!showSlashMenu) return
    const el = document.getElementById('silt-slash-palette')
    if (el && el.offsetWidth && el.offsetHeight) {
      paletteSize = { width: el.offsetWidth, height: el.offsetHeight }
    }
  })

  function detectSlashCommand(): void {
    const editor = opts.getEditor()
    if (!editor || editor.isDestroyed) return
    const sel = editor.state.selection
    const textBefore = sel.$from.parent.textContent.slice(
      0,
      sel.$from.parentOffset
    )
    if (textBefore.startsWith('/')) {
      if (!slashMenuDismissed) {
        showSlashMenu = true
        slashQuery = textBefore.slice(1)
      }
    } else {
      showSlashMenu = false
      slashQuery = ''
      slashMenuDismissed = false
    }
  }

  function slashCoords(): { left: number; top: number } | null {
    if (!showSlashMenu) return null
    const editor = opts.getEditor()
    if (!editor || editor.isDestroyed) return null
    const { selection } = editor.state
    const pos = selection.$from.start()
    try {
      const c = editor.view.coordsAtPos(pos)
      // Flip above the cursor when there is no room below, using the palette's
      // measured size rather than a fixed estimate (#590).
      return flipOrClamp(
        { top: c.top, bottom: c.bottom, left: c.left },
        { width: paletteSize.width, height: paletteSize.height },
        { width: window.innerWidth, height: window.innerHeight }
      )
    } catch {
      return null
    }
  }

  /** Dismiss without selecting (scroll/resize/outside-click/CommandPalette onClose). */
  function dismiss(): void {
    showSlashMenu = false
    slashMenuDismissed = true
  }

  function handleSlashSelect(commandId: string): void {
    showSlashMenu = false
    slashQuery = ''
    slashMenuDismissed = false
    const editor = opts.getEditor()
    if (!editor || editor.isDestroyed) return

    const sel = editor.state.selection
    const from = sel.$from.start()
    const to = from + sel.$from.parentOffset
    editor.commands.deleteRange({ from, to })

    const intent = classifySlashCommand(commandId)
    if (!intent) {
      // v2 SDK plugin-registered slash command (#110): look up the command in
      // the registry and invoke its onSelect handler with the live editor +
      // cursor position. Built-ins are handled by classifySlashCommand; any
      // other id must be a plugin command with a handler.
      const cmd = getSlashCommands().find((c) => c.id === commandId)
      if (cmd?.onSelect) {
        // Isolate plugin-handler failures (#581): a buggy plugin's throw or
        // rejected Promise must not escape into the editor's dispatch path or
        // go unhandled. The slash trigger text is already deleted above, so
        // the editor stays clean either way; surface a non-blocking toast +
        // a console error carrying the plugin + command id.
        const pluginID = cmd.pluginID ?? 'unknown'
        const report = (err: unknown): void => {
          console.error(
            `[silt] plugin ${pluginID} command ${commandId} failed:`,
            err
          )
          pushNotification({
            kind: 'error',
            message: 'Plugin command failed — see console.',
            autoDismissMs: 7000
          })
        }
        runPluginCommand(cmd, editor, editor.state.selection.to, report)
      }
      return
    }
    switch (intent.kind) {
      case 'convert':
        if (intent.depth !== undefined)
          convertToBlock(editor, intent.blockType, intent.depth)
        else convertToBlock(editor, intent.blockType)
        break
      case 'align':
        setBlockAlign(editor, intent.align)
        break
      case 'quote':
        toggleBlockQuote(editor)
        break
      case 'callout':
        insertCallout(editor, intent.variant)
        break
      case 'codeBlock':
        insertCodeBlock(editor, intent.language ?? '')
        break
      case 'math':
        // Open the LaTeX popover (block mode); on commit, insert a block
        // equation at the selection via the same insertBlockMath path the old
        // prompt used. The popover (with live preview) replaces window.prompt.
        if (!editor || editor.isDestroyed) return
        try {
          const { selection } = editor.state
          const c = editor.view.coordsAtPos(selection.from)
          opts.onOpenMathPopover({
            latex: '',
            displayMode: true,
            coords: { left: c.left, top: c.bottom },
            onCommit: (l: string) => {
              const e = opts.getEditor()
              if (e) insertBlockMath(e, l)
            }
          })
        } catch {
          /* no selection coords → don't open the popover */
        }
        break
      case 'details':
        insertDetails(editor)
        break
      case 'table':
        insertTable(editor, intent.rows, intent.cols)
        break
      case 'tableCustom':
        // Open an in-app size popover instead of the native window.prompt.
        // The picker receives the cursor anchor rect and flips/clamps itself.
        if (!editor || editor.isDestroyed) return
        try {
          const { selection } = editor.state
          const coords = editor.view.coordsAtPos(selection.from)
          opts.onOpenTableSizePicker({
            top: coords.top,
            bottom: coords.bottom,
            left: coords.left
          })
        } catch {
          opts.onOpenTableSizePicker({ top: 100, bottom: 120, left: 100 })
        }
        break
      case 'color':
        opts.onOpenColorPicker(intent.markType)
        break
      case 'today': {
        const fmt = resolveDateFormat(settings.config?.editor?.date_format)
        editor.commands.insertContent(formatDate(new SvelteDate(), fmt))
        break
      }
      case 'calendar':
        // Open Date Glance beside the caret (coordsAtPos). Slash text is
        // already deleted above; the editor stays the insert target.
        openDateGlanceNearEditor(editor)
        break
      case 'shortcuts':
        // Open the keyboard-shortcut reference overlay (#731). Same surface
        // as the Shift+? hotkey; the slash trigger is already deleted above.
        openShortcutHelp()
        break
      case 'embed':
        // Open the block picker; the selected block is inserted as a complete
        // {{embed:UUID}} token (#593). The bare '{{embed:' fragment the old
        // path emitted never resolved into a live embed portal.
        opts.onShowEmbedPicker()
        break
      case 'template':
        // The `/` text is already deleted above; open the picker. The editor
        // preserves its selection state, so when the user confirms the rendered
        // blocks are inserted at the cursor position (ARCHITECTURE §5.1 — the
        // UniqueBlockIds extension mints fresh UUIDs for the inserted nodes).
        opts.onShowTemplatePicker()
        break
      case 'type':
        // Page-level type assignment is owned by the properties panel, not the
        // editor. Signal the host shell via a window event; the slash trigger
        // text is already deleted above so the editor stays clean either way.
        window.dispatchEvent(new CustomEvent(ASSIGN_PAGE_TYPE_EVENT))
        break
      case 'format':
        // Inline formatting slash commands (#168). Each toggles its mark;
        // the value is also a valid stored mark at a collapsed cursor, so the
        // command does meaningful work without a selection.
        editor.chain().focus().toggleMark(intent.mark).run()
        break
    }
  }

  // State is exposed via getters so reads in the host's template stay reactive
  // (returning $state in a plain object would freeze the initial value).
  return {
    get showSlashMenu() {
      return showSlashMenu
    },
    get slashQuery() {
      return slashQuery
    },
    slashCoords,
    detectSlashCommand,
    handleSlashSelect,
    dismiss
  }
}
