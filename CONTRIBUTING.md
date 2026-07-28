# Contributing to Silt

Silt is a local-first hybrid journal and task manager — plain-text Markdown on
disk, a real-time SQLite index in memory (persisted on disk in WAL mode), and a
Svelte 5 frontend over a Wails Go core. This guide covers the workflow that
keeps the Go, frontend, and IPC-binding layers in sync.

**How to use this document.** The contributor workflow — branching, commits,
the pre-push hook, and how release notes are generated.

- **Authoritative for:** dev/build/test commands, branching & commit
  conventions, the PR merge policy, the release-notes pipeline.

**Principles**
- This is the workflow as the tooling enforces it — when the tooling
  changes, update it here in the same change.
- Commit subjects feed the published changelog, so the commit guidance here
  is load-bearing, not stylistic.

**Rules**
- Keep every command and hook reference current with the repo's actual
  scripts (`.githooks`, `package.json`, workflows).
- Don't restate material that lives in SPECS, ARCHITECTURE, or TESTING —
  link to it instead.

**Best practices**
- Link to the script or workflow file rather than inlining a command that
  can drift.

**Not for**
- Product behavior (SPECS.md), system design (ARCHITECTURE.md), or test
  inventories (TESTING.md).

## Quick start

```sh
# Install hooks so every push is gated (tests, build, binding freshness):
git config core.hooksPath .githooks

# Run the app:
wails3 dev

# Run the Go test suite with the race detector:
go test -race -count=1 ./...

# Type-check + build the frontend:
cd frontend && npm run check && npm run build
```

## Branching & commits

- Work on feature branches off `main`. Open a PR to merge back.
- **Before opening a PR, rebase onto `main`** so `frontend/package-lock.json`
  is current and reviewers see only your real changes.
