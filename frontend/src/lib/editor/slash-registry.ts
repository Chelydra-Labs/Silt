// Slash-command registry (#110). Refactors the previously hardcoded
// CommandPalette command list into an extensible registry so plugins can
// contribute `/`-menu entries alongside the built-ins.
//
// The registry is module-scoped (a Map<id, SlashCommand>). Built-ins register
// at boot; plugins register via ctx.registerSlashCommand. CommandPalette reads
// the union (grouped: Built-ins / Plugins). When a command is selected, the
// editor calls its handler with the live editor + cursor position.
//
// Capability gate (#158): plugin commands (with a pluginID) are checked
// against the trusted Go-provided grant cache before registration. Built-in
// commands (no pluginID) bypass the gate. This closes the advisory-gap: a
// plugin importing registerSlashCommand directly still hits the gate.

import type { Editor } from '@tiptap/core'
import { isGranted } from '../../plugins/grants.svelte'

export interface SlashCommand {
  /** Unique id. Plugin commands are namespaced as `<pluginID>:<id>`. */
  id: string
  /** Display label in the menu. */
  label: string
  description?: string
  icon?: string
  /**
   * Hotkey ACTION name (e.g. `'format_bold'`, `'set_h2'`) keyed into
   * `config.hotkeys`. Resolved to the live display binding at render time via
   * `resolveHotkeyDisplay`, so the hint always matches the user's actual
   * (possibly remapped or disabled) binding. NOT a keystring literal — the
   * binding is read from the config source of truth.
   */
  hotkey?: string
  /** The plugin id that registered this command, or undefined for built-ins. */
  pluginID?: string
  /**
   * Invoked when the user selects the command from the slash menu. Receives
   * the live TipTap editor instance and the cursor position. For built-ins
   * this may be undefined (the editor dispatches by id instead); for plugin
   * commands it is required.
   */
  onSelect?: (editor: Editor, pos: number) => void
}

const registry = new Map<string, SlashCommand>()

/**
 * Register a slash command. A plugin command's id is namespaced as
 * `<pluginID>:<id>` to avoid collisions with built-ins. Re-registering the
 * same namespaced id replaces the prior entry (idempotent on reload).
 *
 * Capability gate (#158): if the command has a pluginID, the registry checks
 * isGranted(pluginID, 'editor-schema') from the trusted Go grant cache. An
 * ungranted plugin's command is silently dropped (warn). Built-in commands
 * (no pluginID) bypass the gate.
 */
export function registerSlashCommand(cmd: SlashCommand): void {
  if (!cmd.id || !cmd.label) {
    throw new Error('SlashCommand requires id + label')
  }
  if (cmd.pluginID && !isGranted(cmd.pluginID, 'editor-schema')) {
    console.warn(
      `[silt] plugin ${cmd.pluginID} cannot register slash commands without the editor-schema capability`
    )
    return
  }
  registry.set(cmd.id, cmd)
}

/** Unregister a command by id (used on plugin disable/uninstall). */
export function unregisterSlashCommand(id: string): void {
  registry.delete(id)
}

/** Unregister every command for a plugin (by pluginID prefix). */
export function unregisterPluginSlashCommands(pluginID: string): void {
  for (const id of [...registry.keys()]) {
    if (registry.get(id)?.pluginID === pluginID) {
      registry.delete(id)
    }
  }
}

/** Get the full command list (built-ins + plugins), sorted for display. */
export function getSlashCommands(): SlashCommand[] {
  return [...registry.values()].sort((a, b) => {
    // Built-ins first (alphabetical), then plugins (alphabetical).
    const aPlugin = a.pluginID ? 1 : 0
    const bPlugin = b.pluginID ? 1 : 0
    if (aPlugin !== bPlugin) return aPlugin - bPlugin
    return a.label.localeCompare(b.label)
  })
}

/** Test-only: clear the registry. */
export function resetSlashRegistryForTests(): void {
  registry.clear()
}

// --- Built-in slash commands ---------------------------------------------
// Registered once at module load. The handlers are NOT set here — the editor
// dispatches built-in ids via its own handler (handleSlashSelect), so the
// registry entries are metadata-only for built-ins. Plugin entries carry
// their own onSelect handler.

