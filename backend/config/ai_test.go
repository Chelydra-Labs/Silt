package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaults_AI(t *testing.T) {
	d := Defaults()
	// AI ships unconfigured: local providers pointing at the Ollama default,
	// with keyring storage ON (#218). No model is preselected — the provider
	// page's empty-state nudge fires until the user picks one.
	if d.AI.UseKeyring == nil || !*d.AI.UseKeyring {
		t.Errorf("defaults use_keyring should be true, got %+v", d.AI.UseKeyring)
	}
	if d.AI.Chat.ProviderType != AIProviderLocal {
		t.Errorf("defaults chat provider_type = %q, want %q", d.AI.Chat.ProviderType, AIProviderLocal)
	}
	if d.AI.Chat.BaseURL != DefaultAIBaseURL {
		t.Errorf("defaults chat base_url = %q, want %q", d.AI.Chat.BaseURL, DefaultAIBaseURL)
	}
	if d.AI.Embedding.ProviderType != AIProviderLocal {
		t.Errorf("defaults embedding provider_type = %q, want %q", d.AI.Embedding.ProviderType, AIProviderLocal)
	}
	if d.AI.Chat.Model != "" || d.AI.Embedding.Model != "" {
		t.Errorf("defaults should ship no model configured: chat=%q embedding=%q", d.AI.Chat.Model, d.AI.Embedding.Model)
	}
}

func TestNormalizeAIConfig(t *testing.T) {
	t.Run("UseKeyringNilDefaultsTrue", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{}) // UseKeyring nil
		if out.UseKeyring == nil || !*out.UseKeyring {
			t.Errorf("nil UseKeyring should normalize to true")
		}
	})
	t.Run("ExplicitUseKeyringFalsePreserved", func(t *testing.T) {
		f := false
		out := NormalizeAIConfig(AIConfig{UseKeyring: &f})
		if out.UseKeyring == nil || *out.UseKeyring {
			t.Errorf("explicit false UseKeyring should be preserved, got %+v", out.UseKeyring)
		}
	})
	t.Run("UnknownProviderTypeCollapsesToLocal", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: "bogus"}})
		if out.Chat.ProviderType != AIProviderLocal {
			t.Errorf("unknown provider_type = %q, want %q", out.Chat.ProviderType, AIProviderLocal)
		}
	})
	t.Run("EmptyBaseURLForLocalGetsDefault", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: AIProviderLocal}})
		if out.Chat.BaseURL != DefaultAIBaseURL {
			t.Errorf("empty local base_url = %q, want %q", out.Chat.BaseURL, DefaultAIBaseURL)
		}
	})
	t.Run("EmptyBaseURLForOpenAICompatStaysEmpty", func(t *testing.T) {
		// A cloud provider with no URL is a user error, but normalize must NOT
		// silently inject the Ollama localhost default (that would route a
		// cloud key to the local machine). The service surfaces the empty URL.
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: AIProviderOpenAICompatible}})
		if out.Chat.BaseURL != "" {
			t.Errorf("empty openai-compatible base_url should stay empty, got %q", out.Chat.BaseURL)
		}
	})
	t.Run("GoogleProviderTypePreserved", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: AIProviderGoogle}})
		if out.Chat.ProviderType != AIProviderGoogle {
			t.Errorf("google provider_type = %q, want %q", out.Chat.ProviderType, AIProviderGoogle)
		}
	})
	t.Run("GoogleDefaultBaseURL", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: AIProviderGoogle}})
		if out.Chat.BaseURL != DefaultGoogleBaseURL {
			t.Errorf("empty google base_url = %q, want %q", out.Chat.BaseURL, DefaultGoogleBaseURL)
		}
	})
	t.Run("AnthropicProviderTypePreserved", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: AIProviderAnthropic}})
		if out.Chat.ProviderType != AIProviderAnthropic {
			t.Errorf("anthropic provider_type = %q, want %q", out.Chat.ProviderType, AIProviderAnthropic)
		}
	})
	t.Run("AnthropicDefaultBaseURL", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ProviderType: AIProviderAnthropic}})
		if out.Chat.BaseURL != DefaultAnthropicBaseURL {
			t.Errorf("empty anthropic base_url = %q, want %q", out.Chat.BaseURL, DefaultAnthropicBaseURL)
		}
	})
	t.Run("ChatDropsDimensions", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{Dimensions: intPtr(128)}})
		if out.Chat.Dimensions != nil {
			t.Errorf("chat block should drop dimensions, got %+v", out.Chat.Dimensions)
		}
	})
	t.Run("EmbeddingDropsTemperatureAndMaxTokens", func(t *testing.T) {
		f := 0.7
		out := NormalizeAIConfig(AIConfig{Embedding: AIProviderConfig{Temperature: &f, MaxTokens: intPtr(100)}})
		if out.Embedding.Temperature != nil || out.Embedding.MaxTokens != nil {
			t.Errorf("embedding block should drop temperature/max_tokens, got temp=%+v max=%+v", out.Embedding.Temperature, out.Embedding.MaxTokens)
		}
	})
	t.Run("TimeoutNegativeClampedToZero", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{TimeoutMs: intPtr(-5)}})
		if out.Chat.TimeoutMs == nil || *out.Chat.TimeoutMs != 0 {
			t.Errorf("negative timeout should clamp to 0, got %+v", out.Chat.TimeoutMs)
		}
	})
	t.Run("TimeoutOverHardCapClamped", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{TimeoutMs: intPtr(999999)}})
		if out.Chat.TimeoutMs == nil || *out.Chat.TimeoutMs != 300000 {
			t.Errorf("oversize timeout should clamp to 300000, got %+v", out.Chat.TimeoutMs)
		}
	})
	t.Run("NonPositiveDimensionsDropped", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Embedding: AIProviderConfig{Dimensions: intPtr(0)}})
		if out.Embedding.Dimensions != nil {
			t.Errorf("non-positive dimensions should be dropped, got %+v", out.Embedding.Dimensions)
		}
	})
	t.Run("ValidReasoningEffortPreservedAndTrimmed", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ReasoningEffort: stringPtr("  high  ")}})
		if out.Chat.ReasoningEffort == nil || *out.Chat.ReasoningEffort != "high" {
			t.Errorf("valid reasoning_effort should be trimmed+kept, got %+v", out.Chat.ReasoningEffort)
		}
	})
	t.Run("UnknownReasoningEffortDropped", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Chat: AIProviderConfig{ReasoningEffort: stringPtr("hig")}})
		if out.Chat.ReasoningEffort != nil {
			t.Errorf("unknown reasoning_effort should be dropped, got %+v", out.Chat.ReasoningEffort)
		}
	})
	t.Run("EmbeddingDropsReasoningEffort", func(t *testing.T) {
		out := NormalizeAIConfig(AIConfig{Embedding: AIProviderConfig{ReasoningEffort: stringPtr("high")}})
		if out.Embedding.ReasoningEffort != nil {
			t.Errorf("embedding block should drop reasoning_effort, got %+v", out.Embedding.ReasoningEffort)
		}
	})
}

