package updates

// Install launches the verified local asset so it can replace the running
// binary, then returns so the caller can quit the app. The exact mechanism is
// OS-specific (see install_windows.go / install_linux.go / install_other.go):
//
//   - windows: the NSIS installer .exe is started as a detached process; it
//     takes over the file replacement and prompts the user as needed.
//   - linux: if running from an AppImage ($APPIMAGE), the new AppImage is
//     renamed over the running file and relaunched; otherwise the asset is
//     opened with xdg-open so the user places it manually.
//   - other OSes (incl. darwin, which has no build leg): ErrPlatformNotSupported.
//
// Install does NOT verify the asset itself — the caller (the App binding) must
// run VerifySHA256Sums first. Launching an unverified file is a security
// regression, so this ordering is load-bearing.
func Install(localPath string) error {
	return installForCurrentOS(localPath)
}
