// Types for silt-ai-assistant (Writing Assistant) — Sprint 23 / #229–#233.

export type ActionId =
  | 'draft-expand'
  | 'rewrite-succinct'
  | 'improve-clarity'
  | 'extract-tasks'
  | 'suggest-tags'
  | 'suggest-related'

export type ProposalKind =
  | 'replace-selection'
  | 'insert-below'
  | 'insert-tasks'
  | 'apply-tags'
  | 'insert-links'

export type ProposalStatus =
  'streaming' | 'ready' | 'accepted' | 'discarded' | 'error'

export interface ActionMeta {
  id: ActionId
  label: string
  description: string
  slashLabel: string
  slashDescription: string
  icon: string
  /** Needs chat provider. */
  needsChat: boolean
  /** Needs embedding provider. */
  needsEmbed: boolean
  /** Free-text instruction field in sidebar (draft/expand). */
  acceptsInstruction: boolean
  /** Prefer selection text as primary input. */
  prefersSelection: boolean
}

export interface AssistantSettings {
  /** Per-action enable flags. Missing keys default to true. */
  actions_enabled: Record<ActionId, boolean>
  /** Tag suggestions must come from existing vault vocabulary. */
  existing_vocab_only: boolean
  max_tag_suggestions: number
  /** Max chars sent to the model (selection/note). */
  max_input_chars: number
  /** Optional system-prompt overrides keyed by action id. */
  prompt_overrides: Partial<Record<ActionId, string>>
  /** Max related-note candidates to rank. */
  related_candidate_limit: number
  /** Max related notes to propose. */
  max_related_suggestions: number
}

export interface ScopeContext {
  notebook: string
  section: string
  page: string
  blockId?: string
  /** Clean text without identity comments. */
  inputText: string
  /** True when input was truncated to max_input_chars. */
  truncated: boolean
  /** Original selection text if from slash/editor. */
  selectionText?: string
  /** TipTap from/to when available (for replace). */
  selectionFrom?: number
  selectionTo?: number
  /** Existing task titles on the page (for dedupe). */
  existingTaskTitles?: string[]
  /** Target block id for mutate when replacing a whole block. */
  targetBlockId?: string
  /** Target block clean text when replacing (selection splice base). */
  targetBlockText?: string
  /** True when the selection covers the entire target block. */
  replaceFullBlock?: boolean
}

export type PanelStatus =
  | 'idle'
  | 'running'
  | 'streaming'
  | 'ready'
  | 'error'
  | 'no-chat-provider'
  | 'no-embedding-provider'
  | 'no-input'
  | 'applied'
  | 'cancelled'

export interface TagSuggestion {
  tag: string
  /** From existing vocabulary. */
  existing: boolean
}

export interface RelatedSuggestion {
  blockId: string
  snippet: string
  notebook?: string
  section?: string
  page?: string
  score: number
}

export interface Proposal {
  id: string
  actionId: ActionId
  kind: ProposalKind
  status: ProposalStatus
  scope: ScopeContext
  /** Streamed or final markdown body (writing actions). */
  proposedMarkdown: string
  /** Structured items for tags / tasks / links. */
  tasks?: string[]
  tags?: TagSuggestion[]
  related?: RelatedSuggestion[]
  /** Selected subset for multi-item proposals (indices or ids). */
  selectedTags?: string[]
  selectedRelatedIds?: string[]
  errorMessage?: string
  /** User-visible warning (e.g. truncated input). */
  warning?: string
  createdAt: number
}
