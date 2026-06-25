//go:build linux

package updates

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// installForCurrentOS replaces the running AppImage in place and relaunches
// it. Linux permits renaming over a running file: the live process keeps its
// mapped inode, so the swap is atomic from the user's perspective. After the
// rename, the new AppImage is relaunched and Silt returns so the caller quits.
//
// If $APPIMAGE is unset (the user runs the .deb build, a bare binary, etc.),
// the verified asset is opened with xdg-open so the user's file manager /
// software center handles placement — Silt cannot self-replace a package-managed
// install safely.
func installForCurrentOS(localPath string) error {
	appImage := os.Getenv("APPIMAGE")
	if appImage == "" {
		return openWithXdg(localPath)
	}

	// The new AppImage must be executable to relaunch.
	if err := os.Chmod(localPath, 0o755); err != nil {
		return fmt.Errorf("chmod new AppImage: %w", err)
	}
	// Rename-over: atomic on the same filesystem. The temp file is on the OS
	// temp dir which is the same FS as the user's install in the common case;
	// if not, fall back to a copy+remove.
	if err := replaceFile(appImage, localPath); err != nil {
		return fmt.Errorf("replace AppImage: %w", err)
	}

	// Relaunch the new version detached, then let the caller quit.
	cmd := exec.Command(appImage)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("relaunch AppImage: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

// openWithXdg hands the downloaded asset to the desktop's default handler
// (file manager / software center). This is the non-AppImage fallback.
func openWithXdg(localPath string) error {
	abs, err := filepath.Abs(localPath)
	if err != nil {
		return fmt.Errorf("resolve asset path: %w", err)
	}
	if err := exec.Command("xdg-open", abs).Start(); err != nil {
		return fmt.Errorf("xdg-open asset: %w", err)
	}
	return nil
}

// replaceFile atomically swaps dst with src when they share a filesystem, and
// falls back to copy+remove across filesystems (os.Rename fails with EXDEV on
// a cross-device rename; we detect any rename failure and copy instead so the
// swap still completes, just non-atomically).
func replaceFile(dst, src string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	// Cross-device (or other rename error): copy then remove.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	return os.Remove(src)
}
