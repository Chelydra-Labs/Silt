package main

import (
	"encoding/json"
	"path/filepath"
	"silt/backend/plugins"
	"silt/backend/safeio"
	"sync"
	"time"
)

// maxRatelimitManifestBytes bounds the plugin.json read in
// resolvePluginRatelimit. It is already implicitly covered by the install-time
// 10 MB per-file cap, but the explicit read is bounded too so a hand-edited or
// corrupted plugin.json cannot drive unbounded allocation here (audit F12).
const maxRatelimitManifestBytes int64 = 64 << 10 // 64 KB

const defaultPluginFetchRPS = 1.0

// defaultPluginFetchBurst is the default bucket capacity (10 requests instantly,
// then throttled to rps).
const defaultPluginFetchBurst = 10

// First-party AI plugins (agent, assistant, QA, …) run multi-turn tool loops
// that legitimately burst many ctx.ai.complete calls. Fetch defaults (1 rps /
// burst 10) starve those loops; these defaults keep a host safety rail while
// matching interactive agent traffic.
const defaultFirstPartyAIRPS = 8.0
const defaultFirstPartyAIBurst = 40

// maxPluginFetchRPS is the hard cap on a manifest-declared rps override. A
// plugin cannot declare more than this; the host rejects it at install.
const maxPluginFetchRPS = 10.0

// maxPluginFetchBurst is the hard cap on a manifest-declared burst override.
// Mirrors the install-time validation in plugins.Validate.
const maxPluginFetchBurst = 100

// aiRateLimitMaxWait is how long AI preflight will sleep for a token before
// denying. Short enough for chat UX; long enough to absorb a full burst drain
// at first-party refill rates (~125ms/token at 8 rps).
const aiRateLimitMaxWait = 3 * time.Second

// tokenBucket is a standard token-bucket rate limiter. tokens refill at rps
// up to burst capacity. allow() consumes one token if available.
type tokenBucket struct {
	tokens float64
	last   time.Time
	rps    float64
	burst  int
}

// refill applies elapsed time into tokens (capped at burst).
func (tb *tokenBucket) refill(now time.Time) {
	elapsed := now.Sub(tb.last).Seconds()
	if elapsed > 0 {
		tb.tokens += elapsed * tb.rps
		if tb.tokens > float64(tb.burst) {
			tb.tokens = float64(tb.burst)
		}
		tb.last = now
	}
}

// allow reports whether one token is available, consuming it if so.
func (tb *tokenBucket) allow(now time.Time) bool {
	tb.refill(now)
	if tb.tokens >= 1 {
		tb.tokens--
		return true
	}
	return false
}

// timeUntilToken returns how long until at least one token is available.
// Zero when a token is already available. Assumes rps > 0.
func (tb *tokenBucket) timeUntilToken(now time.Time) time.Duration {
	tb.refill(now)
	if tb.tokens >= 1 {
		return 0
	}
	if tb.rps <= 0 {
		return time.Hour // degenerate; caller should treat as deny
	}
	need := 1 - tb.tokens
	secs := need / tb.rps
	// Small pad so the next allow() after sleep almost always succeeds.
	return time.Duration(secs*float64(time.Second)) + time.Millisecond
}

// pluginRateLimiter is a per-plugin token-bucket map guarded by a mutex.
// A network-granted plugin's fetch calls consult this before hitting the
// network (#153). Buckets are evicted on uninstall so uninstalled plugins
// don't leak entries.
type pluginRateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
}

func newPluginRateLimiter() *pluginRateLimiter {
	return &pluginRateLimiter{buckets: make(map[string]*tokenBucket)}
}

func (rl *pluginRateLimiter) bucketLocked(vaultPath, pluginID string) *tokenBucket {
	b, ok := rl.buckets[pluginID]
	if ok {
		return b
	}
	rps, burst := resolvePluginRatelimit(vaultPath, pluginID)
	now := time.Now()
	b = &tokenBucket{
		tokens: float64(burst),
		last:   now,
		rps:    rps,
		burst:  burst,
	}
	rl.buckets[pluginID] = b
	return b
}

