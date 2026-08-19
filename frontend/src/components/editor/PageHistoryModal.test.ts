import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'
import {
  registerEditor,
  _resetEditorRegistryForTests
} from '../../lib/editor/editorRegistry.svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListPageVersions: vi.fn(),
    GetPageVersion: vi.fn(),
    RestorePageVersion: vi.fn(),
    RestoreDeletedPageVersion: vi.fn(),
    FetchPageMarkdown: vi.fn()
  })
)

vi.mock('$silt-app', () => appMocks)

const eventsMock = vi.hoisted(() => {
  const handlers = new Map<string, (event: unknown) => void>()
  return {
    handlers,
    On: vi.fn((name: string, cb: (event: unknown) => void) => {
      handlers.set(name, cb)
      return () => handlers.delete(name)
    })
  }
})

vi.mock('@wailsio/runtime', () => ({
  Events: { On: eventsMock.On }
}))

const settingsMock = vi.hoisted(() => ({
  config: {
    editor: { auto_versioning_enabled: true }
  }
}))

vi.mock('../../settings/store.svelte', () => ({
  settings: settingsMock
}))

import PageHistoryModal from './PageHistoryModal.svelte'
import { OPEN_DELETED_PAGE_HISTORY_EVENT } from './openDeletedPageHistory'
import { COMPARE_MAX_CHARS } from '../../lib/editor/pageDiff'

const VERSIONS = [
  {
    id: 'v-new',
    timestamp: '2026-08-16T18:00:00Z',
    source: 'editor',
    bytes: 120
  },
  {
    id: 'v-old',
    timestamp: '2026-08-15T09:30:00Z',
    source: 'source',
    bytes: 96
  }
]

function renderModal(onClose = vi.fn()) {
  return render(PageHistoryModal, {
    props: {
      notebook: 'Work',
      section: 'Journal',
      page: 'Daily',
      onClose
    }
  })
}

beforeEach(() => {
  settingsMock.config.editor.auto_versioning_enabled = true
  appMocks.ListPageVersions.mockReset().mockResolvedValue(VERSIONS)
  appMocks.GetPageVersion.mockReset().mockImplementation(
    async (_nb: string, _sec: string, _pg: string, id: string) =>
      id === 'v-old' ? '# older body' : '# newest body'
  )
  appMocks.RestorePageVersion.mockReset().mockResolvedValue(undefined)
  appMocks.RestoreDeletedPageVersion.mockReset().mockResolvedValue(undefined)
  appMocks.FetchPageMarkdown.mockReset().mockResolvedValue('# live body')
  _resetEditorRegistryForTests()
  eventsMock.handlers.clear()
  eventsMock.On.mockClear()
})

afterEach(() => {
  cleanup()
  _resetEditorRegistryForTests()
})

