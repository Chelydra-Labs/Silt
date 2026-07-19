package mcp

import (
	"sync"
	"testing"
	"time"

	"silt/backend/keyring"
)

// TestHost_ConcurrentStartStop exercises startMu serialization under -race.
func TestHost_ConcurrentStartStop(t *testing.T) {
	kr := keyring.NewFake()
	h := NewHost(Options{Keyring: kr, Auditor: &MemoryAuditor{}, Version: "test"})
	bridge := &fakeBridge{path: t.TempDir()}
	port := freePort(t)
	cfg := Config{Enabled: true, HTTPEnabled: true, HTTPPort: port}

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				_ = h.Start(bridge, cfg)
			} else {
				h.Stop()
			}
		}(i)
	}
	close(start)
	wg.Wait()
	// Leave host stopped.
	h.Stop()
	time.Sleep(20 * time.Millisecond)
}
