package main

import (
	"context"
	"fmt"
	"time"

	"silt/backend/updates"
	"silt/backend/vault"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// =========================================================================
// In-app update check + self-upgrade (#312)
// =========================================================================
//
// Thin Wails-bound wrappers over backend/updates. The package owns the HTTP,
// semver, download, and SHA256 logic; this file binds it to the App and emits
// progress via the same runtime.EventsEmit channel used by the archive import
// (vault:archive:progress). No token is embedded — unauthenticated reads of
// the public repo are sufficient at 24h-throttled desktop volumes (#312 AC7).

// updateProgressEvent is the Wails event name the frontend subscribes to for
// download progress. Payload: {received:int64, total:int64} (total is -1 when
// ContentLength is unknown).
const updateProgressEvent = "update:download:progress"

// UpdateSettingsResult is the frontend-facing view of the update preferences
// in settings.json. AutoCheck is the resolved default-on value (nil→true).
type UpdateSettingsResult struct {
	AutoCheck       bool   `json:"autoCheck"`
	LastCheckRFC3339 string `json:"lastCheck"` // empty when never checked
}

// CheckForUpdates queries GitHub Releases for the latest non-prerelease and
// reports whether the running version is older. On success it stamps
// LastUpdateCheck in settings.json so the startup auto-check throttle (24h)
// resets. A network/rate-limit/parse failure returns a wrapped error for the
// UI to surface as a quiet "couldn't check" state — it never crashes.
func (a *App) CheckForUpdates() (updates.UpdateInfo, error) {
	client := updates.NewClient(appVersion)
	info, err := client.CheckForUpdates(context.Background())
	if err != nil {
		return updates.UpdateInfo{}, err
	}
	// Stamp the check timestamp regardless of whether an update exists, so the
	// 24h throttle reflects "we just looked." A persist failure is non-fatal:
	// the check still succeeded; the next launch simply re-checks.
	_ = a.stampUpdateCheckTime()
	return info, nil
}

// DownloadUpdate downloads the chosen asset and verifies it against the
// release's SHA256SUMS before returning the local path. Progress is streamed
// to the frontend via the update:download:progress event. It NEVER returns a
// path for an unverified asset — a checksum mismatch or a SHA256SUMS fetch
// failure cleans up the temp file and returns the error.
//
// The asset URL is re-validated against a fresh /releases/latest fetch inside
// the package, so a stale or coerced URL cannot make Silt download + hand to
// InstallUpdate an arbitrary file.
func (a *App) DownloadUpdate(assetURL string) (string, error) {
	client := updates.NewClient(appVersion)
	emitProgress := func(received, total int64) {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, updateProgressEvent, map[string]any{
				"received": received,
				"total":    total,
			})
		}
	}
	return client.DownloadAndVerify(context.Background(), assetURL, emitProgress)
}

// InstallUpdate launches the verified local asset so it can replace the
// running binary, then the caller quits the app. The asset at localPath MUST
// have been verified by DownloadUpdate first; this binding intentionally does
// not re-verify (the file is already on disk and trusted from the prior step).
func (a *App) InstallUpdate(localPath string) error {
	return updates.Install(localPath)
}

// GetUpdateSettings returns the user's update preferences from settings.json.
// AutoCheck reflects the default-on resolution (absent → true).
func (a *App) GetUpdateSettings() (UpdateSettingsResult, error) {
	settings, err := vault.LoadSettings()
	if err != nil {
		return UpdateSettingsResult{}, fmt.Errorf("load settings: %w", err)
	}
	return UpdateSettingsResult{
		AutoCheck:        settings.AutoCheckUpdatesEnabled(),
		LastCheckRFC3339: settings.LastUpdateCheck,
	}, nil
}

// SetUpdateSettings persists the auto-check toggle. It preserves every other
// settings.json field (vault path, theme, trusted publishers) via the existing
// read-modify-write + atomic SaveSettings path.
func (a *App) SetUpdateSettings(autoCheck bool) error {
	settings, err := vault.LoadSettings()
	if err != nil {
		return fmt.Errorf("load settings: %w", err)
	}
	settings.AutoCheckUpdates = &autoCheck
	if err := vault.SaveSettings(settings); err != nil {
		return fmt.Errorf("save settings: %w", err)
	}
	return nil
}

// stampUpdateCheckTime records now as the last update-check time. Non-fatal on
// error: the check itself already succeeded and the throttle is best-effort.
func (a *App) stampUpdateCheckTime() error {
	settings, err := vault.LoadSettings()
	if err != nil {
		return err
	}
	settings.LastUpdateCheck = time.Now().UTC().Format(time.RFC3339)
	return vault.SaveSettings(settings)
}
