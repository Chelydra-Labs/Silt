package mcp

import (
	"context"
	"testing"
)

// TestHost_NegotiatesProtocol20251125 pins the stateful HTTP transport to
// protocol 2025-11-25. go-sdk 1.7.0 serves the 2026-07-28 protocol only in
// stateless mode (SEP-2575); this host stays stateful, so any client — old or
// new — must negotiate down and still round-trip tools over the real loopback
// transport.
func TestHost_NegotiatesProtocol20251125(t *testing.T) {
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: freePort(t)}
	_, _, cs := startHTTPHost(t, &fakeBridge{path: t.TempDir()}, cfg)

	if got := cs.InitializeResult().ProtocolVersion; got != "2025-11-25" {
		t.Fatalf("negotiated protocol = %q, want %q", got, "2025-11-25")
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools over negotiated stateful session: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("ListTools returned no tools over the negotiated session")
	}
}
