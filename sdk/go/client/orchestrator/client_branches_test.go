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
)

// =============================================================================
// CreateTask: missing subject-validation, nil-context, nil-response,
// type-mismatch and transport-error branches.
// =============================================================================

func TestCreateTask_HappyPath(t *testing.T) {
	c := NewClient(okTransport(t, "CreateTask", &Task{}))
	resp, err := c.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "t1", Subject: "s"})
	require.NoError(t, err)
	require.NotNil(t, resp)
}

func TestCreateTask_MissingSubject(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subject is required")
}

func TestCreateTask_MissingTenant(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CreateTask(context.Background(), &CreateTaskRequest{Subject: "s"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
}

func TestCreateTask_NilRequest(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CreateTask(context.Background(), nil)
	require.Error(t, err)
}

func TestCreateTask_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "CreateTask", &Task{}))
	var nilCtx context.Context
	_, err := c.CreateTask(nilCtx, &CreateTaskRequest{TenantID: "t1", Subject: "s"})
	require.Error(t, err)
}

func TestCreateTask_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "t1", Subject: "s"})
	require.Error(t, err)
}

func TestCreateTask_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "t1", Subject: "s"})
	require.Error(t, err)
}

func TestCreateTask_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.CreateTask(context.Background(), &CreateTaskRequest{TenantID: "t1", Subject: "s"})
	require.Error(t, err)
}

// =============================================================================
// CreateWorkflow: nil-context, nil-response, type-mismatch + extra validation.
// =============================================================================

func validWorkflowDef() *WorkflowDefinition {
	return &WorkflowDefinition{Name: "wf"}
}

func TestCreateWorkflow_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "CreateWorkflow", validWorkflowDef()))
	var nilCtx context.Context
	_, err := c.CreateWorkflow(nilCtx, &CreateWorkflowRequest{TenantID: "t1", Definition: validWorkflowDef()})
	require.Error(t, err)
}

func TestCreateWorkflow_MissingDefinition(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "definition is required")
}

func TestCreateWorkflow_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{TenantID: "t1", Definition: validWorkflowDef()})
	require.Error(t, err)
}

func TestCreateWorkflow_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.CreateWorkflow(context.Background(), &CreateWorkflowRequest{TenantID: "t1", Definition: validWorkflowDef()})
	require.Error(t, err)
}

// =============================================================================
// ListWorkflows: nil-context, transport-error, type-mismatch.
// =============================================================================

func TestListWorkflows_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListWorkflows", &ListWorkflowsResponse{}))
	var nilCtx context.Context
	_, err := c.ListWorkflows(nilCtx, &ListWorkflowsRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListWorkflows_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListWorkflows(context.Background(), &ListWorkflowsRequest{TenantID: "t1"})
	require.Error(t, err)
}

func TestListWorkflows_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListWorkflows(context.Background(), &ListWorkflowsRequest{TenantID: "t1"})
	require.Error(t, err)
}

// =============================================================================
// GetWorkflow: nil-context, transport-error, nil-response, type-mismatch.
// =============================================================================

func TestGetWorkflow_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "GetWorkflow", validWorkflowDef()))
	var nilCtx context.Context
	_, err := c.GetWorkflow(nilCtx, &GetWorkflowRequest{TenantID: "t1", Name: "wf"})
	require.Error(t, err)
}

func TestGetWorkflow_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetWorkflow(context.Background(), &GetWorkflowRequest{TenantID: "t1", Name: "wf"})
	require.Error(t, err)
}

func TestGetWorkflow_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.GetWorkflow(context.Background(), &GetWorkflowRequest{TenantID: "t1", Name: "wf"})
	require.Error(t, err)
}

func TestGetWorkflow_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetWorkflow(context.Background(), &GetWorkflowRequest{TenantID: "t1", Name: "wf"})
	require.Error(t, err)
}

// =============================================================================
// UpdateWorkflow: nil-context, transport-error, nil-response, type-mismatch.
// =============================================================================

func validUpdateWorkflowReq() *UpdateWorkflowRequest {
	return &UpdateWorkflowRequest{TenantID: "t1", Name: "wf", Definition: validWorkflowDef()}
}

func TestUpdateWorkflow_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "UpdateWorkflow", validWorkflowDef()))
	var nilCtx context.Context
	_, err := c.UpdateWorkflow(nilCtx, validUpdateWorkflowReq())
	require.Error(t, err)
}

func TestUpdateWorkflow_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.UpdateWorkflow(context.Background(), validUpdateWorkflowReq())
	require.Error(t, err)
}

func TestUpdateWorkflow_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.UpdateWorkflow(context.Background(), validUpdateWorkflowReq())
	require.Error(t, err)
}

func TestUpdateWorkflow_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.UpdateWorkflow(context.Background(), validUpdateWorkflowReq())
	require.Error(t, err)
}

// =============================================================================
// DisableFleetNode: missing-agent-id and nil-response branches.
// =============================================================================

