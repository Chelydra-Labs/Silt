export interface RetainedTemplateDraft {
  vaultId: string
  draft: unknown
  baseline: string
  selectedId: string
}

let retained: RetainedTemplateDraft | null = null

export function getRetainedTemplateDraft(
  vaultId: string
): RetainedTemplateDraft | null {
  if (retained && retained.vaultId !== vaultId) retained = null
  return retained
}

export function retainTemplateDraft(
  vaultId: string,
  session: Omit<RetainedTemplateDraft, 'vaultId'> | null
): void {
  retained = session ? { ...session, vaultId } : null
}

export function clearRetainedTemplateDraft(): void {
  retained = null
}

export function hasUnsavedTemplateDraft(): boolean {
  return (
    !!retained?.draft && JSON.stringify(retained.draft) !== retained.baseline
  )
}

export function resetTemplateDraftForTests(): void {
  retained = null
}
