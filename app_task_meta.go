package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
	"silt/backend/plugins"
)

// taskTagRegex mirrors backend/db.tagRegex (the canonical hashtag detector
// shared by the indexer). Kept as a local copy so tag byte-surgery in this
// file does not depend on a package-private symbol; the pattern is fixed by
// SPECS §4.2 and any change must land in both places in lockstep. The two
// are pinned equal by TestTaskTagRegex_MirrorsIndexer.
var taskTagRegex = regexp.MustCompile(`\B#([a-zA-Z][a-zA-Z0-9_/-]*)`)

// SetTaskOwner rewrites the [owner:: NAME] inline token on a task block,
// the assignee field surfaced by the unified TaskEditDrawer (#412). Pass an
// empty string to clear the token. The renderer re-emits the token from
// ParsedBlock.Owner (writer.go ~:1210, omit-when-empty), so a parse → render
// round trip is byte-stable and never produces two competing tokens.
//
// Follows the canonical write chain (same as SetTaskRecurrence):
// LockBlockWrite -> LockFileWrite -> ReadFile -> ParseFileContent -> mutate
// block.Owner -> RenderFileContent -> WriteFileAtomic -> re-parse ->
// IndexFileBlocks -> emit block:changed.
func (a *App) SetTaskOwner(blockID, owner string) error {
	return a.setTaskOwner(blockID, owner)
}

// PluginSetTaskOwner is the plugin-SDK wrapper for SetTaskOwner, gated by the
// standard capability + session checks (SPECS §8.3 — plugins go through
// PluginContext, never direct wailsjs bindings). Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskOwner(pluginID, sessionToken, blockID, owner string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskOwner(blockID, owner); err != nil {
		return false, err
	}
	return true, nil
}

// mutateTaskBlock is the canonical task write-chain shared by the four
// single-field setters (SetTaskOwner/Priority/Tags/Title): locate the block,
// sanity-check its file metadata, then under LockBlockWrite+LockFileWrite
// read -> parse -> apply `mutate` -> render -> WriteFileAtomic -> re-parse ->
// IndexFileBlocks -> emit block:changed. `label` prefixes diagnostic log and
// parse-error messages so failures stay attributable. Callers retain any
// input validation (empty-title guard, tag dedupe) BEFORE calling; this
// helper owns only the shared write path so a future cross-cutting guard
// (e.g. focus-lock, #444) lands in exactly one place.
func (a *App) mutateTaskBlock(blockID, label string, mutate func(*parser.ParsedBlock)) error {
	a.vaultMu.RLock()
	defer a.vaultMu.RUnlock()
	if a.db == nil {
		return fmt.Errorf("vault database not loaded")
	}
	a.wg.Add(1)
	defer a.wg.Done()

	var loc db.BlockLocation
	err := a.coordinator.WithDBReadResult(func() error {
		var e error
		loc, e = a.db.GetBlockLocation(blockID)
		return e
	})
	if err != nil {
		return fmt.Errorf("block %s not found in SQLite: %w", blockID, err)
	}
	notebook, section, page, blockType := loc.Notebook, loc.Section, loc.Page, loc.BlockType
	if blockType != string(parser.BlockTask) {
		return fmt.Errorf("block %s is not a task", blockID)
	}

	safeNotebook := sanitizePathSegment(notebook)
	safeSection := sanitizePathSegment(section)
	safePage := sanitizePathSegment(page)
	if safeNotebook == "" || safePage == "" {
		return fmt.Errorf("invalid file metadata for block %s", blockID)
	}
	notebookDir, err := a.resolveNotebookDir(safeNotebook, loc.Source)
	if err != nil {
		return fmt.Errorf("resolve notebook dir for block %s: %w", blockID, err)
	}
	filePath := filepath.Join(notebookDir, safeSection, safePage+".md")
	if !isPathWithinRoot(filePath, notebookDir) {
		return fmt.Errorf("resolved file path %q escapes notebook root %q", filePath, notebookDir)
	}

	var writeErr error
	var emitFileDate string
	didWrite := false
	a.coordinator.LockBlockWrite(blockID, func() {
		a.coordinator.LockFileWrite(filePath, func() {
			contentBytes, err := os.ReadFile(filePath)
			if err != nil {
				writeErr = err
				return
			}
			fileDate := fileOrDefaultDate(filePath)
			parsedBlocks, meta, _, _, parseErr := parser.ParseFileContent(string(contentBytes), safeNotebook, safeSection, safePage, fileDate, a.spacesPerTab)
			if parseErr != nil {
				writeErr = fmt.Errorf("failed to parse file for %s: %w", label, parseErr)
				return
			}
			found := false
			for i := range parsedBlocks {
				if parsedBlocks[i].ID == blockID && parsedBlocks[i].Type == parser.BlockTask {
					mutate(&parsedBlocks[i])
					found = true
					break
				}
			}
			if !found {
				writeErr = fmt.Errorf("block %s not found in file %s", blockID, filePath)
				return
			}

			frontmatter, body := parser.SplitFrontmatter(string(contentBytes))
			if frontmatter == "" {
				fmDate := meta.Date
				if fmDate == "" {
					fmDate = fileDate
				}
				frontmatter = fmt.Sprintf("---\nnotebook: %s\nsection: %s\npage: %s\ndate: %s\ntags: []\n---\n", strconv.Quote(safeNotebook), strconv.Quote(safeSection), strconv.Quote(safePage), strconv.Quote(fmDate))
				body = string(contentBytes)
			}
			newContent := parser.RenderFileContent(parsedBlocks, body, frontmatter, a.spacesPerTab)
			a.tracker.RegisterWrite(filePath)
			if err := parser.WriteFileAtomic(filePath, []byte(newContent)); err != nil {
				writeErr = err
				return
			}
			didWrite = true

			blocks, remeta, _, _, err := parser.ParseFileContent(newContent, meta.Notebook, meta.Section, meta.Page, meta.Date, a.spacesPerTab)
			if err == nil {
				var idxErr error
				a.coordinator.WithDBWrite(func() {
					idxErr = a.db.IndexFileBlocks(loc.Source, remeta.Notebook, remeta.Section, remeta.Page, blocks, remeta.Tags, remeta.Warnings...)
				})
				if idxErr != nil {
					log.Printf("%s: IndexFileBlocks failed: %v", label, idxErr)
				}
				for _, b := range blocks {
					if b.ID == blockID {
						emitFileDate = b.FileDate
					}
				}
			} else {
				log.Printf("%s: re-parse of rendered content failed (file written, index stale until next scan): %v", label, err)
			}
			if emitFileDate == "" {
				emitFileDate = fileDate
			}
		})
	}) // LockBlockWrite
	if writeErr != nil {
		return writeErr
	}
	if didWrite {
		a.emitBlockChanged(blockID, safeNotebook, safeSection, safePage, emitFileDate)
	}
	return nil
}

