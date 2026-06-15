import type { PluginContext, SqliteQueryResult, TaskStatus } from './sdk'
import {
  PluginRawQuery,
  PluginMutateBlock,
  PluginUpdateBlockState,
  PluginUpdateTaskMeta
} from '../../wailsjs/go/main/App.js'

/**
 * Build a PluginContext bound to the currently active location. Plugins read
 * the active notebook/section/page from this object and use the query/mutation
 * hooks to talk to the Go backend.
 */
export function makePluginContext(
  activeNotebook: string,
  activeSection: string,
  activePage: string
): PluginContext {
  return {
    activeNotebook,
    activeSection,
    activePage,
    // The Go side returns PluginRawQueryResult{Rows, Truncated}. Surface the
    // structured shape (not just Rows) so plugins can warn on truncation;
    // a missing/empty Rows slice is normalised to [] for the caller's
    // convenience (Wails sometimes hands back undefined for an empty
    // top-level struct, especially before the vault is open).
    sqliteQuery: (sql, params) =>
      PluginRawQuery(sql, params ?? []).then((res) => {
        const out: SqliteQueryResult = {
          rows: (res?.rows as Record<string, unknown>[]) ?? [],
          truncated: !!res?.truncated
        }
        return out
      }),
    mutateBlock: (id, text) => PluginMutateBlock(id, text),
    updateBlockState: (id, status: TaskStatus) =>
      PluginUpdateBlockState(id, status),
    // Pin/progress are file-resident user intent (ARCHITECTURE §0). The
    // Go side uses int sentinels (-1 = no change, 0/1 = pin value); the
    // SDK wrapper translates the ergonomic boolean/number API to them.
    updateTaskMeta: (id, meta) =>
      PluginUpdateTaskMeta(
        id,
        meta.pinned === undefined ? -1 : meta.pinned ? 1 : 0,
        meta.progress === undefined ? -1 : meta.progress
      )
  }
}
