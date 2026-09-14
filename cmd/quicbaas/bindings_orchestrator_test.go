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

//go:build desktop

package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// newTestOrchestratorBinding constructs an OrchestratorBinding wired to a
// mockOrchestratorClient through the same ConnectionBinding adapter used
// by the session binding tests.
func newTestOrchestratorBinding(t *testing.T, mock *mockOrchestratorClient) *OrchestratorBinding {
	t.Helper()

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)

	conn := NewConnectionBinding(log)
	var client quicbaas.Client = &sessionBindingClient{orch: mock}
	conn.client.Store(&client)

	binding := NewOrchestratorBinding(log, conn)
	binding.ctx = context.Background()
	return binding
}

// -----------------------------------------------------------------------------
// ListJobs
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListJobs_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{}
	binding := newTestOrchestratorBinding(t, mock)

	// Default ListJobs returns an empty list; we only assert the binding
	// returns a non-nil slice and forwards the tenant ID into the SDK call.
	result, err := binding.ListJobs(&orchestratorSDK.ListJobsRequest{TenantID: "acme"})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestOrchestratorBinding_ListJobs_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.ListJobs(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// GetJobStatus
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_GetJobStatus_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		getJobStatusFn: func(ctx context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "job-1", req.JobID)
			return &orchestratorSDK.JobStatus{JobID: "job-1", State: "running", Iteration: 3}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.GetJobStatus(&orchestratorSDK.GetJobStatusRequest{TenantID: "acme", JobID: "job-1"})
	require.NoError(t, err)
	assert.Equal(t, "job-1", result.JobID)
	assert.Equal(t, "running", result.State)
	assert.Equal(t, 3, result.Iteration)
}

