// Deterministic fixture data for the dev-only browser visual-audit harness.
// Covers shell bootstrap, editor blocks/properties, tags, backlinks, search,
// type dashboards, tasks hub, and all 9 settings tabs so every view and modal
// renders populated states in a standalone browser without the Go backend.

export const AUDIT_APP_VERSION = 'v0.18.0-audit'
export const AUDIT_LOCAL_AUTHOR = 'Alex Rivera'
export const AUDIT_SIDEBAR_WIDTH = 264

export const AUDIT_SYSTEM_CONFIG = {
  schema_version: '1',
  vault_id: 'vault-audit-001',
  vault: {
    default_path: '/Users/alex/Documents/SiltVault',
    linked_notebooks: [
      {
        id: 'linked-eng',
        name: 'Team Handbook',
        path: '/Users/alex/Shared/TeamHandbook',
        read_only: false
      }
    ]
  },
  appearance: {
    active_theme: 'cyber-ink',
    mode: 'dark'
  },
  ui: {
    sidebar_width_px: AUDIT_SIDEBAR_WIDTH,
    active_notebook: 'Product',
    open_devtools_on_startup: true,
    close_to_tray: false,
    dismissed_tips: [],
    formatting: {
      date_format: 'YYYY-MM-DD',
      time_format: '24h'
    },
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
  },
  editor: {
    font_size_px: 15,
    font_family: 'Inter',
    mono_font_family: 'JetBrains Mono',
    tab_indent_spaces: 2,
    line_height: 1.65,
    auto_save_delay_ms: 800,
    max_versions_per_page: 50,
    vim_mode: false,
    smart_quotes: false,
    show_line_numbers: true,
    spellcheck: {
      enabled: true,
      locale: 'en-US',
      domain_packs: ['software', 'markdown']
    }
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
  ai: {
    provider: 'openai_compatible',
    model: 'gpt-4o-mini',
    base_url: 'https://api.openai.com/v1',
    embedding_provider: 'openai_compatible',
    embedding_model: 'text-embedding-3-small',
    embedding_base_url: 'https://api.openai.com/v1',
    temperature: 0.3,
    max_tokens: 4096,
    timeout_seconds: 30,
    local_mcp: {
      enabled: true,
      write: false,
      http: true,
      port: 17887
    }
  },
  plugins: {
    enabled: [
      'silt-tasks',
      'silt-ai-qa',
      'silt-ai-assistant',
      'silt-ai-summary'
    ],
    disabled: [],
    granted_capabilities: {},
    plugin_settings: {
      'silt-tasks': {
        default_display_mode: 'list',
        week_start: 'monday',
        show_completed: true
      }
    }
  },
  updates: {
    auto_check: true,
    last_checked: '2026-04-19T14:30:00Z',
    skipped_version: ''
  }
}

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

export const AUDIT_PAGE_BLOCKS = [
  {
    id: 'blk-heading-1',
    type: 'heading',
    raw_content: '# Design System Roadmap ^blk-heading-1',
    clean_content: '# Design System Roadmap',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    tags: ['design', 'ui/tokens'],
    links: []
  },
  {
    id: 'blk-note-1',
    type: 'note',
    raw_content:
      'Consolidating shared chrome primitives across all views to eliminate ad-hoc borders and spacing drift. Related to [[IPC Transport Specification]] and #ui/tokens. ^blk-note-1',
    clean_content:
      'Consolidating shared chrome primitives across all views to eliminate ad-hoc borders and spacing drift. Related to [[IPC Transport Specification]] and #ui/tokens.',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    tags: ['ui/tokens'],
    links: ['IPC Transport Specification']
  },
  {
    id: 'blk-callout-1',
    type: 'callout',
    raw_content:
      '> [!IMPORTANT] Visual Audit Standard\n> All primary views must use shared `PageHeader`, `EmptyState`, and `ModalShell` primitives with semantic Cyber-Ink tokens. ^blk-callout-1',
    clean_content:
      '> [!IMPORTANT] Visual Audit Standard\n> All primary views must use shared `PageHeader`, `EmptyState`, and `ModalShell` primitives with semantic Cyber-Ink tokens.',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    tags: ['design'],
    links: []
  },
  {
    id: 'blk-task-1',
    type: 'task',
    raw_content:
      '- [ ] Extract `PageHeader` and `ModalShell` components @alex 📅 2026-04-25 ⏫ #ui/tokens ^blk-task-1',
    clean_content: 'Extract `PageHeader` and `ModalShell` components',
    status: 'todo',
    owner: 'alex',
    due_date: '2026-04-25',
    priority: 1,
    tags: ['ui/tokens'],
    links: []
  },
  {
    id: 'blk-task-2',
    type: 'task',
    raw_content:
      '- [/] Audit keyboard focus rings in dialogs and menus @sam 📅 2026-04-22 🔼 #a11y ^blk-task-2',
    clean_content: 'Audit keyboard focus rings in dialogs and menus',
    status: 'in-progress',
    owner: 'sam',
    due_date: '2026-04-22',
    priority: 2,
    tags: ['a11y'],
    links: []
  },
  {
    id: 'blk-code-1',
    type: 'code',
    raw_content:
      '```ts\nexport interface PageHeaderProps {\n  title: string\n  subtitle?: string\n  icon?: string\n}\n``` ^blk-code-1',
    clean_content:
      '```ts\nexport interface PageHeaderProps {\n  title: string\n  subtitle?: string\n  icon?: string\n}\n```',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    tags: [],
    links: []
  },
  {
    id: 'blk-table-1',
    type: 'table',
    raw_content:
      '| Surface | Primitive | Status |\n| --- | --- | --- |\n| TagsExplorer | PageHeader | Planned |\n| TypeDashboard | EmptyState | In Review |\n| SearchModal | ModalShell | Planned | ^blk-table-1',
    clean_content:
      '| Surface | Primitive | Status |\n| --- | --- | --- |\n| TagsExplorer | PageHeader | Planned |\n| TypeDashboard | EmptyState | In Review |\n| SearchModal | ModalShell | Planned |',
    status: '',
    owner: '',
    due_date: '',
    priority: 0,
    tags: [],
    links: []
  }
]

export const AUDIT_PAGE_MARKDOWN = AUDIT_PAGE_BLOCKS.map(
  (b) => b.raw_content
).join('\n\n')

export const AUDIT_PAGE_PROPERTIES = {
  type: 'project',
  properties: {
    status: 'In Progress',
    lead: 'Alex Rivera',
    target_release: 'v0.19.0',
    priority: 'High'
  },
  extra: {
    repository: 'github.com/silt-app/silt'
  }
}

export const AUDIT_TYPES = [
  {
    name: 'project',
    title: 'Project',
    description: 'Cross-functional initiatives and engineering specs.',
    icon: 'rocket_launch',
    color: 'accent',
    version: 1,
    example: false,
    built_in: false,
    default_view: 'board',
    group_by: 'status',
    sort_by: 'target_release',
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
    name: 'meeting',
    title: 'Meeting Note',
    description: 'Recurring syncs, design reviews, and retrospectives.',
    icon: 'groups',
    color: 'info',
    version: 1,
    example: true,
    built_in: false,
    default_view: 'table',
    group_by: 'cadence',
    sort_by: 'meeting_date',
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

export const AUDIT_TYPE_PAGES = [
  {
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    title: 'Design System Roadmap',
    type_name: 'project',
    type_version: 1,
    file_date: '2026-04-18',
    modified_at: '2026-04-19T12:15:00Z',
    properties: {
      status: 'In Progress',
      lead: 'Alex Rivera',
      target_release: 'v0.19.0',
      priority: 'High'
    },
    extra: {},
    diagnostics: []
  },
  {
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'IPC Transport Specification',
    title: 'IPC Transport Specification',
    type_name: 'project',
    type_version: 1,
    file_date: '2026-04-14',
    modified_at: '2026-04-17T09:40:00Z',
    properties: {
      status: 'Review',
      lead: 'Sam Chen',
      target_release: 'v0.19.0',
      priority: 'Critical'
    },
    extra: {},
    diagnostics: []
  },
  {
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Token Hierarchy & Scales',
    title: 'Token Hierarchy & Scales',
    type_name: 'project',
    type_version: 1,
    file_date: '2026-04-10',
    modified_at: '2026-04-16T17:20:00Z',
    properties: {
      status: 'Backlog',
      lead: 'Morgan Vance',
      target_release: 'v0.20.0',
      priority: 'Medium'
    },
    extra: {},
    diagnostics: [
      {
        property: 'target_release',
        code: 'unverified_milestone',
        message: 'Milestone v0.20.0 is not yet scheduled in release calendar.'
      }
    ]
  },
  {
    source: 'vault',
    notebook: 'Operations',
    section: 'Sprints',
    page: 'Q2 Launch Checklist',
    title: 'Q2 Launch Checklist',
    type_name: 'project',
    type_version: 1,
    file_date: '2026-04-02',
    modified_at: '2026-04-15T11:00:00Z',
    properties: {
      status: 'Shipped',
      lead: 'Alex Rivera',
      target_release: 'v0.18.0',
      priority: 'High'
    },
    extra: {},
    diagnostics: []
  }
]

export const AUDIT_TYPES_REPROJECTION_STATUS = {
  running: false,
  queued: 0,
  processed: 4,
  total: 4,
  last_error: ''
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
    clean_content: 'Extract PageHeader and ModalShell components',
    raw_content:
      '- [ ] Extract PageHeader and ModalShell components @alex 📅 2026-04-25 ⏫ #ui/tokens ^blk-task-1',
    file_date: '2026-04-18',
    parent_block_id: null,
    sort_order: 1000,
    created_at: 1776400000,
    modified_at: 1776500000,
    status: 'todo',
    owner: 'alex',
    due_date: '2026-04-25',
    start_date: '2026-04-19',
    completed_at: null,
    priority: 1,
    recurrence: null,
    estimate_minutes: 90,
    snooze_until: null,
    blocked_by: '[]',
    tags_csv: 'ui/tokens,design',
    subtask_total: 2,
    subtask_done: 1,
    comment_count: 1
  },
  {
    id: 'blk-task-2',
    source: 'vault',
    notebook: 'Product',
    section: 'Architecture',
    page: 'Design System Roadmap',
    clean_content: 'Audit keyboard focus rings in dialogs and menus',
    raw_content:
      '- [/] Audit keyboard focus rings in dialogs and menus @sam 📅 2026-04-22 🔼 #a11y ^blk-task-2',
    file_date: '2026-04-18',
    parent_block_id: null,
    sort_order: 2000,
    created_at: 1776410000,
    modified_at: 1776510000,
    status: 'in-progress',
    owner: 'sam',
    due_date: '2026-04-22',
    start_date: '2026-04-18',
    completed_at: null,
    priority: 2,
    recurrence: null,
    estimate_minutes: 60,
    snooze_until: null,
    blocked_by: '[]',
    tags_csv: 'a11y',
    subtask_total: 0,
    subtask_done: 0,
    comment_count: 0
  },
  {
    id: 'blk-task-3',
    source: 'vault',
    notebook: 'Operations',
    section: 'Sprints',
    page: 'Q2 Launch Checklist',
    clean_content: 'Publish baseline UI audit matrix and token inventory',
    raw_content:
      '- [x] Publish baseline UI audit matrix and token inventory @alex 📅 2026-04-19 ✅ 2026-04-19 #release/q2 ^blk-task-3',
    file_date: '2026-04-15',
    parent_block_id: null,
    sort_order: 3000,
    created_at: 1776300000,
    modified_at: 1776520000,
    status: 'done',
    owner: 'alex',
    due_date: '2026-04-19',
    start_date: '2026-04-17',
    completed_at: '2026-04-19',
    priority: 2,
    recurrence: null,
    estimate_minutes: 45,
    snooze_until: null,
    blocked_by: '[]',
    tags_csv: 'release/q2',
    subtask_total: 0,
    subtask_done: 0,
    comment_count: 2
  }
]

export const AUDIT_TASK_UNDO_STATE = {
  available: true,
  label: 'Status changed to In Progress'
}

export const AUDIT_THEMES_LIST = [
  {
    id: 'cyber-ink',
    name: 'Cyber Ink',
    author: 'Silt Team',
    version: '1.0.0',
    description: 'High-contrast graphite surfaces with electric cyan accents.',
    builtin: true,
    modes: ['dark', 'light']
  },
  {
    id: 'solar-paper',
    name: 'Solar Paper',
    author: 'Silt Team',
    version: '1.0.0',
    description: 'Warm archival paper palette tuned for long-form reading.',
    builtin: true,
    modes: ['light', 'dark']
  },
  {
    id: 'nordic-slate',
    name: 'Nordic Slate',
    author: 'Alex Rivera',
    version: '1.2.0',
    description: 'Cool slate blues and muted frost highlights.',
    builtin: false,
    modes: ['dark']
  }
]

export const AUDIT_THEME_JSON = JSON.stringify({
  schema_version: '1',
  id: 'cyber-ink',
  name: 'Cyber Ink',
  author: 'Silt Team',
  version: '1.0.0',
  description: 'High-contrast graphite surfaces with electric cyan accents.',
  modes: {
    dark: {
      '--surface-app': '#0d1117',
      '--surface-sidebar': '#11161d',
      '--surface-panel': '#161b22',
      '--surface-panel-border': '#262e3b',
      '--surface-elevated': '#1c232d',
      '--surface-card': '#181e27',
      '--surface-hover': '#212834',
      '--text-primary': '#e6edf3',
      '--text-secondary': '#9da7b3',
      '--text-muted': '#7d8590',
      '--accent-primary': '#38bdf8',
      '--font-body': 'Inter, system-ui, sans-serif',
      '--font-mono': 'JetBrains Mono, monospace'
    },
    light: {
      '--surface-app': '#f8fafc',
      '--surface-sidebar': '#f1f5f9',
      '--surface-panel': '#ffffff',
      '--surface-panel-border': '#e2e8f0',
      '--surface-elevated': '#ffffff',
      '--surface-card': '#f8fafc',
      '--surface-hover': '#e2e8f0',
      '--text-primary': '#0f172a',
      '--text-secondary': '#334155',
      '--text-muted': '#64748b',
      '--accent-primary': '#0284c7',
      '--font-body': 'Inter, system-ui, sans-serif',
      '--font-mono': 'JetBrains Mono, monospace'
    }
  }
})

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
        label: 'Feature Name',
        default: 'Shared UI Primitives',
        required: true
      },
      {
        name: 'owner',
        label: 'Author',
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
        label: 'Sprint Label',
        default: 'Sprint 18',
        required: true
      }
    ],
    body: '# {{sprint_label}} Review\n\n## Shipped\n\n- \n\n## Action Items\n\n- [ ] Follow up on open review items\n'
  }
]

