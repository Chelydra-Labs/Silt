package vault

import (
	"testing"

	"silt/backend/types"
)

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
		})
	}
}
