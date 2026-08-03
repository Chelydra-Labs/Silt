package parser

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// validFrontmatterKey restricts editable keys to the YAML-safe identifier set
// used by typed properties. A key outside this set would either fail the
// line-based prefix match or produce ambiguous YAML, so it is rejected up
// front rather than silently corrupting the file.
var validFrontmatterKey = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// SetFrontmatterField writes key=value into the leading YAML frontmatter of
// content using a line-based edit, so every other frontmatter line —
// comments, unrelated keys, and their exact quoting — survives byte-for-byte.
// value is serialized with yamlInline, which forces every value onto a single
// line; this keeps the edit unambiguous because there is never a multi-line
// block to re-parse on the next write.
//
// An inline comment on the EDITED key's line (e.g. `rating: 5  # out of ten`)
// is not preserved across an edit, because the value is re-rendered from its
// parsed form; comments on all OTHER lines are preserved byte-for-byte.
func SetFrontmatterField(content, key string, value any) (string, error) {
	if !validFrontmatterKey.MatchString(key) {
		return "", fmt.Errorf("invalid frontmatter key %q: must match %s", key, validFrontmatterKey.String())
	}

	// SplitFrontmatter no longer strips the BOM; capture it from the original
	// content and re-prepend on write so the file's byte signature is stable.
	// nl preserves the dominant line ending so a CRLF file does not get mixed
	// to LF on the edited lines.
	hadBOM := strings.HasPrefix(content, "\uFEFF")
	nl := detectLineEnding(content)

	fm, body := SplitFrontmatter(content)
	// In the no-frontmatter path SplitFrontmatter returns body==content, which
	// still carries the BOM; strip it here since we re-prepend the BOM below
	// (avoids a double BOM when wrapping a bare body in a fresh fence).
	if hadBOM {
		body = strings.TrimPrefix(body, "\uFEFF")
	}
	inner := stripTrailingCR(innerFrontmatterLines(fm))
	if countTopLevelKeyOccurrences(inner, key) > 1 {
		return "", fmt.Errorf("frontmatter key %q appears more than once; refusing to edit an ambiguous file", key)
	}
	valueYAML := yamlInline(value)

	idx, indent := findKeyLine(inner, key)
	newLine := indent + key + ": " + valueYAML

	var lines []string
	if idx >= 0 {
		// Collapse the matched line and any indented block-continuation lines
		// that follow it so a multi-line block value (key:\n  - a\n  - b) is
		// fully replaced by the single inline line, not merged with it.
		end := idx + 1
		for end < len(inner) && isContinuationLine(inner[end]) {
			end++
		}
		lines = append(lines, inner[:idx]...)
		lines = append(lines, newLine)
		lines = append(lines, inner[end:]...)
	} else {
		// Absent key: append as the last key line, which lands just before the
		// closing fence when the block is re-emitted.
		lines = append(append([]string{}, inner...), newLine)
	}

	result := "---" + nl + strings.Join(lines, nl) + nl + "---" + nl + body
	if hadBOM {
		result = "\uFEFF" + result
	}
	return result, nil
}

// ClearFrontmatterField removes key (and any indented block-continuation
// lines that belong to it) from the leading frontmatter. It is idempotent:
// an absent key or a file with no frontmatter is returned verbatim so callers
// can re-run it safely after a no-op.
func ClearFrontmatterField(content, key string) (string, error) {
	if !validFrontmatterKey.MatchString(key) {
		return "", fmt.Errorf("invalid frontmatter key %q: must match %s", key, validFrontmatterKey.String())
	}

	// SplitFrontmatter no longer strips the BOM; capture it from the original
	// content and re-prepend on write. The early-return paths below return
	// content verbatim, so the file's BOM and line endings only change when a
	// key is actually removed.
	hadBOM := strings.HasPrefix(content, "\uFEFF")
	nl := detectLineEnding(content)

	fm, body := SplitFrontmatter(content)
	if fm == "" {
		return content, nil
	}

	inner := stripTrailingCR(innerFrontmatterLines(fm))
	if countTopLevelKeyOccurrences(inner, key) > 1 {
		return "", fmt.Errorf("frontmatter key %q appears more than once; refusing to edit an ambiguous file", key)
	}
	idx, _ := findKeyLine(inner, key)
	if idx < 0 {
		return content, nil
	}

	end := idx + 1
	for end < len(inner) && isContinuationLine(inner[end]) {
		end++
	}
	lines := append(append([]string{}, inner[:idx]...), inner[end:]...)

	result := "---" + nl + strings.Join(lines, nl) + nl + "---" + nl + body
	if hadBOM {
		result = "\uFEFF" + result
	}
	return result, nil
}

