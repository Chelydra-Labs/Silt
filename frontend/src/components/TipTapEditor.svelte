<script lang="ts">
  import { SvelteSet } from 'svelte/reactivity'
  import { onDestroy, untrack } from 'svelte'
  import { createEditor, EditorContent } from 'svelte-tiptap'
  import type { Editor } from 'svelte-tiptap'
  import StarterKit from '@tiptap/starter-kit'
  import Placeholder from '@tiptap/extension-placeholder'
  import { CharacterCount, Focus, TrailingNode } from '@tiptap/extensions'
  import Typography from '@tiptap/extension-typography'
  import { AutosaveManager } from '../lib/editor/useAutosave'
  import { registerEditor } from '../lib/editor/editorRegistry.svelte'
  import { FocusLockManager } from '../lib/editor/useFocusLock'
  import { BlockIndentOnDrop } from '../lib/editor/dragIndentDrop'
  import { gateBubbleCoords } from '../lib/editor/selectionBubbleGate'
  import { SiltInlineDragHandle } from '../lib/editor/siltInlineDragHandle'
  import { PlainPaste } from '../lib/editor/plainPaste'
  import { Search } from '../lib/editor/search/searchExtension'
  import {
    ProposedEdit,
    hasProposedEdit
  } from '../lib/editor/proposedEdit/ProposedEditExtension'
  import {
    Spellcheck,
    requestSpellcheckRecheck
  } from '../lib/editor/spellcheck/SpellcheckExtension'
  import {
    loadDictionary,
    setCustomWords,
    loadDomainPacks,
    resetDictionary
  } from '../lib/editor/spellcheck/dictionary'
  import {
    dictionaryStatus,
    friendlyPackError
  } from '../lib/editor/spellcheck/dictionaryStatus.svelte'
  import SpellcheckMenu from './editor/SpellcheckMenu.svelte'
  import { TypewriterMode } from '../lib/editor/typewriter/TypewriterModeExtension'
  import {
    SiltBlockExtensionsWithNodeViews,
    SiltInlineMarkExtensions,
    SiltColorMarkExtensions,
    SiltDetailsExtensions,
    SiltTableExtensions,
    UniqueBlockIds,
    SiltHardBreak,
    SiltBlockKeymaps,
    findActiveBlock,
    TaskMetaSuggest,
    MentionSuggest,
    BlockRefSuggest,
    TagSuggest,
    PageLinkSuggest,
    normalizePageLinkAlias,
    pageLinkSourceLabel,
    blocksToDoc,
    docToBlocks
  } from '../lib/editor'
  import type { ParsedBlock } from '../lib/editor'
  import TemplatePicker from '../templates/TemplatePicker.svelte'
  import ChoiceDialog from './ChoiceDialog.svelte'
  import { settings, appendDismissedTip } from '../settings/store.svelte'
  import { pushNotification } from '../notifications/store.svelte'
  import CommandPalette from './CommandPalette.svelte'
  import BlockPickerModal from './BlockPickerModal.svelte'
  import EditorContextMenu from './editor/EditorContextMenu.svelte'
  import FormattingFirstRunTip from './editor/FormattingFirstRunTip.svelte'
  import PluginNoteBanners from './editor/PluginNoteBanners.svelte'
  import SelectionBubble from './editor/SelectionBubble.svelte'
  import TableContextToolbar from './editor/TableContextToolbar.svelte'
  import TableSizePicker from './editor/TableSizePicker.svelte'
  import MathLatexPopover from './editor/MathLatexPopover.svelte'
  import SuggestPopup from './editor/SuggestPopup.svelte'
  import { popupCoordsAt } from '../lib/editor/suggestPopupCoords'
  import {
    deriveColorPalette,
    readActiveThemeColorTokens,
    resolveColor
  } from '../lib/editor/colors'
  import { createSlashMenu } from '../lib/editor/useSlashMenu.svelte'
  import { createEditorEvents } from './editor/controllers/useEditorEvents.svelte'
  import { createPopoversController } from './editor/controllers/usePopovers.svelte'
  import { useSuggests } from './editor/controllers/useSuggests.svelte'
  import { createTemplateInsert } from './editor/controllers/useTemplateInsert.svelte'
  import { createSpellcheckMenu } from './editor/controllers/useSpellcheckMenu.svelte'
  import { clearInsertEditor } from '../lib/dateGlanceState.svelte'
  import {
    setActiveEditor,
    clearActiveEditorState
  } from '../lib/editor/activeEditor.svelte'
  import { clampToViewport } from '../lib/editor/popoverPositioning'
  import { dispatch as dispatchPluginEvent } from '../plugins/events'
  import {
    clearSelectionFocusIfPage,
    recordSelectionFocus
  } from '../plugins/ui-location'
  import type { Node as ProseMirrorNode } from '@tiptap/pm/model'
  import { isSystemDark } from '../lib/systemTheme.svelte'

  interface Props {
    notebook: string
    section: string
    page: string
    blocks: ParsedBlock[]
    activeFocusedBlockAncestors?: string[]
    onBlockFocus?: (blockId: string, ancestors: string[]) => void
    onBlockBlur?: () => void
    onUpdate: (updatedBlocks: ParsedBlock[]) => void
    editorInstance?: Editor | null
    activeMarks?: Set<string>
    wordCount?: number
    /** Emitted when the editor's save state changes (dirty/error → clean).
     *  Used by the tab strip to show per-tab dirty/save-failed indicators
     *  (#167) and the status bar to show the in-flight phase (#546). */
    onSaveStateChange?: (state: {
      phase: 'idle' | 'pending' | 'saving' | 'saved' | 'error'
      dirty: boolean
      error: string | null
    }) => void
    /** Fired once when the ProseMirror editor finishes its initial mount
     *  (onCreate). Lets the parent schedule post-readiness work such as
     *  restoring scroll across an Edit↔Source round-trip (#319). */
    onReady?: () => void
  }

  let {
    notebook,
    section,
    page,
    blocks,
    activeFocusedBlockAncestors: _activeFocusedBlockAncestors = [],
    onBlockFocus,
    onBlockBlur,
    onUpdate,
    editorInstance = $bindable(null),
    activeMarks = $bindable(new SvelteSet<string>()),
    // eslint-disable-next-line no-useless-assignment -- $bindable out-param for parent word count
    wordCount = $bindable<number>(0),
    onSaveStateChange,
    onReady
  }: Props = $props()
  let editorReady = $state(false)
  let isFocused = $state(false)
  let suppressUpdate = false
  // Template-insert cluster (#664): the showTemplatePicker /
  // pendingTemplateBlocks / templateInsertReturnFocus $state cells and the
  // seven handlers they gate (insert-at-cursor vs append-to-end confirmation).
  // Relocated to createTemplateInsert; reads below use templateInsert.*
  // (getters keep template reads reactive).
  const templateInsert = createTemplateInsert({
    getEditor: () => editorInstance
  })
  // Per-vault math opt-out (#191). Live so toggling it in Settings takes effect
  // on the next slash-menu open (hides the /math command).
  let mathEnabled = $derived(
    settings.config?.ui?.formatting?.math_enabled !== false
  )

  // Active inline marks in the current selection (#168). Updated on every
  // selection change so the FormatToolbar buttons reflect aria-pressed state.
  const ALL_MARKS = [
    'bold',
    'italic',
    'underline',
    'strike',
    'code',
    'highlight',
    'subscript',
    'superscript',
    'link',
    'textColor'
  ]

  // Selection bubble state (#168): tracks whether the selection is non-
  // collapsed and the screen coords for positioning the floating bubble.
  // Coords are withheld while the pointer is down so the bubble does not
  // chase a drag-select; keyboard (Shift+Arrow) still shows immediately.
  let selectionEmpty = $state(true)
  let isLastBlock = $state(false)
  let cursorInTable = $state(false)
  let selectionCoords = $state<{
    left: number
    top: number
    bottom: number
  } | null>(null)
  let selectionPointerDown = false
  let pendingSelectionCoords: {
    left: number
    top: number
    bottom: number
  } | null = null

  function readSelectionCoords(editor: {
    isDestroyed: boolean
    state: { selection: { empty: boolean; from: number; to: number } }
    view: {
      coordsAtPos: (pos: number) => {
        left: number
        top: number
        bottom: number
      }
    }
  }): {
    left: number
    top: number
    bottom: number
  } | null {
    const { selection } = editor.state
    if (selection.empty || editor.isDestroyed) return null
    try {
      const start = editor.view.coordsAtPos(selection.from)
      const end = editor.view.coordsAtPos(selection.to)
      return {
        left: (start.left + end.left) / 2,
        top: Math.min(start.top, end.top),
        bottom: Math.max(start.bottom, end.bottom)
      }
    } catch {
      return null
    }
  }

  function publishSelectionCoords(
    coords: { left: number; top: number; bottom: number } | null
  ): void {
    const gated = gateBubbleCoords(
      selectionPointerDown,
      pendingSelectionCoords,
      coords
    )
    pendingSelectionCoords = gated.pending
    selectionCoords = gated.published
  }

  // Track OS dark/light preference reactively so isDark updates when the
  // OS theme changes under mode === 'system' (#168 color palette).
  let isDark = $derived(isSystemDark())

  // Theme-derived color palette (#408): re-read the active theme's anchors
  // from :root whenever the dark/light mode flips, so the popover's swatch
  // row tracks the theme. deriveColorPalette handles fallback internally
  // (cold start / pre-theme-injection).
  const colorPalette = $derived.by(() => {
    void isDark
    const tokens = readActiveThemeColorTokens()
    return deriveColorPalette(tokens)
  })

  // focus_mode config (default false; Phase 3). When true, CSS dims non-active
  // paragraphs for distraction-free writing.
  // color_enabled / show_word_count are consumed by EditorUtilityBar /
  // VirtualScrollContainer, not this component.
  let focusModeEnabled = $derived(settings.config?.editor?.focus_mode === true)

  // Word count is managed as a bindable prop.

  // Popover cluster (Cluster A): the six selection-anchored floating popovers
  // (link input, color picker, table-size picker, LaTeX math popover, block-
  // embed picker) and their open/apply/cancel handlers + dismiss helper, plus
  // the link-autofocus $effect. Relocated verbatim to createPopoversController
  // (editor/controllers/usePopovers.svelte.ts); the controller owns the $state
  // cells and injects getEditor so handlers always see the live editor. Reads
  // below use popovers.* (getters keep template reads reactive).
  const popovers = createPopoversController({ getEditor: () => editorInstance })

  // View mode (#171) is managed by the parent container.

  // First-run tip: dismissed when 'formatting_tip_v1' is in dismissed_tips.
  let formatTipDismissed = $derived(
    settings.config?.ui?.dismissed_tips?.includes('formatting_tip_v1') ?? false
  )

  async function dismissFormatTip(): Promise<void> {
    if (formatTipDismissed) return
    // Snapshot the previous dismissed_tips so we can roll back the optimistic
    // mirror if the IPC call fails — otherwise the UI hides the tip but the
    // on-disk config never recorded the dismissal, so the tip reappears on
    // next launch with no indication that anything went wrong.
    const previous = settings.config?.ui?.dismissed_tips
      ? [...settings.config.ui.dismissed_tips]
      : []
    const ok = await appendDismissedTip('formatting_tip_v1')
    if (!ok) {
      const cfg = settings.config
      if (cfg?.ui) cfg.ui.dismissed_tips = previous
      pushNotification({
        kind: 'error',
        message: 'Could not save the dismiss preference — please try again.'
      })
    }
  }

  // Suggestion-popover cluster: the five typeaheads (task-metadata %,
  // @-mention, block-reference, #tag, page-link). Relocated to useSuggests
  // (editor/controllers/useSuggests.svelte.ts) — the controller owns the popup
  // $state cells, the debounce/race guards, and the IPC, injecting getEditor so
  // handlers always see the live editor. Reads below use suggests.<name>.*
  // (getters keep template reads reactive). Same pattern as createPopoversController.
  const suggests = useSuggests({ getEditor: () => editorInstance })

  function suggestPopupCoords(
    from: number,
    width: number
  ): { left: number; top: number } | null {
    if (!editorInstance || editorInstance.isDestroyed) return null
    const anchor = popupCoordsAt(editorInstance, from)
    return clampToViewport(
      { x: anchor.left, y: anchor.top, width, height: 260 },
      { width: window.innerWidth, height: window.innerHeight }
    )
  }

  function blocksContentKey(source: ParsedBlock[]): string {
    return source
      .map((b) => `${b.id}\0${b.raw_text || b.clean_text || ''}`)
      .join('\n')
  }

  // Capture the initial blocks under untrack to signal that the one-shot
  // capture is intentional — the $effect below handles live reactivity (#64).
  const initialDoc = untrack(() => blocksToDoc(blocks))
  const initialKey = untrack(() => blocksContentKey(blocks))
  let lastSyncedBlocksKey = $state(initialKey)

  // Read config-driven extension toggles at editor creation time (#168 Phase 3).
  // These take effect on the next page load; toggling in Settings does not
  // hot-swap extensions mid-session (acceptable for v1).
  const typographyEnabled = untrack(
    () => settings.config?.ui?.formatting?.typography_enabled !== false
  )

  const editorExtensions = [
    StarterKit.configure({
      // paragraph stays enabled: TipTap's Table extension fills cells with
      // paragraph nodes (tableCell content is 'block+'), and its row/column
      // commands hard-depend on schema.nodes.paragraph. A stray top-level
      // paragraph self-heals — docToBlocks maps any unknown block to NOTE.
      // StarterKit's trailingNode stays disabled (it appends a paragraph);
      // a noteBlock-based TrailingNode is added separately below so an opaque
      // block (table/code/details/embed) that traps the cursor always has an
      // editable line after it the user can click into and type below.
      heading: false,
      bulletList: false,
      orderedList: false,
      listItem: false,
      blockquote: false,
      codeBlock: false,
      horizontalRule: false,
      trailingNode: false,
      // Stock HardBreak refuses isolating parents; SiltHardBreak replaces it (#828).
      hardBreak: false,
      link: { openOnClick: false, autolink: true }
    }),
    SiltHardBreak,
    ...SiltBlockExtensionsWithNodeViews,
    ...SiltInlineMarkExtensions,
    ...SiltColorMarkExtensions,
    ...SiltDetailsExtensions,
    ...SiltTableExtensions,
    UniqueBlockIds,
    // Append an empty noteBlock after a cursor-trapping block (table/codeBlock/
    // details/embedNode/embedBlock) so there is always a clickable line below it.
    // `notAfter` skips the prose blocks the user can already press Enter from.
    TrailingNode.configure({
      node: 'noteBlock',
      notAfter: ['taskBlock', 'headerBlock', 'calloutBlock']
    }),
    TaskMetaSuggest.configure({
      onChange: suggests.meta.onChange,
      onNavigate: suggests.meta.navigate,
      onSelectActive: suggests.meta.selectActive
    }),
    MentionSuggest.configure({
      owners: () => suggests.mention.owners,
      onChange: suggests.mention.onChange,
      onNavigate: suggests.mention.navigate,
      onSelectActive: suggests.mention.selectActive
    }),
    BlockRefSuggest.configure({
      items: () => suggests.blockRef.items,
      onChange: suggests.blockRef.onChange,
      onNavigate: suggests.blockRef.navigate,
      onSelectActive: suggests.blockRef.selectActive
    }),
    TagSuggest.configure({
      items: () => suggests.tag.items,
      onChange: suggests.tag.onChange,
      onNavigate: suggests.tag.navigate,
      onSelectActive: suggests.tag.selectActive
    }),
    PageLinkSuggest.configure({
      items: () => suggests.pageLink.items,
      resolving: () => suggests.pageLink.resolving,
      onChange: suggests.pageLink.onChange,
      onNavigate: suggests.pageLink.navigate,
      onSelectActive: suggests.pageLink.selectActive
    }),
    // Indent-on-drop + drop-zone indicator (#330, #181
    // follow-up). Watches ProseMirror's handleDrop: when a top-level block
    // is dragged, snaps the dropped block's depth to the horizontal drop
    // position and shows a depth-guide indicator during dragover. Returns
    // false on any uncertainty so native PM drop (reorder-only) still
    // runs — never a partial dispatch. The depth math is a pure helper
    // (dragIndentDrop.ts:resolveDropDepth) unit-tested in jsdom; the
    // interactive drag path is gated on the TESTING.md manual matrix
    // (HTML5 drag/drop can't be driven from jsdom per AGENTS.md).
    // The drag-init side is SiltInlineDragHandle (#339) — see
    // frontend/src/lib/editor/siltInlineDragHandle.ts.
    SiltInlineDragHandle,
    BlockIndentOnDrop,
    SiltBlockKeymaps,
    // Ctrl+Shift+V inserts the clipboard as plain text (strips formatting);
    // Ctrl+V (no shift) falls through to ProseMirror's native rich-HTML paste.
    PlainPaste,
    // In-page find (Ctrl+F) — wraps prosemirror-search; decorations + match
    // navigation. Cheap when the query is empty (FindBar closed).
    Search,
    // In-editor proposed-edit preview for Writing Assistant (#543). Decoration
    // only until accept; cheap (empty DecorationSet) when no proposal is shown.
    ProposedEdit,
    // Inline spellcheck (#196) — wavy underlines on misspellings. The
    // dictionary loads + the decoration set rebuilds when the spellcheck config
    // changes (see the $effect below); cheap (no decorations) when disabled.
    Spellcheck,
    // Typewriter mode (#187) — keeps the active line centered. Reads config
    // reactively; no-op (no scroll math) when disabled.
    TypewriterMode,
    Placeholder.configure({
      placeholder: 'Type / for commands, or start writing…'
    }),
    // Editor UX enhancements (#168 Phase 3):
    CharacterCount, // word/char count (always loaded; visibility is CSS-gated)
    Focus, // focus mode (always loaded; dimming is CSS-gated by .focus-mode)
    ...(typographyEnabled ? [Typography] : []) // smart input replacements
  ]

  const editorStore = createEditor({
    extensions: editorExtensions,
    content: initialDoc,
    onUpdate: () => {
      if (suppressUpdate) return
      // The user typed: any stale "force reload on next external change" flag
      // is voided — a user edit makes the buffer authoritative again and must
      // never be clobbered by a leaked pendingExternalReload (#345).
      pendingExternalReload = false
      slash.detectSlashCommand()
      isLastBlock = editorInstance
        ? editorInstance.state.doc.childCount <= 1
        : false
      // Update word count from CharacterCount storage (#168 Phase 3).
      const storage = editorInstance?.storage as unknown as
        Record<string, unknown> | undefined
      const cc = storage?.characterCount as { words?: () => number } | undefined
      if (cc?.words) wordCount = cc.words()
      triggerAutoSave()
    },
    onSelectionUpdate: ({ editor }) => {
      // Track active marks for the FormatToolbar's aria-pressed state (#168).
      // eslint-disable-next-line svelte/prefer-svelte-reactivity -- non-reactive local/helper
      const marks = new Set<string>()
      for (const m of ALL_MARKS) {
        if (editor.isActive(m)) marks.add(m)
      }
      activeMarks = new SvelteSet(marks)
      // Track selection state for the SelectionBubble (#168).
      const { selection } = editor.state
      selectionEmpty = selection.empty
      // Contextual table toolbar (#172): shown when the cursor is inside a
      // table cell (the selection resolves to a tableCell/tableHeader node).
      cursorInTable =
        editor.isActive('tableCell') || editor.isActive('tableHeader')
      publishSelectionCoords(readSelectionCoords(editor))
      // Emit selection:changed on the plugin event bus (#106/#110).
      const selFrom = selection.$from
      // Attempt to read the block id at the selection anchor.
      let blockId: string | undefined
      try {
        const parentAttrs = selFrom.parent.attrs
        if (parentAttrs && parentAttrs.id) blockId = parentAttrs.id
      } catch {
        /* not in a block node */
      }
      // Emit selection:changed on the plugin event bus (#106/#110).
      dispatchPluginEvent('selection:changed', {
        notebook,
        section,
        page,
        blockId
      })
      // Feed agent UI-location snapshot (#680).
      recordSelectionFocus({ notebook, section, page, blockId })
    },
    onFocus: () => {
      isFocused = true
      setActiveEditor(editorInstance)
      acquireFocus()
      startHeartbeat()
      notifyFocus()
      // Refresh the owner list so newly-assigned owners are mentionable.
      // Debounced (~150ms) so a micro focus-blip doesn't fire an IPC round-trip
      // immediately; the TTL guard inside loadOwners collapses repeats (#332).
      suggests.mention.refreshOwners()
      void suggests.tag.loadTags()
    },
    onBlur: () => {
      isFocused = false
      setActiveEditor(null)
      stopHeartbeat()
      // Flush the pending save BEFORE releasing the focus lock so an embed's
      // MutateBlock retry sees the just-saved content rather than overwriting
      // it (#64). The save is awaited, then the lock is released — but only if
      // the editor hasn't been re-focused in the meantime (Date Glance
      // re-focuses the editor after a day-pick; releasing then would drop the
      // lock while the user is actively editing).
      void flushPendingSave().then(() => {
        if (!isFocused) releaseFocus()
      })
      onBlockBlur?.()
    },
    onCreate: ({ editor }) => {
      editorInstance = editor as Editor
      editorReady = true
      isLastBlock = editor.state.doc.childCount <= 1
      onReady?.()
      // Seed the @-mention owner list on mount (#184).
      void suggests.mention.loadOwners()
      void suggests.tag.loadTags()
    }
  })

  // --- Spellcheck dictionary loading — intentionally NOT extracted ---------
  // Only the dictionary/config $effect stays inline here; the menu surface
  // (spellMenu state, openSpellMenuAt, the contextmenu + silt:open-spellcheck
  // listeners) moved to createSpellcheckMenu. This $effect stays because it is
  // intrinsically coupled to ProseMirror decoration rechecks — it calls
  // requestSpellcheckRecheck(editor) and fans out into pushNotification +
  // editor-readiness. Splitting it would fracture a cohesive decoration unit
  // for no decoupling gain.
  // Spellcheck (#196 / #336 / #337): load language dictionary + custom words +
  // domain packs whenever config changes. Disabled → reset → no underlines.
  $effect(() => {
    const enabled = settings.config?.editor?.spellcheck_enabled !== false
    const lang = settings.config?.editor?.spellcheck_language || 'en-US'
    const custom = settings.config?.editor?.custom_dictionary ?? []
    const domains = (
      settings.config?.editor as { spellcheck_domains?: string[] } | undefined
    )?.spellcheck_domains ?? ['software-terms']
    const editor = editorInstance
    if (!editor) return
    void enabled
    void lang
    void custom
    void domains
    if (!enabled) {
      resetDictionary()
      requestSpellcheckRecheck(editor)
      return
    }
    setCustomWords(custom)
    void loadDomainPacks(domains)
      .catch((err: unknown) => {
        console.warn('[silt] domain packs:', err)
        pushNotification({
          kind: 'error',
          message:
            dictionaryStatus.domainError ||
            friendlyPackError(err) ||
            'Could not load technical word lists.'
        })
      })
      .finally(() => {
        requestSpellcheckRecheck(editor)
      })
    void loadDictionary(lang)
      .then(() => {
        requestSpellcheckRecheck(editor)
      })
      .catch((err: unknown) => {
        // Fail loudly: silent degrade leaves no squiggles and looks "fine".
        pushNotification({
          kind: 'error',
          message:
            dictionaryStatus.loadError ||
            friendlyPackError(err) ||
            'Could not load the spellcheck dictionary. Check Settings → Editor.'
        })
      })
  })

  // Spellcheck corrections menu (#196). Right-click on a misspelled word opens
  // the suggestions popover. Disabled when spellcheck is off (no decorations to
  // click). The menu is also opened by the FormatToolbar spellcheck button via
  // the `silt:open-spellcheck` window event (finds the misspelling at/after the
  // cursor) — keeps the toolbar decoupled from the editor internals. Relocated
  // to createSpellcheckMenu; reads below use spellcheckMenu.* (getters keep
  // template reads reactive). The dictionary-loading $effect above STAYS here
  // (coupled to requestSpellcheckRecheck); only the menu surface + DOM
  // listeners moved.
  const spellcheckMenu = createSpellcheckMenu({
    getEditor: () => editorInstance
  })

  // Custom-event bus (silt:* window events). Handlers live in the events
  // controller and bridge into the popover controller's open methods so no
  // state is duplicated. attach()/detach() preserve the original lifecycle:
  // register during init, unregister in onDestroy.
  const events = createEditorEvents({
    getEditor: () => editorInstance,
    openLinkInput: popovers.openLinkInput,
    openColorPickerPopover: popovers.openColorPickerPopover,
    setMathPopover: popovers.setMathPopover
  })
  events.attach()

  // Dismiss the selection-anchored popovers (link / color / math) on ancestor
  // scroll / window resize (#594). Lives in the popover controller; the scroll
  // /resize handlers below delegate to it.
  let selectionScrollReShowTimer: ReturnType<typeof setTimeout> | undefined
  function onEditorScroll(): void {
    selectionCoords = null
    pendingSelectionCoords = null
    // Dismiss the slash palette on scroll (parity with the selection bubble)
    // so it never floats at stale coordinates (#590).
    if (slash.showSlashMenu) slash.dismiss()
    popovers.dismissFloatingPopovers()
    // Hide while scrolling; re-publish fresh viewport coords after settle so
    // SelectionBubble can re-show (coordsAtPos is viewport-relative).
    if (selectionScrollReShowTimer) clearTimeout(selectionScrollReShowTimer)
    selectionScrollReShowTimer = setTimeout(() => {
      selectionScrollReShowTimer = undefined
      const ed = editorInstance
      if (!ed || ed.isDestroyed || selectionPointerDown) return
      publishSelectionCoords(readSelectionCoords(ed))
    }, 160)
  }
  function onWindowResize(): void {
    // A resize can push an open palette/popover off-screen; dismiss rather
    // than chase the cursor (#590 / #594).
    if (slash.showSlashMenu) slash.dismiss()
    popovers.dismissFloatingPopovers()
  }
  // Dismiss SelectionBubble when clicking outside the editor and bubble (#168).
  // The slash palette is guarded by its dedicated data-slash-palette marker
  // (decoupled from the .glass-palette visual class) so restyling the glass
  // treatment cannot silently break dismissal (#584).
  function onDocumentClick(e: MouseEvent): void {
    const target = e.target as HTMLElement | null
    if (!target) return
    if (
      target.closest('.ProseMirror') ||
      target.closest('.selection-bubble') ||
      target.closest('[data-slash-palette]')
    )
      return
    selectionCoords = null
    pendingSelectionCoords = null
    slash.dismiss()
  }

  // Pointer-up gate for the selection bubble: hide while drag-selecting,
  // publish coords on release. Keyboard selection is unaffected.
  function onSelectionPointerDown(e: PointerEvent): void {
    if (e.button !== 0 && e.pointerType === 'mouse') return
    const target = e.target as HTMLElement | null
    if (!target?.closest?.('.ProseMirror')) return
    selectionPointerDown = true
  }
  function onSelectionPointerUp(): void {
    if (!selectionPointerDown) return
    selectionPointerDown = false
    const ed = editorInstance
    if (!ed || ed.isDestroyed) {
      pendingSelectionCoords = null
      return
    }
    // Prefer a fresh read so coords match the final selection range.
    const fresh = readSelectionCoords(ed)
    publishSelectionCoords(fresh ?? pendingSelectionCoords)
  }
  // Blur / tab-away mid-drag must not leave the gate stuck suppressing the bubble.
  function onSelectionPointerReset(): void {
    if (!selectionPointerDown) return
    selectionPointerDown = false
    pendingSelectionCoords = null
  }
  function onVisibilityChange(): void {
    if (document.visibilityState === 'hidden') onSelectionPointerReset()
  }

  window.addEventListener('scroll', onEditorScroll, true)
  window.addEventListener('resize', onWindowResize)
  window.addEventListener('blur', onSelectionPointerReset)
  document.addEventListener('visibilitychange', onVisibilityChange)
  document.addEventListener('click', onDocumentClick)
  document.addEventListener('pointerdown', onSelectionPointerDown, true)
  document.addEventListener('pointerup', onSelectionPointerUp, true)
  document.addEventListener('pointercancel', onSelectionPointerUp, true)

  onDestroy(() => {
    stopHeartbeat()
    // Drop caret-block memory for this page so the agent does not keep a
    // stale block id after the editor unmounts (#680 harden).
    clearSelectionFocusIfPage(notebook, section, page)
    // Drop this editor as the Date Glance insert target so a destroyed editor
    // doesn't receive a stale insert after page navigation (#730 harden).
    clearInsertEditor()
    clearActiveEditorState()
    // Cancel any pending suggest-popover timers / in-flight searches so they
    // don't fire after teardown (#332).
    suggests.mention.destroy()
    suggests.blockRef.destroy()
    suggests.pageLink.destroy()
    void flushPendingSave().then(() => releaseFocus())
    events.detach()
    spellcheckMenu.dispose()
    if (selectionScrollReShowTimer) clearTimeout(selectionScrollReShowTimer)
    window.removeEventListener('scroll', onEditorScroll, true)
    window.removeEventListener('resize', onWindowResize)
    window.removeEventListener('blur', onSelectionPointerReset)
    document.removeEventListener('visibilitychange', onVisibilityChange)
    document.removeEventListener('click', onDocumentClick)
    document.removeEventListener('pointerdown', onSelectionPointerDown, true)
    document.removeEventListener('pointerup', onSelectionPointerUp, true)
    document.removeEventListener('pointercancel', onSelectionPointerUp, true)
  })

  // --- External content sync ------------------------------------------------

  // Set by the editor registry's forceExternalReload() when an out-of-band
  // writer (global replace) has just persisted this page after flushing the
  // editor's own buffer. It lets the next external block update bypass the
  // focused-edit guard below: after a flush the editor has nothing unsaved
  // to clobber, so the replaced content must be reloaded even while focused
  // (#345). Consumed once, then cleared.
  let pendingExternalReload = false

  $effect(() => {
    const key = blocksContentKey(blocks)
    if (!editorInstance || editorInstance.isDestroyed) return
    // Fingerprint id+text so restore/replace with the same IDs still applies.
    // A same-content echo (flush of the body already on screen) must not
    // consume pendingExternalReload — that one-shot is for the restored body.
    if (key === lastSyncedBlocksKey) return
    // Don't clobber the editor's content while the user is actively editing.
    // The editor is the source of truth until blur; external updates wait —
    // unless a flush-then-replace sequence (pendingExternalReload) has made
    // the in-memory buffer stale and safe to overwrite.
    if (isFocused && !pendingExternalReload) return
    pendingExternalReload = false
    lastSyncedBlocksKey = key

    suppressUpdate = true
    editorInstance.commands.setContent(blocksToDoc(blocks), {
      emitUpdate: false
    })
    suppressUpdate = false
    // Reset save state — new content loaded, nothing is dirty (#167).
    autosave.markClean()
  })

  // --- Auto-save (debounced, config-driven, same contract as legacy) --------

  let unsavedChanges = $state(false)

  const autosave = new AutosaveManager({
    getEditor: () => editorInstance,
    getNotebook: () => notebook,
    getSection: () => section,
    getPage: () => page,
    getDelay: () =>
      Math.max(settings.config?.editor?.auto_save_delay_ms ?? 500, 50),
    onUpdate: (blocks) => onUpdate(blocks),
    onStateChange: (dirty) => {
      unsavedChanges = dirty
    },
    onSaveStateChange: (state) => onSaveStateChange?.(state)
  })

  function triggerAutoSave(): void {
    autosave.trigger()
  }
  function flushPendingSave(): Promise<void> {
    return autosave.flush()
  }

  // Register with the editor reconciliation registry so out-of-band writers
  // (global replace) can flush this editor's unsaved buffer before writing
  // and force a reload after (#345). Re-registers when the page triple
  // changes (e.g. a rename) without re-subscribing on every dirty toggle
  // (isDirty reads state lazily inside the closure, not in the effect body).
  $effect(() => {
    const nb = notebook,
      sec = section,
      pg = page
    const key = `${nb}\x00${sec}\x00${pg}`
    return registerEditor({
      key,
      isDirty: () => unsavedChanges,
      flush: async () => {
        await autosave.flush()
        return !unsavedChanges
      },
      getMarkdown: () => {
        if (!editorInstance || editorInstance.isDestroyed) return null
        const blocks = docToBlocks(editorInstance.getJSON())
        return blocks
          .map(
            (b) =>
              '  '.repeat(b.depth || 0) + (b.raw_text || b.clean_text || '')
          )
          .join('\n')
      },
      forceExternalReload: () => {
        pendingExternalReload = true
      },
      clearExternalReload: () => {
        pendingExternalReload = false
      },
      setProposedEdit: (opts) => {
        if (!editorInstance || editorInstance.isDestroyed) return false
        return editorInstance.commands.setProposedEdit(opts)
      },
      clearProposedEdit: () => {
        if (!editorInstance || editorInstance.isDestroyed) return
        editorInstance.commands.rejectProposedEdit()
      },
      hasProposal: () => {
        if (!editorInstance || editorInstance.isDestroyed) return false
        return hasProposedEdit(editorInstance)
      },
      acceptProposedEdit: () => {
        if (!editorInstance || editorInstance.isDestroyed) return false
        return editorInstance.commands.acceptProposedEdit()
      },
      verifySelectionText: (from: number, to: number, expected: string) => {
        if (!editorInstance || editorInstance.isDestroyed) return false
        try {
          const text = editorInstance.state.doc.textBetween(from, to, '\n')
          return text === expected
        } catch {
          return false
        }
      }
    })
  })

  // --- Slash menu -----------------------------------------------------------

  const slash = createSlashMenu({
    getEditor: () => editorInstance,
    onOpenMathPopover: popovers.setMathPopover,
    onOpenTableSizePicker: popovers.openTableSizePicker,
    onOpenColorPicker: popovers.openColorPickerPopover,
    onShowEmbedPicker: popovers.openEmbedPicker,
    onShowTemplatePicker: templateInsert.openTemplatePicker
  })

  // --- Focus lock (reuses the #38 TTL-lease bindings) -----------------------

  const focusLock = new FocusLockManager({
    getNotebook: () => notebook,
    getSection: () => section,
    getPage: () => page,
    getEditor: () => editorInstance,
    onBlockFocus: (id, ancestors) => onBlockFocus?.(id, ancestors)
  })

  function acquireFocus(): void {
    void focusLock.acquire()
  }
  function releaseFocus(): void {
    void focusLock.release()
  }
  function startHeartbeat(): void {
    focusLock.startHeartbeat()
  }
  function stopHeartbeat(): void {
    focusLock.stopHeartbeat()
  }
  function notifyFocus(): void {
    focusLock.notifyFocus()
  }

  // Context Menu state — the host owns the opener (`handleContextMenu` on the
  // host wrapper) and the open payload; EditorContextMenu owns rendering,
  // keyboard nav, and the clipboard action handlers.
  let contextMenu = $state<{
    x: number
    y: number
    activeBlockId?: string
    activeBlockNode?: ProseMirrorNode
  } | null>(null)

  function handleContextMenu(e: MouseEvent): void {
    if (!editorInstance || editorInstance.isDestroyed) return
    e.preventDefault()

    // Move editor cursor to the click location if the click is outside the current selection.
    const pos = editorInstance.view.posAtCoords({
      left: e.clientX,
      top: e.clientY
    })
    if (pos) {
      const { selection } = editorInstance.state
      if (pos.pos < selection.from || pos.pos > selection.to) {
        editorInstance.commands.setTextSelection(pos.pos)
      }
    }

    // Resolve the active block and its unique ID
    let activeBlockId: string | undefined
    let activeBlockNode: ProseMirrorNode | null = null
    const active = findActiveBlock(editorInstance)
    if (active) {
      activeBlockId = active.node.attrs.id
      activeBlockNode = active.node
    }

    // Viewport collision boundary adjustment to prevent offscreen rendering
    const { left: x, top: y } = clampToViewport(
      { x: e.clientX, y: e.clientY, width: 220, height: 320 },
      { width: window.innerWidth, height: window.innerHeight }
    )

    contextMenu = {
      x,
      y,
      activeBlockId,
      activeBlockNode: activeBlockNode ?? undefined
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<!-- Contextmenu listener is on the outer host wrapper to handle editor-wide custom right-click menus -->
<div
  class="tiptap-editor-host"
  class:focused={isFocused}
  class:focus-mode={focusModeEnabled}
  oncontextmenu={handleContextMenu}
>
  {#if editorReady}
    <PluginNoteBanners />
    <FormattingFirstRunTip
      dismissed={formatTipDismissed}
      onDismiss={dismissFormatTip}
    />
    <SelectionBubble
      editor={editorInstance}
      {activeMarks}
      {selectionEmpty}
      {selectionCoords}
      {isDark}
      colorEnabled={settings.config?.ui?.formatting?.color_enabled !== false}
    />
    {#if spellcheckMenu.spellMenu && editorInstance}
      <SpellcheckMenu
        editor={editorInstance}
        word={spellcheckMenu.spellMenu.word}
        range={spellcheckMenu.spellMenu.range}
        anchor={spellcheckMenu.spellMenu.anchor}
        onClose={() => (spellcheckMenu.spellMenu = null)}
      />
    {/if}
    {#if cursorInTable && editorInstance}
      <TableContextToolbar editor={editorInstance} />
    {/if}
    {#if $editorStore}
      <EditorContent editor={$editorStore} />
    {/if}
  {/if}

  {#if contextMenu && editorInstance}
    <EditorContextMenu
      menu={contextMenu}
      editor={editorInstance}
      {selectionEmpty}
      {isLastBlock}
      onClose={() => (contextMenu = null)}
    />
  {/if}

  <!-- Unsaved changes & word count are managed by the parent VirtualScrollContainer floating badge -->
  {#if slash.showSlashMenu}
    {@const coords = slash.slashCoords()}
    {#if coords}
      <CommandPalette
        style="position: fixed; left: {coords.left}px; top: {coords.top}px;"
        query={slash.slashQuery}
        textboxEl={editorInstance?.view.dom ?? null}
        onSelect={slash.handleSlashSelect}
        exclude={mathEnabled ? [] : ['math']}
        onClose={slash.dismiss}
      />
    {/if}
  {/if}
  <!-- Visually-hidden live region: announces typeahead open/close + match count
       for screen-reader users (both @-mention and %-metadata popups). -->
  <div
    aria-live="polite"
    style="position:absolute;width:1px;height:1px;padding:0;margin:-1px;overflow:hidden;clip:rect(0,0,0,0);white-space:nowrap;border:0"
  >
    {suggests.suggestStatus}
  </div>
  {#if suggests.meta.popup}
    {@const c = suggestPopupCoords(suggests.meta.popup.ctx.from, 260)}
    {#if c}
      <SuggestPopup
        items={suggests.meta.popup.items.map((item) => ({
          id: item.key,
          label: item.key,
          hint: item.description
        }))}
        selected={suggests.meta.popup.selected}
        coords={c}
        emptyLabel="No matching metadata keys"
        ariaLabel="Task metadata"
        className="meta-suggest"
        onPick={(i) => {
          const item = suggests.meta.popup?.items[i]
          if (item) suggests.meta.pick(item.key)
        }}
        onHover={(i) => {
          if (suggests.meta.popup) suggests.meta.popup.selected = i
        }}
      />
    {/if}
  {/if}
  {#if suggests.mention.popup}
    {@const c = suggestPopupCoords(suggests.mention.popup.ctx.from, 220)}
    {#if c}
      <SuggestPopup
        items={suggests.mention.popup.items.map((item) => ({
          id: item,
          label: `@${item}`
        }))}
        selected={suggests.mention.popup.selected}
        coords={c}
        emptyLabel="No matching owners"
        ariaLabel="Mention an owner"
        className="mention-suggest"
        onPick={(i) => {
          const item = suggests.mention.popup?.items[i]
          if (item) suggests.mention.pick(item)
        }}
        onHover={(i) => {
          if (suggests.mention.popup) suggests.mention.popup.selected = i
        }}
      />
    {/if}
  {/if}
  {#if suggests.blockRef.popup}
    {@const c = suggestPopupCoords(suggests.blockRef.popup.ctx.from, 360)}
    {#if c}
      <SuggestPopup
        items={suggests.blockRef.popup.items.map((item) => ({
          id: `${item.source || 'vault'}:${item.id}`,
          label: item.clean_content || 'Untitled block',
          hint: `${suggests.blockRef.blockSourceLabel(item.source)} · ${[
            item.notebook,
            item.section,
            item.page
          ]
            .filter(Boolean)
            .join(' / ')}`
        }))}
        selected={suggests.blockRef.popup.selected}
        coords={c}
        emptyLabel={suggests.blockRef.popup.error
          ? 'Block search unavailable'
          : suggests.blockRef.popup.searching
            ? 'Searching blocks…'
            : suggests.blockRef.popup.ctx.query.trim()
              ? 'No blocks found'
              : 'Type to search for a block…'}
        ariaLabel="Reference a block"
        className="block-ref-suggest"
        onPick={(i) => {
          const item = suggests.blockRef.popup?.items[i]
          if (item) suggests.blockRef.pick(item.id)
        }}
        onHover={(i) => {
          if (suggests.blockRef.popup) suggests.blockRef.popup.selected = i
        }}
      />
    {/if}
  {/if}
  {#if suggests.tag.popup}
    {@const c = suggestPopupCoords(suggests.tag.popup.ctx.from, 280)}
    {#if c}
      <SuggestPopup
        items={suggests.tag.popup.items.map((item) => ({
          id: item.path,
          label: `#${item.path}`,
          hint: `${item.count} ${item.count === 1 ? 'use' : 'uses'}`
        }))}
        selected={suggests.tag.popup.selected}
        coords={c}
        emptyLabel={suggests.tag.tagsLoadError
          ? 'Tag suggestions unavailable'
          : suggests.tag.tagsLoading
            ? 'Loading tags…'
            : 'No matching tags'}
        ariaLabel="Insert a tag"
        className="tag-suggest"
        onPick={(i) => {
          const item = suggests.tag.popup?.items[i]
          if (item) suggests.tag.pick(item.path)
        }}
        onHover={(i) => {
          if (suggests.tag.popup) suggests.tag.popup.selected = i
        }}
      />
    {/if}
  {/if}
  {#if suggests.pageLink.popup}
    {@const c = suggestPopupCoords(suggests.pageLink.popup.ctx.from, 340)}
    {#if c}
      {#snippet pageLinkFooter()}
        <div class="page-link-alias-footer">
          {#if suggests.pageLink.popup?.resolving}
            <div class="page-link-progress" role="status">
              <span class="page-link-spinner" aria-hidden="true"></span>
              Resolving {suggests.pageLink.popup.resolvingItem?.page ?? 'page'}…
            </div>
          {:else if suggests.pageLink.popup?.error}
            <div class="page-link-retry" role="alert">
              <span>
                {suggests.pageLink.popup.error === 'search'
                  ? 'Couldn’t refresh suggestions.'
                  : 'Couldn’t insert this link.'}
              </span>
              <button type="button" onclick={suggests.pageLink.retry}>
                {suggests.pageLink.popup.error === 'search'
                  ? 'Retry search'
                  : 'Retry link'}
              </button>
            </div>
          {:else if suggests.pageLink.popup?.searching && suggests.pageLink.popup.items.length}
            <div class="page-link-progress" role="status">
              <span class="page-link-spinner" aria-hidden="true"></span>
              Refreshing suggestions…
            </div>
          {/if}
          <button
            type="button"
            class="page-link-alias-toggle"
            aria-pressed={suggests.pageLink.popup?.aliasEnabled ?? false}
            disabled={suggests.pageLink.popup?.resolving ?? false}
            onclick={() => {
              if (!suggests.pageLink.popup) return
              const selected =
                suggests.pageLink.popup.items[suggests.pageLink.popup.selected]
              suggests.pageLink.popup.aliasEnabled =
                !suggests.pageLink.popup.aliasEnabled
              if (
                suggests.pageLink.popup.aliasEnabled &&
                !suggests.pageLink.popup.alias
              ) {
                suggests.pageLink.popup.alias = selected?.page ?? ''
              }
            }}
          >
            <span class="material-symbols-outlined" aria-hidden="true"
              >label</span
            >
            Use display alias
          </button>
          {#if suggests.pageLink.popup?.aliasEnabled}
            <label class="page-link-alias-field">
              <span>Alias</span>
              <input
                value={suggests.pageLink.popup.alias}
                disabled={suggests.pageLink.popup.resolving}
                oninput={(event) => {
                  if (suggests.pageLink.popup) {
                    suggests.pageLink.popup.alias = normalizePageLinkAlias(
                      event.currentTarget.value
                    )
                  }
                }}
                onkeydown={suggests.pageLink.aliasKeydown}
                onfocus={(event) => event.currentTarget.select()}
                placeholder="Link text"
                aria-label="Page link display alias"
                aria-haspopup="listbox"
              />
            </label>
          {/if}
        </div>
      {/snippet}
      <SuggestPopup
        items={suggests.pageLink.popup.resolving
          ? []
          : suggests.pageLink.popup.items.map((item) => ({
              id: `${item.source ?? ''}:${item.notebook}/${item.section}/${item.page}`,
              label: item.page || 'Untitled page',
              hint: [
                pageLinkSourceLabel(item.source),
                [item.notebook, item.section].filter(Boolean).join(' / ')
              ]
                .filter(Boolean)
                .join(' · ')
            }))}
        selected={suggests.pageLink.popup.selected}
        coords={c}
        emptyLabel={suggests.pageLink.popup.resolving
          ? 'Resolving page link…'
          : suggests.pageLink.popup.error === 'search'
            ? 'Page suggestions unavailable'
            : !suggests.pageLink.hasEnoughQuery(
                  suggests.pageLink.popup.ctx.query
                )
              ? 'Type at least 2 characters'
              : suggests.pageLink.popup.searching
                ? 'Searching pages…'
                : 'No matching pages'}
        ariaLabel="Link to a page"
        className="page-link-suggest"
        footer={pageLinkFooter}
        onPick={(i) => {
          const item = suggests.pageLink.popup?.items[i]
          if (item) void suggests.pageLink.pick(item)
        }}
        onHover={(i) => {
          if (suggests.pageLink.popup && !suggests.pageLink.popup.resolving) {
            suggests.pageLink.popup.selected = i
          }
        }}
      />
    {/if}
  {/if}
  {#if popovers.showLinkInput && popovers.linkInputCoords}
    <div
      class="link-input-popover"
      style="left:{popovers.linkInputCoords.left}px; top:{popovers
        .linkInputCoords.top}px"
      role="dialog"
      aria-label="Insert link URL"
    >
      <input
        type="url"
        class="link-input"
        placeholder="https://"
        bind:value={popovers.linkInputValue}
        onkeydown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            popovers.applyLinkInput()
          } else if (e.key === 'Escape') {
            e.preventDefault()
            popovers.cancelLinkInput()
          }
        }}
        onblur={popovers.applyLinkInput}
      />
    </div>
  {/if}
  {#if popovers.showColorPicker && popovers.colorPickerCoords}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <div
      class="color-picker-popover"
      style="left:{popovers.colorPickerCoords.left}px; top:{popovers
        .colorPickerCoords.top}px"
      role="menu"
      tabindex="-1"
      aria-label={popovers.colorPickerMarkType === 'textColor'
        ? 'Text color'
        : 'Background color'}
      onclick={(e) => e.stopPropagation()}
    >
      <button
        type="button"
        class="cp-swatch cp-reset"
        onclick={() => popovers.applyColorFromPopover(null)}
        aria-label="No color"
      >
        <span
          class="material-symbols-outlined"
          style="font-size:16px"
          aria-hidden="true">format_color_reset</span
        >
      </button>
      <div class="cp-palette-sections">
        <div class="cp-grid" role="group" aria-label="Theme colors">
          {#each colorPalette.theme as entry (entry.id)}
            <button
              type="button"
              class="cp-swatch"
              style="background-color: {resolveColor(entry, isDark)}"
              aria-label={entry.label}
              role="menuitem"
              onclick={() =>
                popovers.applyColorFromPopover(resolveColor(entry, isDark))}
            >
            </button>
          {/each}
        </div>
        <div
          class="cp-divider"
          role="separator"
          aria-orientation="horizontal"
        ></div>
        <div class="cp-grid" role="group" aria-label="Standard colors">
          {#each colorPalette.standard as entry (entry.id)}
            <button
              type="button"
              class="cp-swatch"
              style="background-color: {resolveColor(entry, isDark)}"
              aria-label={entry.label}
              role="menuitem"
              onclick={() =>
                popovers.applyColorFromPopover(resolveColor(entry, isDark))}
            >
            </button>
          {/each}
        </div>
      </div>
      <label class="cp-custom-row">
        <input
          type="color"
          class="cp-custom-input"
          onchange={(e) =>
            popovers.applyColorFromPopover(e.currentTarget.value)}
          aria-label="Custom color"
        />
      </label>
    </div>
  {/if}
  {#if popovers.showTableSizePicker && popovers.tableSizeCoords}
    <TableSizePicker
      anchor={popovers.tableSizeCoords}
      onConfirm={popovers.confirmTableSize}
      onCancel={popovers.cancelTableSize}
    />
  {/if}
  {#if popovers.mathPopover}
    <MathLatexPopover
      latex={popovers.mathPopover.latex}
      displayMode={popovers.mathPopover.displayMode}
      coords={popovers.mathPopover.coords}
      onCommit={popovers.commitMathPopover}
      onCancel={popovers.cancelMathPopover}
    />
  {/if}
</div>

{#if templateInsert.showTemplatePicker}
  <TemplatePicker
    mode="insert"
    onClose={() => (templateInsert.showTemplatePicker = false)}
    onInsertBlocks={templateInsert.handleTemplateInsert}
  />
{/if}

{#if templateInsert.pendingTemplateBlocks}
  <ChoiceDialog
    title="Insert template?"
    message="This page already has content. Insert the template at the cursor, or append it at the end?"
    primaryLabel="Insert at cursor"
    secondaryLabel="Append to end"
    returnFocusTo={templateInsert.templateInsertReturnFocus}
    dataTestId="template-insert-choice"
    onPrimary={templateInsert.confirmTemplateAtCursor}
    onSecondary={templateInsert.confirmTemplateAppend}
    onCancel={templateInsert.cancelTemplateInsert}
  />
{/if}

{#if popovers.showEmbedPicker}
  <BlockPickerModal
    onPick={popovers.handleEmbedPick}
    onClose={popovers.closeEmbedPicker}
  />
{/if}

<style>
  .page-link-alias-footer {
    display: grid;
    gap: 7px;
  }

  .page-link-progress,
  .page-link-retry {
    display: flex;
    align-items: center;
    gap: 7px;
  }

  .page-link-retry {
    justify-content: space-between;
    color: var(--color-text-primary);
  }

  .page-link-retry button {
    flex: none;
    padding: 3px 7px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 5px;
    background: var(--color-surface-raised, var(--color-surface-popover));
    color: var(--color-accent-primary-start);
    font: inherit;
    font-weight: 600;
    cursor: pointer;
  }

  .page-link-spinner {
    width: 11px;
    height: 11px;
    border: 2px solid color-mix(in srgb, currentColor 25%, transparent);
    border-top-color: currentColor;
    border-radius: 50%;
    animation: page-link-spin 0.7s linear infinite;
  }

  @keyframes page-link-spin {
    to {
      transform: rotate(1turn);
    }
  }

  .page-link-alias-toggle {
    display: inline-flex;
    align-items: center;
    width: max-content;
    gap: 5px;
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: pointer;
  }

  .page-link-alias-toggle[aria-pressed='true'] {
    color: var(--color-accent-primary-start);
  }

  .page-link-alias-toggle:disabled,
  .page-link-alias-field input:disabled {
    opacity: 0.55;
    cursor: wait;
  }

  .page-link-alias-toggle .material-symbols-outlined {
    font-size: 15px;
  }

  .page-link-alias-toggle:focus-visible,
  .page-link-alias-field input:focus-visible,
  .page-link-retry button:focus-visible {
    outline: 2px solid var(--color-accent-primary-start);
    outline-offset: 2px;
  }

  .page-link-alias-field {
    display: grid;
    grid-template-columns: auto 1fr;
    align-items: center;
    gap: 8px;
  }

  .page-link-alias-field input {
    min-width: 0;
    padding: 5px 7px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 5px;
    background: var(--color-surface-raised, var(--color-surface-popover));
    color: var(--color-text-primary);
    font: inherit;
  }

  @media (prefers-reduced-motion: reduce) {
    .page-link-spinner {
      animation: none;
    }
  }

  /* Readable measure centered in the pane (#841). Rhythm/list density live
     in index.css under .ProseMirror so all editor surfaces share one source. */
  .tiptap-editor-host {
    width: 100%;
    max-width: var(--editor-measure, 70ch);
    margin-inline: auto;
  }

  .tiptap-editor-host :global(.ProseMirror) {
    min-height: 22px;
    outline: none;
  }

  /* Focus mode (#168 Phase 3): dim all top-level blocks except the one with
     cursor focus. The Focus extension adds .has-focus to the active block. */
  .focus-mode :global(.ProseMirror > div:not(.has-focus)) {
    opacity: 0.3;
    transition: opacity 0.2s;
  }
  @media (prefers-reduced-motion: reduce) {
    .focus-mode :global(.ProseMirror > div:not(.has-focus)) {
      transition: none;
    }
  }

  .link-input-popover {
    position: fixed;
    z-index: 100;
    margin-top: 4px;
    padding: 4px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
  }

  .link-input {
    width: 240px;
    padding: 4px 8px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 6px;
    background: var(--color-surface-popover);
    color: var(--color-text-primary);
    font-size: 0.8rem;
    outline: none;
  }

  .link-input:focus {
    border-color: var(--color-accent-primary-glow);
  }

  .color-picker-popover {
    position: fixed;
    z-index: 100;
    margin-top: 4px;
    padding: 6px;
    border-radius: 8px;
    background: var(--color-surface-popover);
    border: 1px solid var(--color-surface-popover-border);
    box-shadow: 0 4px 16px rgba(0, 0, 0, 0.4);
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-width: 200px;
  }

  .cp-palette-sections {
    display: flex;
    flex-direction: column;
  }

  .cp-divider {
    height: 1px;
    background: var(--color-surface-popover-border);
    margin: 4px 0;
  }

  .cp-grid {
    display: grid;
    grid-template-columns: repeat(6, 1fr);
    gap: 3px;
    padding: 4px 0;
  }

  .cp-swatch {
    width: 24px;
    height: 24px;
    border: 2px solid transparent;
    border-radius: 5px;
    cursor: pointer;
    padding: 0;
    transition: border-color 0.1s;
  }

  .cp-swatch:hover {
    border-color: var(--color-text-primary);
  }

  .cp-reset {
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    color: var(--color-text-muted);
    align-self: flex-start;
  }

  .cp-custom-row {
    display: flex;
    justify-content: center;
    margin-top: 2px;
  }

  .cp-custom-input {
    width: 28px;
    height: 22px;
    border: 1px solid var(--color-surface-popover-border);
    border-radius: 4px;
    background: transparent;
    cursor: pointer;
    padding: 0;
  }

  @media (prefers-reduced-motion: reduce) {
    .cp-swatch {
      transition: none;
    }
  }
</style>
