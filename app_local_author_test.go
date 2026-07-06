package main

import "testing"

// GetLocalAuthor mirrors auditActor so the local_author preference has a
// deterministic default without a second OS-user lookup path. The auditActor
// var is reassignable, so this test pins the binding to the var without
// depending on the host user.
func TestGetLocalAuthor_MirrorsAuditActor(t *testing.T) {
	app := &App{}
	original := auditActor
	defer func() { auditActor = original }()
	auditActor = func() string { return "test-user" }

	if got := app.GetLocalAuthor(); got != "test-user" {
		t.Fatalf("GetLocalAuthor = %q, want %q", got, "test-user")
	}

	// The "unknown" fallback path (host can't resolve the OS user) must also
	// flow through, since downstream code treats "" as "still unresolved".
	auditActor = func() string { return "unknown" }
	if got := app.GetLocalAuthor(); got != "unknown" {
		t.Fatalf("GetLocalAuthor fallback = %q, want %q", got, "unknown")
	}
}