// innerFrontmatterLines returns the lines strictly between the --- fences.
// SplitFrontmatter returns fm as "---\n...\n---\n" (both fences plus the
// trailing newline); we slice off the opening fence (first element) and the
// closing fence + trailing empty (last two elements). When there was no
// frontmatter we return nil so callers can uniformly build a fresh block.
func innerFrontmatterLines(fm string) []string {
	if fm == "" {
		return nil
	}
	all := strings.Split(fm, "\n")
	// all[0] is the opening fence; the closing fence is the second-to-last
	// element and the final element is "" from the trailing "\n".
	if len(all) < 3 {
		return nil
	}
	return all[1 : len(all)-2]
}

// detectLineEnding returns the dominant line ending of s. CRLF files come from
// Windows editors and sync targets (Obsidian/OneDrive/Dropbox); re-emitting
// the frontmatter with the same ending avoids mixed-ending whole-file diffs.
func detectLineEnding(s string) string {
	if strings.Contains(s, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// stripTrailingCR removes a trailing "\r" from each line so the join with the
// detected ending re-emits a clean CRLF (or LF) instead of doubling up
// ("\r\r\n") or mixing CR-bearing lines with CR-free fences.
func stripTrailingCR(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSuffix(l, "\r")
	}
	return out
}

// keyLinePattern matches a top-level `key:` line, tolerating optional single
// or double quotes around the key and optional whitespace before the colon.
// YAML treats rating, 'rating', and "rating" as the SAME key (quotes are
// styling), and `rating :` is valid YAML with whitespace before the colon —
// both forms occur in frontmatter produced by external editors (Obsidian /
// sync targets). Without tolerating them, the matcher misses those lines, so
// findKeyLine returns -1 and SetFrontmatterField appends a duplicate (the old
// line survives as dead weight; last-wins masks it behaviorally but the file
// rots), and the duplicate guard (which uses this same pattern) never fires.
// Matching stays case-sensitive: YAML keys are case-sensitive (Title != title).
func keyLinePattern(key string) *regexp.Regexp {
	return regexp.MustCompile(`^['"]?` + regexp.QuoteMeta(key) + `['"]?\s*:`)
}

// findKeyLine locates the top-level (non-indented) line whose prefix is
// `key:`. Typed-property keys always live at the top of the frontmatter, so
// indented lines are skipped to avoid matching nested children of a block
// value. It returns the line index and the leading indentation of the match
// (always empty in practice, but preserved for verbatim reconstruction).
// Returns -1 when the key is absent.
func findKeyLine(lines []string, key string) (int, string) {
	pattern := keyLinePattern(key)
	for i, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if pattern.MatchString(line) {
			return i, ""
		}
	}
	return -1, ""
}

// countTopLevelKeyOccurrences counts non-indented lines whose prefix is
// `key:`. YAML is last-wins on duplicate keys, so findKeyLine (which returns
// the first match) would edit the wrong copy and the file would read back the
// untouched last value — silent corruption. Callers refuse such files.
func countTopLevelKeyOccurrences(lines []string, key string) int {
	pattern := keyLinePattern(key)
	n := 0
	for _, line := range lines {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue
		}
		if pattern.MatchString(line) {
			n++
		}
	}
	return n
}

// isContinuationLine reports whether line belongs to a multi-line block value
// under the key above it: a block sequence/map indents its entries, so any
// line beginning with a space or tab is a continuation. Blank lines are not
// continuations (they end the block), which preserves meaningful separators.
func isContinuationLine(line string) bool {
	return line != "" && (line[0] == ' ' || line[0] == '\t')
}

// yamlInline renders value as a single-line YAML scalar or flow collection.
// Single-line output is required because the editor works one line at a time:
// a value that spanned multiple lines would make "replace the key's line"
// ambiguous on the next edit. Strings are always quoted to match the existing
// scaffold convention and to avoid YAML's implicit-type coercion (e.g. "true"
// staying a string rather than becoming a boolean).
func yamlInline(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strconv.Quote(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(v), 'f', -1, 32)
	case []string:
		parts := make([]string, len(v))
		for i, s := range v {
			parts[i] = strconv.Quote(s)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case []any:
		parts := make([]string, len(v))
		for i, el := range v {
			// Elements are strings in practice; non-strings fall back to a
			// recursive inline render so the flow collection stays valid.
			if s, ok := el.(string); ok {
				parts[i] = strconv.Quote(s)
			} else {
				parts[i] = yamlInline(el)
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return strconv.Quote(fmt.Sprintf("%v", value))
	}
}
