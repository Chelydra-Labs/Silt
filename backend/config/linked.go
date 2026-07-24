package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
	"silt/backend/safeio"
)

// LinkedConfigPath returns the absolute path to a linked notebook's
// co-located config.yaml. Per the storage-of-truth model (#133), data
// attached to a notebook travels with the notebook: for a linked (external)
// notebook, per-notebook plugin overrides live at
// `<linkedRoot>/.system/config.yaml`, so an external notebook on SharePoint
// carries its own config with it — not in the vault. Silt treats this file
// as READ-ONLY / user-authored; plugin settings continue to persist to the
// vault-scoped config.yaml via the atomic UpdatePluginSetting path. The
// co-located file is purely an override layer the user authors on the
// external mount.
func LinkedConfigPath(linkedRoot string) string {
	return filepath.Join(linkedRoot, ".system", "config.yaml")
}

// LoadLinked reads a linked notebook's co-located `<linkedRoot>/.system/
// config.yaml` (#133). A missing file is NOT an error: it returns Defaults()
// with a nil error, because a linked notebook without a co-located config is
// the normal case (the vault-scoped config.yaml still provides the baseline).
// A file that exists but fails to parse returns Defaults() with a wrapped
// error — the caller MUST surface this so the user can fix the source rather
// than silently inheriting defaults. Mirrors Load's decode-over-Defaults
// semantics so omitted sections keep their default values.
//
// Unlike Load, this does NOT run migrateLegacyQuickAccessCollapsed: a linked
// config is a read-only override layer whose UI fields are never merged into
// the active sidebar state (only MergePluginSettings is consumed, §3.1), so a
// legacy quick_access_collapsed key here is inert and migration is moot.
func LoadLinked(linkedRoot string) (SystemConfig, error) {
	path := LinkedConfigPath(linkedRoot)
	data, err := safeio.ReadFileMax(path, maxConfigYAMLBytes)
	if err != nil {
		if os.IsNotExist(err) {
			return Defaults(), nil
		}
		return Defaults(), fmt.Errorf("failed to read linked config.yaml: %w", err)
	}
	cfg := Defaults()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Defaults(), fmt.Errorf("failed to parse linked config.yaml: %w", err)
	}
	return normalize(cfg), nil
}

// MergePluginSettings deep-merges two per-plugin settings maps for the
// co-located config override layer (#133). `vault` is the plugin's entry in
// the vault-scoped config.yaml; `linked` is the plugin's entry in the linked
// notebook's co-located config.yaml. The result is a NEW map (vault is not
// mutated) where:
//   - keys present ONLY in vault are preserved;
//   - keys present ONLY in linked are added;
//   - keys present in BOTH are merged: nested `map[string]any` values merge
//     recursively (linked's sub-keys override vault's per-key); scalars and
//     arrays from linked REPLACE vault's.
//
// This mirrors the user expectation that "the notebook's value wins" without
// losing vault defaults the notebook did not override. Both inputs may be nil
// (treated as empty); the result is always non-nil.
func MergePluginSettings(vault, linked map[string]any) map[string]any {
	out := make(map[string]any, len(vault)+len(linked))
	for k, v := range vault {
		out[k] = cloneValue(v)
	}
	for k, lv := range linked {
		if rv, ok := out[k]; ok {
			if rmap, rOK := rv.(map[string]any); rOK {
				if lmap, lOK := lv.(map[string]any); lOK {
					out[k] = mergeMaps(rmap, lmap)
					continue
				}
			}
		}
		out[k] = cloneValue(lv)
	}
	return out
}

// mergeMaps returns a new map that is `a` deep-merged with `b` (b wins per
// key, nested maps recurse). Neither input is mutated.
func mergeMaps(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = cloneValue(v)
	}
	for k, bv := range b {
		if av, ok := out[k]; ok {
			if amap, aOK := av.(map[string]any); aOK {
				if bmap, bOK := bv.(map[string]any); bOK {
					out[k] = mergeMaps(amap, bmap)
					continue
				}
			}
		}
		out[k] = cloneValue(bv)
	}
	return out
}

// cloneValue returns a deep copy of the supported plugin-setting value forms.
// YAML values use map[string]any and []any; direct plugin updates may also use
// typed string maps/slices. Scalars are returned as-is because they are
// immutable.
func cloneValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, vv := range x {
			out[k] = cloneValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = cloneValue(vv)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(x))
		for k, vv := range x {
			out[k] = vv
		}
		return out
	case []string:
		return append([]string(nil), x...)
	default:
		return v
	}
}
