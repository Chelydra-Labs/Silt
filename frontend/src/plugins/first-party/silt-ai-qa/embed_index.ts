// Incremental vector index for silt-ai-qa (#224).
// Thin wrapper over shared createEmbedIndex() — isolated instance per plugin.

import {
  createEmbedIndex,
  DEFAULT_MIN_COSINE_SIMILARITY,
  type ProgressCb
} from '../../shared/retrieval/embed_index'

export { DEFAULT_MIN_COSINE_SIMILARITY }
export type { ProgressCb }

const idx = createEmbedIndex()

export const resetIndexState = (): void => idx.resetIndexState()
export const migrateIndex = idx.migrateIndex.bind(idx)
export const ensureIndexReady = idx.ensureIndexReady.bind(idx)
export const needsFullRebuildForModel = idx.needsFullRebuildForModel.bind(idx)
export const metaGet = idx.metaGet.bind(idx)
export const metaSet = idx.metaSet.bind(idx)
export const rebuildIndex = idx.rebuildIndex.bind(idx)
export const dropPageIndex = idx.dropPageIndex.bind(idx)
export const indexPage = idx.indexPage.bind(idx)
export const vectorSearch = idx.vectorSearch.bind(idx)
export const getIndexInfo = idx.getIndexInfo.bind(idx)
