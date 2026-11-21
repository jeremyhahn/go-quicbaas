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
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/page"
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
		var nilCtx context.Context
		ch, err := c.SubscribeJobEvents(nilCtx, &SubscribeJobEventsRequest{
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
		var nilCtx context.Context
		resp, err := c.StartJob(nilCtx, &StartJobRequest{Description: "test"})
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

// =============================================================================
// Workflow CRUD Tests
// =============================================================================

func TestCreateWorkflowValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		wf, err := c.CreateWorkflow(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		wf, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{
			Definition: &WorkflowDefinition{Name: "wf1"},
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("NilDefinition", func(t *testing.T) {
		wf, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{
			TenantID: "t1",
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "definition is required")
	})

	t.Run("EmptyWorkflowName", func(t *testing.T) {
		wf, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{
			TenantID:   "t1",
			Definition: &WorkflowDefinition{},
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "workflow name is required")
	})

	t.Run("NilContext", func(t *testing.T) {
		var nilCtx context.Context
		wf, err := c.CreateWorkflow(nilCtx, &CreateWorkflowRequest{
			TenantID:   "t1",
			Definition: &WorkflowDefinition{Name: "wf1"},
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "context is required")
	})
}

func TestCreateWorkflowSuccess(t *testing.T) {
	want := &WorkflowDefinition{
		Name: "my-workflow",
		Mode: WorkflowModeConstrained,
		Agents: []AgentRoleSpec{
			{Role: "lead", Backend: "claude"},
		},
	}

	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, "CreateWorkflow", method)
			r, ok := req.(*CreateWorkflowRequest)
			require.True(t, ok)
			assert.Equal(t, "t1", r.TenantID)
			assert.Equal(t, "my-workflow", r.Definition.Name)
			return want, nil
		},
	}
	c := NewClient(mt)

	got, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{
		TenantID:   "t1",
		Definition: want,
	})
	require.NoError(t, err)
	assert.Equal(t, want.Name, got.Name)
	assert.Equal(t, want.Mode, got.Mode)
}

func TestListWorkflowsValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		wfs, err := c.ListWorkflows(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, wfs)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		wfs, err := c.ListWorkflows(context.Background(), &ListWorkflowsRequest{})
		require.Error(t, err)
		assert.Nil(t, wfs)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})
}

func TestListWorkflowsSuccess(t *testing.T) {
	resp := &ListWorkflowsResponse{
		Workflows: []*WorkflowDefinition{
			{Name: "wf-a", Mode: WorkflowModeAutonomous},
			{Name: "wf-b", Mode: WorkflowModeConstrained},
		},
		Count: 2,
	}

	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "ListWorkflows", method)
			return resp, nil
		},
	}
	c := NewClient(mt)

	wfs, err := c.ListWorkflows(context.Background(), &ListWorkflowsRequest{TenantID: "t1"})
	require.NoError(t, err)
	require.NotNil(t, wfs)
	assert.Len(t, wfs.Workflows, 2)
	assert.Equal(t, "wf-a", wfs.Workflows[0].Name)
}

func TestListWorkflowsNilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)

	wfs, err := c.ListWorkflows(context.Background(), &ListWorkflowsRequest{TenantID: "t1"})
	require.NoError(t, err)
	require.NotNil(t, wfs)
	assert.Empty(t, wfs.Workflows)
}

func TestListBackendsNoArgsFetchesAllPages(t *testing.T) {
	var offsets []int
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			require.Equal(t, "ListBackends", method)
			listReq, ok := req.(*ListBackendsRequest)
			require.True(t, ok)
			offsets = append(offsets, listReq.Offset)
			if listReq.Offset == 0 {
				return &ListBackendsResponse{
					Backends: []*BackendInfo{{Name: "a"}},
					Envelope: page.Envelope{Total: 2, Limit: 1, Offset: 0, NextOffset: 1},
				}, nil
			}
			return &ListBackendsResponse{
				Backends: []*BackendInfo{{Name: "b"}},
				Envelope: page.Envelope{Total: 2, Limit: 1, Offset: 1},
			}, nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackends(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []int{0, 1}, offsets)
	assert.Equal(t, []string{"a", "b"}, []string{resp.Backends[0].Name, resp.Backends[1].Name})
	assert.Equal(t, 2, resp.Count)
}

