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
//
// Known gap: an in-place writer (no temp+rename) that keeps the byte size and
// restores the mtime forges a clean stat on Windows — creation time does not
// move on in-place overwrites, so all four stat fields match the baseline and
// the edit is only caught if it lands inside the racy window. Temp+rename
// writers (the common sync clients) always advance creation time and stay
// caught. Closing this would require hashing on every save.
func statCtime(info os.FileInfo) (int64, int64) {
	if d, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		// base syscall.Filetime has Nanoseconds, not Unix/Nanosecond (those
		// live on golang.org/x/sys/windows.Filetime).
		ns := d.CreationTime.Nanoseconds()
		return ns / 1e9, ns % 1e9
	}
	return 0, 0
}
