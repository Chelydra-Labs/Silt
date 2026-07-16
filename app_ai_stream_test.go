package main

// App-layer stream lifecycle tests for PluginAIComplete(stream=true) /
// PluginAICancelStream / PluginAIStreamReady (#226, PR #540 review).
// Complements backend/ai/stream_test.go (SSE parse) and the frontend SDK
// tests — this file pins wg/drain balancing, ownership, and the ready handshake.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// sseHoldServer streams one delta then blocks until the request context is
// cancelled (user Stop / vault close). Signals when the first delta is sent.
func sseHoldServer(t *testing.T) (srv *httptest.Server, firstDelta <-chan struct{}) {
	t.Helper()
	ch := make(chan struct{})
	var once atomic.Bool
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		if !strings.Contains(string(body), `"stream":true`) {
			t.Errorf("expected stream:true in body, got %s", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"model":"m","choices":[{"delta":{"content":"hi"}}]}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		if once.CompareAndSwap(false, true) {
			close(ch)
		}
		<-r.Context().Done()
	}))
	return srv, ch
}

func TestPluginAIComplete_Stream_StartReadyCancel_DrainsWG(t *testing.T) {
	app := newTestApp(t)
	// Clear disabled so first-party AI plugins can exercise CapAI if needed;
	// silt-tasks is not disabled by default.
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.configMu.Unlock()

	srv, firstDelta := sseHoldServer(t)
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("PluginAIComplete stream start: %v", err)
	}
	if res.StreamID == "" {
		t.Fatal("stream start returned empty stream_id")
	}

	// Handshake: frontend attached listeners.
	if err := app.PluginAIStreamReady("silt-tasks", tok, res.StreamID); err != nil {
		t.Fatalf("PluginAIStreamReady: %v", err)
	}

	select {
	case <-firstDelta:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not emit first delta after ready")
	}

	// Cancel mid-flight — must abort upstream and balance vaultClosingWG + a.wg.
	if err := app.PluginAICancelStream("silt-tasks", tok, res.StreamID); err != nil {
		t.Fatalf("PluginAICancelStream: %v", err)
	}

	// vaultClosingWG must drain promptly (stream goroutine Done'd).
	waitDone := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(3 * time.Second):
		t.Fatal("vaultClosingWG.Wait() did not return after stream cancel — drain leak")
	}

	// a.wg must also be balanced (PluginAIComplete's Add).
	wgDone := make(chan struct{})
	go func() {
		app.wg.Wait()
		close(wgDone)
	}()
	select {
	case <-wgDone:
	case <-time.After(3 * time.Second):
		t.Fatal("a.wg.Wait() did not return after stream cancel — wg leak")
	}
}

func TestPluginAICancelStream_WrongPluginID_IsNoOp(t *testing.T) {
	app := newTestApp(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.configMu.Unlock()

	srv, firstDelta := sseHoldServer(t)
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	ownerTok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession owner: %v", err)
	}
	otherTok, err := app.RegisterPluginSession("silt-attachments")
	if err != nil {
		t.Fatalf("RegisterPluginSession other: %v", err)
	}

	res, err := app.PluginAIComplete("silt-tasks", ownerTok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream start: %v", err)
	}
	if err := app.PluginAIStreamReady("silt-tasks", ownerTok, res.StreamID); err != nil {
		t.Fatalf("ready: %v", err)
	}
	select {
	case <-firstDelta:
	case <-time.After(5 * time.Second):
		t.Fatal("no first delta")
	}

	// Foreign plugin cancel must succeed as a no-op (no error) without killing
	// the owner's stream session.
	if err := app.PluginAICancelStream("silt-attachments", otherTok, res.StreamID); err != nil {
		t.Fatalf("foreign cancel: %v", err)
	}

	// Owner session must still be registered.
	app.aiStreamsMu.Lock()
	_, stillLive := app.aiStreams[res.StreamID]
	app.aiStreamsMu.Unlock()
	if !stillLive {
		t.Fatal("foreign cancel removed owner stream — ownership check regression")
	}

	// Owner can still cancel.
	if err := app.PluginAICancelStream("silt-tasks", ownerTok, res.StreamID); err != nil {
		t.Fatalf("owner cancel: %v", err)
	}
	// Drain.
	done := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("drain after owner cancel")
	}
}