func TestListBackendsExplicitPageDoesNotFetchAllPages(t *testing.T) {
	calls := 0
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			calls++
			return &ListBackendsResponse{
				Backends: []*BackendInfo{{Name: "a"}},
				Envelope: page.Envelope{Total: 2, Limit: 1, Offset: 0, NextOffset: 1},
			}, nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackends(context.Background(), page.Params{Limit: 1})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 1, calls)
	assert.Equal(t, 1, resp.Count)
	assert.Equal(t, 1, resp.NextOffset)
}

func TestListRunnersNoArgsFetchesAllPages(t *testing.T) {
	var offsets []int
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			require.Equal(t, "ListRunners", method)
			listReq, ok := req.(*ListRunnersRequest)
			require.True(t, ok)
			offsets = append(offsets, listReq.Offset)
			if listReq.Offset == 0 {
				return &ListJobRunnersResponse{
					Runners:  []*JobRunnerInfo{{Name: "sdlc"}},
					Envelope: page.Envelope{Total: 2, Limit: 1, Offset: 0, NextOffset: 1},
				}, nil
			}
			return &ListJobRunnersResponse{
				Runners:  []*JobRunnerInfo{{Name: "review"}},
				Envelope: page.Envelope{Total: 2, Limit: 1, Offset: 1},
			}, nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListRunners(context.Background())
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, []int{0, 1}, offsets)
	assert.Equal(t, []string{"sdlc", "review"}, []string{resp.Runners[0].Name, resp.Runners[1].Name})
	assert.Equal(t, 2, resp.Count)
}

func TestGetWorkflowValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		wf, err := c.GetWorkflow(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		wf, err := c.GetWorkflow(context.Background(), &GetWorkflowRequest{Name: "wf1"})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("EmptyName", func(t *testing.T) {
		wf, err := c.GetWorkflow(context.Background(), &GetWorkflowRequest{TenantID: "t1"})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "workflow name is required")
	})
}

func TestGetWorkflowSuccess(t *testing.T) {
	want := &WorkflowDefinition{Name: "wf1", Mode: WorkflowModeAutonomous}

	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "GetWorkflow", method)
			r, ok := req.(*GetWorkflowRequest)
			require.True(t, ok)
			assert.Equal(t, "wf1", r.Name)
			return want, nil
		},
	}
	c := NewClient(mt)

	got, err := c.GetWorkflow(context.Background(), &GetWorkflowRequest{TenantID: "t1", Name: "wf1"})
	require.NoError(t, err)
	assert.Equal(t, "wf1", got.Name)
}

func TestUpdateWorkflowValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		wf, err := c.UpdateWorkflow(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		wf, err := c.UpdateWorkflow(context.Background(), &UpdateWorkflowRequest{
			Name:       "wf1",
			Definition: &WorkflowDefinition{Name: "wf1"},
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("EmptyName", func(t *testing.T) {
		wf, err := c.UpdateWorkflow(context.Background(), &UpdateWorkflowRequest{
			TenantID:   "t1",
			Definition: &WorkflowDefinition{Name: "wf1"},
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "workflow name is required")
	})

	t.Run("NilDefinition", func(t *testing.T) {
		wf, err := c.UpdateWorkflow(context.Background(), &UpdateWorkflowRequest{
			TenantID: "t1",
			Name:     "wf1",
		})
		require.Error(t, err)
		assert.Nil(t, wf)
		assert.Contains(t, err.Error(), "definition is required")
	})
}

func TestUpdateWorkflowSuccess(t *testing.T) {
	updated := &WorkflowDefinition{Name: "wf1", Mode: WorkflowModeConstrained, Description: "updated"}

	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "UpdateWorkflow", method)
			r, ok := req.(*UpdateWorkflowRequest)
			require.True(t, ok)
			assert.Equal(t, "wf1", r.Name)
			return updated, nil
		},
	}
	c := NewClient(mt)

	got, err := c.UpdateWorkflow(context.Background(), &UpdateWorkflowRequest{
		TenantID:   "t1",
		Name:       "wf1",
		Definition: updated,
	})
	require.NoError(t, err)
	assert.Equal(t, "updated", got.Description)
}

func TestDeleteWorkflowValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		err := c.DeleteWorkflow(context.Background(), nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyTenantID", func(t *testing.T) {
		err := c.DeleteWorkflow(context.Background(), &DeleteWorkflowRequest{Name: "wf1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tenant ID is required")
	})

	t.Run("EmptyName", func(t *testing.T) {
		err := c.DeleteWorkflow(context.Background(), &DeleteWorkflowRequest{TenantID: "t1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "workflow name is required")
	})

	t.Run("NilContext", func(t *testing.T) {
		var nilCtx context.Context
		err := c.DeleteWorkflow(nilCtx, &DeleteWorkflowRequest{TenantID: "t1", Name: "wf1"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "context is required")
	})
}

func TestDeleteWorkflowSuccess(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "DeleteWorkflow", method)
			r, ok := req.(*DeleteWorkflowRequest)
			require.True(t, ok)
			assert.Equal(t, "wf1", r.Name)
			return nil, nil
		},
	}
	c := NewClient(mt)

	err := c.DeleteWorkflow(context.Background(), &DeleteWorkflowRequest{TenantID: "t1", Name: "wf1"})
	require.NoError(t, err)
}

func TestDeleteWorkflowTransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewNotFound("orchestrator.DeleteWorkflow", "workflow not found")
		},
	}
	c := NewClient(mt)

	err := c.DeleteWorkflow(context.Background(), &DeleteWorkflowRequest{TenantID: "t1", Name: "no-such-wf"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// =============================================================================
// ListBackendModels
// =============================================================================

func TestListBackendModelsValidation(t *testing.T) {
	c := NewClient(&mockTransport{})

	t.Run("NilRequest", func(t *testing.T) {
		resp, err := c.ListBackendModels(context.Background(), nil)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "request is required")
	})

	t.Run("EmptyBackendName", func(t *testing.T) {
		resp, err := c.ListBackendModels(context.Background(), &ListBackendModelsRequest{NodeID: "a"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "backend name is required")
	})

	t.Run("NilContext", func(t *testing.T) {
		var nilCtx context.Context
		resp, err := c.ListBackendModels(nilCtx, &ListBackendModelsRequest{BackendName: "ollama"})
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "context is required")
	})
}

func TestListBackendModelsSuccess(t *testing.T) {
	want := &ListBackendModelsResponse{
		Backend: "ollama",
		Models:  []string{"llama3:latest", "mistral:7b"},
		Count:   2,
	}
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, serviceName, service)
			assert.Equal(t, "ListBackendModels", method)
			r, ok := req.(*ListBackendModelsRequest)
			require.True(t, ok)
			assert.Equal(t, "agent-1", r.NodeID)
			assert.Equal(t, "ollama", r.BackendName)
			return want, nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackendModels(context.Background(), &ListBackendModelsRequest{
		NodeID:      "agent-1",
		BackendName: "ollama",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, want.Models, resp.Models)
	assert.Equal(t, 2, resp.Count)
}

// TestListBackendModelsNilResponse verifies the SDK normalises a nil
// transport response into an empty model list rather than an error.
func TestListBackendModelsNilResponse(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackendModels(context.Background(), &ListBackendModelsRequest{
		BackendName: "ollama",
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "ollama", resp.Backend)
	assert.NotNil(t, resp.Models)
	assert.Empty(t, resp.Models)
}

// TestListBackendModelsNilModelsNormalised verifies the SDK normalises a
// nil Models slice (which the wire format permits) into an empty slice so
// JSON callers always see a JSON array, never null.
func TestListBackendModelsNilModelsNormalised(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return &ListBackendModelsResponse{Backend: "static", Models: nil, Count: 0}, nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackendModels(context.Background(), &ListBackendModelsRequest{
		BackendName: "static",
	})
	require.NoError(t, err)
	require.NotNil(t, resp.Models)
	assert.Empty(t, resp.Models)
}

// TestListBackendModelsTransportError verifies that a non-nil transport
// error propagates unchanged so callers can pattern-match on it.
func TestListBackendModelsTransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return nil, errors.NewInternal("test", "backend resolver unavailable")
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackendModels(context.Background(), &ListBackendModelsRequest{
		BackendName: "ollama",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "backend resolver unavailable")
}

// TestListBackendModelsInvalidResponseType verifies that an unexpected
// response type from the transport is converted to a typed Internal error.
func TestListBackendModelsInvalidResponseType(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
			return "not-a-response", nil
		},
	}
	c := NewClient(mt)

	resp, err := c.ListBackendModels(context.Background(), &ListBackendModelsRequest{
		BackendName: "ollama",
	})
	require.Error(t, err)
	assert.Nil(t, resp)
	assert.Contains(t, err.Error(), "invalid response type")
}