// setTaskOwner is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain.
func (a *App) setTaskOwner(blockID, owner string) error {
	return a.mutateTaskBlock(blockID, "SetTaskOwner", func(b *parser.ParsedBlock) { b.Owner = owner })
}

// SetTaskOrder rewrites the [order:: N] inline token on a task block (#426).
// Pass 0 to clear the token (the renderer omits it when ManualOrder == 0,
// writer.go ~:1258). A negative value is rejected up front so a UI glitch
// can't stamp an off-by-one into the file. Follows the canonical write
// chain (same as SetTaskOwner); the row mapper caches the new value into
// tasks.manual_order so the next query sees it without re-parsing markdown.
func (a *App) SetTaskOrder(blockID string, order int) error {
	return a.setTaskOrder(blockID, order)
}

// PluginSetTaskOrder is the plugin-SDK wrapper for SetTaskOrder, gated by
// the standard capability + session checks. Mirrors PluginSetTaskOwner.
func (a *App) PluginSetTaskOrder(pluginID, sessionToken, blockID string, order int) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskOrder(blockID, order); err != nil {
		return false, err
	}
	return true, nil
}

// setTaskOrder is the shared core for the app-level and plugin-level entry
// points. Validates the input (negative order is a contract violation —
// the token is 1-based; 0 is the "unset" sentinel and clears the token)
// BEFORE entering the write chain so a rejection leaves the file untouched.
func (a *App) setTaskOrder(blockID string, order int) error {
	if order < 0 {
		return fmt.Errorf("task order must be >= 0 (got %d)", order)
	}
	return a.mutateTaskBlock(blockID, "SetTaskOrder", func(b *parser.ParsedBlock) { b.ManualOrder = order })
}

// SetTaskPriority rewrites the [priority:: N] inline token on a task block
// (#412). Pass 0 (or 3, the renderer's "normal" sentinel) to omit the token.
// The renderer re-emits from ParsedBlock.Priority (writer.go ~:1198, omit
// when 0 or 3), so the round trip is byte-stable.
//
// Follows the canonical write chain (same as SetTaskOwner).
func (a *App) SetTaskPriority(blockID string, priority int) error {
	return a.setTaskPriority(blockID, priority)
}

