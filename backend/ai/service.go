// Package ai is Silt's core AI service: a thin, stdlib-only proxy that turns
// plugin chat-completion and embedding requests into OpenAI-compatible HTTP
// calls against a user-configured provider (Ollama, llama.cpp server,
// OpenRouter, LM Studio, OpenAI, …) and normalizes the failure modes into a
// typed result.
//
// Design notes (Sprint 20 / #216):
//
//   - ONE request shape for both provider types. Ollama, llama-server,
//     OpenRouter, LM Studio, and OpenAI all expose OpenAI-compatible
//     /v1/chat/completions + /v1/embeddings. AIProviderConfig.ProviderType only
//     nudges the default base URL and whether a Bearer token is expected; the
//     request/response body is the OpenAI-compatible shape in every case.
//   - The service holds NO credentials. The caller (the App binding layer)
//     resolves the API key (config.yaml in Phase 1; OS-keyring-first in #218)
//     and passes it via CompleteRequest / EmbedRequest, so this package never
//     imports the keyring or config packages.
//   - Errors are normalized into AIError so plugin JS gets an actionable Kind
//     ("unauthorized", "rate-limited", "model-missing", …) instead of a raw
//     status code or transport string.
//   - Request and response bodies are size-capped so a runaway plugin cannot
//     drive unbounded allocation (defense in depth, mirroring maxPluginFetchBytes).
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// MaxRequestBytes bounds the JSON body sent to a provider (the serialized
// messages / input texts). Mirrors maxPluginFetchRequestBytes: generous for real
// prompts, bounded so a plugin cannot stream a multi-hundred-MB body through
// the host.
const MaxRequestBytes = 10 * 1024 * 1024 // 10 MB

// MaxResponseBytes bounds a provider response body before it is parsed. Chat
// completions are small; embedding batches dominate (e.g. 1024 dims × 100 texts
// ≈ 800 KB). 50 MB covers large embedding batches while still bounding memory.
const MaxResponseBytes = 50 * 1024 * 1024 // 50 MB

// DefaultTimeout is applied when AIProviderConfig.TimeoutMs is unset. LLM
// completions are slow, so this is generous, but bounded so a dead endpoint
// cannot hang a plugin call indefinitely. Mirrors config.DefaultAITimeoutMs.
const DefaultTimeout = 60 * time.Second

// httpClient is the dedicated client for all AI provider calls. It carries a
// CheckRedirect that rejects cross-host redirects AND same-host scheme
// downgrades (https→http), so a compromised or misconfigured endpoint cannot
// redirect the request (bearing the Authorization: Bearer <key> header) to a
// different host or carry it over plaintext where it would leak. Same-host
// same-scheme redirects (and http→https upgrades) are allowed (load balancers,
// path normalization). A dedicated client (not http.DefaultClient) also
// isolates AI calls from any global transport changes.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		prev := via[len(via)-1].URL
		if req.URL.Host != prev.Host {
			return fmt.Errorf("ai: refused cross-host redirect from %s to %s", prev.Host, req.URL.Host)
		}
		// Same host, but never follow an https→http downgrade: the bearer token
		// must not cross a plaintext hop even if the attacker controls only the
		// redirect response (e.g. a stripped TLS redirect on a hijacked path).
		if prev.Scheme == "https" && req.URL.Scheme == "http" {
			return fmt.Errorf("ai: refused scheme downgrade from https to http on host %s", prev.Host)
		}
		return nil
	},
}

// validateBaseURL checks the resolved endpoint for a valid http(s) scheme.
// Called by Complete and Embed as defense in depth (the binding layer also
// validates before persisting).
func validateBaseURL(baseURL string) error {
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		return fmt.Errorf("base URL must start with http:// or https://")
	}
	return nil
}

// ChatMessage is one message in a chat-completion conversation. The OpenAI
// roles "system" / "user" / "assistant" are the only ones every provider honors.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// CompleteRequest is the input to Complete. Messages is required and non-empty;
// the rest override the provider's configured defaults for this call only.
type CompleteRequest struct {
	Provider        AIProvider    // resolved endpoint + key + model + advanced knobs
	Messages        []ChatMessage `json:"messages"`
	Model           string        `json:"model,omitempty"`            // override Provider.Model for this call
	Temperature     *float64      `json:"temperature,omitempty"`      // override Provider.Temperature
	MaxTokens       *int          `json:"max_tokens,omitempty"`       // override Provider.MaxTokens
	ReasoningEffort *string       `json:"reasoning_effort,omitempty"` // override Provider.ReasoningEffort ("none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max")
	Stream          bool          `json:"stream,omitempty"`           // signature present now; Sprint 22 delivers streaming
}

