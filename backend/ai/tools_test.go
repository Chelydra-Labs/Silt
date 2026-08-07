package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// dummySchema is a minimal JSON Schema reused across the round-trip tests.
var dummySchema = json.RawMessage(`{"type":"object","properties":{"q":{"type":"string"}},"required":["q"]}`)

// dummyTool is the ToolDef the round-trip tests ask each provider to expose.
func dummyTool() ToolDef {
	return ToolDef{
		Name:        "search_notes",
		Description: "Search the vault",
		Parameters:  dummySchema,
	}
}

// --- OpenAI-compatible ---------------------------------------------------

func TestCompleteOpenAI_EncodesToolsAndParsesToolCalls(t *testing.T) {
	var captured chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &captured); err != nil {
			t.Errorf("parse request: %v", err)
		}
		// Reply with a tool_call (OpenAI shape: arguments is a JSON string).
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m",
			"choices": []map[string]any{
				{
					"message": map[string]any{
						"role": "assistant",
						"tool_calls": []map[string]any{
							{
								"id":   "call_01",
								"type": "function",
								"function": map[string]any{
									"name":      "search_notes",
									"arguments": `{"q":"meetings"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
		})
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), CompleteRequest{
		Provider:   AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages:   []ChatMessage{{Role: "user", Content: "find meetings"}},
		Tools:      []ToolDef{dummyTool()},
		ToolChoice: &ToolChoice{Mode: ToolChoiceAuto},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Request encoded the tool as a function wrapper.
	if len(captured.Tools) != 1 || captured.Tools[0].Type != "function" {
		t.Fatalf("tools = %+v, want one function tool", captured.Tools)
	}
	if name := captured.Tools[0].Function.Name; name != "search_notes" {
		t.Errorf("tool name = %q, want search_notes", name)
	}
	// tool_choice "auto" is the bare keyword.
	if captured.ToolChoice != "auto" {
		t.Errorf("tool_choice = %v, want \"auto\"", captured.ToolChoice)
	}
	// Response tool_call decoded into the unified ToolCall with raw-JSON args.
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.ID != "call_01" || tc.Name != "search_notes" {
		t.Errorf("tool_call = %+v", tc)
	}
	// Arguments unwrapped from the stringified form to raw JSON object bytes.
	var args map[string]any
	if err := json.Unmarshal(tc.Arguments, &args); err != nil {
		t.Fatalf("arguments not valid JSON object: %v (raw=%s)", err, tc.Arguments)
	}
	if args["q"] != "meetings" {
		t.Errorf("arguments.q = %v, want meetings", args["q"])
	}
}

func TestCompleteOpenAI_NormalizesNonObjectToolArguments(t *testing.T) {
	cases := []struct {
		name string
		args string
	}{
		{name: "string", args: `"hello"`},
		{name: "number", args: `42`},
		{name: "array", args: `[1,2,3]`},
		{name: "malformed", args: `{not-json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"model": "m",
					"choices": []map[string]any{{"message": map[string]any{
						"tool_calls": []map[string]any{{
							"id":       "call_invalid",
							"function": map[string]any{"name": "search_notes", "arguments": tc.args},
						}},
					}}},
				})
			}))
			defer srv.Close()

			res, err := Complete(context.Background(), CompleteRequest{
				Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
				Messages: []ChatMessage{{Role: RoleUser, Content: "find meetings"}},
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(res.ToolCalls) != 1 {
				t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
			}
			if got := string(res.ToolCalls[0].Arguments); got != "{}" {
				t.Errorf("arguments = %q, want {}", got)
			}
		})
	}
}

func TestToolArgumentNormalizersCoerceInvalidShapes(t *testing.T) {
	cases := []struct {
		name string
		raw  json.RawMessage
	}{
		{name: "string", raw: json.RawMessage(`"hello"`)},
		{name: "number", raw: json.RawMessage(`42`)},
		{name: "array", raw: json.RawMessage(`[1,2,3]`)},
		{name: "malformed", raw: json.RawMessage(`{not-json`)},
		{name: "empty", raw: nil},
	}
	normalizers := []struct {
		name string
		fn   func(json.RawMessage) json.RawMessage
	}{
		{name: "openai", fn: func(raw json.RawMessage) json.RawMessage { return openaiArgsToRaw(string(raw)) }},
		{name: "anthropic", fn: anthropicInputFromRaw},
		{name: "google", fn: googleArgsFromRaw},
	}
	for _, normalizer := range normalizers {
		for _, tc := range cases {
			t.Run(normalizer.name+"/"+tc.name, func(t *testing.T) {
				if got := string(normalizer.fn(tc.raw)); got != "{}" {
					t.Errorf("normalized arguments = %q, want {}", got)
				}
			})
		}
	}
	if got := string(normalizeToolArguments(json.RawMessage(`{"q":"meetings"}`))); got != `{"q":"meetings"}` {
		t.Errorf("valid object changed = %q", got)
	}
}