// PluginSetTaskPriority is the plugin-SDK wrapper for SetTaskPriority, gated
// by the standard capability + session checks. Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskPriority(pluginID, sessionToken, blockID string, priority int) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskPriority(blockID, priority); err != nil {
		return false, err
	}
	return true, nil
}

// setTaskPriority is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain.
func (a *App) setTaskPriority(blockID string, priority int) error {
	return a.mutateTaskBlock(blockID, "SetTaskPriority", func(b *parser.ParsedBlock) { b.Priority = priority })
}

// SetTaskTags rewrites the hashtag set on a task's CleanText (the prose),
// the tag field surfaced by the unified TaskEditDrawer (#412). Tags are
// #namespace/path hashtags living in the prose, NOT [key::] tokens — the
// two live in disjoint parts of the block (prose vs. re-emitted tokens) so
// tag surgery never touches the [key::] tokens, the checkbox marker, or the
// identity comment.
//
// Byte-preservation invariant: prose words, ((uuid)) block-refs, and tags
// that remain in the set stay byte-for-byte. Only added/removed hashtags
// change. The diff is computed against db.ExtractTags (the canonical
// indexer derivation), so the canonical names always match what the index
// sees:
//   - toRemove = current − new: each removed #tag substring is stripped from
//     CleanText with careful whitespace handling (one adjacent space is
//     removed along with the tag so no double/trailing/leading space is left).
//   - toAdd = new − current: appended as " #tag" at the end of the prose.
//
// After re-render + re-index, db.ExtractTags re-derives the new set into
// the tags table. Pass an empty slice (or nil) to clear all hashtags.
//
// Follows the canonical write chain (same as SetTaskOwner).
func (a *App) SetTaskTags(blockID string, tags []string) error {
	return a.setTaskTags(blockID, tags)
}

// PluginSetTaskTags is the plugin-SDK wrapper for SetTaskTags, gated by the
// standard capability + session checks. Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskTags(pluginID, sessionToken, blockID string, tags []string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskTags(blockID, tags); err != nil {
		return false, err
	}
	return true, nil
}

// setTaskTags is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain. The byte-surgery helpers (stripTagsFromCleanText,
// appendTagsToCleanText) preserve every other byte of CleanText.
func (a *App) setTaskTags(blockID string, tags []string) error {
	// Normalize (drop empties, de-dupe, strip leading #) before the write chain.
	newTags := dedupeTags(tags)
	return a.mutateTaskBlock(blockID, "SetTaskTags", func(b *parser.ParsedBlock) {
		b.CleanText = rebuildTagSet(b.CleanText, newTags)
	})
}

// SetTaskTitle rewrites the prose portion of a task's CleanText, the "title"
// field surfaced by the unified TaskEditDrawer (#412). The prose is what the
// user reads as the task description; hashtags (#namespace/path) and
// ((uuid)) block-refs live alongside the prose in CleanText and MUST be
// preserved across the rewrite.
//
// Byte-preservation invariant: every hashtag and every ((uuid)) ref in the
// original CleanText is preserved verbatim in the result; only the prose
// portion changes. The checkbox marker, all [key::] tokens, and the
// block-identity comment are NOT in CleanText (the renderer re-emits them
// from ParsedBlock fields), so they are safe by construction.
//
// Follows the canonical write chain (same as SetTaskOwner).
func (a *App) SetTaskTitle(blockID, title string) error {
	return a.setTaskTitle(blockID, title)
}

// PluginSetTaskTitle is the plugin-SDK wrapper for SetTaskTitle, gated by the
// standard capability + session checks. Mirrors PluginSetTaskDueDate.
func (a *App) PluginSetTaskTitle(pluginID, sessionToken, blockID, title string) (bool, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return false, err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return false, err
	}
	if err := a.setTaskTitle(blockID, title); err != nil {
		return false, err
	}
	return true, nil
}

// setTaskTitle is the shared core for the app-level and plugin-level entry
// points. Delegates the shared write-chain to mutateTaskBlock; see that
// method for the chain. The byte-surgery helper (replaceTitleInCleanText)
// tokenizes CleanText into hashtags, block-refs, and prose, then reassembles
// the new title + preserved tokens.
func (a *App) setTaskTitle(blockID, title string) error {
	// SDK contract guard: an empty/whitespace title would silently strip all
	// prose from the task. Reject before touching disk.
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("task title must not be empty")
	}
	return a.mutateTaskBlock(blockID, "SetTaskTitle", func(b *parser.ParsedBlock) {
		b.CleanText = replaceTitleInCleanText(b.CleanText, title)
	})
}

