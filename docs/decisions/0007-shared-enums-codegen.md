# ADR 0007: Shared Go→TypeScript enums via a parallel generator (cmd/genenums)

Date: 2026-07-26
Status: Accepted

## Context

Issue #760 collapses four duplicated constant families into Go as the single
source of truth and has the frontend consume generated TypeScript instead of
re-typing bare string literals:

- **AIProviderType** — provider-type discriminator (`local`,
  `openai-compatible`, `google`, `anthropic`).
- **AIErrorKind** — normalized AI failure taxonomy (`unauthorized`,
  `rate-limited`, `model-missing`, …).
- **IPCErrorCode** — stable IPC error codes (`block_being_edited`,
  `page_exists`, …, #478).
- **EventName** — the Wails event-name set (`block:changed`, `vault:moved`, …).

Each was previously duplicated: Go typed consts on one side, hand-typed string
literals or re-typed unions on the other, so the two could drift silently.

The natural sharing mechanism in a Wails v3 app is `wails3 generate bindings`,
which emits TS model files from the Go type graph. Researching the Wails v3
generator source (`v3/internal/generator/collect/model.go`) and the official
Enums docs, and confirming against Silt's own generated `frontend/bindings/`,
established two hard constraints:

1. **Untyped string consts are never emitted.** `AIProvider*` were untyped
   `string` consts — invisible to the generator under all circumstances.
2. **Typed-const enums are emitted only when reachable** from a bound service
   method's type graph. `AIErrorKind` and `IPCErrorCode` are unreachable today
   because they live only on `error`-returning types (`*AIError`, `*IPCError`),
   which Wails serializes via `MarshalError` as strings, not as modeled structs.

Forcing reachability for the two error enums would require a bound accessor
that exists only for codegen (e.g. `func (a *App) AIErrorKinds() []AIErrorKind`).
That pollutes the IPC method surface that ARCHITECTURE.md §4.3 keeps
authoritative and that `cmd/inventory`'s `-compare` gate enforces in CI.

## Decision

**Add a parallel Go generator, `cmd/genenums` (`tools` build tag, sibling to
`cmd/inventory`), that parses the four Go typed-const blocks via `go/ast` and
emits one committed TypeScript module at `frontend/src/generated/enums.ts`.**

`AIProviderType` is promoted from untyped to a typed enum so all four families
share one parsing shape. The output is a const object + derived union type per
family, plus a sorted `*Names` tuple for iteration. Output is deterministic
(source order, sorted name lists) so a CI drift gate
(`cmd/genenums -compare frontend/src/generated/enums.ts`, mirroring
`cmd/inventory -compare`) fails when a Go const changes without regenerating
the TS module.

The module is **committed** (unlike `frontend/bindings/`, which is gitignored
and Wails-regenerated) so the frontend typechecks without every contributor
needing the `go` toolchain — the same stance as the committed
`cmd/inventory/current-approved-v3.json` fixture. The binding-regen script
(`frontend/scripts/regenerate-bindings.mjs`) regenerates both.

`AIProviderType` additionally becomes reachable through the bound
`AIProviderConfig` model (its `ProviderType` field is now typed), so Wails's own
generator emits it too — a harmless bonus; the frontend imports from
`cmd/genenums`'s output uniformly so there is one coherent pipeline.

## Consequences

- Go is the single source of truth for all four families; the frontend cannot
  drift because it consumes generated values, and CI catches Go↔TS drift.
- The IPC method surface stays clean — no reachability-only accessors.
- One additional committed generated artifact (`enums.ts`) and one additional
  CI drift gate, both following the existing `cmd/inventory` idiom.
- Adding a new enum member is a one-line Go change + `cmd/genenums -update`
  (the CI gate reminds contributors); the exhaustive-switch guard
  (`backend/ai/provider_exhaustive_test.go`) and the `AIErrorKind`
  exhaustiveness test ensure dispatch/formatting covers new members.

## Alternatives considered

- **Wails's own generator + reachability tricks.** Rejected: pollutes the IPC
  surface that `cmd/inventory` gates.
- **Hand-maintained TS modules + a drift test.** Rejected: keeps two sources of
  truth (only guarded, not eliminated) and the test is weaker than codegen.
- **Node-based regex parsing of Go source in `regenerate-bindings.mjs`.**
  Rejected: fragile vs. `go/parser` AST parsing; the project already has the
  Go-tools-build-tag precedent (`cmd/inventory`).