// allow checks (and consumes) one token for pluginID. Returns false if the
// rate limit is exceeded. vaultPath is used only to resolve a manifest-declared
// ratelimit override (#153) the first time a plugin's bucket is created; the
// bucket is then cached, so the disk read happens at most once per plugin per
// session (and is evicted on uninstall).
func (rl *pluginRateLimiter) allow(vaultPath, pluginID string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.bucketLocked(vaultPath, pluginID).allow(time.Now())
}

// allowOrWait tries to consume one token. If the bucket is empty it waits up to
// maxWait for a refill (sleeping WITHOUT holding the limiter mutex), then
// retries. Returns (waited, true) on success or (0, false) when the wait would
// exceed maxWait or a token is still unavailable after waiting.
//
// Callers MUST NOT hold vaultMu while invoking this — the sleep can last
// several seconds and would stall vault close/switch.
func (rl *pluginRateLimiter) allowOrWait(vaultPath, pluginID string, maxWait time.Duration) (waited time.Duration, ok bool) {
	deadline := time.Now().Add(maxWait)
	for {
		rl.mu.Lock()
		b := rl.bucketLocked(vaultPath, pluginID)
		now := time.Now()
		if b.allow(now) {
			rl.mu.Unlock()
			return waited, true
		}
		wait := b.timeUntilToken(now)
		rl.mu.Unlock()

		if wait <= 0 {
			wait = time.Millisecond * 50
		}
		remaining := time.Until(deadline)
		if remaining <= 0 || wait > remaining {
			return waited, false
		}
		time.Sleep(wait)
		waited += wait
	}
}

// timeUntilAllow peeks how long until the next token without consuming.
func (rl *pluginRateLimiter) timeUntilAllow(vaultPath, pluginID string) time.Duration {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.bucketLocked(vaultPath, pluginID).timeUntilToken(time.Now())
}

// resolvePluginRatelimit reads the installed plugin's manifest ratelimit
// override (#153) and returns the effective (rps, burst). Returns the host
// defaults when vaultPath is empty, the plugin has no manifest on disk, or the
// declared values are out of range. First-party plugins get higher AI-oriented
// defaults when no valid manifest override is present.
//
// This is defense in depth — Install already validates the override — so a
// hand-edited or corrupted plugin.json falls back to the safe default instead
// of granting an outsized quota.
func resolvePluginRatelimit(vaultPath, pluginID string) (rps float64, burst int) {
	rps, burst = hostDefaultRatelimit(pluginID)
	if vaultPath == "" || !plugins.IsValidID(pluginID) {
		return
	}
	manifestPath := filepath.Join(vaultPath, ".system", "plugins", pluginID, "plugin.json")
	data, err := safeio.ReadFileMax(manifestPath, maxRatelimitManifestBytes)
	if err != nil {
		return
	}
	var raw struct {
		Ratelimit *struct {
			RPS   float64 `json:"rps"`
			Burst int     `json:"burst"`
		} `json:"ratelimit"`
	}
	if json.Unmarshal(data, &raw) != nil || raw.Ratelimit == nil {
		return
	}
	if raw.Ratelimit.RPS > 0 && raw.Ratelimit.RPS <= maxPluginFetchRPS {
		rps = raw.Ratelimit.RPS
	}
	if raw.Ratelimit.Burst > 0 && raw.Ratelimit.Burst <= maxPluginFetchBurst {
		burst = raw.Ratelimit.Burst
	}
	return
}

// hostDefaultRatelimit returns fetch defaults for third-party plugins and
// higher AI-oriented defaults for bundled first-party plugins.
func hostDefaultRatelimit(pluginID string) (rps float64, burst int) {
	if plugins.IsFirstPartyID(pluginID) {
		return defaultFirstPartyAIRPS, defaultFirstPartyAIBurst
	}
	return defaultPluginFetchRPS, defaultPluginFetchBurst
}

// evict removes the bucket for pluginID (called on uninstall/disable).
func (rl *pluginRateLimiter) evict(pluginID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.buckets, pluginID)
}