// TestIsValidAIReasoningEffort locks the documented enum so a value added to
// the SDK/docs without updating the validator is caught here, not at a provider.
func TestIsValidAIReasoningEffort(t *testing.T) {
	valid := []string{"none", "minimal", "low", "medium", "high", "xhigh", "max"}
	for _, v := range valid {
		if !IsValidAIReasoningEffort(v) {
			t.Errorf("expected %q to be valid", v)
		}
	}
	for _, v := range []string{"", "hig", "MAX", "ultra", "0", "off"} {
		if IsValidAIReasoningEffort(v) {
			t.Errorf("expected %q to be INVALID (exact match, no trim)", v)
		}
	}
}

// TestAI_APIKeyNeverSerializedToJSON verifies the json:"-" tag holds: a config
// with a key round-trips through JSON marshaling with the key stripped, so a
// GetSystemConfig → frontend → SaveSystemConfig path can never leak it. (The
// server-side SaveSystemConfig key-preservation is covered in app_ai_test.go.)
func TestAI_APIKeyNeverSerializedToJSON(t *testing.T) {
	cfg := Defaults()
	cfg.AI.Chat.APIKey = "super-secret"
	// encoding/json is what Wails uses to ship the struct over IPC.
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "super-secret") {
		t.Errorf("API key leaked into JSON output:\n%s", b)
	}
}

// TestAI_APIKeyPersistsToYAML verifies the yaml tag holds: a key IS written to
// config.yaml (the migration/fallback slot), so a pre-#218 vault keeps working
// and the #218 migration has a source to read from.
func TestAI_APIKeyPersistsToYAML(t *testing.T) {
	dir := t.TempDir()
	cfg := Defaults()
	cfg.AI.Chat.APIKey = "yaml-secret"
	if err := Save(dir, cfg); err != nil {
		t.Fatalf("Save: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".system", "config.yaml"))
	if err != nil {
		t.Fatalf("read config.yaml: %v", err)
	}
	if !strings.Contains(string(raw), "yaml-secret") {
		t.Errorf("API key should be present in config.yaml (the fallback slot):\n%s", raw)
	}
	// And a Load should bring it back.
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AI.Chat.APIKey != "yaml-secret" {
		t.Errorf("loaded key = %q, want yaml-secret", loaded.AI.Chat.APIKey)
	}
}
