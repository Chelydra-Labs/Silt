# Testing & Verification — Sprint 1 (Foundation)

## Automated Tests

Run with: `go test -race -count=1 ./...`

### Coverage by package

| Package | Tests | What is covered |
|---|---|---|
| `notes-sharp` (main) | FindLineByBlockID, sanitizePathSegment, isPathWithinVault, UpdateBlockState (transitions, traversal rejection, non-task rejection), FetchSectionTimeline (pagination, empty), QueryTasks (owner, priority, tags, hydration) | Wails API surface |
| `backend/core` | DB write serialization, DB read concurrency, per-file lock isolation, same-file serialization, error propagation | ExecutionCoordinator |
| `backend/db` | Block insertion with cascade, replacement, empty re-index, frontmatter tag attachment (loop-index fix), metadata-change re-index stability, tag deduplication, N+1 fix verification, pagination and empty timeline, filter combinations, tag hydration, IndexScanResults skip-collection | DatabaseManager |
| `backend/monitor` | Tracker immediate check, cooldown timeout, expired entry cleanup, background sweeper, prune expired, stop idempotency, concurrent PruneExpired, reindexFile lock-holding test, reindexFile end-to-end | DirectoryWatcher, WriteTracker |
| `backend/parser` | ID injection, date normalization (4 cases), line parsing (task/header/note), file content parsing (frontmatter metadata, parent-child), code block ID protection (single + multiple fences), YAML frontmatter error surfacing | AST parser |
| `backend/vault` | Settings round-trip (atomic write + load), corrupt JSON error path | Settings durability |

### Benchmark

Run with: `go test -bench=. -count=3 ./backend/parser/`

**Phase 3 startup budget:** < 450ms for 1,000 daily-note files.

Baseline (Ryzen AI MAX+, Go 1.25, Windows): **~280ms** — within budget.

```
BenchmarkScanWorkspace_1000Files    1    ~252–334ms/op
```

## Manual Verification

Per Phase 6 of `PLAN.md`:

1. **`wails dev` onboarding flow**
   - Run `wails dev` from the project root.
   - Confirm the "Initialize Workspace Folder" button opens the native Wails folder selector.
   - Select a folder; confirm the vault scaffolds `Work/Journal/<today>.md`, `.system/config.yaml`, `.system/themes/cyber_forest.json`.
   - Confirm the UI transitions to "Vault Ready".
   - Close and reopen; confirm the vault auto-loads without re-showing the folder picker.

2. **Task state transitions**
   - With the vault loaded, use the browser console to invoke `window.go.main.App.UpdateBlockState("<block-id>", "DOING")` on a known block ID from the welcome note.
   - Verify the file on disk has the updated checkbox state.

3. **Watcher self-loop prevention**
   - Edit a `.md` file externally (e.g., in VS Code) while `wails dev` is running.
   - Confirm the change is indexed (DB query visible in logs) and no infinite write-loop occurs.

## Known Gaps (deferred to future sprints)

- No Wails integration test (requires `wails dev` runtime, see #32)
- No watcher e2e test against real fsnotify events
- No symlink-loop detection in `ScanWorkspace` (see #32 follow-up)
- No `ClearVault` / switch-workspace path (see #33)