// TestOrchestratorBinding_GetJobStatus_CarriesProvenance: the binding used to
// hand-build a ten-key map, so every field the job page was built for —
// runner, task id, agent, stages, attempts, terminal reason, readmit state,
// delivery, verification — was dropped on the way to the desktop UI. It
// returns the SDK status now, so the projection cannot go stale again.
func TestOrchestratorBinding_GetJobStatus_CarriesProvenance(t *testing.T) {
	full := &orchestratorSDK.JobStatus{
		JobID:          "job-3",
		State:          "failed",
		Runner:         "sdlc",
		TaskID:         "task-26",
		Attempt:        3,
		AgentID:        "agent-worker-04",
		Stages:         []orchestratorSDK.JobStageRecord{{Stage: "prepare", EnteredAt: 1}},
		Attempts:       []orchestratorSDK.JobAttemptRecord{{Attempt: 3, JobID: "job-3", Current: true}},
		AttemptBudget:  3,
		TerminalReason: "budget_exhausted",
		ReadmitAllowed: true,
		Delivery: &orchestratorSDK.JobDelivery{
			Branch:            "issue/26",
			PullRequestNumber: 57,
			Reviews:           []orchestratorSDK.JobReviewRound{{Round: 1, Verdict: "approve"}},
		},
		Verification: []orchestratorSDK.JobVerificationResult{{Name: "make test", Passed: false}},
	}
	mock := &mockOrchestratorClient{
		getJobStatusFn: func(context.Context, *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			return full, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.GetJobStatus(&orchestratorSDK.GetJobStatusRequest{TenantID: "acme", JobID: "job-3"})
	require.NoError(t, err)
	assert.Equal(t, "sdlc", result.Runner)
	assert.Equal(t, "task-26", result.TaskID)
	assert.Equal(t, "agent-worker-04", result.AgentID)
	assert.Len(t, result.Stages, 1)
	assert.Len(t, result.Attempts, 1)
	assert.Equal(t, 3, result.AttemptBudget)
	assert.Equal(t, "budget_exhausted", result.TerminalReason)
	assert.True(t, result.ReadmitAllowed)
	require.NotNil(t, result.Delivery)
	assert.Equal(t, 57, result.Delivery.PullRequestNumber)
	assert.Len(t, result.Delivery.Reviews, 1)
	assert.Len(t, result.Verification, 1)
}

func TestOrchestratorBinding_GetJobStatus_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.GetJobStatus(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// StartJob
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_StartJob_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "build the db", req.Description)
			assert.Equal(t, "claude", req.LeadBackend)
			assert.Equal(t, "sonnet", req.LeadModel)
			return &orchestratorSDK.StartJobResponse{JobID: "job-99", Timestamp: 42}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.StartJob(&orchestratorSDK.StartJobRequest{
		TenantID:    "acme",
		Description: "build the db",
		LeadBackend: "claude",
		LeadModel:   "sonnet",
	})
	require.NoError(t, err)
	assert.Equal(t, "job-99", result["job_id"])
	assert.Equal(t, int64(42), result["timestamp"])
}

func TestOrchestratorBinding_StartJob_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.StartJob(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestOrchestratorBinding_StartJob_TransportError(t *testing.T) {
	sentinel := errors.New("start failed")
	mock := &mockOrchestratorClient{
		startJobFn: func(ctx context.Context, req *orchestratorSDK.StartJobRequest) (*orchestratorSDK.StartJobResponse, error) {
			return nil, sentinel
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	_, err := binding.StartJob(&orchestratorSDK.StartJobRequest{Description: "x"})
	require.Error(t, err)

	var qberr *qberrors.QBError
	require.ErrorAs(t, err, &qberr)
	assert.Equal(t, "OrchestratorBinding.StartJob", qberr.Op)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// CancelJob
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_CancelJob_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		cancelJobFn: func(ctx context.Context, req *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "job-x", req.JobID)
			return &orchestratorSDK.CancelJobResponse{JobID: req.JobID, Cancelled: true}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	err := binding.CancelJob(&orchestratorSDK.CancelJobRequest{TenantID: "acme", JobID: "job-x"})
	require.NoError(t, err)
}

func TestOrchestratorBinding_CancelJob_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	err := binding.CancelJob(nil)
	require.Error(t, err)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// ReadmitTask — the job page's Readmit action in desktop mode.
//
// The TypeScript side called `OrchestratorBinding.ReadmitTask` before this
// method existed, so every click threw "ReadmitTask not implemented"
// (go-quicbaas#796 review). Like every other task verb here it takes the SDK
// request struct, which is the Wails wire format.
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ReadmitTask_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		readmitTaskFn: func(_ context.Context, req *orchestratorSDK.ReadmitTaskRequest) (*orchestratorSDK.ReadmitTaskResponse, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "task-26", req.TaskID)
			return &orchestratorSDK.ReadmitTaskResponse{TaskID: req.TaskID, Readmitted: true}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	err := binding.ReadmitTask(&orchestratorSDK.ReadmitTaskRequest{TenantID: "acme", TaskID: "task-26"})
	require.NoError(t, err)
}

func TestOrchestratorBinding_ReadmitTask_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	err := binding.ReadmitTask(nil)
	require.Error(t, err)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestOrchestratorBinding_ReadmitTask_TransportError(t *testing.T) {
	sentinel := errors.New("the task is failed, not parked")
	mock := &mockOrchestratorClient{
		readmitTaskFn: func(context.Context, *orchestratorSDK.ReadmitTaskRequest) (*orchestratorSDK.ReadmitTaskResponse, error) {
			return nil, sentinel
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	err := binding.ReadmitTask(&orchestratorSDK.ReadmitTaskRequest{TenantID: "acme", TaskID: "task-26"})
	require.Error(t, err)

	var qberr *qberrors.QBError
	require.ErrorAs(t, err, &qberr)
	assert.Equal(t, "OrchestratorBinding.ReadmitTask", qberr.Op)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// ListAgents — the direct cause of the "json: cannot unmarshal object into
// Go value of type string" runtime error. The regression test verifies that
// the binding takes the SDK request struct (matching the Wails wire format)
// rather than a bare string.
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListAgents_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listAgentsFn: func(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			assert.Equal(t, "default", req.TenantID)
			return &orchestratorSDK.ListAgentsResponse{
				Agents: []*orchestratorSDK.AgentRegistration{
					{ID: "a1", Name: "alpha", Status: "running"},
				},
				Count: 1,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.ListAgents(&orchestratorSDK.ListAgentsRequest{TenantID: "default"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "a1", result[0]["id"])
}

// TestOrchestratorBinding_ListAgents_WailsPayloadShape simulates the exact
// JSON body Wails delivers when the frontend calls
// orchestratorBinding().ListAgents({tenant_id: "default"}). This is the
// regression test for the runtime error
// "json: cannot unmarshal object into Go value of type string" — the binding
// now accepts a *ListAgentsRequest so the object unmarshals successfully.
func TestOrchestratorBinding_ListAgents_WailsPayloadShape(t *testing.T) {
	payload := []byte(`{"tenant_id":"default","status":"running"}`)
	var req orchestratorSDK.ListAgentsRequest
	require.NoError(t, json.Unmarshal(payload, &req))

	mock := &mockOrchestratorClient{
		listAgentsFn: func(ctx context.Context, got *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			assert.Equal(t, "default", got.TenantID)
			assert.Equal(t, "running", got.Status)
			return &orchestratorSDK.ListAgentsResponse{Agents: []*orchestratorSDK.AgentRegistration{}, Count: 0}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	_, err := binding.ListAgents(&req)
	require.NoError(t, err)
}

func TestOrchestratorBinding_ListAgents_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.ListAgents(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestOrchestratorBinding_ListAgents_TransportError(t *testing.T) {
	sentinel := errors.New("agents unavailable")
	mock := &mockOrchestratorClient{
		listAgentsFn: func(ctx context.Context, req *orchestratorSDK.ListAgentsRequest) (*orchestratorSDK.ListAgentsResponse, error) {
			return nil, sentinel
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	_, err := binding.ListAgents(&orchestratorSDK.ListAgentsRequest{TenantID: "t"})
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

// -----------------------------------------------------------------------------
// GetAgent
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_GetAgent_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		getAgentFn: func(ctx context.Context, req *orchestratorSDK.GetAgentRequest) (*orchestratorSDK.AgentRegistration, error) {
			assert.Equal(t, "t", req.TenantID)
			assert.Equal(t, "a-1", req.AgentID)
			return &orchestratorSDK.AgentRegistration{ID: "a-1", Name: "alpha"}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.GetAgent(&orchestratorSDK.GetAgentRequest{TenantID: "t", AgentID: "a-1"})
	require.NoError(t, err)
	assert.Equal(t, "a-1", result["id"])
	assert.Equal(t, "alpha", result["name"])
}

func TestOrchestratorBinding_GetAgent_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.GetAgent(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// ListSessions — Bug #5 made tenantID required in the request. The Wails
// binding now accepts the full request struct so the tenantID flows through
// to the SDK.
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListSessions_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listSessionsFn: func(ctx context.Context, req *orchestratorSDK.ListSessionsRequest) (*orchestratorSDK.ListSessionsResponse, error) {
			assert.Equal(t, "claude", req.Backend)
			return &orchestratorSDK.ListSessionsResponse{
				Sessions: []*orchestratorSDK.SessionStatusResponse{
					{SessionID: "s1", Backend: "claude", Status: "active"},
				},
				Count: 1,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.ListSessions(&orchestratorSDK.ListSessionsRequest{Backend: "claude"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "s1", result[0]["session_id"])
}

func TestOrchestratorBinding_ListSessions_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.ListSessions(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// ListTasks / GetTask
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListTasks_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listTasksFn: func(ctx context.Context, req *orchestratorSDK.ListTasksRequest) (*orchestratorSDK.ListTasksResponse, error) {
			assert.Equal(t, "acme", req.TenantID)
			return &orchestratorSDK.ListTasksResponse{
				Tasks: []*orchestratorSDK.Task{{ID: "t1", Subject: "s1", Status: "pending"}},
				Count: 1,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.ListTasks(&orchestratorSDK.ListTasksRequest{TenantID: "acme"})
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "t1", result[0]["id"])
}

func TestOrchestratorBinding_ListTasks_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.ListTasks(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

func TestOrchestratorBinding_GetTask_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		getTaskFn: func(ctx context.Context, req *orchestratorSDK.GetTaskRequest) (*orchestratorSDK.Task, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "t-1", req.TaskID)
			return &orchestratorSDK.Task{ID: "t-1", Subject: "s"}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.GetTask(&orchestratorSDK.GetTaskRequest{TenantID: "acme", TaskID: "t-1"})
	require.NoError(t, err)
	assert.Equal(t, "t-1", result["id"])
}

func TestOrchestratorBinding_GetTask_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	result, err := binding.GetTask(nil)
	require.Error(t, err)
	assert.Nil(t, result)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// ListBackends / HealthCheck (parameterless)
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListBackends_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listBackendsFn: func(ctx context.Context) (*orchestratorSDK.ListBackendsResponse, error) {
			return &orchestratorSDK.ListBackendsResponse{
				Backends: []*orchestratorSDK.BackendInfo{
					{Name: "claude", Type: "agent", Status: "available", Available: true},
				},
				Count: 1,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.ListBackends()
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "claude", result[0]["name"])
}

// -----------------------------------------------------------------------------
// ListRunners
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListRunners_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		listRunnersFn: func(ctx context.Context) (*orchestratorSDK.ListJobRunnersResponse, error) {
			return &orchestratorSDK.ListJobRunnersResponse{
				Runners: []*orchestratorSDK.JobRunnerInfo{
					{
						Name: "workflow",
						States: []orchestratorSDK.JobStateSpec{
							{State: "initializing", Label: "Initializing"},
							{State: "executing", Label: "Executing", SessionBacked: true},
						},
					},
				},
				Count: 1,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.ListRunners()
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "workflow", result[0]["name"])

	states, ok := result[0]["states"].([]map[string]interface{})
	require.True(t, ok, "states marshal as a list of objects for the GUI")
	require.Len(t, states, 2)
	assert.Equal(t, "initializing", states[0]["state"])
	assert.Equal(t, "Initializing", states[0]["label"])
	assert.Equal(t, false, states[0]["session_backed"])
	assert.Equal(t, true, states[1]["session_backed"])
}

func TestOrchestratorBinding_ListRunners_TransportError(t *testing.T) {
	mock := &mockOrchestratorClient{
		listRunnersFn: func(ctx context.Context) (*orchestratorSDK.ListJobRunnersResponse, error) {
			return nil, errors.New("orchestrator unreachable")
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.ListRunners()
	require.Error(t, err)
	assert.Nil(t, result)
}

// -----------------------------------------------------------------------------
// ListBackendModels
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_ListBackendModels_HappyPath(t *testing.T) {
	wantModels := []string{"llama3:latest", "mistral:7b"}
	mock := &mockOrchestratorClient{
		listBackendModelsFn: func(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error) {
			require.NotNil(t, req)
			assert.Equal(t, "agent-1", req.AgentID)
			assert.Equal(t, "ollama", req.BackendName)
			return &orchestratorSDK.ListBackendModelsResponse{
				Backend: req.BackendName,
				Models:  wantModels,
				Count:   len(wantModels),
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	models, err := binding.ListBackendModels(&orchestratorSDK.ListBackendModelsRequest{
		AgentID:     "agent-1",
		BackendName: "ollama",
	})
	require.NoError(t, err)
	assert.Equal(t, wantModels, models)
}

// TestOrchestratorBinding_ListBackendModels_EmptyAgent verifies that when no
// agent matches the request the binding returns an empty slice and a nil
// error rather than a transport error. This mirrors the REST contract where
// an agent that has not registered yields an empty model list.
func TestOrchestratorBinding_ListBackendModels_EmptyAgent(t *testing.T) {
	mock := &mockOrchestratorClient{
		listBackendModelsFn: func(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error) {
			return &orchestratorSDK.ListBackendModelsResponse{
				Backend: req.BackendName,
				Models:  []string{},
				Count:   0,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	models, err := binding.ListBackendModels(&orchestratorSDK.ListBackendModelsRequest{
		AgentID:     "missing-agent",
		BackendName: "ollama",
	})
	require.NoError(t, err)
	require.NotNil(t, models)
	assert.Empty(t, models)
}

// TestOrchestratorBinding_ListBackendModels_NonListerBackend verifies that a
// backend without a ModelLister implementation surfaces as an empty slice +
// nil error. This is the same shape the orchestrator service emits for
// non-lister backends; the binding must not promote it to an error.
func TestOrchestratorBinding_ListBackendModels_NonListerBackend(t *testing.T) {
	mock := &mockOrchestratorClient{
		listBackendModelsFn: func(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error) {
			// Service returns nil Models (rather than empty slice) when the
			// backend exposes no model lister; the binding must normalise to [].
			return &orchestratorSDK.ListBackendModelsResponse{
				Backend: req.BackendName,
				Models:  nil,
				Count:   0,
			}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	models, err := binding.ListBackendModels(&orchestratorSDK.ListBackendModelsRequest{
		BackendName: "static-backend",
	})
	require.NoError(t, err)
	require.NotNil(t, models)
	assert.Empty(t, models)
}

// TestOrchestratorBinding_ListBackendModels_NilRequest verifies that a nil
// request returns the canonical GUIRequestRequired typed error.
func TestOrchestratorBinding_ListBackendModels_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	models, err := binding.ListBackendModels(nil)
	require.Error(t, err)
	assert.Nil(t, models)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// TestOrchestratorBinding_ListBackendModels_TransportError verifies that
// errors from the underlying SDK client are wrapped in a typed
// GUITransportError so the frontend can distinguish connectivity failures
// from validation failures.
func TestOrchestratorBinding_ListBackendModels_TransportError(t *testing.T) {
	sentinel := errors.New("backend unreachable")
	mock := &mockOrchestratorClient{
		listBackendModelsFn: func(ctx context.Context, req *orchestratorSDK.ListBackendModelsRequest) (*orchestratorSDK.ListBackendModelsResponse, error) {
			return nil, sentinel
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	models, err := binding.ListBackendModels(&orchestratorSDK.ListBackendModelsRequest{
		BackendName: "ollama",
	})
	require.Error(t, err)
	assert.Nil(t, models)

	var qberr *qberrors.QBError
	require.ErrorAs(t, err, &qberr)
	assert.Equal(t, "OrchestratorBinding.ListBackendModels", qberr.Op)
	assert.ErrorIs(t, err, sentinel)
}

func TestOrchestratorBinding_HealthCheck_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		healthFn: func(ctx context.Context) (*orchestratorSDK.HealthCheckReport, error) {
			return &orchestratorSDK.HealthCheckReport{Healthy: true, Status: "ok", Timestamp: 123}, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)

	result, err := binding.HealthCheck()
	require.NoError(t, err)
	assert.Equal(t, true, result["healthy"])
	assert.Equal(t, "ok", result["status"])
}

// -----------------------------------------------------------------------------
// SubscribeJobEvents
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_SubscribeJobEvents_HappyPath(t *testing.T) {
	mock := &mockOrchestratorClient{
		subscribeJobEventsFn: func(ctx context.Context, req *orchestratorSDK.SubscribeJobEventsRequest) (<-chan *orchestratorSDK.JobEvent, error) {
			assert.Equal(t, "acme", req.TenantID)
			assert.Equal(t, "job-1", req.JobID)
			ch := make(chan *orchestratorSDK.JobEvent)
			go func() {
				<-ctx.Done()
				close(ch)
			}()
			return ch, nil
		},
	}
	binding := newTestOrchestratorBinding(t, mock)
	ctx, cancel := context.WithCancel(context.Background())
	binding.ctx = ctx
	defer cancel()

	err := binding.SubscribeJobEvents(&orchestratorSDK.SubscribeJobEventsRequest{
		TenantID: "acme",
		JobID:    "job-1",
	})
	require.NoError(t, err)
}

func TestOrchestratorBinding_SubscribeJobEvents_NilRequest(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})

	err := binding.SubscribeJobEvents(nil)
	require.Error(t, err)

	var typed *GUIRequestRequiredError
	assert.ErrorAs(t, err, &typed)
}

// -----------------------------------------------------------------------------
// Lifecycle
// -----------------------------------------------------------------------------

func TestOrchestratorBinding_Startup_StoresContext(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "console", Output: "stderr"})
	require.NoError(t, err)
	conn := NewConnectionBinding(log)
	binding := NewOrchestratorBinding(log, conn)

	ctx := context.Background()
	binding.startup(ctx)
	assert.Equal(t, ctx, binding.ctx)
}

func TestOrchestratorBinding_Shutdown_Noop(t *testing.T) {
	binding := newTestOrchestratorBinding(t, &mockOrchestratorClient{})
	binding.shutdown(context.Background())
}
