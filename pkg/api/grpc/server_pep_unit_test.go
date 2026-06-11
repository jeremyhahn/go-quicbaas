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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// NOTE: the PEPService test double for this file is mockPEPServiceCoverage,
// defined below. The previous local mockPEPService type was removed because it
// duplicated (and collided with) the canonical mockPEPService in
// server_iam_rag_test.go once the phantom grpc build tag was dropped.

func createTestPEPServer(t *testing.T) (*PEPServer, *mockPEPServiceCoverage) {
	t.Helper()

	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "pep-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := &mockPEPServiceCoverage{}
	srv := NewPEPServer(mockSvc, log)
	return srv, mockSvc
}

func TestNewPEPServer(t *testing.T) {
	logConfig := logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "pep-test",
	}
	log, err := logger.New(logConfig)
	require.NoError(t, err)

	mockSvc := &mockPEPServiceCoverage{}
	srv := NewPEPServer(mockSvc, log)

	assert.NotNil(t, srv)
	assert.NotNil(t, srv.pepService)
	assert.NotNil(t, srv.logger)
}

func TestPEPServer_Check(t *testing.T) {
	ctx := context.Background()

	t.Run("success allowed", func(t *testing.T) {
		srv, mockSvc := createTestPEPServer(t)
		mockSvc.checkFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return &iam.CheckResponse{
				Allowed:          true,
				MatchedPolicy:    "policy-1",
				MatchedRule:      "rule-1",
				EvaluationTimeNs: 5000,
			}, nil
		}

		resp, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user1", TenantId: "tenant1"},
			Action:   "read",
			Resource: "document:123",
			Context:  map[string]string{"key": "value"},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.True(t, resp.Allowed)
		assert.Equal(t, "policy-1", resp.MatchedPolicy)
		assert.Equal(t, "rule-1", resp.MatchedRule)
		assert.Equal(t, int64(5000), resp.EvaluationTimeNs)
	})

	t.Run("success denied", func(t *testing.T) {
		srv, mockSvc := createTestPEPServer(t)
		mockSvc.checkFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return &iam.CheckResponse{
				Allowed:          false,
				EvaluationTimeNs: 3000,
			}, nil
		}

		resp, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user1", TenantId: "tenant1"},
			Action:   "delete",
			Resource: "document:123",
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.False(t, resp.Allowed)
	})

	t.Run("error missing subject", func(t *testing.T) {
		srv, _ := createTestPEPServer(t)

		resp, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  nil,
			Action:   "read",
			Resource: "document:123",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error missing action", func(t *testing.T) {
		srv, _ := createTestPEPServer(t)

		resp, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user1"},
			Action:   "",
			Resource: "document:123",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error missing resource", func(t *testing.T) {
		srv, _ := createTestPEPServer(t)

		resp, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user1"},
			Action:   "read",
			Resource: "",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error service failure", func(t *testing.T) {
		srv, mockSvc := createTestPEPServer(t)
		mockSvc.checkFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return nil, errors.New("service error")
		}

		resp, err := srv.Check(ctx, &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user1"},
			Action:   "read",
			Resource: "document:123",
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestPEPServer_CheckBatch(t *testing.T) {
	ctx := context.Background()

	t.Run("success all allowed", func(t *testing.T) {
		srv, mockSvc := createTestPEPServer(t)
		mockSvc.checkBatchFunc = func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
			return []bool{true, true, true}, 10000, nil
		}

		resp, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user1", TenantId: "tenant1"},
			Action:    "read",
			Resources: []string{"doc:1", "doc:2", "doc:3"},
			Context:   map[string]string{"env": "prod"},
		})

		require.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Len(t, resp.Results, 3)
		assert.True(t, resp.Results[0])
		assert.True(t, resp.Results[1])
		assert.True(t, resp.Results[2])
		assert.Equal(t, int64(10000), resp.EvaluationTimeNs)
	})

	t.Run("success mixed results", func(t *testing.T) {
		srv, mockSvc := createTestPEPServer(t)
		mockSvc.checkBatchFunc = func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
			return []bool{true, false, true}, 8000, nil
		}

		resp, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user1"},
			Action:    "write",
			Resources: []string{"doc:1", "doc:2", "doc:3"},
		})

		require.NoError(t, err)
		assert.True(t, resp.Results[0])
		assert.False(t, resp.Results[1])
		assert.True(t, resp.Results[2])
	})

	t.Run("error missing subject", func(t *testing.T) {
		srv, _ := createTestPEPServer(t)

		resp, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   nil,
			Action:    "read",
			Resources: []string{"doc:1"},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error missing action", func(t *testing.T) {
		srv, _ := createTestPEPServer(t)

		resp, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user1"},
			Action:    "",
			Resources: []string{"doc:1"},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error empty resources", func(t *testing.T) {
		srv, _ := createTestPEPServer(t)

		resp, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user1"},
			Action:    "read",
			Resources: []string{},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("error service failure", func(t *testing.T) {
		srv, mockSvc := createTestPEPServer(t)
		mockSvc.checkBatchFunc = func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
			return nil, 0, errors.New("batch error")
		}

		resp, err := srv.CheckBatch(ctx, &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user1"},
			Action:    "read",
			Resources: []string{"doc:1"},
		})

		require.Error(t, err)
		assert.Nil(t, resp)
	})
}

