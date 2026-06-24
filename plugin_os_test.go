package main

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// PluginNotify caps title to maxNotifyTitleRunes and body to
// maxNotifyBodyRunes at the binding entry point, truncating with an ellipsis
// rather than erroring (#255, audit F22). This test passes a 1 MB payload and
// asserts the mocked notifier receives only the capped string — confirming the
// truncation happens BEFORE the OS subprocess would see the payload.
func TestPluginNotify_TruncatesOversizedTitleBody(t *testing.T) {
	app := newTestApp(t)

	token, err := app.RegisterPluginSession("silt-kanban")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	var gotTitle, gotBody string
	orig := notifyDesktop
	notifyDesktop = func(title, body string) error {
		gotTitle = title
		gotBody = body
		return nil
	}
	t.Cleanup(func() { notifyDesktop = orig })

	hugeTitle := strings.Repeat("T", 1024*1024) // 1 MB
	hugeBody := strings.Repeat("B", 1024*1024)

	if err := app.PluginNotify("silt-kanban", token, hugeTitle, hugeBody); err != nil {
		t.Fatalf("PluginNotify returned error (should be best-effort, no error): %v", err)
	}

	if gotTitle == "" {
		t.Fatal("notifyDesktop was not called")
	}
	titleRunes := utf8.RuneCountInString(gotTitle)
	bodyRunes := utf8.RuneCountInString(gotBody)

	if titleRunes != maxNotifyTitleRunes {
		t.Errorf("title rune count = %d, want %d (capped)", titleRunes, maxNotifyTitleRunes)
	}
	if bodyRunes != maxNotifyBodyRunes {
		t.Errorf("body rune count = %d, want %d (capped)", bodyRunes, maxNotifyBodyRunes)
	}
	if !strings.HasSuffix(gotTitle, "…") {
		t.Error("truncated title should end with ellipsis")
	}
	if !strings.HasSuffix(gotBody, "…") {
		t.Error("truncated body should end with ellipsis")
	}
}

// PluginNotify passes short strings through unchanged — no truncation, no
// ellipsis appended.
func TestPluginNotify_PassesShortStringsUnchanged(t *testing.T) {
	app := newTestApp(t)
	token, _ := app.RegisterPluginSession("silt-kanban")

	var gotTitle, gotBody string
	orig := notifyDesktop
	notifyDesktop = func(title, body string) error {
		gotTitle, gotBody = title, body
		return nil
	}
	t.Cleanup(func() { notifyDesktop = orig })

	if err := app.PluginNotify("silt-kanban", token, "Hello", "World"); err != nil {
		t.Fatalf("PluginNotify: %v", err)
	}
	if gotTitle != "Hello" || gotBody != "World" {
		t.Errorf("notifyDesktop got title=%q body=%q; want unchanged", gotTitle, gotBody)
	}
}

// capRunes truncates to the limit with an ellipsis and leaves short strings
// alone (no allocation).
func TestCapRunes(t *testing.T) {
	if got := capRunes("short", 10); got != "short" {
		t.Errorf("capRunes(short) = %q, want %q", got, "short")
	}
	if got := capRunes("", 10); got != "" {
		t.Errorf("capRunes(empty) = %q, want empty", got)
	}
	exactly := strings.Repeat("x", 5)
	if got := capRunes(exactly, 5); got != exactly {
		t.Errorf("capRunes(at limit) should not truncate, got %q", got)
	}
	over := strings.Repeat("x", 8)
	got := capRunes(over, 5)
	if utf8.RuneCountInString(got) != 5 {
		t.Errorf("capRunes(over) rune count = %d, want 5", utf8.RuneCountInString(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("capRunes(over) should end with ellipsis, got %q", got)
	}
	// Multibyte: truncate at a rune boundary, not a byte boundary.
	multi := strings.Repeat("é", 10) // 2 bytes per rune
	gotMulti := capRunes(multi, 3)
	if utf8.RuneCountInString(gotMulti) != 3 {
		t.Errorf("capRunes(multibyte) rune count = %d, want 3", utf8.RuneCountInString(gotMulti))
	}
}
