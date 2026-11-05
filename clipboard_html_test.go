package main

import (
	"errors"
	"reflect"
	"testing"
)

// stubReadHTMLNative swaps the OS seam for the duration of one test. Tests are
// not parallel: the seam is a package-level variable.
func stubReadHTMLNative(t *testing.T, fn func() (string, error)) {
	t.Helper()
	previous := readHTMLNative
	readHTMLNative = fn
	t.Cleanup(func() { readHTMLNative = previous })
}

func TestReadClipboardHTML(t *testing.T) {
	tests := []struct {
		name     string
		seam     func() (string, error)
		wantHTML string
		wantErr  string
	}{
		{
			name:     "html is returned verbatim",
			seam:     func() (string, error) { return "<p>hello <b>world</b></p>", nil },
			wantHTML: "<p>hello <b>world</b></p>",
		},
		{
			name: "no html target reads as empty, not an error",
			seam: func() (string, error) { return "", nil },
		},
		{
			name: "read failure propagates",
			seam: func() (string, error) { return "", errors.New("clipboard read timed out") },
			// The binding must not swallow the failure: the frontend decides
			// whether to fall back to text from the error alone.
			wantErr: "clipboard read timed out",
		},
		{
			name:    "empty payload alongside an error stays an error",
			seam:    func() (string, error) { return "", errors.New("clipboard HTML exceeds the size limit") },
			wantErr: "clipboard HTML exceeds the size limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stubReadHTMLNative(t, tt.seam)

			// A zero App is enough: the binding is a pure pass-through, so this
			// also pins that it never touches application state.
			got, err := (&App{}).ReadClipboardHTML()

			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("ReadClipboardHTML() error = nil, want %q", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("ReadClipboardHTML() error = %q, want %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("ReadClipboardHTML() unexpected error: %v", err)
			}

			if got != tt.wantHTML {
				t.Fatalf("ReadClipboardHTML() = %q, want %q", got, tt.wantHTML)
			}
		})
	}
}

func TestReadClipboardHTMLSeamRestored(t *testing.T) {
	original := reflect.ValueOf(readHTMLNative).Pointer()

	t.Run("stubbed", func(t *testing.T) {
		stubReadHTMLNative(t, func() (string, error) { return "<b>stub</b>", nil })
		if got, _ := (&App{}).ReadClipboardHTML(); got != "<b>stub</b>" {
			t.Fatalf("ReadClipboardHTML() = %q, want the stubbed value", got)
		}
	})

	// t.Cleanup ran when the subtest ended; a leaked stub would silently make
	// every later test read the fake clipboard.
	if got := reflect.ValueOf(readHTMLNative).Pointer(); got != original {
		t.Fatal("readHTMLNative was not restored after the stubbed test")
	}
}