func TestCompleteOpenAI_ToolChoiceForceEncodesObject(t *testing.T) {
	var captured chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m",
			"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider:   AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages:   []ChatMessage{{Role: "user", Content: "x"}},
		Tools:      []ToolDef{dummyTool()},
		ToolChoice: &ToolChoice{Mode: ToolChoiceForce, ToolName: "search_notes"},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	tc, ok := captured.ToolChoice.(map[string]any)
	if !ok || tc["type"] != "function" {
		t.Errorf("tool_choice = %+v, want function object", captured.ToolChoice)
	}
	fn, _ := tc["function"].(map[string]any)
	if fn["name"] != "search_notes" {
		t.Errorf("force tool name = %v, want search_notes", fn["name"])
	}
}

// TestCompleteOpenAI_ReplaysToolTurnsInHistory verifies that an assistant
// tool_call turn and a following tool result encode into the OpenAI request as
// tool_calls + role:tool respectively — the multi-turn agent-loop contract.
func TestCompleteOpenAI_ReplaysToolTurnsInHistory(t *testing.T) {
	var captured chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "m",
			"choices": []map[string]any{{"message": map[string]any{"content": "done"}}},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{BaseURL: srv.URL, Model: "m"},
		Messages: []ChatMessage{
			{Role: "user", Content: "find meetings"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "call_01", Name: "search_notes", Arguments: json.RawMessage(`{"q":"meetings"}`)},
			}},
			{Role: "tool", ToolCallID: "call_01", Content: "3 matches"},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(captured.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(captured.Messages))
	}
	// Assistant turn carries tool_calls with stringified arguments.
	asst := captured.Messages[1]
	if asst.Role != "assistant" || len(asst.ToolCalls) != 1 {
		t.Errorf("assistant turn = %+v, want tool_calls", asst)
	}
	if asst.ToolCalls[0].Function.Arguments != `{"q":"meetings"}` {
		t.Errorf("arguments = %q, want stringified JSON", asst.ToolCalls[0].Function.Arguments)
	}
	// Tool result turn carries role + tool_call_id + content.
	tool := captured.Messages[2]
	if tool.Role != "tool" || tool.ToolCallID != "call_01" || tool.Content != "3 matches" {
		t.Errorf("tool turn = %+v", tool)
	}
}

// --- Anthropic -----------------------------------------------------------

func TestCompleteAnthropic_RealToolsAdditiveAndDecoded(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": "Let me search."},
				{
					"type":  "tool_use",
					"id":    "toolu_01",
					"name":  "search_notes",
					"input": map[string]any{"q": "meetings"},
				},
			},
			"model":       "claude-sonnet-5",
			"stop_reason": "tool_use",
		})
	}))
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages: []ChatMessage{{Role: "user", Content: "find meetings"}},
		Tools:    []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Caller tool lands in the request tools[] as a real tool (not forced).
	if len(captured.Tools) != 1 || captured.Tools[0].Name != "search_notes" {
		t.Fatalf("tools = %+v, want one search_notes tool", captured.Tools)
	}
	// No ResponseSchema → no forced structured_output choice; caller's nil
	// ToolChoice → tool_choice omitted.
	if captured.ToolChoice != nil {
		t.Errorf("tool_choice = %v, want nil (auto default)", captured.ToolChoice)
	}
	// Text + real tool_use decoded.
	if res.Content != "Let me search." {
		t.Errorf("content = %q, want 'Let me search.'", res.Content)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.ID != "toolu_01" || tc.Name != "search_notes" {
		t.Errorf("tool_call = %+v", tc)
	}
	var args map[string]any
	if err := json.Unmarshal(tc.Arguments, &args); err != nil {
		t.Fatalf("arguments not object: %v", err)
	}
	if args["q"] != "meetings" {
		t.Errorf("args.q = %v", args["q"])
	}
}

