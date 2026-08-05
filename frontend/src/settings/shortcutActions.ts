export type ShortcutGroup =
  'Editor' | 'Navigation' | 'Search' | 'Tabs' | 'Templates' | 'Tasks' | 'App'

export interface ShortcutActionDefinition {
  id: string
  label: string
  group: ShortcutGroup
  defaultBinding?: string
}

export const SHORTCUT_ACTIONS: ShortcutActionDefinition[] = [
  {
    id: 'new_page',
    label: 'New page',
    group: 'Navigation',
    defaultBinding: 'Ctrl+N'
  },
  {
    id: 'new_section',
    label: 'New section',
    group: 'Navigation',
    defaultBinding: 'Ctrl+Alt+N'
  },
  {
    id: 'new_notebook',
    label: 'New notebook',
    group: 'Navigation',
    defaultBinding: 'Ctrl+Alt+Shift+N'
  },
  {
    id: 'open_quick_switcher',
    label: 'Switch page',
    group: 'Navigation',
    defaultBinding: 'Ctrl+P'
  },
  {
    id: 'toggle_sidebar',
    label: 'Show or hide sidebar',
    group: 'Navigation',
    defaultBinding: 'Ctrl+\\'
  },
  {
    id: 'focus_sidebar',
    label: 'Focus sidebar',
    group: 'Navigation',
    defaultBinding: 'Ctrl+Shift+B'
  },
  {
    id: 'cycle_view_layout',
    label: 'Cycle view',
    group: 'Navigation',
    defaultBinding: 'Ctrl+Alt+V'
  },
  {
    id: 'open_search',
    label: 'Search vault',
    group: 'Search',
    defaultBinding: 'Ctrl+Shift+F'
  },
  {
    id: 'find_in_page',
    label: 'Find in page',
    group: 'Search',
    defaultBinding: 'Ctrl+F'
  },
  {
    id: 'replace',
    label: 'Replace in page',
    group: 'Search',
    defaultBinding: 'Ctrl+H'
  },
  {
    id: 'global_replace',
    label: 'Replace in vault',
    group: 'Search',
    defaultBinding: 'Ctrl+Shift+G'
  },
  {
    id: 'next_tab',
    label: 'Next tab',
    group: 'Tabs',
    defaultBinding: 'Ctrl+Alt+Right'
  },
  {
    id: 'prev_tab',
    label: 'Previous tab',
    group: 'Tabs',
    defaultBinding: 'Ctrl+Alt+Left'
  },
  {
    id: 'close_tab',
    label: 'Close tab',
    group: 'Tabs',
    defaultBinding: 'Ctrl+Shift+W'
  },
  {
    id: 'open_template_picker',
    label: 'New page from template',
    group: 'Templates',
    defaultBinding: 'Ctrl+Shift+T'
  },
  {
    id: 'new_task',
    label: 'New task',
    group: 'Tasks',
    defaultBinding: 'Ctrl+Shift+N'
  },
  {
    id: 'format_bold',
    label: 'Bold',
    group: 'Editor',
    defaultBinding: 'Ctrl+B'
  },
  {
    id: 'format_italic',
    label: 'Italic',
    group: 'Editor',
    defaultBinding: 'Ctrl+I'
  },
  {
    id: 'format_underline',
    label: 'Underline',
    group: 'Editor',
    defaultBinding: 'Ctrl+U'
  },
  {
    id: 'format_link',
    label: 'Add link',
    group: 'Editor',
    defaultBinding: 'Ctrl+K'
  },
  {
    id: 'indent_block',
    label: 'Indent block',
    group: 'Editor',
    defaultBinding: 'Tab'
  },
  {
    id: 'unindent_block',
    label: 'Unindent block',
    group: 'Editor',
    defaultBinding: 'Shift+Tab'
  },
  {
    id: 'toggle_bullet_list',
    label: 'Toggle bullet list',
    group: 'Editor',
    defaultBinding: 'Ctrl+Shift+8'
  },
  {
    id: 'toggle_ordered_list',
    label: 'Toggle numbered list',
    group: 'Editor',
    defaultBinding: 'Ctrl+Shift+7'
  },
  {
    id: 'toggle_view_mode',
    label: 'Toggle source view',
    group: 'Editor',
    defaultBinding: 'Ctrl+Alt+R'
  },
  {
    id: 'toggle_properties_panel',
    label: 'Edit page properties',
    group: 'Editor',
    defaultBinding: 'Ctrl+;'
  },
  {
    id: 'toggle_format_toolbar',
    label: 'Toggle formatting toolbar',
    group: 'Editor',
    defaultBinding: 'Ctrl+F1'
  },
  {
    id: 'toggle_focus_mode',
    label: 'Toggle focus mode',
    group: 'Editor',
    defaultBinding: 'Ctrl+Shift+D'
  },
  {
    id: 'toggle_typewriter_mode',
    label: 'Toggle typewriter mode',
    group: 'Editor',
    defaultBinding: 'Ctrl+Shift+Y'
  },
  {
    id: 'open_settings',
    label: 'Open settings',
    group: 'App',
    defaultBinding: 'Ctrl+,'
  },
  {
    id: 'open_shortcuts_help',
    label: 'Keyboard shortcuts',
    group: 'App',
    defaultBinding: 'Shift+?'
  },
  {
    id: 'open_date_glance',
    label: 'Date glance',
    group: 'App',
    defaultBinding: 'Ctrl+Alt+D'
  }
]

export const SHORTCUT_GROUPS: ShortcutGroup[] = [
  'Navigation',
  'Search',
  'Tabs',
  'Editor',
  'Templates',
  'Tasks',
  'App'
]

export function shortcutBinding(
  action: string,
  configured: Record<string, string | undefined>
): string {
  if (Object.prototype.hasOwnProperty.call(configured, action)) {
    return configured[action] ?? ''
  }
  return (
    SHORTCUT_ACTIONS.find((item) => item.id === action)?.defaultBinding ?? ''
  )
}

export function effectiveHotkeys(
  configured: Record<string, string | undefined>
): Record<string, string | undefined> {
  const result = { ...configured }
  for (const action of SHORTCUT_ACTIONS) {
    if (
      !Object.prototype.hasOwnProperty.call(result, action.id) &&
      action.defaultBinding
    ) {
      result[action.id] = action.defaultBinding
    }
  }
  return result
}

function inferredGroup(id: string): ShortcutGroup {
  if (
    /^(format_|set_|align_|table_|indent_|unindent_|toggle_quote|toggle_details|toggle_bullet_list|toggle_ordered_list)/.test(
      id
    )
  )
    return 'Editor'
  if (id.includes('template')) return 'Templates'
  if (id.startsWith('tasks_') || id === 'new_task') return 'Tasks'
  if (id.includes('tab')) return 'Tabs'
  if (/search|find|replace/.test(id)) return 'Search'
  if (/sidebar|view_layout|page|section|notebook|switcher/.test(id))
    return 'Navigation'
  return 'App'
}

export function shortcutActionDefinitions(
  configured: Record<string, string | undefined>
): ShortcutActionDefinition[] {
  const known = new Set(SHORTCUT_ACTIONS.map((action) => action.id))
  const extras = Object.keys(configured)
    .filter((id) => !known.has(id))
    .sort((a, b) => a.localeCompare(b))
    .map((id) => ({
      id,
      label: id
        .replace(/_/g, ' ')
        .replace(/\b\w/g, (letter) => letter.toUpperCase()),
      group: inferredGroup(id)
    }))
  return [...SHORTCUT_ACTIONS, ...extras]
}