// =============================================================================
// DisableFleetNode (Gap 4)
// =============================================================================

func TestDisableAgent_Success(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "DisableFleetNode", method)
			r := req.(*DisableFleetNodeRequest)
			return &DisableFleetNodeResponse{
				NodeID:   r.NodeID,
				Disabled: true,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-1",
	})
	require.NoError(t, err)
	assert.True(t, resp.Disabled)
	assert.Equal(t, "agent-1", resp.NodeID)
}

func TestDisableAgent_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{NodeID: "agent-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID")
}

func TestDisableAgent_MissingAgentID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent ID")
}

func TestDisableAgent_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.DisableFleetNode(context.Background(), nil)
	require.Error(t, err)
}

func TestDisableAgent_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("test", "transport error")
		},
	}
	c := NewClient(mt)
	_, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// EnableFleetNode (Gap 4)
// =============================================================================

func TestEnableAgent_Success(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "EnableFleetNode", method)
			r := req.(*EnableFleetNodeRequest)
			return &EnableFleetNodeResponse{
				NodeID:  r.NodeID,
				Enabled: true,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.EnableFleetNode(context.Background(), &EnableFleetNodeRequest{
		TenantID: "t1",
		NodeID:   "agent-1",
	})
	require.NoError(t, err)
	assert.True(t, resp.Enabled)
}

func TestEnableAgent_MissingTenantID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.EnableFleetNode(context.Background(), &EnableFleetNodeRequest{NodeID: "agent-1"})
	require.Error(t, err)
}

func TestEnableAgent_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.EnableFleetNode(context.Background(), nil)
	require.Error(t, err)
}

// =============================================================================
// SubmitTaskArtifacts / GetTaskArtifact / ListTaskArtifacts (Gap 4)
// =============================================================================