export const AUDIT_AI_PROVIDER_CONFIG = {
  provider: 'openai_compatible',
  model: 'gpt-4o-mini',
  base_url: 'https://api.openai.com/v1',
  has_api_key: true,
  key_source: 'keyring',
  use_keyring: true,
  keyring_available: true,
  embedding_provider: 'openai_compatible',
  embedding_model: 'text-embedding-3-small',
  embedding_base_url: 'https://api.openai.com/v1',
  has_embedding_api_key: true,
  embedding_key_source: 'keyring',
  temperature: 0.3,
  max_tokens: 4096,
  timeout_seconds: 30,
  reasoning_effort: 'medium',
  features: {
    enabled: true,
    rag_enabled: true,
    summaries_enabled: true,
    agent_writes: 'confirm'
  }
}

export const AUDIT_AI_MODELS = [
  'gpt-4o-mini',
  'gpt-4o',
  'text-embedding-3-small',
  'text-embedding-3-large'
]

export const AUDIT_AI_AUDIT_ENTRIES = [
  {
    timestamp: '2026-04-19T14:22:10Z',
    plugin_id: 'silt-ai-qa',
    operation: 'chat',
    provider: 'openai_compatible',
    model: 'gpt-4o-mini',
    prompt_tokens: 612,
    completion_tokens: 184,
    duration_ms: 840,
    status: 'ok'
  },
  {
    timestamp: '2026-04-19T14:19:05Z',
    plugin_id: 'silt-ai-summary',
    operation: 'embed',
    provider: 'openai_compatible',
    model: 'text-embedding-3-small',
    prompt_tokens: 320,
    completion_tokens: 0,
    duration_ms: 210,
    status: 'ok'
  }
]

export const AUDIT_MCP_CONFIG = {
  enabled: true,
  write: false,
  http: true,
  port: 17887
}

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
  auto_check: true,
  last_checked: '2026-04-19T14:30:00Z',
  skipped_version: ''
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

export const AUDIT_CUSTOM_DICTIONARY = ['Silt', 'Wails', 'TipTap', 'Cyber-Ink']

export const AUDIT_PLUGIN_SECURITY_STATS = [
  {
    pluginId: 'silt-tasks',
    allowedCalls: 42,
    deniedCalls: 0,
    rateLimitedCalls: 0,
    lastActive: '2026-04-19T14:31:00Z'
  },
  {
    pluginId: 'silt-ai-qa',
    allowedCalls: 15,
    deniedCalls: 0,
    rateLimitedCalls: 0,
    lastActive: '2026-04-19T14:22:10Z'
  }
]