describe('PageHistoryModal', () => {
  it('renders the version list and previews without restoring', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Editor/i })).toBeTruthy()
    })
    expect(screen.getByRole('option', { name: /Source/i })).toBeTruthy()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# newest body'
      )
    })
    expect(appMocks.GetPageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-new'
    )
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('loads a preview when another version is selected', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Source/i })).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('option', { name: /Source/i }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# older body'
      )
    })
    expect(appMocks.GetPageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-old'
    )
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('confirming Restore calls RestorePageVersion', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    const confirm = await screen.findByTestId('page-history-confirm-confirm')
    expect(document.activeElement).not.toBe(confirm)
    await fireEvent.click(confirm)
    await waitFor(() => {
      expect(appMocks.RestorePageVersion).toHaveBeenCalledWith(
        'Work',
        'Journal',
        'Daily',
        'v-new'
      )
    })
  })

  it('arms forceExternalReload before restore so a dirty buffer cannot overwrite', async () => {
    const order: string[] = []
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => true,
      flush: async () => {
        order.push('flush')
        return true
      },
      forceExternalReload: () => {
        order.push('forceExternalReload')
      },
      clearExternalReload: () => {
        order.push('clearExternalReload')
      },
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    appMocks.RestorePageVersion.mockImplementation(async () => {
      order.push('RestorePageVersion')
    })
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(order).toEqual([
        'flush',
        'forceExternalReload',
        'RestorePageVersion'
      ])
    })
  })

  it('closes on Escape', async () => {
    const onClose = vi.fn()
    renderModal(onClose)
    await waitFor(() => {
      expect(screen.getByTestId('page-history-modal')).toBeTruthy()
    })
    await fireEvent.keyDown(window, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('ignores a stale preview response after another version is selected', async () => {
    let releaseStale: ((body: string) => void) | undefined
    appMocks.GetPageVersion.mockImplementation(
      (_nb: string, _sec: string, _pg: string, id: string) => {
        if (id === 'v-new') {
          return new Promise<string>((resolve) => {
            releaseStale = resolve
          })
        }
        return Promise.resolve('# older body')
      }
    )
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Source/i })).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('option', { name: /Source/i }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# older body'
      )
    })
    releaseStale?.('# newest body')
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# older body'
      )
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(appMocks.RestorePageVersion).toHaveBeenCalledWith(
        'Work',
        'Journal',
        'Daily',
        'v-old'
      )
    })
  })

  it('does not restore when the dirty buffer cannot be flushed', async () => {
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => true,
      flush: async () => false,
      forceExternalReload: () => {},
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(
        screen.getByText(/Couldn't save the current page before restoring/)
      ).toBeTruthy()
    })
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('disarms forceExternalReload when restore fails', async () => {
    const order: string[] = []
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => false,
      flush: async () => true,
      forceExternalReload: () => {
        order.push('forceExternalReload')
      },
      clearExternalReload: () => {
        order.push('clearExternalReload')
      },
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    appMocks.RestorePageVersion.mockRejectedValue(new Error('disk full'))
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(order).toEqual(['forceExternalReload', 'clearExternalReload'])
    })
    expect(screen.getByText(/disk full/)).toBeTruthy()
  })

  it('lets keyboard users focus the preview pane', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toBeTruthy()
    })
    expect(screen.getByTestId('page-history-preview-body')).toHaveAttribute(
      'tabindex',
      '0'
    )
  })

  it('keeps Restore disabled until the selected version has a preview', async () => {
    appMocks.GetPageVersion.mockRejectedValue(new Error('preview failed'))
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
    })
    expect(screen.getByTestId('page-history-restore')).toBeDisabled()
    appMocks.GetPageVersion.mockResolvedValue('# newest body')
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# newest body'
      )
    })
    expect(screen.getByTestId('page-history-restore')).not.toBeDisabled()
  })

  it('states that snapshots are not a backup and shows 0 B', async () => {
    appMocks.ListPageVersions.mockResolvedValue([
      {
        id: 'v-empty',
        timestamp: '2026-08-16T18:00:00Z',
        source: 'editor',
        bytes: 0
      }
    ])
    appMocks.GetPageVersion.mockResolvedValue('')
    renderModal()
    await waitFor(() => {
      expect(screen.getByText('0 B')).toBeTruthy()
    })
    expect(
      screen.getByText(/are not encrypted, and are not a backup/)
    ).toBeTruthy()
  })

  it('shows an enable hint when history is off and empty', async () => {
    settingsMock.config.editor.auto_versioning_enabled = false
    appMocks.ListPageVersions.mockResolvedValue([])
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-empty')).toBeTruthy()
    })
    expect(screen.getByText('No versions yet')).toBeTruthy()
    expect(
      screen.getByText(/Turn on Capture page history in Settings → Editor/)
    ).toBeTruthy()
    expect(screen.getByTestId('page-history-empty-deleted')).toBeTruthy()
    expect(screen.queryByRole('listbox', { name: 'Versions' })).toBeNull()
    expect(
      screen.getByRole('button', { name: 'Open Editor settings' })
    ).toBeTruthy()
  })

  it('does not show the empty-history copy when the list fails', async () => {
    appMocks.ListPageVersions.mockRejectedValueOnce(
      new Error(
        JSON.stringify({
          code: 'navigation_unavailable',
          message: 'snapshot store read failed'
        })
      )
    )
    renderModal()
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        /snapshot store read failed/
      )
    })
    expect(screen.queryByTestId('page-history-empty')).toBeNull()
    expect(screen.queryByText('No versions yet')).toBeNull()
  })

  it('treats a failed live-page fetch as a compare error', async () => {
    appMocks.FetchPageMarkdown.mockRejectedValueOnce(
      new Error('Could not read the current page.')
    )
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-pane-compare')).not.toBeDisabled()
    })
    await fireEvent.click(screen.getByTestId('page-history-pane-compare'))
    await waitFor(() => {
      expect(
        screen.getByTestId('page-history-compare-error')
      ).toHaveTextContent(/Could not read the current page/)
    })
    expect(screen.queryByTestId('page-history-compare')).toBeNull()
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  async function openCompare() {
    await waitFor(() => {
      expect(screen.getByTestId('page-history-pane-compare')).not.toBeDisabled()
    })
    await fireEvent.click(screen.getByTestId('page-history-pane-compare'))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-compare')).toBeTruthy()
    })
  }

  it('does not call RestorePageVersion when opening Compare', async () => {
    renderModal()
    await openCompare()
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('loads the selected version and the live page when opening Compare', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-preview-body')).toHaveTextContent(
        '# newest body'
      )
    })
    expect(appMocks.GetPageVersion).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily',
      'v-new'
    )
    const versionCalls = appMocks.GetPageVersion.mock.calls.length
    await openCompare()
    expect(appMocks.FetchPageMarkdown).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily'
    )
    expect(appMocks.GetPageVersion).toHaveBeenCalled()
    expect(appMocks.GetPageVersion.mock.calls.length).toBe(versionCalls)
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('refreshes compare live body after page:external-reload', async () => {
    renderModal()
    await openCompare()
    expect(screen.getByTestId('page-history-compare')).toHaveTextContent(
      'live body'
    )
    appMocks.FetchPageMarkdown.mockResolvedValue('# restored body')
    eventsMock.handlers.get('page:external-reload')?.({
      data: { notebook: 'Work', section: 'Journal', page: 'Daily' }
    })
    await waitFor(() => {
      expect(screen.getByTestId('page-history-compare')).toHaveTextContent(
        'restored body'
      )
    })
    expect(appMocks.FetchPageMarkdown).toHaveBeenCalledTimes(2)
  })

  it('refreshes compare after a case-variant page:external-reload', async () => {
    renderModal()
    await openCompare()
    appMocks.FetchPageMarkdown.mockResolvedValue('# restored body')
    eventsMock.handlers.get('page:external-reload')?.({
      data: { notebook: 'work', section: 'journal', page: 'daily' }
    })
    await waitFor(() => {
      expect(screen.getByTestId('page-history-compare')).toHaveTextContent(
        'restored body'
      )
    })
  })

  it('restarts compare fetch when page:external-reload arrives while loading', async () => {
    let resolveFirst!: (value: string) => void
    appMocks.FetchPageMarkdown.mockImplementationOnce(
      () =>
        new Promise<string>((resolve) => {
          resolveFirst = resolve
        })
    )
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-pane-compare')).not.toBeDisabled()
    })
    await fireEvent.click(screen.getByTestId('page-history-pane-compare'))
    await waitFor(() => {
      expect(appMocks.FetchPageMarkdown).toHaveBeenCalledTimes(1)
    })
    appMocks.FetchPageMarkdown.mockResolvedValue('# restored-during-load')
    eventsMock.handlers.get('page:external-reload')?.({
      data: { notebook: 'Work', section: 'Journal', page: 'Daily' }
    })
    resolveFirst('# stale-in-flight')
    await waitFor(() => {
      expect(screen.getByTestId('page-history-compare')).toHaveTextContent(
        'restored-during-load'
      )
    })
    expect(screen.getByTestId('page-history-compare')).not.toHaveTextContent(
      'stale-in-flight'
    )
  })

  it('restores from Compare through ConfirmDialog and RestorePageVersion once', async () => {
    renderModal()
    await openCompare()
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(appMocks.RestorePageVersion).toHaveBeenCalledTimes(1)
      expect(appMocks.RestorePageVersion).toHaveBeenCalledWith(
        'Work',
        'Journal',
        'Daily',
        'v-new'
      )
    })
  })

  it('shows an empty-diff status when the version matches the current page', async () => {
    appMocks.GetPageVersion.mockResolvedValue('# same body')
    appMocks.FetchPageMarkdown.mockResolvedValue('# same body')
    renderModal()
    await openCompare()
    expect(screen.getByTestId('page-history-diff-summary')).toHaveTextContent(
      /No body changes\. Frontmatter is not compared/
    )
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('does not announce no-change when the compare is too large', async () => {
    const huge = 'x'.repeat(COMPARE_MAX_CHARS + 1)
    appMocks.GetPageVersion.mockResolvedValue(huge)
    appMocks.FetchPageMarkdown.mockResolvedValue(`${huge}y`)
    renderModal()
    await openCompare()
    expect(screen.getByTestId('page-history-diff-summary')).toHaveTextContent(
      /too large to show/
    )
    expect(
      screen.getByTestId('page-history-diff-summary')
    ).not.toHaveTextContent(/No body changes/)
    expect(screen.getByTestId('page-history-compare')).toHaveTextContent(
      /too large to show/
    )
  })

  it('toggles split and unified compare layout', async () => {
    renderModal()
    await openCompare()
    await fireEvent.click(screen.getByTestId('page-history-layout-unified'))
    expect(screen.getByTestId('page-history-layout-unified')).toHaveAttribute(
      'aria-checked',
      'true'
    )
    await fireEvent.click(screen.getByTestId('page-history-layout-split'))
    expect(screen.getByTestId('page-history-layout-split')).toHaveAttribute(
      'aria-checked',
      'true'
    )
  })

  it('clears the restore status when another version is selected', async () => {
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-status')).toBeTruthy()
    })
    const options = screen.getAllByRole('option')
    expect(options.length).toBeGreaterThan(1)
    await fireEvent.click(options[1])
    await waitFor(() => {
      expect(screen.queryByTestId('page-history-status')).toBeNull()
    })
  })

  it('uses disk markdown when a registered editor is clean', async () => {
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => false,
      flush: async () => true,
      forceExternalReload: () => {},
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    appMocks.GetPageVersion.mockResolvedValue('# Title')
    appMocks.FetchPageMarkdown.mockResolvedValue('# Title')
    renderModal()
    await openCompare()
    expect(appMocks.FetchPageMarkdown).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily'
    )
    expect(screen.getByTestId('page-history-diff-summary')).toHaveTextContent(
      /No body changes/
    )
  })

  it('compares last-saved disk content when the editor is dirty', async () => {
    const flush = vi.fn(async () => {
      throw new Error('flush should not run')
    })
    registerEditor({
      key: 'Work\x00Journal\x00Daily',
      isDirty: () => true,
      flush,
      forceExternalReload: () => {},
      clearExternalReload: () => {},
      setProposedEdit: () => false,
      clearProposedEdit: () => {},
      hasProposal: () => false,
      acceptProposedEdit: () => false,
      verifySelectionText: () => false
    })
    appMocks.GetPageVersion.mockResolvedValue('# Title')
    appMocks.FetchPageMarkdown.mockResolvedValue('# Title')
    renderModal()
    await openCompare()
    expect(flush).not.toHaveBeenCalled()
    expect(appMocks.FetchPageMarkdown).toHaveBeenCalledWith(
      'Work',
      'Journal',
      'Daily'
    )
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    expect(screen.getByTestId('page-history-diff-summary')).toHaveTextContent(
      /No body changes/
    )
    expect(screen.getByTestId('page-history-compare-saved')).toHaveTextContent(
      /last saved page/
    )
    expect(screen.getByText('Last saved page')).toBeTruthy()
  })

  it('clears a compare error after a successful restore', async () => {
    appMocks.FetchPageMarkdown.mockRejectedValueOnce(
      new Error('Could not read the current page.')
    )
    renderModal()
    await waitFor(() => {
      expect(screen.getByTestId('page-history-pane-compare')).not.toBeDisabled()
    })
    await fireEvent.click(screen.getByTestId('page-history-pane-compare'))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-compare-error')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(appMocks.RestorePageVersion).toHaveBeenCalled()
    })
    expect(screen.queryByTestId('page-history-compare-error')).toBeNull()
  })

  it('opens deleted pages from the live modal header', async () => {
    const onClose = vi.fn()
    const seen = vi.fn()
    window.addEventListener(OPEN_DELETED_PAGE_HISTORY_EVENT, seen)
    renderModal(onClose)
    await waitFor(() => {
      expect(screen.getByTestId('page-history-deleted-pages')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-deleted-pages'))
    expect(onClose).toHaveBeenCalledTimes(1)
    expect(seen).toHaveBeenCalled()
    window.removeEventListener(OPEN_DELETED_PAGE_HISTORY_EVENT, seen)
  })

  describe('deleted mode', () => {
    function renderDeleted(onClose = vi.fn()) {
      return render(PageHistoryModal, {
        props: {
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          deleted: true,
          onClose
        }
      })
    }

    it('restores with RestoreDeletedPageVersion and empty dest', async () => {
      const onClose = vi.fn()
      const navigated = vi.fn()
      window.addEventListener('navigate-to-page', navigated)
      renderDeleted(onClose)
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(appMocks.RestoreDeletedPageVersion).toHaveBeenCalledWith(
          'Work',
          'Journal',
          'Daily',
          'v-new',
          '',
          '',
          ''
        )
      })
      expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
      expect(navigated).toHaveBeenCalled()
      const detail = navigated.mock.calls[0][0].detail
      expect(detail).toMatchObject({
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily'
      })
      expect(onClose).toHaveBeenCalledTimes(1)
      window.removeEventListener('navigate-to-page', navigated)
    })

    it('shows Restore as… after page_history_exists leftover dest', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_history_exists',
            message:
              'that name still has leftover page history; restore it in place or choose a different location'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore-as')).toBeTruthy()
      })
      expect(screen.getByText(/leftover page history/)).toBeTruthy()
    })

    it('shows Restore as… after page_exists and retries with dest fields', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'a page already exists at that location'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore-as')).toBeTruthy()
      })
      expect(screen.getByLabelText('Restore as page')).toHaveValue('Daily 2')
      expect(screen.getByLabelText('Restore as notebook')).toHaveValue('Work')
      expect(
        screen.getByText(/a page already exists at that location/)
      ).toBeTruthy()
      expect(screen.getByLabelText('Restore as page')).toHaveValue('Daily 2')
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'restoring here would overwrite an existing page'
          })
        )
      )
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByLabelText('Restore as page')).toHaveValue('Daily 3')
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(appMocks.RestoreDeletedPageVersion).toHaveBeenLastCalledWith(
          'Work',
          'Journal',
          'Daily',
          'v-new',
          'Work',
          'Journal',
          'Daily 3'
        )
      })
      expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    })

    it('keeps a cleared restore-as section after a second collision', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'a page already exists at that location'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore-as')).toBeTruthy()
      })
      await fireEvent.input(screen.getByLabelText('Restore as section'), {
        target: { value: '' }
      })
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'restoring here would overwrite an existing page'
          })
        )
      )
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByLabelText('Restore as page')).toHaveValue('Daily 3')
      })
      expect(screen.getByLabelText('Restore as section')).toHaveValue('')
    })

    it('does not open Restore as… when the original page still exists', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_still_exists',
            message:
              'that page still exists; restore it from page history instead'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent(
          /restore it from page history instead/
        )
      })
      expect(screen.queryByTestId('page-history-restore-as')).toBeNull()
    })

    it('navigates to the restore-as destination after success', async () => {
      const navigated = vi.fn()
      window.addEventListener('navigate-to-page', navigated)
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'restoring here would overwrite an existing page'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore-as')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(navigated).toHaveBeenCalled()
      })
      expect(navigated.mock.calls[0][0].detail).toMatchObject({
        notebook: 'Work',
        section: 'Journal',
        page: 'Daily 2'
      })
      window.removeEventListener('navigate-to-page', navigated)
    })

    it('surfaces a generic restore failure without opening Restore as…', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'navigation_unavailable',
            message: 'snapshot store read failed'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByRole('alert')).toHaveTextContent(
          /snapshot store read failed/
        )
      })
      expect(screen.queryByTestId('page-history-restore-as')).toBeNull()
    })

    it('disables Restore when restore-as dest fields are cleared', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'restoring here would overwrite an existing page'
          })
        )
      )
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore-as')).toBeTruthy()
      })
      await fireEvent.input(screen.getByLabelText('Restore as page'), {
        target: { value: '' }
      })
      expect(screen.getByTestId('page-history-restore')).toBeDisabled()
    })

    it('keeps a year suffix when suggesting a restore-as name', async () => {
      appMocks.RestoreDeletedPageVersion.mockRejectedValueOnce(
        new Error(
          JSON.stringify({
            code: 'page_exists',
            message: 'restoring here would overwrite an existing page'
          })
        )
      )
      render(PageHistoryModal, {
        props: {
          notebook: 'Work',
          section: 'Journal',
          page: 'Budget 2026',
          deleted: true,
          onClose: vi.fn()
        }
      })
      await waitFor(() => {
        expect(screen.getByTestId('page-history-restore')).toBeTruthy()
      })
      await fireEvent.click(screen.getByTestId('page-history-restore'))
      await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
      await waitFor(() => {
        expect(screen.getByLabelText('Restore as page')).toHaveValue(
          'Budget 2026 2'
        )
      })
    })

    it('routes header close and backdrop to onBack in deleted mode', async () => {
      const onClose = vi.fn()
      const onBack = vi.fn()
      render(PageHistoryModal, {
        props: {
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          deleted: true,
          onBack,
          onClose
        }
      })
      await waitFor(() => {
        expect(screen.getByTestId('page-history-preview-body')).toBeTruthy()
      })
      const closers = screen.getAllByLabelText('Back to deleted pages')
      await fireEvent.click(closers[0])
      expect(onBack).toHaveBeenCalledTimes(1)
      expect(onClose).not.toHaveBeenCalled()
    })

    it('routes Esc to onBack instead of closing the stack', async () => {
      const onClose = vi.fn()
      const onBack = vi.fn()
      render(PageHistoryModal, {
        props: {
          notebook: 'Work',
          section: 'Journal',
          page: 'Daily',
          deleted: true,
          onBack,
          onClose
        }
      })
      await waitFor(() => {
        expect(screen.getByTestId('page-history-preview-body')).toBeTruthy()
      })
      await fireEvent.keyDown(window, { key: 'Escape' })
      expect(onBack).toHaveBeenCalledTimes(1)
      expect(onClose).not.toHaveBeenCalled()
    })

    it('disables Compare for deleted pages', async () => {
      renderDeleted()
      await waitFor(() => {
        expect(screen.getByTestId('page-history-preview-body')).toBeTruthy()
      })
      expect(screen.getByTestId('page-history-pane-compare')).toBeDisabled()
      await fireEvent.click(screen.getByTestId('page-history-pane-compare'))
      expect(screen.queryByTestId('page-history-compare')).toBeNull()
      expect(appMocks.FetchPageMarkdown).not.toHaveBeenCalled()
    })
  })
})
