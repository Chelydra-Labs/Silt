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
	Text             string              `json:"text,omitempty"`
	Thought          bool                `json:"thought,omitempty"`
	FunctionCall     *googleFunctionCall `json:"functionCall,omitempty"`
	FunctionResponse *googleFunctionResp `json:"functionResponse,omitempty"`
}

// googleFunctionCall is the model's request to invoke a tool (#595). Google
// identifies the function by name and newer API versions may also provide an
// opaque id for correlating the later function response.
type googleFunctionCall struct {
	ID   string          `json:"id,omitempty"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

// googleFunctionResp is a tool result fed back to the model (#595), sent as a
// part inside a user turn. Name identifies the function; ID is included when
// the originating call supplied a distinct opaque id. Response is a JSON object.
type googleFunctionResp struct {
	ID       string          `json:"id,omitempty"`
	Name     string          `json:"name"`
	Response json.RawMessage `json:"response"`
}

// googleGenerateRequest is the generateContent request body.
type googleGenerateRequest struct {
	SystemInstruction *googleContent          `json:"systemInstruction,omitempty"`
	Contents          []googleContent         `json:"contents"`
	GenerationConfig  *googleGenerationConfig `json:"generationConfig,omitempty"`
	Tools             []googleTool            `json:"tools,omitempty"`
	ToolConfig        *googleToolConfig       `json:"toolConfig,omitempty"`
}

// googleTool carries function declarations (Google nests them one level deep).
type googleTool struct {
	FunctionDeclarations []googleFunctionDecl `json:"functionDeclarations,omitempty"`
}

// googleFunctionDecl is one tool declaration. Parameters is a JSON Schema with
// Google's uppercase type enum (converted from standard lowercase).
type googleFunctionDecl struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// googleToolConfig selects tool-calling behavior (AUTO/ANY/NONE) and optional
// allowed function names for force-mode.
type googleToolConfig struct {
	FunctionCallingConfig googleFunctionCallingConfig `json:"functionCallingConfig"`
}

type googleFunctionCallingConfig struct {
	Mode                 string   `json:"mode,omitempty"`
	AllowedFunctionNames []string `json:"allowedFunctionNames,omitempty"`
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
	// TaskType optimizes embeddings for asymmetric retrieval
	// (RETRIEVAL_DOCUMENT at index time, RETRIEVAL_QUERY at search time).
	TaskType string `json:"taskType,omitempty"`
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

// googleTools encodes the unified ToolDef slice as Google's tools[] array
// (one wrapper with functionDeclarations[]). Parameter schemas are uppercased
// to Google's type-enum convention, matching responseSchema handling.
func googleTools(tools []ToolDef) []googleTool {
	if len(tools) == 0 {
		return nil
	}
	decls := make([]googleFunctionDecl, len(tools))
	for i, t := range tools {
		var params json.RawMessage
		if len(t.Parameters) > 0 {
			params = googleConvertSchema(t.Parameters)
		}
		decls[i] = googleFunctionDecl{
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		}
	}
	return []googleTool{{FunctionDeclarations: decls}}
}

// googleBuildToolConfig maps the unified ToolChoice onto Google's
// functionCallingConfig: auto→AUTO, required→ANY, none→NONE, force→ANY with
// allowedFunctionNames pinned to the one tool. nil when there is no caller
// preference and no structured output (Google's default is AUTO).
func googleBuildToolConfig(tc *ToolChoice, schema json.RawMessage) *googleToolConfig {
	if tc == nil {
		return nil
	}
	cfg := &googleToolConfig{}
	switch tc.Mode {
	case ToolChoiceAuto:
		cfg.FunctionCallingConfig.Mode = "AUTO"
	case ToolChoiceRequired:
		cfg.FunctionCallingConfig.Mode = "ANY"
	case ToolChoiceNone:
		cfg.FunctionCallingConfig.Mode = "NONE"
	case ToolChoiceForce:
		if tc.ToolName == "" {
			cfg.FunctionCallingConfig.Mode = "ANY"
		} else {
			cfg.FunctionCallingConfig.Mode = "ANY"
			cfg.FunctionCallingConfig.AllowedFunctionNames = []string{tc.ToolName}
		}
	default:
		return nil
	}
	return cfg
}

// googleArgsFromRaw normalizes a tool-call arguments RawMessage into the JSON
// object bytes Google's functionCall.args expects (defaults to {} when empty or
// non-object).
func googleArgsFromRaw(raw json.RawMessage) json.RawMessage {
	return normalizeToolArguments(raw)
}

// googleToolResponse renders a tool-result content string as the JSON object
// Google's functionResponse.response requires. A valid JSON body is passed
// through; otherwise the string is wrapped under a "result" key.
func googleToolResponse(content string) json.RawMessage {
	if content != "" && json.Valid([]byte(content)) {
		return json.RawMessage(content)
	}
	b, _ := json.Marshal(map[string]string{"result": content})
	return b
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
	// assistant→model; tool turns are encoded as functionCall/functionResponse
	// parts (#595).
	//
	// Single-pass id→name index for tool results (#637): O(1) lookup instead of
	// scanning prior assistant turns for every RoleTool message.
	toolCallNameByID := make(map[string]string)
	for _, m := range req.Messages {
		if m.Role != RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				toolCallNameByID[tc.ID] = tc.Name
			}
		}
	}
	var system *googleContent
	var contents []googleContent
	for _, m := range req.Messages {
		if m.Role == RoleSystem {
			if system == nil {
				system = &googleContent{Parts: []googleTextPart{{Text: m.Content}}}
			} else {
				system.Parts = append(system.Parts, googleTextPart{Text: m.Content})
			}
			continue
		}
		// Tool result → user turn with a functionResponse part. Resolve the
		// function name from the id→name index when the tool result carries an
		// opaque call id. Older name-based histories fall back to treating
		// ToolCallID as the function name.
		if m.Role == RoleTool {
			name := m.ToolCallID
			id := ""
			if resolved, ok := toolCallNameByID[m.ToolCallID]; ok && resolved != "" {
				name = resolved
				if m.ToolCallID != "" && m.ToolCallID != resolved {
					id = m.ToolCallID
				}
			}
			contents = append(contents, googleContent{
				Role: RoleUser,
				Parts: []googleTextPart{{
					FunctionResponse: &googleFunctionResp{
						ID:       id,
						Name:     name,
						Response: googleToolResponse(m.Content),
					},
				}},
			})
			continue
		}
		// Assistant turn that requested tools → model turn with functionCall
		// parts (plus an optional leading text part when Content is set).
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			parts := make([]googleTextPart, 0, 1+len(m.ToolCalls))
			if m.Content != "" {
				parts = append(parts, googleTextPart{Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				args := googleArgsFromRaw(tc.Arguments)
				parts = append(parts, googleTextPart{
					FunctionCall: &googleFunctionCall{ID: tc.ID, Name: tc.Name, Args: args},
				})
			}
			contents = append(contents, googleContent{Role: "model", Parts: parts})
			continue
		}
		role := m.Role
		if role == RoleAssistant {
			role = "model"
		}
		if role == "" {
			role = RoleUser
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
		Tools:             googleTools(req.Tools),
		ToolConfig:        googleBuildToolConfig(req.ToolChoice, req.ResponseSchema),
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
	// Concatenate non-thought text parts and collect functionCall parts.
	// Gemini 2.5+ thinking models emit reasoning in parts with thought:true —
	// those are internal scratchpad, not the user-facing answer, so skipped.
	var sb strings.Builder
	var toolCalls []ToolCall
	for _, p := range resp.Candidates[0].Content.Parts {
		if p.Thought {
			continue
		}
		if p.FunctionCall != nil {
			// Preserve Google's opaque id when present; callers use it to
			// correlate the later tool-result message. Older responses without
			// an id retain the name-based fallback.
			id := p.FunctionCall.ID
			if id == "" {
				id = p.FunctionCall.Name
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        id,
				Name:      p.FunctionCall.Name,
				Arguments: googleArgsFromRaw(p.FunctionCall.Args),
			})
			continue
		}
		// Skip empty Text on a functionResponse-bearing part (only relevant
		// on the request side; defensive on decode).
		sb.WriteString(p.Text)
	}
	out := CompleteResult{Content: sb.String(), Model: model, ToolCalls: toolCalls}
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
			TaskType:             req.TaskType,
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
