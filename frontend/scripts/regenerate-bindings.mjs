#!/usr/bin/env node
// Regenerate frontend/bindings/ (the Go→JS/TS IPC stubs) from the live Go
// signatures on `App`. Runs `wails3 generate bindings -d frontend/bindings`
// after ensuring the `frontend/dist` placeholder exists (required because
// main.go declares `//go:embed all:frontend/dist` which fails Go compilation
// if the dir is absent). Tolerates a missing `wails3` CLI: most contributors
// have it installed, but CI's `npm audit` job and brand-new contributors
// don't, and `npm install` should never fail because of an unrelated dev
// tool. When `wails3` is absent we print a one-line pointer and exit 0 — the
// binding staleness is a hotfix-vs-day-1 tradeoff, not a fatal error.
//
// Lives at frontend/scripts/ so it can be invoked from package.json
// (`generate` for explicit refresh, `prepare` for auto-on-install). Cross-
// platform: no shell, no Bashisms — uses Node + child_process.spawnSync.

import { spawnSync } from 'node:child_process'
import { mkdirSync, existsSync, writeFileSync, chmodSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const FRONTEND_DIR = dirname(HERE)
const REPO_ROOT = dirname(FRONTEND_DIR)
const DIST_DIR = join(FRONTEND_DIR, 'dist')
const DIST_PLACEHOLDER = join(DIST_DIR, '.gitkeep')

function ensureDistPlaceholder() {
  if (existsSync(DIST_PLACEHOLDER)) return
  mkdirSync(DIST_DIR, { recursive: true })
  // Empty file is enough — `//go:embed all:frontend/dist` only requires the
  // directory to exist; an extra `.gitkeep` lets contributors notice a
  // forgotten `npm run build` in `git status` without polluting the dev
  // server with a stray HTML file.
  writeFileSync(DIST_PLACEHOLDER, '')
}

function hasWailsCli() {
  const probe = process.platform === 'win32' ? 'where' : 'which'
  const result = spawnSync(probe, ['wails3'], { stdio: 'ignore' })
  return result.status === 0
}

const SKIP_FLAG = process.env.SILT_SKIP_BINDING_REGEN
if (SKIP_FLAG === '1' || SKIP_FLAG === 'true') {
  console.log('[regenerate-bindings] skipped (SILT_SKIP_BINDING_REGEN set)')
  process.exit(0)
}

if (!hasWailsCli()) {
  console.log(
    '[regenerate-bindings] `wails3` CLI not found on PATH — skipping. ' +
      'The frontend/bindings/ stubs will be stale until you run ' +
      '`go install github.com/wailsapp/wails/v3/cmd/wails3@latest` and ' +
      're-run `npm run generate` (or `wails3 dev` / `wails3 build`).'
  )
  process.exit(0)
}

ensureDistPlaceholder()

const result = spawnSync(
  'wails3',
  ['generate', 'bindings', '-d', 'frontend/bindings'],
  {
    cwd: REPO_ROOT,
    stdio: 'inherit'
  }
)
process.exit(result.status ?? 1)
