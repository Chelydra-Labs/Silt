// Deterministic fixture data for the dev-only browser visual-audit harness.
// Covers shell bootstrap, editor blocks/properties, tags, backlinks, search,
// type dashboards, tasks hub, and all 9 settings tabs so every view and modal
// renders populated states in a standalone browser without the Go backend.

import { flattenTheme } from '../../theme/flatten'
import type { ThemeDoc } from '../../theme/types'

export const AUDIT_APP_VERSION = 'v0.18.0-audit'
export const AUDIT_LOCAL_AUTHOR = 'Alex Rivera'
export const AUDIT_SIDEBAR_WIDTH = 264

export const AUDIT_NAVIGATION = {
  notebooks: [
    {
      name: 'Product',
      source: 'vault',
      sections: [
        {
          name: 'Architecture',
          path: 'Architecture',
          pages: [
            { name: 'Design System Roadmap', count: 7 },
            { name: 'IPC Transport Specification', count: 5 },
            { name: 'Token Hierarchy & Scales', count: 4 }
          ]
        },
        {
          name: 'Research',
          path: 'Research',
          pages: [
            { name: 'User Interview Synthesis', count: 6 },
            { name: 'Competitive Accessibility Notes', count: 3 }
          ]
        }
      ]
    },
    {
      name: 'Operations',
      source: 'vault',
      sections: [
        {
          name: 'Sprints',
          path: 'Sprints',
          pages: [
            { name: 'Q2 Launch Checklist', count: 8 },
            { name: 'Incident Retrospectives', count: 4 }
          ]
        }
      ]
    }
  ]
}

export const AUDIT_OPEN_TABS = {
  open_tabs: [
    {
      notebook: 'Product',
      section: 'Architecture',
      page: 'Design System Roadmap',
      view_mode: ''
    },
    {
      notebook: 'Product',
      section: 'Research',
      page: 'User Interview Synthesis',
      view_mode: ''
    }
  ],
  active_tab: {
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap'
  }
}

export const AUDIT_NAV_ORDER = {
  notebooks: ['Product', 'Operations'],
  sections: {
    Product: ['Architecture', 'Research'],
    Operations: ['Sprints']
  },
  pages: {
    'Product/Architecture': [
      'Design System Roadmap',
      'IPC Transport Specification',
      'Token Hierarchy & Scales'
    ],
    'Product/Research': [
      'User Interview Synthesis',
      'Competitive Accessibility Notes'
    ],
    'Operations/Sprints': ['Q2 Launch Checklist', 'Incident Retrospectives']
  }
}

export const AUDIT_CUSTOM_DICTIONARY = ['Silt', 'Wails', 'TipTap', 'Cyber-Ink']

export const AUDIT_MCP_CONFIG = {
  enabled: true,
  write_enabled: false,
  http_enabled: true,
  http_port: 17887
}

