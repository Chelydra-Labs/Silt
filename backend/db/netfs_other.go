//go:build !linux && !windows

package db

// detectNetworkFilesystem is a no-op on unsupported platforms. The WAL
// journal_mode assert in initSchema is the belt-and-suspenders fallback.
func detectNetworkFilesystem(_ string) error {
	return nil
}

// detectNetworkFilesystemForWrite is the same no-op: unsupported platforms
// (macOS among them — Windows + Linux are the release targets) have no
// extended write detection either (#1020 scope).
func detectNetworkFilesystemForWrite(path string) error {
	return detectNetworkFilesystem(path)
}
