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

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mockTransport implements transport.Transport for testing.
type mockTransport struct {
	requestFn       func(ctx context.Context, service, method string, req interface{}) (interface{}, error)
	requestStreamFn func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error)
}

func (m *mockTransport) Request(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	if m.requestFn != nil {
		return m.requestFn(ctx, service, method, req)
	}
	return nil, nil
}

func (m *mockTransport) RequestStream(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
	if m.requestStreamFn != nil {
		return m.requestStreamFn(ctx, service, method, req)
	}
	return nil, errors.NewInternal("mock", "not implemented")
}

func (m *mockTransport) Conn() interface{} { return nil }
func (m *mockTransport) Close() error      { return nil }
func (m *mockTransport) Healthy() bool     { return true }

// mockStream implements transport.Stream for testing.
type mockStream struct {
	recvFn func() (interface{}, error)
}

func (s *mockStream) Send(msg interface{}) error { return nil }
func (s *mockStream) CloseSend() error           { return nil }
func (s *mockStream) Close() error               { return nil }
func (s *mockStream) Recv() (interface{}, error) {
	if s.recvFn != nil {
		return s.recvFn()
	}
	return nil, errors.NewInternal("mock", "no data")
}

// =============================================================================
// NewClient Tests
// =============================================================================

func TestNewClient(t *testing.T) {
	t.Run("ValidTransport", func(t *testing.T) {
		c := NewClient(&mockTransport{})
		assert.NotNil(t, c)
	})

	t.Run("NilTransport", func(t *testing.T) {
		c := NewClient(nil)
		assert.Nil(t, c)
	})
}

// =============================================================================
// SubscribeJobEvents Validation Tests
// =============================================================================

func TestSubscribeJobEventsValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		ch, err := c.SubscribeJobEvents(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{
			JobID: "job_1",
		})
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("EmptyJobID", func(t *testing.T) {
		ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{
			TenantID: "tenant_1",
		})
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.Contains(t, err.Error(), "job ID is required")
	})

	t.Run("NilContext", func(t *testing.T) {
		ch, err := c.SubscribeJobEvents(nil, &SubscribeJobEventsRequest{ //nolint:staticcheck
			TenantID: "tenant_1",
			JobID:    "job_1",
		})
		require.Error(t, err)
		assert.Nil(t, ch)
		assert.Contains(t, err.Error(), "context is required")
	})
}

func TestSubscribeJobEventsStreamError(t *testing.T) {
	mt := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return nil, errors.NewInternal("test", "stream unavailable")
		},
	}
	c := NewClient(mt)

	ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{
		TenantID: "tenant_1",
		JobID:    "job_1",
	})
	require.Error(t, err)
	assert.Nil(t, ch)
	assert.Contains(t, err.Error(), "stream unavailable")
}

func TestSubscribeJobEventsSuccess(t *testing.T) {
	events := []*JobEvent{
		{JobID: "job_1", Type: "task_started", Message: "Starting task"},
		{JobID: "job_1", Type: "task_completed", Message: "Task done"},
	}
	idx := 0

	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			if idx >= len(events) {
				return nil, errors.NewInternal("mock", "stream ended")
			}
			evt := events[idx]
			idx++
			return evt, nil
		},
	}

	mt := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, "SubscribeJobEvents", method)
			r, ok := req.(*SubscribeJobEventsRequest)
			require.True(t, ok)
			assert.Equal(t, "tenant_1", r.TenantID)
			assert.Equal(t, "job_1", r.JobID)
			return ms, nil
		},
	}

	c := NewClient(mt)

	ch, err := c.SubscribeJobEvents(context.Background(), &SubscribeJobEventsRequest{
		TenantID: "tenant_1",
		JobID:    "job_1",
	})
	require.NoError(t, err)
	require.NotNil(t, ch)

	var received []*JobEvent
	for evt := range ch {
		received = append(received, evt)
	}

	assert.Len(t, received, 2)
	assert.Equal(t, "task_started", received[0].Type)
	assert.Equal(t, "Starting task", received[0].Message)
	assert.Equal(t, "task_completed", received[1].Type)
}

func TestSubscribeJobEventsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	blockCh := make(chan struct{})
	ms := &mockStream{
		recvFn: func() (interface{}, error) {
			<-blockCh
			return nil, errors.NewInternal("mock", "cancelled")
		},
	}

	mt := &mockTransport{
		requestStreamFn: func(ctx context.Context, service, method string, req interface{}) (transport.Stream, error) {
			return ms, nil
		},
	}

	c := NewClient(mt)

	ch, err := c.SubscribeJobEvents(ctx, &SubscribeJobEventsRequest{
		TenantID: "tenant_1",
		JobID:    "job_1",
	})
	require.NoError(t, err)
	require.NotNil(t, ch)

	// Cancel the context to trigger shutdown.
	cancel()
	close(blockCh)

	// The channel should be closed after context cancellation.
	for range ch {
		// drain
	}
	// If we get here, the channel was closed properly.
}

// =============================================================================
// StartJob Validation Tests
// =============================================================================

func TestStartJobValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		resp, err := c.StartJob(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyDescription", func(t *testing.T) {
		resp, err := c.StartJob(context.Background(), &StartJobRequest{})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "description is required")
	})

	t.Run("NilContext", func(t *testing.T) {
		resp, err := c.StartJob(nil, &StartJobRequest{Description: "test"}) //nolint:staticcheck
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "context is required")
	})
}

// =============================================================================
// GetJobStatus Validation Tests
// =============================================================================

func TestGetJobStatusValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		resp, err := c.GetJobStatus(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		resp, err := c.GetJobStatus(context.Background(), &GetJobStatusRequest{JobID: "job_1"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("EmptyJobID", func(t *testing.T) {
		resp, err := c.GetJobStatus(context.Background(), &GetJobStatusRequest{TenantID: "t1"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "job ID is required")
	})
}

// =============================================================================
// CancelJob Validation Tests
// =============================================================================

func TestCancelJobValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		resp, err := c.CancelJob(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		resp, err := c.CancelJob(context.Background(), &CancelJobRequest{JobID: "job_1"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("EmptyJobID", func(t *testing.T) {
		resp, err := c.CancelJob(context.Background(), &CancelJobRequest{TenantID: "t1"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "job ID is required")
	})
}