export const AUDIT_SYSTEM_CONFIG = {
  notebooks: {
    path: '/Users/alex/Documents/SiltVault',
    default_active: 'Product'
  },
  linked_notebooks: [
    {
      id: 'linked-eng',
      root_path: '/Users/alex/Shared/TeamHandbook',
      display_name: 'Team Handbook'
    }
  ],
  editor: {
    font_family: 'Inter',
    mono_font_family: 'JetBrains Mono',
    font_size_px: 15,
    line_height: 1.65,
    tab_indent_spaces: 2,
    auto_save_delay_ms: 800,
    focus_highlight_ancestors: true,
    show_word_count: true,
    focus_mode: false,
    default_view_mode: 'edit',
    spellcheck_enabled: true,
    spellcheck_language: 'en-US',
    date_format: 'YYYY-MM-DD',
    typewriter_mode: false,
    typewriter_mode_ratio: 0.5,
    auto_versioning_enabled: true,
    max_versions_per_page: 50,
    auto_versioning_min_interval_sec: 300,
    custom_dictionary: AUDIT_CUSTOM_DICTIONARY,
    spellcheck_domains: ['software', 'markdown']
  },
  parsing: {
    auto_inject_uuid: true,
    default_task_priority: 2
  },
  hotkeys: {
    search: 'Ctrl+K',
    quick_switcher: 'Ctrl+P',
    new_page: 'Ctrl+N',
    quick_add_task: 'Ctrl+Shift+A',
    open_settings: 'Ctrl+,',
    toggle_sidebar: 'Ctrl+\\',
    global_replace: 'Ctrl+Shift+H',
    shortcut_help: 'Ctrl+/'
  },
  plugins: {
    active: [
      'silt-tasks',
      'silt-ai-qa',
      'silt-ai-assistant',
      'silt-ai-summary'
    ],
    disabled: [],
    plugin_settings: {
      'silt-tasks': {
        default_display_mode: 'list',
        week_start: 'monday',
        show_completed: true
      }
    }
  },
  ui: {
    sidebar_width: AUDIT_SIDEBAR_WIDTH,
    nav_order: AUDIT_NAV_ORDER,
    open_tabs: AUDIT_OPEN_TABS.open_tabs,
    active_tab: AUDIT_OPEN_TABS.active_tab,
    expanded_sections: [
      { notebook: 'Product', path: 'Architecture' },
      { notebook: 'Product', path: 'Research' },
      { notebook: 'Operations', path: 'Sprints' }
    ],
    recent_pages: [
      {
        notebook: 'Product',
        section: 'Architecture',
        page: 'Design System Roadmap',
        opened_at: 1776520000
      },
      {
        notebook: 'Product',
        section: 'Research',
        page: 'User Interview Synthesis',
        opened_at: 1776510000
      }
    ],
    favorites: [
      {
        notebook: 'Product',
        section: 'Architecture',
        page: 'Design System Roadmap'
      }
    ],
    enable_preview_tabs: true,
    max_open_tabs: 12,
    show_format_toolbar: true,
    show_tab_dirty_indicators: true,
    dismissed_tips: [],
    sidebar_view: 'tree',
    open_devtools_on_startup: true,
    formatting: {
      typography_enabled: true,
      color_enabled: true,
      math_enabled: true
    },
    recent_tags: ['ui/tokens', 'design', 'a11y'],
    note_zoom: 1.0,
    dashboards: {}
  },
  ai: {
    chat: {
      provider_type: 'openai_compatible',
      base_url: 'https://api.openai.com/v1',
      model: 'gpt-4o-mini',
      temperature: 0.3,
      max_tokens: 4096,
      reasoning_effort: 'medium',
      timeout_ms: 30000
    },
    embedding: {
      provider_type: 'openai_compatible',
      base_url: 'https://api.openai.com/v1',
      model: 'text-embedding-3-small',
      timeout_ms: 30000,
      dimensions: 1536
    },
    features: {
      enabled: true,
      rag_enabled: true,
      summaries_enabled: true,
      agent_writes: 'confirm'
    },
    local_mcp: AUDIT_MCP_CONFIG,
    use_keyring: true
  }
}

