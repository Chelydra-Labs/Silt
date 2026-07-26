//go:build !linux && !darwin && !windows

package db

// detectCloudSyncedFolder is a no-op on unsupported platforms. Cloud-sync
// detection is best-effort and never blocks opening; the absence of a signal
// here simply means no cloud-sync warning is surfaced.
func detectCloudSyncedFolder(_ string) (provider string, ok bool) {
	return "", false
}
