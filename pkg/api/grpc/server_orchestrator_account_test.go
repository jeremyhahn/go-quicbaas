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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// spawnCapture wires a fake agent service that records the SpawnSessionRequest
// the handler built, so a dropped field is visible.
func spawnCapture() (*grpcTestAgentService, func() *orchestration.SpawnSessionRequest) {
	var seen *orchestration.SpawnSessionRequest
	svc := &grpcTestAgentService{
		spawnSessionFunc: func(ctx context.Context, req *orchestration.SpawnSessionRequest) (*orchestration.SpawnSessionResponse, error) {
			seen = req
			return &orchestration.SpawnSessionResponse{SessionID: "session-abc", Backend: req.Backend}, nil
		},
	}
	return svc, func() *orchestration.SpawnSessionRequest { return seen }
}

// TestOrchestratorGRPC_Spawn_PinsAccountID asserts an explicit account_id
// reaches the service layer. Dropping it would silently spawn against the
// daemon host's shared identity instead of the pinned pooled account.
func TestOrchestratorGRPC_Spawn_PinsAccountID(t *testing.T) {
	agentSvc, captured := spawnCapture()
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	_, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend:   "claude",
		AccountId: "acct-pinned",
	})
	require.NoError(t, err)

	seen := captured()
	require.NotNil(t, seen)
	assert.Equal(t, "acct-pinned", seen.AccountID, "gRPC spawn dropped account_id")
	assert.False(t, seen.AccountAuto, "gRPC spawn invented account_auto")
}

// TestOrchestratorGRPC_Spawn_CarriesAccountAuto asserts account_auto and the
// named strategy reach the service layer. Without account_auto the dynamic
// least-usage strategy is unreachable from outside the process.
func TestOrchestratorGRPC_Spawn_CarriesAccountAuto(t *testing.T) {
	agentSvc, captured := spawnCapture()
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	_, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend:         "claude",
		AccountAuto:     true,
		AccountStrategy: "least_usage",
	})
	require.NoError(t, err)

	seen := captured()
	require.NotNil(t, seen)
	assert.True(t, seen.AccountAuto, "gRPC spawn dropped account_auto")
	assert.Equal(t, "least_usage", seen.AccountStrategy, "gRPC spawn dropped account_strategy")
	assert.Empty(t, seen.AccountID, "gRPC spawn invented an account_id")
}

// TestOrchestratorGRPC_Spawn_NoAccountFieldsStaysUnset is the negative case:
// a spawn that names no account must leave every account field zero so the
// pre-account behavior (backend authenticates however its CLI already does)
// is preserved exactly.
func TestOrchestratorGRPC_Spawn_NoAccountFieldsStaysUnset(t *testing.T) {
	agentSvc, captured := spawnCapture()
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	_, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{Backend: "claude"})
	require.NoError(t, err)

	seen := captured()
	require.NotNil(t, seen)
	assert.Empty(t, seen.AccountID)
	assert.False(t, seen.AccountAuto)
	assert.Empty(t, seen.AccountStrategy)
}

// TestOrchestratorGRPC_Spawn_RefusesToForwardExtraArgs is a SECURITY boundary,
// and the only thing holding it is the absence of one field mapping.
//
// `extra_args` exists on the shared protobuf so the SDLC module can state its
// worker's permission grant to the daemon that runs the process
// (go-quicbaas#218). This endpoint is the surface an EXTERNAL client reaches,
// and argv forwarded from there would let any caller add any flag the agent CLI
// takes — `--dangerously-skip-permissions` above all, which grants the Bash tool
// the operator's ExecutionGuard allowlist exists to govern.
//
// The omission is one absent line in an otherwise mechanical field-by-field
// mapping, which is exactly the kind of thing a later reader "completes" without
// knowing what it was holding back. This test is what makes completing it fail.
func TestOrchestratorGRPC_Spawn_RefusesToForwardExtraArgs(t *testing.T) {
	agentSvc, captured := spawnCapture()
	server := NewOrchestratorServer(agentSvc, createOrchestratorGRPCTestLogger())

	_, err := server.SpawnSession(context.Background(), &pb.SpawnSessionRequest{
		Backend: "claude",
		ExtraArgs: []string{
			"--dangerously-skip-permissions",
			"--permission-mode", "bypassPermissions",
			"--add-dir", "/",
		},
	})
	require.NoError(t, err)

	seen := captured()
	require.NotNil(t, seen)
	assert.Empty(t, seen.ExtraArgs,
		"the public gRPC endpoint forwarded caller-supplied agent argv; an external client can now choose the agent's permissions")
}