func TestPluginAIStreamReady_UnknownStream_IsNoOp(t *testing.T) {
	app := newTestApp(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.configMu.Unlock()
	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	if err := app.PluginAIStreamReady("silt-tasks", tok, "does-not-exist"); err != nil {
		t.Fatalf("ready unknown stream: %v", err)
	}
	if err := app.PluginAICancelStream("silt-tasks", tok, "does-not-exist"); err != nil {
		t.Fatalf("cancel unknown stream: %v", err)
	}
}

func TestPluginAIComplete_Stream_ReadyTimeout_StillStarts(t *testing.T) {
	// If the client never acks ready, the producer must still start after
	// aiStreamReadyWait rather than hang forever.
	app := newTestApp(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.configMu.Unlock()

	// Fast complete SSE (no hold) so we observe completion after timeout.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, `data: {"model":"m","choices":[{"delta":{"content":"ok"}}]}`+"\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if flusher != nil {
			flusher.Flush()
		}
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}

	res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream start: %v", err)
	}
	if res.StreamID == "" {
		t.Fatal("empty stream_id")
	}
	// Deliberately skip PluginAIStreamReady — wait for ready-timeout path.

	// Stream goroutine must finish (timeout + short SSE) and balance the WG.
	// aiStreamReadyWait is 2s; allow generous headroom.
	done := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(aiStreamReadyWait + 5*time.Second):
		t.Fatal("stream did not complete via ready-timeout fallback")
	}
}

func TestPluginAIComplete_Stream_RejectsInvalidSession(t *testing.T) {
	app := newTestApp(t)
	_, err := app.PluginAIComplete("silt-tasks", "bad-token", PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "x"}},
		Stream:   true,
	})
	if err == nil {
		t.Fatal("stream start with bad session must fail")
	}
	if err := app.PluginAICancelStream("silt-tasks", "bad-token", "any"); err == nil {
		t.Fatal("cancel with bad session must fail")
	}
	if err := app.PluginAIStreamReady("silt-tasks", "bad-token", "any"); err == nil {
		t.Fatal("ready with bad session must fail")
	}
}

