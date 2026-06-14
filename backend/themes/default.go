// Package themes owns Silt's canonical theme schema, the embedded default
// theme, and (see loader.go/validate.go) the on-disk theme loader + validator.
//
// The canonical default theme is embedded in the binary so the app always has
// a guaranteed-correct fallback: it works before a vault exists, when the
// themes directory has been wiped, and when a user-selected theme id is
// missing or invalid. ScaffoldVault writes this same embedded JSON when it
// bootstraps a new vault, so there is a single source of truth for the
// default theme's content.
package themes

import _ "embed"

// DefaultThemeID is the id of the bundled default theme. It is the value used
// when AppSettings.ActiveTheme is empty/invalid and the fallback applies.
const DefaultThemeID = "cyber_forest"

//go:embed themes/cyber_forest.json
var defaultThemeJSON []byte

// DefaultThemeJSON returns the raw canonical default theme JSON. Callers that
// need the parsed form should use the loader (LoadTheme / ParseDefault).
func DefaultThemeJSON() []byte {
	// Return a copy so callers cannot mutate the embedded bytes.
	out := make([]byte, len(defaultThemeJSON))
	copy(out, defaultThemeJSON)
	return out
}
