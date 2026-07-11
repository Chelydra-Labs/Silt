package main

import (
	"sync"
	"time"
)

// PluginSecurityStats is the per-plugin rolled-up denial / rate-limit counter
// for Settings → Plugins observability (#518). Session-scoped memory only —
// not persisted (ARCHITECTURE §0: not reproducible from markdown).
type PluginSecurityStats struct {
	PluginID     string `json:"pluginId"`
	Denials      int    `json:"denials"`
	RateLimited  int    `json:"rateLimited"`
	LastDenialAt int64  `json:"lastDenialAt,omitempty"` // unix ms
	LastRateAt   int64  `json:"lastRateAt,omitempty"`   // unix ms
	LastCap      string `json:"lastCapability,omitempty"`
}

// SecurityEvent is the payload of the Wails `security:event` emission (#518).
type SecurityEvent struct {
	PluginID     string `json:"pluginId"`
	Kind         string `json:"kind"` // "capability_denied" | "rate_limited"
	Capability   string `json:"capability,omitempty"`
	Denials      int    `json:"denials"`
	RateLimited  int    `json:"rateLimited"`
	At           int64  `json:"at"` // unix ms
}

// pluginSecurityStats is an in-memory per-plugin counter map.
type pluginSecurityStats struct {
	mu   sync.Mutex
	byID map[string]*PluginSecurityStats
}

func newPluginSecurityStats() *pluginSecurityStats {
	return &pluginSecurityStats{byID: make(map[string]*PluginSecurityStats)}
}

func (s *pluginSecurityStats) getOrCreate(pluginID string) *PluginSecurityStats {
	st, ok := s.byID[pluginID]
	if !ok {
		st = &PluginSecurityStats{PluginID: pluginID}
		s.byID[pluginID] = st
	}
	return st
}

func (s *pluginSecurityStats) all() []PluginSecurityStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PluginSecurityStats, 0, len(s.byID))
	for _, st := range s.byID {
		if st.Denials == 0 && st.RateLimited == 0 {
			continue
		}
		out = append(out, *st)
	}
	return out
}

func (s *pluginSecurityStats) evict(pluginID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byID, pluginID)
}

func (s *pluginSecurityStats) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byID = make(map[string]*PluginSecurityStats)
}

func (s *pluginSecurityStats) recordDenial(pluginID, capability string) PluginSecurityStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreate(pluginID)
	st.Denials++
	st.LastDenialAt = time.Now().UnixMilli()
	st.LastCap = capability
	return *st
}

func (s *pluginSecurityStats) recordRateLimit(pluginID string) PluginSecurityStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.getOrCreate(pluginID)
	st.RateLimited++
	st.LastRateAt = time.Now().UnixMilli()
	return *st
}

// recordCapabilityDenied increments the denial counter and emits security:event.
// Safe to call while holding other App locks (uses its own mutex; emit is noop in tests).
func (a *App) recordCapabilityDenied(pluginID, capability string) {
	if a.securityStats == nil || pluginID == "" || pluginID == "<invalid>" {
		return
	}
	st := a.securityStats.recordDenial(pluginID, capability)
	a.emit("security:event", SecurityEvent{
		PluginID:    pluginID,
		Kind:        "capability_denied",
		Capability:  capability,
		Denials:     st.Denials,
		RateLimited: st.RateLimited,
		At:          st.LastDenialAt,
	})
}

// recordRateLimited increments the rate-limit counter and emits security:event.
func (a *App) recordRateLimited(pluginID string) {
	if a.securityStats == nil || pluginID == "" {
		return
	}
	st := a.securityStats.recordRateLimit(pluginID)
	a.emit("security:event", SecurityEvent{
		PluginID:    pluginID,
		Kind:        "rate_limited",
		Denials:     st.Denials,
		RateLimited: st.RateLimited,
		At:          st.LastRateAt,
	})
}

// GetPluginSecurityStats returns session aggregates for plugins that have been
// denied a capability or rate-limited at least once (#518). Host Settings UI only.
func (a *App) GetPluginSecurityStats() []PluginSecurityStats {
	if a.securityStats == nil {
		return nil
	}
	return a.securityStats.all()
}
