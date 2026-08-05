// Hotkey parsing/matching for config-driven key bindings. Parses the
// "Ctrl+Shift+X" notation stored in config.hotkeys into a modifier+key tuple
// and matches it against a KeyboardEvent. Supports single-character keys and
// digit keys with modifier combos (the global shortcuts Silt binds today).

export interface ParsedHotkey {
  ctrl: boolean
  shift: boolean
  alt: boolean
  meta: boolean
  key: string // lower-cased logical key, e.g. "p", "b", "/"
}

// Config bindings may use KeyboardEvent.code-style names ("Slash", "Period")
// whose corresponding KeyboardEvent.key is a different character ("/", ".").
// Without normalization, a binding like "Ctrl+Slash" would parse to key
// "slash" and never match e.key "/". Map the common named tokens to their
// KeyboardEvent.key value (lower-cased) so either spelling works.
const KEY_ALIASES: Record<string, string> = {
  slash: '/',
  backslash: '\\',
  period: '.',
  comma: ',',
  semicolon: ';',
  quote: "'",
  backquote: '`',
  minus: '-',
  equal: '=',
  bracketleft: '[',
  bracketright: ']',
  space: ' ',
  escape: 'escape',
  esc: 'escape',
  enter: 'enter',
  return: 'enter',
  tab: 'tab',
  delete: 'delete',
  del: 'delete',
  backspace: 'backspace',
  insert: 'insert',
  home: 'home',
  end: 'end',
  pageup: 'pageup',
  pagedown: 'pagedown',
  arrowup: 'arrowup',
  arrowdown: 'arrowdown',
  arrowleft: 'arrowleft',
  arrowright: 'arrowright',
  // Short-form arrow tokens (e.g. "Ctrl+Alt+Right") normalize to the
  // KeyboardEvent.key form so they match arrow-key events. Mirrors the
  // PM_KEY_NORMALIZE mapping used by the ProseMirror keymap converter.
  up: 'arrowup',
  down: 'arrowdown',
  left: 'arrowleft',
  right: 'arrowright'
}

/** Parse a "Ctrl+Shift+P"-style binding. Returns null for empty/invalid input. */
export function parseHotkey(s: string | undefined | null): ParsedHotkey | null {
  if (!s) return null
  const parts = s
    .toLowerCase()
    .split('+')
    .map((p) => p.trim())
    .filter(Boolean)
  if (parts.length === 0) return null

  let ctrl = false
  let shift = false
  let alt = false
  let meta = false
  let key = ''

  for (const p of parts) {
    switch (p) {
      case 'ctrl':
      case 'control':
        ctrl = true
        break
      case 'shift':
        shift = true
        break
      case 'alt':
      case 'option':
        alt = true
        break
      case 'meta':
      case 'cmd':
      case 'command':
      case 'win':
        meta = true
        break
      default:
        key = p
    }
  }
  if (!key) return null
  // Normalize named keys (slash → /) so matching works against KeyboardEvent.key.
  key = KEY_ALIASES[key] ?? key
  return { ctrl, shift, alt, meta, key }
}

/**
 * Format a ParsedHotkey to the canonical config binding string
 * (e.g. "Ctrl+Shift+9"). Modifier order is stable: Ctrl, Alt, Shift, Meta.
 *
 * Named tokens (Space, ArrowUp, Escape, …) must re-parse via parseHotkey and
 * convert via configKeyToProseMirrorKey — capture uses KeyboardEvent.key forms
 * that free-typed short names (Up) also accept.
 */
export function formatHotkey(h: ParsedHotkey): string {
  const parts: string[] = []
  if (h.ctrl) parts.push('Ctrl')
  if (h.alt) parts.push('Alt')
  if (h.shift) parts.push('Shift')
  if (h.meta) parts.push('Meta')
  // Display single-character keys uppercased for letters; keep symbols as-is.
  const key =
    h.key.length === 1 && /[a-z]/.test(h.key) ? h.key.toUpperCase() : h.key
  // Named special keys for readability and stable re-parse tokens.
  // Space must be "Space" not " " — parseHotkey filters empty segments after trim.
  // Arrows must be ArrowUp (not Arrowup) so PM_KEY_NORMALIZE / PM can match.
  const named =
    key === ' ' || key === 'space'
      ? 'Space'
      : key === 'escape'
        ? 'Escape'
        : key === 'enter'
          ? 'Enter'
          : key === 'tab'
            ? 'Tab'
            : key === 'backspace'
              ? 'Backspace'
              : key === 'delete'
                ? 'Delete'
                : key.startsWith('arrow') && key.length > 5
                  ? 'Arrow' + key.charAt(5).toUpperCase() + key.slice(6)
                  : key
  parts.push(named)
  return parts.join('+')
}

const MODIFIER_KEYS = new Set(['control', 'shift', 'alt', 'meta', 'altgraph'])

/**
 * Build a ParsedHotkey from a KeyboardEvent during capture mode.
 * Returns null for pure-modifier keydowns (user is still holding modifiers)
 * and for Dead/Unidentified keys (IME composition / international layouts).
 */
export function hotkeyFromKeyboardEvent(e: KeyboardEvent): ParsedHotkey | null {
  const raw = e.key
  if (!raw || raw === 'Dead' || raw === 'Unidentified') return null
  if (MODIFIER_KEYS.has(raw.toLowerCase())) return null

  let key = raw
  // Prefer e.key logical value; normalize aliases the same way parseHotkey does.
  if (key.length === 1) {
    key = key.toLowerCase()
  } else {
    key = key.toLowerCase()
    key = KEY_ALIASES[key] ?? key
  }

  return {
    ctrl: e.ctrlKey,
    shift: e.shiftKey,
    alt: e.altKey,
    meta: e.metaKey,
    key
  }
}