export const AUDIT_PAGE_BLOCKS = [
  {
    id: 'blk-heading-1',
    parent_id: '',
    type: 'HEADER',
    depth: 0,
    raw_text: '# Design System Roadmap <!-- id: blk-heading-1 @ 2026-04-18 -->',
    clean_text: '# Design System Roadmap',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    line_number: 1,
    file_date: '2026-04-18',
    language: ''
  },
  {
    id: 'blk-note-1',
    parent_id: '',
    type: 'NOTE',
    depth: 0,
    raw_text:
      'Consolidating shared chrome primitives across all views to eliminate ad-hoc borders and spacing drift. Related to [[IPC Transport Specification]] and #ui/tokens. <!-- id: blk-note-1 @ 2026-04-18 -->',
    clean_text:
      'Consolidating shared chrome primitives across all views to eliminate ad-hoc borders and spacing drift. Related to [[IPC Transport Specification]] and #ui/tokens.',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    line_number: 3,
    file_date: '2026-04-18',
    language: ''
  },
  {
    id: 'blk-callout-1',
    parent_id: '',
    type: 'CALLOUT',
    depth: 0,
    raw_text:
      '> [!IMPORTANT] Visual Audit Standard\n> All primary views must use shared `Dialog`, `ViewHeader`, and `ViewState` primitives with semantic Cyber-Ink tokens.\n<!-- id: blk-callout-1 @ 2026-04-18 -->',
    clean_text:
      '> [!IMPORTANT] Visual Audit Standard\n> All primary views must use shared `Dialog`, `ViewHeader`, and `ViewState` primitives with semantic Cyber-Ink tokens.',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    line_number: 5,
    file_date: '2026-04-18',
    language: ''
  },
  {
    id: 'blk-task-1',
    parent_id: '',
    type: 'TASK',
    depth: 0,
    raw_text:
      '- [ ] Extract `ViewHeader` and `Dialog` components [owner:: alex] [due:: 2026-04-25] [priority:: 1] #ui/tokens <!-- id: blk-task-1 @ 2026-04-18 -->',
    clean_text: 'Extract `ViewHeader` and `Dialog` components',
    status: 'TODO',
    owner: 'alex',
    due_date: '2026-04-25',
    priority: 1,
    line_number: 8,
    file_date: '2026-04-18',
    language: ''
  },
  {
    id: 'blk-task-2',
    parent_id: '',
    type: 'TASK',
    depth: 0,
    raw_text:
      '- [/] Audit keyboard focus rings in dialogs and menus [owner:: sam] [due:: 2026-04-22] [priority:: 2] #a11y <!-- id: blk-task-2 @ 2026-04-18 -->',
    clean_text: 'Audit keyboard focus rings in dialogs and menus',
    status: 'DOING',
    owner: 'sam',
    due_date: '2026-04-22',
    priority: 2,
    line_number: 10,
    file_date: '2026-04-18',
    language: ''
  },
  {
    id: 'blk-code-1',
    parent_id: '',
    type: 'CODE',
    depth: 0,
    raw_text:
      '```ts\nexport interface ViewHeaderProps {\n  title: string\n  subtitle?: string\n  icon?: string\n}\n```\n<!-- id: blk-code-1 @ 2026-04-18 -->',
    clean_text:
      'export interface ViewHeaderProps {\n  title: string\n  subtitle?: string\n  icon?: string\n}',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    line_number: 12,
    file_date: '2026-04-18',
    language: 'ts'
  },
  {
    id: 'blk-table-1',
    parent_id: '',
    type: 'TABLE',
    depth: 0,
    raw_text:
      '| Surface | Primitive | Status |\n| --- | --- | --- |\n| TagsExplorer | ViewHeader | Planned |\n| TypeDashboard | ViewState | In Review |\n| SearchModal | Dialog | Planned |\n<!-- id: blk-table-1 @ 2026-04-18 -->',
    clean_text:
      '| Surface | Primitive | Status |\n| --- | --- | --- |\n| TagsExplorer | ViewHeader | Planned |\n| TypeDashboard | ViewState | In Review |\n| SearchModal | Dialog | Planned |',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    line_number: 20,
    file_date: '2026-04-18',
    language: ''
  }
]

export const AUDIT_PAGE_MARKDOWN = AUDIT_PAGE_BLOCKS.map(
  (b) => b.raw_text
).join('\n\n')

export const AUDIT_TYPES = [
  {
    id: 'project',
    name: 'Project',
    description: 'Cross-functional initiatives and engineering specs.',
    icon: 'rocket_launch',
    heroField: 'status',
    properties: [
      {
        name: 'status',
        label: 'Status',
        type: 'select',
        required: true,
        options: ['Backlog', 'In Progress', 'Review', 'Shipped'],
        default: 'Backlog'
      },
      {
        name: 'lead',
        label: 'Owner / Lead',
        type: 'text',
        required: false
      },
      {
        name: 'target_release',
        label: 'Target Release',
        type: 'text',
        required: false
      },
      {
        name: 'priority',
        label: 'Priority',
        type: 'select',
        required: false,
        options: ['Low', 'Medium', 'High', 'Critical'],
        default: 'Medium'
      }
    ]
  },
  {
    id: 'meeting',
    name: 'Meeting Note',
    description: 'Recurring syncs, design reviews, and retrospectives.',
    icon: 'groups',
    heroField: 'meeting_date',
    properties: [
      {
        name: 'meeting_date',
        label: 'Date',
        type: 'date',
        required: true
      },
      {
        name: 'cadence',
        label: 'Cadence',
        type: 'select',
        required: false,
        options: ['Weekly', 'Biweekly', 'Ad-hoc'],
        default: 'Weekly'
      },
      {
        name: 'facilitator',
        label: 'Facilitator',
        type: 'text',
        required: false
      }
    ]
  }
]

export const AUDIT_PAGE_TYPE_INFO = {
  typeId: 'project',
  type: AUDIT_TYPES[0],
  isSet: true,
  rawType: 'project'
}

