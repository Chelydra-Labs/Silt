package main

import (
	"testing"

	"silt/backend/plugins"
)

func TestSecurityStats_DenialIncrements(t *testing.T) {
	app := NewApp()
	err := app.requireGrant("hostile-plugin", plugins.CapNetwork)
	if err == nil {
		t.Fatal("expected denial")
	}
	stats := app.GetPluginSecurityStats()
	if len(stats) != 1 {
		t.Fatalf("stats len = %d, want 1", len(stats))
	}
	if stats[0].PluginID != "hostile-plugin" || stats[0].Denials != 1 {
		t.Fatalf("stats = %+v", stats[0])
	}
	if stats[0].LastCap != string(plugins.CapNetwork) {
		t.Fatalf("LastCap = %q", stats[0].LastCap)
	}

	_ = app.requireGrant("hostile-plugin", plugins.CapAI)
	stats = app.GetPluginSecurityStats()
	if stats[0].Denials != 2 {
		t.Fatalf("denials = %d, want 2", stats[0].Denials)
	}
}

func TestSecurityStats_RateLimitIncrements(t *testing.T) {
	app := NewApp()
	app.recordRateLimited("noisy")
	app.recordRateLimited("noisy")
	stats := app.GetPluginSecurityStats()
	if len(stats) != 1 || stats[0].RateLimited != 2 {
		t.Fatalf("stats = %+v", stats)
	}
}

func TestSecurityStats_EvictAndClear(t *testing.T) {
	app := NewApp()
	app.recordCapabilityDenied("p1", "network")
	app.recordRateLimited("p2")
	app.securityStats.evict("p1")
	stats := app.GetPluginSecurityStats()
	if len(stats) != 1 || stats[0].PluginID != "p2" {
		t.Fatalf("after evict: %+v", stats)
	}
	app.securityStats.clear()
	if got := app.GetPluginSecurityStats(); len(got) != 0 {
		t.Fatalf("after clear: %+v", got)
	}
}

func TestSecurityStats_InvalidPluginIDNotCounted(t *testing.T) {
	app := NewApp()
	_ = app.requireGrant("../evil", plugins.CapNetwork)
	if got := app.GetPluginSecurityStats(); len(got) != 0 {
		t.Fatalf("invalid id should not be counted: %+v", got)
	}
}