func TestConvertPolicyUpdateType(t *testing.T) {
	tests := []struct {
		name     string
		input    PolicyUpdateType
		expected iampb.PolicyUpdate_Type
	}{
		{
			name:     "snapshot",
			input:    PolicyUpdateSnapshot,
			expected: iampb.PolicyUpdate_SNAPSHOT,
		},
		{
			name:     "created",
			input:    PolicyUpdateCreated,
			expected: iampb.PolicyUpdate_CREATED,
		},
		{
			name:     "updated",
			input:    PolicyUpdateUpdated,
			expected: iampb.PolicyUpdate_UPDATED,
		},
		{
			name:     "deleted",
			input:    PolicyUpdateDeleted,
			expected: iampb.PolicyUpdate_DELETED,
		},
		{
			name:     "unknown defaults to snapshot",
			input:    PolicyUpdateType(999),
			expected: iampb.PolicyUpdate_SNAPSHOT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertPolicyUpdateType(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestConvertPEPError(t *testing.T) {
	t.Run("nil error returns nil", func(t *testing.T) {
		result := convertPEPError(nil)
		assert.Nil(t, result)
	})

	t.Run("non-QBError converts to Internal", func(t *testing.T) {
		err := errors.New("some error")
		result := convertPEPError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})

	t.Run("QBError NotFound", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrNotFound, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.NotFound, status.Code(result))
	})

	t.Run("QBError AlreadyExists", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.AlreadyExists, status.Code(result))
	})

	t.Run("QBError InvalidInput", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.InvalidArgument, status.Code(result))
	})

	t.Run("QBError Validation", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrValidation, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.InvalidArgument, status.Code(result))
	})

	t.Run("QBError Unauthorized", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrUnauthorized, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.Unauthenticated, status.Code(result))
	})

	t.Run("QBError Forbidden", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.PermissionDenied, status.Code(result))
	})

	t.Run("QBError Timeout", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrTimeout, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.DeadlineExceeded, status.Code(result))
	})

	t.Run("QBError DeadlineExceeded", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrDeadlineExceeded, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.DeadlineExceeded, status.Code(result))
	})

	t.Run("QBError ResourceExhausted", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrResourceExhausted, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(result))
	})

	t.Run("QBError Unavailable", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrUnavailable, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.Unavailable, status.Code(result))
	})

	t.Run("QBError Conflict", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrConflict, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.FailedPrecondition, status.Code(result))
	})

	t.Run("QBError Unimplemented", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrUnimplemented, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.Unimplemented, status.Code(result))
	})

	t.Run("QBError NotImplemented", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrNotImplemented, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.Unimplemented, status.Code(result))
	})

	t.Run("QBError Canceled", func(t *testing.T) {
		err := &dberrors.QBError{Code: dberrors.ErrCanceled, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.Canceled, status.Code(result))
	})

	t.Run("QBError unknown code defaults to Internal", func(t *testing.T) {
		err := &dberrors.QBError{Code: 9999, Op: "test"}
		result := convertPEPError(err)
		assert.Equal(t, codes.Internal, status.Code(result))
	})
}

