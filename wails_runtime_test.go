package main

import (
	"errors"
	"testing"
)

// normalizeCancel pins the v3 dialog cancel-error contract. v3's cfd library
// returns ("", ErrorCancelled) on user cancel; v2 returned ("", nil). The
// helper must map the cancel shape to ("", nil) so every picker callsite
// honors the documented "Returns \"\" on cancel" contract. A dependency bump
// that changes the cancel-error wording would surface here.
func TestNormalizeCancel(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		err      error
		wantPath string
		wantErr  error
	}{
		{
			name:     "nil error returns path unchanged",
			path:     "/some/file.md",
			err:      nil,
			wantPath: "/some/file.md",
			wantErr:  nil,
		},
		{
			name:     "cancel error maps to empty path nil error",
			path:     "",
			err:      errors.New("ErrorCancelled: dialog cancelled by user"),
			wantPath: "",
			wantErr:  nil,
		},
		{
			name:     "cancelled lowercase variant",
			path:     "",
			err:      errors.New("operation cancelled"),
			wantPath: "",
			wantErr:  nil,
		},
		{
			name:     "non-cancel error preserved",
			path:     "",
			err:      errors.New("dialog subsystem unavailable"),
			wantPath: "",
			wantErr:  errors.New("dialog subsystem unavailable"),
		},
		{
			name:     "path returned with non-cancel error even if non-empty",
			path:     "/partial/path",
			err:      errors.New("I/O timeout"),
			wantPath: "/partial/path",
			wantErr:  errors.New("I/O timeout"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotErr := normalizeCancel(tt.path, tt.err)
			if gotPath != tt.wantPath {
				t.Errorf("normalizeCancel path = %q, want %q", gotPath, tt.wantPath)
			}
			if (gotErr == nil) != (tt.wantErr == nil) {
				t.Errorf("normalizeCancel err = %v, want %v", gotErr, tt.wantErr)
			}
			if gotErr != nil && tt.wantErr != nil && gotErr.Error() != tt.wantErr.Error() {
				t.Errorf("normalizeCancel err = %q, want %q", gotErr.Error(), tt.wantErr.Error())
			}
		})
	}
}

// emitOrQueue must queue OR emit, never both. Before MarkFrontendReady events
// are queued ONLY (no live emit); after, they emit ONLY (no queue). This
// prevents the duplicate-delivery race where a pre-ready event reaches the
// already-registered listener live AND is replayed by GetStartupEvents.
func TestEmitOrQueueQueuesOnlyBeforeReady(t *testing.T) {
	app := &App{}
	app.frontendReady = false

	app.emitOrQueue("test:event", "payload")
	if len(app.startupEvents) != 1 {
		t.Fatalf("pre-ready emitOrQueue: want 1 queued event, got %d", len(app.startupEvents))
	}
	if app.startupEvents[0].Name != "test:event" {
		t.Errorf("queued event name = %q, want %q", app.startupEvents[0].Name, "test:event")
	}
}

func TestEmitOrQueueEmitsOnlyAfterReady(t *testing.T) {
	app := &App{}
	app.frontendReady = true

	app.emitOrQueue("test:event", "payload")
	if len(app.startupEvents) != 0 {
		t.Fatalf("post-ready emitOrQueue: want 0 queued events, got %d (should emit-only, not queue)", len(app.startupEvents))
	}
}

func TestGetStartupEventsDrainsQueue(t *testing.T) {
	app := &App{}
	app.frontendReady = false

	app.emitOrQueue("a", 1)
	app.emitOrQueue("b", 2)
	events := app.GetStartupEvents()
	if len(events) != 2 {
		t.Fatalf("GetStartupEvents: want 2, got %d", len(events))
	}
	if len(app.startupEvents) != 0 {
		t.Errorf("GetStartupEvents did not drain queue: %d remaining", len(app.startupEvents))
	}
}

func TestEmitOrQueueCapsStartupEvents(t *testing.T) {
	app := &App{}
	app.frontendReady = false

	for i := 0; i < maxStartupEvents+50; i++ {
		app.emitOrQueue("capped", i)
	}
	if len(app.startupEvents) > maxStartupEvents {
		t.Fatalf("startupEvents exceeded cap: got %d, max %d", len(app.startupEvents), maxStartupEvents)
	}
}
