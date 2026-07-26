package db

import (
	"path/filepath"
	"testing"
)

func TestPathWithin(t *testing.T) {
	tmp := t.TempDir()
	sub := filepath.Join(tmp, "child", "grand")
	sibling := filepath.Join(filepath.Dir(tmp), "other")
	parent := filepath.Dir(tmp)

	cases := []struct {
		name string
		p    string
		dir  string
		want bool
	}{
		{"nested child", sub, tmp, true},
		{"same dir", tmp, tmp, true},
		{"sibling", sibling, tmp, false},
		{"parent of tmp", parent, tmp, false},
		{"empty path", "", tmp, false},
		{"empty dir", tmp, "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pathWithin(c.p, c.dir); got != c.want {
				t.Errorf("pathWithin(%q, %q) = %v, want %v", c.p, c.dir, got, c.want)
			}
		})
	}
}

func TestPathWithin_CaseInsensitive(t *testing.T) {
	// Detection lowercases both sides, so a vault path whose components differ
	// in case from the cloud root still matches. Correct on Windows/macOS; an
	// over-match against a same-named non-cloud Linux folder is harmless for a
	// best-effort warning. pathWithin is pure path-string logic and does not
	// require the paths to exist on disk.
	tmp := t.TempDir()
	dir := filepath.Join(tmp, "OneDrive")
	p := filepath.Join(tmp, "onedrive", "Documents", "vault")
	if got := pathWithin(p, dir); !got {
		t.Errorf("pathWithin with differing case = %v, want true (case-insensitive)", got)
	}
}
