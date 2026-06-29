// Slash-command registry tests (#110, #158).
import { describe, expect, it, beforeEach, vi } from 'vitest'

// Mock the grants module so the registry-internal gate can be controlled
// per-test without hitting wailsjs IPC (#158).
vi.mock('../../plugins/grants.svelte', () => ({
  isGranted: vi.fn(() => true),
  initGrants: vi.fn(),
  refreshGrants: vi.fn(),
  resetGrantsForTests: vi.fn(),
  setGrantsForTests: vi.fn()
}))

import {
  registerSlashCommand,
  unregisterSlashCommand,
  unregisterPluginSlashCommands,
  getSlashCommands,
  resetSlashRegistryForTests
} from './slash-registry'
import { isGranted } from '../../plugins/grants.svelte'

describe('slash-command registry (#110, #158)', () => {
  beforeEach(() => {
    resetSlashRegistryForTests()
    vi.mocked(isGranted).mockReturnValue(true)
  })

  it('registers and retrieves a command', () => {
    registerSlashCommand({ id: 'todo', label: 'Task', icon: 'check_box' })
    const cmds = getSlashCommands()
    expect(cmds).toHaveLength(1)
    expect(cmds[0].label).toBe('Task')
  })

  it('built-ins + plugin commands are sorted (built-ins first)', () => {
    registerSlashCommand({ id: 'todo', label: 'Task' })
    registerSlashCommand({ id: 'h1', label: 'Heading' })
    registerSlashCommand({
      id: 'attach-plugin:attach',
      label: 'Attach File',
      pluginID: 'attach-plugin'
    })
    const cmds = getSlashCommands()
    // Built-ins first (alphabetical), then plugin commands.
    expect(cmds[0].id).toBe('h1')
    expect(cmds[1].id).toBe('todo')
    expect(cmds[2].id).toBe('attach-plugin:attach')
    expect(cmds[2].pluginID).toBe('attach-plugin')
  })

  it('unregister removes a single command', () => {
    registerSlashCommand({ id: 'todo', label: 'Task' })
    unregisterSlashCommand('todo')
    expect(getSlashCommands()).toHaveLength(0)
  })

  it('unregisterPluginSlashCommands removes all commands for a plugin', () => {
    registerSlashCommand({ id: 'todo', label: 'Task' })
    registerSlashCommand({ id: 'p:cmd1', label: 'One', pluginID: 'p' })
    registerSlashCommand({ id: 'p:cmd2', label: 'Two', pluginID: 'p' })
    unregisterPluginSlashCommands('p')
    const cmds = getSlashCommands()
    expect(cmds).toHaveLength(1)
    expect(cmds[0].id).toBe('todo')
  })

  it('re-registering the same id replaces the entry', () => {
    registerSlashCommand({ id: 'todo', label: 'Old' })
    registerSlashCommand({ id: 'todo', label: 'New' })
    expect(getSlashCommands()).toHaveLength(1)
    expect(getSlashCommands()[0].label).toBe('New')
  })

  it('rejects a command without id or label', () => {
    expect(() => registerSlashCommand({ id: '', label: 'X' })).toThrow()
    expect(() => registerSlashCommand({ id: 'x', label: '' } as any)).toThrow()
  })

  // --- #158: registry-internal capability gate -------------------------------

  it('refuses plugin commands without editor-schema grant', () => {
    vi.mocked(isGranted).mockReturnValue(false)
    registerSlashCommand({
      id: 'ungranted:cmd',
      label: 'Blocked',
      pluginID: 'ungranted'
    })
    expect(getSlashCommands()).toHaveLength(0)
  })

  it('built-in commands (no pluginID) bypass the gate even when ungranted', () => {
    vi.mocked(isGranted).mockReturnValue(false)
    registerSlashCommand({ id: 'builtin', label: 'Built-in' })
    expect(getSlashCommands()).toHaveLength(1)
    expect(getSlashCommands()[0].id).toBe('builtin')
  })
})

describe('formatting slash commands (#168)', () => {
  beforeEach(() => {
    resetSlashRegistryForTests()
    vi.mocked(isGranted).mockReturnValue(true)
  })

  it('registers formatting commands keyed to hotkey ACTION NAMES (not bindings)', () => {
    // The display binding is resolved at render time from config — the
    // registry stores the action name only. A stale literal here would defeat
    // the whole point of the refactor.
    const formatCmds = [
      { id: 'bold', label: 'Bold', icon: 'format_bold', hotkey: 'format_bold' },
      {
        id: 'italic',
        label: 'Italic',
        icon: 'format_italic',
        hotkey: 'format_italic'
      },
      {
        id: 'underline',
        label: 'Underline',
        icon: 'format_underlined',
        hotkey: 'format_underline'
      },
      {
        id: 'strike',
        label: 'Strikethrough',
        icon: 'format_strikethrough',
        hotkey: 'format_strike'
      },
      { id: 'code', label: 'Inline code', icon: 'code', hotkey: 'format_code' },
      {
        id: 'highlight',
        label: 'Highlight',
        icon: 'highlight',
        hotkey: 'format_highlight'
      },
      {
        id: 'subscript',
        label: 'Subscript',
        icon: 'subscript',
        hotkey: 'format_subscript'
      },
      {
        id: 'superscript',
        label: 'Superscript',
        icon: 'superscript',
        hotkey: 'format_superscript'
      },
      { id: 'link', label: 'Link', icon: 'link', hotkey: 'format_link' }
    ]
    for (const cmd of formatCmds) {
      registerSlashCommand(cmd)
    }

    const cmds = getSlashCommands()
    expect(cmds).toHaveLength(formatCmds.length)

    const bold = cmds.find((c) => c.id === 'bold')
    expect(bold?.label).toBe('Bold')
    expect(bold?.hotkey).toBe('format_bold')
    expect(bold?.icon).toBe('format_bold')

    const strike = cmds.find((c) => c.id === 'strike')
    expect(strike?.hotkey).toBe('format_strike')

    const sub = cmds.find((c) => c.id === 'subscript')
    expect(sub?.hotkey).toBe('format_subscript')
  })

  it('clear-formatting carries no hotkey (no config action backs it)', () => {
    registerSlashCommand({
      id: 'clear-formatting',
      label: 'Clear formatting',
      icon: 'format_clear'
    })
    const clear = getSlashCommands().find((c) => c.id === 'clear-formatting')
    expect(clear?.hotkey).toBeUndefined()
    expect(clear?.shortcut).toBeUndefined()
  })

  it('non-hotkey entries keep using `shortcut` for slash-trigger characters', () => {
    registerSlashCommand({
      id: 'todo',
      label: 'Task',
      icon: 'check_box',
      shortcut: '[]'
    })
    registerSlashCommand({
      id: 'h1',
      label: 'Heading 1',
      icon: 'format_size',
      shortcut: '#'
    })
    const cmds = getSlashCommands()
    expect(cmds.find((c) => c.id === 'todo')?.shortcut).toBe('[]')
    expect(cmds.find((c) => c.id === 'h1')?.shortcut).toBe('#')
    // Trigger-char entries do NOT carry a hotkey action.
    expect(cmds.find((c) => c.id === 'todo')?.hotkey).toBeUndefined()
  })
})
