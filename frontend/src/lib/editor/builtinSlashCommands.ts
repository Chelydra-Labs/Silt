// Pure classification of a built-in slash-command id into a structured intent,
// extracted from TipTapEditor.svelte's handleSlashSelect. The ~20-command
// dispatch was a 115-line if/else buried inline with no direct unit test; this
// module makes the command surface (id → intent) testable independent of the
// live editor instance. Editor side-effects (convertToBlock, popover state,
// chain().run()) stay in the component, which switch-executes the intent.
//
// A null result means "not a built-in" — the component then looks the id up in
// the plugin slash-command registry (getSlashCommands from slash-registry.ts).

// Maps inline-formatting slash-command ids to their TipTap mark type. Each
// toggles a stored mark that is valid at a collapsed cursor, so they remain in
// the slash catalog. Link / clear-formatting / remove-color are selection-only
// no-ops at a collapsed cursor and were dropped from the catalog (#592); their
// toolbar, selection-bubble, and hotkey entry points call TipTap directly.
export const FORMAT_COMMANDS: Record<string, string> = {
  bold: 'bold',
  italic: 'italic',
  underline: 'underline',
  strike: 'strike',
  code: 'code',
  highlight: 'highlight',
  subscript: 'subscript',
  superscript: 'superscript'
}

export type SlashIntent =
  | {
      kind: 'convert'
      blockType: 'taskBlock' | 'headerBlock' | 'noteBlock'
      depth?: number
    }
  | { kind: 'align'; align: 'left' | 'center' | 'right' | 'justify' }
  | { kind: 'quote' }
  | { kind: 'callout'; variant: string }
  | { kind: 'codeBlock'; language?: string }
  | { kind: 'math' }
  | { kind: 'details' }
  | { kind: 'table'; rows: number; cols: number }
  | { kind: 'tableCustom' }
  | { kind: 'color'; markType: 'textColor' | 'highlight' }
  | { kind: 'today' }
  | { kind: 'calendar' }
  | { kind: 'shortcuts' }
  | { kind: 'embed' }
  | { kind: 'template' }
  | { kind: 'type' }
  | { kind: 'format'; mark: string }

const ALIGNED: Record<string, 'left' | 'center' | 'right' | 'justify'> = {
  'align-left': 'left',
  'align-center': 'center',
  'align-right': 'right',
  'align-justify': 'justify'
}

// classifySlashCommand returns the structured intent for a built-in slash
// command id, or null when the id is not a built-in (a plugin command or
// unknown). Pure and deterministic; 'today' deliberately carries no date — the
// component computes the current date at execution time.
export function classifySlashCommand(commandId: string): SlashIntent | null {
  switch (commandId) {
    case 'task':
      return { kind: 'convert', blockType: 'taskBlock' }
    case 'h1':
      return { kind: 'convert', blockType: 'headerBlock', depth: 1 }
    case 'h2':
      return { kind: 'convert', blockType: 'headerBlock', depth: 2 }
    case 'h3':
      return { kind: 'convert', blockType: 'headerBlock', depth: 3 }
    case 'h4':
      return { kind: 'convert', blockType: 'headerBlock', depth: 4 }
    case 'h5':
      return { kind: 'convert', blockType: 'headerBlock', depth: 5 }
    case 'h6':
      return { kind: 'convert', blockType: 'headerBlock', depth: 6 }
    case 'note':
      return { kind: 'convert', blockType: 'noteBlock' }
    case 'quote':
      return { kind: 'quote' }
    case 'code-block':
      return { kind: 'codeBlock' }
    case 'mermaid':
      return { kind: 'codeBlock', language: 'mermaid' }
    case 'math':
      return { kind: 'math' }
    case 'details':
      return { kind: 'details' }
    case 'table':
      return { kind: 'table', rows: 3, cols: 3 }
    case 'table-custom':
      return { kind: 'tableCustom' }
    case 'text-color':
      return { kind: 'color', markType: 'textColor' }
    case 'background-color':
      return { kind: 'color', markType: 'highlight' }
    case 'today':
      return { kind: 'today' }
    case 'calendar':
      return { kind: 'calendar' }
    case 'shortcuts':
      return { kind: 'shortcuts' }
    case 'embed':
      return { kind: 'embed' }
    case 'template':
      return { kind: 'template' }
    case 'type':
      return { kind: 'type' }
    case 'callout':
      return { kind: 'callout', variant: 'note' }
    default:
      if (commandId.startsWith('callout-')) {
        return { kind: 'callout', variant: commandId.slice('callout-'.length) }
      }
      if (ALIGNED[commandId]) {
        return { kind: 'align', align: ALIGNED[commandId] }
      }
      if (FORMAT_COMMANDS[commandId]) {
        return { kind: 'format', mark: FORMAT_COMMANDS[commandId] }
      }
      return null
  }
}
