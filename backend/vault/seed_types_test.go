package vault

import (
	"testing"

	"silt/backend/types"
)

// expectedExampleTypeIDs is the ordered eight-type pack (#914). Keep in sync
// with exampleTypeFiles and issue acceptance criteria.
var expectedExampleTypeIDs = []string{
	"book",
	"meeting",
	"person",
	"project",
	"decision",
	"one_on_one",
	"standup",
	"retrospective",
}

// TestSeedExampleTypesValid guards the seeded example types against future
// reserved-name collisions (e.g. a property named `date`/`type`/`tags`) that
// would make a fresh vault's seed fail ValidateTypeDef and never register —
// breaking typed-notes discoverability on first run.
func TestSeedExampleTypesValid(t *testing.T) {
	if len(exampleTypes) == 0 {
		t.Fatal("no example types seeded")
	}
	for name, content := range exampleTypes {
		t.Run(name, func(t *testing.T) {
			td, err := types.ParseTypeBytes([]byte(content), name)
			if err != nil {
				t.Fatalf("ParseTypeBytes(%s): %v", name, err)
			}
			if err := types.ValidateTypeDef(td); err != nil {
				t.Errorf("ValidateTypeDef(%s): %v", name, err)
			}
			if len(td.Properties) > 5 {
				t.Errorf("%s has %d properties, want ≤5", name, len(td.Properties))
			}
			if td.HeroField == "" {
				t.Errorf("%s missing heroField", name)
			}
			if td.Description == "" {
				t.Errorf("%s missing description", name)
			}
		})
	}
}

// TestExampleTypesOrderedPack pins the shipped eight-type set and order so
// restore/scaffold callers and UI empty-states stay aligned with #914.
func TestExampleTypesOrderedPack(t *testing.T) {
	got := ExampleTypes()
	if len(got) != len(expectedExampleTypeIDs) {
		t.Fatalf("ExampleTypes len = %d, want %d", len(got), len(expectedExampleTypeIDs))
	}
	for i, want := range expectedExampleTypeIDs {
		if got[i].ID != want {
			t.Errorf("ExampleTypes()[%d].ID = %q, want %q", i, got[i].ID, want)
		}
	}
	// Explicit non-goals: date journals and PARA extras must not ship as seeds.
	forbidden := []string{"daily", "weekly_review", "task", "area", "goal", "notes"}
	have := map[string]bool{}
	for _, td := range got {
		have[td.ID] = true
	}
	for _, id := range forbidden {
		if have[id] {
			t.Errorf("forbidden example type %q is seeded", id)
		}
	}
	if len(exampleTypes) != len(exampleTypeFiles) {
		t.Errorf("exampleTypes map size %d != exampleTypeFiles %d", len(exampleTypes), len(exampleTypeFiles))
	}
}