func TestCompleteAnthropic_NormalizesNonObjectToolArguments(t *testing.T) {
	cases := []struct {
		name  string
		input any
	}{
		{name: "string", input: "hello"},
		{name: "number", input: 42},
		{name: "array", input: []any{1, 2, 3}},
		{name: "malformed", input: "{not-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"content": []map[string]any{{
						"type": "tool_use", "id": "toolu_invalid", "name": "search_notes", "input": tc.input,
					}},
					"model": "claude-sonnet-5",
				})
			}))
			defer srv.Close()

			res, err := Complete(context.Background(), CompleteRequest{
				Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
				Messages: []ChatMessage{{Role: RoleUser, Content: "find meetings"}},
				Tools:    []ToolDef{dummyTool()},
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(res.ToolCalls) != 1 {
				t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
			}
			if got := string(res.ToolCalls[0].Arguments); got != "{}" {
				t.Errorf("arguments = %q, want {}", got)
			}
		})
	}
}

// TestCompleteAnthropic_CoalescesParallelToolResults guards the multi-tool
// contract: a run of RoleTool messages (one per parallel tool call) must be
// encoded as a SINGLE user turn carrying all tool_result blocks, not one user
// turn per result (which desyncs Anthropic's tool loop).
func TestCompleteAnthropic_CoalescesParallelToolResults(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "done"}},
			"model":   "claude-sonnet-5",
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "find meetings"},
			{Role: RoleAssistant, Content: "", ToolCalls: []ToolCall{
				{ID: "toolu_a", Name: "search_notes", Arguments: json.RawMessage(`{"q":"m"}`)},
				{ID: "toolu_b", Name: "read_blocks", Arguments: json.RawMessage(`{"block_ids":["x"]}`)},
			}},
			{Role: RoleTool, ToolCallID: "toolu_a", Content: "result a"},
			{Role: RoleTool, ToolCallID: "toolu_b", Content: "result b"},
		},
		Tools: []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Expect: user, assistant(2 tool_use), user(2 tool_result) = 3 messages.
	if len(captured.Messages) != 3 {
		t.Fatalf("messages = %d, want 3 (tool results must coalesce into one user turn): %+v", len(captured.Messages), captured.Messages)
	}
	if captured.Messages[2].Role != RoleUser {
		t.Fatalf("third message role = %q, want user", captured.Messages[2].Role)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(captured.Messages[2].Content, &blocks); err != nil {
		t.Fatalf("third message content is not a block array: %v", err)
	}
	if len(blocks) != 2 || blocks[0]["type"] != "tool_result" || blocks[1]["type"] != "tool_result" {
		t.Fatalf("third message blocks = %+v, want two tool_result blocks", blocks)
	}
	ids := []any{blocks[0]["tool_use_id"], blocks[1]["tool_use_id"]}
	if !((ids[0] == "toolu_a" && ids[1] == "toolu_b") || (ids[0] == "toolu_b" && ids[1] == "toolu_a")) {
		t.Fatalf("tool_use_ids = %v, want toolu_a+toolu_b", ids)
	}
}

// TestCompleteAnthropic_StructuredOutputUnchangedWithCallerTools guards the
// regression: when ResponseSchema is set, the structured_output path dominates
// — the response content is the JSON-stringified tool input, with no ToolCalls
// on the result, even if caller tools are also present.
func TestCompleteAnthropic_StructuredOutputUnchangedWithCallerTools(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{
				{
					"type":  "tool_use",
					"id":    "toolu_01",
					"name":  "structured_output",
					"input": map[string]any{"summary": "Meeting notes"},
				},
			},
			"model": "claude-sonnet-5",
		})
	}))
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider:       AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages:       []ChatMessage{{Role: "user", Content: "summarize"}},
		ResponseSchema: dummySchema,
		Tools:          []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Both tools present; structured_output forced.
	if len(captured.Tools) != 2 {
		t.Fatalf("tools = %d, want 2 (caller + structured_output)", len(captured.Tools))
	}
	tc, ok := captured.ToolChoice.(map[string]any)
	if !ok || tc["name"] != "structured_output" {
		t.Errorf("tool_choice = %+v, want forced structured_output", captured.ToolChoice)
	}
	// Content is the JSON-stringified structured output; no ToolCalls leaked.
	if !contains(res.Content, `"summary":"Meeting notes"`) {
		t.Errorf("content = %q, want JSON-stringified structured input", res.Content)
	}
	if len(res.ToolCalls) != 0 {
		t.Errorf("tool_calls = %d, want 0 on structured-output path", len(res.ToolCalls))
	}
}

