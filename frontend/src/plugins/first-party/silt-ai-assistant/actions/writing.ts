// Core writing actions: draft/expand, rewrite succinct, improve clarity (#231).

import type { PluginAIChatMessage, PluginContext } from '../../../sdk'
import { systemPromptFor } from '../prompts/defaults'
import { createProposal } from '../proposal/model'
import { stripModelPreamble } from '../text'
import type {
  ActionId,
  AssistantSettings,
  Proposal,
  ProposalKind,
  ScopeContext
} from '../types'
import { completeStreaming, type StreamSession } from './runChat'

export function buildWritingMessages(
  actionId: ActionId,
  input: string,
  settings: AssistantSettings,
  instruction?: string
): PluginAIChatMessage[] {
  const system = systemPromptFor(actionId, settings.prompt_overrides[actionId])
  let user = input
  if (actionId === 'draft-expand' && instruction?.trim()) {
    user = instruction.trim()
    if (input.trim()) {
      user += `\n\nContext from the note:\n${input}`
    }
  }
  return [
    { role: 'system', content: system },
    { role: 'user', content: user }
  ]
}

export function parseWritingOutput(raw: string): string {
  return stripModelPreamble(raw)
}

/** Prefer replace only when we resolved a target block; otherwise insert. */
export function writingProposalKind(
  actionId: 'draft-expand' | 'rewrite-succinct' | 'improve-clarity',
  scope: ScopeContext,
  instruction?: string
): ProposalKind {
  if (
    actionId === 'draft-expand' &&
    instruction?.trim() &&
    !scope.selectionText
  ) {
    return 'insert-below'
  }
  if (scope.targetBlockId) return 'replace-selection'
  return 'insert-below'
}

export async function runWritingAction(
  ctx: PluginContext,
  actionId: 'draft-expand' | 'rewrite-succinct' | 'improve-clarity',
  scope: ScopeContext,
  settings: AssistantSettings,
  opts: {
    instruction?: string
    onStream?: (full: string) => void
    onSession?: (session: StreamSession) => void
    isCancelled?: () => boolean
  } = {}
): Promise<Proposal> {
  const messages = buildWritingMessages(
    actionId,
    scope.inputText,
    settings,
    opts.instruction
  )
  const kind = writingProposalKind(actionId, scope, opts.instruction)

  const proposal = createProposal({
    actionId,
    kind,
    scope,
    proposedMarkdown: '',
    status: 'streaming',
    warning: scope.truncated
      ? `Input truncated to ${settings.max_input_chars} characters for the model context window.`
      : undefined
  })

  const { content } = await completeStreaming(
    ctx,
    messages,
    (_d, full) => {
      opts.onStream?.(full)
    },
    {
      onSession: opts.onSession,
      isCancelled: opts.isCancelled
    }
  )

  proposal.proposedMarkdown = parseWritingOutput(content)
  proposal.status = proposal.proposedMarkdown ? 'ready' : 'error'
  if (!proposal.proposedMarkdown) {
    proposal.errorMessage = 'Model returned empty output'
  }
  return proposal
}
