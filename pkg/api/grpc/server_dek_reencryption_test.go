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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// setupTestDEKServer creates a DEKServer with a mock KMS service for testing.
func setupTestDEKServer(t *testing.T) (*DEKServer, *testutil.MockQuicBaaS) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "dek-grpc-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := testutil.NewMockQuicBaaS()
	srv := NewDEKServer(mockSvc, log)

	return srv, mockSvc
}

// ========================================
// StartReencryption Tests
// ========================================

func TestDEKServer_StartReencryption_Success(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("StartReencryption", mock.Anything, mock.Anything).Return(
		&service.StartReencryptionResponse{
			OperationID:   "op-123",
			TotalEntities: 1000,
			StartedAt:     time.Now(),
		}, nil)

	req := &pb.StartReencryptionRequest{
		Path:        "/test/keys",
		FromVersion: 1,
		ToVersion:   2,
		BatchSize:   100,
	}

	resp, err := srv.StartReencryption(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "op-123", resp.OperationId)
	assert.Equal(t, "/test/keys", resp.Path)
	assert.True(t, resp.Started)
	assert.Equal(t, int32(1000), resp.EstimatedItems)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_StartReencryption_EmptyPath(t *testing.T) {
	srv, _ := setupTestDEKServer(t)
	ctx := context.Background()

	req := &pb.StartReencryptionRequest{
		Path:        "",
		FromVersion: 1,
		ToVersion:   2,
	}

	resp, err := srv.StartReencryption(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "path is required")
}

func TestDEKServer_StartReencryption_ServiceError(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("StartReencryption", mock.Anything, mock.Anything).Return(
		(*service.StartReencryptionResponse)(nil),
		&dberrors.DragonError{
			Code: dberrors.ErrInternal,
			Op:   "StartReencryption",
			Err:  &dberrors.MessageError{Message: "reencryption failed"},
		})

	req := &pb.StartReencryptionRequest{
		Path:        "/test/keys",
		FromVersion: 1,
		ToVersion:   2,
	}

	resp, err := srv.StartReencryption(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_StartReencryption_LargeEntityCount(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	// Test with a very large entity count to trigger bounds check
	mockSvc.On("StartReencryption", mock.Anything, mock.Anything).Return(
		&service.StartReencryptionResponse{
			OperationID:   "op-large",
			TotalEntities: int64(1) << 40, // Very large number exceeding MaxInt32
			StartedAt:     time.Now(),
		}, nil)

	req := &pb.StartReencryptionRequest{
		Path:      "/test/keys",
		BatchSize: 500,
	}

	resp, err := srv.StartReencryption(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	// Should be capped at MaxInt32
	assert.Equal(t, int32(2147483647), resp.EstimatedItems)
	mockSvc.AssertExpectations(t)
}

// ========================================
// GetReencryptionProgress Tests
// ========================================

func TestDEKServer_GetReencryptionProgress_Success_Running(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		&service.GetReencryptionProgressResponse{
			Progress: &service.ReencryptionProgress{
				TotalKeys:     1000,
				ProcessedKeys: 500,
				FailedKeys:    0,
				InProgress:    true,
				Status:        "",
			},
			Timestamp: time.Now(),
		}, nil)

	req := &pb.GetReencryptionProgressRequest{
		Path:        "/test/keys",
		OperationId: "op-123",
	}

	resp, err := srv.GetReencryptionProgress(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "running", resp.Status)
	assert.Equal(t, "/test/keys", resp.Path)
	assert.NotNil(t, resp.Progress)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_GetReencryptionProgress_Success_Completed(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		&service.GetReencryptionProgressResponse{
			Progress: &service.ReencryptionProgress{
				TotalKeys:     1000,
				ProcessedKeys: 1000,
				FailedKeys:    0,
				InProgress:    false,
				Status:        "",
			},
			Timestamp: time.Now(),
		}, nil)

	req := &pb.GetReencryptionProgressRequest{
		Path: "/test/keys",
	}

	resp, err := srv.GetReencryptionProgress(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "completed", resp.Status)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_GetReencryptionProgress_Success_Failed(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		&service.GetReencryptionProgressResponse{
			Progress: &service.ReencryptionProgress{
				TotalKeys:     1000,
				ProcessedKeys: 500,
				FailedKeys:    10,
				InProgress:    false,
				Status:        "",
			},
			Timestamp: time.Now(),
		}, nil)

	req := &pb.GetReencryptionProgressRequest{
		Path: "/test/keys",
	}

	resp, err := srv.GetReencryptionProgress(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "failed", resp.Status)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_GetReencryptionProgress_Success_WithExplicitStatus(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		&service.GetReencryptionProgressResponse{
			Progress: &service.ReencryptionProgress{
				Status: "paused",
			},
			Timestamp: time.Now(),
		}, nil)

	req := &pb.GetReencryptionProgressRequest{
		Path: "/test/keys",
	}

	resp, err := srv.GetReencryptionProgress(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "paused", resp.Status)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_GetReencryptionProgress_Success_NilProgress(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		&service.GetReencryptionProgressResponse{
			Progress:  nil,
			Timestamp: time.Now(),
		}, nil)

	req := &pb.GetReencryptionProgressRequest{
		Path: "/test/keys",
	}

	resp, err := srv.GetReencryptionProgress(ctx, req)

	require.NoError(t, err)
	assert.NotNil(t, resp)
	assert.Equal(t, "not_started", resp.Status)
	mockSvc.AssertExpectations(t)
}

func TestDEKServer_GetReencryptionProgress_ServiceError(t *testing.T) {
	srv, mockSvc := setupTestDEKServer(t)
	ctx := context.Background()

	mockSvc.On("GetReencryptionProgress", mock.Anything, mock.Anything).Return(
		(*service.GetReencryptionProgressResponse)(nil),
		&dberrors.DragonError{
			Code: dberrors.ErrNotFound,
			Op:   "GetReencryptionProgress",
			Err:  &dberrors.MessageError{Message: "operation not found"},
		})

	req := &pb.GetReencryptionProgressRequest{
		Path:        "/test/keys",
		OperationId: "unknown-op",
	}

	resp, err := srv.GetReencryptionProgress(ctx, req)

	require.Error(t, err)
	assert.Nil(t, resp)
	mockSvc.AssertExpectations(t)
}