func TestSubmitTaskArtifacts_Success(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "SubmitTaskArtifacts", method)
			r := req.(*SubmitTaskArtifactsRequest)
			return &SubmitTaskArtifactsResponse{
				TaskID: r.TaskID,
				Stored: len(r.Artifacts),
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.SubmitTaskArtifacts(context.Background(), &SubmitTaskArtifactsRequest{
		TenantID: "t1",
		TaskID:   "task-1",
		Artifacts: []*TaskArtifact{
			{Kind: "inline", Name: "output.txt", MIMEType: "text/plain", Content: []byte("hello")},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "task-1", resp.TaskID)
	assert.Equal(t, 1, resp.Stored)
}

func TestSubmitTaskArtifacts_MissingTaskID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.SubmitTaskArtifacts(context.Background(), &SubmitTaskArtifactsRequest{
		TenantID:  "t1",
		Artifacts: []*TaskArtifact{},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task ID")
}

func TestSubmitTaskArtifacts_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.SubmitTaskArtifacts(context.Background(), nil)
	require.Error(t, err)
}

func TestSubmitTaskArtifacts_TransportError(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
			return nil, errors.NewInternal("test", "store failed")
		},
	}
	c := NewClient(mt)
	_, err := c.SubmitTaskArtifacts(context.Background(), &SubmitTaskArtifactsRequest{
		TenantID:  "t1",
		TaskID:    "task-1",
		Artifacts: []*TaskArtifact{},
	})
	require.Error(t, err)
}

func TestGetTaskArtifact_Success(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "GetTaskArtifact", method)
			return &TaskArtifact{
				Kind:     "inline",
				Name:     "output.txt",
				MIMEType: "text/plain",
				Content:  []byte("result"),
			}, nil
		},
	}
	c := NewClient(mt)
	artifact, err := c.GetTaskArtifact(context.Background(), &GetTaskArtifactRequest{
		TenantID: "t1",
		TaskID:   "task-1",
		Name:     "output.txt",
	})
	require.NoError(t, err)
	assert.Equal(t, "output.txt", artifact.Name)
	assert.Equal(t, []byte("result"), artifact.Content)
}

func TestGetTaskArtifact_MissingName(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GetTaskArtifact(context.Background(), &GetTaskArtifactRequest{
		TenantID: "t1",
		TaskID:   "task-1",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "artifact name")
}

func TestGetTaskArtifact_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.GetTaskArtifact(context.Background(), nil)
	require.Error(t, err)
}

func TestListTaskArtifacts_Success(t *testing.T) {
	mt := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "ListTaskArtifacts", method)
			return &ListTaskArtifactsResponse{
				TaskID: "task-1",
				Artifacts: []*TaskArtifact{
					{Name: "a.txt"},
					{Name: "b.json"},
				},
				Count: 2,
			}, nil
		},
	}
	c := NewClient(mt)
	resp, err := c.ListTaskArtifacts(context.Background(), &ListTaskArtifactsRequest{
		TenantID: "t1",
		TaskID:   "task-1",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, resp.Count)
	assert.Len(t, resp.Artifacts, 2)
}

func TestListTaskArtifacts_MissingTaskID(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.ListTaskArtifacts(context.Background(), &ListTaskArtifactsRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListTaskArtifacts_NilRequest(t *testing.T) {
	c := NewClient(&mockTransport{})
	_, err := c.ListTaskArtifacts(context.Background(), nil)
	require.Error(t, err)
}

// =============================================================================
// CreateTaskRequest.ProjectID and StartJobRequest.LeadAgentID propagation
// =============================================================================

// TestCreateTask_ForwardsProjectID asserts the new ProjectID field on
// CreateTaskRequest reaches the transport layer unchanged. Without this,
// project-scoped task creation through the SDK silently drops project
// access enforcement.
func TestCreateTask_ForwardsProjectID(t *testing.T) {
	var captured *CreateTaskRequest
	tr := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "CreateTask", method)
			captured = req.(*CreateTaskRequest)
			return &Task{ID: "task-1"}, nil
		},
	}
	c := NewClient(tr)

	_, err := c.CreateTask(context.Background(), &CreateTaskRequest{
		TenantID:  "tenant-a",
		Subject:   "fix bug",
		ProjectID: "proj-xyz",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "proj-xyz", captured.ProjectID)
}

