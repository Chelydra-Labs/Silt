package parser

import (
	"strings"
	"testing"
)

func TestSetFrontmatterField_PreservesOtherLines(t *testing.T) {
	input := "---\n" +
		"# this is a comment\n" +
		"title: \"My Note\"\n" +
		"count: 42\n" +
		"tags:\n" +
		"  - a\n" +
		"  - b\n" +
		"---\n" +
		"body line\n"

	expected := "---\n" +
		"# this is a comment\n" +
		"title: \"New Title\"\n" +
		"count: 42\n" +
		"tags:\n" +
		"  - a\n" +
		"  - b\n" +
		"---\n" +
		"body line\n"

	got, err := SetFrontmatterField(input, "title", "New Title")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("SetFrontmatterField did not preserve other lines\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

func TestSetFrontmatterField_ReplacesBlockSequence(t *testing.T) {
	input := "---\n" +
		"tags:\n" +
		"  - a\n" +
		"  - b\n" +
		"title: \"Hello\"\n" +
		"---\n" +
		"body\n"

	expected := "---\n" +
		"tags: [\"a\", \"b\"]\n" +
		"title: \"Hello\"\n" +
		"---\n" +
		"body\n"

	got, err := SetFrontmatterField(input, "tags", []string{"a", "b"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("block sequence not collapsed to inline\ngot:\n%s\nwant:\n%s", got, expected)
	}
	if strings.Contains(got, "  - a") || strings.Contains(got, "  - b") {
		t.Errorf("orphan block entries left behind:\n%s", got)
	}
}

func TestSetFrontmatterField_InsertsMissingKey(t *testing.T) {
	input := "---\n" +
		"title: \"Hello\"\n" +
		"---\n" +
		"body\n"

	expected := "---\n" +
		"title: \"Hello\"\n" +
		"author: \"Chris\"\n" +
		"---\n" +
		"body\n"

	got, err := SetFrontmatterField(input, "author", "Chris")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("missing key not inserted before closing fence\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

func TestSetFrontmatterField_NoFrontmatter(t *testing.T) {
	input := "Hello world\nsecond line\n"
	expected := "---\n" +
		"title: \"Hi\"\n" +
		"---\n" +
		"Hello world\nsecond line\n"

	got, err := SetFrontmatterField(input, "title", "Hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != expected {
		t.Errorf("frontmatter not created for bare body\ngot:\n%s\nwant:\n%s", got, expected)
	}
}

func TestSetFrontmatterField_ValueTypes(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"string", "foo", "key: \"foo\""},
		{"int", 42, "key: 42"},
		{"int64", int64(7), "key: 7"},
		{"float", 3.14, "key: 3.14"},
		{"floatWhole", 100.0, "key: 100"},
		{"boolTrue", true, "key: true"},
		{"boolFalse", false, "key: false"},
		{"stringList", []string{"a", "b"}, "key: [\"a\", \"b\"]"},
		{"anyList", []any{"x", "y"}, "key: [\"x\", \"y\"]"},
		{"nil", nil, "key: "},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			input := "---\nkey: old\n---\nbody\n"
			got, err := SetFrontmatterField(input, "key", c.value)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := "---\n" + c.want + "\n---\nbody\n"
			if got != want {
				t.Errorf("value type render mismatch\ngot:  %q\nwant: %q", got, want)
			}
		})
	}
}

func TestSetFrontmatterField_InvalidKey(t *testing.T) {
	cases := []string{
		"bad key", // space
		"bad:key", // colon
		"1starts", // leading digit
		"",        // empty
		"has.dot", // dot
	}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			_, err := SetFrontmatterField("---\nkey: v\n---\nbody\n", key, "v")
			if err == nil {
				t.Fatalf("expected error for invalid key %q, got nil", key)
			}
		})
	}
}

func TestClearFrontmatterField_RemovesKey(t *testing.T) {
	t.Run("presentWithBlock", func(t *testing.T) {
		input := "---\n" +
			"title: \"Keep\"\n" +
			"tags:\n" +
			"  - a\n" +
			"  - b\n" +
			"count: 5\n" +
			"---\n" +
			"body\n"
		expected := "---\n" +
			"title: \"Keep\"\n" +
			"count: 5\n" +
			"---\n" +
			"body\n"
		got, err := ClearFrontmatterField(input, "tags")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != expected {
			t.Errorf("clear did not remove key and its block\ngot:\n%s\nwant:\n%s", got, expected)
		}
	})

	t.Run("absentKeyIsIdempotent", func(t *testing.T) {
		input := "---\ntitle: \"Hello\"\n---\nbody\n"
		got, err := ClearFrontmatterField(input, "missing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != input {
			t.Errorf("clearing absent key changed content\ngot:  %q\nwant: %q", got, input)
		}
	})

	t.Run("noFrontmatterIsIdempotent", func(t *testing.T) {
		input := "just a body\n"
		got, err := ClearFrontmatterField(input, "title")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != input {
			t.Errorf("clearing with no frontmatter changed content\ngot:  %q\nwant: %q", got, input)
		}
	})
}

