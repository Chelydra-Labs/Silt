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
func SetFrontmatterField(content, key string, value any) (string, error) {
	if !validFrontmatterKey.MatchString(key) {
		return "", fmt.Errorf("invalid frontmatter key %q: must match %s", key, validFrontmatterKey.String())
	}

	fm, body := SplitFrontmatter(content)
	inner := innerFrontmatterLines(fm)
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

	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body, nil
}

// ClearFrontmatterField removes key (and any indented block-continuation
// lines that belong to it) from the leading frontmatter. It is idempotent:
// an absent key or a file with no frontmatter is returned verbatim so callers
// can re-run it safely after a no-op.
func ClearFrontmatterField(content, key string) (string, error) {
	if !validFrontmatterKey.MatchString(key) {
		return "", fmt.Errorf("invalid frontmatter key %q: must match %s", key, validFrontmatterKey.String())
	}

	fm, body := SplitFrontmatter(content)
	if fm == "" {
		return content, nil
	}

	inner := innerFrontmatterLines(fm)
	idx, _ := findKeyLine(inner, key)
	if idx < 0 {
		return content, nil
	}

	end := idx + 1
	for end < len(inner) && isContinuationLine(inner[end]) {
		end++
	}
	lines := append(append([]string{}, inner[:idx]...), inner[end:]...)

	return "---\n" + strings.Join(lines, "\n") + "\n---\n" + body, nil
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

// findKeyLine locates the top-level (non-indented) line whose prefix is
// `key:`. Typed-property keys always live at the top of the frontmatter, so
// indented lines are skipped to avoid matching nested children of a block
// value. It returns the line index and the leading indentation of the match
// (always empty in practice, but preserved for verbatim reconstruction).
// Returns -1 when the key is absent.
func findKeyLine(lines []string, key string) (int, string) {
	pattern := regexp.MustCompile("^" + regexp.QuoteMeta(key) + ":")
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
