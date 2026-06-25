//go:build !windows && !linux

package updates

import (
	"errors"
	"testing"
)

// TestInstall_UnsupportedOS asserts the non-Windows/non-Linux build path
// returns ErrPlatformNotSupported (macOS has no build leg). This file is
// excluded from the windows/linux builds where install_windows.go /
// install_linux.go supply installForCurrentOS.
func TestInstall_UnsupportedOS(t *testing.T) {
	err := Install("/tmp/anything")
	if !errors.Is(err, ErrPlatformNotSupported) {
		t.Fatalf("expected ErrPlatformNotSupported on unsupported OS, got %v", err)
	}
}
