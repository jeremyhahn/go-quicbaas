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

package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	proto "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// roundTripBackend is a minimal AgentBackend whose Spawn stores the returned
// session id in an in-memory registry and whose Prompt / Status look it up by
// the same key — exactly the contract a real ACPBackend must honor. The
// storeKeyFn lets a test deliberately diverge the storage key from the returned
// id to reproduce the "session created at spawn is not found by the immediate
// prompt" failure.
type roundTripBackend struct {
	mu       sync.Mutex
	sessions map[string]struct{}

	// returnIDFn computes the session id Spawn returns to the caller.
	returnIDFn func(req *orchestration.SpawnRequest) string

	// storeKeyFn computes the key the session is stored under. When nil, the
	// returned id is used (the correct, bug-free behavior).
	storeKeyFn func(returnedID string) string
}

func newRoundTripBackend() *roundTripBackend {
	return &roundTripBackend{sessions: map[string]struct{}{}}
}

func (b *roundTripBackend) Spawn(_ context.Context, req *orchestration.SpawnRequest) (*orchestration.Session, error) {
	id := "ses-roundtrip-001"
	if b.returnIDFn != nil {
		id = b.returnIDFn(req)
	}
	key := id
	if b.storeKeyFn != nil {
		key = b.storeKeyFn(id)
	}
	b.mu.Lock()
	b.sessions[key] = struct{}{}
	b.mu.Unlock()
	return &orchestration.Session{
		ID:        id,
		Backend:   b.Type(),
		Status:    orchestration.SessionStateActive,
		CreatedAt: time.Now().UnixNano(),
	}, nil
}

func (b *roundTripBackend) Prompt(_ context.Context, req *orchestration.PromptRequest) (*orchestration.PromptResponse, error) {
	b.mu.Lock()
	_, ok := b.sessions[req.SessionID]
	b.mu.Unlock()
	if !ok {
		return nil, &orchestration.SessionNotFoundError{SessionID: req.SessionID}
	}
	return &orchestration.PromptResponse{
		SessionID:  req.SessionID,
		Content:    []orchestration.ContentBlock{{Type: "text", Text: "ok"}},
		StopReason: "end_turn",
	}, nil
}

func (b *roundTripBackend) Status(_ context.Context, sessionID string) (*orchestration.SessionStatus, error) {
	b.mu.Lock()
	_, ok := b.sessions[sessionID]
	b.mu.Unlock()
	if !ok {
		return nil, &orchestration.SessionNotFoundError{SessionID: sessionID}
	}
	return &orchestration.SessionStatus{SessionID: sessionID, State: orchestration.SessionStateActive}, nil
}

func (b *roundTripBackend) Close(_ context.Context, sessionID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.sessions[sessionID]; !ok {
		return &orchestration.SessionNotFoundError{SessionID: sessionID}
	}
	delete(b.sessions, sessionID)
	return nil
}

func (b *roundTripBackend) Capabilities() orchestration.BackendCapabilities {
	return orchestration.BackendCapabilities{Kind: orchestration.BackendKindAgent, PersistentSessions: true}
}

func (b *roundTripBackend) Type() string         { return "acp" }
func (b *roundTripBackend) ToolGuidance() string { return "" }

// promptOnce drives a single Prompt RPC and returns the first frame's session
// id (or the RPC error).
func promptOnce(t *testing.T, client proto.AgentRuntimeClient, sessionID string) (string, error) {
	t.Helper()
	stream, err := client.Prompt(context.Background(), &proto.PromptSessionRequest{
		Backend:   "acp",
		SessionId: sessionID,
		Prompt:    "hello",
		TenantId:  "tenant-a",
	})
	if err != nil {
		return "", err
	}
	frame, recvErr := stream.Recv()
	if recvErr != nil {
		return "", recvErr
	}
	return frame.GetSessionId(), nil
}

// TestSpawnServer_SpawnThenImmediatePromptFindsSession is the regression for the
// embedded-daemon "session created at spawn is not found by the immediate
// prompt" bug. It drives Spawn then Prompt over the same bufconn-backed daemon
// and asserts the session id round-trips: the id returned by Spawn MUST be the
// id the daemon-side backend stores under, so the immediate Prompt resolves it.
func TestSpawnServer_SpawnThenImmediatePromptFindsSession(t *testing.T) {
	backend := newRoundTripBackend()
	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	spawnResp, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		WorkDir:  "/tmp",
		TenantId: "tenant-a",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if spawnResp.GetSessionId() == "" {
		t.Fatal("Spawn returned empty session id")
	}

	// Immediate prompt using the id Spawn handed back. With a correct
	// store-key == return-id contract this resolves to the live session.
	gotID, promptErr := promptOnce(t, client, spawnResp.GetSessionId())
	if promptErr != nil {
		t.Fatalf("immediate Prompt after Spawn failed: %v", promptErr)
	}
	if gotID != spawnResp.GetSessionId() {
		t.Errorf("Prompt session id = %q, want %q", gotID, spawnResp.GetSessionId())
	}
}

// TestSpawnServer_StoreKeyMismatchReproducesNotFound proves the failure mode:
// when the daemon-side backend stores a session under a key that differs from
// the id it returns to the orchestrator, the immediate prompt (which uses the
// returned id) cannot find it and surfaces as codes.NotFound — the exact
// "acp: session not found" the live daemon logged. This guards against any
// future spawn path that returns one id but registers another.
func TestSpawnServer_StoreKeyMismatchReproducesNotFound(t *testing.T) {
	backend := newRoundTripBackend()
	// Deliberately diverge the storage key from the returned id.
	backend.storeKeyFn = func(returnedID string) string { return returnedID + "-divergent" }

	_, client, cleanup := buildSpawnServer(t, backend, newTestIdentity())
	defer cleanup()

	spawnResp, err := client.Spawn(context.Background(), &proto.SpawnSessionRequest{
		Backend:  "acp",
		WorkDir:  "/tmp",
		TenantId: "tenant-a",
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	_, promptErr := promptOnce(t, client, spawnResp.GetSessionId())
	if promptErr == nil {
		t.Fatal("expected NotFound when store key diverges from returned id")
	}
	st, ok := status.FromError(promptErr)
	if !ok {
		t.Fatalf("expected gRPC status error, got %T: %v", promptErr, promptErr)
	}
	if st.Code() != codes.NotFound {
		t.Errorf("code = %s, want NotFound", st.Code())
	}
	if !errors.Is(promptErr, status.Error(codes.NotFound, st.Message())) {
		// errors.Is on status errors compares code+message; this is a sanity
		// assertion that the surfaced error is the not-found status.
		t.Logf("not-found message: %s", st.Message())
	}
}