export const AUDIT_PAGE_PROPERTIES = [
  {
    name: 'status',
    label: 'Status',
    type: 'select',
    value: 'In Progress',
    isSet: true,
    required: true,
    options: ['Backlog', 'In Progress', 'Review', 'Shipped']
  },
  {
    name: 'lead',
    label: 'Owner / Lead',
    type: 'text',
    value: 'Alex Rivera',
    isSet: true,
    required: false
  },
  {
    name: 'target_release',
    label: 'Target Release',
    type: 'text',
    value: 'v0.19.0',
    isSet: true,
    required: false
  },
  {
    name: 'priority',
    label: 'Priority',
    type: 'select',
    value: 'High',
    isSet: true,
    required: false,
    options: ['Low', 'Medium', 'High', 'Critical']
  }
]

export const AUDIT_PAGE_CORE_METADATA = {
  notebook: 'Product',
  section: 'Architecture',
  page: 'Design System Roadmap',
  type: 'project',
  date: '2026-04-18',
  tags: ['ui/tokens', 'design'],
  aliases: ['UI Refresh Roadmap'],
  created: '2026-04-10',
  modified: '2026-04-19T12:15:00Z',
  tagsAreReadOnly: false
}

export const AUDIT_TYPE_PAGES = [
  {
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    properties: [
      { name: 'status', valueText: 'In Progress', valueType: 'select' },
      { name: 'lead', valueText: 'Alex Rivera', valueType: 'text' },
      { name: 'target_release', valueText: 'v0.19.0', valueType: 'text' },
      { name: 'priority', valueText: 'High', valueType: 'select' }
    ]
  },
  {
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'IPC Transport Specification',
    properties: [
      { name: 'status', valueText: 'Review', valueType: 'select' },
      { name: 'lead', valueText: 'Sam Chen', valueType: 'text' },
      { name: 'target_release', valueText: 'v0.19.0', valueType: 'text' },
      { name: 'priority', valueText: 'Critical', valueType: 'select' }
    ]
  },
  {
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Token Hierarchy & Scales',
    properties: [
      { name: 'status', valueText: 'Backlog', valueType: 'select' },
      { name: 'lead', valueText: 'Morgan Vance', valueType: 'text' },
      { name: 'target_release', valueText: 'v0.20.0', valueType: 'text' },
      { name: 'priority', valueText: 'Medium', valueType: 'select' }
    ]
  },
  {
    source: 'vault',
    notebook: 'Operations',
    section: 'Sprints',
    page: 'Q2 Launch Checklist',
    properties: [
      { name: 'status', valueText: 'Shipped', valueType: 'select' },
      { name: 'lead', valueText: 'Alex Rivera', valueType: 'text' },
      { name: 'target_release', valueText: 'v0.18.0', valueType: 'text' },
      { name: 'priority', valueText: 'High', valueType: 'select' }
    ]
  }
]

export const AUDIT_TYPES_REPROJECTION_STATUS = {
  active: false,
  processed: 4,
  total: 4
}

export const AUDIT_TAG_HIERARCHY = [
  {
    level_0: 'ui',
    level_1: 'tokens',
    level_2: '',
    full_tag: 'ui/tokens',
    block_count: 3
  },
  {
    level_0: 'ui',
    level_1: 'modals',
    level_2: '',
    full_tag: 'ui/modals',
    block_count: 2
  },
  {
    level_0: 'design',
    level_1: '',
    level_2: '',
    full_tag: 'design',
    block_count: 2
  },
  {
    level_0: 'a11y',
    level_1: '',
    level_2: '',
    full_tag: 'a11y',
    block_count: 2
  },
  {
    level_0: 'release',
    level_1: 'q2',
    level_2: '',
    full_tag: 'release/q2',
    block_count: 1
  }
]

