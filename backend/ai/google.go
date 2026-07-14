// Native Google Generative AI (Gemini) provider. Speaks generateContent /
// batchEmbedContents / listModels against generativelanguage.googleapis.com,
// bypassing the flakier OpenAI-compat shim. Auth is the x-goog-api-key header
// (preferred over ?key= to keep the key out of URLs/logs).

package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// googleContent is one turn in the Google contents[] array. Role is "user" or
// "model" (Google's name for the assistant side). Parts holds the text segments.
type googleContent struct {
	Role  string           `json:"role"`
	Parts []googleTextPart `json:"parts"`
}

type googleTextPart struct {
	Text    string `json:"text"`
	Thought bool   `json:"thought,omitempty"`
}

// googleGenerateRequest is the generateContent request body.
type googleGenerateRequest struct {
	SystemInstruction *googleContent          `json:"systemInstruction,omitempty"`
	Contents          []googleContent         `json:"contents"`
	GenerationConfig  *googleGenerationConfig `json:"generationConfig,omitempty"`
}

type googleGenerationConfig struct {
	Temperature      *float64        `json:"temperature,omitempty"`
	MaxOutputTokens  *int            `json:"maxOutputTokens,omitempty"`
	ResponseMimeType string          `json:"responseMimeType,omitempty"`
	ResponseSchema   json.RawMessage `json:"responseSchema,omitempty"`
}

