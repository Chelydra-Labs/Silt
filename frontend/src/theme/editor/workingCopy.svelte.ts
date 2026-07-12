// Working-copy model for the custom theme editor. Edits never mutate the
// seed (or embeds); live preview is FE flatten → injectTokens (rAF-coalesced).
// Discard / leave restore the active theme tokens.
//
// Hot path (#532): setAtPath/deleteAtPath clone only the path spine (shallow
// copies along the path) so rapid OKLCH slider/plane drags do not deep-clone
// the whole theme. Dirty is a flag flipped on mutate and cleared on
// load/resetAll/discard; resetPath/resetGroup recompute via JSON equality
// (rare) so partial resets stay correct without per-tick stringify.

import { injectTokens } from '../inject'
import { flattenTheme } from '../flatten'
import { restoreActiveTheme } from '../store.svelte'
import { deriveActive, deriveDisabled, deriveHover, toHex } from '../color'
import type { AdvancedGroup, ThemeDoc, ThemeModeKey } from '../types'

export type WorkingCopy = ReturnType<typeof createWorkingCopy>

/** Seed → derived interaction token wiring (editor session only; not schema). */
export const DERIVED_FROM_SEED: ReadonlyArray<{
  seedSuffix: string
  derivedSuffix: string
  derive: (seed: string) => string | null
}> = [
  {
    seedSuffix: 'surfaces.app.bg',
    derivedSuffix: 'hover',
    derive: deriveHover
  },
  {
    seedSuffix: 'surfaces.app.bg',
    derivedSuffix: 'active',
    derive: deriveActive
  },
  {
    seedSuffix: 'surfaces.app.text',
    derivedSuffix: 'text_disabled',
    derive: deriveDisabled
  }
]

/**
 * Optional surface zones must be complete (bg + border + text) or absent.
 * After a partial reset, drop incomplete zones so they inherit cleanly.
 */
export function pruneIncompleteSurfaces(doc: ThemeDoc): ThemeDoc {
  const next = structuredClone(doc)
  for (const modeKey of ['dark', 'light'] as const) {
    const surfaces = next.modes[modeKey]?.surfaces
    if (!surfaces || typeof surfaces !== 'object') continue
    for (const zone of Object.keys(surfaces) as (keyof typeof surfaces)[]) {
      if (zone === 'app') continue
      const surface = surfaces[zone]
      if (!surface || typeof surface !== 'object') {
        delete surfaces[zone]
        continue
      }
      const bg = surface.bg
      const border = surface.border
      const text = surface.text
      if (!bg || !border || !text) {
        delete surfaces[zone]
      }
    }
  }
  return next
}

/** Format-tolerant color equality for derived-match checks. */
export function colorsMatch(a: unknown, b: unknown): boolean {
  if (a === b) return true
  if (typeof a !== 'string' || typeof b !== 'string') return false
  const ha = toHex(a)
  const hb = toHex(b)
  if (ha && hb) return ha === hb
  return a.trim().toLowerCase() === b.trim().toLowerCase()
}

