package themes

// Theme is the parsed canonical theme. It mirrors the JSON schema in
// DESIGN.md §2.1 / SPECS.md §6.4 exactly: a modes-based object with
// hue-agnostic semantic accents. See themes/cyber_forest.json for the
// canonical example.
type Theme struct {
	SchemaVersion string `json:"schema_version"`
	ID            string `json:"id"`
	Name          string `json:"name"`
	Author        string `json:"author"`
	Description   string `json:"description"`
	Modes         Modes  `json:"modes"`
}

// Modes holds the per-appearance token sets. Both dark and light are
// required for a theme to be valid.
type Modes struct {
	Dark  Mode `json:"dark"`
	Light Mode `json:"light"`
}

// Mode is one appearance (dark or light) of a theme.
type Mode struct {
	BG     BG     `json:"bg"`
	Border Border `json:"border"`
	Text   Text   `json:"text"`
	Accent Accent `json:"accent"`
	Status Status `json:"status"`
}

// BG is the canvas/background scale.
type BG struct {
	Void    string `json:"void"`
	Surface string `json:"surface"`
	Panel   string `json:"panel"`
	Hover   string `json:"hover"`
	Active  string `json:"active"`
}

// Border is the hairline-isolation scale.
type Border struct {
	Muted  string `json:"muted"`
	Zinc   string `json:"zinc"`
	Active string `json:"active"`
	Focus  string `json:"focus"`
}

// Text is the foreground type scale.
type Text struct {
	Primary  string `json:"primary"`
	Muted    string `json:"muted"`
	Disabled string `json:"disabled"`
}

// Accent holds the two semantic accents (primary = "go/done", secondary =
// "in progress"). Components reference only the semantic names; each theme
// maps its concrete hues onto them.
type Accent struct {
	Primary   AccentTriple `json:"primary"`
	Secondary AccentTriple `json:"secondary"`
}

// AccentTriple is a start/end/glow gradient triple.
type AccentTriple struct {
	Start string `json:"start"`
	End   string `json:"end"`
	Glow  string `json:"glow"`
}

// Status holds warn/danger semantic colors.
type Status struct {
	Warn   string `json:"warn"`
	Danger string `json:"danger"`
}

// Flatten produces the flat map of CSS custom-property names → values for
// the given mode ("dark" or "light"). The keys are exactly the names the
// frontend injects on :root (--bg-void, --accent-primary-start, …). An
// unknown mode falls back to "dark".
func (t *Theme) Flatten(mode string) map[string]string {
	m := t.Modes.Dark
	if mode == "light" {
		m = t.Modes.Light
	}
	out := map[string]string{}
	out["--bg-void"] = m.BG.Void
	out["--bg-surface"] = m.BG.Surface
	out["--bg-panel"] = m.BG.Panel
	out["--bg-hover"] = m.BG.Hover
	out["--bg-active"] = m.BG.Active

	out["--border-muted"] = m.Border.Muted
	out["--border-zinc"] = m.Border.Zinc
	out["--border-active"] = m.Border.Active
	out["--border-focus"] = m.Border.Focus

	out["--text-primary"] = m.Text.Primary
	out["--text-muted"] = m.Text.Muted
	out["--text-disabled"] = m.Text.Disabled

	out["--accent-primary-start"] = m.Accent.Primary.Start
	out["--accent-primary-end"] = m.Accent.Primary.End
	out["--accent-primary-glow"] = m.Accent.Primary.Glow

	out["--accent-secondary-start"] = m.Accent.Secondary.Start
	out["--accent-secondary-end"] = m.Accent.Secondary.End
	out["--accent-secondary-glow"] = m.Accent.Secondary.Glow

	out["--status-warn"] = m.Status.Warn
	out["--status-danger"] = m.Status.Danger

	return out
}

// BGVoid returns the resolved bg.void for the given mode, used to set the
// native webview BackgroundColour without a full flatten round-trip.
func (t *Theme) BGVoid(mode string) string {
	if mode == "light" {
		return t.Modes.Light.BG.Void
	}
	return t.Modes.Dark.BG.Void
}

// ThemeInfo is the lightweight metadata returned by ListThemes for the
// picker UI (#47) and the active-theme summary.
type ThemeInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Author      string   `json:"author"`
	Description string   `json:"description"`
	Swatches    []string `json:"swatches"` // preview color hexes (primary/secondary start)
	Source      string   `json:"source"`   // "disk" or "default" (embedded fallback)
}

// AsInfo converts a parsed Theme into the lightweight ThemeInfo, deriving
// preview swatches from the dark-mode accent starts.
func (t *Theme) AsInfo(source string) ThemeInfo {
	return ThemeInfo{
		ID:          t.ID,
		Name:        t.Name,
		Author:      t.Author,
		Description: t.Description,
		Swatches:    []string{t.Modes.Dark.Accent.Primary.Start, t.Modes.Dark.Accent.Secondary.Start},
		Source:      source,
	}
}