export const AUDIT_TAGGED_BLOCKS = [
  {
    id: 'blk-note-1',
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    type: 'note',
    clean_content:
      'Consolidating shared chrome primitives across all views to eliminate ad-hoc borders and spacing drift. Related to [[IPC Transport Specification]] and #ui/tokens.',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    file_date: '2026-04-18',
    full_tag: 'ui/tokens'
  },
  {
    id: 'blk-task-1',
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    type: 'task',
    clean_content: 'Extract `PageHeader` and `ModalShell` components',
    status: 'todo',
    owner: 'alex',
    due_date: '2026-04-25',
    priority: 1,
    file_date: '2026-04-18',
    full_tag: 'ui/tokens'
  },
  {
    id: 'blk-tok-3',
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Token Hierarchy & Scales',
    type: 'note',
    clean_content:
      'Standardize surface elevation tokens (`surface-elevated`, `surface-card`, `surface-hover`) across light and dark palettes #ui/tokens.',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    file_date: '2026-04-15',
    full_tag: 'ui/tokens'
  }
]

export const AUDIT_BACKLINKS_PAGE = {
  results: [
    {
      linkKind: 'page',
      source: 'vault',
      sourceNotebook: 'Product',
      sourceSection: 'Research',
      sourcePage: 'User Interview Synthesis',
      sourceBlockId: 'blk-res-10',
      snippet:
        'Participants praised keyboard speed but noted inconsistent header spacing — addressed in [[Design System Roadmap]].'
    },
    {
      linkKind: 'block-ref',
      source: 'vault',
      sourceNotebook: 'Operations',
      sourceSection: 'Sprints',
      sourcePage: 'Q2 Launch Checklist',
      sourceBlockId: 'blk-ops-04',
      snippet:
        'Verify all views against [[Design System Roadmap#^blk-callout-1]] before cutting release candidate.'
    }
  ],
  cursor: '',
  hasMore: false
}

export const AUDIT_UNLINKED_MENTIONS_PAGE = {
  results: [
    {
      source: 'vault',
      sourceNotebook: 'Product',
      sourceSection: 'Architecture',
      sourcePage: 'Token Hierarchy & Scales',
      sourceBlockIds: ['blk-tok-9'],
      sourceSnippets: [
        'See the Design System Roadmap for the rollout sequence across Settings and Tasks Hub.'
      ],
      matchCount: 1,
      title: 'Design System Roadmap',
      ambiguous: false,
      candidates: [],
      candidatesTruncated: false,
      candidatesTotal: 0
    }
  ],
  cursor: '',
  hasMore: false
}

export const AUDIT_SEARCH_RESULTS = {
  results: [
    {
      id: 'blk-note-1',
      source: 'vault',
      notebook: 'Product',
      section: 'Architecture',
      page: 'Design System Roadmap',
      file_date: '2026-04-18',
      clean_content:
        'Consolidating shared chrome primitives across all views to eliminate ad-hoc borders and spacing drift.',
      snippet:
        'Consolidating shared <mark>chrome</mark> primitives across all views to eliminate ad-hoc borders and spacing drift.'
    },
    {
      id: 'blk-task-1',
      source: 'vault',
      notebook: 'Product',
      section: 'Architecture',
      page: 'Design System Roadmap',
      file_date: '2026-04-18',
      clean_content: 'Extract PageHeader and ModalShell components',
      status: 'todo',
      owner: 'alex',
      due_date: '2026-04-25',
      priority: 1,
      snippet: 'Extract <mark>PageHeader</mark> and ModalShell components'
    },
    {
      id: 'blk-res-2',
      source: 'vault',
      notebook: 'Product',
      section: 'Research',
      page: 'User Interview Synthesis',
      file_date: '2026-04-12',
      clean_content:
        'Keyboard-first navigation between Notes, Tasks, and Type Dashboards is the top power-user workflow.',
      snippet:
        'Keyboard-first <mark>navigation</mark> between Notes, Tasks, and Type Dashboards is the top power-user workflow.'
    }
  ],
  total: 3,
  offset: 0,
  limit: 20,
  has_more: false
}

