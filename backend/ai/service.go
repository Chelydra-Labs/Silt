// Package ai is Silt's core AI service: a thin, stdlib-only proxy that turns
// plugin chat-completion and embedding requests into provider-specific HTTP
// calls and normalizes the failure modes into a typed result.
//
// Design notes (Sprint 20 / #216, native providers / #479):
//
//   - Complete/Embed are the stable entry points. They validate the shared
//     inputs (messages, model, base URL) then dispatch on ProviderType to a
//     per-provider encoder/decoder. The OpenAI-compatible path is the universal
//     default/fallback; native google + anthropic paths bypass the OpenAI
//     request shape and speak the provider's first-party API.
//   - The service holds NO credentials. The caller (the App binding layer)
//     resolves the API key (config.yaml or OS-keyring-first) and passes it via
//     CompleteRequest / EmbedRequest, so this package never imports the keyring
//     or config packages.
//   - Errors are normalized into AIError so plugin JS gets an actionable Kind
//     ("unauthorized", "rate-limited", "model-missing", …) instead of a raw
//     status code or transport string.
//   - Request and response bodies are size-capped so a runaway plugin cannot
//     drive unbounded allocation (defense in depth, mirroring maxPluginFetchBytes).
//   - All providers share one transport: timeout, redirect guard, size caps,
//     retry-with-backoff on transient 5xx/429, and HTTP-status classification.
//     Per-provider code owns only URL construction, auth headers, request-body
//     encoding, response-body decoding, and (optionally) structured-error
//     classification.
package ai