- Use [Conventional Commits](https://www.conventionalcommits.org/) prefixes:
  `feat:`, `fix:`, `refactor:`, `docs:`, `chore:`.
- Keep commits focused and reviewable; one logical change per commit.
- **Merge-commit PRs** (`gh pr merge --merge`), not squash. Silt generates
  its changelog from Conventional Commits at tag time using git-cliff (see
  *Release notes* below); every `feat:`/`fix:`/`refactor:` commit subject
  on `main` becomes a changelog bullet. Squashing collapses a PR to one
  commit (one bullet) and defeats the mechanism. Make every commit subject
  a user-facing Conventional Commit — `cliff.toml`'s
  `filter_unconventional` silently drops anything else.

## Release notes

Silt generates its changelog from **Conventional Commits at tag time using
[git-cliff](https://git-cliff.org)** (configured in
[`cliff.toml`](./cliff.toml); the generation step is in
[`.github/workflows/release.yml`](./.github/workflows/release.yml)). There is
**no per-PR changelog file** — commit messages are the source.

- **Write user-facing commit subjects.** Because git-cliff renders the
  Conventional-Commit subject (the text after `type(scope):`) into the
  published release notes, each commit's subject line should read like a
  release bullet — describe what changed for the person using Silt, not the
  implementation detail. Example: `feat(find): in-page find and replace`.
- Use the keep-a-changelog-style groups git-cliff maps: `feat:` → Highlights,
  `fix:` → Fixes, `refactor:` and `chore(deps):` → Improvements. `chore:`
  (non-deps), `chore(release):`, `docs:`, `test:`, and `ci:` are skipped from
  the changelog, so routine maintenance never clutters a release.
- **The published version history lives in the GitHub Releases**, not in the
  repo. The generated notes are a draft that's editable on the GitHub Release
  before publishing, so a technical subject can be polished into user-facing
  prose at release time if a commit slipped through with a code-first
  message.

PRs with no user-visible surface (refactors, test-only changes, internal
plumbing) use a skipped prefix (`chore:`, `refactor:` lands in Improvements —
use `chore:` or `test:` if you want it omitted entirely) and contribute
nothing to the changelog.

## Lockfile conflicts

`frontend/package-lock.json` is large and ordering-sensitive, so hand-merging
the conflict markers produces an inconsistent lockfile. `.gitattributes`
configures it with `merge=union`, so most lockfile conflicts resolve
automatically (both sides' added packages are kept). When a manual conflict
remains:

1. Take either side: `git checkout --theirs frontend/package-lock.json`
2. Regenerate from the merged `package.json`:
   ```sh
   cd frontend && npm install
   ```
3. `git add frontend/package-lock.json` and commit.

Do **not** attempt to resolve conflict markers in the lockfile by hand.

## Styling tokens (no arbitrary Tailwind values)

Chrome UI must use **named design tokens** — never hardcoded colors or
arbitrary Tailwind brackets like `text-[12px]`, `grid-cols-[2fr_1fr]`, or
`shadow-[…]`. See **DESIGN.md §2.1.1** for the UI type/icon/grid/shadow scale
(`text-type-*`, `text-icon-*`, `text-display-*`, `grid-cols-settings-theme`,
`shadow-accent-glow`, …). Extending the scale is required before introducing a
new size. All `frontend/src/**/*.svelte` files are guarded by
`frontend/src/theme/arbitrary-values.test.ts`.

## Wails bindings — auto-regenerated on `npm install`

The Go→JS IPC layer is **generated**: every method exposed on `App` in
`app.go` is reflected into `frontend/bindings/` (the JS/TS stubs + models).
The frontend imports those generated files; they must match the live Go
signatures or the frontend calls a function that does not exist (or with the
wrong arg shape).

`frontend/bindings/` is **gitignored** — it is a build artifact, never
committed. Binding regeneration is now automatic:

- `npm install` runs the `prepare` script (`scripts/regenerate-bindings.mjs`),
  which calls `wails3 generate bindings -d frontend/bindings` from the
  project root. A fresh clone produces a working `frontend/bindings/` without
  a manual step, so a newly-added Go method can never silently drift from the
  frontend imports a user has.

If the `wails3` CLI is not on `PATH` (e.g. a brand-new machine that hasn't run
`go install github.com/wailsapp/wails/v3/cmd/wails3@latest` yet), the script
prints a one-line pointer and exits 0 — `npm install` is never blocked by an
unrelated dev-tool install.

You can also run `npm run generate` explicitly at any time to force a refresh
(it calls the same script with the same skip-tolerant behavior).

CI (`.github/workflows/ci.yml`) regenerates the bindings fresh on every run as
part of the build, then runs `svelte-check` + `vite build` — that is the real
Go↔binding consistency guarantee (if a signature changed and the frontend
import went stale, the type-check fails the build).

## Shared enums — `cmd/genenums`

Four constant families — `AIProviderType`, `AIErrorKind`, `IPCErrorCode`, and
`EventName` — have **Go as the single source of truth**. `cmd/genenums` parses
the Go typed-const blocks and emits the committed
`frontend/src/generated/enums.ts`, which the frontend imports directly.

- `npm install` / `npm run generate` regenerate `enums.ts` alongside the Wails
  bindings (same `prepare` script).
- After changing a Go const in one of the four families, regenerate and commit
  the module: `go run -tags tools ./cmd/genenums/ -update frontend/src/generated/enums.ts`
- CI runs `cmd/genenums -compare` (formatting-agnostic) as a drift gate, mirroring
  the `cmd/inventory` parity gate.
- `frontend/src/generated/` is in `.prettierignore` — it is generated output, not
  hand-formatted.
- **Event inventory:** `cmd/inventory` resolves frontend `Events.On(EventName.*)`
  (and template compositions / `${EventName.X}` interpolations) via `enums.ts`.
  The canonical event surface for the IPC gate is still **`go_events`** (from
  `events.go`); `frontend_events` is **best-effort, informational** subscription
  coverage — section diffs do not fail the method-signature gate. The scan also
  resolves an `EventName.*[]` allowlist (`const`/`let`/`var`, `readonly T[]` /
  `ReadonlyArray<T>`, optional `as const`) when `Events.On(ident)` references
  the array or a parameter guarded by `<array>.includes(param)` in the **same
  enclosing function scope** (e.g. `plugins/events.ts`). It does **not** follow
  mixed-type arrays, cross-function includes→On pairs, arbitrary non-allowlist
  locals, or cross-file dataflow; those events only appear if another site uses
  a resolvable form.
- Design rationale (why Wails's own generator was insufficient):
  [`docs/decisions/0007-shared-enums-codegen.md`](./docs/decisions/0007-shared-enums-codegen.md).

## Custom Go analyzers (vettools)

Two project-specific analyzers run in CI and the pre-push hook (same pattern:
`//go:build tools` CLI under `cmd/`, `go build -tags tools -o ./.gobins/…`,
then `go vet -vettool=… ./...`):

| Analyzer | Package | Catches |
|----------|---------|---------|
| **withaipreflightdefer** | `backend/analysis/withaipreflightdefer` | `withAIPreflight`'s done func not deferred (vault-close drain leak) |
| **eventnameliteral** | `backend/analysis/eventnameliteral` | bare string literals / `EventName("…")` as the first arg to `emit` / `emitOrQueue` |

**eventnameliteral** closes the Go untyped-string-constant hole: after
`emit` takes `EventName`, a typo’d `a.emit("tpyo:event", …)` still type-checks.
The analyzer rejects that at CI time. It flags bare string literals,
`EventName("…")` conversions, and those values carried through locals or
same-package single-literal helpers (dominance-aware stores into locals). It
**allows** declared `EventName` consts (including imported packages),
`aiStreamEventName(const, pluginID)`, params, dynamic helpers, phi merges, and
builder/field/map/slice patterns. `_test.go` is skipped so queue tests can keep
bare strings.

Local run (Windows may need a `.exe` suffix on the binary path):

```bash
go build -tags tools -o ./.gobins/eventnameliteral ./cmd/eventnameliteral
go vet -vettool=./.gobins/eventnameliteral ./...
```

## Pre-push hook

`git config core.hooksPath .githooks` enables a fast local Go gate on every
push when any `.go` file changed:

- **`go test -race -count=1 ./...`**
- **IPC binding-parity** — `go run -tags tools ./cmd/inventory/ -compare`
- **Shared-enum drift** — `go run -tags tools ./cmd/genenums/ -compare`
- **withAIPreflight defer-contract** vettool
- **EventName emit-literal** vettool

This is intentionally a *fast local gate* — it catches Go regressions in
seconds before you push, so you're not waiting on CI for a broken build.
**CI (`.github/workflows/ci.yml`) is the authoritative gate** and runs the
full pipeline on Linux (go test -race, npm build, svelte-check, binding
regeneration), including the cross-platform signal the local Windows hook
can't give (symlink + fsnotify tests that skip on Windows). Frontend
validation is left to CI: your IDE + `wails3 dev` cover live editing, and CI
re-validates authoritatively on push.

Documentation-only / asset-only pushes are exempt automatically.

## Testing

See [`TESTING.md`](./TESTING.md) for the full test matrix (per-package
coverage, the startup benchmark budget, and the manual verification checklist
for `wails3 dev`).

## Architecture

See [`ARCHITECTURE.md`](./ARCHITECTURE.md) for the system topology, the
on-disk WAL SQLite index + incremental re-indexing model, the execution
coordinator's locking, and the TTL-lease focus-lock model. Read it before
changing the persistence or concurrency layers.
