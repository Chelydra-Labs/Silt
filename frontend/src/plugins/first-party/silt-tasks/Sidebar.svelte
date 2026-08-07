<script lang="ts">
  // Unified Tasks sidebar (#432, decomposed #763). Three section components
  // render under this layout-only container:
  //
  //   1. Smart Lists — Today/Upcoming/Overdue/Completed/All with live
  //      counts (sidebar/SmartLists.svelte).
  //   2. Saved Views — system + user views with fingerprint highlight
  //      (sidebar/SavedViews.svelte).
  //   3. Jump-to-Date mini-cal — per-day dots + click-to-focus
  //      (sidebar/MiniCalendar.svelte).
  //
  // All three read+write the unified hub singleton (state.svelte.ts) so a
  // tweak here is instantly reflected in the header FilterBar and every
  // display-mode renderer. The container owns only lifecycle: the reload
  // signal that fans out to SmartLists + MiniCalendar, and the 60s nowTick
  // that gates the local-day-rollover check.
  import { onMount } from 'svelte'
  import type { PluginContext, PluginManifest } from '../../sdk'
  import SmartLists from './sidebar/SmartLists.svelte'
  import SavedViews from './sidebar/SavedViews.svelte'
  import MiniCalendar from './sidebar/MiniCalendar.svelte'

  interface Props {
    ctx: PluginContext
    manifest?: PluginManifest
  }

  let { ctx, manifest: _manifest }: Props = $props()

  // Reload orchestration: block:changed (200ms debounce) and
  // refresh-navigation bump reloadSignal, which SmartLists + MiniCalendar
  // watch to re-query. The 60s nowTick feeds the day-rollover gate below.
  let reloadSignal = $state(0)
  let nowTick = $state(0)
  let blockTimer: ReturnType<typeof setTimeout> | null = null
  let offBlock: (() => void) | undefined
  let nowInterval: ReturnType<typeof setInterval> | undefined

  onMount(() => {
    offBlock = ctx.on('block:changed', () => {
      if (blockTimer) clearTimeout(blockTimer)
      blockTimer = setTimeout(() => {
        reloadSignal++
      }, 200)
    })
    const onRefresh = () => {
      reloadSignal++
    }
    window.addEventListener('refresh-navigation', onRefresh)
    nowInterval = setInterval(() => {
      nowTick++
    }, 60_000)
    return () => {
      window.removeEventListener('refresh-navigation', onRefresh)
      if (blockTimer) clearTimeout(blockTimer)
      offBlock?.()
      if (nowInterval) clearInterval(nowInterval)
    }
  })

  // Re-bucket children only when the local-day actually changes — a bare
  // nowTick with no day change is a no-op (mirrors CalendarSidebar's gate).
  // The first run seeds lastSeenToday WITHOUT bumping reloadSignal: children
  // already load once at mount, and an initial 0→1 bump would trigger a
  // wasteful second query whose stale result loadSeq then drops.
  let lastSeenToday = ''
  let dayGateSeeded = false
  $effect(() => {
    void nowTick
    const t = ctx.today
    if (!dayGateSeeded) {
      dayGateSeeded = true
      lastSeenToday = t
      return
    }
    if (t === lastSeenToday) return
    lastSeenToday = t
    reloadSignal++
  })
</script>

<aside
  class="flex flex-col gap-3 px-2.5 py-3"
  aria-label="Tasks sidebar"
  data-test-tasks-sidebar
>
  <SmartLists {ctx} {reloadSignal} />
  <SavedViews />
  <MiniCalendar {ctx} {reloadSignal} />
</aside>
