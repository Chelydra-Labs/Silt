package main

import "silt/backend/plugins"

// PluginListPageVersions returns retained snapshots for a page, newest first.
// Session-token verified. List/preview stay ungated beyond a valid session.
func (a *App) PluginListPageVersions(pluginID, sessionToken, notebook, section, page string) ([]PageVersionInfo, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return nil, err
	}
	return a.ListPageVersions(notebook, section, page)
}

// PluginGetPageVersion returns the stored markdown body (no frontmatter).
// Session-token verified. Does not mutate the live page.
func (a *App) PluginGetPageVersion(pluginID, sessionToken, notebook, section, page, versionID string) (string, error) {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return "", err
	}
	return a.GetPageVersion(notebook, section, page, versionID)
}

// PluginRestorePageVersion replaces the live page body with a stored version.
// Session-token verified and gated by content-mutate.
func (a *App) PluginRestorePageVersion(pluginID, sessionToken, notebook, section, page, versionID string) error {
	if err := a.validatePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapContentMutate); err != nil {
		return err
	}
	return a.RestorePageVersion(notebook, section, page, versionID)
}
