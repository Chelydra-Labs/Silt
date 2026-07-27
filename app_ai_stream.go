package main

// =========================================================================
// AI completion streaming runtime (#226, #762)
// =========================================================================
//
// Goroutine fan-out, ready handshake, bounded-channel backpressure, cancel,
// and the aiStreamSession type. Split from app_ai_plugin.go (gateway stays
// there) and app.go (App fields stay; only the session type moves here).
// cancelAllAIStreams lives here too (called from UpdateAIFeatures when AI
// is turned off).

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"silt/backend/ai"
	"silt/backend/plugins"
	"strings"
	"sync"
	"time"
)

// AI stream event bases live in events.go as the canonical EventAIComplete*
// EventName consts (#226/#635). The full owner-scoped name appends
// ":"+pluginID so concurrent plugin streams do not share a global bus.
// Payload still includes plugin_id for debugging.

// aiStreamEventName returns the owner-scoped Wails event name for a stream
// event base + pluginID (#635). The result is EventName-typed (constructed
// from a declared base const) so it flows through emit without a cast.
func aiStreamEventName(base EventName, pluginID string) EventName {
	if pluginID == "" {
		return base
	}
	return EventName(string(base) + ":" + pluginID)
}

// aiStreamBufferCap is the max number of unconsumed delta events buffered per
// stream before the producer blocks (natural backpressure). Generous for UI
// consumers that coalesce on rAF; tight enough to bound memory if a plugin stalls.
const aiStreamBufferCap = 256

// aiStreamReadyWait is how long the producer waits for PluginAIStreamReady
// before starting anyway. Covers the IPC round-trip for listener attach; if the
// client never acks (crashed plugin), the stream still proceeds rather than
// hanging until the provider timeout.
const aiStreamReadyWait = 2 * time.Second

// aiStreamSession is one in-flight PluginAIComplete(stream=true) call.
// ready is closed when the frontend has attached Events.On listeners
// (PluginAIStreamReady) so terminal events are not lost to a race.
type aiStreamSession struct {
	pluginID  string
	cancel    context.CancelFunc
	ready     chan struct{}
	readyOnce sync.Once
}

// startAIStream launches an async CompleteStream and returns stream_id immediately.
// The caller's a.wg.Add(1) is balanced when the stream goroutine finishes.
// drainDone is deferred inside the goroutine so vault-close waits for the stream.
func (a *App) startAIStream(pluginID string, provider ai.AIProvider, effectiveModel string, req ai.CompleteRequest, drainDone func()) (ai.CompleteResult, error) {
	// Owner-scoped stream events are named ":<pluginID>"; an empty id would
	// fall back to the global unscoped bus (#635). Preflight validates this, but
	// assert structurally so a future refactor cannot silently regress it.
	if pluginID == "" {
		a.wg.Done()
		drainDone()
		return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrBadRequest, Message: "plugin_id is required for a streamed completion"}
	}
	streamID, err := newAIStreamID()
	if err != nil {
		a.wg.Done()
		drainDone()
		return ai.CompleteResult{}, &ai.AIError{Kind: ai.ErrUnknown, Message: fmt.Sprintf("allocate stream id: %v", err)}
	}
	// Child of vault/app AI context so close/shutdown cancels the HTTP body.
	streamCtx, streamCancel := context.WithCancel(a.aiContext())

	ready := make(chan struct{})
	a.aiStreamsMu.Lock()
	if a.aiStreams == nil {
		a.aiStreams = make(map[string]*aiStreamSession)
	}
	a.aiStreams[streamID] = &aiStreamSession{
		pluginID: pluginID,
		cancel:   streamCancel,
		ready:    ready,
	}
	a.aiStreamsMu.Unlock()

	// Buffered channel for backpressure between SSE reader and event emit.
	// Producer blocks if the buffer fills (consumer not keeping up).
	deltaCh := make(chan string, aiStreamBufferCap)
	toolDeltaCh := make(chan ai.ToolCallDelta, aiStreamBufferCap)

	// Audit stream start (one row); terminal status is audited when the
	// goroutine finishes (#226 — not per-token).
	a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, "stream-start", nil)

	go func() {
		defer a.wg.Done()
		defer drainDone()
		defer streamCancel()
		defer func() {
			a.aiStreamsMu.Lock()
			delete(a.aiStreams, streamID)
			a.aiStreamsMu.Unlock()
		}()

		// Wait for the frontend to attach Events.On listeners (PluginAIStreamReady)
		// before starting the upstream request. Immediate failures (native
		// provider reject, empty model) would otherwise emit done/error before
		// createAIStream installs handlers, leaving the client hung (PR #540).
		select {
		case <-ready:
		case <-time.After(aiStreamReadyWait):
		case <-streamCtx.Done():
			a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, "cancelled", nil)
			a.emit(aiStreamEventName(EventAICompleteError, pluginID), map[string]any{
				"stream_id": streamID,
				"plugin_id": pluginID,
				"kind":      string(ai.ErrCanceled),
				"message":   "stream cancelled before start",
			})
			return
		}

		// Fan-out deltas to Wails events on a separate goroutine so the SSE
		// parser only blocks on the bounded channel (backpressure), not on IPC.
		// Event names are owner-scoped by pluginID (#635).
		emitDone := make(chan struct{})
		go func() {
			defer close(emitDone)
			idx := 0
			for delta := range deltaCh {
				a.emit(aiStreamEventName(EventAICompleteDelta, pluginID), map[string]any{
					"stream_id": streamID,
					"plugin_id": pluginID,
					"delta":     delta,
					"index":     idx,
				})
				idx++
			}
		}()

		// Fan-out tool-call fragments to a parallel event so the chat UX can
		// surface in-progress tool invocations live (#595).
		emitToolDone := make(chan struct{})
		go func() {
			defer close(emitToolDone)
			for frag := range toolDeltaCh {
				a.emit(aiStreamEventName(EventAICompleteToolDelta, pluginID), map[string]any{
					"stream_id":          streamID,
					"plugin_id":          pluginID,
					"index":              frag.Index,
					"id":                 frag.ID,
					"name":               frag.Name,
					"arguments_fragment": frag.ArgumentsFragment,
				})
			}
		}()

		// Natural backpressure: block until the emit goroutine drains a slot
		// or the stream is cancelled. A default arm would turn a momentary
		// full buffer into a hard abort mid-answer (PR #540 review).
		result, callErr := ai.CompleteStream(streamCtx, req, func(delta string) error {
			select {
			case deltaCh <- delta:
				return nil
			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		}, func(frag ai.ToolCallDelta) error {
			select {
			case toolDeltaCh <- frag:
				return nil
			case <-streamCtx.Done():
				return streamCtx.Err()
			}
		})
		close(deltaCh)
		close(toolDeltaCh)
		<-emitDone
		<-emitToolDone

		status := "ok"
		if callErr != nil {
			status = aiErrKind(callErr)
			// Cancellation is a first-class terminal status for audit.
			if streamCtx.Err() != nil && (errors.Is(callErr, context.Canceled) || strings.Contains(callErr.Error(), "cancel")) {
				status = "cancelled"
			}
			a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, status, nil)
			kind, msg := "unknown", callErr.Error()
			if e, ok := callErr.(*ai.AIError); ok {
				kind, msg = string(e.Kind), e.Message
			}
			a.emit(aiStreamEventName(EventAICompleteError, pluginID), map[string]any{
				"stream_id": streamID,
				"plugin_id": pluginID,
				"kind":      kind,
				"message":   msg,
			})
			return
		}
		a.auditAI(pluginID, aiChatKind, provider.BaseURL, effectiveModel, status, result.Usage)
		payload := map[string]any{
			"stream_id": streamID,
			"plugin_id": pluginID,
			"content":   result.Content,
			"model":     result.Model,
		}
		if len(result.ToolCalls) > 0 {
			payload["tool_calls"] = result.ToolCalls
		}
		if result.Usage != nil {
			payload["usage"] = result.Usage
		}
		a.emit(aiStreamEventName(EventAICompleteDone, pluginID), payload)
	}()

	return ai.CompleteResult{StreamID: streamID, Model: effectiveModel}, nil
}