func TestDisableAgent_MissingAgentID_Branch(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent ID is required")
}

func TestDisableAgent_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "DisableFleetNode", &DisableFleetNodeResponse{}))
	var nilCtx context.Context
	_, err := c.DisableFleetNode(nilCtx, &DisableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestDisableAgent_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestDisableAgent_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.DisableFleetNode(context.Background(), &DisableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// EnableFleetNode: nil-context, missing-agent-id, transport-error, nil-response,
// type-mismatch.
// =============================================================================

func TestEnableAgent_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "EnableFleetNode", &EnableFleetNodeResponse{}))
	var nilCtx context.Context
	_, err := c.EnableFleetNode(nilCtx, &EnableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestEnableAgent_MissingAgentID(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.EnableFleetNode(context.Background(), &EnableFleetNodeRequest{TenantID: "t1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "agent ID is required")
}

func TestEnableAgent_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.EnableFleetNode(context.Background(), &EnableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestEnableAgent_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.EnableFleetNode(context.Background(), &EnableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

func TestEnableAgent_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.EnableFleetNode(context.Background(), &EnableFleetNodeRequest{TenantID: "t1", NodeID: "a1"})
	require.Error(t, err)
}

// =============================================================================
// SubmitTaskArtifacts: nil-context, missing-tenant, nil-response, type-mismatch.
// =============================================================================

func TestSubmitTaskArtifacts_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "SubmitTaskArtifacts", &SubmitTaskArtifactsResponse{}))
	var nilCtx context.Context
	_, err := c.SubmitTaskArtifacts(nilCtx, &SubmitTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestSubmitTaskArtifacts_MissingTenant(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.SubmitTaskArtifacts(context.Background(), &SubmitTaskArtifactsRequest{TaskID: "tk1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
}

func TestSubmitTaskArtifacts_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.SubmitTaskArtifacts(context.Background(), &SubmitTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestSubmitTaskArtifacts_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.SubmitTaskArtifacts(context.Background(), &SubmitTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

// =============================================================================
// GetTaskArtifact: nil-context, missing-tenant, missing-task, transport-error,
// nil-response, type-mismatch.
// =============================================================================

func validGetArtifactReq() *GetTaskArtifactRequest {
	return &GetTaskArtifactRequest{TenantID: "t1", TaskID: "tk1", Name: "art"}
}

func TestGetTaskArtifact_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "GetTaskArtifact", &TaskArtifact{}))
	var nilCtx context.Context
	_, err := c.GetTaskArtifact(nilCtx, validGetArtifactReq())
	require.Error(t, err)
}

func TestGetTaskArtifact_MissingTenant(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetTaskArtifact(context.Background(), &GetTaskArtifactRequest{TaskID: "tk1", Name: "art"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
}

func TestGetTaskArtifact_MissingTask(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetTaskArtifact(context.Background(), &GetTaskArtifactRequest{TenantID: "t1", Name: "art"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task ID is required")
}

func TestGetTaskArtifact_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.GetTaskArtifact(context.Background(), validGetArtifactReq())
	require.Error(t, err)
}

func TestGetTaskArtifact_NilResponse(t *testing.T) {
	c := NewClient(nilTransport())
	_, err := c.GetTaskArtifact(context.Background(), validGetArtifactReq())
	require.Error(t, err)
}

func TestGetTaskArtifact_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.GetTaskArtifact(context.Background(), validGetArtifactReq())
	require.Error(t, err)
}

// =============================================================================
// ListTaskArtifacts: nil-context, missing-tenant, transport-error,
// nil-response (returns empty), type-mismatch.
// =============================================================================

func TestListTaskArtifacts_NilContext(t *testing.T) {
	c := NewClient(okTransport(t, "ListTaskArtifacts", &ListTaskArtifactsResponse{}))
	var nilCtx context.Context
	_, err := c.ListTaskArtifacts(nilCtx, &ListTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestListTaskArtifacts_MissingTenant(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListTaskArtifacts(context.Background(), &ListTaskArtifactsRequest{TaskID: "tk1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant ID is required")
}

func TestListTaskArtifacts_TransportError(t *testing.T) {
	c := NewClient(failTransport())
	_, err := c.ListTaskArtifacts(context.Background(), &ListTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}

func TestListTaskArtifacts_NilResponseReturnsEmpty(t *testing.T) {
	c := NewClient(nilTransport())
	resp, err := c.ListTaskArtifacts(context.Background(), &ListTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, 0, resp.Count)
	assert.NotNil(t, resp.Artifacts)
	assert.Equal(t, "tk1", resp.TaskID)
}

func TestListTaskArtifacts_TypeMismatch(t *testing.T) {
	c := NewClient(mismatchTransport())
	_, err := c.ListTaskArtifacts(context.Background(), &ListTaskArtifactsRequest{TenantID: "t1", TaskID: "tk1"})
	require.Error(t, err)
}