/** True if a KeyboardEvent matches the given binding string. */
export function matchHotkey(
  e: KeyboardEvent,
  binding: string | undefined | null
): boolean {
  const h = parseHotkey(binding)
  if (!h) return false
  // Compares against e.key (the logical glyph). For Shift + a punctuation key
  // whose shifted glyph differs from the base (e.g. "," -> "<" on US layouts),
  // e.key is the shifted glyph, so a binding like "Ctrl+Shift+," would never
  // match here. No global default uses such a chord today — the only Shift+
  // punctuation default (format_subscript) is consumed by the editor keymap,
  // not this path. If a global Shift+punctuation binding is ever added, fall
  // back to e.code (layout-stable) when e.key doesn't match.
  return (
    e.ctrlKey === h.ctrl &&
    e.shiftKey === h.shift &&
    e.altKey === h.alt &&
    e.metaKey === h.meta &&
    e.key.toLowerCase() === h.key
  )
}

// ---- Config → ProseMirror keymap converter (#311) -------------------------
// TipTap's addKeyboardShortcuts returns a static { 'Mod-Shift-9': handler }
// map registered at editor-creation time. The config entries in config.yaml
// use "Ctrl+Shift+9" notation. This converter bridges the two formats so the
// editor honors user-remapped hotkeys at creation time.
//
// Per prosemirror-keymap source (verified from node_modules):
// - Separator is '-', NOT '+'.
// - 'Mod' = Cmd on Mac, Ctrl everywhere else.
// - Modifier order is normalized (input order doesn't matter).
// - Special keys use KeyboardEvent.key names: ArrowUp, ArrowDown, etc.
// - Letters must be lowercase (uppercase implies Shift).
// - Punctuation keys (., /, ,) are single-character key names.

// Map arrow/direction names from config notation to KeyboardEvent.key names.
// Short forms (up) come from free-typed config; long forms (arrowup) come from
// the capture widget (KeyboardEvent.key lowercased) and formatHotkey output
// after parseHotkey normalizes "ArrowUp" → "arrowup".
const PM_KEY_NORMALIZE: Record<string, string> = {
  up: 'ArrowUp',
  down: 'ArrowDown',
  left: 'ArrowLeft',
  right: 'ArrowRight',
  arrowup: 'ArrowUp',
  arrowdown: 'ArrowDown',
  arrowleft: 'ArrowLeft',
  arrowright: 'ArrowRight',
  space: ' ',
  ' ': ' ',
  esc: 'Escape',
  del: 'Delete',
  // Named special keys must match KeyboardEvent.key / prosemirror-keymap
  // (capitalized). parseHotkey lowercases tokens, so without this "Tab" and
  // "Shift+Tab" would become "tab" / "Shift-tab" and never fire.
  tab: 'Tab',
  enter: 'Enter',
  escape: 'Escape',
  backspace: 'Backspace',
  delete: 'Delete'
}

/**
 * Convert a config-style binding ("Ctrl+Shift+9") to a ProseMirror keymap
 * binding string ("Mod-Shift-9"). Returns '' for empty/invalid input.
 */
export function configKeyToProseMirrorKey(
  binding: string | undefined | null
): string {
  if (!binding) return ''
  const parsed = parseHotkey(binding)
  if (!parsed || !parsed.key) return ''

  const mods: string[] = []
  // Mod first (matches the codebase convention: Mod-Alt-1, Mod-Shift-9).
  // PM normalizes modifier order on its end anyway, so this is cosmetic.
  if (parsed.ctrl || parsed.meta) mods.push('Mod')
  if (parsed.alt) mods.push('Alt')
  if (parsed.shift) mods.push('Shift')

  const key = PM_KEY_NORMALIZE[parsed.key] ?? parsed.key
  return [...mods, key].join('-')
}

/**
 * Resolve a keyboard shortcut from config, falling back to a default
 * ProseMirror key string. Reads hotkeys[configKey], converts via
 * configKeyToProseMirrorKey, and returns the result.
 *
 * An explicitly empty binding ("") means the user disabled the shortcut
 * ("Leave empty to disable" in HotkeysTab) — returns '' so callers omit the
 * keymap entry instead of restoring the default. Only an ABSENT (undefined)
 * or unparseable entry falls back to defaultPmKey.
 */
export function resolveShortcut(
  configKey: string,
  defaultPmKey: string,
  hotkeys: Record<string, string | undefined>
): string {
  const configBinding = hotkeys[configKey]
  // Explicitly empty = disabled (HotkeysTab "Leave empty to disable").
  if (configBinding === '') return ''
  const converted = configKeyToProseMirrorKey(configBinding)
  return converted || defaultPmKey
}

/**
 * Resolve a hotkey's DISPLAY binding (e.g. "Ctrl+Shift+9") for tooltips,
 * slash-menu hints, and aria-keyshortcuts. Returns the configured binding
 * straight from the hotkeys map — no ProseMirror conversion — so the display
 * always matches what the user sees in config.yaml. Returns '' when the
 * action is absent or explicitly disabled (set to ""), so callers can omit
 * the hint / aria attribute entirely for unbound actions. Unlike
 * {@link resolveShortcut} (which returns a ProseMirror keystring for
 * keymaps), this returns the human-readable config binding.
 */
export function resolveHotkeyDisplay(
  action: string,
  hotkeys: Record<string, string | undefined>
): string {
  const binding = hotkeys[action]
  return binding ?? ''
}
