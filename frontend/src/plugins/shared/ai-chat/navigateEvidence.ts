import type { EvidenceTarget } from './types'

/** Detail payload for window `navigate-to-block` from an AI evidence target. */
export function evidenceNavigateDetail(target: EvidenceTarget) {
  return {
    notebook: target.notebook,
    section: target.section,
    page: target.page,
    blockId: target.blockId
  }
}

/** Open the cited note/block via the shell navigate-to-block bus. */
export function dispatchNavigateEvidence(target: EvidenceTarget): void {
  window.dispatchEvent(
    new CustomEvent('navigate-to-block', {
      detail: evidenceNavigateDetail(target)
    })
  )
}
