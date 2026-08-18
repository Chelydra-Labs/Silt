import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest'
import {
  render,
  screen,
  cleanup,
  fireEvent,
  waitFor
} from '@testing-library/svelte'

const appMocks = vi.hoisted(() =>
  createAppIpcMocks({
    ListDeletedPageHistory: vi.fn(),
    ListPageVersions: vi.fn(),
    GetPageVersion: vi.fn(),
    RestorePageVersion: vi.fn(),
    RestoreDeletedPageVersion: vi.fn(),
    FetchPageMarkdown: vi.fn()
  })
)

vi.mock('$silt-app', () => appMocks)

const settingsMock = vi.hoisted(() => ({
  config: {
    editor: { auto_versioning_enabled: true }
  }
}))

vi.mock('../../settings/store.svelte', () => ({
  settings: settingsMock
}))

import DeletedPageHistoryModal from './DeletedPageHistoryModal.svelte'
import {
  OPEN_DELETED_PAGE_HISTORY_EVENT,
  openDeletedPageHistory
} from './openDeletedPageHistory'

const ORPHANS = [
  {
    notebook: 'Work',
    section: 'Journal',
    page: 'Gone',
    source: 'vault',
    versionCount: 3,
    latestTimestamp: '2026-08-16T18:00:00Z',
    latestBytes: 120
  },
  {
    notebook: 'Personal',
    section: 'Notes',
    page: 'OldIdea',
    source: 'vault',
    versionCount: 1,
    latestTimestamp: '2026-08-10T09:30:00Z',
    latestBytes: 40
  }
]

const VERSIONS = [
  {
    id: 'v-new',
    timestamp: '2026-08-16T18:00:00Z',
    source: 'editor',
    bytes: 120
  }
]

beforeEach(() => {
  appMocks.ListDeletedPageHistory.mockReset().mockResolvedValue(ORPHANS)
  appMocks.ListPageVersions.mockReset().mockResolvedValue(VERSIONS)
  appMocks.GetPageVersion.mockReset().mockResolvedValue('# deleted body')
  appMocks.RestorePageVersion.mockReset().mockResolvedValue(undefined)
  appMocks.RestoreDeletedPageVersion.mockReset().mockResolvedValue(undefined)
  appMocks.FetchPageMarkdown.mockReset().mockResolvedValue('')
})

afterEach(() => {
  cleanup()
})

describe('DeletedPageHistoryModal', () => {
  it('filters rows from the search field', async () => {
    render(DeletedPageHistoryModal, { props: { onClose: vi.fn() } })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Gone/ })).toBeTruthy()
    })
    expect(screen.getByRole('option', { name: /OldIdea/ })).toBeTruthy()
    await fireEvent.input(screen.getByLabelText('Search deleted pages'), {
      target: { value: 'gone' }
    })
    expect(screen.getByRole('option', { name: /Gone/ })).toBeTruthy()
    expect(screen.queryByRole('option', { name: /OldIdea/ })).toBeNull()
    expect(screen.getByTestId('deleted-page-history-count')).toHaveTextContent(
      /1 matching deleted page/
    )
  })

  it('restores a selected deleted page through RestoreDeletedPageVersion', async () => {
    render(DeletedPageHistoryModal, { props: { onClose: vi.fn() } })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Gone/ })).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('option', { name: /Gone/ }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-restore')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-restore'))
    expect(appMocks.RestoreDeletedPageVersion).not.toHaveBeenCalled()
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
    await fireEvent.click(screen.getByTestId('page-history-confirm-confirm'))
    await waitFor(() => {
      expect(appMocks.RestoreDeletedPageVersion).toHaveBeenCalledWith(
        'Work',
        'Journal',
        'Gone',
        'v-new',
        '',
        '',
        ''
      )
    })
    expect(appMocks.RestorePageVersion).not.toHaveBeenCalled()
  })

  it('shows a Retry control when the deleted list fails to load', async () => {
    appMocks.ListDeletedPageHistory.mockRejectedValueOnce(
      new Error('could not list deleted pages')
    )
    render(DeletedPageHistoryModal, { props: { onClose: vi.fn() } })
    await waitFor(() => {
      expect(screen.getByRole('alert')).toHaveTextContent(
        /could not list deleted pages/
      )
    })
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Gone/ })).toBeTruthy()
    })
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('returns focus to search after Back from a nested page', async () => {
    render(DeletedPageHistoryModal, { props: { onClose: vi.fn() } })
    await waitFor(() => {
      expect(screen.getByRole('option', { name: /Gone/ })).toBeTruthy()
    })
    await fireEvent.click(screen.getByRole('option', { name: /Gone/ }))
    await waitFor(() => {
      expect(screen.getByTestId('page-history-back')).toBeTruthy()
    })
    await fireEvent.click(screen.getByTestId('page-history-back'))
    await waitFor(() => {
      expect(screen.getByLabelText('Search deleted pages')).toBe(
        document.activeElement
      )
    })
    expect(screen.getByRole('option', { name: /Gone/ })).toBeTruthy()
  })

  it('dispatches silt:open-deleted-page-history from the helper', () => {
    const seen = vi.fn()
    window.addEventListener(OPEN_DELETED_PAGE_HISTORY_EVENT, seen)
    openDeletedPageHistory()
    expect(seen).toHaveBeenCalled()
    expect(seen.mock.calls[0][0].type).toBe(OPEN_DELETED_PAGE_HISTORY_EVENT)
    window.removeEventListener(OPEN_DELETED_PAGE_HISTORY_EVENT, seen)
  })
})
