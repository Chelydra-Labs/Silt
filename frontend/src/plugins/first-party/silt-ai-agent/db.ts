// Per-plugin SQLite migration for silt-ai-agent (#596).
//
// The Go host tracks schema via a single PRAGMA user_version counter per
// plugin DB, so all schemas in this DB must share ONE linear version
// sequence. The embed-index tables (index_meta/chunks, owned by
// shared/retrieval) are therefore folded into v2 alongside the staging
// schema rather than registering a separate "version 1" that would collide
// with the already-stamped staging v1 and silently never apply.

import type { PluginContext } from '../../sdk'
import { INDEX_TABLES_SQL } from '../../shared/retrieval/embed_index'

/** Historical v1 (staging schema only) — already stamped on existing installs. */
const MIGRATION_V1_SQL = `
CREATE TABLE IF NOT EXISTS staging_tokens (
  token TEXT PRIMARY KEY,
  plugin_id TEXT NOT NULL,
  operation TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  expires_at INTEGER NOT NULL,
  used INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_staging_expires ON staging_tokens(expires_at);
`

export const MIGRATION_V2 = 2
/**
 * v2 = staging + index tables. Both sets are included so the schema
 * converges regardless of which migration path hits a DB first: activation
 * (staging) and RAG start (index) are both fire-and-forget and unordered,
 * and all SQL is IF NOT EXISTS so re-application is idempotent.
 */
export const MIGRATION_V2_SQL = MIGRATION_V1_SQL + INDEX_TABLES_SQL

/**
 * Run the current migration. Safe on every vault open — the Go side tracks
 * PRAGMA user_version per-vault-DB and no-ops when already applied, so no
 * global flag is needed (a module-level flag would race on rapid vault
 * switches, letting a detached Vault-A migration stamp the flag after Vault B
 * opens and skip Vault B's schema).
 */
export async function migrateSchema(ctx: PluginContext): Promise<void> {
  await ctx.pluginDb.migrate(MIGRATION_V2, MIGRATION_V2_SQL)
}
