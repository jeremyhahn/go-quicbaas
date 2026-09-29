// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

//go:build desktop

package main

import (
	"context"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// SessionMeta is the Wails-facing summary of an orchestrator session. It
// mirrors the shape the Svelte frontend expects (see
// frontend/src/stores/orchestrator.svelte.ts → SessionInfo). The Go struct
// is explicit so Wails' TypeScript generator emits a stable type rather
// than an opaque `Record<string, any>`.
type SessionMeta struct {
	SessionID   string `json:"session_id"`
	TenantID    string `json:"tenant_id,omitempty"`
	Backend     string `json:"backend"`
	Status      string `json:"status"`
	ContainerID string `json:"container_id,omitempty"`
	CreatedAt   int64  `json:"created_at"`
	LastActive  int64  `json:"last_active"`
}

// SessionBinding is the Wails IPC surface the Svelte frontend uses to
// interact with sessions: spawn/prompt/close lifecycle, historical event
// retrieval, and live event subscriptions. Live subscriptions are tracked
// per sessionID so Unsubscribe can terminate the forwarding goroutine
// deterministically.
type SessionBinding struct {
	logger *logger.Logger
	conn   *ConnectionBinding
	ctx    context.Context

	subsMu sync.Mutex
	subs   map[string]*sessionSubscription
}

// sessionSubscription is one registered live stream. The registry stores
// it by pointer so a releaser can tell whether the entry under a sessionID
// is still the one it registered: a forwarder that exits after a
// resubscribe must not remove (and cancel) its replacement.
type sessionSubscription struct {
	cancel context.CancelFunc
}

// NewSessionBinding creates a SessionBinding that reuses the SDK client
// managed by the provided ConnectionBinding. The session binding never
// constructs its own SDK client — the single client connection is owned
// by ConnectionBinding and shared across all binding consumers.
func NewSessionBinding(log *logger.Logger, conn *ConnectionBinding) *SessionBinding {
	return &SessionBinding{
		logger: log,
		conn:   conn,
		subs:   make(map[string]*sessionSubscription),
	}
}

// startup is called by Wails when the application starts. It stores the
// application context for use in all subsequent subscribe operations.
func (b *SessionBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is called by Wails when the application is closing. It cancels
// every live subscription goroutine so no forwarder outlives the process.
func (b *SessionBinding) shutdown(ctx context.Context) {
	b.subsMu.Lock()
	defer b.subsMu.Unlock()
	for id, sub := range b.subs {
		sub.cancel()
		delete(b.subs, id)
	}
}

// Spawn creates a new agent session. It forwards the SDK request verbatim
// to the orchestrator; validation (required backend, sandbox mode, etc.)
// is performed server-side via the SDK's own validator.
func (b *SessionBinding) Spawn(req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("SessionBinding.Spawn")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().SpawnSession(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.Spawn", err)
	}
	return resp, nil
}

// Prompt sends a prompt to an existing session and returns the acceptance
// acknowledgement — turn ID, status, and the session-log sequence to attach
// from. The turn runs in the background and its output arrives on the session
// event stream, which the frontend already consumes via Subscribe; Prompt
// never carries the agent's reply.
func (b *SessionBinding) Prompt(req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("SessionBinding.Prompt")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().PromptSession(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.Prompt", err)
	}
	return resp, nil
}

// Close terminates an active session and releases its resources.
func (b *SessionBinding) Close(req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("SessionBinding.Close")
	}
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().CloseSession(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.Close", err)
	}
	return resp, nil
}

// List returns all sessions visible to the active connection, optionally
// filtered by backend / status passed in the filter map. The filter keys
// match the ListSessionsRequest field names ("backend", "status"); unknown
// keys are ignored.
func (b *SessionBinding) List(filter map[string]interface{}) ([]SessionMeta, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	req := &orchestratorSDK.ListSessionsRequest{}
	if v, ok := filter["backend"].(string); ok {
		req.Backend = v
	}
	if v, ok := filter["status"].(string); ok {
		req.Status = v
	}

	resp, err := client.Orchestrator().ListSessions(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.List", err)
	}

	result := make([]SessionMeta, 0, len(resp.Sessions))
	for _, s := range resp.Sessions {
		result = append(result, sessionMetaFromSDK(s))
	}
	return result, nil
}

// Get returns full session meta for a single session. It is a thin wrapper
// around the SDK's SessionStatus call.
func (b *SessionBinding) Get(sessionID string) (*SessionMeta, error) {
	if sessionID == "" {
		return nil, NewGUISessionIDRequiredError("SessionBinding.Get")
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().SessionStatus(b.ctx, &orchestratorSDK.SessionStatusRequest{
		SessionID: sessionID,
	})
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.Get", err)
	}
	meta := sessionMetaFromSDK(resp)
	return &meta, nil
}

// GetHistory pages through historical events. The request struct is
// forwarded verbatim to the SDK — this matches the TypeScript Wails client
// which passes the GetSessionHistoryRequest object directly over IPC.
func (b *SessionBinding) GetHistory(req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("SessionBinding.GetHistory")
	}
	if req.SessionID == "" {
		return nil, NewGUISessionIDRequiredError("SessionBinding.GetHistory")
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().GetSessionHistory(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.GetHistory", err)
	}
	return resp, nil
}

// GetConversation pages through a session's CONVERSATION — who said what,
// which tools ran, and what each answer cost. The request struct is forwarded
// verbatim to the SDK, matching the TypeScript Wails client which passes the
// GetSessionConversationRequest object directly over IPC.
//
// It is GetHistory's coarser sibling: that one pages the raw event log for a
// client following one turn, this one returns the conversation an operator
// reads. Both come from the same durable transcript.
func (b *SessionBinding) GetConversation(req *orchestratorSDK.GetSessionConversationRequest) (*orchestratorSDK.GetSessionConversationResponse, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("SessionBinding.GetConversation")
	}
	if req.SessionID == "" {
		return nil, NewGUISessionIDRequiredError("SessionBinding.GetConversation")
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	resp, err := client.Orchestrator().GetSessionConversation(b.ctx, req)
	if err != nil {
		return nil, NewGUITransportError("SessionBinding.GetConversation", err)
	}
	return resp, nil
}

// Subscribe opens a live subscription for the given session. Any historical
// events with sequence >= fromSeq are replayed as part of the stream. Each
// event is emitted to the Wails event bus under topic
// "session-event:{sessionID}". Calling Subscribe twice for the same session
// replaces the previous subscription.
func (b *SessionBinding) Subscribe(sessionID string, fromSeq uint64) ([]orchestratorSDK.SessionEventEnvelope, error) {
	if sessionID == "" {
		return nil, NewGUISessionIDRequiredError("SessionBinding.Subscribe")
	}

	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}

	// Tear down any prior subscription for this session so we never fan
	// out duplicate events to the frontend.
	b.cancelSubscription(sessionID)

	streamCtx, cancel := context.WithCancel(b.ctx)
	events, err := client.Orchestrator().SubscribeSessionEvents(streamCtx, &orchestratorSDK.SubscribeSessionEventsRequest{
		SessionID:    sessionID,
		FromSequence: fromSeq,
		Follow:       true,
	})
	if err != nil {
		cancel()
		return nil, NewGUITransportError("SessionBinding.Subscribe", err)
	}

	sub := &sessionSubscription{cancel: cancel}
	b.subsMu.Lock()
	displaced := b.subs[sessionID]
	b.subs[sessionID] = sub
	b.subsMu.Unlock()
	// A concurrent Subscribe for the same session may have registered
	// between the teardown above and this registration; cancel it rather
	// than orphan its forwarder, so exactly one stream stays live.
	if displaced != nil {
		displaced.cancel()
	}

	topic := "session-event:" + sessionID
	go b.forwardEvents(streamCtx, sessionID, sub, topic, events)

	// Historical replay is delivered over the same stream when
	// fromSeq > 0; the SDK dispatches replay events with FromReplay=true.
	// We return an empty slice here and let the frontend react via the
	// event bus — this keeps memory bounded when the history is long.
	return []orchestratorSDK.SessionEventEnvelope{}, nil
}