// CompleteResult is the output of a successful (non-streaming) completion. Usage
// is optional because not every provider returns token counts.
type CompleteResult struct {
	Content string   `json:"content"`
	Model   string   `json:"model"`
	Usage   *AIUsage `json:"usage,omitempty"`
}

// EmbedRequest is the input to Embed. Texts is required and non-empty; the
// whole batch is sent in a single OpenAI-compatible /v1/embeddings request.
type EmbedRequest struct {
	Provider   AIProvider `json:"-"`
	Texts      []string   `json:"input"`
	Model      string     `json:"model,omitempty"`      // override Provider.Model
	Dimensions *int       `json:"dimensions,omitempty"` // override Provider.Dimensions (truncation)
}

// EmbedResult is the output of a successful embedding batch. Embeddings[i] is
// the vector for Texts[i]. Dimensions is the per-vector length actually returned
// (the provider's native length, or the requested truncation length).
type EmbedResult struct {
	Embeddings [][]float64 `json:"embeddings"`
	Model      string      `json:"model"`
	Dimensions int         `json:"dimensions"`
	Usage      *AIUsage    `json:"usage,omitempty"`
}

// AIUsage mirrors the OpenAI usage object subset every compatible provider
// returns (some omit it entirely, hence pointers).
type AIUsage struct {
	PromptTokens     *int `json:"prompt_tokens,omitempty"`
	CompletionTokens *int `json:"completion_tokens,omitempty"`
	TotalTokens      *int `json:"total_tokens,omitempty"`
}

// AIProvider is the resolved endpoint configuration handed to the service. It is
// the AIProviderConfig from backend/config with the API key already resolved by
// the caller (config.yaml in Phase 1; OS keyring-first in #218). The App
// binding layer constructs one of these directly from a config.AIProviderConfig
// before calling Complete/Embed/Probe — keeping this package free of any import
// on config (or the keyring) so the service is unit-testable with httptest and
// no vault.
type AIProvider struct {
	ProviderType    string // "local" | "openai-compatible"
	BaseURL         string // e.g. http://localhost:11434
	APIKey          string // resolved by caller; "" for a keyless local endpoint
	Model           string
	ReasoningEffort *string // "none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max"; nil = omit
	TimeoutMs       *int
}

// AIErrorKind enumerates the normalized failure categories a plugin can act on.
type AIErrorKind string

const (
	// ErrUnreachable — DNS resolution or TCP connection failed (provider down,
	// wrong host, offline).
	ErrUnreachable AIErrorKind = "unreachable"
	// ErrTimeout — the call exceeded its deadline (context canceled/deadline
	// exceeded). Distinct from unreachable so the UI can suggest "slow model"
	// vs "wrong endpoint".
	ErrTimeout AIErrorKind = "timeout"
	// ErrUnauthorized — 401. The configured key is missing, wrong, or expired.
	ErrUnauthorized AIErrorKind = "unauthorized"
	// ErrForbidden — 403. The key is valid but lacks permission for the model.
	ErrForbidden AIErrorKind = "forbidden"
	// ErrRateLimited — 429. The provider throttled the call; retry with backoff.
	ErrRateLimited AIErrorKind = "rate-limited"
	// ErrModelMissing — 404. The model name is not available at this endpoint
	// (typo, not pulled, wrong provider).
	ErrModelMissing AIErrorKind = "model-missing"
	// ErrBadRequest — 400. The provider rejected the payload (bad dimensions,
	// unsupported param, malformed messages).
	ErrBadRequest AIErrorKind = "bad-request"
	// ErrServer — 5xx. The provider errored server-side; transient.
	ErrServer AIErrorKind = "server"
	// ErrUnknown — anything not classified above.
	ErrUnknown AIErrorKind = "unknown"
)

// AIError is the structured error returned by the service. It is JSON-
// serializable so the frontend SDK surfaces an actionable Kind + Message rather
// than a raw status code. Kind serializes as "code" to match the plugin SDK
// contract (rejections are keyed by `code`); the Go field name is unchanged.
// Status is 0 for transport-level failures.
type AIError struct {
	Kind    AIErrorKind `json:"code"`
	Status  int         `json:"status"`
	Message string      `json:"message"`
}