registerSlashCommand({
  id: 'h1',
  label: 'Heading 1',
  description: 'Large section header',
  icon: 'format_size'
})
registerSlashCommand({
  id: 'today',
  label: 'Today',
  description: "Insert today's date",
  icon: 'calendar_today'
})
registerSlashCommand({
  id: 'embed',
  label: 'Embed Block',
  description: 'Insert a block embed',
  icon: 'link'
})
registerSlashCommand({
  id: 'template',
  label: 'Template',
  description: 'Insert a page template at cursor',
  icon: 'content_copy'
})

// --- Inline formatting commands (#168) ------------------------------------
// Metadata-only built-ins; the editor dispatches them by id via
// handleSlashSelect. Each toggles its mark on the current selection. `hotkey`
// is the config action name — the display binding is resolved at render time
// (CommandPalette) so the slash hint always tracks the user's actual keymap.
registerSlashCommand({
  id: 'bold',
  label: 'Bold',
  description: 'Make the selection bold',
  icon: 'format_bold',
  hotkey: 'format_bold'
})
registerSlashCommand({
  id: 'italic',
  label: 'Italic',
  description: 'Make the selection italic',
  icon: 'format_italic',
  hotkey: 'format_italic'
})
registerSlashCommand({
  id: 'underline',
  label: 'Underline',
  description: 'Underline the selection',
  icon: 'format_underlined',
  hotkey: 'format_underline'
})
registerSlashCommand({
  id: 'strike',
  label: 'Strikethrough',
  description: 'Cross out the selection',
  icon: 'format_strikethrough',
  hotkey: 'format_strike'
})
registerSlashCommand({
  id: 'code',
  label: 'Inline code',
  description: 'Format as inline code',
  icon: 'code',
  hotkey: 'format_code'
})
registerSlashCommand({
  id: 'highlight',
  label: 'Highlight',
  description: 'Highlight the selection',
  icon: 'highlight',
  hotkey: 'format_highlight'
})
registerSlashCommand({
  id: 'subscript',
  label: 'Subscript',
  description: 'Lower the selection below the line',
  icon: 'subscript',
  hotkey: 'format_subscript'
})
registerSlashCommand({
  id: 'superscript',
  label: 'Superscript',
  description: 'Raise the selection above the line',
  icon: 'superscript',
  hotkey: 'format_superscript'
})
// --- Heading / block-type commands (#169) ---------------------------------
registerSlashCommand({
  id: 'h2',
  label: 'Heading 2',
  description: 'Convert the block to an H2',
  icon: 'format_size',
  hotkey: 'set_h2'
})
registerSlashCommand({
  id: 'h3',
  label: 'Heading 3',
  description: 'Convert the block to an H3',
  icon: 'format_size',
  hotkey: 'set_h3'
})
registerSlashCommand({
  id: 'h4',
  label: 'Heading 4',
  description: 'Convert the block to an H4',
  icon: 'format_size',
  hotkey: 'set_h4'
})
registerSlashCommand({
  id: 'h5',
  label: 'Heading 5',
  description: 'Convert the block to an H5',
  icon: 'format_size',
  hotkey: 'set_h5'
})
registerSlashCommand({
  id: 'h6',
  label: 'Heading 6',
  description: 'Convert the block to an H6',
  icon: 'format_size',
  hotkey: 'set_h6'
})
registerSlashCommand({
  id: 'note',
  label: 'Plain note',
  description: 'Convert the block to a plain note (strip header / task)',
  icon: 'notes',
  hotkey: 'set_note'
})
registerSlashCommand({
  id: 'task',
  label: 'Task',
  description: 'Convert the block to a task',
  icon: 'check_box',
  hotkey: 'set_task'
})

