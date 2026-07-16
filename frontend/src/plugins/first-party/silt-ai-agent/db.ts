// Per-plugin SQLite migration for silt-ai-agent (#596).
//
// The staging_tokens table is defined now so the DB schema is ready for
// Phase 5 (staged writes with user confirmation). Phase 3 does not populate
// it, but creating it at first vault-open means a later Phase 5 deploy does
// not need a migration window. The expires_at index lets a GC sweep delete
// expired tokens in one range scan.

import type { PluginContext } from '../../sdk'

const MIGRATION_V1 = 1
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

/** Run the v1 migration. Safe on every vault open — the Go side tracks
 *  PRAGMA user_version per-vault-DB and no-ops when already applied, so no
 *  global flag is needed (a module-level flag would race on rapid vault
 *  switches, letting a detached Vault-A migration stamp the flag after Vault B
 *  opens and skip Vault B's schema). */
export async function migrateSchema(ctx: PluginContext): Promise<void> {
  await ctx.pluginDb.migrate(MIGRATION_V1, MIGRATION_V1_SQL)
}
