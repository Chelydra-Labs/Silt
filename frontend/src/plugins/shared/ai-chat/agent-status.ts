// Structured agent activity status for the shared chat shell (#629).

export type AgentActivityStatus =
  | 'idle'
  | 'thinking'
  | 'running_tool'
  | 'reviewing'
  | 'waiting_confirmation'
  | 'applying'
  | 'done'
  | 'error'

/** Friendly labels for tool names shown in the status region. */
export const TOOL_STATUS_LABELS: Record<string, string> = {
  search_notes: 'Searching notes',
  read_blocks: 'Reading notes',
  get_backlinks: 'Finding backlinks',
  query_tasks: 'Querying tasks',
  create_note: 'Creating a note',
  get_related_notes: 'Finding related notes',
  update_block: 'Updating a block',
  list_tags: 'Listing tags',
  find_untagged: 'Finding untagged tasks',
  rename_tag: 'Renaming a tag',
  get_vault_statistics: 'Gathering vault stats',
  suggest_link_targets: 'Suggesting link targets',
  extract_and_save: 'Extracting content'
}

export function toolStatusLabel(toolName: string): string {
  return (
    TOOL_STATUS_LABELS[toolName] ?? `Running ${toolName.replace(/_/g, ' ')}`
  )
}

export function agentStatusMessage(
  status: AgentActivityStatus,
  toolName?: string
): string {
  switch (status) {
    case 'idle':
      return ''
    case 'thinking':
      return 'Thinking…'
    case 'running_tool':
      return toolName ? `${toolStatusLabel(toolName)}…` : 'Using a tool…'
    case 'reviewing':
      return 'Reviewing results…'
    case 'waiting_confirmation':
      return 'Waiting for your confirmation…'
    case 'applying':
      return 'Applying changes…'
    case 'done':
      return 'Done'
    case 'error':
      return 'Something went wrong'
  }
}

/** Non-terminal statuses that the rolling activity line may promote through. */
const LIVE_AGENT_STATUSES = new Set<AgentActivityStatus>([
  'thinking',
  'running_tool',
  'reviewing',
  'waiting_confirmation',
  'applying'
])

export function isLiveAgentStatus(status: string): boolean {
  return LIVE_AGENT_STATUSES.has(status as AgentActivityStatus)
}
