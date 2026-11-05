package main

// Native clipboard HTML read for the context-menu Paste action.
//
// Wails' own clipboard API is text-only, and the Linux webview cannot use the
// Async Clipboard API for HTML (Wails' GTK permission handler hard-denies
// navigator.clipboard.read()). The frontend therefore probes this binding as
// its second HTML source and degrades to text if it is missing or fails, so the
// contract here is deliberately narrow: "" with nil error means "the clipboard
// holds no HTML" (including an empty clipboard), and a non-nil error means the
// read itself failed and the caller should paste text instead.

// ReadClipboardHTML returns the clipboard's text/html target, or "" when the
// clipboard advertises no HTML.
func (a *App) ReadClipboardHTML() (string, error) {
	return readHTMLNative()
}

// readHTMLNative is the OS seam. It is a variable so the plumbing above is
// testable without a GTK display; readHTMLClipboard is defined per platform.
var readHTMLNative = readHTMLClipboard
