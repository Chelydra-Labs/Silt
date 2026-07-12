// Proposal factory for Writing Assistant.

import type {
  ActionId,
  Proposal,
  ProposalKind,
  RelatedSuggestion,
  ScopeContext,
  TagSuggestion
} from '../types'

let seq = 0

export function newProposalId(): string {
  seq += 1
  return `wa-${Date.now()}-${seq}`
}

export function createProposal(opts: {
  actionId: ActionId
  kind: ProposalKind
  scope: ScopeContext
  proposedMarkdown?: string
  tasks?: string[]
  tags?: TagSuggestion[]
  related?: RelatedSuggestion[]
  warning?: string
  errorMessage?: string
  status?: Proposal['status']
}): Proposal {
  return {
    id: newProposalId(),
    actionId: opts.actionId,
    kind: opts.kind,
    status: opts.status ?? 'ready',
    scope: opts.scope,
    proposedMarkdown: opts.proposedMarkdown ?? '',
    tasks: opts.tasks,
    tags: opts.tags,
    related: opts.related,
    selectedTags: opts.tags?.map((t) => t.tag),
    selectedRelatedIds: opts.related?.map((r) => r.blockId),
    warning: opts.warning,
    errorMessage: opts.errorMessage,
    createdAt: Date.now()
  }
}
