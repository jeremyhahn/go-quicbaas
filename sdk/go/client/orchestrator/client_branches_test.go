// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package orchestrator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// branchCall invokes one Client method with a request that passes validation,
// so each table below reaches the branch it names.
type branchCall func(ctx context.Context, c Client) (interface{}, error)

var branchCalls = map[string]branchCall{
	"CreateTask": func(ctx context.Context, c Client) (interface{}, error) {
		return c.CreateTask(ctx, &CreateTaskRequest{TenantID: "t1", Subject: "s"})
	},
	"CreateWorkflow": func(ctx context.Context, c Client) (interface{}, error) {
		return c.CreateWorkflow(ctx, &CreateWorkflowRequest{TenantID: "t1", Definition: &WorkflowDefinition{Name: "wf"}})
	},
	"ListWorkflows": func(ctx context.Context, c Client) (interface{}, error) {
		return c.ListWorkflows(ctx, &ListWorkflowsRequest{TenantID: "t1"})
	},
	"GetWorkflow": func(ctx context.Context, c Client) (interface{}, error) {
		return c.GetWorkflow(ctx, &GetWorkflowRequest{TenantID: "t1", Name: "wf"})
	},
	"UpdateWorkflow": func(ctx context.Context, c Client) (interface{}, error) {
		return c.UpdateWorkflow(ctx, &UpdateWorkflowRequest{TenantID: "t1", Name: "wf", Definition: &WorkflowDefinition{Name: "wf"}})
	},
	"DisableFleetNode": func(ctx context.Context, c Client) (interface{}, error) {
		return c.DisableFleetNode(ctx, &DisableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	},
	"EnableFleetNode": func(ctx context.Context, c Client) (interface{}, error) {
		return c.EnableFleetNode(ctx, &EnableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	},
	"SubmitTaskArtifacts": func(ctx context.Context, c Client) (interface{}, error) {
		return c.SubmitTaskArtifacts(ctx, &SubmitTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	},
	"GetTaskArtifact": func(ctx context.Context, c Client) (interface{}, error) {
		return c.GetTaskArtifact(ctx, &GetTaskArtifactRequest{TenantID: "t1", TaskID: "tk1", Name: "art"})
	},
	"ListTaskArtifacts": func(ctx context.Context, c Client) (interface{}, error) {
		return c.ListTaskArtifacts(ctx, &ListTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	},
}

// TestClientBranches_NilContext checks that a nil context is rejected as invalid
// input before the transport is reached (the failing transport would
// otherwise surface an internal error).
func TestClientBranches_NilContext(t *testing.T) {
	for _, method := range []string{
		"CreateTask", "CreateWorkflow", "ListWorkflows", "GetWorkflow", "UpdateWorkflow",
		"DisableFleetNode", "EnableFleetNode", "SubmitTaskArtifacts", "GetTaskArtifact", "ListTaskArtifacts",
	} {
		t.Run(method, func(t *testing.T) {
			var nilCtx context.Context
			_, err := branchCalls[method](nilCtx, NewClient(failTransport()))
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "got %v", err)
			assert.Contains(t, err.Error(), "context is required")
		})
	}
}

// TestClientBranches_TransportError checks that a transport failure is returned
// to the caller unchanged.
func TestClientBranches_TransportError(t *testing.T) {
	for _, method := range []string{
		"CreateTask", "ListWorkflows", "GetWorkflow", "UpdateWorkflow",
		"EnableFleetNode", "GetTaskArtifact", "ListTaskArtifacts",
	} {
		t.Run(method, func(t *testing.T) {
			_, err := branchCalls[method](context.Background(), NewClient(failTransport()))
			require.Error(t, err)
			assert.Equal(t, errTransport().Error(), err.Error())
		})
	}
}

// TestClientBranches_NilResponse checks the (nil, nil) transport result: every
// method reports an internal error except ListTaskArtifacts, which returns
// an empty listing for the requested task.
func TestClientBranches_NilResponse(t *testing.T) {
	for _, method := range []string{
		"CreateTask", "CreateWorkflow", "GetWorkflow", "UpdateWorkflow",
		"DisableFleetNode", "EnableFleetNode", "SubmitTaskArtifacts", "GetTaskArtifact",
	} {
		t.Run(method, func(t *testing.T) {
			_, err := branchCalls[method](context.Background(), NewClient(nilTransport()))
			require.Error(t, err)
			assert.Equal(t, errors.CodeInternal, errors.Code(err), "got %v", err)
		})
	}

	t.Run("ListTaskArtifacts", func(t *testing.T) {
		got, err := branchCalls["ListTaskArtifacts"](context.Background(), NewClient(nilTransport()))
		require.NoError(t, err)
		resp, ok := got.(*ListTaskArtifactsResponse)
		require.True(t, ok)
		require.NotNil(t, resp)
		assert.Equal(t, 0, resp.Count)
		assert.NotNil(t, resp.Artifacts)
		assert.Equal(t, "tk1", resp.TaskID)
	})
}

// TestClientBranches_TypeMismatch checks that a response of the wrong concrete type
// is reported as an internal error.
func TestClientBranches_TypeMismatch(t *testing.T) {
	for _, method := range []string{
		"CreateTask", "CreateWorkflow", "ListWorkflows", "GetWorkflow", "UpdateWorkflow",
		"DisableFleetNode", "EnableFleetNode", "SubmitTaskArtifacts", "GetTaskArtifact", "ListTaskArtifacts",
	} {
		t.Run(method, func(t *testing.T) {
			_, err := branchCalls[method](context.Background(), NewClient(mismatchTransport()))
			require.Error(t, err)
			assert.Equal(t, errors.CodeInternal, errors.Code(err), "got %v", err)
			assert.Contains(t, err.Error(), "invalid response type")
		})
	}
}

// TestClient_RequestValidation checks that an invalid request is rejected
// as invalid input before the transport is reached.
func TestClient_RequestValidation(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		call    func(c Client) error
		wantMsg string
	}{
		{"CreateTask/NilRequest", func(c Client) error {
			_, err := c.CreateTask(ctx, nil)
			return err
		}, ""},
		{"CreateTask/MissingTenant", func(c Client) error {
			_, err := c.CreateTask(ctx, &CreateTaskRequest{Subject: "s"})
			return err
		}, "tenant ID is required"},
		{"CreateTask/MissingSubject", func(c Client) error {
			_, err := c.CreateTask(ctx, &CreateTaskRequest{TenantID: "t1"})
			return err
		}, "subject is required"},
		{"CreateWorkflow/MissingDefinition", func(c Client) error {
			_, err := c.CreateWorkflow(ctx, &CreateWorkflowRequest{TenantID: "t1"})
			return err
		}, "definition is required"},
		{"DisableFleetNode/MissingAgentID", func(c Client) error {
			_, err := c.DisableFleetNode(ctx, &DisableFleetNodeRequest{TenantID: "t1"})
			return err
		}, "agent ID is required"},
		{"EnableFleetNode/MissingAgentID", func(c Client) error {
			_, err := c.EnableFleetNode(ctx, &EnableFleetNodeRequest{TenantID: "t1"})
			return err
		}, "agent ID is required"},
		{"SubmitTaskArtifacts/MissingTenant", func(c Client) error {
			_, err := c.SubmitTaskArtifacts(ctx, &SubmitTaskArtifactsRequest{TaskID: "tk1"})
			return err
		}, "tenant ID is required"},
		{"GetTaskArtifact/MissingTenant", func(c Client) error {
			_, err := c.GetTaskArtifact(ctx, &GetTaskArtifactRequest{TaskID: "tk1", Name: "art"})
			return err
		}, "tenant ID is required"},
		{"GetTaskArtifact/MissingTask", func(c Client) error {
			_, err := c.GetTaskArtifact(ctx, &GetTaskArtifactRequest{TenantID: "t1", Name: "art"})
			return err
		}, "task ID is required"},
		{"ListTaskArtifacts/MissingTenant", func(c Client) error {
			_, err := c.ListTaskArtifacts(ctx, &ListTaskArtifactsRequest{TaskID: "tk1"})
			return err
		}, "tenant ID is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call(NewClient(failTransport()))
			require.Error(t, err)
			assert.True(t, errors.IsInvalidInput(err), "got %v", err)
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}
}
