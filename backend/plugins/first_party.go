package plugins

// first_party.go holds the reserved set of first-party (bundled) plugin IDs.
// It lives in package plugins (not package main) so plugins.Validate can
// reach it at install time and reject a third-party archive whose id collides
// with a bundled plugin (#240, audit F5). The frontend loader's getFirstParty
// skip remains as defense-in-depth; this install-time gate is the primary one.
//
// Keep in sync with frontend/src/plugins/registry.ts (the bundled-component
// registry) — every id registered there MUST appear here. If a first-party
// plugin is ever split out of the bundle intentionally, remove its id here at
// the same time and re-grant via the standard capability-prompt flow.

// FirstPartyPluginIDs is the set of plugin ids reserved for bundled plugins.
// A third-party archive whose manifest id appears in this set is rejected at
// install time so an impostor cannot shadow a bundled plugin (which would
// either confuse the user with a duplicate Settings entry or, should the
// bundle ever drop the id, inherit the first-party grants seeded by
// seedFirstPartyGrants).
var FirstPartyPluginIDs = map[string]bool{
	"silt-agenda":      true,
	"silt-calendar":    true,
	"silt-kanban":      true,
	"silt-attachments": true,
}

// IsFirstPartyID reports whether pluginID is a reserved (bundled) plugin id.
// The match is exact — near-collisions like "silt-kanban2" or "silts-kanban"
// do NOT match, so the gate does not over-broadly reject legitimate ids that
// merely share a prefix.
func IsFirstPartyID(pluginID string) bool {
	return FirstPartyPluginIDs[pluginID]
}