// PluginAICancelStream aborts an in-flight streamed completion started by
// PluginAIComplete(stream=true). The plugin must own the stream (pluginID match).
// Idempotent: cancelling an unknown/finished stream is a no-op success.
func (a *App) PluginAICancelStream(pluginID, sessionToken, streamID string) error {
	if err := a.requirePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return err
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: "stream_id is required"}
	}
	a.aiStreamsMu.Lock()
	sess, ok := a.aiStreams[streamID]
	if ok && sess.pluginID == pluginID {
		// Leave the map entry; the stream goroutine removes it on exit.
		cancel := sess.cancel
		// Unblock a producer still waiting on ready so it observes cancel.
		if sess.ready != nil {
			sess.readyOnce.Do(func() { close(sess.ready) })
		}
		a.aiStreamsMu.Unlock()
		cancel()
		return nil
	}
	a.aiStreamsMu.Unlock()
	return nil
}

// PluginAIStreamReady signals that the frontend has attached Events.On
// listeners for streamID and is ready to receive deltas/terminal events.
// Must be called after PluginAIComplete(stream=true) returns stream_id.
// Idempotent; unknown streams are a no-op success.
func (a *App) PluginAIStreamReady(pluginID, sessionToken, streamID string) error {
	if err := a.requirePluginSession(pluginID, sessionToken); err != nil {
		return err
	}
	if err := a.requireGrant(pluginID, plugins.CapAI); err != nil {
		return err
	}
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return &ai.AIError{Kind: ai.ErrBadRequest, Message: "stream_id is required"}
	}
	a.aiStreamsMu.Lock()
	sess, ok := a.aiStreams[streamID]
	if ok && sess.pluginID == pluginID && sess.ready != nil {
		sess.readyOnce.Do(func() { close(sess.ready) })
	}
	a.aiStreamsMu.Unlock()
	return nil
}

func newAIStreamID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// cancelAllAIStreams aborts every in-flight streamed completion. Used when AI
// is turned off so active provider requests stop immediately instead of
// running to their per-call timeout. Mirrors PluginAICancelStream's per-stream
// teardown (close the ready gate, then cancel) but for the whole map. The
// ready gate is closed so a producer still blocked on the ready handshake
// observes the cancel (#632).
func (a *App) cancelAllAIStreams() {
	a.aiStreamsMu.Lock()
	sessions := make([]*aiStreamSession, 0, len(a.aiStreams))
	for _, s := range a.aiStreams {
		sessions = append(sessions, s)
	}
	a.aiStreamsMu.Unlock()
	// Cancel outside the lock: each stream's goroutine re-acquires aiStreamsMu
	// in its cleanup defer to delete itself, so holding it across cancel() would
	// self-deadlock.
	for _, s := range sessions {
		if s.ready != nil {
			s.readyOnce.Do(func() { close(s.ready) })
		}
		s.cancel()
	}
}
