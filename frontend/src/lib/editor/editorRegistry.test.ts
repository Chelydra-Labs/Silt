import { describe, expect, it, afterEach, vi } from 'vitest'
import {
  editorKey,
  getEditorForLocator,
  registerEditor,
  _applyPageExternalReloadForTests,
  _resetEditorRegistryForTests
} from './editorRegistry.svelte'

afterEach(() => {
  _resetEditorRegistryForTests()
})

describe('page external reload', () => {
  it('arms forceExternalReload for the matching editor', () => {
    const forceExternalReload = vi.fn()
    registerEditor({
      key: editorKey('Work', 'Journal', 'Daily'),
      isDirty: () => false,
      flush: async () => true,
      forceExternalReload,
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    _applyPageExternalReloadForTests({
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily'
    })
    expect(forceExternalReload).toHaveBeenCalledTimes(1)
  })

  it('getEditorForLocator matches a case-variant locator', () => {
    const forceExternalReload = vi.fn()
    registerEditor({
      key: editorKey('Work', 'Journal', 'Daily'),
      isDirty: () => false,
      flush: async () => true,
      forceExternalReload,
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    expect(getEditorForLocator('work', 'journal', 'daily')).toBeTruthy()
  })
})