func TestSetFrontmatterField_BodyByteExact(t *testing.T) {
	// Body contains a horizontal rule (---) and blank lines to prove the edit
	// only touches the LEADING frontmatter block, not any later --- in the body.
	input := "---\n" +
		"title: \"Hello\"\n" +
		"---\n" +
		"\n" +
		"---\n" +
		"\n" +
		"some content\n" +
		"\n" +
		"---\n"

	origFM, origBody := SplitFrontmatter(input)
	if origFM == "" {
		t.Fatalf("test setup: input has no frontmatter")
	}

	got, err := SetFrontmatterField(input, "title", "World")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, newBody := SplitFrontmatter(got)
	if newBody != origBody {
		t.Errorf("body not byte-exact after edit\norig: %q\nnew:  %q", origBody, newBody)
	}

	// And the title line actually changed.
	if !strings.HasPrefix(got, "---\ntitle: \"World\"\n") {
		t.Errorf("title line not updated in result:\n%s", got)
	}
}

func TestSetFrontmatterField_DuplicateKey(t *testing.T) {
	// YAML is last-wins on duplicate keys; editing the first match would read
	// back the untouched last value. The editor must refuse such a file.
	input := "---\nrating: 3\nrating: 7\n---\nbody\n"

	t.Run("setRefuses", func(t *testing.T) {
		got, err := SetFrontmatterField(input, "rating", 5)
		if err == nil {
			t.Fatal("expected error for duplicate key, got nil")
		}
		if !strings.Contains(err.Error(), "appears more than once") {
			t.Errorf("error should mention duplicate, got %v", err)
		}
		if got != "" {
			t.Errorf("on error result should be empty, got %q", got)
		}
	})

	t.Run("clearRefuses", func(t *testing.T) {
		got, err := ClearFrontmatterField(input, "rating")
		if err == nil {
			t.Fatal("expected error for duplicate key, got nil")
		}
		if !strings.Contains(err.Error(), "appears more than once") {
			t.Errorf("error should mention duplicate, got %v", err)
		}
		if got != "" {
			t.Errorf("on error result should be empty, got %q", got)
		}
	})
}

func TestSetFrontmatterField_BOMPrefixed(t *testing.T) {
	// A BOM-prefixed file (synced from Obsidian/OneDrive/Dropbox) must be
	// treated as having frontmatter; before BOM-stripping it was seen as a
	// bare body and the whole body was wrapped in a fresh fence.
	input := "\uFEFF---\nrating: 5\n---\nbody\n"

	got, err := SetFrontmatterField(input, "rating", 9)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fm, body := SplitFrontmatter(got)
	if fm == "" {
		t.Fatalf("BOM file lost its frontmatter; got fm=%q body=%q", fm, body)
	}
	if !strings.Contains(got, "rating: 9") {
		t.Errorf("rating not updated\ngot: %q", got)
	}
	if body != "body\n" {
		t.Errorf("body corrupted by BOM handling\ngot body: %q", body)
	}
	// Catastrophic failure mode: body double-wrapped in fences. The single
	// frontmatter block means SplitFrontmatter returns the body cleanly.
	if strings.Count(body, "---") != 0 {
		t.Errorf("body should not contain fence lines; got %q", body)
	}
}

func TestSetFrontmatterField_EditedKeyInlineCommentDropped(t *testing.T) {
	// An inline comment on the EDITED line is re-rendered away; comments on
	// every other line survive byte-for-byte. Documents the one known
	// byte-exactness exception honestly.
	input := "---\n" +
		"# header comment\n" +
		"rating: 5  # out of ten\n" +
		"status: todo  # keep me\n" +
		"---\n" +
		"body\n"

	got, err := SetFrontmatterField(input, "rating", 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(got, "out of ten") {
		t.Errorf("edited-key inline comment should be dropped\ngot: %q", got)
	}
	if !strings.Contains(got, "# header comment") {
		t.Errorf("header comment should be preserved\ngot: %q", got)
	}
	if !strings.Contains(got, "# keep me") {
		t.Errorf("other-line comment should be preserved\ngot: %q", got)
	}
	if !strings.Contains(got, "rating: 7") {
		t.Errorf("rating not updated\ngot: %q", got)
	}
}
