package plugins

import (
	"slices"
	"testing"
)

// first_party_test.go guards the reserved first-party id set (#407).
//
// first_party.go:9-12 documents an invariant: every id registered in the
// frontend bundle (frontend/src/plugins/registry.ts) MUST also appear in
// FirstPartyPluginIDs so that (a) a third-party archive claiming the id is
// rejected at install time and (b) seedFirstPartyGrants seeds every known
// capability for the bundled plugin. silt-tasks was added to the frontend
// registry without updating this set, which broke task creation
// (PluginCreateTask's content-mutate gate denied every quick-add).
//
// The exact-set assertion below is the drift guard: a future bundled plugin
// that forgets to register its id here fails this test. The frontend mirror
// lives in registry.test.ts.

// TestFirstPartyPluginIDs_ExactSet pins the reserved-id roster so a new
// bundled plugin that forgets to add its id fails loudly here rather than
// silently losing its grants at runtime.
func TestFirstPartyPluginIDs_ExactSet(t *testing.T) {
	want := []string{
		"silt-attachments",
		"silt-tasks",
		"silt-ai-summary",
		"silt-ai-qa",
		"silt-ai-assistant",
	}
	got := make([]string, 0, len(FirstPartyPluginIDs))
	for id := range FirstPartyPluginIDs {
		got = append(got, id)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("FirstPartyPluginIDs roster drift: got %v, want %v.\n"+
			"Adding a bundled plugin? Add its id here AND in "+
			"frontend/src/plugins/registry.ts (the parity test there enforces "+
			"the other direction).", got, want)
	}
}

// TestFirstPartyPluginIDs_ContainsTasks is the focused regression for #407:
// silt-tasks MUST be reserved so its content-mutate grant is seeded.
func TestFirstPartyPluginIDs_ContainsTasks(t *testing.T) {
	if !IsFirstPartyID("silt-tasks") {
		t.Fatal("silt-tasks must be a reserved first-party id; without it " +
			"seedFirstPartyGrants never seeds content-mutate and the Tasks " +
			"view quick-add is denied (#407)")
	}
}

// TestFirstPartyPluginIDs_ContainsAISummary is the focused regression for
// #220–#223: silt-ai-summary MUST be reserved so its ai + plugin-db grants are
// seeded. Without the reservation, requireGrant denies every summarize() call
// even though the frontend grant cache masks it as first-party.
func TestFirstPartyPluginIDs_ContainsAISummary(t *testing.T) {
	if !IsFirstPartyID("silt-ai-summary") {
		t.Fatal("silt-ai-summary must be a reserved first-party id; without it " +
			"seedFirstPartyGrants never seeds ai/plugin-db and every summarize() " +
			"call is denied at the Go requireGrant gate (#220)")
	}
}

// TestFirstPartyPluginIDs_ContainsAIQA is the focused regression for #224–#228:
// silt-ai-qa MUST be reserved so its ai + plugin-db grants are seeded.
func TestFirstPartyPluginIDs_ContainsAIQA(t *testing.T) {
	if !IsFirstPartyID("silt-ai-qa") {
		t.Fatal("silt-ai-qa must be a reserved first-party id; without it " +
			"seedFirstPartyGrants never seeds ai/plugin-db and every embed/complete " +
			"call is denied at the Go requireGrant gate (#224)")
	}
}

// TestFirstPartyPluginIDs_ContainsAIAssistant is the focused regression for
// #229–#233: silt-ai-assistant MUST be reserved so its ai + content-mutate
// grants are seeded.
func TestFirstPartyPluginIDs_ContainsAIAssistant(t *testing.T) {
	if !IsFirstPartyID("silt-ai-assistant") {
		t.Fatal("silt-ai-assistant must be a reserved first-party id; without it " +
			"seedFirstPartyGrants never seeds ai/content-mutate and every " +
			"complete/accept path is denied at the Go requireGrant gate (#230)")
	}
}
