export type ChatRole = 'user' | 'assistant' | 'system'

export interface EvidenceTarget {
  blockId: string
  notebook?: string
  section?: string
  page?: string
}

interface EntryBase {
  id: string
  role: ChatRole
  createdAt: number
}

export interface TextEntry extends EntryBase {
  kind: 'text'
  content: string
  streaming?: boolean
}

export interface EvidenceEntry extends EntryBase {
  kind: 'evidence'
  citationIndex: number
  target: EvidenceTarget
  title: string
  excerpt?: string
}

export interface ToolCallEntry extends EntryBase {
  kind: 'tool-call'
  toolCallId: string
  toolName: string
  args: Record<string, unknown>
}

export interface ToolResultEntry extends EntryBase {
  kind: 'tool-result'
  toolCallId: string
  toolName: string
  output: string
  truncated?: boolean
  error?: string
}

export interface ProposalEntry extends EntryBase {
  kind: 'proposal'
  title: string
  content: string
  description?: string
  state?: 'pending' | 'accepted' | 'discarded'
}

export interface ConfirmationEntry extends EntryBase {
  kind: 'confirmation'
  token: string
  operation: string
  summary: string
  details?: string
  affectedCount?: number
  /** Bulk/irreversible vs single reversible edit (default danger for legacy). */
  severity?: 'normal' | 'danger'
  state?: 'pending' | 'confirmed' | 'rejected' | 'failed'
}

/** Transcript status kinds (includes legacy aliases used by writing capability). */
export type ChatStatus =
  | 'thinking'
  | 'running'
  | 'running_tool'
  | 'reviewing'
  | 'waiting_confirmation'
  | 'applying'
  | 'done'
  | 'stopped'
  | 'iteration-limit'
  | 'error'

export interface StatusEntry extends EntryBase {
  kind: 'status'
  status: ChatStatus
  message: string
}

export type AIChatEntry =
  | TextEntry
  | EvidenceEntry
  | ToolCallEntry
  | ToolResultEntry
  | ProposalEntry
  | ConfirmationEntry
  | StatusEntry

type EntryInput<T extends EntryBase> = Omit<T, 'id' | 'createdAt' | 'kind'> & {
  id?: string
  createdAt?: number
}

let entrySequence = 0

function withIdentity<T extends EntryBase>(
  kind: T extends { kind: infer Kind } ? Kind : never,
  entry: EntryInput<T>
): T {
  entrySequence += 1
  return {
    ...entry,
    kind,
    id: entry.id ?? `ai-entry-${Date.now()}-${entrySequence}`,
    createdAt: entry.createdAt ?? Date.now()
  } as unknown as T
}

export const textEntry = (entry: EntryInput<TextEntry>): TextEntry =>
  withIdentity<TextEntry>('text', entry)

export const evidenceEntry = (
  entry: EntryInput<EvidenceEntry>
): EvidenceEntry => withIdentity<EvidenceEntry>('evidence', entry)

export const toolCallEntry = (
  entry: EntryInput<ToolCallEntry>
): ToolCallEntry => withIdentity<ToolCallEntry>('tool-call', entry)

export const toolResultEntry = (
  entry: EntryInput<ToolResultEntry>
): ToolResultEntry => withIdentity<ToolResultEntry>('tool-result', entry)

export const proposalEntry = (
  entry: EntryInput<ProposalEntry>
): ProposalEntry => withIdentity<ProposalEntry>('proposal', entry)

export const confirmationEntry = (
  entry: EntryInput<ConfirmationEntry>
): ConfirmationEntry => withIdentity<ConfirmationEntry>('confirmation', entry)

export const statusEntry = (entry: EntryInput<StatusEntry>): StatusEntry =>
  withIdentity<StatusEntry>('status', entry)