// Error implements error.
func (e *AIError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("ai: %s (HTTP %d): %s", e.Kind, e.Status, e.Message)
	}
	return fmt.Sprintf("ai: %s: %s", e.Kind, e.Message)
}

// httpDo builds the request, attaches auth, sends it with the resolved timeout,
// and returns the decoded raw body. Centralizes timeout/auth/size handling so
// Complete and Embed share one code path. The caller decodes `raw` into its own
// response shape.
func httpDo(ctx context.Context, method, url, apiKey string, timeoutMs *int, reqBody []byte) ([]byte, int, *AIError) {
	if len(reqBody) > MaxRequestBytes {
		return nil, 0, &AIError{Kind: ErrBadRequest, Message: fmt.Sprintf("request body exceeds %d-byte cap", MaxRequestBytes)}
	}
	timeout := DefaultTimeout
	if timeoutMs != nil && *timeoutMs > 0 {
		timeout = time.Duration(*timeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(reqBody))
	if err != nil {
		// Malformed URL — surface as unreachable; the caller's base URL is bad.
		return nil, 0, &AIError{Kind: ErrUnreachable, Message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	// Ollama's OpenAI-compatible routes accept any non-empty Bearer token; cloud
	// providers require a real key. Sending an empty bearer would 401 on cloud,
	// so only attach the header when a key is present (a keyless local endpoint
	// stays truly anonymous).
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Distinguish timeout from generic transport failure so the UI can hint
		// "slow model / raise timeout" vs "endpoint unreachable".
		if ctx.Err() == context.DeadlineExceeded {
			return nil, 0, &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("request timed out after %s: %v", timeout, err)}
		}
		return nil, 0, &AIError{Kind: ErrUnreachable, Message: err.Error()}
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if readErr != nil {
		return nil, resp.StatusCode, &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: fmt.Sprintf("read response: %v", readErr)}
	}
	if int64(len(raw)) > MaxResponseBytes {
		return nil, resp.StatusCode, &AIError{Kind: ErrServer, Status: resp.StatusCode, Message: fmt.Sprintf("response body exceeds %d-byte cap", MaxResponseBytes)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Trim the provider's error body to a reasonable length for the message.
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		return nil, resp.StatusCode, &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: msg}
	}
	return raw, resp.StatusCode, nil
}

// classifyStatus maps an HTTP status to a normalized AIErrorKind. Centralized so
// Complete and Embed agree on the taxonomy.
func classifyStatus(status int) AIErrorKind {
	switch {
	case status == 401:
		return ErrUnauthorized
	case status == 403:
		return ErrForbidden
	case status == 429:
		return ErrRateLimited
	case status == 404:
		return ErrModelMissing
	case status >= 400 && status < 500:
		return ErrBadRequest
	case status >= 500:
		return ErrServer
	default:
		return ErrUnknown
	}
}

// chatRequest is the OpenAI-compatible /v1/chat/completions request body.
type chatRequest struct {
	Model           string        `json:"model"`
	Messages        []ChatMessage `json:"messages"`
	Temperature     *float64      `json:"temperature,omitempty"`
	MaxTokens       *int          `json:"max_tokens,omitempty"`
	ReasoningEffort *string       `json:"reasoning_effort,omitempty"`
	Stream          bool          `json:"stream,omitempty"`
}

// chatResponse is the OpenAI-compatible /v1/chat/completions response body.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens     *int `json:"prompt_tokens"`
		CompletionTokens *int `json:"completion_tokens"`
		TotalTokens      *int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// Complete performs a chat completion against the provider's
// /v1/chat/completions endpoint. Streaming is NOT implemented (Sprint 22); the
// Stream field is accepted and forwarded so the signature is additive when
// streaming lands, but a provider that would stream is asked not to (stream is
// only set true when the caller explicitly opts in, and even then this path
// returns the buffered non-streamed body).
func Complete(ctx context.Context, req CompleteRequest) (CompleteResult, error) {
	if len(req.Messages) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: "messages must not be empty"}
	}
	model := req.Model
	if model == "" {
		model = req.Provider.Model
	}
	if model == "" {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: "no model configured"}
	}
	baseURL := strings.TrimRight(req.Provider.BaseURL, "/")
	if baseURL == "" {
		return CompleteResult{}, &AIError{Kind: ErrUnreachable, Message: "no base URL configured"}
	}
	if err := validateBaseURL(baseURL); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrBadRequest, Message: err.Error()}
	}
	// Resolve effective reasoning effort: per-call override, else provider default.
	reasoning := req.ReasoningEffort
	if reasoning == nil {
		reasoning = req.Provider.ReasoningEffort
	}
	body, err := json.Marshal(chatRequest{
		Model:           model,
		Messages:        req.Messages,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		ReasoningEffort: reasoning,
		Stream:          false, // see doc comment — streaming lands in Sprint 22
	})
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	raw, _, aiErr := httpDo(ctx, http.MethodPost, baseURL+"/v1/chat/completions", req.Provider.APIKey, req.Provider.TimeoutMs, body)
	if aiErr != nil {
		return CompleteResult{}, aiErr
	}
	var resp chatResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	if len(resp.Choices) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no choices"}
	}
	out := CompleteResult{Content: resp.Choices[0].Message.Content, Model: resp.Model}
	if resp.Usage != nil {
		out.Usage = &AIUsage{
			PromptTokens:     resp.Usage.PromptTokens,
			CompletionTokens: resp.Usage.CompletionTokens,
			TotalTokens:      resp.Usage.TotalTokens,
		}
	}
	return out, nil
}

