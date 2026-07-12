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

// Ensure json import stays used if future tests drop encoder usage.
var _ = json.Marshal
