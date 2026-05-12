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
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// sessionBindingClient is a quicbaas.Client adapter that wires the
// package-level mockClient to a caller-supplied mockOrchestratorClient.
// It lets the binding tests exercise real method dispatch (conn.loadClient
// -> client.Orchestrator()) without spinning up a network transport.
type sessionBindingClient struct {
	mockClient
	orch *mockOrchestratorClient
}

func (c *sessionBindingClient) Orchestrator() quicbaas.OrchestratorClient {
	return c.orch
}

// newTestSessionBinding constructs a SessionBinding whose ConnectionBinding
// holds a fake client returning the provided mockOrchestratorClient.
// The test context is stored as the binding's Wails context.
func newTestSessionBinding(t *testing.T, mock *mockOrchestratorClient) (*SessionBinding, context.CancelFunc) {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)

	conn := NewConnectionBinding(log)

	// Store the fake quicbaas.Client directly via the atomic pointer.
	var client quicbaas.Client = &sessionBindingClient{orch: mock}
	conn.client.Store(&client)

	binding := NewSessionBinding(log, conn)

	ctx, cancel := context.WithCancel(context.Background())
	binding.ctx = ctx

	t.Cleanup(func() {
		cancel()
		clearSessionOverrides(mock)
	})

	return binding, cancel
}

// -----------------------------------------------------------------------------
// List
// -----------------------------------------------------------------------------

func TestSessionBinding_List_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listSessionsFn: func(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
			assert.Equal(t, "claude", req.Backend)
			return &orchestratorSDK.ListSessionsResponse{
				Sessions: []*orchestratorSDK.SessionStatusResponse{
					{SessionID: "s1", Backend: "claude", Status: "active", CreatedAt: 100, LastActive: 200},
					{SessionID: "s2", Backend: "claude", Status: "idle", CreatedAt: 300, LastActive: 400},
				},
				Count: 2,
			}, nil
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	sessions, err := binding.List(map[string]interface{}{"backend": "claude"})
	require.NoError(t, err)
	require.Len(t, sessions, 2)
	assert.Equal(t, "s1", sessions[0].SessionID)
	assert.Equal(t, "active", sessions[0].Status)
	assert.Equal(t, int64(200), sessions[0].LastActive)
	assert.Equal(t, "s2", sessions[1].SessionID)
}

