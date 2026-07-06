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
		"silt-agenda",
		"silt-calendar",
		"silt-kanban",
		"silt-attachments",
		"silt-tasks",
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
