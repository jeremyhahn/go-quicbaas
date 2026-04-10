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

//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestObjectStorageAdmin_ServiceErrors covers the service-level error return
// paths in DeleteBackend, SetBackendStatus, GetStorageStats (with backend/tenant
// stats), RunGarbageCollection, and GC progress with completed timestamp.
func TestObjectStorageAdmin_ServiceErrors(t *testing.T) {
	ctx := context.Background()

	t.Run("DeleteBackend_ServiceError", func(t *testing.T) {
		mock := &mockObjectStorageAdminService{
			deleteBackendFunc: func(_ context.Context, _ *service.DeleteBackendRequest) (*service.DeleteBackendResponse, error) {
				return nil, dberrors.New(dberrors.ErrNotFound, "DeleteBackend", "backend not found")
			},
		}
		srv := NewObjectStorageAdminServer(mock)

		_, err := srv.CreateBackend(ctx, &pb.CreateBackendRequest{Name: "test"}) // just verify the server works
		require.NoError(t, err)

		_, err = srv.DeleteBackend(ctx, &pb.DeleteBackendRequest{Name: "missing"})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("SetBackendStatus_ServiceError", func(t *testing.T) {
		mock := &mockObjectStorageAdminService{
			setBackendStatusFunc: func(_ context.Context, _ *service.SetBackendStatusRequest) (*service.SetBackendStatusResponse, error) {
				return nil, dberrors.New(dberrors.ErrNotFound, "SetBackendStatus", "backend not found")
			},
		}
		srv := NewObjectStorageAdminServer(mock)

		_, err := srv.SetBackendStatus(ctx, &pb.SetBackendStatusRequest{Name: "missing", Enabled: true})
		require.Error(t, err)
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("GetStorageStats_ServiceError", func(t *testing.T) {
		mock := &mockObjectStorageAdminService{
			getStorageStatsFunc: func(_ context.Context, _ *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
				return nil, dberrors.New(dberrors.ErrInternal, "GetStorageStats", "stats unavailable")
			},
		}
		srv := NewObjectStorageAdminServer(mock)

		_, err := srv.GetStorageStats(ctx, &pb.GetStorageStatsRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("GetStorageStats_WithBackendAndTenantStats", func(t *testing.T) {
		mock := &mockObjectStorageAdminService{
			getStorageStatsFunc: func(_ context.Context, _ *service.GetStorageStatsRequest) (*service.GetStorageStatsResponse, error) {
				return &service.GetStorageStatsResponse{
					TotalBytes:      5000000,
					TotalObjects:    500,
					TotalBackends:   2,
					HealthyBackends: 2,
					BackendStats: []*service.BackendInfo{
						{
							Name:        "local-1",
							Type:        "local",
							Enabled:     true,
							Tier:        "hot",
							ObjectCount: 250,
							TotalSize:   2500000,
							Health: &service.BackendHealthInfo{
								Healthy:   true,
								LastCheck: 1000000000,
							},
						},
						{
							Name:        "s3-1",
							Type:        "s3",
							Enabled:     true,
							Tier:        "cold",
							Region:      "us-east-1",
							ObjectCount: 250,
							TotalSize:   2500000,
						},
					},
					TenantStats: []*service.TenantStorageStats{
						{
							TenantID:     "tenant-1",
							TotalBytes:   3000000,
							ObjectCount:  300,
							HotBytes:     1000000,
							WarmBytes:    1000000,
							ColdBytes:    1000000,
							ArchiveBytes: 0,
						},
					},
					CalculatedAt: 1000000000,
				}, nil
			},
		}
		srv := NewObjectStorageAdminServer(mock)

		resp, err := srv.GetStorageStats(ctx, &pb.GetStorageStatsRequest{
			IncludeBackends: true,
			IncludeTenants:  true,
		})
		require.NoError(t, err)
		assert.Len(t, resp.BackendStats, 2)
		assert.True(t, resp.BackendStats[0].Healthy)
		assert.Len(t, resp.TenantStats, 1)
		assert.Equal(t, "tenant-1", resp.TenantStats[0].TenantId)
	})

	t.Run("RunGarbageCollection_ServiceError", func(t *testing.T) {
		mock := &mockObjectStorageAdminService{
			runGarbageCollectionFunc: func(_ context.Context, _ *service.RunGarbageCollectionRequest) (*service.RunGarbageCollectionResponse, error) {
				return nil, dberrors.New(dberrors.ErrInternal, "RunGarbageCollection", "gc failed")
			},
		}
		srv := NewObjectStorageAdminServer(mock)

		_, err := srv.RunGarbageCollection(ctx, &pb.RunGarbageCollectionRequest{})
		require.Error(t, err)
		assert.Equal(t, codes.Internal, status.Code(err))
	})

	t.Run("GetGarbageCollectionProgress_WithCompletedAt", func(t *testing.T) {
		mock := &mockObjectStorageAdminService{
			getGarbageCollectionProgressFunc: func(_ context.Context, _ *service.GetGarbageCollectionProgressRequest) (*service.GetGarbageCollectionProgressResponse, error) {
				return &service.GetGarbageCollectionProgressResponse{
					OperationID:     "gc-123",
					Status:          service.GCStatusCompleted,
					ObjectsScanned:  100,
					ObjectsDeleted:  10,
					BytesReclaimed:  100000,
					PercentComplete: 100.0,
					CompletedAt:     1000000000,
				}, nil
			},
		}
		srv := NewObjectStorageAdminServer(mock)

		resp, err := srv.GetGarbageCollectionProgress(ctx, &pb.GetGarbageCollectionProgressRequest{
			OperationId: "gc-123",
		})
		require.NoError(t, err)
		assert.Equal(t, "gc-123", resp.OperationId)
		assert.NotNil(t, resp.CompletedAt)
		assert.InDelta(t, 100.0, resp.PercentComplete, 0.01)
	})

	t.Run("NilService_AllMethods", func(t *testing.T) {
		srv := &objectStorageAdminServer{service: nil}

		_, err := srv.CreateBackend(ctx, &pb.CreateBackendRequest{Name: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.UpdateBackend(ctx, &pb.UpdateBackendRequest{Name: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.DeleteBackend(ctx, &pb.DeleteBackendRequest{Name: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.SetBackendStatus(ctx, &pb.SetBackendStatusRequest{Name: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.GetStorageStats(ctx, &pb.GetStorageStatsRequest{})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.RunGarbageCollection(ctx, &pb.RunGarbageCollectionRequest{})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.GetGarbageCollectionProgress(ctx, &pb.GetGarbageCollectionProgressRequest{OperationId: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.RepairObject(ctx, &pb.RepairObjectRequest{Key: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))

		_, err = srv.CompactBackend(ctx, &pb.CompactBackendRequest{Name: "x"})
		assert.Equal(t, codes.Unavailable, status.Code(err))
	})
}