// googleGenerateResponse is the generateContent response body.
type googleGenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []googleTextPart `json:"parts"`
		} `json:"content"`
		FinishReason string `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata *struct {
		PromptTokenCount     *int `json:"promptTokenCount"`
		CandidatesTokenCount *int `json:"candidatesTokenCount"`
		TotalTokenCount      *int `json:"totalTokenCount"`
	} `json:"usageMetadata,omitempty"`
}

// googleEmbedRequest is the batchEmbedContents request body.
type googleEmbedRequest struct {
	Requests []googleEmbedOne `json:"requests"`
}

type googleEmbedOne struct {
	Model                string        `json:"model"`
	Content              googleContent `json:"content"`
	OutputDimensionality *int          `json:"outputDimensionality,omitempty"`
}

// googleEmbedResponse is the batchEmbedContents response body.
type googleEmbedResponse struct {
	Embeddings []struct {
		Values []float64 `json:"values"`
	} `json:"embeddings"`
}

// googleError is the structured Google error body: {error: {code, message, status}}.
type googleError struct {
	Error struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// googleModelName ensures the model id carries the required models/ prefix for
// the path-style URL (generateContent takes models/<id>:generateContent).
func googleModelName(model string) string {
	if strings.HasPrefix(model, "models/") {
		return model
	}
	return "models/" + model
}

// googleConvertSchema converts a standard JSON Schema (lowercase type strings)
// to Google's responseSchema format (uppercase type enum: STRING, NUMBER,
// INTEGER, BOOLEAN, ARRAY, OBJECT). Google accepts the rest of JSON Schema
// (properties, items, required, enum, description) verbatim — only the type
// field needs uppercasing. The conversion is recursive into properties and
// items so nested objects/arrays are handled.
func googleConvertSchema(raw json.RawMessage) json.RawMessage {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw // malformed → pass through; the provider will reject it
	}
	converted := googleConvertValue(v)
	out, err := json.Marshal(converted)
	if err != nil {
		return raw
	}
	return out
}

func googleConvertValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "type" {
				if s, ok := val.(string); ok {
					out[k] = strings.ToUpper(s)
					continue
				}
			}
			out[k] = googleConvertValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = googleConvertValue(val)
		}
		return out
	default:
		return v
	}
}

// googleClassifyError maps a Google structured error body to an AIError. Returns
// nil when the body isn't the expected shape so the default status-based path
// runs.
func googleClassifyError(raw []byte, status int) *AIError {
	var ge googleError
	if err := json.Unmarshal(raw, &ge); err != nil || ge.Error.Message == "" {
		return nil
	}
	kind := classifyStatus(status)
	switch ge.Error.Status {
	case "INVALID_ARGUMENT", "FAILED_PRECONDITION":
		kind = ErrBadRequest
	case "NOT_FOUND":
		kind = ErrModelMissing
	case "RESOURCE_EXHAUSTED":
		kind = ErrRateLimited
	case "INTERNAL", "UNAVAILABLE", "DEADLINE_EXCEEDED":
		kind = ErrServer
	}
	msg := strings.TrimSpace(ge.Error.Message)
	if len(msg) > 500 {
		msg = msg[:500] + "…"
	}
	return &AIError{Kind: kind, Status: status, Message: msg}
}

// completeGoogle performs a chat completion via generateContent.
func completeGoogle(ctx context.Context, req CompleteRequest, model, baseURL string) (CompleteResult, error) {
	// Split system messages into the top-level systemInstruction field; Google
	// does not accept role:"system" in contents[]. Remaining messages map
	// assistant→model.
	var system *googleContent
	var contents []googleContent
	for _, m := range req.Messages {
		if m.Role == "system" {
			if system == nil {
				system = &googleContent{Parts: []googleTextPart{{Text: m.Content}}}
			} else {
				system.Parts = append(system.Parts, googleTextPart{Text: m.Content})
			}
			continue
		}
		role := m.Role
		if role == "assistant" {
			role = "model"
		}
		if role == "" {
			role = "user"
		}
		contents = append(contents, googleContent{
			Role:  role,
			Parts: []googleTextPart{{Text: m.Content}},
		})
	}
	gc := &googleGenerationConfig{Temperature: req.Temperature, MaxOutputTokens: req.MaxTokens}
	// Native structured output: Google's responseSchema uses uppercase type
	// enum values (STRING, ARRAY, OBJECT, …). Convert from standard JSON Schema.
	if len(req.ResponseSchema) > 0 {
		gc.ResponseMimeType = "application/json"
		gc.ResponseSchema = googleConvertSchema(req.ResponseSchema)
	}
	body, err := json.Marshal(googleGenerateRequest{
		SystemInstruction: system,
		Contents:          contents,
		GenerationConfig:  gc,
	})
	if err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    fmt.Sprintf("%s/v1beta/%s:generateContent", baseURL, googleModelName(model)),
		body:   body,
		setHeaders: func(r *http.Request) {
			if req.Provider.APIKey != "" {
				r.Header.Set("x-goog-api-key", req.Provider.APIKey)
			}
		},
		classifyErr: googleClassifyError,
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
	if aiErr != nil {
		return CompleteResult{}, aiErr
	}
	var resp googleGenerateResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	if len(resp.Candidates) == 0 {
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no candidates"}
	}
	if len(resp.Candidates[0].Content.Parts) == 0 {
		// Google blocks content via safety filters: the candidate exists but
		// has empty parts and a finishReason like "SAFETY". Surface the reason
		// so the user knows the output wasn't empty by mistake.
		reason := resp.Candidates[0].FinishReason
		if reason == "" {
			reason = "unknown"
		}
		return CompleteResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("provider returned no content (finishReason: %s)", reason)}
	}
	// Concatenate non-thought text parts. Gemini 2.5+ thinking models emit
	// reasoning in parts with thought:true — those are internal scratchpad,
	// not the user-facing answer, so they are skipped.
	var sb strings.Builder
	for _, p := range resp.Candidates[0].Content.Parts {
		if p.Thought {
			continue
		}
		sb.WriteString(p.Text)
	}
	out := CompleteResult{Content: sb.String(), Model: model}
	if resp.UsageMetadata != nil {
		out.Usage = &AIUsage{
			PromptTokens:     resp.UsageMetadata.PromptTokenCount,
			CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
			TotalTokens:      resp.UsageMetadata.TotalTokenCount,
		}
	}
	return out, nil
}

// embedGoogle computes embeddings via batchEmbedContents.
func embedGoogle(ctx context.Context, req EmbedRequest, model, baseURL string) (EmbedResult, error) {
	name := googleModelName(model)
	one := make([]googleEmbedOne, len(req.Texts))
	for i, t := range req.Texts {
		one[i] = googleEmbedOne{
			Model:                name,
			Content:              googleContent{Parts: []googleTextPart{{Text: t}}},
			OutputDimensionality: req.Dimensions,
		}
	}
	body, err := json.Marshal(googleEmbedRequest{Requests: one})
	if err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("marshal request: %v", err)}
	}
	pr := providerRequest{
		method: "POST",
		url:    fmt.Sprintf("%s/v1beta/%s:batchEmbedContents", baseURL, name),
		body:   body,
		setHeaders: func(r *http.Request) {
			if req.Provider.APIKey != "" {
				r.Header.Set("x-goog-api-key", req.Provider.APIKey)
			}
		},
		classifyErr: googleClassifyError,
	}
	raw, _, aiErr := sendWithRetry(ctx, pr, req.Provider.TimeoutMs)
	if aiErr != nil {
		return EmbedResult{}, aiErr
	}
	var resp googleEmbedResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse response: %v", err)}
	}
	if len(resp.Embeddings) == 0 {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: "provider returned no embeddings"}
	}
	if len(resp.Embeddings) != len(req.Texts) {
		return EmbedResult{}, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("provider returned %d embeddings for %d texts", len(resp.Embeddings), len(req.Texts))}
	}
	out := EmbedResult{
		Embeddings: make([][]float64, len(resp.Embeddings)),
		Model:      model,
		Dimensions: len(resp.Embeddings[0].Values),
	}
	for i, e := range resp.Embeddings {
		out.Embeddings[i] = e.Values
	}
	return out, nil
}

// listModelsGoogle polls GET <baseURL>/v1beta/models. Strips the models/ prefix
// from the resource name for the ID; carries displayName as the dropdown label.
// When which is "embedding", only models supporting embedContent are returned;
// otherwise only generateContent-supporting models. Follows nextPageToken to
// consume all pages so the dropdown isn't silently truncated for accounts with
// many models.
func listModelsGoogle(ctx context.Context, p AIProvider, baseURL, which string) ([]AIModel, error) {
	want := "generateContent"
	if which == "embedding" {
		want = "embedContent"
	}
	var out []AIModel
	pageToken := ""
	// Cap iterations as a safety valve against a misbehaving endpoint that
	// loops forever on the same token.
	for page := 0; page < 20; page++ {
		url := baseURL + "/v1beta/models"
		if pageToken != "" {
			url += "?pageToken=" + pageToken
		}
		pr := providerRequest{
			method: "GET",
			url:    url,
			setHeaders: func(r *http.Request) {
				if p.APIKey != "" {
					r.Header.Set("x-goog-api-key", p.APIKey)
				}
			},
			classifyErr: googleClassifyError,
		}
		raw, _, aiErr := sendWithRetry(ctx, pr, p.TimeoutMs)
		if aiErr != nil {
			return nil, aiErr
		}
		var resp struct {
			Models []struct {
				Name                       string   `json:"name"`
				DisplayName                string   `json:"displayName"`
				SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
			} `json:"models"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, &AIError{Kind: ErrUnknown, Message: fmt.Sprintf("parse models response: %v", err)}
		}
		for _, m := range resp.Models {
			if !supportsMethod(m.SupportedGenerationMethods, want) {
				continue
			}
			id := strings.TrimSpace(strings.TrimPrefix(m.Name, "models/"))
			if id == "" {
				continue
			}
			display := strings.TrimSpace(m.DisplayName)
			if display == "" {
				display = id
			}
			out = append(out, AIModel{ID: id, DisplayName: display})
		}
		pageToken = resp.NextPageToken
		if pageToken == "" {
			break
		}
	}
	return out, nil
}

// supportsMethod reports whether the Google supportedGenerationMethods list
// names the given method.
func supportsMethod(methods []string, want string) bool {
	for _, m := range methods {
		if m == want {
			return true
		}
	}
	return false
}
