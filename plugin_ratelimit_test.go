package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Direct unit tests for the token-bucket refill and burst-cap logic. Existing
// tests (TestTokenBucket_AllowsBurstThenThrottles) cover the happy path; this
// covers the burst-cap and zero-token edge cases.
func TestTokenBucket_BurstCap(t *testing.T) {
	now := time.Now()
	tb := tokenBucket{
		tokens: 0,
		last:   now,
		rps:    10.0,
		burst:  3,
	}

	// After 10 seconds, 100 tokens would refill but burst caps at 3.
	later := now.Add(10 * time.Second)
	for i := 0; i < 3; i++ {
		if !tb.allow(later) {
			t.Errorf("request %d should be allowed (burst cap=3)", i)
		}
	}
	if tb.allow(later) {
		t.Error("4th should be denied (burst cap hit)")
	}
}

func TestTokenBucket_RefillRate(t *testing.T) {
	now := time.Now()
	tb := tokenBucket{
		tokens: 0,
		last:   now,
		rps:    2.0, // 2 tokens per second
		burst:  5,
	}

	// After 1 second, 2 tokens should refill.
	later := now.Add(1 * time.Second)
	if !tb.allow(later) {
		t.Error("should allow after 1s refill (2 tokens)")
	}
	if !tb.allow(later) {
		t.Error("should allow 2nd token after 1s refill")
	}
	if tb.allow(later) {
		t.Error("3rd should be denied (only 2 refilled)")
	}
}

// Tests that two plugins get independent buckets — exhaustion of one does not
// affect the other.
func TestPluginRateLimiter_SeparatePlugins(t *testing.T) {
	rl := newPluginRateLimiter()

	// Exhaust plugin-a's bucket.
	for i := 0; i < defaultPluginFetchBurst; i++ {
		rl.allow("", "plugin-a")
	}
	// plugin-b should still have its own bucket.
	if !rl.allow("", "plugin-b") {
		t.Error("plugin-b should have its own bucket (unaffected by plugin-a)")
	}
}

// Tests that resolvePluginRatelimit returns safe defaults for edge cases.
func TestResolvePluginRatelimit_EdgeCases(t *testing.T) {
	// Empty vaultPath returns third-party defaults.
	rps, burst := resolvePluginRatelimit("", "any-plugin")
	if rps != defaultPluginFetchRPS || burst != defaultPluginFetchBurst {
		t.Errorf("empty vaultPath should return defaults, got rps=%v burst=%v", rps, burst)
	}

	// First-party AI plugins get higher defaults (no manifest required).
	rps, burst = resolvePluginRatelimit("", "silt-ai-agent")
	if rps != defaultFirstPartyAIRPS || burst != defaultFirstPartyAIBurst {
		t.Errorf("silt-ai-agent defaults = rps=%v burst=%v, want %v/%v",
			rps, burst, defaultFirstPartyAIRPS, defaultFirstPartyAIBurst)
	}

	// Invalid plugin ID returns third-party defaults.
	rps, burst = resolvePluginRatelimit("/tmp", "../../../etc/passwd")
	if rps != defaultPluginFetchRPS || burst != defaultPluginFetchBurst {
		t.Errorf("invalid pluginID should return defaults, got rps=%v burst=%v", rps, burst)
	}

	// Non-existent third-party manifest returns third-party defaults.
	dir := t.TempDir()
	rps, burst = resolvePluginRatelimit(dir, "nonexistent-plugin")
	if rps != defaultPluginFetchRPS || burst != defaultPluginFetchBurst {
		t.Errorf("missing manifest should return defaults, got rps=%v burst=%v", rps, burst)
	}
}

func TestPluginRateLimiter_AllowOrWait(t *testing.T) {
	rl := newPluginRateLimiter()
	// Tiny bucket via a third-party id with no manifest (1 rps, burst 10).
	// Exhaust burst, then wait should succeed within 2s for one token.
	for i := 0; i < defaultPluginFetchBurst; i++ {
		if !rl.allow("", "wait-plugin") {
			t.Fatalf("burst allow %d failed", i)
		}
	}
	start := time.Now()
	waited, ok := rl.allowOrWait("", "wait-plugin", 2*time.Second)
	elapsed := time.Since(start)
	if !ok {
		t.Fatal("allowOrWait should succeed after short refill wait")
	}
	if waited < 50*time.Millisecond {
		t.Errorf("expected a non-trivial wait, got %v", waited)
	}
	if elapsed > 2*time.Second {
		t.Errorf("wait took too long: %v", elapsed)
	}
}

func TestPluginRateLimiter_AllowOrWaitDeniesWhenMaxWaitTooShort(t *testing.T) {
	rl := newPluginRateLimiter()
	for i := 0; i < defaultPluginFetchBurst; i++ {
		rl.allow("", "deny-wait")
	}
	// 1 rps → need ~1s for next token; 10ms maxWait must deny.
	_, ok := rl.allowOrWait("", "deny-wait", 10*time.Millisecond)
	if ok {
		t.Fatal("allowOrWait should deny when maxWait is shorter than refill")
	}
}

// TestResolvePluginRatelimit_OversizeManifestFallsBack pins F12: an oversized
// or hostile plugin.json is rejected by the read cap and resolve falls back to
// the host defaults rather than panicking or allocating unbounded bytes.
func TestResolvePluginRatelimit_OversizeManifestFallsBack(t *testing.T) {
	vault := t.TempDir()
	pluginDir := filepath.Join(vault, ".system", "plugins", "p")
	if err := os.MkdirAll(pluginDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), make([]byte, maxRatelimitManifestBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	rps, burst := resolvePluginRatelimit(vault, "p")
	if rps != defaultPluginFetchRPS || burst != defaultPluginFetchBurst {
		t.Errorf("oversize manifest should fall back to defaults, got rps=%v burst=%v", rps, burst)
	}
}

func TestHostDefaultRatelimit_FirstParty(t *testing.T) {
	rps, burst := hostDefaultRatelimit("silt-ai-agent")
	if rps != defaultFirstPartyAIRPS || burst != defaultFirstPartyAIBurst {
		t.Fatalf("got %v/%v", rps, burst)
	}
	rps, burst = hostDefaultRatelimit("third-party-x")
	if rps != defaultPluginFetchRPS || burst != defaultPluginFetchBurst {
		t.Fatalf("third-party got %v/%v", rps, burst)
	}
}
