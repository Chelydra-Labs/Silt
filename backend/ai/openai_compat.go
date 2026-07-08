// OpenAI-compatible provider: /v1/chat/completions + /v1/embeddings. This is the
// universal default/fallback spoken by Ollama, llama-server, OpenRouter, LM
// Studio, OpenAI, and Google's compat shim. Auth is Authorization: Bearer when a
// key is present (keyless local endpoints stay anonymous). Extracted from the
// original single-shape service so native providers can share the transport.

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

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

// completeOpenAI performs a chat completion against /v1/chat/completions.
func completeOpenAI(ctx context.Context, req CompleteRequest, model, baseURL string) (CompleteResult, error) {
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
		Stream:          false, // streaming lands in Sprint 22
	})
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    baseURL + "/v1/chat/completions",
		body:   body,
		setHeaders: func(r *http.Request) {
			// Ollama's OpenAI-compatible routes accept any non-empty Bearer
			// token; cloud providers require a real key. Sending an empty
			// bearer would 401 on cloud, so only attach when present.
			if req.Provider.APIKey != "" {
				r.Header.Set("Authorization", "Bearer "+req.Provider.APIKey)
			}
		},
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
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

// embedOpenAI computes embeddings via /v1/embeddings.
func embedOpenAI(ctx context.Context, req EmbedRequest, model, baseURL string) (EmbedResult, error) {
	body, err := json.Marshal(embedRequest{Model: model, Input: req.Texts, Dimensions: req.Dimensions})
	if err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    baseURL + "/v1/embeddings",
		body:   body,
		setHeaders: func(r *http.Request) {
			if req.Provider.APIKey != "" {
				r.Header.Set("Authorization", "Bearer "+req.Provider.APIKey)
			}
		},
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
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

// listModelsOpenAI polls GET <baseURL>/v1/models (served by Ollama, OpenRouter,
// LM Studio, OpenAI, Google's compat shim, …). Returns model ids; DisplayName
// equals ID since the compat shape carries no human-readable name.
func listModelsOpenAI(ctx context.Context, p AIProvider, baseURL string) ([]AIModel, error) {
	pr := providerRequest{
		method: "GET",
		url:    baseURL + "/v1/models",
		setHeaders: func(r *http.Request) {
			if p.APIKey != "" {
				r.Header.Set("Authorization", "Bearer "+p.APIKey)
			}
		},
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, p.TimeoutMs)
	if aiErr != nil {
		return nil, aiErr
	}
	var resp struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse models response: %v", err)}
	}
	out := make([]AIModel, 0, len(resp.Data))
	for _, m := range resp.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			out = append(out, AIModel{ID: id, DisplayName: id})
		}
	}
	return out, nil
}
