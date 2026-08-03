package parser

import (
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		fmWant   string
		bodyWant string
	}{
		{
			name:     "standard frontmatter",
			content:  "---\ntitle: Hello\ntags: [a, b]\n---\nbody line 1\nbody line 2",
			fmWant:   "---\ntitle: Hello\ntags: [a, b]\n---\n",
			bodyWant: "body line 1\nbody line 2",
		},
		{
			name:     "no frontmatter",
			content:  "just a body\nwith lines",
			fmWant:   "",
			bodyWant: "just a body\nwith lines",
		},
		{
			name:     "empty content",
			content:  "",
			fmWant:   "",
			bodyWant: "",
		},
		{
			name:     "opening only no closing",
			content:  "---\nnot really frontmatter\nbody here",
			fmWant:   "",
			bodyWant: "---\nnot really frontmatter\nbody here",
		},
		{
			name:     "frontmatter only no body",
			content:  "---\nkey: val\n---\n",
			fmWant:   "---\nkey: val\n---\n",
			bodyWant: "",
		},
		{
			// A leading UTF-8 BOM (Obsidian / OneDrive / Dropbox sync) must
			// not defeat frontmatter detection, and is PRESERVED in the
			// returned frontmatter so reassembling writers keep the file's
			// byte signature.
			name:     "bom prefixed frontmatter",
			content:  "\uFEFF---\ntitle: Hello\n---\nbody",
			fmWant:   "\uFEFF---\ntitle: Hello\n---\n",
			bodyWant: "body",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fm, body := SplitFrontmatter(c.content)
			if fm != c.fmWant {
				t.Errorf("frontmatter = %q, want %q", fm, c.fmWant)
			}
			if body != c.bodyWant {
				t.Errorf("body = %q, want %q", body, c.bodyWant)
			}
		})
	}
}

func TestSplitFrontmatter_BOMRoundTrip(t *testing.T) {
	// The BOM must round-trip through SplitFrontmatter so reassembling writers
	// (RenderFileContent: fm+body) keep the file's byte signature stable.
	original := "\uFEFF---\ntitle: Hello\n---\nbody\n"
	fm, body := SplitFrontmatter(original)
	if !strings.HasPrefix(fm, "\uFEFF") {
		t.Errorf("fm should preserve the BOM, got %q", fm)
	}
	if fm+body != original {
		t.Errorf("round-trip mismatch\ngot:  %q\nwant: %q", fm+body, original)
	}
}
