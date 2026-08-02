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