// Unsubscribe stops live event pushes for a session. It is safe to call
// for a session that has no active subscription.
func (b *SessionBinding) Unsubscribe(sessionID string) error {
	if sessionID == "" {
		return NewGUISessionIDRequiredError("SessionBinding.Unsubscribe")
	}
	b.cancelSubscription(sessionID)
	return nil
}

// cancelSubscription cancels and forgets whatever subscription is
// registered for sessionID. It is the caller-initiated teardown used by
// Unsubscribe and by Subscribe's replace step. Safe to call concurrently.
func (b *SessionBinding) cancelSubscription(sessionID string) {
	b.subsMu.Lock()
	sub, ok := b.subs[sessionID]
	if ok {
		delete(b.subs, sessionID)
	}
	b.subsMu.Unlock()
	if ok {
		sub.cancel()
	}
}

// releaseSubscription is the forwarder's own teardown: it cancels sub and
// removes the registry entry only if that entry is still sub. A forwarder
// whose stream was replaced by a later Subscribe exits after the
// replacement has registered; an unguarded delete-by-key here would remove
// and cancel the live replacement stream.
func (b *SessionBinding) releaseSubscription(sessionID string, sub *sessionSubscription) {
	b.subsMu.Lock()
	if b.subs[sessionID] == sub {
		delete(b.subs, sessionID)
	}
	b.subsMu.Unlock()
	sub.cancel()
}

// forwardEvents drains the SDK event channel and republishes each event
// to the Wails frontend. When the channel closes or ctx fires, the
// subscription is removed from the registry — but only if the registry
// still holds sub, so a replaced forwarder never tears down its successor.
func (b *SessionBinding) forwardEvents(ctx context.Context, sessionID string, sub *sessionSubscription, topic string, events <-chan *orchestratorSDK.SessionEventEnvelope) {
	defer b.releaseSubscription(sessionID, sub)
	for {
		select {
		case <-ctx.Done():
			return
		case env, ok := <-events:
			if !ok {
				return
			}
			if env == nil {
				continue
			}
			wailsRuntime.EventsEmit(b.ctx, topic, env)
		}
	}
}

// sessionMetaFromSDK converts an SDK SessionStatusResponse into the
// Wails-facing SessionMeta shape.
func sessionMetaFromSDK(s *orchestratorSDK.SessionStatusResponse) SessionMeta {
	if s == nil {
		return SessionMeta{}
	}
	return SessionMeta{
		SessionID:   s.SessionID,
		Backend:     s.Backend,
		Status:      s.Status,
		ContainerID: s.ContainerID,
		CreatedAt:   s.CreatedAt,
		LastActive:  s.LastActive,
	}
}