// --- Text alignment commands (#173) ---------------------------------------
registerSlashCommand({
  id: 'align-left',
  label: 'Align left',
  description: 'Align the current block to the left',
  icon: 'format_align_left',
  hotkey: 'align_left'
})
registerSlashCommand({
  id: 'align-center',
  label: 'Align center',
  description: 'Center the current block',
  icon: 'format_align_center',
  hotkey: 'align_center'
})
registerSlashCommand({
  id: 'align-right',
  label: 'Align right',
  description: 'Align the current block to the right',
  icon: 'format_align_right',
  hotkey: 'align_right'
})
registerSlashCommand({
  id: 'align-justify',
  label: 'Align justify',
  description: 'Justify the current block',
  icon: 'format_align_justify',
  hotkey: 'align_justify'
})

// --- Quote / blockquote (#188) --------------------------------------------
registerSlashCommand({
  id: 'quote',
  label: 'Quote',
  description: 'Toggle a blockquote on the current block',
  icon: 'format_quote',
  hotkey: 'toggle_quote'
})

// --- Callouts / admonitions (#180) ----------------------------------------
// `/callout` inserts a default note callout; per-variant slash cmds insert
// directly. Change variant later via the NodeView icon picker (#658).
registerSlashCommand({
  id: 'callout',
  label: 'Callout',
  description: 'Insert a Note callout',
  icon: 'info'
})
registerSlashCommand({
  id: 'callout-note',
  label: 'Callout: Note',
  description: 'Insert a note callout',
  icon: 'info'
})
registerSlashCommand({
  id: 'callout-info',
  label: 'Callout: Info',
  description: 'Insert an info callout',
  icon: 'campaign'
})
registerSlashCommand({
  id: 'callout-tip',
  label: 'Callout: Tip',
  description: 'Insert a tip callout',
  icon: 'lightbulb'
})
registerSlashCommand({
  id: 'callout-warning',
  label: 'Callout: Warning',
  description: 'Insert a warning callout',
  icon: 'warning'
})
registerSlashCommand({
  id: 'callout-danger',
  label: 'Callout: Danger',
  description: 'Insert a danger callout',
  icon: 'error'
})
registerSlashCommand({
  id: 'callout-success',
  label: 'Callout: Success',
  description: 'Insert a success callout',
  icon: 'check_circle'
})

// --- Code blocks (#189) ---------------------------------------------------
registerSlashCommand({
  id: 'code-block',
  label: 'Code block',
  description: 'Insert a fenced code block with syntax highlighting',
  icon: 'code_blocks'
})

// --- Mermaid diagrams -----------------------------------------------------
// Mermaid is a render branch on codeBlock (ARCHITECTURE §5.1): a fence whose
// language is `mermaid` renders an SVG via useMermaid. This command inserts
// such a fence so the feature is discoverable through the slash menu instead
// of requiring the user to remember the ```mermaid syntax.
registerSlashCommand({
  id: 'mermaid',
  label: 'Mermaid diagram',
  description: 'Insert a Mermaid diagram code block',
  icon: 'schema'
})

// --- Block math (#191) ----------------------------------------------------
registerSlashCommand({
  id: 'math',
  label: 'Math equation',
  description: 'Insert a centered LaTeX equation ($$…$$) rendered with KaTeX',
  icon: 'functions'
})

// --- Foldable details (#183) ----------------------------------------------
// Note: `toggle_details` config action only opens/closes an EXISTING
// <details>; this slash command INSERTS a new one. They're different ops,
// so this entry carries no `hotkey` (the legacy `Ctrl+Shift+.` hint was
// misleading — that binding toggles open/close, not insert).
registerSlashCommand({
  id: 'details',
  label: 'Foldable section',
  description: 'Insert a collapsible <details> section',
  icon: 'unfold_more'
})

// --- GFM tables (#172) ----------------------------------------------------
registerSlashCommand({
  id: 'table',
  label: 'Table',
  description: 'Insert a 3×3 table',
  icon: 'table_view'
})
registerSlashCommand({
  id: 'table-custom',
  label: 'Custom table…',
  description: 'Insert a table with custom dimensions',
  icon: 'grid_view'
})

// --- Color commands (#170) ------------------------------------------------
registerSlashCommand({
  id: 'text-color',
  label: 'Text color',
  description: 'Pick a text color for the selection',
  icon: 'palette'
})
registerSlashCommand({
  id: 'background-color',
  label: 'Background color',
  description: 'Pick a background color for the selection',
  icon: 'format_color_fill'
})
