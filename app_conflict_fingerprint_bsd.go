//go:build !linux && !windows

package main

import (
	"os"
	"syscall"
)

// statCtime extracts the inode-change time on POSIX platforms whose Stat_t
// spells the field Ctimespec (darwin, the BSDs). Linux has its own file.
func statCtime(info os.FileInfo) (int64, int64) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ctimespec.Sec, stat.Ctimespec.Nsec
	}
	return 0, 0
}
