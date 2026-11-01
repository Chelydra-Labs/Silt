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
		// base syscall.Filetime has Nanoseconds, not Unix/Nanosecond (those
		// live on golang.org/x/sys/windows.Filetime).
		ns := d.CreationTime.Nanoseconds()
		return ns / 1e9, ns % 1e9
	}
	return 0, 0
}
