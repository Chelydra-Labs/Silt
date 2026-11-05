//go:build !linux

package main

// readHTMLClipboard has no native implementation outside Linux: Windows and
// macOS webviews can read clipboard HTML through navigator.clipboard.read(),
// which the frontend tries before this binding. Reporting "no HTML" keeps the
// binding present (so the generated frontend surface is platform-independent)
// without claiming a capability those platforms do not need.
func readHTMLClipboard() (string, error) {
	return "", nil
}
