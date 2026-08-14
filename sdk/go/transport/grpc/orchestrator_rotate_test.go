// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// RotateSessionAccount completes fakeOrchestratorServer for the rotation verb,
// so the shared orchCases table can exercise both its success and error paths.
func (f *fakeOrchestratorServer) RotateSessionAccount(
	_ context.Context,
	req *pb.RotateSessionAccountRequest,
) (*pb.RotateSessionAccountResponse, error) {
	if err := f.e(); err != nil {
		return nil, err
	}
	return &pb.RotateSessionAccountResponse{
		SessionId:         req.GetSessionId(),
		PreviousAccountId: "acct-a",
		AccountId:         "acct-b",
		Strategy:          "least_usage",
		Mode:              "resume",
		Timestamp:         orchTime(),
	}, nil
}

// recordingRotateServer captures the proto request the SDK built so the field
// mapping can be asserted rather than inferred.
type recordingRotateServer struct {
	pb.UnimplementedOrchestratorServiceServer

	mu        sync.Mutex
	last      *pb.RotateSessionAccountRequest
	lastSpawn *pb.SpawnSessionRequest
}

func (s *recordingRotateServer) RotateSessionAccount(
	_ context.Context,
	req *pb.RotateSessionAccountRequest,
) (*pb.RotateSessionAccountResponse, error) {
	s.mu.Lock()
	s.last = req
	s.mu.Unlock()
	return &pb.RotateSessionAccountResponse{
		SessionId:         req.GetSessionId(),
		PreviousAccountId: "acct-a",
		AccountId:         "acct-b",
		Strategy:          "least_usage",
		Mode:              "in_place",
	}, nil
}

func (s *recordingRotateServer) request() *pb.RotateSessionAccountRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

// SpawnSession lets the same recording server double as the spawn-shape probe.
func (s *recordingRotateServer) SpawnSession(
	_ context.Context,
	req *pb.SpawnSessionRequest,
) (*pb.SpawnSessionResponse, error) {
	s.mu.Lock()
	s.lastSpawn = req
	s.mu.Unlock()
	return &pb.SpawnSessionResponse{SessionId: "s1", Backend: req.GetBackend()}, nil
}

func TestOrchestratorRotateSessionAccount_MapsEveryField(t *testing.T) {
	fake := &recordingRotateServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))

	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	availableAt := time.Now().UTC().Add(35 * time.Minute).Truncate(time.Second)

	resp, err := h.handleRequest(ctx, "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestrator.RotateSessionAccountRequest{
			SessionID:    "s1",
			Reason:       "seat retired",
			HoldOutgoing: true,
			AvailableAt:  availableAt,
		})
	require.NoError(t, err)

	got := fake.request()
	require.NotNil(t, got)
	assert.Equal(t, "s1", got.GetSessionId())
	assert.Equal(t, "tenant-a", got.GetTenantId(), "the context tenant must cross the wire")
	assert.Equal(t, "seat retired", got.GetReason())
	assert.True(t, got.GetHoldOutgoing())
	assert.Equal(t, availableAt.Unix(), got.GetAvailableAtUnix())

	result, ok := resp.(*orchestrator.RotateSessionAccountResponse)
	require.Truef(t, ok, "response is %T", resp)
	assert.Equal(t, "acct-a", result.PreviousAccountID)
	assert.Equal(t, "acct-b", result.AccountID)
	assert.Equal(t, "least_usage", result.Strategy)
	assert.Equal(t, "in_place", result.Mode)
}

// TestOrchestratorRotateSessionAccount_UnsetAvailableAtStaysZero keeps a caller
// that wants the server's default backoff from sending a 1970 hold window.
func TestOrchestratorRotateSessionAccount_UnsetAvailableAtStaysZero(t *testing.T) {
	fake := &recordingRotateServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))

	_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "RotateSessionAccount",
		&orchestrator.RotateSessionAccountRequest{SessionID: "s1", HoldOutgoing: true})
	require.NoError(t, err)

	got := fake.request()
	require.NotNil(t, got)
	assert.Zero(t, got.GetAvailableAtUnix())
}

func TestOrchestratorRotateSessionAccount_WrongRequestType(t *testing.T) {
	h := newOrchestratorHandlerWithServer(t, false)

	_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator",
		"RotateSessionAccount", &orchestrator.CloseSessionRequest{SessionID: "s1"})
	require.Error(t, err)
}

// TestOrchestratorSpawnSession_CarriesAccountAllow is the gRPC-SDK half of gap
// 2: the repeated field has to be populated on the proto request or the
// enumerated rotation pool never reaches the server.
func TestOrchestratorSpawnSession_CarriesAccountAllow(t *testing.T) {
	fake := &recordingRotateServer{}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterOrchestratorServiceServer(s, fake)
	})
	h := newOrchestratorHandler(srv.dial(t))

	_, err := h.handleRequest(context.Background(), "quicbaas.Orchestrator", "SpawnSession",
		&orchestrator.SpawnSessionRequest{
			Backend:      "claude",
			AccountID:    "acct-a",
			AccountAllow: []string{"acct-a", "acct-b"},
		})
	require.NoError(t, err)

	fake.mu.Lock()
	spawn := fake.lastSpawn
	fake.mu.Unlock()
	require.NotNil(t, spawn)
	assert.Equal(t, []string{"acct-a", "acct-b"}, spawn.GetAccountAllow())
	assert.Equal(t, "acct-a", spawn.GetAccountId())
}