// TestCompleteAnthropic_EncodesToolResultHistory verifies a tool-role message
// becomes a user turn with a tool_result content block.
func TestCompleteAnthropic_EncodesToolResultHistory(t *testing.T) {
	var captured anthropicRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []map[string]any{{"type": "text", "text": "ok"}},
			"model":   "claude-sonnet-5",
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderAnthropic, BaseURL: srv.URL, Model: "claude-sonnet-5"},
		Messages: []ChatMessage{
			{Role: "user", Content: "find meetings"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "toolu_01", Name: "search_notes", Arguments: json.RawMessage(`{"q":"meetings"}`)},
			}},
			{Role: "tool", ToolCallID: "toolu_01", Content: "3 matches"},
		},
		Tools: []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// The request should carry 3 messages (user, assistant, user-toolresult).
	if len(captured.Messages) != 3 {
		t.Fatalf("messages = %d, want 3", len(captured.Messages))
	}
	// Assistant turn content is a JSON block array with a tool_use entry.
	var asstBlocks []map[string]any
	if err := json.Unmarshal(captured.Messages[1].Content, &asstBlocks); err != nil {
		t.Fatalf("assistant content not block array: %v", err)
	}
	if asstBlocks[0]["type"] != "tool_use" || asstBlocks[0]["id"] != "toolu_01" {
		t.Errorf("assistant tool_use block = %+v", asstBlocks[0])
	}
	// Tool result is a user turn with a tool_result block.
	if captured.Messages[2].Role != "user" {
		t.Errorf("tool result role = %q, want user", captured.Messages[2].Role)
	}
	var toolBlocks []map[string]any
	if err := json.Unmarshal(captured.Messages[2].Content, &toolBlocks); err != nil {
		t.Fatalf("tool-result content not block array: %v", err)
	}
	if toolBlocks[0]["type"] != "tool_result" || toolBlocks[0]["tool_use_id"] != "toolu_01" {
		t.Errorf("tool_result block = %+v", toolBlocks[0])
	}
}

// --- Google --------------------------------------------------------------

func TestCompleteGoogle_EncodesFunctionDeclarationsAndParsesFunctionCall(t *testing.T) {
	var captured googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{
					{"functionCall": map[string]any{"name": "search_notes", "args": map[string]any{"q": "meetings"}}},
				}}},
			},
		})
	}))
	defer srv.Close()
	res, err := Complete(context.Background(), CompleteRequest{
		Provider:   AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages:   []ChatMessage{{Role: "user", Content: "find meetings"}},
		Tools:      []ToolDef{dummyTool()},
		ToolChoice: &ToolChoice{Mode: ToolChoiceAuto},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// Tools nested as functionDeclarations under tools[].
	if len(captured.Tools) != 1 || len(captured.Tools[0].FunctionDeclarations) != 1 {
		t.Fatalf("tools = %+v, want one wrapper with one decl", captured.Tools)
	}
	decl := captured.Tools[0].FunctionDeclarations[0]
	if decl.Name != "search_notes" {
		t.Errorf("decl name = %q", decl.Name)
	}
	// Google's uppercase type-enum conversion applied to the parameters.
	var params map[string]any
	_ = json.Unmarshal(decl.Parameters, &params)
	if params["type"] != "OBJECT" {
		t.Errorf("parameters type = %v, want OBJECT", params["type"])
	}
	// tool_choice AUTO present.
	if captured.ToolConfig == nil || captured.ToolConfig.FunctionCallingConfig.Mode != "AUTO" {
		t.Errorf("toolConfig = %+v, want AUTO", captured.ToolConfig)
	}
	// A functionCall without an id retains the name-based fallback ID.
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.Name != "search_notes" || tc.ID != "search_notes" {
		t.Errorf("tool_call = %+v, want name/id = search_notes", tc)
	}
	var args map[string]any
	if err := json.Unmarshal(tc.Arguments, &args); err != nil {
		t.Fatalf("arguments not object: %v", err)
	}
	if args["q"] != "meetings" {
		t.Errorf("args.q = %v", args["q"])
	}
}

func TestCompleteGoogle_NormalizesNonObjectToolArguments(t *testing.T) {
	cases := []struct {
		name string
		args any
	}{
		{name: "string", args: "hello"},
		{name: "number", args: 42},
		{name: "array", args: []any{1, 2, 3}},
		{name: "malformed", args: "{not-json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"candidates": []map[string]any{{"content": map[string]any{
						"parts": []map[string]any{{"functionCall": map[string]any{
							"id": "call_invalid", "name": "search_notes", "args": tc.args,
						}}},
					}}},
				})
			}))
			defer srv.Close()

			res, err := Complete(context.Background(), CompleteRequest{
				Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
				Messages: []ChatMessage{{Role: RoleUser, Content: "find meetings"}},
			})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(res.ToolCalls) != 1 {
				t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
			}
			if got := string(res.ToolCalls[0].Arguments); got != "{}" {
				t.Errorf("arguments = %q, want {}", got)
			}
		})
	}
}

