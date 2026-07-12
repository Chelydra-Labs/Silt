package spellcheck

import (
	"strings"
	"unicode"
)

// ParseWordList parses a Hunspell personal-dict / cspell-style word list:
// one word per line, UTF-8, # comments (full-line or trailing), blank lines
// skipped. Words are trimmed and lowercased. Empty/control-only tokens dropped.
func ParseWordList(text string) []string {
	lines := strings.Split(text, "\n")
	seen := make(map[string]struct{}, len(lines))
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// cspell-tools directives
		if strings.HasPrefix(line, "cspell-tools:") {
			continue
		}
		// Strip trailing # comment
		if i := strings.Index(line, "#"); i >= 0 {
			line = strings.TrimSpace(line[:i])
			if line == "" {
				continue
			}
		}
		// cspell may put multiple words; take first token only for safety
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		w := strings.ToLower(strings.TrimSpace(fields[0]))
		if w == "" || !isValidWord(w) {
			continue
		}
		if _, ok := seen[w]; ok {
			continue
		}
		seen[w] = struct{}{}
		out = append(out, w)
	}
	return out
}

// isValidWord rejects empty strings and tokens with control characters.
func isValidWord(w string) bool {
	if w == "" {
		return false
	}
	for _, r := range w {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// FormatWordList writes a sorted personal-dictionary file (caller sorts).
func FormatWordList(words []string) string {
	if len(words) == 0 {
		return ""
	}
	return strings.Join(words, "\n") + "\n"
}

// MaxImportBytes caps custom-dictionary import file size (2 MiB).
const MaxImportBytes = 2 << 20

// MaxImportWords caps words accepted from one import file.
const MaxImportWords = 50_000
