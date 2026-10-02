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
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// startOrchestratorListener serves the real in-memory orchestrator behind the
// legacy listener's authorization interceptor for a caller of callerTenant.
func startOrchestratorListener(t *testing.T, callerTenant string) pb.OrchestratorServiceClient {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	agentSvc, err := orchestration.NewAgentService(
		orchestration.NewInMemoryAgentStore(), map[string]orchestration.AgentBackend{}, logger.NewAdapter(log))
	require.NoError(t, err)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(
		UnaryAuthorizationInterceptor(&tenantTokenAuthorizer{token: "good", tenantID: callerTenant})))
	pb.RegisterOrchestratorServiceServer(server, NewOrchestratorServer(agentSvc, log))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewOrchestratorServiceClient(conn)
}

// TestOrchestratorGRPC_TenantIDNamingAnotherTenantIsRefused is a
// go-quicbaas#1150 regression: the orchestrator's gRPC methods acted for
// whatever tenant_id the request named, unchecked against the caller.
func TestOrchestratorGRPC_TenantIDNamingAnotherTenantIsRefused(t *testing.T) {
	client := startOrchestratorListener(t, "tenant-a")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	_, err := client.ListSessions(ctx, &pb.ListSessionsRequest{TenantId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = client.ListTasks(ctx, &pb.ListTasksRequest{TenantId: "system"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = client.ListSessions(ctx, &pb.ListSessionsRequest{TenantId: "tenant-a"})
	require.NoError(t, err, "a caller may always name its own tenant")
}

// TestOrchestratorGRPC_SystemCallerNamesOnlyItself proves the system tenant
// gets no cross-tenant lift through tenant_id, and still acts for itself.
func TestOrchestratorGRPC_SystemCallerNamesOnlyItself(t *testing.T) {
	client := startOrchestratorListener(t, "system")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	_, err := client.ListSessions(ctx, &pb.ListSessionsRequest{TenantId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = client.ListSessions(ctx, &pb.ListSessionsRequest{})
	require.NoError(t, err)
}
