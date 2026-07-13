// User-facing Accept labels for proposals.

import type { Proposal } from '../types'

export function acceptLabel(proposal: Proposal): string {
  switch (proposal.kind) {
    case 'replace-selection':
      if (
        proposal.scope.selectionText &&
        proposal.scope.targetBlockText &&
        !proposal.scope.replaceFullBlock
      ) {
        return 'Replace selection in block'
      }
      return proposal.scope.targetBlockId ? 'Replace block' : 'Insert into note'
    case 'insert-below':
      return 'Insert into note'
    case 'insert-tasks': {
      const n = proposal.tasks?.length ?? 0
      return n === 1 ? 'Insert 1 task' : `Insert ${n} tasks`
    }
    case 'apply-tags': {
      const n = proposal.selectedTags?.length ?? proposal.tags?.length ?? 0
      return n === 1 ? 'Apply 1 tag' : `Apply ${n} tags`
    }
    case 'insert-links': {
      const n =
        proposal.selectedRelatedIds?.length ?? proposal.related?.length ?? 0
      return n === 1 ? 'Insert 1 link' : `Insert ${n} links`
    }
    default:
      return 'Accept'
  }
}

export function outcomeSummary(proposal: Proposal): string {
  switch (proposal.kind) {
    case 'replace-selection':
      return proposal.scope.targetBlockId
        ? 'Will update the target block in your note.'
        : 'Will insert the text as a new block.'
    case 'insert-below':
      return 'Will insert the text as a new block on this page.'
    case 'insert-tasks':
      return 'Will create new task blocks (existing matching tasks are skipped).'
    case 'apply-tags':
      return proposal.scope.targetBlockId
        ? 'Will merge selected tags onto the target task (existing tags kept).'
        : 'Will insert selected tags as a new line.'
    case 'insert-links':
      return 'Will insert a Related list with block links.'
    default:
      return ''
  }
}