import (
	"bytes"
	"context"
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

// Provider-type discriminators used by the dispatcher. These string values MUST
// match config.AIProvider* — the App binding layer copies the configured value
// verbatim into AIProvider.ProviderType. Duplicated (rather than imported) so
// this package stays free of a config import, per the layering rule.
const (
	ProviderLocal            = "local"
	ProviderOpenAICompatible = "openai-compatible"
	ProviderGoogle           = "google"
	ProviderAnthropic        = "anthropic"
)

// httpClient is the dedicated client for all AI provider calls. It carries a
// CheckRedirect that rejects cross-host redirects AND same-host scheme
// downgrades (https→http), so a compromised or misconfigured endpoint cannot
// redirect the request (bearing an auth header) to a different host or carry
// it over plaintext where it would leak. Same-host same-scheme redirects (and
// http→https upgrades) are allowed (load balancers, path normalization). A
// dedicated client (not http.DefaultClient) also isolates AI calls from any
// global transport changes.
var httpClient = &http.Client{
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 {
			return nil
		}
		prev := via[len(via)-1].URL
		if req.URL.Host != prev.Host {
			return fmt.Errorf("ai: refused cross-host redirect from %s to %s", prev.Host, req.URL.Host)
		}
		// Same host, but never follow an https→http downgrade: the auth token
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
// roles "system" / "user" / "assistant" are the universal input shape; native
// providers translate as needed (e.g. system → top-level field).
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
// whole batch is sent in a single request.
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

// AIModel is one entry in a ListModels poll. ID is the value the user selects
// (and that gets stored as AIProviderConfig.Model); DisplayName is the
// human-readable label for the dropdown (falls back to ID when the provider's
// list endpoint carries no name field).
type AIModel struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

// AIProvider is the resolved endpoint configuration handed to the service. It is
// the AIProviderConfig from backend/config with the API key already resolved by
// the caller (config.yaml or OS keyring-first). The App binding layer constructs
// one of these directly from a config.AIProviderConfig before calling
// Complete/Embed/Probe — keeping this package free of any import on config (or
// the keyring) so the service is unit-testable with httptest and no vault.
type AIProvider struct {
	ProviderType    string // ProviderLocal | ProviderOpenAICompatible | ProviderGoogle | ProviderAnthropic
	BaseURL         string // e.g. http://localhost:11434
	APIKey          string // resolved by caller; "" for a keyless local endpoint
	Model           string
	ReasoningEffort *string // "none"|"minimal"|"low"|"medium"|"high"|"xhigh"|"max"; nil = omit (OpenAI-compat only)
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

// providerRequest is the per-provider output handed to the shared transport: the
// HTTP method, URL, serialized body, an optional header-setter (auth + provider
// specifics), and an optional error classifier. The transport builds a fresh
// *http.Request from these per attempt (so the body reader is never reused),
// applies the timeout + redirect guard + size caps, and retries transient
// failures. classifyErr, when set, parses a structured error body for richer
// fidelity than bare HTTP-status classification; returning nil falls back to the
// default status-based classification + trimmed body message.
type providerRequest struct {
	method      string
	url         string
	body        []byte
	setHeaders  func(req *http.Request)
	classifyErr func(raw []byte, status int) *AIError
}

// sendOnce builds and sends one attempt. It is the per-attempt half of
// sendWithRetry: timeout + request build + send + response size cap + status
// classification. buildReq is invoked once (a fresh request per attempt is the
// caller's responsibility in sendWithRetry).
func sendOnce(ctx context.Context, pr providerRequest, timeoutMs *int) ([]byte, int, *AIError) {
	timeout := DefaultTimeout
	if timeoutMs != nil && *timeoutMs > 0 {
		timeout = time.Duration(*timeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, pr.method, pr.url, bytes.NewReader(pr.body))
	if err != nil {
		// Malformed URL — surface as unreachable; the caller's base URL is bad.
		return nil, 0, &AIError{Kind: ErrUnreachable, Message: fmt.Sprintf("build request: %v", err)}
	}
	req.Header.Set("Content-Type", "application/json")
	if pr.setHeaders != nil {
		pr.setHeaders(req)
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
		// Let the provider parse its structured error body for better fidelity;
		// fall back to status-based classification + trimmed body.
		if pr.classifyErr != nil {
			if e := pr.classifyErr(raw, resp.StatusCode); e != nil {
				return nil, resp.StatusCode, e
			}
		}
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 500 {
			msg = msg[:500] + "…"
		}
		return nil, resp.StatusCode, &AIError{Kind: classifyStatus(resp.StatusCode), Status: resp.StatusCode, Message: msg}
	}
	return raw, resp.StatusCode, nil
}

// classifyStatus maps an HTTP status to a normalized AIErrorKind. Centralized so
// every provider agrees on the taxonomy.
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

// isTransient reports whether a provider error is worth retrying. Only
// server-side failures (5xx) and rate-limiting (429) qualify: a 5xx is the
// provider's own "try again" signal, and a 429 explicitly invites a backoff.
// 4xx errors (bad request, unauthorized, forbidden, model-missing) are
// deterministic — retrying an identical request only wastes the user's time.
// Transport errors (unreachable/timeout) are excluded too: unreachable
// usually means a misconfigured endpoint, and a timeout has already consumed
// the full per-call budget, so a retry would double an already-long wait.
func isTransient(aiErr *AIError) bool {
	return aiErr != nil && (aiErr.Kind == ErrServer || aiErr.Kind == ErrRateLimited)
}

// retryBackoff is the wait before each retry attempt; its length also caps the
// retry count (len entries → len retries on top of the initial attempt). The
// schedule is modest because 5xx/429 responses arrive immediately, so the
// waits — not request latency — dominate the cost of a persistent failure.
// ~2s of total backoff is a cheap price for absorbing a provider's transient
// blip (e.g. Google's OpenAI-compatible shim intermittently returns INTERNAL
// 500s). A package var (not a const) so tests can shrink it to keep the suite
// fast.
var retryBackoff = []time.Duration{
	500 * time.Millisecond,
	1500 * time.Millisecond,
}

// sendWithRetry wraps sendOnce with bounded retry on transient provider errors
// (5xx / 429). Each attempt gets its own per-call timeout — sendOnce derives a
// fresh context.WithTimeout every call — so one slow response does not eat the
// retry budget, and the caller's ctx still bounds the whole sequence (a
// cancellation during backoff aborts immediately rather than waiting out the
// timer).
func sendWithRetry(ctx context.Context, pr providerRequest, timeoutMs *int) ([]byte, int, *AIError) {
	if len(pr.body) > MaxRequestBytes {
		return nil, 0, &AIError{Kind: ErrBadRequest, Message: fmt.Sprintf("request body exceeds %d-byte cap", MaxRequestBytes)}
	}
	var last *AIError
	var lastStatus int
	for attempt := 0; attempt <= len(retryBackoff); attempt++ {
		raw, status, aiErr := sendOnce(ctx, pr, timeoutMs)
		if aiErr == nil {
			return raw, status, nil
		}
		last, lastStatus = aiErr, status
		// Non-transient errors (4xx, transport) return immediately — no retry.
		if !isTransient(aiErr) {
			return raw, status, aiErr
		}
		// Transient: wait before the next attempt, unless this was the last try.
		if attempt < len(retryBackoff) {
			timer := time.NewTimer(retryBackoff[attempt])
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, lastStatus, &AIError{Kind: ErrTimeout, Message: fmt.Sprintf("aborted during retry backoff: %v", ctx.Err())}
			case <-timer.C:
			}
		}
	}
	return nil, lastStatus, last
}

// Complete performs a chat completion against the configured provider. It
// validates the shared inputs then dispatches on ProviderType to the matching
// native encoder. Streaming is NOT implemented (Sprint 22); the Stream field is
// accepted so the signature is additive when streaming lands, but the provider
// is asked not to stream (stream is only set true by the caller explicitly, and
// even then this path returns the buffered non-streamed body).
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
	switch req.Provider.ProviderType {
	case ProviderGoogle:
		return completeGoogle(ctx, req, model, baseURL)
	case ProviderAnthropic:
		return completeAnthropic(ctx, req, model, baseURL)
	default:
		// ProviderLocal and ProviderOpenAICompatible (and any unknown value)
		// all use the OpenAI-compatible shape — it is the universal default.
		return completeOpenAI(ctx, req, model, baseURL)
	}
}

// Embed computes embeddings for a batch of texts against the configured
// provider. Google uses native batchEmbedContents; the OpenAI-compatible path
// uses /v1/embeddings; Anthropic has no embeddings endpoint and returns a clear
// error. The whole batch is sent in a single request; embeddings[i]
// corresponds to texts[i].
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
	switch req.Provider.ProviderType {
	case ProviderGoogle:
		return embedGoogle(ctx, req, model, baseURL)
	case ProviderAnthropic:
		return EmbedResult{}, &AIError{Kind: ErrBadRequest, Message: "anthropic provider does not support embeddings; use an OpenAI-compatible or local embedding endpoint"}
	default:
		return embedOpenAI(ctx, req, model, baseURL)
	}
}

// Probe performs the minimal call the AI Provider page's "Test connection" uses:
// a 1-token chat completion (chat) or a single short embed (embedding). It
// returns nil on success and an *AIError (normalized) on failure. isChat selects
// the probe kind so one binding serves both provider blocks. Dispatches through
// Complete/Embed, so every native provider is probed via its own API shape.
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