export const AUDIT_TASKS_ROWS = [
  {
    id: 'blk-task-1',
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    file_date: '2026-04-18',
    line_number: 8,
    clean_content: 'Extract ViewHeader and Dialog components',
    status: 'TODO',
    owner: 'alex',
    start_date: '2026-04-19',
    due_date: '2026-04-25',
    priority: 1,
    pinned: 0,
    progress: 0,
    recurrence: '',
    comments_count: 1,
    links_count: 1,
    created_at: '2026-04-18T09:00:00',
    completed_at: '',
    manual_order: 1000,
    modified_at: '2026-04-19T10:30:00',
    estimate_minutes: 90,
    subtask_total: 2,
    subtask_done: 1,
    tags: 'ui/tokens|design',
    blocked_by: '',
    is_blocked: 0
  },
  {
    id: 'blk-task-2',
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    file_date: '2026-04-18',
    line_number: 10,
    clean_content: 'Audit keyboard focus rings in dialogs and menus',
    status: 'DOING',
    owner: 'sam',
    start_date: '2026-04-18',
    due_date: '2026-04-22',
    priority: 2,
    pinned: 0,
    progress: 0,
    recurrence: '',
    comments_count: 0,
    links_count: 1,
    created_at: '2026-04-18T09:00:00',
    completed_at: '',
    manual_order: 2000,
    modified_at: '2026-04-19T10:30:00',
    estimate_minutes: 60,
    subtask_total: 0,
    subtask_done: 0,
    tags: 'a11y',
    blocked_by: '',
    is_blocked: 0
  },
  {
    id: 'blk-task-3',
    source: 'vault',
    notebook: 'Operations',
    section: 'Sprints',
    page: 'Q2 Launch Checklist',
    file_date: '2026-04-15',
    line_number: 4,
    clean_content: 'Publish baseline UI audit matrix and token inventory',
    status: 'DONE',
    owner: 'alex',
    start_date: '2026-04-17',
    due_date: '2026-04-19',
    priority: 2,
    pinned: 0,
    progress: 0,
    recurrence: '',
    comments_count: 2,
    links_count: 1,
    created_at: '2026-04-18T09:00:00',
    completed_at: '2026-04-19T16:00:00',
    manual_order: 3000,
    modified_at: '2026-04-19T10:30:00',
    estimate_minutes: 45,
    subtask_total: 0,
    subtask_done: 0,
    tags: 'release/q2',
    blocked_by: '',
    is_blocked: 0
  }
]

export const AUDIT_TASK_UNDO_STATE = {
  canUndo: true,
  remaining: 1,
  changedFields: ['status'],
  blockIds: ['blk-task-1'],
  notebook: 'Product',
  section: 'Architecture',
  page: 'Design System Roadmap',
  source: 'vault'
}

export const AUDIT_TASK_UNDO_RESULT = {
  undone: true,
  skipped: false,
  blockIds: ['blk-task-1'],
  skippedBlockIds: [],
  notebook: 'Product',
  section: 'Architecture',
  page: 'Design System Roadmap',
  removedSpawnedId: '',
  changedFields: ['status'],
  remaining: 0
}

export const AUDIT_THEME_DOC: ThemeDoc = {
  schema_version: '2.0.0',
  id: 'cyber_forest',
  name: 'Cyber Forest',
  author: 'Silt Team',
  description: 'High-contrast graphite surfaces with electric cyan accents.',
  modes: {
    dark: {
      surfaces: {
        app: { bg: '#0d1117', border: '#262e3b', text: '#e6edf3' },
        sidebar: { bg: '#11161d', border: '#262e3b', text: '#e6edf3' },
        editor: { bg: '#0d1117', border: '#262e3b', text: '#e6edf3' },
        panel: { bg: '#161b22', border: '#262e3b', text: '#e6edf3' },
        card: { bg: '#181e27', border: '#262e3b', text: '#e6edf3' },
        modal: { bg: '#1c232d', border: '#262e3b', text: '#e6edf3' }
      },
      hover: '#212834',
      active: '#283140',
      border_active: '#38bdf8',
      border_focus: '#38bdf8',
      text_muted: '#8b949e',
      text_disabled: '#484f58',
      accent: {
        primary: {
          start: '#38bdf8',
          end: '#0284c7',
          glow: 'rgba(56, 189, 248, 0.16)'
        },
        secondary: {
          start: '#6366f1',
          end: '#a855f7',
          glow: 'rgba(99, 102, 241, 0.14)'
        }
      },
      status: { warn: '#e0b04a', danger: '#dd5a72', success: '#37b594' },
      error: { fg: '#e8728a', bg: '#171015', border: '#4a2a38' },
      radius: {
        sm: '4px',
        md: '8px',
        lg: '12px',
        xl: '16px',
        full: '9999px'
      }
    },
    light: {
      surfaces: {
        app: { bg: '#f8fafc', border: '#e2e8f0', text: '#0f172a' },
        sidebar: { bg: '#f1f5f9', border: '#e2e8f0', text: '#0f172a' },
        editor: { bg: '#ffffff', border: '#e2e8f0', text: '#0f172a' },
        panel: { bg: '#ffffff', border: '#e2e8f0', text: '#0f172a' }
      },
      hover: '#e2e8f0',
      active: '#cbd5e1',
      border_active: '#0284c7',
      border_focus: '#0284c7',
      text_muted: '#64748b',
      text_disabled: '#94a3b8',
      accent: {
        primary: {
          start: '#0284c7',
          end: '#0369a1',
          glow: 'rgba(2, 132, 199, 0.12)'
        },
        secondary: {
          start: '#4f46e5',
          end: '#7c3aed',
          glow: 'rgba(79, 70, 229, 0.1)'
        }
      },
      status: { warn: '#b45309', danger: '#be123c', success: '#0f766e' },
      error: { fg: '#be123c', bg: '#fff1f2', border: '#fecdd3' },
      radius: {
        sm: '4px',
        md: '8px',
        lg: '12px',
        xl: '16px',
        full: '9999px'
      }
    }
  },
  typography: {
    font_family: 'Inter, system-ui, sans-serif',
    mono_font_family: 'JetBrains Mono, monospace'
  }
}

