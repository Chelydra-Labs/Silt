package main

// app_local_author.go — host-side default for the per-vault local_author
// preference (#430). The Tasks hub's comment composer uses this to seed the
// author name on first run; the user can override via the preference and the
// override is persisted under plugins.plugin_settings.silt-tasks.local_author
// (ARCHITECTURE §0 rule 2: per-vault plugin prefs → YAML). Reads the same
// os/user source audit_trail.go uses for the audit log Actor field — single
// source of truth for "who is the local OS user" across the app.

// GetLocalAuthor returns the OS username Silt is running as, falling back to
// "unknown" when the host can't resolve it (e.g. cross-compiled without cgo
// on some platforms). Frontend uses this as the default for the local_author
// preference; the user's explicit pref (if set) always wins.
//
// The literal "unknown" placeholder is filtered to "" here so the comment
// composer prompts the user on first run rather than seeding the YAML with a
// permanent "unknown" attribution that the user would then have to find and
// delete.
func (a *App) GetLocalAuthor() string {
	name := auditActor()
	if name == "unknown" {
		return ""
	}
	return name
}