func TestPluginAIComplete_Stream_EmitsToolDeltas(t *testing.T) {
	// #631: injectable emit captures owner-scoped tool-delta + done tool_calls.
	app := newTestApp(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.configMu.Unlock()

	type captured struct {
		name string
		data map[string]any
	}
	var (
		mu   sync.Mutex
		got  []captured
		done = make(chan struct{})
	)
	app.eventEmit = func(name string, data ...any) {
		var payload map[string]any
		if len(data) > 0 {
			if m, ok := data[0].(map[string]any); ok {
				payload = m
			}
		}
		mu.Lock()
		got = append(got, captured{name: name, data: payload})
		if strings.HasPrefix(name, aiEventCompleteDone) || strings.HasPrefix(name, aiEventCompleteError) {
			select {
			case <-done:
			default:
				close(done)
			}
		}
		mu.Unlock()
	}

	// SSE with split tool_calls fragments (OpenAI stream shape).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		chunks := []string{
			`data: {"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"search_notes","arguments":""}}]}}]}`,
			`data: {"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":"}}]}}]}`,
			`data: {"model":"m","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"x\"}"}}]}}]}`,
			`data: {"model":"m","choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
			`data: [DONE]`,
		}
		for _, c := range chunks {
			fmt.Fprint(w, c+"\n\n")
			if flusher != nil {
				flusher.Flush()
			}
		}
	}))
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	const pluginID = "silt-tasks"
	tok, err := app.RegisterPluginSession(pluginID)
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIComplete(pluginID, tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("stream start: %v", err)
	}
	if err := app.PluginAIStreamReady(pluginID, tok, res.StreamID); err != nil {
		t.Fatalf("ready: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for stream done/error emit")
	}

	// Drain stream goroutine.
	wgDone := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(wgDone)
	}()
	select {
	case <-wgDone:
	case <-time.After(3 * time.Second):
		t.Fatal("vaultClosingWG did not drain")
	}

	mu.Lock()
	defer mu.Unlock()
	wantToolEvent := aiStreamEventName(aiEventCompleteToolDelta, pluginID)
	wantDoneEvent := aiStreamEventName(aiEventCompleteDone, pluginID)
	var toolDeltas int
	var sawDone bool
	for _, e := range got {
		if e.name == wantToolEvent {
			toolDeltas++
			if e.data["stream_id"] != res.StreamID {
				t.Errorf("tool-delta stream_id = %v, want %s", e.data["stream_id"], res.StreamID)
			}
			if e.data["plugin_id"] != pluginID {
				t.Errorf("tool-delta plugin_id = %v", e.data["plugin_id"])
			}
		}
		if e.name == wantDoneEvent {
			sawDone = true
			tcs, ok := e.data["tool_calls"]
			if !ok {
				t.Fatal("done payload missing tool_calls")
			}
			// tool_calls is []ai.ToolCall; JSON-ish via map may keep typed slice.
			raw, _ := json.Marshal(tcs)
			if !strings.Contains(string(raw), "search_notes") {
				t.Errorf("done tool_calls = %s, want search_notes", raw)
			}
			if !strings.Contains(string(raw), "call_1") {
				t.Errorf("done tool_calls = %s, want call_1", raw)
			}
		}
		// Must not emit unscoped global names for this stream.
		if e.name == aiEventCompleteToolDelta || e.name == aiEventCompleteDone {
			t.Errorf("unscoped event %q emitted; want owner-scoped names", e.name)
		}
	}
	if toolDeltas == 0 {
		t.Fatal("expected at least one owner-scoped tool-delta emit")
	}
	if !sawDone {
		t.Fatal("expected owner-scoped done emit")
	}
}

// Ensure json import stays used if future tests drop encoder usage.
var _ = json.Marshal

// TestUpdateAIFeatures_DisablingCancelsInFlightStreams: turning master AI off
// must abort any in-flight provider stream immediately instead of letting it
// run to its per-call timeout (consuming tokens/cost) — the frontend teardown
// path (PluginAICancelStream) rejects once the AI grant is revoked, so the
// cancel cannot depend on it (#632).
func TestUpdateAIFeatures_DisablingCancelsInFlightStreams(t *testing.T) {
	app := newTestApp(t)
	app.configMu.Lock()
	app.cfg.Plugins.Disabled = nil
	app.cfg.AI.Features.Enabled = true
	app.configMu.Unlock()

	srv, firstDelta := sseHoldServer(t)
	defer srv.Close()
	pointAIProviderAt(t, app, "chat", srv.URL, "test")

	tok, err := app.RegisterPluginSession("silt-tasks")
	if err != nil {
		t.Fatalf("RegisterPluginSession: %v", err)
	}
	res, err := app.PluginAIComplete("silt-tasks", tok, PluginAICompleteInput{
		Messages: []PluginAIChatMessage{{Role: "user", Content: "ping"}},
		Stream:   true,
	})
	if err != nil {
		t.Fatalf("PluginAIComplete stream start: %v", err)
	}
	if err := app.PluginAIStreamReady("silt-tasks", tok, res.StreamID); err != nil {
		t.Fatalf("PluginAIStreamReady: %v", err)
	}
	select {
	case <-firstDelta:
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not emit first delta after ready")
	}

	app.aiStreamsMu.Lock()
	live := len(app.aiStreams)
	app.aiStreamsMu.Unlock()
	if live != 1 {
		t.Fatalf("expected 1 live stream before disable, got %d", live)
	}

	if err := app.UpdateAIFeatures(AIFeaturesPatch{Enabled: boolPtrAI(false)}); err != nil {
		t.Fatalf("UpdateAIFeatures: %v", err)
	}

	// The stream's drain must be released (goroutine cancelled + Done'd).
	waitDone := make(chan struct{})
	go func() {
		app.vaultClosingWG.Wait()
		close(waitDone)
	}()
	select {
	case <-waitDone:
	case <-time.After(3 * time.Second):
		t.Fatal("vaultClosingWG.Wait() did not return after AI off — stream was not cancelled")
	}
	// Cleanup defer must have removed the cancelled stream.
	app.aiStreamsMu.Lock()
	gone := app.aiStreams[res.StreamID] == nil
	app.aiStreamsMu.Unlock()
	if !gone {
		t.Error("expected the disabled stream to be removed from aiStreams")
	}
}
