# ADR 0009: Modal primitive — shared overlay component vs native `<dialog>`

Date: 2026-08-06
Status: **Proposed (selection gated on a WebKitGTK de-risk spike)**

## Context

Silt has **eight** overlay-modal surfaces that each reimplement their own
focus trap with a copy-pasted `FOCUSABLE` selector, plus one alert-style
surface (`ModalShell`) that is deliberately trap-less. The selectors have
already drifted three ways, which is the textbook signature of duplicated
logic nobody back-ports:

| Surface | Selector today |
|---|---|
| `components/ConfirmDialog.svelte` | `button, input, [tabindex]` — omits `select`/`textarea`/`a[href]`/`[contenteditable]` |
| `components/ChoiceDialog.svelte` | same — near-clone of ConfirmDialog |
| `components/NamePromptDialog.svelte` | same |
| `components/BlockPickerModal.svelte` | same |
| `properties/TurnIntoDialog.svelte` | same — comment says "mirrors ConfirmDialog" |
| `properties/TypeEditorDialog.svelte` | patched in `select` |
| `properties/PropertiesEditModal.svelte` (new, #873) | uses the shared `lib/focusTrap.ts` |
| `plugins/.../silt-tasks/components/TaskSubEditorModal.svelte` (#780) | patched in `a[href]/select/textarea` |
| `components/settings/ModalShell.svelte` | **no trap** — alert-style by design |

Sprint 44 (#873) created `frontend/src/lib/focusTrap.ts` — a spec-aligned
selector (includes `select`/`textarea`/`a[href]`/`[contenteditable]`, excludes
negative-tabindex/disabled/hidden/`inert`/`aria-hidden`/closed-`<details`) and
Tab/Shift+Tab wrap — and adopted it across the three properties-family dialogs
(`PropertiesEditModal`, `TurnIntoDialog`, `TypeEditorDialog`), fixing two latent
a11y bugs. That util is a **committed seed**, not a finalized primitive: the
going-forward shape for *all* modals still needs a deliberate decision, because
the standards direction (native `<dialog>`) and a possible library both change
what "the primitive" should be. This ADR owns that decision.

A 2026 research + strategy pass (librarian + oracle) established the facts that
constrain it:

- **Native `<dialog>.showModal()`** is the standards direction and is reliable
  on WebView2 (Chromium) and macOS WebKit. It gives a real focus trap, Top
  Layer (immune to parent `z-index`/stacking-context/transform bugs), `inert`
  background, and `::backdrop` for free. But **WebKitGTK is the exposure**:
  Wails uses the distro's `libwebkit2gtk`, which on LTS/stable distros can lag
  macOS WebKit by 12–24 months, and its corners (`::backdrop` painting,
  Top-Layer stacking against Silt's existing `z-[180]`/`z-[200]` overlays,
  `inert` propagation, focus-restore timing, `cancel`/`close` on Esc) are
  exactly where older WebKitGTK has had quirks.
- **jsdom cannot prove any of this.** Silt's CI is jsdom-only (AGENTS.md
  forbids Playwright/headless-webview because the Wails webview cannot run
  headless in CI). So a native-`<dialog>` regression on WebKitGTK would be
  invisible to CI and only surface as a Linux-user focus/a11y bug.
- **A library (Bits UI / shadcn-svelte)** is mature for Svelte 5, but Bits UI's
  Dialog is itself a maintained custom portal (it does **not** delegate to
  native `<dialog>`, so it gains no Top Layer), and adding it conflicts with
  the repo's custom-Svelte, no-UI-framework ethos and #873's explicit non-goal.

## Decision

**The primitive selection is deferred, gated on a manual WebKitGTK de-risk
spike. Until the spike clears, the interim primitive is: the shared
`lib/focusTrap.ts` Tab-wrap action + the in-repo overlay-`<div>` pattern
(`role="dialog"` `aria-modal="true"`, backdrop sentinel, focus restore). New
modals follow this pattern; no new modal introduces native `<dialog>` until the
gate below passes.**

The spike must be run on a real WebKitGTK build (Fedora / Ubuntu LTS), not just
WebView2/macOS, and must verify, for a `showModal()` modal:

1. Tab/Shift+Tab focus wrap is correct (browser-managed, not our action).
2. `::backdrop` paints and is styleable.
3. Top-Layer stacking wins over Silt's existing `z-[180]`/`z-[200]` overlays
   (and over the global-hotkey `dialog, [role="dialog"]` gate in `App.svelte`).
4. `inert` makes the background non-interactive and non-tabbable.
5. Focus restores to the opener on close.
6. Esc fires `cancel`/`close` correctly and does not double-fire with our own
   Esc routing.

## Consequences

- **`lib/focusTrap.ts` is the committed seed** and the interim contract. It is
  consumed by the three properties-family dialogs today; the remaining five
  cross-domain surfaces migrate onto it (or onto whatever the spike selects)
  under **#905**, sequenced: properties-family (done) → generic dialogs
  (`ConfirmDialog`/`ChoiceDialog`/`NamePromptDialog`/`BlockPickerModal`) →
  `TaskSubEditorModal`. `ModalShell` stays as-is (alert-style, no trap).
- **No native `<dialog>` migration happens before the spike.** Shipping one
  native-`<dialog>` modal next to eight overlay-`<div>` modals would be *more*
  divergence, not less, and would bet foundational a11y behavior (focus trap,
  backdrop inertness) on an unverified WebView foundation inside unrelated
  feature work.
- **If the spike passes**, the selected primitive becomes native `<dialog>` +
  a thin Svelte wrapper, and #905 migrates all eight surfaces (and
  `PropertiesEditModal`) to it. If it fails (or is never run), the primitive
  stays the shared overlay-`<div>` + `focusTrap` action — a defensible
  long-term state, just not standards-aligned.
- **No dependency added.** Bits UI / shadcn-svelte remain rejected for this
  purpose; they'd add coupling without solving Top Layer and would re-open the
  no-UI-framework ethos as a separate decision.

## Alternatives considered

- **Adopt native `<dialog>` immediately, migrate incrementally.** Rejected for
  now: one native modal alongside eight overlay modals is more inconsistency,
  the "shared wrapper" would have a population of one, and the WebKitGTK risk
  is undetectable in CI. Reconsider after the spike.
- **Introduce Bits UI / shadcn-svelte as the modal primitive.** Rejected:
  violates the custom-Svelte ethos and #873's non-goal; Bits UI's Dialog is a
  custom portal that does not provide native Top Layer, so it does not solve
  the stacking problem that motivates native `<dialog>`.
- **Status quo (keep hand-rolling per surface).** Rejected: the selector drift
  is already real (three forks), and every new modal re-implements focus
  trap, backdrop, Esc, and restore. `focusTrap.ts` + this ADR exist precisely
  to end that.

## References

- ADR `0008-typed-notes.md` (the bottom-dock rationale; #873 added a modal tier
  rather than replacing the peek).
- #873 (created `PropertiesEditModal` + `focusTrap.ts`; adopted it across the
  properties-family dialogs).
- #905 (tracks the five cross-domain surfaces' migration, blocked on this ADR's
  WebKitGTK gate).