// embedRequest is the OpenAI-compatible /v1/embeddings request body.
type embedRequest struct {
	Model      string   `json:"model"`
	Input      []string `json:"input"`
	Dimensions *int     `json:"dimensions,omitempty"`
}

// embedResponse is the OpenAI-compatible /v1/embeddings response body.
type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
	} `json:"data"`
	Model string `json:"model"`
	Usage *struct {
		PromptTokens *int `json:"prompt_tokens"`
		TotalTokens  *int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// Embed computes embeddings for a batch of texts against the provider's
// /v1/embeddings endpoint. The whole batch is sent in a single request (the
// OpenAI-compatible API accepts an input array); embeddings[i] corresponds to
// texts[i].
func Embed(ctx context.Context, req EmbedRequest) (EmbedResult, error) {
	if len(req.Texts) == 0 {
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: "texts must not be empty"}
	}
	model := req.Model
	if model == "" {
		model = req.Provider.Model
	}
	if model == "" {
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: "no model configured"}
	}
	baseURL := strings.TrimRight(req.Provider.BaseURL, "/")
	if baseURL == "" {
		return EmbedResult{}, &AIError{Kind: ErrUnreachable, Message: "no base URL configured"}
	}
	if err := validateBaseURL(baseURL); err != nil {
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: err.Error()}
	}
	body, err := json.Marshal(embedRequest{Model: model, Input: req.Texts, Dimensions: req.Dimensions})
	if err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	raw, _, aiErr := httpDo(ctx, http.MethodPost, baseURL+"/v1/embeddings", req.Provider.APIKey, req.Provider.TimeoutMs, body)
	if aiErr != nil {
		return EmbedResult{}, aiErr
	}
	var resp embedResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	if len(resp.Data) == 0 {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no embeddings"}
	}
	// Embeddings[i] ↔ Texts[i] is the contract; a partial response would
	// silently misalign vectors with inputs, so any count mismatch is fatal.
	if len(resp.Data) != len(req.Texts) {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("provider returned %d embeddings for %d texts", len(resp.Data), len(req.Texts))}
	}
	out := EmbedResult{
		Embeddings: make([][]float64, len(resp.Data)),
		Model:      resp.Model,
		Dimensions: len(resp.Data[0].Embedding),
	}
	for i, d := range resp.Data {
		out.Embeddings[i] = d.Embedding
	}
	if resp.Usage != nil {
		out.Usage = &AIUsage{PromptTokens: resp.Usage.PromptTokens, TotalTokens: resp.Usage.TotalTokens}
	}
	return out, nil
}

// Probe performs the minimal call the AI Provider page's "Test connection" uses:
// a 1-token chat completion (chat) or a single short embed (embedding). It
// returns nil on success and an *AIError (normalized) on failure. isChat selects
// the probe kind so one binding serves both provider blocks.
func Probe(ctx context.Context, p AIProvider, isChat bool) error {
	if isChat {
		// No MaxTokens cap: some reasoning endpoints reject an extremely low
		// token budget (treating it as an impossible request) and would fail
		// "Test connection" for a config that actually works in normal use.
		// The provider timeout already bounds the probe's wall-clock cost.
		_, err := Complete(ctx, CompleteRequest{
			Provider: p,
			Messages: []ChatMessage{{Role: "user", Content: "ping"}},
		})
		return err
	}
	_, err := Embed(ctx, EmbedRequest{Provider: p, Texts: []string{"ping"}})
	return err
}
