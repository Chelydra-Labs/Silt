//go:build linux && gtk3

package main

import "errors"

// readHTMLClipboard is unimplemented for the opt-in GTK3 build. GDK3 has no
// async clipboard read API (gtk_clipboard_wait_for_contents blocks on the owner
// with no way to bound it), and the GTK4 implementation cannot be shared with
// it. The frontend treats this error as "no HTML" and pastes text, so a GTK3
// build keeps working with reduced paste fidelity.
func readHTMLClipboard() (string, error) {
	return "", errors.New("clipboard HTML read requires the GTK4 build (rebuild without the gtk3 tag)")
}