// dedupeTags drops empty entries and de-duplicates a tag slice while
// preserving first-occurrence order, and strips a single leading "#" so
// callers may pass "#work" or "work" interchangeably (covers plugin SDK
// callers, not just the drawer). Mirrors the indexer's dedupe so the
// rendered prose matches what db.ExtractTags will re-derive.
func dedupeTags(tags []string) []string {
	seen := make(map[string]bool, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimPrefix(t, "#")
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// rebuildTagSet applies the tag diff to a block's CleanText and returns the
// new prose. current tags are derived via db.ExtractTags (the canonical
// indexer derivation, including TrimRight of trailing "/" and "-" so
// "#work/" canonicalizes to "work"); the toRemove set is the complement of
// the new set against current, and toAdd is the complement of new against
// current. Stripping removes one adjacent space along with the #tag (leading
// space preferred; trailing space if there is no leading space) so no
// double/trailing/leading space is introduced. Adding appends " #tag" at the
// end of the prose. Prose words, ((uuid)) refs, and tags that remain in the
// set are byte-for-byte preserved.
func rebuildTagSet(cleanText string, newTags []string) string {
	currentTags := db.ExtractTags(cleanText)
	currentSet := make(map[string]bool, len(currentTags))
	for _, t := range currentTags {
		currentSet[t] = true
	}
	newSet := make(map[string]bool, len(newTags))
	for _, t := range newTags {
		newSet[t] = true
	}

	// toRemove: canonical names of the source tags that are not in the new
	// set. A source tag's canonical name is TrimRight(match, "/-") — same
	// rule db.ExtractTags applies — so the two stay in lockstep.
	toRemove := make(map[string]bool)
	for _, t := range currentTags {
		if !newSet[t] {
			toRemove[t] = true
		}
	}
	// toAdd: new tags not already present (in canonical form).
	var toAdd []string
	for _, t := range newTags {
		if !currentSet[t] {
			toAdd = append(toAdd, t)
		}
	}

	out := stripTagsFromCleanText(cleanText, toRemove)
	for _, t := range toAdd {
		if out == "" {
			out = "#" + t
		} else {
			out += " #" + t
		}
	}
	return out
}

// stripTagsFromCleanText removes every hashtag whose canonical name is in
// toRemove, taking one adjacent space with each removal (leading space
// preferred; trailing space if no leading space exists; bare removal when
// neither exists). Everything outside the removed spans is byte-for-byte
// preserved.
func stripTagsFromCleanText(text string, toRemove map[string]bool) string {
	if len(toRemove) == 0 {
		return text
	}
	matches := taskTagRegex.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text))
	last := 0
	for _, m := range matches {
		// m = [matchStart, matchEnd, groupStart, groupEnd]
		canonical := strings.TrimRight(text[m[2]:m[3]], "/-")
		if !toRemove[canonical] {
			continue
		}
		start, end := m[0], m[1]
		// Prefer the leading space so the more common " #tag " case collapses
		// to a single space rather than a trailing one. Fall back to the
		// trailing space for a tag at the start of the string. Bare removal
		// when neither side has a space.
		if start > 0 && text[start-1] == ' ' {
			start--
		} else if end < len(text) && text[end] == ' ' {
			end++
		}
		b.WriteString(text[last:start])
		last = end
	}
	b.WriteString(text[last:])
	return b.String()
}

// replaceTitleInCleanText swaps the prose portion of CleanText for newTitle
// while preserving every hashtag and every ((uuid)) block-ref verbatim. The
// tokenizer walks CleanText with taskTagRegex and parser.BlockRefRegex,
// collecting the literal source spans of each token; anything not in a token
// span is the prose and is replaced wholesale by newTitle. The result is
// "newTitle" + hashtag tokens + ref tokens joined by single spaces (the
// relative reordering after the title is acceptable per #412 as long as the
// SET of tokens is intact and no token is dropped or corrupted).
func replaceTitleInCleanText(cleanText, newTitle string) string {
	var tokens []string
	for _, m := range taskTagRegex.FindAllStringSubmatchIndex(cleanText, -1) {
		tokens = append(tokens, cleanText[m[0]:m[1]])
	}
	for _, m := range parser.BlockRefRegex.FindAllStringSubmatchIndex(cleanText, -1) {
		tokens = append(tokens, cleanText[m[0]:m[1]])
	}
	parts := make([]string, 0, 1+len(tokens))
	if newTitle != "" {
		parts = append(parts, newTitle)
	}
	parts = append(parts, tokens...)
	return strings.Join(parts, " ")
}