// TestStartJob_ForwardsLeadAgentID asserts the new LeadAgentID field on
// StartJobRequest reaches the transport layer unchanged.
func TestStartJob_ForwardsLeadAgentID(t *testing.T) {
	var captured *StartJobRequest
	tr := &mockTransport{
		requestFn: func(_ context.Context, _, method string, req interface{}) (interface{}, error) {
			assert.Equal(t, "StartJob", method)
			captured = req.(*StartJobRequest)
			return &StartJobResponse{JobID: "job-1"}, nil
		},
	}
	c := NewClient(tr)

	_, err := c.StartJob(context.Background(), &StartJobRequest{
		TenantID:    "tenant-a",
		Description: "do the thing",
		LeadAgentID: "agent-lead-1",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "agent-lead-1", captured.LeadAgentID)
}

// TestTaskArtifact_DecodesTheServerShape is the regression test for an SDK type
// that disagreed with the server in five of its seven fields.
//
// THE PAYLOAD BELOW IS VERBATIM from the burn-in node (2026-09-14), the
// outcome_comment artifact the SDLC module writes on every issue it touches:
//
//	GET /api/v1/orchestrator/tasks/<id>?tenant_id=<t>
//
// Against the old type this failed outright with "cannot unmarshal object into
// Go struct field TaskArtifact.output_artifacts.ref of type string", so
// `quicbaas orchestrator task get` could not read ANY task that had ever
// commented on an issue — which is every task that has ever run. `ref` is an
// object, `mime_type` was spelled `content_type`, `content` was spelled
// `data`, and `kind` and `metadata` had nowhere to go at all.
func TestTaskArtifact_DecodesTheServerShape(t *testing.T) {
	const payload = `{
	  "kind": "url",
	  "name": "outcome_comment",
	  "ref": {"path": "http://forge.invalid/jeremyhahn/go-luks2/issues/128"},
	  "metadata": {"comment_id": "62663", "issue": "jeremyhahn/go-luks2#128", "outcome": "readmitted"}
	}`

	var artifact TaskArtifact
	require.NoError(t, json.Unmarshal([]byte(payload), &artifact),
		"the SDK type must decode what the server actually sends")

	assert.Equal(t, "url", artifact.Kind)
	assert.Equal(t, "outcome_comment", artifact.Name)
	require.NotNil(t, artifact.Ref, "ref is an object on the wire, not a string")
	assert.Equal(t, "http://forge.invalid/jeremyhahn/go-luks2/issues/128", artifact.Ref.Path)
	assert.Equal(t, "readmitted", artifact.Metadata["outcome"])
	assert.Equal(t, "jeremyhahn/go-luks2#128", artifact.Metadata["issue"])
}

// TestTaskArtifact_DecodesInlineAndAbsentRef covers the other two shapes a real
// artifact arrives in: inline content with no ref at all, and a ref carrying
// the commit fields rather than a path.
func TestTaskArtifact_DecodesInlineAndAbsentRef(t *testing.T) {
	var inline TaskArtifact
	require.NoError(t, json.Unmarshal([]byte(
		`{"kind":"inline","name":"log.txt","mime_type":"text/plain","content":"aGVsbG8=","size":5}`), &inline))
	assert.Equal(t, []byte("hello"), inline.Content)
	assert.Equal(t, "text/plain", inline.MIMEType)
	assert.Equal(t, int64(5), inline.Size)
	assert.Nil(t, inline.Ref, "an inline artifact carries no ref")

	var commit TaskArtifact
	require.NoError(t, json.Unmarshal([]byte(
		`{"kind":"commit","name":"head","ref":{"sha":"deadbeef","branch":"issue/128-x"}}`), &commit))
	require.NotNil(t, commit.Ref)
	assert.Equal(t, "deadbeef", commit.Ref.SHA)
	assert.Equal(t, "issue/128-x", commit.Ref.Branch)
	assert.Empty(t, commit.Ref.Path)
}
