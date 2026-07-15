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

let migrated = false

export function resetMigrationState(): void {
  migrated = false
}

/** Run the v1 migration once per process. Safe to call on every vault open. */
export async function migrateSchema(ctx: PluginContext): Promise<void> {
  if (migrated) return
  await ctx.pluginDb.migrate(MIGRATION_V1, MIGRATION_V1_SQL)
  migrated = true
}