func TestPEPServiceAdapter(t *testing.T) {
	t.Run("NewPEPServiceAdapter", func(t *testing.T) {
		// Create a minimal policy engine for testing
		adapter := NewPEPServiceAdapter(nil)
		assert.NotNil(t, adapter)
	})

	t.Run("Subscribe blocks until context cancelled", func(t *testing.T) {
		adapter := NewPEPServiceAdapter(nil)
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		updates := make(chan *PolicyUpdate, 10)
		err := adapter.Subscribe(ctx, "test-service", []string{"resource:*"}, updates)

		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// --- Merged from server_pep_coverage_test.go ---

// mockPEPServiceCoverage implements PEPService for testing without grpc build tag.
type mockPEPServiceCoverage struct {
	checkFunc      func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)
	checkBatchFunc func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error)
	subscribeFunc  func(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error
}

func (m *mockPEPServiceCoverage) Check(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx, req)
	}
	return &iam.CheckResponse{Allowed: true, EvaluationTimeNs: 1000}, nil
}

func (m *mockPEPServiceCoverage) CheckBatch(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}) ([]bool, int64, error) {
	if m.checkBatchFunc != nil {
		return m.checkBatchFunc(ctx, subject, action, resources, context)
	}
	results := make([]bool, len(resources))
	for i := range results {
		results[i] = true
	}
	return results, 1000, nil
}

func (m *mockPEPServiceCoverage) Subscribe(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, serviceID, resourcePatterns, updates)
	}
	<-ctx.Done()
	return ctx.Err()
}

func newTestPEPServer(t *testing.T, svc PEPService) *PEPServer {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stderr"})
	require.NoError(t, err)
	return NewPEPServer(svc, log)
}

func TestPEPServerCoverage_Check_Success(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkFunc: func(_ context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
			return &iam.CheckResponse{
				Allowed:          true,
				MatchedPolicy:    "policy-1",
				MatchedRule:      "rule-1",
				EvaluationTimeNs: 5000,
			}, nil
		},
	}
	srv := newTestPEPServer(t, svc)

	resp, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user1", TenantId: "tenant1"},
		Action:   "read",
		Resource: "document:123",
		Context:  map[string]string{"key": "value"},
	})
	require.NoError(t, err)
	assert.True(t, resp.Allowed)
	assert.Equal(t, "policy-1", resp.MatchedPolicy)
	assert.Equal(t, "rule-1", resp.MatchedRule)
	assert.Equal(t, int64(5000), resp.EvaluationTimeNs)
}

func TestPEPServerCoverage_Check_Denied(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkFunc: func(_ context.Context, _ *iam.CheckRequest) (*iam.CheckResponse, error) {
			return &iam.CheckResponse{Allowed: false}, nil
		},
	}
	srv := newTestPEPServer(t, svc)

	resp, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user1"},
		Action:   "delete",
		Resource: "document:123",
	})
	require.NoError(t, err)
	assert.False(t, resp.Allowed)
}

func TestPEPServerCoverage_Check_MissingSubject(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	_, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  nil,
		Action:   "read",
		Resource: "doc:1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPServerCoverage_Check_MissingAction(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	_, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user1"},
		Action:   "",
		Resource: "doc:1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPServerCoverage_Check_MissingResource(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	_, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user1"},
		Action:   "read",
		Resource: "",
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPServerCoverage_Check_ServiceError(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkFunc: func(_ context.Context, _ *iam.CheckRequest) (*iam.CheckResponse, error) {
			return nil, &dberrors.QBError{Code: dberrors.ErrInternal, Op: "test"}
		},
	}
	srv := newTestPEPServer(t, svc)
	_, err := srv.Check(context.Background(), &iampb.CheckRequest{
		Subject:  &iampb.Subject{Id: "user1"},
		Action:   "read",
		Resource: "doc:1",
	})
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
}

func TestPEPServerCoverage_CheckBatch_Success(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkBatchFunc: func(_ context.Context, _ *iam.Subject, _ string, resources []string, _ map[string]interface{}) ([]bool, int64, error) {
			return []bool{true, false, true}, 10000, nil
		},
	}
	srv := newTestPEPServer(t, svc)

	resp, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user1", TenantId: "tenant1"},
		Action:    "read",
		Resources: []string{"doc:1", "doc:2", "doc:3"},
		Context:   map[string]string{"env": "prod"},
	})
	require.NoError(t, err)
	assert.Len(t, resp.Results, 3)
	assert.True(t, resp.Results[0])
	assert.False(t, resp.Results[1])
	assert.True(t, resp.Results[2])
	assert.Equal(t, int64(10000), resp.EvaluationTimeNs)
}