func TestCompleteGoogle_PreservesOpaqueFunctionCallID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{
					{"functionCall": map[string]any{
						"id": "call_google_01", "name": "search_notes",
						"args": map[string]any{"q": "meetings"},
					}},
				}}},
			},
		})
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{{Role: "user", Content: "find meetings"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(res.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(res.ToolCalls))
	}
	tc := res.ToolCalls[0]
	if tc.Name != "search_notes" || tc.ID != "call_google_01" {
		t.Errorf("tool_call = %+v, want name search_notes and id call_google_01", tc)
	}
}

func TestCompleteGoogle_RoundTripsOpaqueFunctionCallIDWithFunctionName(t *testing.T) {
	var captured []googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req googleGenerateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse request: %v", err)
		}
		captured = append(captured, req)
		w.Header().Set("Content-Type", "application/json")
		if len(captured) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{{"content": map[string]any{
					"parts": []map[string]any{{"functionCall": map[string]any{
						"id": "call_google_opaque", "name": "search_notes", "args": map[string]any{"q": "meetings"},
					}}},
				}}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{
				"parts": []map[string]any{{"text": "done"}},
			}}},
		})
	}))
	defer srv.Close()

	provider := AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"}
	first, err := Complete(context.Background(), CompleteRequest{
		Provider: provider,
		Messages: []ChatMessage{{Role: RoleUser, Content: "find meetings"}},
		Tools:    []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Name != "search_notes" || first.ToolCalls[0].ID != "call_google_opaque" {
		t.Fatalf("first tool call = %+v", first.ToolCalls)
	}

	_, err = Complete(context.Background(), CompleteRequest{
		Provider: provider,
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "find meetings"},
			{Role: RoleAssistant, ToolCalls: first.ToolCalls},
			{Role: RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: `{"count":3}`},
		},
		Tools: []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("requests = %d, want 2", len(captured))
	}
	contents := captured[1].Contents
	if len(contents) != 3 || len(contents[2].Parts) != 1 || contents[2].Parts[0].FunctionResponse == nil {
		t.Fatalf("second request contents = %+v", contents)
	}
	response := contents[2].Parts[0].FunctionResponse
	if response.Name != "search_notes" {
		t.Errorf("functionResponse name = %q, want search_notes", response.Name)
	}
	if response.ID != "call_google_opaque" {
		t.Errorf("functionResponse id = %q, want call_google_opaque", response.ID)
	}
}

