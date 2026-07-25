//go:build windows

package db

import (
	"os"
	"path/filepath"
)

// detectCloudSyncedFolder checks whether path lives under a known Windows
// cloud-sync root. OneDrive env vars take precedence (the client sets them and
// they cover business/personal tenants + custom install locations); known
// default roots are a fallback for when the env is unset.
func detectCloudSyncedFolder(path string) (provider string, ok bool) {
	for _, root := range windowsCloudRoots() {
		if pathWithin(path, root.dir) {
			return root.provider, true
		}
	}
	return "", false
}

func windowsCloudRoots() []cloudRoot {
	var roots []cloudRoot
	// OneDrive sets these on the process environment (personal + business).
	addRoot(&roots, os.Getenv("OneDrive"), "Microsoft OneDrive")
	addRoot(&roots, os.Getenv("OneDriveConsumer"), "Microsoft OneDrive")
	addRoot(&roots, os.Getenv("OneDriveCommercial"), "Microsoft OneDrive")
	// Known default roots (fallback when the env vars are absent).
	if home, err := os.UserHomeDir(); err == nil {
		addRoot(&roots, filepath.Join(home, "OneDrive"), "Microsoft OneDrive")
		addRoot(&roots, filepath.Join(home, "Google Drive"), "Google Drive")
		addRoot(&roots, filepath.Join(home, "Dropbox"), "Dropbox")
		addRoot(&roots, filepath.Join(home, "iCloudDrive"), "Apple iCloud")
	}
	// Google Drive for Desktop streams under %LOCALAPPDATA%\Google\Drive.
	// os.UserCacheDir() returns %LOCALAPPDATA% on Windows.
	if local, err := os.UserCacheDir(); err == nil {
		addRoot(&roots, filepath.Join(local, "Google", "Drive"), "Google Drive")
	}
	return roots
}
