//go:build darwin

package db

import (
	"os"
	"path/filepath"
	"strings"
)

// detectCloudSyncedFolder checks whether path lives under a known macOS
// cloud-sync root. macOS 13+ (Ventura) places File Provider mounts under
// ~/Library/CloudStorage/<provider>, so the provider is resolved from the
// immediate child folder name. Legacy home-folder roots are a fallback.
func detectCloudSyncedFolder(path string) (provider string, ok bool) {
	for _, root := range darwinCloudRoots() {
		if pathWithin(path, root.dir) {
			return root.provider, true
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		cs := filepath.Join(home, "Library", "CloudStorage")
		if provider, matched := cloudStorageProvider(path, cs); matched {
			return provider, true
		}
	}
	return "", false
}

func darwinCloudRoots() []cloudRoot {
	var roots []cloudRoot
	if home, err := os.UserHomeDir(); err == nil {
		addRoot(&roots, filepath.Join(home, "Google Drive"), "Google Drive")
		addRoot(&roots, filepath.Join(home, "Dropbox"), "Dropbox")
	}
	return roots
}

// cloudStorageProvider resolves the provider for a path under a macOS
// ~/Library/CloudStorage directory from its immediate child folder name
// (e.g. "iCloud Drive", "OneDrive", "OneDrive – Contoso", "Dropbox").
func cloudStorageProvider(path, cloudStorageDir string) (provider string, ok bool) {
	pa, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(strings.ToLower(filepath.Clean(cloudStorageDir)), strings.ToLower(filepath.Clean(pa)))
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if strings.HasPrefix(rel, "..") {
		return "", false // outside CloudStorage
	}
	child := rel
	if i := strings.Index(child, "/"); i >= 0 {
		child = child[:i]
	}
	if child == "" || child == "." {
		// path is CloudStorage itself.
		return "a cloud-synced folder", true
	}
	return mapCloudProvider(child), true
}

// mapCloudProvider maps a macOS CloudStorage child folder name to a provider.
func mapCloudProvider(child string) string {
	l := strings.ToLower(child)
	switch l {
	case "icloud drive", "iclouddrive":
		return "Apple iCloud"
	case "dropbox":
		return "Dropbox"
	case "google drive", "googledrive":
		return "Google Drive"
	}
	// OneDrive business tenants carry a suffix ("OneDrive – Contoso").
	if strings.HasPrefix(l, "onedrive") {
		return "Microsoft OneDrive"
	}
	return "a cloud-synced folder"
}
