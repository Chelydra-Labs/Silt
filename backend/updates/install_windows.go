//go:build windows

package updates

import (
	"fmt"
	"os/exec"
	"syscall"
)

// installForCurrentOS launches the verified NSIS installer detached from the
// Silt process so it survives the app's exit and can replace the locked
// binary files. The installer is a GUI process that prompts the user through
// the upgrade; returning nil here lets the caller call runtime.Quit so the
// installer's file replacement does not collide with open handles.
//
// CREATE_NEW_PROCESS_GROUP (0x00000200) decouples the child from the parent's
// process group, so a Ctrl-C / window-close on Silt does not propagate to the
// installer mid-upgrade.
func installForCurrentOS(localPath string) error {
	cmd := exec.Command(localPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000200}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("launch installer: %w", err)
	}
	// Release the child so it is not reaped when Silt exits; the installer
	// runs to completion independently.
	_ = cmd.Process.Release()
	return nil
}
