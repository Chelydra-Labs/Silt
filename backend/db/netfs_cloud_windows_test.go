//go:build windows

package db

import (
	"path/filepath"
	"testing"
)

// clearOneDriveEnv unsets the OneDrive env vars so a prior test (or the host
// machine) cannot leak a real OneDrive path into detection. Used in a sub-test
// that probes the home-relative fallback roots instead.
func clearOneDriveEnv(t *testing.T) {
	t.Helper()
	t.Setenv("OneDrive", "")
	t.Setenv("OneDriveConsumer", "")
	t.Setenv("OneDriveCommercial", "")
}

func TestDetectCloudSyncedFolder_OneDriveEnv(t *testing.T) {
	cloud := t.TempDir()
	t.Setenv("OneDrive", cloud)
	t.Setenv("OneDriveConsumer", "")
	t.Setenv("OneDriveCommercial", "")

	vault := filepath.Join(cloud, "MyVault")
	provider, ok := DetectCloudSyncedFolder(vault)
	if !ok {
		t.Fatalf("expected detection for %q under OneDrive %q", vault, cloud)
	}
	if provider != "Microsoft OneDrive" {
		t.Errorf("provider = %q, want %q", provider, "Microsoft OneDrive")
	}

	// A path outside the cloud root must not be flagged.
	other := filepath.Join(t.TempDir(), "elsewhere")
	if _, ok := DetectCloudSyncedFolder(other); ok {
		t.Errorf("expected no detection for path outside cloud root")
	}
}

func TestDetectCloudSyncedFolder_HomeFallbackRoots(t *testing.T) {
	// No OneDrive env vars: detection should still match a known root under
	// the home dir (Dropbox). Redirect USERPROFILE + LOCALAPPDATA so the host
	// machine's real folders cannot interfere.
	home := t.TempDir()
	clearOneDriveEnv(t)
	t.Setenv("USERPROFILE", home)
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))

	dropbox := filepath.Join(home, "Dropbox")
	vault := filepath.Join(dropbox, "vault")
	provider, ok := DetectCloudSyncedFolder(vault)
	if !ok {
		t.Fatalf("expected Dropbox detection via home fallback root")
	}
	if provider != "Dropbox" {
		t.Errorf("provider = %q, want %q", provider, "Dropbox")
	}

	// A non-cloud folder under home is not flagged.
	plain := filepath.Join(home, "Plain", "vault")
	if _, ok := DetectCloudSyncedFolder(plain); ok {
		t.Errorf("expected no detection for a non-cloud home folder")
	}
}
