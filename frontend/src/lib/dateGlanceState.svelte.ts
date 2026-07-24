import type { Editor } from '@tiptap/core'

// Shared state for the Date Glance popover (#730). One popover instance,
// rendered once in App.svelte, driven by this state. The three openers
// (status-bar chip, global hotkey, /calendar slash command) each call
// openDateGlance with the editor to insert into (when one is available).
//
// The anchor element is owned by the status-bar chip — it registers itself
// on mount so every opener (including the slash command, which has no chip
// ref) surfaces the popover in the same consistent place. insertEditor is
// captured at open time (before the popover takes focus and blurs the
// editor), so the day-pick handler can re-focus + insert at the cursor.
//
// openGen increments on every open so the popover can detect re-opens
// (not just false→true transitions) and reset its view each time.

export interface DateGlanceState {
  open: boolean
  anchor: HTMLElement | null
  insertEditor: Editor | null
  /** Bumped on every open; lets consumers detect re-opens while already open. */
  openGen: number
}

export const dateGlance: DateGlanceState = $state({
  open: false,
  anchor: null,
  insertEditor: null,
  openGen: 0
})

/** Register the persistent anchor element (the status-bar chip) on mount. */
export function setDateGlanceAnchor(el: HTMLElement | null): void {
  dateGlance.anchor = el
}

export function openDateGlance(insertEditor: Editor | null = null): void {
  dateGlance.insertEditor = insertEditor
  dateGlance.open = true
  dateGlance.openGen++
}

/** Toggle the popover; closes if already open, opens otherwise. */
export function toggleDateGlance(insertEditor: Editor | null = null): void {
  if (dateGlance.open) closeDateGlance()
  else openDateGlance(insertEditor)
}

export function closeDateGlance(): void {
  dateGlance.open = false
  dateGlance.insertEditor = null
}

/**
 * Drop the insert target. Called when the editor unmounts (page switch) so a
 * destroyed editor doesn't receive a stale insert. Silt has one editor at a
 * time (one page = one TipTap instance), so clearing unconditionally is safe —
 * a $state proxy makes reference-equality comparison unreliable anyway.
 */
export function clearInsertEditor(): void {
  dateGlance.insertEditor = null
}