func TestSessionBinding_List_TransportError(t *testing.T) {
	sentinel := errors.New("boom")
	mock := &mockOrchestratorClient{
		listSessionsFn: func(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
			return nil, sentinel
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	sessions, err := binding.List(nil)
	require.Error(t, err)
	assert.Nil(t, sessions)

	var qberr *qberrors.QBError
	require.ErrorAs(t, err, &qberr)
	assert.Equal(t, "SessionBinding.List", qberr.Op)
	assert.ErrorIs(t, err, sentinel)
}

func TestSessionBinding_List_NotConnected(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	conn := NewConnectionBinding(log)
	binding := NewSessionBinding(log, conn)
	binding.ctx = context.Background()

	sessions, err := binding.List(nil)
	require.Error(t, err)
	assert.Nil(t, sessions)

	var notConn *GUINotConnectedError
	assert.ErrorAs(t, err, &notConn)
}

// -----------------------------------------------------------------------------
// Get
// -----------------------------------------------------------------------------

func TestSessionBinding_Get_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		sessionStatusFn: func(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
			assert.Equal(t, "sess-42", req.SessionID)
			return &orchestratorSDK.SessionStatusResponse{
				SessionID:   "sess-42",
				Backend:     "codex",
				Status:      "active",
				ContainerID: "ctr-1",
				CreatedAt:   1,
				LastActive:  2,
			}, nil
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	meta, err := binding.Get("sess-42")
	require.NoError(t, err)
	require.NotNil(t, meta)
	assert.Equal(t, "sess-42", meta.SessionID)
	assert.Equal(t, "codex", meta.Backend)
	assert.Equal(t, "ctr-1", meta.ContainerID)
}

func TestSessionBinding_Get_EmptyID(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	meta, err := binding.Get("")
	require.Error(t, err)
	assert.Nil(t, meta)

	var typed *GUISessionIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_Get_TransportError(t *testing.T) {
	sentinel := errors.New("not found")
	mock := &mockOrchestratorClient{
		sessionStatusFn: func(ctx context.Context, req *orchestratorSDK.SessionStatusRequest) (*orchestratorSDK.SessionStatusResponse, error) {
			return nil, sentinel
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	meta, err := binding.Get("sess-x")
	require.Error(t, err)
	assert.Nil(t, meta)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// Spawn / Prompt / Close
// -----------------------------------------------------------------------------

func TestSessionBinding_Spawn_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		spawnSessionFn: func(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			assert.Equal(t, "claude", req.Backend)
			assert.Equal(t, "sonnet", req.Model)
			assert.Equal(t, "node-42", req.HostAffinity)
			return &orchestratorSDK.SpawnSessionResponse{
				SessionID:   "spawn-1",
				Backend:     "claude",
				ContainerID: "ctr-abc",
			}, nil
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Spawn(&orchestratorSDK.SpawnSessionRequest{
		Backend:      "claude",
		Model:        "sonnet",
		HostAffinity: "node-42",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "spawn-1", resp.SessionID)
	assert.Equal(t, "ctr-abc", resp.ContainerID)
}

// TestSessionBinding_Spawn_NilRequest simulates the Wails dispatcher delivering
// a malformed payload (e.g. a bare string where an object was expected). When
// Wails fails to unmarshal the JSON argument into *SpawnSessionRequest the
// pointer arrives nil; the binding must reject this cleanly with a typed
// GUIRequestRequiredError rather than dereferencing nil.
func TestSessionBinding_Spawn_NilRequest(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	resp, err := binding.Spawn(nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_Spawn_TransportError(t *testing.T) {
	sentinel := errors.New("spawn failed")
	mock := &mockOrchestratorClient{
		spawnSessionFn: func(ctx context.Context, req *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			return nil, sentinel
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Spawn(&orchestratorSDK.SpawnSessionRequest{Backend: "claude"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, sentinel)
}

// TestSessionBinding_Spawn_WailsPayloadShape asserts that a JSON payload
// identical to what the Svelte frontend emits via Wails unmarshals into
// *SpawnSessionRequest and is forwarded to the SDK verbatim. This is the
// regression test for the object-vs-string dispatcher bug.
func TestSessionBinding_Spawn_WailsPayloadShape(t *testing.T) {
	// This is the exact shape wails.ts's spawnSession(req) serialises.
	payload := []byte(`{"backend":"claude","model":"claude-opus-4-5","sandbox_mode":1,"host_affinity":"node-42"}`)

	var req orchestratorSDK.SpawnSessionRequest
	require.NoError(t, json.Unmarshal(payload, &req))

	mock := &mockOrchestratorClient{
		spawnSessionFn: func(ctx context.Context, got *orchestratorSDK.SpawnSessionRequest) (*orchestratorSDK.SpawnSessionResponse, error) {
			assert.Equal(t, "claude", got.Backend)
			assert.Equal(t, "claude-opus-4-5", got.Model)
			assert.Equal(t, orchestratorSDK.SandboxModeRequired, got.SandboxMode)
			assert.Equal(t, "node-42", got.HostAffinity)
			return &orchestratorSDK.SpawnSessionResponse{SessionID: "ok"}, nil
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Spawn(&req)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.SessionID)
}

func TestSessionBinding_Prompt_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		promptSessionFn: func(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
			assert.Equal(t, "sess-1", req.SessionID)
			assert.Equal(t, "hello", req.Prompt)
			return &orchestratorSDK.PromptSessionResponse{SessionID: req.SessionID, Response: "hi", Done: true}, nil
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Prompt(&orchestratorSDK.PromptSessionRequest{
		SessionID: "sess-1",
		Prompt:    "hello",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "hi", resp.Response)
	assert.True(t, resp.Done)
}

func TestSessionBinding_Prompt_NilRequest(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	resp, err := binding.Prompt(nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_Prompt_TransportError(t *testing.T) {
	sentinel := errors.New("prompt error")
	mock := &mockOrchestratorClient{
		promptSessionFn: func(ctx context.Context, req *orchestratorSDK.PromptSessionRequest) (*orchestratorSDK.PromptSessionResponse, error) {
			return nil, sentinel
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Prompt(&orchestratorSDK.PromptSessionRequest{SessionID: "s", Prompt: "p"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, sentinel)
}

func TestSessionBinding_Close_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		closeSessionFn: func(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
			assert.Equal(t, "sess-close", req.SessionID)
			return &orchestratorSDK.CloseSessionResponse{SessionID: req.SessionID, Closed: true}, nil
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Close(&orchestratorSDK.CloseSessionRequest{SessionID: "sess-close"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Closed)
}

func TestSessionBinding_Close_NilRequest(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	resp, err := binding.Close(nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_Close_TransportError(t *testing.T) {
	sentinel := errors.New("close error")
	mock := &mockOrchestratorClient{
		closeSessionFn: func(ctx context.Context, req *orchestratorSDK.CloseSessionRequest) (*orchestratorSDK.CloseSessionResponse, error) {
			return nil, sentinel
		},
	}
	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.Close(&orchestratorSDK.CloseSessionRequest{SessionID: "s"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// GetHistory
// -----------------------------------------------------------------------------

func TestSessionBinding_GetHistory_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{}
	setSessionHistoryFn(mock, func(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error) {
		assert.Equal(t, "sess-1", req.SessionID)
		assert.Equal(t, uint64(5), req.FromSequence)
		assert.Equal(t, 10, req.Limit)
		return &orchestratorSDK.GetSessionHistoryResponse{
			Events: []*orchestratorSDK.SessionEventEnvelope{
				{SessionID: "sess-1", Sequence: 5, Timestamp: time.Unix(100, 0)},
				{SessionID: "sess-1", Sequence: 6, Timestamp: time.Unix(200, 0)},
			},
			NextSeq: 7,
		}, nil
	})

	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.GetHistory(&orchestratorSDK.GetSessionHistoryRequest{
		SessionID:    "sess-1",
		FromSequence: 5,
		Limit:        10,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, uint64(7), resp.NextSeq)
	require.Len(t, resp.Events, 2)
	assert.Equal(t, uint64(5), resp.Events[0].Sequence)
	assert.Equal(t, uint64(6), resp.Events[1].Sequence)
}

func TestSessionBinding_GetHistory_NilRequest(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	resp, err := binding.GetHistory(nil)
	require.Error(t, err)
	assert.Nil(t, resp)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_GetHistory_EmptyID(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	resp, err := binding.GetHistory(&orchestratorSDK.GetSessionHistoryRequest{SessionID: ""})
	require.Error(t, err)
	assert.Nil(t, resp)

	var typed *GUISessionIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_GetHistory_TransportError(t *testing.T) {
	sentinel := errors.New("history io fail")
	mock := &mockOrchestratorClient{}
	setSessionHistoryFn(mock, func(ctx context.Context, req *orchestratorSDK.GetSessionHistoryRequest) (*orchestratorSDK.GetSessionHistoryResponse, error) {
		return nil, sentinel
	})

	binding, _ := newTestSessionBinding(t, mock)

	resp, err := binding.GetHistory(&orchestratorSDK.GetSessionHistoryRequest{SessionID: "sess-1"})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// Subscribe / Unsubscribe
// -----------------------------------------------------------------------------

func TestSessionBinding_Subscribe_RegistersCancel(t *testing.T) {
	mock := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(mock, func(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope)
		// Close on ctx.Done so the forwarder exits cleanly.
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	})
	binding, _ := newTestSessionBinding(t, mock)

	initial, err := binding.Subscribe("sess-abc", 0)
	require.NoError(t, err)
	assert.Empty(t, initial, "subscribe returns no synchronous history")

	// Cancel registration should now be present.
	binding.subsMu.Lock()
	_, ok := binding.subs["sess-abc"]
	binding.subsMu.Unlock()
	assert.True(t, ok)

	require.NoError(t, binding.Unsubscribe("sess-abc"))

	// Subsequent unsubscribe on a removed key is a no-op.
	require.NoError(t, binding.Unsubscribe("sess-abc"))

	// Give the forwarder goroutine a moment to exit. We use a short
	// poll loop bounded by a hard deadline rather than time.Sleep so
	// the test does not introduce wall-clock delay when the forwarder
	// exits promptly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		binding.subsMu.Lock()
		empty := len(binding.subs) == 0
		binding.subsMu.Unlock()
		if empty {
			return
		}
	}
	t.Fatalf("subscription registry did not drain")
}

func TestSessionBinding_Subscribe_EmptyID(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})

	out, err := binding.Subscribe("", 0)
	require.Error(t, err)
	assert.Nil(t, out)

	var typed *GUISessionIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestSessionBinding_Subscribe_TransportError(t *testing.T) {
	sentinel := errors.New("stream broken")
	mock := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(mock, func(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		return nil, sentinel
	})
	binding, _ := newTestSessionBinding(t, mock)

	out, err := binding.Subscribe("sess-1", 0)
	require.Error(t, err)
	assert.Nil(t, out)
	assert.ErrorIs(t, err, sentinel)

	binding.subsMu.Lock()
	_, exists := binding.subs["sess-1"]
	binding.subsMu.Unlock()
	assert.False(t, exists, "failed subscription must not leak a cancel fn")
}

func TestSessionBinding_Subscribe_ReplacesPreviousSubscription(t *testing.T) {
	// Track how many streams have been opened; each call installs a new
	// cancel in the registry.
	var opened int
	mock := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(mock, func(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		opened++
		ch := make(chan *orchestratorSDK.SessionEventEnvelope)
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	})
	binding, _ := newTestSessionBinding(t, mock)

	_, err := binding.Subscribe("sess-dup", 0)
	require.NoError(t, err)

	_, err = binding.Subscribe("sess-dup", 0)
	require.NoError(t, err)

	assert.Equal(t, 2, opened)

	binding.subsMu.Lock()
	size := len(binding.subs)
	binding.subsMu.Unlock()
	assert.Equal(t, 1, size, "only one live cancel per sessionID")

	require.NoError(t, binding.Unsubscribe("sess-dup"))
}

func TestSessionBinding_Unsubscribe_EmptyID(t *testing.T) {
	binding, _ := newTestSessionBinding(t, &mockOrchestratorClient{})
	err := binding.Unsubscribe("")
	require.Error(t, err)

	var typed *GUISessionIDRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

func TestSessionBinding_Shutdown_CancelsAllSubscriptions(t *testing.T) {
	mock := &mockOrchestratorClient{}
	setSubscribeSessionEventsFn(mock, func(ctx context.Context, req *orchestratorSDK.SubscribeSessionEventsRequest) (<-chan *orchestratorSDK.SessionEventEnvelope, error) {
		ch := make(chan *orchestratorSDK.SessionEventEnvelope)
		go func() {
			<-ctx.Done()
			close(ch)
		}()
		return ch, nil
	})

	binding, _ := newTestSessionBinding(t, mock)

	for _, id := range []string{"a", "b", "c"} {
		_, err := binding.Subscribe(id, 0)
		require.NoError(t, err)
	}

	binding.subsMu.Lock()
	before := len(binding.subs)
	binding.subsMu.Unlock()
	require.Equal(t, 3, before)

	binding.shutdown(context.Background())

	binding.subsMu.Lock()
	after := len(binding.subs)
	binding.subsMu.Unlock()
	assert.Equal(t, 0, after)
}

func TestSessionBinding_Startup_StoresContext(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	conn := NewConnectionBinding(log)
	binding := NewSessionBinding(log, conn)

	ctx := context.Background()
	binding.startup(ctx)
	assert.Equal(t, ctx, binding.ctx)
}

// -----------------------------------------------------------------------------
// Helpers
// -----------------------------------------------------------------------------

func TestSessionMetaFromSDK_NilInput(t *testing.T) {
	meta := sessionMetaFromSDK(nil)
	assert.Equal(t, SessionMeta{}, meta)
}

func TestSessionMetaFromSDK_ProjectsAllFields(t *testing.T) {
	meta := sessionMetaFromSDK(&orchestratorSDK.SessionStatusResponse{
		SessionID:   "sid",
		Backend:     "b",
		Status:      "active",
		ContainerID: "ctr",
		CreatedAt:   10,
		LastActive:  20,
	})
	assert.Equal(t, "sid", meta.SessionID)
	assert.Equal(t, "b", meta.Backend)
	assert.Equal(t, "active", meta.Status)
	assert.Equal(t, "ctr", meta.ContainerID)
	assert.Equal(t, int64(10), meta.CreatedAt)
	assert.Equal(t, int64(20), meta.LastActive)
}
