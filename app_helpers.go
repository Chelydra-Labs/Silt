package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	goruntime "runtime"
	"strings"
	"time"

	"silt/backend/history"
)

var updateLineIDRegex = regexp.MustCompile(`<!-- id: ([a-f0-9\-]{36}) -->`)

// fileOrDefaultDate returns the file's modification date (YYYY-MM-DD), falling
// back to today if the stat fails. Used consistently by SaveFileBlocks,
// MutateBlock, and UpdateBlockState as the defaultDate passed to
// ParseFileContent — ensures old blocks without a @ date suffix inherit the
// file's actual mtime rather than silently shifting to today.
func fileOrDefaultDate(filePath string) string {
	if fi, err := os.Stat(filePath); err == nil {
		return fi.ModTime().Format("2006-01-02")
	}
	return time.Now().Format("2006-01-02")
}

// findLineByBlockID returns the 0-based index of the line in `lines` whose
// trailing `<!-- id: UUID -->` comment matches blockID, or -1 if no such line
// exists.
func findLineByBlockID(lines []string, blockID string) int {
	for i, line := range lines {
		matches := updateLineIDRegex.FindStringSubmatch(line)
		if len(matches) >= 2 && matches[1] == blockID {
			return i
		}
	}
	return -1
}

// sanitizePathSegment strips path-traversal indicators from a single path
// component: directory separators, NUL, control chars, and a LEADING `..`
// (or run of leading `..`s) which is the path-traversal signal. Internal `..`
// substrings (e.g. `2.0..2.1`, `a..b..c`) are preserved verbatim — they are
// legitimate filename characters, not traversal (#89). The contract is
// "single segment": `/` and `\` are stripped so the join can never produce
// a multi-segment path.
func sanitizePathSegment(s string) string {
	cleaned := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r < 32 {
			return -1
		}
		return r
	}, s)
	cleaned = strings.TrimSpace(cleaned)
	for strings.HasPrefix(cleaned, "..") {
		cleaned = strings.TrimSpace(strings.TrimPrefix(cleaned, ".."))
	}
	if cleaned == "." {
		cleaned = ""
	}
	return cleaned
}

// sanitizeSectionPath sanitizes a multi-segment section path (e.g.
// "Projects/Active"). Each segment is sanitized independently via
// sanitizePathSegment, preserving the `/` separator so deeply-nested
// section paths survive the sanitize pass (#88, #97). An empty input
// (or all-empty segments) returns "".
func sanitizeSectionPath(s string) string {
	if s == "" {
		return ""
	}
	parts := strings.Split(s, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if c := sanitizePathSegment(p); c != "" {
			out = append(out, c)
		}
	}
	return strings.Join(out, "/")
}

// validateSectionPath accepts only a canonical relative path. Unlike the old
// sanitizer it never silently drops traversal components, because doing so can
// make a requested locator identify a different sibling section.
func validateSectionPath(s string, allowEmpty bool) (string, error) {
	if s == "" {
		if allowEmpty {
			return "", nil
		}
		return "", fmt.Errorf("section path is required")
	}
	if strings.ContainsRune(s, 0) || strings.ContainsAny(s, "\\") {
		return "", fmt.Errorf("section path must be a relative slash-separated path")
	}
	parts := strings.Split(s, "/")
	for i, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "." || part == ".." || part == history.EmptySectionName || strings.ContainsAny(part, "\\") || strings.IndexFunc(part, func(r rune) bool { return r < 32 }) >= 0 {
			return "", fmt.Errorf("invalid section path %q", s)
		}
		parts[i] = part
	}
	return strings.Join(parts, "/"), nil
}

func invalidNavigationPath(err error) error {
	if err == nil {
		return nil
	}
	return NewIPCError(CodeInvalidNavigationPath, err.Error())
}

// isPathWithinRoot reports whether target is the same as or a descendant of
// root. Generalized from the vault-only check for #100: callers pass the
// resolved notebook root (vault root, an in-vault notebook dir, or a linked
// notebook root) so the same traversal guard covers external notebooks.
//
// Both paths are cleaned, made absolute, and resolved through EvalSymlinks
// (mirroring backend/plugins/installer.go:isWithin) so a symlink planted
// inside a notebook that points outside it cannot mask an escape. The
// comparison is case-insensitive on Windows where the filesystem itself is
// case-insensitive. EvalSymlinks errors (e.g. non-existent target during
// construction) fall back to the lexical form.
func isPathWithinRoot(target, root string) bool {
	absTarget, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(absTarget); err == nil {
		absTarget = resolved
	}
	if resolved, err := filepath.EvalSymlinks(absRoot); err == nil {
		absRoot = resolved
	}
	absTarget = filepath.Clean(absTarget)
	absRoot = filepath.Clean(absRoot)
	if absTarget == absRoot {
		return true
	}
	prefix := absRoot + string(os.PathSeparator)
	if goruntime.GOOS == "windows" {
		return strings.HasPrefix(strings.ToLower(absTarget), strings.ToLower(prefix))
	}
	return strings.HasPrefix(absTarget, prefix)
}

// isCreationPathWithinRoot applies the containment check to a path whose leaf
// does not exist yet. It resolves the nearest existing parent first, so a
// symlink in the path's existing parent chain cannot redirect a creation into
// an external tree. isPathWithinRoot intentionally keeps its permissive
// non-existent-target behavior for callers that only validate paths.
func isCreationPathWithinRoot(target, root string) bool {
	absTarget, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return false
	}
	absRoot, err := filepath.Abs(filepath.Clean(root))
	if err != nil {
		return false
	}
	resolvedRoot, rootMissing, err := resolveExistingParent(absRoot)
	if err != nil {
		return false
	}
	for i := len(rootMissing) - 1; i >= 0; i-- {
		resolvedRoot = filepath.Join(resolvedRoot, rootMissing[i])
	}
	resolvedParent, missingSuffix, err := resolveExistingParent(filepath.Dir(absTarget))
	if err != nil {
		return false
	}
	resolvedTarget := resolvedParent
	for i := len(missingSuffix) - 1; i >= 0; i-- {
		resolvedTarget = filepath.Join(resolvedTarget, missingSuffix[i])
	}
	return isPathWithinRoot(resolvedTarget, resolvedRoot)
}

func resolveExistingParent(path string) (string, []string, error) {
	current := filepath.Clean(path)
	var missingSuffix []string
	for {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return resolved, missingSuffix, nil
		}
		if !os.IsNotExist(err) {
			return "", nil, err
		}
		parent := filepath.Dir(current)
		if parent == current {
			return "", nil, err
		}
		missingSuffix = append(missingSuffix, filepath.Base(current))
		current = parent
	}
}