func TestCompleteGoogle_EncodesToolResultAsFunctionResponse(t *testing.T) {
	var captured googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}},
			},
		})
	}))
	defer srv.Close()
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{
			{Role: "user", Content: "find meetings"},
			{Role: "assistant", ToolCalls: []ToolCall{
				{ID: "call_google_01", Name: "search_notes", Arguments: json.RawMessage(`{"q":"meetings"}`)},
			}},
			{Role: "tool", ToolCallID: "call_google_01", Content: `{"count":3}`},
		},
		Tools: []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// contents: user, model(functionCall), user(functionResponse).
	if len(captured.Contents) != 3 {
		t.Fatalf("contents = %d, want 3", len(captured.Contents))
	}
	model := captured.Contents[1]
	if model.Role != "model" {
		t.Errorf("assistant turn role = %q, want model", model.Role)
	}
	if len(model.Parts) != 1 || model.Parts[0].FunctionCall == nil {
		t.Fatalf("model part missing functionCall: %+v", model.Parts)
	}
	if model.Parts[0].FunctionCall.ID != "call_google_01" || model.Parts[0].FunctionCall.Name != "search_notes" {
		t.Errorf("functionCall = %+v, want id call_google_01 and name search_notes", model.Parts[0].FunctionCall)
	}
	res := captured.Contents[2]
	if res.Role != "user" {
		t.Errorf("tool result role = %q, want user", res.Role)
	}
	if len(res.Parts) != 1 || res.Parts[0].FunctionResponse == nil {
		t.Fatalf("tool result missing functionResponse: %+v", res.Parts)
	}
	fr := res.Parts[0].FunctionResponse
	if fr.Name != "search_notes" {
		t.Errorf("functionResponse name = %q, want search_notes", fr.Name)
	}
	if fr.ID != "call_google_01" {
		t.Errorf("functionResponse id = %q, want call_google_01", fr.ID)
	}
	var resp map[string]any
	if err := json.Unmarshal(fr.Response, &resp); err != nil {
		t.Fatalf("response not object: %v", err)
	}
	if resp["count"] != float64(3) {
		t.Errorf("response.count = %v, want 3", resp["count"])
	}
}

// Gemini 3+ requires thoughtSignature on functionCall parts to be echoed on
// the next multi-turn request (#915). Capture on decode and re-attach on encode.
func TestCompleteGoogle_RoundTripsThoughtSignature(t *testing.T) {
	const sig = "opaque-thought-sig-abc123"
	var captured []googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req googleGenerateRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("parse request: %v", err)
		}
		captured = append(captured, req)
		w.Header().Set("Content-Type", "application/json")
		if len(captured) == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"candidates": []map[string]any{{"content": map[string]any{
					"parts": []map[string]any{{
						"functionCall": map[string]any{
							"id": "call_sig", "name": "search_notes",
							"args": map[string]any{"q": "plant"},
						},
						"thoughtSignature": sig,
					}},
				}}},
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{
				"parts": []map[string]any{{"text": "The plant is in Settings."}},
			}}},
		})
	}))
	defer srv.Close()

	provider := AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-3-flash-lite"}
	first, err := Complete(context.Background(), CompleteRequest{
		Provider: provider,
		Messages: []ChatMessage{{Role: RoleUser, Content: "Where is the plant?"}},
		Tools:    []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("first Complete: %v", err)
	}
	if len(first.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, want 1", len(first.ToolCalls))
	}
	if first.ToolCalls[0].ThoughtSignature != sig {
		t.Fatalf("ThoughtSignature = %q, want %q", first.ToolCalls[0].ThoughtSignature, sig)
	}

	_, err = Complete(context.Background(), CompleteRequest{
		Provider: provider,
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "Where is the plant?"},
			{Role: RoleAssistant, ToolCalls: first.ToolCalls},
			{Role: RoleTool, ToolCallID: first.ToolCalls[0].ID, Content: `{"hits":1}`},
		},
		Tools: []ToolDef{dummyTool()},
	})
	if err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("requests = %d, want 2", len(captured))
	}
	// Second request: user, model(functionCall+sig), user(functionResponse).
	model := captured[1].Contents[1]
	if model.Role != "model" || len(model.Parts) != 1 {
		t.Fatalf("model turn = %+v", model)
	}
	if model.Parts[0].FunctionCall == nil {
		t.Fatal("missing functionCall on second request")
	}
	if model.Parts[0].ThoughtSignature != sig {
		t.Errorf("egress thoughtSignature = %q, want %q", model.Parts[0].ThoughtSignature, sig)
	}
	// Raw JSON must use camelCase thoughtSignature (Google wire form).
	raw, _ := json.Marshal(model.Parts[0])
	if !strings.Contains(string(raw), `"thoughtSignature":"`+sig+`"`) {
		t.Errorf("egress part JSON missing camelCase thoughtSignature: %s", raw)
	}
}