const darkTokens = flattenTheme(AUDIT_THEME_DOC, 'dark')
const lightTokens = flattenTheme(AUDIT_THEME_DOC, 'light')

export const AUDIT_ACTIVE_THEME_RESULT = {
  id: 'cyber_forest',
  name: 'Cyber Forest',
  mode: 'dark',
  tokens: darkTokens,
  dark_tokens: darkTokens,
  light_tokens: lightTokens,
  bg_void: '#0d1117'
}

export const AUDIT_THEMES_LIST = [
  {
    id: 'cyber_forest',
    name: 'Cyber Forest',
    author: 'Silt Team',
    description: 'High-contrast graphite surfaces with electric cyan accents.',
    swatches: ['#38bdf8', '#11161d'],
    source: 'default'
  },
  {
    id: 'solar_paper',
    name: 'Solar Paper',
    author: 'Silt Team',
    description: 'Warm archival paper palette tuned for long-form reading.',
    swatches: ['#0284c7', '#f8fafc'],
    source: 'default'
  },
  {
    id: 'nordic_slate',
    name: 'Nordic Slate',
    author: 'Alex Rivera',
    description: 'Cool slate blues and muted frost highlights.',
    swatches: ['#38bdf8', '#11161d'],
    source: 'disk'
  }
]

export const AUDIT_THEMES_RESULT = {
  themes: AUDIT_THEMES_LIST,
  errors: [],
  flat_tokens: {
    cyber_forest: { dark: darkTokens, light: lightTokens },
    solar_paper: { dark: darkTokens, light: lightTokens },
    nordic_slate: { dark: darkTokens, light: lightTokens }
  }
}

export const AUDIT_THEME_JSON = JSON.stringify(AUDIT_THEME_DOC, null, 2)

export const AUDIT_TEMPLATES_LIST = [
  {
    schema_version: '1',
    id: 'architecture-rfc',
    title: 'Architecture RFC',
    description:
      'Structured technical proposal with context, trade-offs, and rollout plan.',
    category: 'Engineering',
    icon: 'architecture',
    source: 'builtin',
    plugin_id: '',
    placeholders: [
      {
        name: 'feature_name',
        description: 'Feature Name',
        default: 'Shared UI Primitives',
        required: true
      },
      {
        name: 'owner',
        description: 'Author',
        default: 'Alex Rivera',
        required: false
      }
    ],
    body: '# RFC: {{feature_name}}\n\n**Author:** {{owner}}  \n**Date:** {{date}}\n\n## Summary\n\nDescribe the core problem and proposed solution.\n\n## Trade-offs\n\n- Option A\n- Option B\n'
  },
  {
    schema_version: '1',
    id: 'weekly-sprint-review',
    title: 'Weekly Sprint Review',
    description:
      'Highlights, blockers, and action items for weekly team syncs.',
    category: 'Operations',
    icon: 'event_note',
    source: 'disk',
    plugin_id: '',
    placeholders: [
      {
        name: 'sprint_label',
        description: 'Sprint Label',
        default: 'Sprint 18',
        required: true
      }
    ],
    body: '# {{sprint_label}} Review\n\n## Shipped\n\n- \n\n## Action Items\n\n- [ ] Follow up on open review items\n'
  }
]

