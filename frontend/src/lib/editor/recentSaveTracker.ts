export interface EditorSaveState {
  phase: 'idle' | 'pending' | 'saving' | 'saved' | 'error'
  dirty: boolean
}

export function createRecentSaveTracker(onConfirmedSave: (id: string) => void) {
  const pending = new Set<string>()
  return (id: string, state: EditorSaveState): void => {
    if (state.dirty) pending.add(id)
    if (state.phase === 'error') pending.delete(id)
    if (state.phase === 'saved' && pending.delete(id)) onConfirmedSave(id)
  }
}
