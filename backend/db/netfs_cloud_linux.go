//go:build linux

package db

import (
	"os"
	"path/filepath"
)

// detectCloudSyncedFolder checks whether path lives under a known Linux
// cloud-sync root. Linux clients default to home-relative folders; there are
// no standard env vars, so only the default roots are probed.
func detectCloudSyncedFolder(path string) (provider string, ok bool) {
	for _, root := range linuxCloudRoots() {
		if pathWithin(path, root.dir) {
			return root.provider, true
		}
	}
	return "", false
}

func linuxCloudRoots() []cloudRoot {
	var roots []cloudRoot
	if home, err := os.UserHomeDir(); err == nil {
		addRoot(&roots, filepath.Join(home, "OneDrive"), "Microsoft OneDrive")
		addRoot(&roots, filepath.Join(home, "Dropbox"), "Dropbox")
		addRoot(&roots, filepath.Join(home, "Google Drive"), "Google Drive")
		addRoot(&roots, filepath.Join(home, "GoogleDrive"), "Google Drive")
	}
	return roots
}