export const AUDIT_AI_PROVIDER_CONFIG = {
  chat: {
    provider_type: 'openai_compatible',
    base_url: 'https://api.openai.com/v1',
    model: 'gpt-4o-mini',
    has_key: true,
    temperature: 0.3,
    max_tokens: 4096,
    reasoning_effort: 'medium',
    timeout_ms: 30000
  },
  embedding: {
    provider_type: 'openai_compatible',
    base_url: 'https://api.openai.com/v1',
    model: 'text-embedding-3-small',
    has_key: true,
    timeout_ms: 30000,
    dimensions: 1536
  },
  features: {
    enabled: true,
    rag_enabled: true,
    summaries_enabled: true,
    agent_writes: 'confirm'
  },
  use_keyring: true,
  keyring_available: true,
  keyring_unusable_for: []
}

export const AUDIT_AI_MODELS = [
  'gpt-4o-mini',
  'gpt-4o',
  'text-embedding-3-small',
  'text-embedding-3-large'
]

export const AUDIT_AI_AUDIT_ENTRIES = [
  {
    plugin: 'silt-ai-qa',
    kind: 'chat',
    host: 'api.openai.com',
    model: 'gpt-4o-mini',
    status: 'ok',
    at: '2026-04-19T14:22:10Z',
    prompt_tokens: 612,
    completion_tokens: 184,
    total_tokens: 796
  },
  {
    plugin: 'silt-ai-summary',
    kind: 'embed',
    host: 'api.openai.com',
    model: 'text-embedding-3-small',
    status: 'ok',
    at: '2026-04-19T14:19:05Z',
    prompt_tokens: 320,
    completion_tokens: 0,
    total_tokens: 320
  }
]

export const AUDIT_MCP_STATUS = {
  state: 'running',
  message: 'Listening on 127.0.0.1:17887 (read-only)',
  endpoint: 'http://127.0.0.1:17887/mcp',
  write_enabled: false
}

export const AUDIT_MCP_INSTALL_HINT = JSON.stringify(
  {
    mcpServers: {
      silt: {
        url: 'http://127.0.0.1:17887/mcp',
        headers: {
          Authorization: 'Bearer <token>'
        }
      }
    }
  },
  null,
  2
)

export const AUDIT_MCP_AUDIT_ENTRIES = [
  {
    ts: '2026-04-19T14:25:00Z',
    client: 'claude-desktop',
    tool: 'search_notes',
    vault: 'SiltVault',
    outcome: 'ok',
    args: { query: 'design tokens' }
  },
  {
    ts: '2026-04-19T14:26:12Z',
    client: 'cursor-mcp',
    tool: 'read_page',
    vault: 'SiltVault',
    outcome: 'ok',
    args: { notebook: 'Product', page: 'Design System Roadmap' }
  }
]

export const AUDIT_UPDATE_SETTINGS = {
  autoCheck: true,
  lastCheck: '2026-04-19T14:30:00Z',
  shouldAutoCheck: false
}

export const AUDIT_SPELLCHECK_LANGUAGE_PACKS = [
  {
    locale: 'en-US',
    name: 'English (United States)',
    installed: true,
    builtin: true,
    word_count: 124500
  },
  {
    locale: 'en-GB',
    name: 'English (United Kingdom)',
    installed: false,
    builtin: false,
    word_count: 122800
  },
  {
    locale: 'de-DE',
    name: 'German (Germany)',
    installed: false,
    builtin: false,
    word_count: 165000
  }
]

export const AUDIT_SPELLCHECK_DOMAIN_PACKS = [
  {
    id: 'software',
    name: 'Software Engineering',
    description:
      'Frameworks, CLI tools, protocols, and programming terminology.',
    word_count: 3420,
    enabled: true
  },
  {
    id: 'markdown',
    name: 'Markdown & PKM',
    description:
      'Wiki-links, frontmatter, callouts, and knowledge management terms.',
    word_count: 680,
    enabled: true
  }
]

export const AUDIT_PLUGIN_SECURITY_STATS = [
  {
    pluginId: 'silt-tasks',
    denials: 0,
    rateLimited: 0
  },
  {
    pluginId: 'silt-ai-qa',
    denials: 0,
    rateLimited: 0
  }
]