export function createWorkingCopy() {
  let seed = $state.raw<ThemeDoc | null>(null)
  let draft = $state.raw<ThemeDoc | null>(null)
  let editMode = $state<ThemeModeKey>('dark')
  let advancedGroup = $state<AdvancedGroup>('surfaces')
  let showAdvanced = $state(false)
  let loadError = $state<string | null>(null)
  let loading = $state(false)
  /** Cheap dirty flag — not JSON.stringify on every slider tick (#532). */
  let dirtyFlag = $state(false)
  /** Bumps on every draft mutation (tests / optional consumers). */
  let mutationGen = $state(0)
  /**
   * Editor-session locks for derived tokens (#529). Not persisted in theme JSON.
   * When locked, seed edits do not overwrite the derived path.
   */
  let lockedDerived = $state.raw<Set<string>>(new Set())

  let rafId: number | null = null
  let pendingTokens: Record<string, string> | null = null

  const dirty = $derived(dirtyFlag)

  function bumpMutation(): void {
    mutationGen += 1
  }

  function clearDirty(): void {
    dirtyFlag = false
    bumpMutation()
  }

  function recomputeDirty(): void {
    if (!seed || !draft) {
      dirtyFlag = false
      return
    }
    dirtyFlag = JSON.stringify(seed) !== JSON.stringify(draft)
  }

  function loadFromJson(json: string): void {
    loadError = null
    try {
      const parsed = JSON.parse(json) as ThemeDoc
      if (!parsed?.modes?.dark || !parsed?.modes?.light) {
        throw new Error('Theme is missing dark/light modes')
      }
      seed = structuredClone(parsed)
      draft = structuredClone(parsed)
      lockedDerived = new Set()
      clearDirty()
      schedulePreview()
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err)
      seed = null
      draft = null
      lockedDerived = new Set()
      dirtyFlag = false
    }
  }

  /** Surface an IPC/bootstrap failure without a fake parse path. */
  function setLoadError(message: string): void {
    loadError = message
    seed = null
    draft = null
    lockedDerived = new Set()
    dirtyFlag = false
  }

  function resetAll(): void {
    if (!seed) return
    draft = structuredClone(seed)
    lockedDerived = new Set()
    clearDirty()
    schedulePreview()
  }

  /**
   * Reset a dotted path under the draft to the seed value.
   * Path is relative to the document root, e.g.
   * `modes.dark.surfaces.app.bg` or `typography.font_family`.
   * When the seed has no value at the path, the draft key is deleted
   * (not set to undefined) so inherited zones stay clean.
   */
  function resetPath(path: string): void {
    if (!seed || !draft) return
    const value = getAtPath(seed, path)
    let next =
      value === undefined
        ? deleteAtPath(draft, path)
        : setAtPath(draft, path, cloneLeaf(value))
    // Partial optional surfaces fail backend validate — drop incomplete zones.
    next = pruneIncompleteSurfaces(next)
    unlockDerived(path)
    // Restoring a seed leaf/object must re-derive unlocked interaction tokens.
    next = applyDerivedFromSeedEdit(next, path)
    draft = next
    recomputeDirty()
    bumpMutation()
    schedulePreview()
  }

  /** Reset several document paths to seed in one draft write. */
  function resetGroup(paths: string[]): void {
    if (!seed || !draft || paths.length === 0) return
    let next = draft
    for (const path of paths) {
      const value = getAtPath(seed, path)
      next =
        value === undefined
          ? deleteAtPath(next, path)
          : setAtPath(next, path, cloneLeaf(value))
      unlockDerived(path)
    }
    next = pruneIncompleteSurfaces(next)
    for (const path of paths) {
      next = applyDerivedFromSeedEdit(next, path)
    }
    draft = next
    recomputeDirty()
    bumpMutation()
    schedulePreview()
  }

  function setAt(path: string, value: unknown): void {
    if (!draft) return
    // Manual edit of a derived path auto-locks so seed edits won't clobber it.
    // Applied here (not only setColor) so any caller path stays consistent.
    if (isDerivedPath(path)) {
      lockDerived(path)
    }
    // undefined means "remove this key" (fonts, optional surfaces, etc.).
    let next =
      value === undefined
        ? deleteAtPath(draft, path)
        : setAtPath(draft, path, value)

    // Re-derive unlocked interaction tokens when a seed color changes (#529).
    next = applyDerivedFromSeedEdit(next, path)
    draft = next
    // Recompute vs seed so editing back to original clears dirty (sticky-dirty fix).
    // One JSON.stringify per mutation is far cheaper than full-doc clone; hot path
    // cost remains path-spine clone + flatten, not structuredClone(theme).
    recomputeDirty()
    bumpMutation()
    schedulePreview()
  }

  /** Convenience: set a color string at a document path. */
  function setColor(path: string, value: string): void {
    setAt(path, value)
  }

  /**
   * Re-derive unlocked interaction tokens when app seed colors change.
   * Accepts leaf paths (`…surfaces.app.bg|text`) and whole-object writes
   * (`…surfaces.app`) used by Advanced → Surfaces `updateSurface`.
   */
  function applyDerivedFromSeedEdit(doc: ThemeDoc, path: string): ThemeDoc {
    const leaf = /^modes\.(dark|light)\.(surfaces\.app\.(?:bg|text))$/.exec(
      path
    )
    if (leaf) {
      return rederiveFromSeedSuffix(doc, leaf[1] as ThemeModeKey, leaf[2])
    }
    // Whole app surface object (bg + border + text + optional background).
    const obj = /^modes\.(dark|light)\.surfaces\.app$/.exec(path)
    if (obj) {
      const mode = obj[1] as ThemeModeKey
      let next = doc
      next = rederiveFromSeedSuffix(next, mode, 'surfaces.app.bg')
      next = rederiveFromSeedSuffix(next, mode, 'surfaces.app.text')
      return next
    }
    return doc
  }

  function rederiveFromSeedSuffix(
    doc: ThemeDoc,
    mode: ThemeModeKey,
    seedSuffix: string
  ): ThemeDoc {
    const seedPath = `modes.${mode}.${seedSuffix}`
    const seedVal = getAtPath(doc, seedPath)
    if (typeof seedVal !== 'string') return doc
    let next = doc
    for (const rule of DERIVED_FROM_SEED) {
      if (rule.seedSuffix !== seedSuffix) continue
      const derivedPath = `modes.${mode}.${rule.derivedSuffix}`
      if (lockedDerived.has(derivedPath)) continue
      const derived = rule.derive(seedVal)
      if (derived == null) continue
      next = setAtPath(next, derivedPath, derived)
    }
    return next
  }

  function isDerivedPath(path: string): boolean {
    return DERIVED_FROM_SEED.some(
      (r) =>
        path === `modes.dark.${r.derivedSuffix}` ||
        path === `modes.light.${r.derivedSuffix}`
    )
  }

  function lockDerived(path: string): void {
    if (lockedDerived.has(path)) return
    const next = new Set(lockedDerived)
    next.add(path)
    lockedDerived = next
  }

  function unlockDerived(path: string): void {
    if (!lockedDerived.has(path)) return
    const next = new Set(lockedDerived)
    next.delete(path)
    lockedDerived = next
  }

  function isDerivedLocked(path: string): boolean {
    return lockedDerived.has(path)
  }

  function setDerivedLocked(path: string, locked: boolean): void {
    if (locked) lockDerived(path)
    else unlockDerived(path)
  }

  /**
   * Reset a derived token to derive*(current seed value) and unlock it.
   */
  function resetDerivedToFormula(path: string): void {
    if (!draft) return
    const m = /^modes\.(dark|light)\.(.+)$/.exec(path)
    if (!m) return
    const mode = m[1] as ThemeModeKey
    const suffix = m[2]
    const rule = DERIVED_FROM_SEED.find((r) => r.derivedSuffix === suffix)
    if (!rule) return
    const seedPath = `modes.${mode}.${rule.seedSuffix}`
    const seedVal = getAtPath(draft, seedPath)
    if (typeof seedVal !== 'string') return
    const derived = rule.derive(seedVal)
    if (derived == null) return
    unlockDerived(path)
    draft = setAtPath(draft, path, derived)
    recomputeDirty()
    bumpMutation()
    schedulePreview()
  }

  /** True when draft value equals pure derivation from current seed color. */
  function isDerivedMatch(path: string): boolean {
    if (!draft) return false
    const m = /^modes\.(dark|light)\.(.+)$/.exec(path)
    if (!m) return false
    const mode = m[1] as ThemeModeKey
    const suffix = m[2]
    const rule = DERIVED_FROM_SEED.find((r) => r.derivedSuffix === suffix)
    if (!rule) return false
    const seedVal = getAtPath(draft, `modes.${mode}.${rule.seedSuffix}`)
    const cur = getAtPath(draft, path)
    if (typeof seedVal !== 'string' || typeof cur !== 'string') return false
    const expected = rule.derive(seedVal)
    if (expected == null) return false
    return colorsMatch(cur, expected)
  }

  function setEditMode(mode: ThemeModeKey): void {
    if (editMode === mode) return
    editMode = mode
    schedulePreview()
  }

  function schedulePreview(): void {
    if (!draft) return
    pendingTokens = flattenTheme(draft, editMode)
    if (typeof requestAnimationFrame !== 'function') {
      flushPreview()
      return
    }
    if (rafId !== null) return
    rafId = requestAnimationFrame(() => {
      rafId = null
      flushPreview()
    })
  }

  function flushPreview(): void {
    if (!pendingTokens) return
    injectTokens(pendingTokens)
    pendingTokens = null
  }

  /** Force an immediate inject (tests / mode switch after rAF cancel). */
  function previewNow(): void {
    if (rafId !== null && typeof cancelAnimationFrame === 'function') {
      cancelAnimationFrame(rafId)
      rafId = null
    }
    if (!draft) return
    pendingTokens = flattenTheme(draft, editMode)
    flushPreview()
  }

  function discard(): void {
    if (rafId !== null && typeof cancelAnimationFrame === 'function') {
      cancelAnimationFrame(rafId)
      rafId = null
    }
    pendingTokens = null
    if (seed) {
      draft = structuredClone(seed)
    }
    lockedDerived = new Set()
    clearDirty()
    restoreActiveTheme()
  }

  function modePath(rel: string): string {
    return `modes.${editMode}.${rel}`
  }

  function getModeValue<T = unknown>(rel: string): T | undefined {
    if (!draft) return undefined
    return getAtPath(draft, modePath(rel)) as T | undefined
  }

  return {
    get seed() {
      return seed
    },
    get draft() {
      return draft
    },
    get editMode() {
      return editMode
    },
    get dirty() {
      return dirty
    },
    get mutationGen() {
      return mutationGen
    },
    get advancedGroup() {
      return advancedGroup
    },
    set advancedGroup(v: AdvancedGroup) {
      advancedGroup = v
    },
    get showAdvanced() {
      return showAdvanced
    },
    set showAdvanced(v: boolean) {
      showAdvanced = v
    },
    get loadError() {
      return loadError
    },
    setLoadError,
    get loading() {
      return loading
    },
    set loading(v: boolean) {
      loading = v
    },
    loadFromJson,
    resetAll,
    resetPath,
    resetGroup,
    setAt,
    setColor,
    setEditMode,
    schedulePreview,
    previewNow,
    discard,
    modePath,
    getModeValue,
    isDerivedLocked,
    setDerivedLocked,
    resetDerivedToFormula,
    isDerivedMatch
  }
}

