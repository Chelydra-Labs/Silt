package ai

import "testing"

// allProviders is the canonical list of provider-type values. When you add a
// new provider, append it here AND add a branch to providerCategory (the
// production routing function in service.go that Complete/Embed/ListModels/
// CompleteStream all switch on); the tests below then enforce that every known
// provider resolves to a non-empty category and that no two consts share a
// value.
var allProviders = []AIProviderType{
	ProviderLocal,
	ProviderOpenAICompatible,
	ProviderGoogle,
	ProviderAnthropic,
}

// TestProviderDispatchExhaustive fails if any known provider constant lacks a
// dispatch category, or if a dispatch path loses its backing provider. It
// exercises the PRODUCTION providerCategory (service.go) — the single source of
// truth the four dispatchers route through — so the guard cannot drift from
// real dispatch the way a parallel mirror could.
func TestProviderDispatchExhaustive(t *testing.T) {
	seen := map[string]bool{}
	for _, p := range allProviders {
		cat := providerCategory(p)
		if cat == "" {
			t.Errorf("provider %q has no dispatch branch — add it to providerCategory", p)
			continue
		}
		seen[cat] = true
	}
	for _, want := range []string{"openai-compatible", "google", "anthropic"} {
		if !seen[want] {
			t.Errorf("dispatch path %q is not backed by any provider — was a provider removed without updating the routing?", want)
		}
	}
}

// TestAllProvidersDistinct guards against two consts accidentally sharing a
// string value, which would collapse dispatch and silently misroute one of them.
func TestAllProvidersDistinct(t *testing.T) {
	seen := map[AIProviderType]bool{}
	for _, p := range allProviders {
		if seen[p] {
			t.Errorf("duplicate provider-type value %q — two consts share a string", p)
		}
		seen[p] = true
	}
}
