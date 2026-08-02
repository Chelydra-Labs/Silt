package parser

import (
	"strings"
	"testing"
)

// TestTypeField_AbsentRoundTripsUnchanged is the AC#6 regression: an untyped
// page (no `type:` in its frontmatter) must round-trip through ParseFileContent
// → RenderFileContent with no `type:` key added. The Type field is purely
// additive, so an absent value cannot silently become `type: ""` (or any other
// shape) on save.
//
// The byte-for-byte assertion is on the frontmatter only — the body's block-id
// minting is governed by a separate invariant (TestLifecycleTokens_*). Splitting
// the focus keeps this test honest about what AC#6 actually requires: an absent
// `type:` key stays absent through a parse/render pass.
func TestTypeField_AbsentRoundTripsUnchanged(t *testing.T) {
	src := "---\n" +
		"notebook: \"Notes\"\n" +
		"section: \"\"\n" +
		"page: \"Plain\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"---\n" +
		"# Plain\n"

	blocks, meta, _, _, err := ParseFileContent(src, "Notes", "", "Plain", "2026-08-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if meta.Type != "" {
		t.Errorf("Type = %q, want empty (no type: in source)", meta.Type)
	}
	if _, present := meta.Frontmatter["type"]; present {
		t.Errorf("Frontmatter map unexpectedly carries a `type` key: %v", meta.Frontmatter)
	}

	origFM, body := SplitFrontmatter(src)
	rendered := RenderFileContent(blocks, body, origFM, 4)
	gotFM, _ := SplitFrontmatter(rendered)

	if strings.Contains(gotFM, "type:") {
		t.Errorf("rendered frontmatter unexpectedly contains `type:`:\n%s", gotFM)
	}
	if gotFM != origFM {
		t.Errorf("frontmatter drifted through render:\n--- src ---\n%s\n--- rendered ---\n%s", origFM, gotFM)
	}
}

// TestTypeField_PresentRoundTripsUnchanged pins the symmetric case: when
// `type:` IS present, it survives the round trip with its exact value. This is
// the contract the typed-notes feature relies on — a typed page stays typed.
func TestTypeField_PresentRoundTripsUnchanged(t *testing.T) {
	src := "---\n" +
		"notebook: \"Books\"\n" +
		"section: \"\"\n" +
		"page: \"Dune\"\n" +
		"date: \"2026-08-01\"\n" +
		"tags: []\n" +
		"type: \"book\"\n" +
		"---\n" +
		"# Dune\n"

	blocks, meta, _, _, err := ParseFileContent(src, "Books", "", "Dune", "2026-08-01", 4)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if meta.Type != "book" {
		t.Errorf("Type = %q, want %q", meta.Type, "book")
	}
	if v, present := meta.Frontmatter["type"]; !present || v != "book" {
		t.Errorf("Frontmatter[type] = %v (%v present), want \"book\"", v, present)
	}

	origFM, body := SplitFrontmatter(src)
	rendered := RenderFileContent(blocks, body, origFM, 4)
	gotFM, _ := SplitFrontmatter(rendered)
	if !strings.Contains(gotFM, "type: \"book\"") {
		t.Errorf("rendered frontmatter lost the type: key:\n%s", gotFM)
	}
	if gotFM != origFM {
		t.Errorf("frontmatter drifted through render:\n--- src ---\n%s\n--- rendered ---\n%s", origFM, gotFM)
	}
}

// TestTypeField_FileMetadataTypeReflectsFrontmatter pins that the parsed
// FileMetadata.Type mirrors whatever `type:` value the frontmatter carries,
// and is empty when the key is absent. The projection layer relies on this to
// decide whether a page participates in the typed-notes feature.
func TestTypeField_FileMetadataTypeReflectsFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		fmBody   string
		wantType string
	}{
		{
			name:     "absent type key",
			fmBody:   "notebook: \"N\"\nsection: \"\"\npage: \"P\"\ndate: \"2026-08-01\"\ntags: []\n",
			wantType: "",
		},
		{
			name:     "quoted book id",
			fmBody:   "notebook: \"N\"\nsection: \"\"\npage: \"P\"\ndate: \"2026-08-01\"\ntags: []\ntype: \"book\"\n",
			wantType: "book",
		},
		{
			name:     "unquoted book id",
			fmBody:   "notebook: \"N\"\nsection: \"\"\npage: \"P\"\ndate: \"2026-08-01\"\ntags: []\ntype: book\n",
			wantType: "book",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "---\n" + c.fmBody + "---\n# P\n"
			_, meta, _, _, err := ParseFileContent(src, "N", "", "P", "2026-08-01", 4)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if meta.Type != c.wantType {
				t.Errorf("Type = %q, want %q", meta.Type, c.wantType)
			}
		})
	}
}
