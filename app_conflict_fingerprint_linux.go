//go:build linux

package main

import (
	"os"
	"syscall"
)

// statCtime extracts the inode-change time on Linux. Ctime advances on
// metadata changes (rename, chmod) that mtime can miss, tightening drift
// detection; the hash arbiter remains the final judge either way.
func statCtime(info os.FileInfo) (int64, int64) {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return stat.Ctim.Sec, stat.Ctim.Nsec
	}
	return 0, 0
}
