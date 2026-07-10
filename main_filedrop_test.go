package main

import (
	"reflect"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TestThemeDropPaths_Routing: the bridge must forward ONLY drops that land on
// the Appearance target (ElementID == themeFileDropTargetID) and carry files.
// Any other drop target (editor, attachments), a missing target, or an empty
// payload yields nil so unrelated OS drops never reach the theme importer.
func TestThemeDropPaths_Routing(t *testing.T) {
	files := []string{"/vault/.system/themes/custom.json", "/tmp/other.css"}

	tests := []struct {
		name    string
		details *application.DropTargetDetails
		files   []string
		want    []string
	}{
		{
			name:    "appearance target with files forwards paths",
			details: &application.DropTargetDetails{ElementID: themeFileDropTargetID},
			files:   files,
			want:    files,
		},
		{
			name:    "non-appearance target is dropped",
			details: &application.DropTargetDetails{ElementID: "editor-drop-target"},
			files:   files,
			want:    nil,
		},
		{
			name:    "empty element id is dropped",
			details: &application.DropTargetDetails{ElementID: ""},
			files:   files,
			want:    nil,
		},
		{
			name:    "nil details is a safe no-op",
			details: nil,
			files:   files,
			want:    nil,
		},
		{
			name:    "appearance target with no files yields nil",
			details: &application.DropTargetDetails{ElementID: themeFileDropTargetID},
			files:   nil,
			want:    nil,
		},
		{
			name:    "appearance target with empty file slice yields nil",
			details: &application.DropTargetDetails{ElementID: themeFileDropTargetID},
			files:   []string{},
			want:    nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := themeDropPaths(tt.details, tt.files)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("themeDropPaths() = %v; want %v", got, tt.want)
			}
		})
	}
}

// TestThemeDropPaths_ForwardsExactSlice: when routing applies, the returned
// slice must be the caller's (no defensive copy) so the importer sees the
// exact paths — a copy would just add allocation cost with no isolation gain
// since the slice is freshly produced by the Wails runtime per drop.
func TestThemeDropPaths_ForwardsExactSlice(t *testing.T) {
	files := []string{"/a.json", "/b.json"}
	details := &application.DropTargetDetails{ElementID: themeFileDropTargetID}
	if got := themeDropPaths(details, files); !reflect.DeepEqual(got, files) {
		t.Fatalf("themeDropPaths() = %v; want %v", got, files)
	}
}

// TestThemeDropTargetConstants: the drop-target id and bridge event name are
// the backend<->frontend contract for the Appearance file-drop; drift breaks
// the feature silently (drops go nowhere), so pin them here.
func TestThemeDropTargetConstants(t *testing.T) {
	if themeFileDropTargetID != "theme-file-drop-target" {
		t.Fatalf("themeFileDropTargetID = %q; want %q", themeFileDropTargetID, "theme-file-drop-target")
	}
	if themeFilesDroppedEvent != "theme:files-dropped" {
		t.Fatalf("themeFilesDroppedEvent = %q; want %q", themeFilesDroppedEvent, "theme:files-dropped")
	}
}
