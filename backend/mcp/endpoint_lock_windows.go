//go:build windows

package mcp

import (
	"os"

	"golang.org/x/sys/windows"
)

// withEndpointFileLock runs fn while holding an exclusive LockFileEx on path+".lock".
func withEndpointFileLock(path string, fn func() error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	var ol windows.Overlapped
	// Lock 1 byte from offset 0 (exclusive, blocking).
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
	if err != nil {
		return err
	}
	defer func() {
		var ol2 windows.Overlapped
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol2)
	}()
	return fn()
}