func TestCompleteGoogle_ThoughtSignatureSnakeCaseIngress(t *testing.T) {
	const sig = "snake-sig-xyz"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{
				"parts": []map[string]any{{
					"functionCall": map[string]any{
						"name": "search_notes", "args": map[string]any{"q": "x"},
					},
					"thought_signature": sig,
				}},
			}}},
		})
	}))
	defer srv.Close()

	res, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-3-flash-lite"},
		Messages: []ChatMessage{{Role: RoleUser, Content: "x"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(res.ToolCalls) != 1 || res.ToolCalls[0].ThoughtSignature != sig {
		t.Fatalf("tool_call = %+v, want ThoughtSignature %q", res.ToolCalls, sig)
	}
}

func TestCompleteGoogle_ParallelToolCallsThoughtSignatureOnFirstOnly(t *testing.T) {
	const sig = "first-only-sig"
	var captured googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{{"content": map[string]any{
				"parts": []map[string]any{{"text": "ok"}},
			}}},
		})
	}))
	defer srv.Close()

	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-3-flash-lite"},
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "parallel"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "c1", Name: "search_notes", Arguments: json.RawMessage(`{"q":"a"}`), ThoughtSignature: sig},
				{ID: "c2", Name: "read_blocks", Arguments: json.RawMessage(`{"ids":["1"]}`)},
			}},
			{Role: RoleTool, ToolCallID: "c1", Content: `{}`},
			{Role: RoleTool, ToolCallID: "c2", Content: `{}`},
		},
		Tools: []ToolDef{
			dummyTool(),
			{Name: "read_blocks", Parameters: json.RawMessage(`{"type":"object"}`)},
		},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	model := captured.Contents[1]
	if len(model.Parts) != 2 {
		t.Fatalf("model parts = %d, want 2", len(model.Parts))
	}
	if model.Parts[0].ThoughtSignature != sig {
		t.Errorf("first FC signature = %q, want %q", model.Parts[0].ThoughtSignature, sig)
	}
	if model.Parts[1].ThoughtSignature != "" {
		t.Errorf("second FC signature = %q, want empty", model.Parts[1].ThoughtSignature)
	}
}

// Pins #637 single-pass id→name index: two prior tool_calls with distinct ids
// must resolve to the correct function names on the tool-result turn (O(1)
// map lookup, not an O(n) scan that could mis-associate names).
func TestCompleteGoogle_MultiToolResultNameIndex(t *testing.T) {
	var captured googleGenerateRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &captured)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"candidates": []map[string]any{
				{"content": map[string]any{"parts": []map[string]any{{"text": "ok"}}}},
			},
		})
	}))
	defer srv.Close()

	tools := []ToolDef{
		dummyTool(),
		{Name: "read_blocks", Description: "read", Parameters: json.RawMessage(`{"type":"object"}`)},
	}
	_, err := Complete(context.Background(), CompleteRequest{
		Provider: AIProvider{ProviderType: ProviderGoogle, BaseURL: srv.URL, Model: "gemini-2.0-flash"},
		Messages: []ChatMessage{
			{Role: RoleUser, Content: "search then read"},
			{Role: RoleAssistant, ToolCalls: []ToolCall{
				{ID: "call_a", Name: "search_notes", Arguments: json.RawMessage(`{"q":"x"}`)},
				{ID: "call_b", Name: "read_blocks", Arguments: json.RawMessage(`{"ids":["1"]}`)},
			}},
			{Role: RoleTool, ToolCallID: "call_a", Content: `{"hits":1}`},
			{Role: RoleTool, ToolCallID: "call_b", Content: `{"text":"body"}`},
		},
		Tools: tools,
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// user, model(2 functionCalls), user(functionResponse a), user(functionResponse b)
	if len(captured.Contents) != 4 {
		t.Fatalf("contents = %d, want 4; got %+v", len(captured.Contents), captured.Contents)
	}
	frA := captured.Contents[2].Parts[0].FunctionResponse
	frB := captured.Contents[3].Parts[0].FunctionResponse
	if frA == nil || frB == nil {
		t.Fatalf("missing functionResponse parts: a=%+v b=%+v", captured.Contents[2], captured.Contents[3])
	}
	if frA.Name != "search_notes" || frA.ID != "call_a" {
		t.Errorf("first tool result = name=%q id=%q, want search_notes/call_a", frA.Name, frA.ID)
	}
	if frB.Name != "read_blocks" || frB.ID != "call_b" {
		t.Errorf("second tool result = name=%q id=%q, want read_blocks/call_b", frB.Name, frB.ID)
	}
}
