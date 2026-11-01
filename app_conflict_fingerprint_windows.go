//go:build windows

package main

import (
	"os"
	"syscall"
)

// statCtime maps the NTFS creation time onto the fingerprint's ctime slot.
// POSIX ctime has no exact Windows analogue; creation time is the closest
// drift signal. A zero value is fine — size+mtime already drive the fast path
// and the hash is the arbiter.
func statCtime(info os.FileInfo) (int64, int64) {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		return d.CreationTime.Unix(), int64(d.CreationTime.Nanosecond())
	}
	return 0, 0
}
