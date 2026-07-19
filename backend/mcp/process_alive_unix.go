//go:build unix

package mcp

import (
	"os"
	"syscall"
)

// processAlive reports whether pid looks like a running process.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks existence without delivering a signal.
	err = p.Signal(syscall.Signal(0))
	return err == nil
}
