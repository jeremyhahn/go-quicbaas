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
	"math"
	"strings"
	"testing"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	apigrpc "github.com/jeremyhahn/go-quicbaas/pkg/api/grpc"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// newRealProjectHandler binds the SDK project handler to the REAL gRPC project
// server over the REAL project service, so a round trip exercises the SDK →
// proto mapping, the server's proto → service mapping, the service's builder
// and applier, and the record → proto → SDK read path together. The Unix
// transport is gRPC on a domain socket and dispatches through this handler.
func newRealProjectHandler(t *testing.T) *projectHandler {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	// The project server honours tenant_id only for the caller's own tenant
	// (#1150), so the server binds the tenant the tests name, as the daemon's
	// auth interceptor does.
	bindTenant := func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(tenantctx.WithTenant(ctx, "t1"), req)
	}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterProjectServiceServer(s, apigrpc.NewProjectServer(svc, log))
	}, grpc.UnaryInterceptor(bindTenant))
	return newProjectHandler(srv.dial(t))
}

func grpcProject(t *testing.T, h *projectHandler, method string, req interface{}) *project.ProjectInfo {
	t.Helper()
	resp, err := h.handleRequest(context.Background(), projectServicePrefix, method, req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	info, ok := resp.(*project.ProjectInfo)
	if !ok {
		t.Fatalf("%s returned %T", method, resp)
	}
	return info
}

// TestProjectGRPC_StorageRoundTrip: set the tier over gRPC, read it back, and
// assert it survived; an update omitting the sizes leaves them, and one
// sending zero clears them.
func TestProjectGRPC_StorageRoundTrip(t *testing.T) {
	h := newRealProjectHandler(t)

	created := grpcProject(t, h, "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		WorkspaceTier:     project.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	})
	want := project.ProjectStorage{
		WorkspaceTier:     project.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}
	if created.Storage != want {
		t.Fatalf("create returned storage %+v, want %+v", created.Storage, want)
	}
	get := func() project.ProjectStorage {
		t.Helper()
		return grpcProject(t, h, "Get", &project.GetProjectRequest{TenantID: "t1", ProjectID: created.ID}).Storage
	}
	if got := get(); got != want {
		t.Fatalf("read back storage %+v, want %+v", got, want)
	}

	grpcProject(t, h, "Update", &project.UpdateProjectRequest{TenantID: "t1", ProjectID: created.ID, Description: "renamed"})
	if got := get(); got != want {
		t.Fatalf("an update omitting the sizes changed storage to %+v", got)
	}

	zero := 0
	grpcProject(t, h, "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: created.ID, WorkspaceTmpfsMB: &zero, ContainerMemoryMB: &zero,
	})
	if got := get(); got != (project.ProjectStorage{WorkspaceTier: project.WorkspaceTierEphemeral}) {
		t.Fatalf("an update sending zero left storage %+v, want both sizes cleared", got)
	}
}

// TestProjectGRPC_InvalidTierIsRefusedByTheService: the refusal is the
// service's, surfaced as InvalidArgument naming the field.
func TestProjectGRPC_InvalidTierIsRefusedByTheService(t *testing.T) {
	h := newRealProjectHandler(t)
	ctx := context.Background()

	_, err := h.handleRequest(ctx, projectServicePrefix, "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api", WorkspaceTier: "ramdisk",
	})
	if !errors.IsInvalidInput(err) || !strings.Contains(err.Error(), "workspace_tier") {
		t.Fatalf("create with an unknown tier: err = %v, want an invalid-input refusal naming workspace_tier", err)
	}

	created := grpcProject(t, h, "Create", &project.CreateProjectRequest{TenantID: "t1", Name: "api"})
	_, err = h.handleRequest(ctx, projectServicePrefix, "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: created.ID, WorkspaceTier: "ramdisk",
	})
	if !errors.IsInvalidInput(err) || !strings.Contains(err.Error(), "workspace_tier") {
		t.Fatalf("update with an unknown tier: err = %v, want an invalid-input refusal naming workspace_tier", err)
	}
}

// TestProjectGRPC_StorageSizeOutOfWireRange: a size the int32 wire cannot
// carry is refused, never wrapped onto a limit nobody asked for.
func TestProjectGRPC_StorageSizeOutOfWireRange(t *testing.T) {
	h := newRealProjectHandler(t)
	huge := math.MaxInt32 + 1

	_, err := h.handleRequest(context.Background(), projectServicePrefix, "Create", &project.CreateProjectRequest{
		TenantID: "t1", Name: "api", ContainerMemoryMB: huge,
	})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("create with an out-of-range size: err = %v, want invalid input", err)
	}
	_, err = h.handleRequest(context.Background(), projectServicePrefix, "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", WorkspaceTmpfsMB: &huge,
	})
	if !errors.IsInvalidInput(err) {
		t.Fatalf("update with an out-of-range size: err = %v, want invalid input", err)
	}
}

// TestProjectGRPC_MaxConcurrencyOutOfWireRange: a concurrency cap the int32
// wire cannot carry is refused, never wrapped onto a cap nobody asked for.
func TestProjectGRPC_MaxConcurrencyOutOfWireRange(t *testing.T) {
	h := newRealProjectHandler(t)
	huge := math.MaxInt32 + 1

	_, err := h.handleRequest(context.Background(), projectServicePrefix, "Update", &project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", MaxConcurrency: &huge,
	})
	if !errors.IsInvalidInput(err) || !strings.Contains(err.Error(), "max_concurrency") {
		t.Fatalf("update with an out-of-range cap: err = %v, want an invalid-input refusal naming max_concurrency", err)
	}
}
