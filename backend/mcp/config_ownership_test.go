package mcp

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"silt/backend/keyring"
)

func TestWriteEndpointFile_RefusesLivePeer(t *testing.T) {
	dir := t.TempDir()
	origAlive := aliveCheck
	t.Cleanup(func() { aliveCheck = origAlive })

	peerPID := 424242
	aliveCheck = func(pid int) bool { return pid == peerPID }

	// os.UserConfigDir on Windows reads APPDATA; on Unix HOME/.config or XDG_CONFIG_HOME.
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	_ = os.MkdirAll(filepath.Join(dir, "silt"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".config", "silt"), 0o755)

	epPath, err := EndpointFilePath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(epPath), 0o755)
	b, _ := json.Marshal(EndpointFile{Endpoint: "http://127.0.0.1:17999", Pid: peerPID})
	if err := os.WriteFile(epPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ClearEndpointFileForce)

	err = WriteEndpointFile("http://127.0.0.1:18000")
	if !errors.Is(err, errEndpointOwnedByOther) {
		t.Fatalf("WriteEndpointFile: want errEndpointOwnedByOther, got %v", err)
	}
	// Peer file intact.
	if got := ReadEndpointFile(); got != "http://127.0.0.1:17999" {
		t.Fatalf("peer endpoint clobbered: %q", got)
	}

	ClearEndpointFile()
	if got := ReadEndpointFile(); got != "http://127.0.0.1:17999" {
		t.Fatalf("ClearEndpointFile wiped live peer: %q", got)
	}

	// Stale peer PID may be replaced.
	aliveCheck = func(int) bool { return false }
	if err := WriteEndpointFile("http://127.0.0.1:18001"); err != nil {
		t.Fatalf("stale peer write: %v", err)
	}
	if got := ReadEndpointFile(); got != "http://127.0.0.1:18001" {
		t.Fatalf("got %q", got)
	}
}

func TestClearDiscovery_PreservesPeerPin(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	_ = os.MkdirAll(filepath.Join(dir, "silt"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".config", "silt"), 0o755)

	origAlive := aliveCheck
	t.Cleanup(func() { aliveCheck = origAlive })
	peerPID := 424243
	aliveCheck = func(pid int) bool { return pid == peerPID }

	epPath, err := EndpointFilePath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(epPath), 0o755)
	b, _ := json.Marshal(EndpointFile{Endpoint: "http://127.0.0.1:17998", Pid: peerPID})
	if err := os.WriteFile(epPath, b, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ClearEndpointFileForce)

	kr := keyring.NewFake()
	if err := StorePinnedEndpoint(kr, "http://127.0.0.1:17998"); err != nil {
		t.Fatal(err)
	}
	ClearDiscovery(kr)
	if got := LoadPinnedEndpoint(kr); got != "http://127.0.0.1:17998" {
		t.Fatalf("pin cleared despite live peer: %q", got)
	}
	if got := ReadEndpointFile(); got != "http://127.0.0.1:17998" {
		t.Fatalf("endpoint cleared despite live peer: %q", got)
	}
}

func TestWriteEndpointFile_LegacyNoPid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APPDATA", dir)
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)
	_ = os.MkdirAll(filepath.Join(dir, "silt"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".config", "silt"), 0o755)

	epPath, err := EndpointFilePath()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.MkdirAll(filepath.Dir(epPath), 0o755)
	// Legacy shape without pid.
	if err := os.WriteFile(epPath, []byte(`{"endpoint":"http://127.0.0.1:17887"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ClearEndpointFileForce)
	if err := WriteEndpointFile("http://127.0.0.1:17888"); err != nil {
		t.Fatalf("legacy overwrite: %v", err)
	}
	if got := ReadEndpointFile(); got != "http://127.0.0.1:17888" {
		t.Fatalf("got %q", got)
	}
	// Written record includes pid.
	raw, _ := os.ReadFile(epPath)
	var ef EndpointFile
	if err := json.Unmarshal(raw, &ef); err != nil {
		t.Fatal(err)
	}
	if ef.Pid != os.Getpid() {
		t.Fatalf("pid=%d want %d", ef.Pid, os.Getpid())
	}
}
