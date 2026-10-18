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
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// pepTestService is a mock PEPService for coverage testing.
type pepTestService struct {
	checkFunc      func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error)
	checkBatchFunc func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}, stopOnDeny bool) ([]bool, int64, error)
	subscribeFunc  func(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error
}

func (m *pepTestService) Check(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
	if m.checkFunc != nil {
		return m.checkFunc(ctx, req)
	}
	return &iam.CheckResponse{Allowed: true, MatchedPolicy: "default-allow"}, nil
}

func (m *pepTestService) CheckBatch(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}, stopOnDeny bool) ([]bool, int64, error) {
	if m.checkBatchFunc != nil {
		return m.checkBatchFunc(ctx, subject, action, resources, context, stopOnDeny)
	}
	results := make([]bool, len(resources))
	for i := range results {
		results[i] = true
	}
	return results, 100, nil
}

func (m *pepTestService) Subscribe(ctx context.Context, serviceID string, resourcePatterns []string, updates chan<- *PolicyUpdate) error {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, serviceID, resourcePatterns, updates)
	}
	<-ctx.Done()
	return ctx.Err()
}

func newPEPTestLogger() *logger.Logger {
	log, _ := logger.New(logger.LogConfig{Level: "debug", Format: "json", Output: "stdout"})
	return log
}

// =============================================================================
// NewPEPServer
// =============================================================================

func TestPEPServerCoverage_NewPEPServer(t *testing.T) {
	t.Run("creates_server_with_service", func(t *testing.T) {
		svc := &pepTestService{}
		server := NewPEPServer(svc, newPEPTestLogger())
		require.NotNil(t, server)
		assert.NotNil(t, server.pepService)
		assert.NotNil(t, server.logger)
	})
}

// =============================================================================
// Check
// =============================================================================

func TestPEPServerCoverage_Check(t *testing.T) {
	svc := &pepTestService{}
	server := NewPEPServer(svc, newPEPTestLogger())

	t.Run("missing_subject_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Check(context.Background(), &iampb.CheckRequest{
			Action:   "read",
			Resource: "/data",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "subject")
	})

	t.Run("missing_action_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Check(context.Background(), &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user-1"},
			Resource: "/data",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "action")
	})

	t.Run("missing_resource_returns_invalid_argument", func(t *testing.T) {
		_, err := server.Check(context.Background(), &iampb.CheckRequest{
			Subject: &iampb.Subject{Id: "user-1"},
			Action:  "read",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Contains(t, st.Message(), "resource")
	})

	t.Run("successful_check_returns_response", func(t *testing.T) {
		resp, err := server.Check(context.Background(), &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user-1"},
			Action:   "read",
			Resource: "/data/items",
			Context:  map[string]string{"env": "test"},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.True(t, resp.Allowed)
		assert.Equal(t, "default-allow", resp.MatchedPolicy)
	})

	t.Run("check_with_service_error", func(t *testing.T) {
		errSvc := &pepTestService{
			checkFunc: func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
				return nil, errors.New("policy evaluation failed")
			},
		}
		errServer := NewPEPServer(errSvc, newPEPTestLogger())

		_, err := errServer.Check(context.Background(), &iampb.CheckRequest{
			Subject:  &iampb.Subject{Id: "user-1"},
			Action:   "read",
			Resource: "/data",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

// =============================================================================
// CheckBatch
// =============================================================================

func TestPEPServerCoverage_CheckBatch(t *testing.T) {
	svc := &pepTestService{}
	server := NewPEPServer(svc, newPEPTestLogger())

	t.Run("missing_subject_returns_invalid_argument", func(t *testing.T) {
		_, err := server.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
			Action:    "read",
			Resources: []string{"/data"},
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("missing_action_returns_invalid_argument", func(t *testing.T) {
		_, err := server.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user-1"},
			Resources: []string{"/data"},
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("empty_resources_returns_invalid_argument", func(t *testing.T) {
		_, err := server.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
			Subject: &iampb.Subject{Id: "user-1"},
			Action:  "read",
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
	})

	t.Run("successful_batch_check_returns_results", func(t *testing.T) {
		resp, err := server.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user-1"},
			Action:    "read",
			Resources: []string{"/data/a", "/data/b"},
			Context:   map[string]string{"env": "test"},
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		assert.Len(t, resp.Results, 2)
		assert.True(t, resp.Results[0])
		assert.True(t, resp.Results[1])
	})

	t.Run("batch_check_with_service_error", func(t *testing.T) {
		errSvc := &pepTestService{
			checkBatchFunc: func(ctx context.Context, subject *iam.Subject, action string, resources []string, context map[string]interface{}, stopOnDeny bool) ([]bool, int64, error) {
				return nil, 0, errors.New("batch check failed")
			},
		}
		errServer := NewPEPServer(errSvc, newPEPTestLogger())

		_, err := errServer.CheckBatch(context.Background(), &iampb.CheckBatchRequest{
			Subject:   &iampb.Subject{Id: "user-1"},
			Action:    "read",
			Resources: []string{"/data"},
		})
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
	})
}

// =============================================================================
// PEPServiceAdapter
// =============================================================================

func TestNewPEPServiceAdapterCoverage(t *testing.T) {
	t.Run("creates_adapter_with_nil_engine", func(t *testing.T) {
		adapter := NewPEPServiceAdapter(nil)
		require.NotNil(t, adapter)
		assert.Nil(t, adapter.policyEngine)
	})
}

// TestPEPServiceAdapter_Subscribe tests the Subscribe method which blocks
// until context cancellation.
func TestPEPServiceAdapter_Subscribe(t *testing.T) {
	t.Run("returns_context_canceled_on_cancel", func(t *testing.T) {
		adapter := NewPEPServiceAdapter(nil)
		ctx, cancel := context.WithCancel(context.Background())
		updates := make(chan *PolicyUpdate, 1)

		var wg sync.WaitGroup
		var subErr error
		wg.Add(1)
		go func() {
			defer wg.Done()
			subErr = adapter.Subscribe(ctx, "svc-1", []string{"/data/*"}, updates)
		}()

		// Cancel the context to unblock Subscribe
		cancel()
		wg.Wait()

		require.Error(t, subErr)
		assert.Equal(t, context.Canceled, subErr)
	})
}
