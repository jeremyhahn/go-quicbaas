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

package apimodule

import (
	"context"
	"net"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// qrdbAuthenticated is the context go-qrdb's AuthMiddleware hands a handler on
// the unified server once it has authenticated a caller of tenantID: the
// identity under qrdbsdk.WithIdentity and its tenant under go-qrdb's central
// key (pkg/auth/middleware finalizeIdentity). It is built from the SDK
// re-exports alone, so it is the same two bindings, not a model of them.
func qrdbAuthenticated(tenantID string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		ctx = qrdbsdk.WithIdentity(ctx, &qrdbsdk.Identity{Subject: "alice", TenantID: tenantID})
		ctx = qrdbsdk.ContextWithTenant(ctx, tenantID)
		return handler(ctx, req)
	}
}

// startAuthenticatedModule serves the module on bufconn behind
// qrdbAuthenticated, the way the unified server serves it behind go-qrdb's
// auth chain.
func startAuthenticatedModule(t *testing.T, m *Module, tenantID string) *grpc.ClientConn {
	t.Helper()
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(qrdbAuthenticated(tenantID)))
	m.RegisterGRPC(server)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// seededScheduler is a real scheduler engine over an in-memory store holding
// one system job and one job of tenant-b, behind the real SchedulerAdapter.
func seededScheduler(t *testing.T) (svc service.SchedulerService, systemJob, tenantBJob uint64) {
	t.Helper()
	storage := schedstorage.NewStorage(kvstore.NewMemory())
	far := time.Now().Add(24 * time.Hour)
	ctx := context.Background()

	sys := &scheduler.Job{
		ID: 11, Name: "system-rotation", Type: scheduler.JobTypeOneShot, Handler: "h",
		Status: scheduler.JobStatusPending, OwnerType: scheduler.OwnerTypeSystem,
		NextRun: far, ExecuteAt: far,
	}
	tenantB := &scheduler.Job{
		ID: 12, Name: "b-report", Type: scheduler.JobTypeOneShot, Handler: "h",
		Status: scheduler.JobStatusPending, OwnerType: scheduler.OwnerTypeTenant,
		OwnerID: "tenant-b", TargetTenantID: "tenant-b", NextRun: far, ExecuteAt: far,
	}
	require.NoError(t, storage.CreateJob(ctx, sys))
	require.NoError(t, storage.CreateJob(ctx, tenantB))

	engine := scheduler.New(scheduler.DefaultConfig(), storage)
	return service.NewSchedulerAdapter(engine), sys.ID, tenantB.ID
}

// TestModule_UnifiedGRPC_TenantCallerIsConfinedToItsTenant is the go-quicbaas
// #1150 regression: go-qrdb authenticates a tenant-a caller on the unified
// gRPC server and binds its tenant, and the scheduler it reaches must scope to
// tenant-a. Before the fix the services read a go-quicbaas-private tenant key
// nothing on this path set, read the absence as "single-tenant / system", and
// then honoured whatever tenant the request named.
func TestModule_UnifiedGRPC_TenantCallerIsConfinedToItsTenant(t *testing.T) {
	sched, systemJob, tenantBJob := seededScheduler(t)
	m, err := NewModule(func() service.QuicBaaS {
		return &mocks.MockQuicBaaS{SchedulerServiceFunc: func() service.SchedulerService { return sched }}
	}, testLogger(t))
	require.NoError(t, err)
	client := pb.NewSchedulerServiceClient(startAuthenticatedModule(t, m, "tenant-a"))
	ctx := context.Background()

	t.Run("naming another tenant is refused", func(t *testing.T) {
		resp, err := client.ListJobs(ctx, &pb.ListJobsRequest{OwnerId: "tenant-b"})
		require.Error(t, err, "tenant-a listed tenant-b's jobs: %v", resp.GetJobs())
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("reading another tenant's job is refused", func(t *testing.T) {
		resp, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: tenantBJob, TenantId: "tenant-b"})
		require.Error(t, err, "tenant-a read tenant-b's job: %v", resp.GetJob())
		assert.Equal(t, codes.PermissionDenied, status.Code(err))
	})

	t.Run("an unscoped listing does not reach system jobs", func(t *testing.T) {
		resp, err := client.ListJobs(ctx, &pb.ListJobsRequest{})
		require.NoError(t, err)
		for _, job := range resp.GetJobs() {
			assert.NotEqual(t, systemJob, job.GetId(), "tenant-a saw a system job")
			assert.NotEqual(t, tenantBJob, job.GetId(), "tenant-a saw tenant-b's job")
		}
	})
}

// TestModule_UnifiedGRPC_SystemCallerNamesOnlyItself proves the system tenant
// gets no cross-tenant lift through a request field on the unified path
// either: naming tenant-b is refused, and its own listing still serves.
func TestModule_UnifiedGRPC_SystemCallerNamesOnlyItself(t *testing.T) {
	sched, systemJob, tenantBJob := seededScheduler(t)
	m, err := NewModule(func() service.QuicBaaS {
		return &mocks.MockQuicBaaS{SchedulerServiceFunc: func() service.SchedulerService { return sched }}
	}, testLogger(t))
	require.NoError(t, err)
	client := pb.NewSchedulerServiceClient(startAuthenticatedModule(t, m, qrdbsdk.SystemTenantID))
	ctx := context.Background()

	_, err = client.GetJob(ctx, &pb.GetJobRequest{JobId: tenantBJob, TenantId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	resp, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: systemJob})
	require.NoError(t, err)
	assert.Equal(t, systemJob, resp.GetJob().GetId())
}