function cloneLeaf(value: unknown): unknown {
  if (value !== null && typeof value === 'object') {
    return structuredClone(value)
  }
  return value
}

function shallowCloneContainer(
  value: unknown
): Record<string, unknown> | unknown[] {
  if (Array.isArray(value)) return value.slice()
  if (value !== null && typeof value === 'object') {
    return { ...(value as Record<string, unknown>) }
  }
  return {}
}

export function getAtPath(obj: unknown, path: string): unknown {
  const parts = path.split('.').filter(Boolean)
  let cur: unknown = obj
  for (const p of parts) {
    if (cur === null || cur === undefined || typeof cur !== 'object') {
      return undefined
    }
    cur = (cur as Record<string, unknown>)[p]
  }
  return cur
}

/** Clone only objects along `path`; share unchanged siblings (#532). */
export function setAtPath<T>(obj: T, path: string, value: unknown): T {
  const parts = path.split('.').filter(Boolean)
  if (parts.length === 0) return obj
  const root = shallowCloneContainer(obj) as Record<string, unknown>
  let cur: Record<string, unknown> = root
  let src: unknown = obj
  for (let i = 0; i < parts.length - 1; i++) {
    const key = parts[i]
    const nextSrc =
      src !== null && typeof src === 'object'
        ? (src as Record<string, unknown>)[key]
        : undefined
    const cloned = shallowCloneContainer(nextSrc) as Record<string, unknown>
    cur[key] = cloned
    cur = cloned
    src = nextSrc
  }
  cur[parts[parts.length - 1]] = cloneLeaf(value)
  return root as T
}

