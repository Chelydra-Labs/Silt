package main

import "testing"

// GetLocalAuthor prefers the OS display name and falls back to the auditActor
// username so the local_author preference has a deterministic default without
// a second OS-user lookup path. Both vars are reassignable, so these tests pin
// the binding to the vars without depending on the host user.
func TestGetLocalAuthor_PrefersDisplayName(t *testing.T) {
	app := &App{}
	originalName := localDisplayName
	originalActor := auditActor
	defer func() {
		localDisplayName = originalName
		auditActor = originalActor
	}()
	localDisplayName = func() string { return "Test User" }
	auditActor = func() string { return "test-user" }

	if got := app.GetLocalAuthor(); got != "Test User" {
		t.Fatalf("GetLocalAuthor = %q, want %q", got, "Test User")
	}
}

func TestGetLocalAuthor_MirrorsAuditActor(t *testing.T) {
	app := &App{}
	originalName := localDisplayName
	originalActor := auditActor
	defer func() {
		localDisplayName = originalName
		auditActor = originalActor
	}()
	// No display name on the account (or os/user failed) → the username is
	// the seed.
	localDisplayName = func() string { return "" }
	auditActor = func() string { return "test-user" }

	if got := app.GetLocalAuthor(); got != "test-user" {
		t.Fatalf("GetLocalAuthor = %q, want %q", got, "test-user")
	}

	// The "unknown" placeholder is filtered to "" so callers treat "" as
	// "still unresolved" rather than persisting a permanent "unknown"
	// attribution.
	auditActor = func() string { return "unknown" }
	if got := app.GetLocalAuthor(); got != "" {
		t.Fatalf("GetLocalAuthor fallback = %q, want %q", got, "")
	}
}
