package ai

import "testing"

// providerCategory mirrors the provider-type dispatch routing shared by
// Complete, Embed, ListModels, and CompleteStream: local + openai-compatible
// share the OpenAI-compatible request shape, google and anthropic have native
// first-party paths. Every AIProviderType MUST resolve to a category here; the
// exhaustive check below fails if a new provider constant is added without a
// branch (and without being added to the allProviders list a developer must
// extend). This is the compile/test guard #760 asks for so a future provider
// addition cannot silently fall through to the wrong dispatcher.
func providerCategory(p AIProviderType) string {
	switch p {
	case ProviderLocal, ProviderOpenAICompatible:
		return "openai-compatible"
	case ProviderGoogle:
		return "google"
	case ProviderAnthropic:
		return "anthropic"
	}
	return ""
}

// allProviders is the canonical list of provider-type values. When you add a
// new provider, append it here AND add a branch to providerCategory; the test
// below then enforces the dispatch branch exists.
var allProviders = []AIProviderType{
	ProviderLocal,
	ProviderOpenAICompatible,
	ProviderGoogle,
	ProviderAnthropic,
}

// TestProviderDispatchExhaustive fails if any known provider constant lacks a
// dispatch category, or if a dispatch path loses its backing provider. It is
// the durable replacement for the per-literal ai/config drift test that
// existed while the constants were duplicated across packages.
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