/**
 * Remove a leaf key at a dotted path. Prunes empty parent objects so
 * resetting an inherited zone (e.g. surfaces.sidebar) does not leave
 * `{ sidebar: { bg: undefined } }` or empty `{}` shells behind.
 * Path-spine clone only (#532).
 */
export function deleteAtPath<T>(obj: T, path: string): T {
  const parts = path.split('.').filter(Boolean)
  if (parts.length === 0) return obj
  const root = shallowCloneContainer(obj) as Record<string, unknown>

  function walk(
    cur: Record<string, unknown>,
    src: unknown,
    depth: number
  ): boolean {
    const key = parts[depth]
    if (depth === parts.length - 1) {
      if (!(key in cur)) return Object.keys(cur).length === 0
      delete cur[key]
      return Object.keys(cur).length === 0
    }
    const nextSrc =
      src !== null && typeof src === 'object'
        ? (src as Record<string, unknown>)[key]
        : undefined
    if (
      nextSrc === null ||
      nextSrc === undefined ||
      typeof nextSrc !== 'object'
    ) {
      return Object.keys(cur).length === 0
    }
    const child = shallowCloneContainer(nextSrc) as Record<string, unknown>
    const empty = walk(child, nextSrc, depth + 1)
    if (empty) {
      delete cur[key]
    } else {
      cur[key] = child
    }
    return Object.keys(cur).length === 0
  }

  walk(root, obj, 0)
  return root as T
}
