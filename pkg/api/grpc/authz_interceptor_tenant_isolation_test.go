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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	schedstorage "github.com/jeremyhahn/go-quicbaas/pkg/scheduler/storage/quicbaas"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// tenantTokenAuthorizer admits one bearer token for every permission and
// reports the tenant it was issued to, so the test exercises the tenant the
// interceptor binds rather than the permission map.
type tenantTokenAuthorizer struct {
	token    string
	tenantID string
}

func (a *tenantTokenAuthorizer) Authorize(_ context.Context, token, _, _, _, _ string) (*platformtoken.AuthorizedContext, error) {
	if token != a.token {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature did not verify"}
	}
	return &platformtoken.AuthorizedContext{Subject: "u1", Username: "alice", TenantID: a.tenantID}, nil
}

// startLegacySchedulerListener serves the real scheduler service behind the
// legacy gRPC listener's authorization interceptor — the chain the TCP gRPC
// and Unix-socket listeners share — and returns a client plus the ids of a
// tenant-a job and a tenant-b job.
func startLegacySchedulerListener(t *testing.T, callerTenant string) (pb.SchedulerServiceClient, uint64, uint64) {
	t.Helper()

	storage := schedstorage.NewStorage(kvstore.NewMemory())
	far := time.Now().Add(24 * time.Hour)
	jobFor := func(id uint64, tenantID string) *scheduler.Job {
		return &scheduler.Job{
			ID: id, Name: tenantID + "-report", Type: scheduler.JobTypeOneShot, Handler: "h",
			Status: scheduler.JobStatusPending, OwnerType: scheduler.OwnerTypeTenant,
			OwnerID: tenantID, TargetTenantID: tenantID, NextRun: far, ExecuteAt: far,
		}
	}
	jobA, jobB := jobFor(21, "tenant-a"), jobFor(22, "tenant-b")
	require.NoError(t, storage.CreateJob(context.Background(), jobA))
	require.NoError(t, storage.CreateJob(context.Background(), jobB))

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)

	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer(grpc.UnaryInterceptor(
		UnaryAuthorizationInterceptor(&tenantTokenAuthorizer{token: "good", tenantID: callerTenant})))
	engine := scheduler.New(scheduler.DefaultConfig(), storage)
	require.NoError(t, engine.RegisterHandler("h", func(context.Context, *scheduler.Job) (*scheduler.JobResult, error) {
		return &scheduler.JobResult{}, nil
	}))
	require.NoError(t, engine.Start(context.Background()))
	t.Cleanup(func() { _ = engine.Stop(context.Background()) })
	pb.RegisterSchedulerServiceServer(server, NewSchedulerServer(service.NewSchedulerAdapter(engine), nil, log))
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)

	conn, err := grpc.NewClient("passthrough:///bufconn",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return pb.NewSchedulerServiceClient(conn), jobA.ID, jobB.ID
}

// TestLegacyListener_TenantCallerIsConfinedToItsTenant is the go-quicbaas
// #1150 guard for the legacy gRPC / Unix-socket listener: its authorization
// interceptor binds the platform token's tenant under go-qrdb's central key,
// which the services read, so a tenant-a caller reaches tenant-a's job and is
// refused with PermissionDenied for tenant-b's.
func TestLegacyListener_TenantCallerIsConfinedToItsTenant(t *testing.T) {
	client, jobA, jobB := startLegacySchedulerListener(t, "tenant-a")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	resp, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: jobA})
	require.NoError(t, err, "tenant-a must reach its own job")
	assert.Equal(t, jobA, resp.GetJob().GetId())

	_, err = client.GetJob(ctx, &pb.GetJobRequest{JobId: jobB, TenantId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))

	_, err = client.ListJobs(ctx, &pb.ListJobsRequest{OwnerId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestLegacyListener_TokenWithoutTenantFailsClosed proves the fail-closed
// half: a token whose AuthorizedContext carries no tenant binds none, and the
// service refuses rather than reading the absence as system reach.
func TestLegacyListener_TokenWithoutTenantFailsClosed(t *testing.T) {
	client, _, jobB := startLegacySchedulerListener(t, "")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	_, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: jobB, TenantId: "tenant-b"})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestLegacyListener_SystemCreateJobForAnotherTenantIsRefused is the
// go-quicbaas#1150 scheduler regression: a system caller creating a job for
// another tenant was answered with success and no job — schedulerTenantEnforce
// Access returns nil for the system tenant, and CreateJob returned that nil as
// its error alongside a nil response. A request field grants the system
// tenant no cross-tenant lift, so the request is refused with a typed error.
func TestLegacyListener_SystemCreateJobForAnotherTenantIsRefused(t *testing.T) {
	client, _, _ := startLegacySchedulerListener(t, "system")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	_, err := client.CreateJob(ctx, &pb.CreateJobRequest{
		Name: "b-backup", Type: pb.JobType_JOB_TYPE_ONE_SHOT, Handler: "h",
		OwnerType: pb.OwnerType_OWNER_TYPE_TENANT, OwnerId: "tenant-b", TargetTenantId: "tenant-b",
		ExecuteAt: time.Now().Add(time.Hour).Unix(),
	})
	require.Error(t, err, "a cross-tenant create must never be answered with success")
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

// TestLegacyListener_SystemCreateJobForItselfIsScheduled proves the system
// tenant still schedules its own (system-namespace) jobs, and that the job it
// is told was created exists.
func TestLegacyListener_SystemCreateJobForItselfIsScheduled(t *testing.T) {
	client, _, _ := startLegacySchedulerListener(t, "system")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	created, err := client.CreateJob(ctx, &pb.CreateJobRequest{
		Name: "rotation", Type: pb.JobType_JOB_TYPE_ONE_SHOT, Handler: "h",
		OwnerType: pb.OwnerType_OWNER_TYPE_SYSTEM, ExecuteAt: time.Now().Add(time.Hour).Unix(),
	})
	require.NoError(t, err)
	require.NotNil(t, created.GetJob())
	require.NotZero(t, created.GetJob().GetId())

	got, err := client.GetJob(ctx, &pb.GetJobRequest{JobId: created.GetJob().GetId()})
	require.NoError(t, err)
	assert.Equal(t, "rotation", got.GetJob().GetName())
}

// TestLegacyListener_TenantCreateJobForAnotherTenantIsRefused proves a tenant
// caller cannot schedule into another tenant, by target or by owner.
func TestLegacyListener_TenantCreateJobForAnotherTenantIsRefused(t *testing.T) {
	client, _, _ := startLegacySchedulerListener(t, "tenant-a")
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer good")

	for name, req := range map[string]*pb.CreateJobRequest{
		"target": {OwnerType: pb.OwnerType_OWNER_TYPE_TENANT, OwnerId: "tenant-a", TargetTenantId: "tenant-b"},
		"owner":  {OwnerType: pb.OwnerType_OWNER_TYPE_TENANT, OwnerId: "tenant-b"},
		"system": {OwnerType: pb.OwnerType_OWNER_TYPE_SYSTEM},
	} {
		req.Name, req.Type, req.Handler = "x", pb.JobType_JOB_TYPE_ONE_SHOT, "h"
		req.ExecuteAt = time.Now().Add(time.Hour).Unix()
		_, err := client.CreateJob(ctx, req)
		require.Error(t, err, name)
		assert.Equal(t, codes.PermissionDenied, status.Code(err), name)
	}
}
