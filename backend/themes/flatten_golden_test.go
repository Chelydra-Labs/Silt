package themes

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
)

// flattenGoldensDir resolves frontend/src/theme/__fixtures__/flatten-goldens
// relative to this test file (repo layout: backend/themes → ../../frontend/...).
func flattenGoldensDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Join(filepath.Dir(file), "..", "..", "frontend", "src", "theme", "__fixtures__", "flatten-goldens")
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

func sortedFlattenJSON(flat map[string]string) ([]byte, error) {
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make(map[string]string, len(flat))
	for _, k := range keys {
		ordered[k] = flat[k]
	}
	return json.MarshalIndent(ordered, "", "  ")
}

// TestFlatten_GoldenParity pins Go Theme.Flatten for every embedded theme ×
// dark/light against committed golden files shared with the FE mirror
// (frontend flatten.golden.test.ts). Update with:
//
//	UPDATE_GOLDENS=1 go test ./backend/themes/ -run TestFlatten_GoldenParity
func TestFlatten_GoldenParity(t *testing.T) {
	outDir := flattenGoldensDir(t)
	update := os.Getenv("UPDATE_GOLDENS") == "1"
	if update {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	list, err := EmbeddedThemes()
	if err != nil {
		t.Fatalf("EmbeddedThemes: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("no embedded themes")
	}

	for _, th := range list {
		for _, mode := range []string{"dark", "light"} {
			th, mode := th, mode
			t.Run(th.ID+"/"+mode, func(t *testing.T) {
				flat := th.Flatten(mode)
				got, err := sortedFlattenJSON(flat)
				if err != nil {
					t.Fatal(err)
				}
				got = append(got, '\n')
				path := filepath.Join(outDir, th.ID+"."+mode+".json")
				if update {
					if err := os.WriteFile(path, got, 0o644); err != nil {
						t.Fatal(err)
					}
					return
				}
				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden %s: %v (run UPDATE_GOLDENS=1 go test ./backend/themes/ -run TestFlatten_GoldenParity)", path, err)
				}
				if string(got) != string(want) {
					t.Errorf("Flatten drift for %s %s\n--- got ---\n%s\n--- want ---\n%s\n(re-run with UPDATE_GOLDENS=1 after intentional Flatten changes)", th.ID, mode, got, want)
				}
			})
		}
	}
}
