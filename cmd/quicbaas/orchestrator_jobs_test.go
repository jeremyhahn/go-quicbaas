// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	orchestratorSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/orchestrator"
)

// These tests exercise the orchestrator job run-funcs (list/status/events/
// cancel/decompose), which were previously uncovered (0%). They drive the
// factory error path, the SDK error path, and the success path through the
// configurable mock OrchestratorClient.

func TestRunOrchJobList_Success(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobListCommand()
	orchOutputFmt = "json"
	setMockOrchestratorFactory(&mockOrchestratorClient{})

	require.NoError(t, runOrchJobList(cmd, nil))
}

func TestRunOrchJobList_FactoryError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobListCommand()
	setErrorOrchestratorFactory()

	require.Error(t, runOrchJobList(cmd, nil))
}

func TestRunOrchJobStatus_Success(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobStatusCommand()
	orchOutputFmt = "json"
	setMockOrchestratorFactory(&mockOrchestratorClient{
		getJobStatusFn: func(_ context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			assert.Equal(t, "job_42", req.JobID)
			return &orchestratorSDK.JobStatus{JobID: req.JobID, State: "running"}, nil
		},
	})

	require.NoError(t, runOrchJobStatus(cmd, []string{"job_42"}))
}

func TestRunOrchJobStatus_FactoryError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobStatusCommand()
	setErrorOrchestratorFactory()

	require.Error(t, runOrchJobStatus(cmd, []string{"job_42"}))
}

func TestRunOrchJobStatus_SDKError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobStatusCommand()
	setMockOrchestratorFactory(&mockOrchestratorClient{
		getJobStatusFn: func(_ context.Context, _ *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			return nil, errors.New(errors.ErrNotFound, "orchestrator", "no such job")
		},
	})

	require.Error(t, runOrchJobStatus(cmd, []string{"missing"}))
}

func TestRunOrchJobEvents_JSONShortcut(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobEventsCommand()
	orchOutputFmt = "json"
	setMockOrchestratorFactory(&mockOrchestratorClient{
		getJobStatusFn: func(_ context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			return &orchestratorSDK.JobStatus{JobID: req.JobID}, nil
		},
	})

	require.NoError(t, runOrchJobEvents(cmd, []string{"job_42"}))
}

func TestRunOrchJobEvents_NoEvents(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobEventsCommand()
	orchOutputFmt = "table"
	setMockOrchestratorFactory(&mockOrchestratorClient{
		getJobStatusFn: func(_ context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			return &orchestratorSDK.JobStatus{JobID: req.JobID}, nil
		},
	})

	require.NoError(t, runOrchJobEvents(cmd, []string{"job_42"}))
}

func TestRunOrchJobEvents_RendersFilteredEvents(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobEventsCommand()
	orchOutputFmt = "table"
	orchVerbosity = "debug"
	setMockOrchestratorFactory(&mockOrchestratorClient{
		getJobStatusFn: func(_ context.Context, req *orchestratorSDK.GetJobStatusRequest) (*orchestratorSDK.JobStatus, error) {
			return &orchestratorSDK.JobStatus{
				JobID: req.JobID,
				Events: []orchestratorSDK.JobAuditEvent{
					{Timestamp: 1, Type: "orchestration:start", Details: "begin"},
					{Timestamp: 2, Type: "agent:assistant", Details: "thinking"},
				},
			}, nil
		},
	})

	require.NoError(t, runOrchJobEvents(cmd, []string{"job_42"}))
}

func TestRunOrchJobEvents_FactoryError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobEventsCommand()
	setErrorOrchestratorFactory()

	require.Error(t, runOrchJobEvents(cmd, []string{"job_42"}))
}

func TestRunOrchJobCancel_Success(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobCancelCommand()
	orchOutputFmt = "json"
	setMockOrchestratorFactory(&mockOrchestratorClient{
		cancelJobFn: func(_ context.Context, req *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error) {
			return &orchestratorSDK.CancelJobResponse{JobID: req.JobID, Cancelled: true}, nil
		},
	})

	require.NoError(t, runOrchJobCancel(cmd, []string{"job_42"}))
}

func TestRunOrchJobCancel_FactoryError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobCancelCommand()
	setErrorOrchestratorFactory()

	require.Error(t, runOrchJobCancel(cmd, []string{"job_42"}))
}

func TestRunOrchJobCancel_SDKError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchJobCancelCommand()
	setMockOrchestratorFactory(&mockOrchestratorClient{
		cancelJobFn: func(_ context.Context, _ *orchestratorSDK.CancelJobRequest) (*orchestratorSDK.CancelJobResponse, error) {
			return nil, errors.New(errors.ErrInternal, "orchestrator", "cancel failed")
		},
	})

	require.Error(t, runOrchJobCancel(cmd, []string{"job_42"}))
}

func TestRunOrchDecompose_Success(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	// Construct the command first; its flag binding resets orchProjectID.
	cmd := newOrchDecomposeCommand()
	orchOutputFmt = "json"
	orchProjectID = "proj-1"
	setMockOrchestratorFactory(&mockOrchestratorClient{
		decomposeTaskFn: func(_ context.Context, req *orchestratorSDK.DecomposeTaskRequest) (*orchestratorSDK.DecomposeTaskResponse, error) {
			assert.Equal(t, "build a feature", req.Task)
			assert.Equal(t, []string{"proj-1"}, req.ProjectIDs)
			return &orchestratorSDK.DecomposeTaskResponse{WorkItems: []*orchestratorSDK.WorkItem{}}, nil
		},
	})

	require.NoError(t, runOrchDecompose(cmd, []string{"build a feature"}))
}

func TestRunOrchDecompose_FactoryError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchDecomposeCommand()
	setErrorOrchestratorFactory()

	require.Error(t, runOrchDecompose(cmd, []string{"build a feature"}))
}

func TestRunOrchDecompose_SDKError(t *testing.T) {
	saveAndRestoreOrchestratorState(t)
	cmd := newOrchDecomposeCommand()
	orchProjectID = ""
	setMockOrchestratorFactory(&mockOrchestratorClient{
		decomposeTaskFn: func(_ context.Context, _ *orchestratorSDK.DecomposeTaskRequest) (*orchestratorSDK.DecomposeTaskResponse, error) {
			return nil, errors.New(errors.ErrInternal, "orchestrator", "decompose failed")
		},
	})

	require.Error(t, runOrchDecompose(cmd, []string{"build a feature"}))
}
