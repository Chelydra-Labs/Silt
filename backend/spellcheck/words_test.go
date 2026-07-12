package spellcheck

import (
	"reflect"
	"testing"
)

func TestParseWordList(t *testing.T) {
	in := `# comment
TypeScript
oauth
# another

typescript
word # trailing comment
cspell-tools: keep-case
`
	got := ParseWordList(in)
	want := []string{"typescript", "oauth", "word"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseWordList = %v, want %v", got, want)
	}
}

func TestParseWordList_RejectsControl(t *testing.T) {
	got := ParseWordList("ok\n\x00bad\n")
	if len(got) != 1 || got[0] != "ok" {
		t.Errorf("got %v, want [ok]", got)
	}
}

func TestFormatWordList(t *testing.T) {
	if FormatWordList(nil) != "" {
		t.Error("empty should format to empty")
	}
	got := FormatWordList([]string{"a", "b"})
	if got != "a\nb\n" {
		t.Errorf("got %q", got)
	}
}

func TestBundledSoftwareTerms(t *testing.T) {
	if BundledSoftwareTerms == "" {
		t.Fatal("embedded software-terms missing")
	}
	words := ParseWordList(BundledSoftwareTerms)
	if len(words) < 50 {
		t.Errorf("expected curated list, got %d words", len(words))
	}
	found := false
	for _, w := range words {
		if w == "typescript" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected typescript in bundled software-terms")
	}
}
