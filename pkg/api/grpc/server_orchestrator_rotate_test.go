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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// RotateSessionAccount — operator-initiated pooled-account rotation
// =============================================================================

// grpcRotatingAgentService is a grpcTestAgentService that also implements the
// optional rotation verb. The bare grpcTestAgentService deliberately does NOT,
// which is what makes the "unsupported deployment" branch reachable.
type grpcRotatingAgentService struct {
	*grpcTestAgentService

	rotateFunc func(ctx context.Context, req *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error)

	lastRequest *orchestration.RotateSessionAccountRequest
	lastTenant  string
}

func (m *grpcRotatingAgentService) RotateSessionAccount(
	ctx context.Context,
	req *orchestration.RotateSessionAccountRequest,
) (*orchestration.RotateSessionAccountResult, error) {
	m.lastRequest = req
	m.lastTenant = orchestration.TenantFromContext(ctx)
	if m.rotateFunc != nil {
		return m.rotateFunc(ctx, req)
	}
	return &orchestration.RotateSessionAccountResult{
		SessionID:         req.SessionID,
		PreviousAccountID: "acct-a",
		AccountID:         "acct-b",
		Strategy:          "least_usage",
		Mode:              orchestration.AccountRotationModeResume,
	}, nil
}

var _ orchestration.SessionAccountRotationService = (*grpcRotatingAgentService)(nil)

func newRotatingOrchestratorServer() (*OrchestratorServer, *grpcRotatingAgentService) {
	svc := &grpcRotatingAgentService{grpcTestAgentService: &grpcTestAgentService{}}
	return NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger()), svc
}

func TestOrchestratorServer_RotateSessionAccount_MovesTheSession(t *testing.T) {
	server, svc := newRotatingOrchestratorServer()

	availableAt := time.Now().UTC().Add(30 * time.Minute).Truncate(time.Second)
	resp, err := server.RotateSessionAccount(context.Background(), &pb.RotateSessionAccountRequest{
		SessionId:       "session-abc",
		TenantId:        "tenant-a",
		Reason:          "seat retired",
		HoldOutgoing:    true,
		AvailableAtUnix: availableAt.Unix(),
	})
	require.NoError(t, err)

	assert.Equal(t, "session-abc", resp.GetSessionId())
	assert.Equal(t, "acct-a", resp.GetPreviousAccountId())
	assert.Equal(t, "acct-b", resp.GetAccountId())
	assert.Equal(t, "least_usage", resp.GetStrategy())
	assert.Equal(t, "resume", resp.GetMode(), "the mode must reach the operator; it is the difference between a free swap and a replayed turn")
	assert.NotNil(t, resp.GetTimestamp())

	require.NotNil(t, svc.lastRequest)
	assert.Equal(t, "seat retired", svc.lastRequest.Reason)
	assert.True(t, svc.lastRequest.HoldOutgoing)
	assert.True(t, availableAt.Equal(svc.lastRequest.AvailableAt),
		"available_at_unix must survive the wire; got %s want %s", svc.lastRequest.AvailableAt, availableAt)
	assert.Equal(t, "tenant-a", svc.lastTenant, "the wire tenant must be stamped on the context the service reads")
}

// TestOrchestratorServer_RotateSessionAccount_UnsetAvailableAtStaysZero keeps a
// caller that asked for the server's default backoff from being handed a hold
// window of 1970.
func TestOrchestratorServer_RotateSessionAccount_UnsetAvailableAtStaysZero(t *testing.T) {
	server, svc := newRotatingOrchestratorServer()

	_, err := server.RotateSessionAccount(context.Background(), &pb.RotateSessionAccountRequest{
		SessionId:    "session-abc",
		TenantId:     "tenant-a",
		HoldOutgoing: true,
	})
	require.NoError(t, err)
	require.NotNil(t, svc.lastRequest)
	assert.True(t, svc.lastRequest.AvailableAt.IsZero(),
		"an omitted available_at_unix must arrive as the zero time so the service applies its default backoff")
}

func TestOrchestratorServer_RotateSessionAccount_RequiresSessionID(t *testing.T) {
	server, svc := newRotatingOrchestratorServer()

	_, err := server.RotateSessionAccount(context.Background(), &pb.RotateSessionAccountRequest{
		TenantId: "tenant-a",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Nil(t, svc.lastRequest, "the service must not be reached with an empty session id")
}

func TestOrchestratorServer_RotateSessionAccount_PinnedSessionIsAConflict(t *testing.T) {
	server, svc := newRotatingOrchestratorServer()
	svc.rotateFunc = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
		return nil, &orchestration.SessionNotRotatableError{
			SessionID: "session-abc",
			Reason:    "the session is pinned to a named account",
		}
	}

	_, err := server.RotateSessionAccount(context.Background(), &pb.RotateSessionAccountRequest{
		SessionId: "session-abc",
		TenantId:  "tenant-a",
	})
	require.Error(t, err)
	assert.Equal(t, codes.FailedPrecondition, status.Code(err),
		"a deliberate pin is a state problem, not a malformed request")
}

func TestOrchestratorServer_RotateSessionAccount_UnknownSessionIsNotFound(t *testing.T) {
	server, svc := newRotatingOrchestratorServer()
	svc.rotateFunc = func(context.Context, *orchestration.RotateSessionAccountRequest) (*orchestration.RotateSessionAccountResult, error) {
		return nil, &orchestration.SessionNotFoundError{SessionID: "missing"}
	}

	_, err := server.RotateSessionAccount(context.Background(), &pb.RotateSessionAccountRequest{
		SessionId: "missing",
		TenantId:  "tenant-a",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

// TestOrchestratorServer_RotateSessionAccount_UnsupportedDeployment covers the
// wrong-type branch: an agent service that predates the pooled account
// subsystem answers Unimplemented rather than panicking on a type assertion.
func TestOrchestratorServer_RotateSessionAccount_UnsupportedDeployment(t *testing.T) {
	server := NewOrchestratorServer(&grpcTestAgentService{}, createOrchestratorGRPCTestLogger())

	_, err := server.RotateSessionAccount(context.Background(), &pb.RotateSessionAccountRequest{
		SessionId: "session-abc",
		TenantId:  "tenant-a",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

// =============================================================================
// account_allow on the spawn shape
// =============================================================================

// TestOrchestratorServer_SpawnSession_CarriesAccountAllow is the gRPC half of
// gap 2: without it, a caller could ask for an enumerated rotation pool and the
// server would silently spawn a session that can never rotate.
func TestOrchestratorServer_SpawnSession_CarriesAccountAllow(t *testing.T) {
	var captured *orchestration.SpawnSessionRequest
	svc := &grpcTestAgentService{
		spawnSessionFunc: func(_ context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			captured = req
			return &orchestration.SpawnSessionResponse{SessionID: "session-abc", Backend: req.Backend}, nil
		},
	}
	server := NewOrchestratorServer(svc, createOrchestratorGRPCTestLogger())

	_, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend:      "claude",
		TenantId:     "tenant-a",
		AccountId:    "acct-a",
		AccountAllow: []string{"acct-a", "acct-b"},
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, []string{"acct-a", "acct-b"}, captured.AccountAllow)
	assert.Equal(t, "acct-a", captured.AccountID)
}
