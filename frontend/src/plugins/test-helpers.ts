// Shared no-op stubs for the v2 SDK PluginContext methods, used by test/mock
// context builders so every mock does not have to repeat 27 no-op closures.
// Production code builds the real context via makePluginContext (context.ts).

import type { PluginContext, SqliteQueryResult } from './sdk'

const emptyResult: SqliteQueryResult = { rows: [], truncated: false }

/**
 * Default no-op implementations of the v2 SDK content/file/OS methods. Spread
 * this into a mock context and override only the methods a test exercises.
 */
export const v2CtxStubs: Pick<
  PluginContext,
  | 'getUiLocation'
  | 'queryByTag'
  | 'queryByDateRange'
  | 'fullTextSearch'
  | 'getBacklinks'
  | 'getEmbeds'
  | 'searchBlocks'
  | 'searchTasks'
  | 'createBlock'
  | 'createTask'
  | 'setTaskDueDate'
  | 'setTaskStartDate'
  | 'setTaskRecurrence'
  | 'setTaskBlockedBy'
  | 'setTaskOwner'
  | 'setTaskOrder'
  | 'setTaskOrders'
  | 'setTaskPriority'
  | 'setTaskTags'
  | 'setTaskTitle'
  | 'setTaskEstimate'
  | 'getTaskBlockers'
  | 'fetchSubtree'
  | 'getLocalAuthor'
  | 'saveSubtreeBlocks'
  | 'deleteBlock'
  | 'moveBlock'
  | 'applyBlocks'
  | 'createPage'
  | 'createSection'
  | 'createNotebook'
  | 'deletePage'
  | 'renamePage'
  | 'readFile'
  | 'writeFile'
  | 'deleteFile'
  | 'listDir'
  | 'notebookRoot'
  | 'scratchDir'
  | 'openInNativeHandler'
  | 'openUrl'
  | 'pickOpenFile'
  | 'pickSaveFile'
  | 'clipboardRead'
  | 'clipboardWrite'
  | 'notify'
  | 'fetch'
  | 'registerSlashCommand'
  | 'provideDecorations'
  | 'getSetting'
  | 'updatePluginSetting'
  | 'openSettings'
  | 'registerSurface'
  | 'addAttachment'
  | 'openAttachment'
  | 'deleteAttachment'
  | 'pluginDb'
  | 'ai'
  | 'vaultScratchDir'
  | 'resolveAsset'
  | 'readPluginAsset'
  | 'getNavigationTree'
  | 'addTaskComment'
> = {
  getUiLocation: () => ({
    notebook: '',
    section: '',
    page: '',
    openTabs: []
  }),
  queryByTag: () => Promise.resolve(emptyResult),
  queryByDateRange: () => Promise.resolve(emptyResult),
  fullTextSearch: () => Promise.resolve(emptyResult),
  getBacklinks: () => Promise.resolve(emptyResult),
  getEmbeds: () => Promise.resolve(emptyResult),
  searchBlocks: () => Promise.resolve([]),
  searchTasks: () => Promise.resolve([]),
  createBlock: () => Promise.resolve(''),
  createTask: () => Promise.resolve(''),
  setTaskDueDate: () => Promise.resolve(true),
  setTaskStartDate: () => Promise.resolve(true),
  setTaskRecurrence: () => Promise.resolve(true),
  setTaskBlockedBy: () => Promise.resolve(true),
  setTaskOwner: () => Promise.resolve(true),
  setTaskOrder: () => Promise.resolve(true),
  setTaskOrders: () => Promise.resolve(true),
  setTaskPriority: () => Promise.resolve(true),
  setTaskTags: () => Promise.resolve(true),
  setTaskTitle: () => Promise.resolve(true),
  setTaskEstimate: () => Promise.resolve(true),
  getTaskBlockers: () => Promise.resolve([]),
  fetchSubtree: () => Promise.resolve([]),
  getLocalAuthor: () => Promise.resolve(''),
  saveSubtreeBlocks: () => Promise.resolve(true),
  deleteBlock: () => Promise.resolve(true),
  moveBlock: () => Promise.resolve(true),
  applyBlocks: () => Promise.resolve(true),
  createPage: () => Promise.resolve(''),
  createSection: () => Promise.resolve(true),
  createNotebook: () => Promise.resolve(true),
  deletePage: () => Promise.resolve(true),
  renamePage: () => Promise.resolve(true),
  readFile: () => Promise.resolve(new Uint8Array(0)),
  writeFile: () => Promise.resolve(true),
  deleteFile: () => Promise.resolve(true),
  listDir: () => Promise.resolve([]),
  notebookRoot: () => Promise.resolve(''),
  scratchDir: () => Promise.resolve(''),
  openInNativeHandler: () => Promise.resolve(true),
  openUrl: () => Promise.resolve(true),
  pickOpenFile: () => Promise.resolve(''),
  pickSaveFile: () => Promise.resolve(''),
  clipboardRead: () => Promise.resolve(''),
  clipboardWrite: () => Promise.resolve(true),
  notify: () => Promise.resolve(true),
  fetch: () =>
    Promise.resolve({
      status: 0,
      headers: {},
      body: '',
      ok: false,
      truncated: false
    }),
  registerSlashCommand: () => () => {},
  provideDecorations: () => () => {},
  getSetting: () => Promise.resolve(undefined),
  updatePluginSetting: () => Promise.resolve(true),
  openSettings: () => {},
  registerSurface: () => () => {},
  addAttachment: () => Promise.resolve(''),
  openAttachment: () => Promise.resolve(true),
  deleteAttachment: () => Promise.resolve(true),
  pluginDb: {
    exec: () => Promise.resolve(),
    query: () => Promise.resolve({ rows: [], truncated: false }),
    migrate: () => Promise.resolve()
  },
  ai: {
    complete: ((req: { stream?: boolean }) => {
      if (req?.stream) {
        const empty = {
          streamId: 'test',
          cancel: async () => {},
          result: async () => ({ content: '', model: '' }),
          async *[Symbol.asyncIterator]() {}
        }
        return Promise.resolve(empty)
      }
      return Promise.resolve({ content: '', model: '', usage: undefined })
    }) as unknown as PluginContext['ai']['complete'],
    embed: () =>
      Promise.resolve({
        embeddings: [],
        model: '',
        dimensions: 0,
        usage: undefined
      })
  },
  vaultScratchDir: () => Promise.resolve(''),
  resolveAsset: () => Promise.resolve(''),
  readPluginAsset: () => Promise.resolve(''),
  getNavigationTree: () => Promise.resolve({ notebooks: [] }),
  addTaskComment: () => Promise.resolve('')
}
