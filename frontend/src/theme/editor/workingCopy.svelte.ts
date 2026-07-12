// Working-copy model for the custom theme editor. Edits never mutate the
// seed (or embeds); live preview is FE flatten → injectTokens (rAF-coalesced).
// Discard / leave restore the active theme tokens.

import { injectTokens } from '../inject'
import { flattenTheme } from '../flatten'
import { restoreActiveTheme } from '../store.svelte'
import type { AdvancedGroup, ThemeDoc, ThemeModeKey } from '../types'

export type WorkingCopy = ReturnType<typeof createWorkingCopy>

export function createWorkingCopy() {
  let seed = $state.raw<ThemeDoc | null>(null)
  let draft = $state.raw<ThemeDoc | null>(null)
  let editMode = $state<ThemeModeKey>('dark')
  let advancedGroup = $state<AdvancedGroup>('surfaces')
  let showAdvanced = $state(false)
  let loadError = $state<string | null>(null)
  let loading = $state(false)

  let rafId: number | null = null
  let pendingTokens: Record<string, string> | null = null

  const dirty = $derived.by(() => {
    if (!seed || !draft) return false
    return JSON.stringify(seed) !== JSON.stringify(draft)
  })

  function loadFromJson(json: string): void {
    loadError = null
    try {
      const parsed = JSON.parse(json) as ThemeDoc
      if (!parsed?.modes?.dark || !parsed?.modes?.light) {
        throw new Error('Theme is missing dark/light modes')
      }
      seed = structuredClone(parsed)
      draft = structuredClone(parsed)
      schedulePreview()
    } catch (err) {
      loadError = err instanceof Error ? err.message : String(err)
      seed = null
      draft = null
    }
  }

  /** Surface an IPC/bootstrap failure without a fake parse path. */
  function setLoadError(message: string): void {
    loadError = message
    seed = null
    draft = null
  }

  function resetAll(): void {
    if (!seed) return
    draft = structuredClone(seed)
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
    draft =
      value === undefined
        ? deleteAtPath(draft, path)
        : setAtPath(draft, path, structuredClone(value))
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
          : setAtPath(next, path, structuredClone(value))
    }
    draft = next
    schedulePreview()
  }

  function setAt(path: string, value: unknown): void {
    if (!draft) return
    // undefined means "remove this key" (fonts, optional surfaces, etc.).
    draft =
      value === undefined
        ? deleteAtPath(draft, path)
        : setAtPath(draft, path, value)
    schedulePreview()
  }

  /** Convenience: set a color string at a document path. */
  function setColor(path: string, value: string): void {
    setAt(path, value)
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
    getModeValue
  }
}

function getAtPath(obj: unknown, path: string): unknown {
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

function setAtPath<T>(obj: T, path: string, value: unknown): T {
  const parts = path.split('.').filter(Boolean)
  if (parts.length === 0) return obj
  const root = structuredClone(obj) as Record<string, unknown>
  let cur: Record<string, unknown> = root
  for (let i = 0; i < parts.length - 1; i++) {
    const key = parts[i]
    const next = cur[key]
    if (next === null || next === undefined || typeof next !== 'object') {
      cur[key] = {}
    } else {
      cur[key] = structuredClone(next)
    }
    cur = cur[key] as Record<string, unknown>
  }
  cur[parts[parts.length - 1]] = value
  return root as T
}

/**
 * Remove a leaf key at a dotted path. Prunes empty parent objects so
 * resetting an inherited zone (e.g. surfaces.sidebar) does not leave
 * `{ sidebar: { bg: undefined } }` or empty `{}` shells behind.
 */
function deleteAtPath<T>(obj: T, path: string): T {
  const parts = path.split('.').filter(Boolean)
  if (parts.length === 0) return obj
  const root = structuredClone(obj) as Record<string, unknown>

  function walk(cur: Record<string, unknown>, depth: number): boolean {
    const key = parts[depth]
    if (depth === parts.length - 1) {
      if (!(key in cur)) return Object.keys(cur).length === 0
      delete cur[key]
      return Object.keys(cur).length === 0
    }
    const next = cur[key]
    if (next === null || next === undefined || typeof next !== 'object') {
      return Object.keys(cur).length === 0
    }
    const child = structuredClone(next) as Record<string, unknown>
    const empty = walk(child, depth + 1)
    if (empty) {
      delete cur[key]
    } else {
      cur[key] = child
    }
    return Object.keys(cur).length === 0
  }

  walk(root, 0)
  return root as T
}
