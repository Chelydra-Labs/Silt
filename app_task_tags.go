package main

import (
	"regexp"
	"strings"

	"silt/backend/db"
	"silt/backend/parser"
)

// taskTagRegex mirrors backend/db.tagRegex (the canonical hashtag detector
// shared by the indexer). Kept as a local copy so tag byte-surgery does not
// depend on a package-private symbol; the pattern is fixed by SPECS §4.2 and
// any change must land in both places in lockstep. The two are pinned equal by
// TestTaskTagRegex_MirrorsIndexer.
var taskTagRegex = regexp.MustCompile(`\B#([a-zA-Z][a-zA-Z0-9_/-]*)`)

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