func TestPEPServerCoverage_CheckBatch_MissingSubject(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	_, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   nil,
		Action:    "read",
		Resources: []string{"doc:1"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPServerCoverage_CheckBatch_MissingAction(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	_, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user1"},
		Action:    "",
		Resources: []string{"doc:1"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPServerCoverage_CheckBatch_EmptyResources(t *testing.T) {
	srv := newTestPEPServer(t, &mockPEPServiceCoverage{})
	_, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user1"},
		Action:    "read",
		Resources: []string{},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestPEPServerCoverage_CheckBatch_ServiceError(t *testing.T) {
	svc := &mockPEPServiceCoverage{
		checkBatchFunc: func(_ context.Context, _ *iam.Subject, _ string, _ []string, _ map[string]interface{}) ([]bool, int64, error) {
			return nil, 0, &dberrors.QBError{Code: dberrors.ErrForbidden, Op: "test"}
		},
	}
	srv := newTestPEPServer(t, svc)
	_, err := srv.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
		Subject:   &iampb.Subject{Id: "user1"},
		Action:    "read",
		Resources: []string{"doc:1"},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestPEPServerCoverage_ConvertPolicyUpdateType(t *testing.T) {
	tests := []struct {
		name     string
		input    PolicyUpdateType
		expected iampb.PolicyUpdate_Type
	}{
		{"snapshot", PolicyUpdateSnapshot, iampb.PolicyUpdate_SNAPSHOT},
		{"created", PolicyUpdateCreated, iampb.PolicyUpdate_CREATED},
		{"updated", PolicyUpdateUpdated, iampb.PolicyUpdate_UPDATED},
		{"deleted", PolicyUpdateDeleted, iampb.PolicyUpdate_DELETED},
		{"unknown", PolicyUpdateType(999), iampb.PolicyUpdate_SNAPSHOT},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, convertPolicyUpdateType(tt.input))
		})
	}
}

func TestPEPServerCoverage_ConvertPEPError(t *testing.T) {
	assert.Nil(t, convertPEPError(nil))

	assert.Equal(t, codes.Internal, status.Code(convertPEPError(errors.New("plain error"))))
	assert.Equal(t, codes.NotFound, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrNotFound, Op: "t"})))
	assert.Equal(t, codes.AlreadyExists, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrAlreadyExists, Op: "t"})))
	assert.Equal(t, codes.InvalidArgument, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrInvalidInput, Op: "t"})))
	assert.Equal(t, codes.InvalidArgument, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrValidation, Op: "t"})))
	assert.Equal(t, codes.Unauthenticated, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrUnauthorized, Op: "t"})))
	assert.Equal(t, codes.PermissionDenied, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrForbidden, Op: "t"})))
	assert.Equal(t, codes.DeadlineExceeded, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrTimeout, Op: "t"})))
	assert.Equal(t, codes.DeadlineExceeded, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrDeadlineExceeded, Op: "t"})))
	assert.Equal(t, codes.ResourceExhausted, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrResourceExhausted, Op: "t"})))
	assert.Equal(t, codes.Unavailable, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrUnavailable, Op: "t"})))
	assert.Equal(t, codes.FailedPrecondition, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrConflict, Op: "t"})))
	assert.Equal(t, codes.Unimplemented, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrUnimplemented, Op: "t"})))
	assert.Equal(t, codes.Unimplemented, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrNotImplemented, Op: "t"})))
	assert.Equal(t, codes.Canceled, status.Code(convertPEPError(&dberrors.QBError{Code: dberrors.ErrCanceled, Op: "t"})))
	assert.Equal(t, codes.Internal, status.Code(convertPEPError(&dberrors.QBError{Code: 9999, Op: "t"})))
}

func TestPEPServerCoverage_NewPEPServiceAdapter(t *testing.T) {
	adapter := NewPEPServiceAdapter(nil)
	assert.NotNil(t, adapter)
}

func TestPEPServerCoverage_SubscribeContextCancel(t *testing.T) {
	adapter := NewPEPServiceAdapter(nil)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	updates := make(chan *PolicyUpdate, 10)
	err := adapter.Subscribe(ctx, "test-service", []string{"resource:*"}, updates)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
