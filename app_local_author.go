package main

import (
	"os/user"
	"strings"
)

// app_local_author.go — host-side default for the per-vault local_author
// preference (#430). Settings prefills the comment-author field with this on
// first run; the user can override via the preference and the override is
// persisted under plugins.plugin_settings.silt-tasks.local_author
// (ARCHITECTURE §0 rule 2: per-vault plugin prefs → YAML).

// localDisplayName returns the OS account's display name — user.User.Name,
// populated from the Windows "Full Name" account field / Linux GECOS —
// trimmed. Var so tests can pin GetLocalAuthor without depending on the host
// user; "" on error or an empty display name. On Windows, os/user bakes the
// SAM-compatible COMPUTER\user name into u.Name when the Full-Name lookup
// fails (stdlib fallback), so such accounts get that here rather than "".
// Separate from auditActor on purpose: the audit log's Actor field keeps
// username semantics, and sharing the lookup would couple the two contracts.
var localDisplayName = func() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(u.Name)
}

// GetLocalAuthor returns the local author seed for the local_author
// preference: the OS account display name when set (friendlier than a login
// handle), else the OS username via auditActor(). The user's explicit pref
// (if set) always wins.
//
// The literal "unknown" placeholder is filtered to "" here so callers treat
// "" as "still unresolved" rather than persisting a permanent "unknown"
// attribution.
func (a *App) GetLocalAuthor() string {
	if name := localDisplayName(); name != "" {
		return name
	}
	name := auditActor()
	if name == "unknown" {
		return ""
	}
	return name
}
