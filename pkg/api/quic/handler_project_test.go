//go:build quic

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package quic

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// =============================================================================
// Harness
// =============================================================================

// mockServiceWithProject exposes a ProjectService to the QUIC handler.
type mockServiceWithProject struct {
	mockService
	projectSvc service.ProjectService
}

func (m *mockServiceWithProject) Project() service.ProjectService { return m.projectSvc }

// createProjectHandler builds a QUIC handler wired to projectSvc.
func createProjectHandler(projectSvc service.ProjectService) *Handler {
	return NewHandler(&mockServiceWithProject{projectSvc: projectSvc}, nil, nil, nil, createOrchestratorTestLogger())
}

// projectRequest builds an OpOrchestrator request carrying a project sub-op.
func projectRequest(t *testing.T, subOp byte, body interface{}) *Request {
	t.Helper()
	payload, err := EncodePayload(body)
	require.NoError(t, err, "encode payload")
	full := make([]byte, 1+len(payload))
	full[0] = subOp
	copy(full[1:], payload)
	return &Request{Opcode: OpOrchestrator, Payload: full}
}

// quicSampleProject is the record the fakes return.
func quicSampleProject() *orchestration.Project {
	return &orchestration.Project{
		ID:               "proj-1",
		TenantID:         "tenant-1",
		Name:             "web-api",
		Description:      "public API",
		Workspace:        orchestration.ProjectWorkspace{AgentID: "agent-1", Path: "/srv/web-api"},
		HostAffinity:     "builder-3",
		DefaultLeadAgent: "agent-1",
		Git:              orchestration.ProjectGit{RepoURL: "https://example.com/r.git", DefaultBranch: "main", CurrentSHA: "abc123"},
		Permissions: orchestration.ProjectPermissions{
			AgentRoles: map[string]orchestration.ProjectRole{"agent-1": orchestration.ProjectRoleLead},
		},
		Quotas:    orchestration.ProjectQuotas{TokenBudget: 1000, GPUMinutes: 60, MaxConcurrency: 4},
		Metadata:  map[string]string{"env": "prod"},
		CreatedAt: time.Unix(1700000000, 0).UTC(),
		UpdatedAt: time.Unix(1700000100, 0).UTC(),
	}
}

// allProjectSubOps is the closed set of project sub-operations.
var allProjectSubOps = []byte{
	SubOpProjectCreate,
	SubOpProjectGet,
	SubOpProjectGetByName,
	SubOpProjectList,
	SubOpProjectUpdate,
	SubOpProjectDelete,
	SubOpProjectGrantAgentRole,
	SubOpProjectRevokeAgentRole,
}

// projectSubOpBodies are minimally valid request bodies per sub-op.
func projectSubOpBodies() map[byte]interface{} {
	return map[byte]interface{}{
		SubOpProjectCreate:          &projectCreateWire{TenantID: "tenant-1", Name: "web-api"},
		SubOpProjectGet:             &projectIDWire{TenantID: "tenant-1", ProjectID: "proj-1"},
		SubOpProjectGetByName:       &projectNameWire{TenantID: "tenant-1", Name: "web-api"},
		SubOpProjectList:            &projectListWire{TenantID: "tenant-1"},
		SubOpProjectUpdate:          &projectUpdateWire{TenantID: "tenant-1", ProjectID: "proj-1", Name: "renamed"},
		SubOpProjectDelete:          &projectIDWire{TenantID: "tenant-1", ProjectID: "proj-1"},
		SubOpProjectGrantAgentRole:  &projectGrantWire{TenantID: "tenant-1", ProjectID: "proj-1", AgentID: "agent-2", Role: orchestration.ProjectRoleWorker},
		SubOpProjectRevokeAgentRole: &projectRevokeWire{TenantID: "tenant-1", ProjectID: "proj-1", AgentID: "agent-2"},
	}
}

// =============================================================================
// Dispatch
// =============================================================================

// TestProjectSubOpRangeRoutesToProjectHandler proves the project sub-op range
// carried on OpOrchestrator reaches the project dispatch table rather than
// falling into the orchestrator switch (which would answer
// UnknownSubOpcodeError).
func TestProjectSubOpRangeRoutesToProjectHandler(t *testing.T) {
	called := map[byte]bool{}
	proj := quicSampleProject()
	svc := &mocks.MockProjectService{
		CreateProjectFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			called[SubOpProjectCreate] = true
			return &service.CreateProjectResponse{Project: proj, Timestamp: proj.CreatedAt}, nil
		},
		GetProjectFunc: func(_ context.Context, req *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			called[SubOpProjectGet] = true
			return &service.GetProjectResponse{Project: proj}, nil
		},
		GetProjectByNameFunc: func(_ context.Context, req *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
			called[SubOpProjectGetByName] = true
			return &service.GetProjectResponse{Project: proj}, nil
		},
		ListProjectsFunc: func(_ context.Context, req *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			called[SubOpProjectList] = true
			return &service.ListProjectsResponse{Projects: []*orchestration.Project{proj}, Total: 1}, nil
		},
		UpdateProjectFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			called[SubOpProjectUpdate] = true
			return &service.UpdateProjectResponse{Project: proj}, nil
		},
		DeleteProjectFunc: func(_ context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			called[SubOpProjectDelete] = true
			return &service.DeleteProjectResponse{ProjectID: req.ProjectID, Deleted: true}, nil
		},
		GrantAgentRoleFunc: func(_ context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			called[SubOpProjectGrantAgentRole] = true
			return &service.GrantAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID, Role: req.Role}, nil
		},
		RevokeAgentRoleFunc: func(_ context.Context, req *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
			called[SubOpProjectRevokeAgentRole] = true
			return &service.RevokeAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID}, nil
		},
	}
	handler := createProjectHandler(svc)

	for subOp, body := range projectSubOpBodies() {
		resp := handler.HandleRequest(context.Background(), projectRequest(t, subOp, body))
		assert.Equalf(t, StatusOK, resp.Status, "sub-op 0x%02X: %s", subOp, string(resp.Payload))
		assert.Truef(t, called[subOp], "sub-op 0x%02X did not reach the project service", subOp)
	}
}

// TestProjectRequestFieldsSurviveTheWire proves every request field the SDK can
// set arrives at the service layer. A dropped tag here silently discards user
// input, which is exactly the drift class this surface was missing.
func TestProjectRequestFieldsSurviveTheWire(t *testing.T) {
	var created *service.CreateProjectRequest
	var updated *service.UpdateProjectRequest
	var granted *service.GrantAgentRoleRequest
	svc := &mocks.MockProjectService{
		CreateProjectFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			created = req
			return &service.CreateProjectResponse{Project: quicSampleProject()}, nil
		},
		UpdateProjectFunc: func(_ context.Context, req *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			updated = req
			return &service.UpdateProjectResponse{Project: quicSampleProject()}, nil
		},
		GrantAgentRoleFunc: func(_ context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			granted = req
			return &service.GrantAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID, Role: req.Role}, nil
		},
	}
	handler := createProjectHandler(svc)

	resp := handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectCreate, &projectCreateWire{
		TenantID:         "tenant-1",
		Name:             "web-api",
		Description:      "public API",
		WorkspaceAgentID: "agent-1",
		WorkspacePath:    "/srv/web-api",
		HostAffinity:     "builder-3",
		DefaultLeadAgent: "agent-1",
		GitRepoURL:       "https://example.com/r.git",
		GitDefaultBranch: "main",
		TokenBudget:      1000,
		GPUMinutes:       60,
		MaxConcurrency:   4,
		Metadata:         map[string]string{"env": "prod"},
	}))
	require.Equal(t, StatusOK, resp.Status, string(resp.Payload))
	require.NotNil(t, created)
	assert.Equal(t, "tenant-1", created.TenantID)
	assert.Equal(t, "web-api", created.Name)
	assert.Equal(t, "public API", created.Description)
	assert.Equal(t, "agent-1", created.WorkspaceAgentID)
	assert.Equal(t, "/srv/web-api", created.WorkspacePath)
	assert.Equal(t, "builder-3", created.HostAffinity)
	assert.Equal(t, "agent-1", created.DefaultLeadAgent)
	assert.Equal(t, "https://example.com/r.git", created.GitRepoURL)
	assert.Equal(t, "main", created.GitDefaultBranch)
	assert.Equal(t, int64(1000), created.TokenBudget)
	assert.Equal(t, int64(60), created.GPUMinutes)
	assert.Equal(t, 4, created.MaxConcurrency)
	assert.Equal(t, map[string]string{"env": "prod"}, created.Metadata)

	resp = handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectUpdate, &projectUpdateWire{
		TenantID:         "tenant-1",
		ProjectID:        "proj-1",
		Name:             "renamed",
		ClearDescription: true,
		GitCurrentSHA:    "def456",
		AgentRoles:       map[string]orchestration.ProjectRole{"agent-2": orchestration.ProjectRoleObserver},
	}))
	require.Equal(t, StatusOK, resp.Status, string(resp.Payload))
	require.NotNil(t, updated)
	assert.Equal(t, "renamed", updated.Name)
	assert.True(t, updated.ClearDescription)
	assert.Equal(t, "def456", updated.GitCurrentSHA)
	assert.Equal(t, orchestration.ProjectRoleObserver, updated.AgentRoles["agent-2"])

	resp = handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectGrantAgentRole, &projectGrantWire{
		TenantID:  "tenant-1",
		ProjectID: "proj-1",
		AgentID:   "agent-2",
		Role:      orchestration.ProjectRoleLead,
	}))
	require.Equal(t, StatusOK, resp.Status, string(resp.Payload))
	require.NotNil(t, granted)
	assert.Equal(t, "agent-2", granted.AgentID)
	assert.Equal(t, orchestration.ProjectRoleLead, granted.Role)
}

// TestProjectResponsesUseSnakeCaseWire pins the response envelopes. The SDK
// QUIC client decodes these exact keys; Go field names on the wire would break
// it silently by yielding a zero-valued project.
func TestProjectResponsesUseSnakeCaseWire(t *testing.T) {
	proj := quicSampleProject()
	svc := &mocks.MockProjectService{
		GetProjectFunc: func(_ context.Context, _ *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return &service.GetProjectResponse{Project: proj, Timestamp: proj.UpdatedAt}, nil
		},
		ListProjectsFunc: func(_ context.Context, _ *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			return &service.ListProjectsResponse{Projects: []*orchestration.Project{proj}, Total: 1}, nil
		},
		DeleteProjectFunc: func(_ context.Context, req *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			return &service.DeleteProjectResponse{ProjectID: req.ProjectID, Deleted: true}, nil
		},
		GrantAgentRoleFunc: func(_ context.Context, req *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			return &service.GrantAgentRoleResponse{ProjectID: req.ProjectID, AgentID: req.AgentID, Role: req.Role}, nil
		},
	}
	handler := createProjectHandler(svc)
	bodies := projectSubOpBodies()

	resp := handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectGet, bodies[SubOpProjectGet]))
	require.Equal(t, StatusOK, resp.Status)
	var single struct {
		Project map[string]interface{} `json:"project"`
	}
	require.NoError(t, json.Unmarshal(resp.Payload, &single))
	require.NotNil(t, single.Project, "response must nest the project under \"project\"")
	for _, key := range []string{"id", "tenant_id", "name", "workspace", "host_affinity", "default_lead_agent", "git", "permissions", "quotas", "created_at", "updated_at"} {
		assert.Containsf(t, single.Project, key, "project body must carry %q", key)
	}

	resp = handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectList, bodies[SubOpProjectList]))
	require.Equal(t, StatusOK, resp.Status)
	var list struct {
		Projects []map[string]interface{} `json:"projects"`
		Total    int                      `json:"total"`
	}
	require.NoError(t, json.Unmarshal(resp.Payload, &list))
	assert.Len(t, list.Projects, 1)
	assert.Equal(t, 1, list.Total)

	resp = handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectDelete, bodies[SubOpProjectDelete]))
	require.Equal(t, StatusOK, resp.Status)
	var del struct {
		ProjectID string `json:"project_id"`
		Deleted   bool   `json:"deleted"`
	}
	require.NoError(t, json.Unmarshal(resp.Payload, &del))
	assert.Equal(t, "proj-1", del.ProjectID)
	assert.True(t, del.Deleted)

	resp = handler.HandleRequest(context.Background(), projectRequest(t, SubOpProjectGrantAgentRole, bodies[SubOpProjectGrantAgentRole]))
	require.Equal(t, StatusOK, resp.Status)
	var grant struct {
		ProjectID string `json:"project_id"`
		AgentID   string `json:"agent_id"`
		Role      string `json:"role"`
	}
	require.NoError(t, json.Unmarshal(resp.Payload, &grant))
	assert.Equal(t, "proj-1", grant.ProjectID)
	assert.Equal(t, "agent-2", grant.AgentID)
	assert.Equal(t, "worker", grant.Role)
}

// TestProjectListNormalizesEmptyResult proves an empty tenant answers with an
// empty array rather than JSON null, which the SDK would decode as a nil slice.
func TestProjectListNormalizesEmptyResult(t *testing.T) {
	handler := createProjectHandler(&mocks.MockProjectService{
		ListProjectsFunc: func(_ context.Context, _ *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			return &service.ListProjectsResponse{}, nil
		},
	})
	resp := handler.HandleRequest(context.Background(),
		projectRequest(t, SubOpProjectList, &projectListWire{TenantID: "tenant-1"}))
	require.Equal(t, StatusOK, resp.Status)
	assert.Contains(t, string(resp.Payload), `"projects":[]`)
}

// TestProjectDispatch_UnknownSubOp covers the error path: a byte with no
// registered handler must fail, not silently succeed.
func TestProjectDispatch_UnknownSubOp(t *testing.T) {
	handler := createProjectHandler(&mocks.MockProjectService{})
	// isProjectSubOp admits 0x40-0x47 and every byte in it is mapped, so drive
	// the negative case through the dispatcher directly with an out-of-table
	// byte.
	resp := handler.handleProjectDispatch(context.Background(), 0x4F, &Request{})
	assert.NotEqual(t, StatusOK, resp.Status, "an unregistered project sub-op must fail")
}

// TestProjectSubOpOutsideRangeFallsThrough proves the project branch did not
// swallow neighbouring sub-opcodes: 0x48 is one past the range and must reach
// the orchestrator switch's unknown-sub-opcode error.
func TestProjectSubOpOutsideRangeFallsThrough(t *testing.T) {
	assert.False(t, isProjectSubOp(0x3F), "0x3F precedes the project range")
	assert.False(t, isProjectSubOp(0x48), "0x48 follows the project range")
	for _, subOp := range allProjectSubOps {
		assert.Truef(t, isProjectSubOp(subOp), "0x%02X must be in the project range", subOp)
	}

	handler := createProjectHandler(&mocks.MockProjectService{})
	resp := handler.HandleRequest(context.Background(), &Request{
		Opcode:  OpOrchestrator,
		Payload: append([]byte{0x48}, []byte(`{}`)...),
	})
	assert.NotEqual(t, StatusOK, resp.Status)
}

// TestProjectDispatch_ServiceUnavailable proves every project sub-op fails
// closed when no ProjectService is wired.
func TestProjectDispatch_ServiceUnavailable(t *testing.T) {
	handler := createProjectHandler(nil)
	for _, subOp := range allProjectSubOps {
		resp := handler.HandleRequest(context.Background(),
			projectRequest(t, subOp, map[string]string{"tenant_id": "t1", "project_id": "p1"}))
		assert.NotEqualf(t, StatusOK, resp.Status,
			"sub-op 0x%02X succeeded with no project service wired", subOp)
	}
}

// TestProjectDispatch_MalformedPayload covers the decode error path for every
// sub-op: a truncated or non-JSON body must be rejected, never partially
// applied.
func TestProjectDispatch_MalformedPayload(t *testing.T) {
	handler := createProjectHandler(&mocks.MockProjectService{})
	for _, subOp := range allProjectSubOps {
		for name, body := range map[string][]byte{
			"not-json":   []byte("not-json"),
			"truncated":  []byte(`{"tenant_id": "t1"`),
			"wrong-type": []byte(`{"tenant_id": 42}`),
		} {
			resp := handler.HandleRequest(context.Background(), &Request{
				Opcode:  OpOrchestrator,
				Payload: append([]byte{subOp}, body...),
			})
			assert.NotEqualf(t, StatusOK, resp.Status,
				"sub-op 0x%02X accepted a %s payload", subOp, name)
		}
	}
}

// TestProjectDispatch_MissingSubOpcode covers the short-frame case: an
// OpOrchestrator frame with no sub-op byte at all.
func TestProjectDispatch_MissingSubOpcode(t *testing.T) {
	handler := createProjectHandler(&mocks.MockProjectService{})
	resp := handler.HandleRequest(context.Background(), &Request{Opcode: OpOrchestrator, Payload: nil})
	assert.NotEqual(t, StatusOK, resp.Status, "an empty OpOrchestrator frame must fail")
}

// TestProjectDispatch_ServiceError proves a service failure propagates rather
// than being reported as success.
func TestProjectDispatch_ServiceError(t *testing.T) {
	sentinel := errors.New("project boom")
	handler := createProjectHandler(&mocks.MockProjectService{
		CreateProjectFunc: func(context.Context, *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			return nil, sentinel
		},
		GetProjectFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return nil, sentinel
		},
		GetProjectByNameFunc: func(context.Context, *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
			return nil, sentinel
		},
		ListProjectsFunc: func(context.Context, *service.ListProjectsRequest) (*service.ListProjectsResponse, error) {
			return nil, sentinel
		},
		UpdateProjectFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
			return nil, sentinel
		},
		DeleteProjectFunc: func(context.Context, *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			return nil, sentinel
		},
		GrantAgentRoleFunc: func(context.Context, *service.GrantAgentRoleRequest) (*service.GrantAgentRoleResponse, error) {
			return nil, sentinel
		},
		RevokeAgentRoleFunc: func(context.Context, *service.RevokeAgentRoleRequest) (*service.RevokeAgentRoleResponse, error) {
			return nil, sentinel
		},
	})
	for subOp, body := range projectSubOpBodies() {
		resp := handler.HandleRequest(context.Background(), projectRequest(t, subOp, body))
		assert.NotEqualf(t, StatusOK, resp.Status, "sub-op 0x%02X hid a service error", subOp)
	}
}

// TestProjectDispatch_NilServiceResponse proves a (nil, nil) service return is
// reported as an error rather than panicking on the envelope build.
func TestProjectDispatch_NilServiceResponse(t *testing.T) {
	handler := createProjectHandler(&mocks.MockProjectService{
		GetProjectFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
			return nil, nil
		},
	})
	resp := handler.HandleRequest(context.Background(),
		projectRequest(t, SubOpProjectGet, &projectIDWire{TenantID: "t1", ProjectID: "p1"}))
	assert.NotEqual(t, StatusOK, resp.Status)
}

// =============================================================================
// Authorization
// =============================================================================

// TestProjectSubOpsAuthorizeAsProject is the authz guard: the project sub-ops
// share the OpOrchestrator dispatch byte, so without sub-op-aware resolution
// they would authorize under the materially weaker orchestrator:status the rest
// of that opcode uses. The values pin the shared vocabulary with the REST PEP
// mapping and the gRPC mapping.
func TestProjectSubOpsAuthorizeAsProject(t *testing.T) {
	cases := []struct {
		subOp  byte
		action string
	}{
		{SubOpProjectCreate, "create"},
		{SubOpProjectGet, "read"},
		{SubOpProjectGetByName, "read"},
		{SubOpProjectList, "list"},
		{SubOpProjectUpdate, "update"},
		{SubOpProjectDelete, "delete"},
		{SubOpProjectGrantAgentRole, "grant"},
		{SubOpProjectRevokeAgentRole, "revoke"},
	}
	for _, tc := range cases {
		resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{tc.subOp})
		require.Truef(t, ok, "sub-op 0x%02X is unmapped (default-denied)", tc.subOp)
		assert.Equalf(t, "project", resource, "sub-op 0x%02X resource", tc.subOp)
		assert.Equalf(t, tc.action, action, "sub-op 0x%02X action", tc.subOp)
	}
}

// TestProjectAuthz_BidirectionalGuard verifies every routable project sub-op is
// authorizable and vice versa. A future sub-op cannot ship
// routable-but-unauthorizable (an authz hole) or authorized-but-unroutable
// (a dead permission).
func TestProjectAuthz_BidirectionalGuard(t *testing.T) {
	for subOp := range projectSubHandlers {
		_, ok := projectSubOpPermissions[subOp]
		assert.Truef(t, ok, "sub-op 0x%02X routes but has no permission mapping", subOp)
	}
	for subOp := range projectSubOpPermissions {
		_, ok := projectSubHandlers[subOp]
		assert.Truef(t, ok, "sub-op 0x%02X is authorized but does not route", subOp)
	}
}

// TestProjectAuthz_GateLevelPassesProjectPermission drives the real PEP: a
// delete must reach the validator as project:delete, not orchestrator:status.
func TestProjectAuthz_GateLevelPassesProjectPermission(t *testing.T) {
	v := &fakeValidatorAuthorizer{
		authzCtx: &platformtoken.AuthorizedContext{
			Subject: "user-1", Username: "alice", TenantID: "tenant-1", Roles: []string{"admin"},
		},
	}
	h := NewHandler(&mockServiceWithProject{projectSvc: &mocks.MockProjectService{}},
		nil, nil, nil, createOrchestratorTestLogger(), WithHandlerValidatorAuthorizer(v))

	body, err := EncodePayload(&projectIDWire{TenantID: "tenant-1", ProjectID: "proj-1"})
	require.NoError(t, err)
	req := &Request{
		Opcode:  OpOrchestrator,
		Token:   "bearer-token",
		Payload: append([]byte{SubOpProjectDelete}, body...),
	}

	_, resp := h.authorizeAndInject(context.Background(), req)
	require.Nil(t, resp, "expected allow, got denial %v", resp)
	assert.Equal(t, "project:delete", v.gotRequiredPerm)
	assert.Equal(t, "bearer-token", v.gotToken)
	assert.Empty(t, v.gotDPoP, "QUIC must pass empty DPoP")
}

// TestProjectAuthz_GateLevelDeniesUnauthorized is the denial path: a validator
// rejection must stop the request before it reaches the project service.
//
// The validator returns a typed *platformtoken.ScopeDeniedError because that is
// what a real scope rejection carries, and validatorErrorStatus maps it to
// StatusPermissionDenied. An untyped error takes the default branch and reports
// StatusUnauthorized instead, which would exercise a different denial reason
// than the one this test names.
func TestProjectAuthz_GateLevelDeniesUnauthorized(t *testing.T) {
	v := &fakeValidatorAuthorizer{err: &platformtoken.ScopeDeniedError{Required: "project:delete"}}
	reached := false
	h := NewHandler(&mockServiceWithProject{projectSvc: &mocks.MockProjectService{
		DeleteProjectFunc: func(context.Context, *service.DeleteProjectRequest) (*service.DeleteProjectResponse, error) {
			reached = true
			return &service.DeleteProjectResponse{Deleted: true}, nil
		},
	}}, nil, nil, nil, createOrchestratorTestLogger(), WithHandlerValidatorAuthorizer(v))

	body, err := EncodePayload(&projectIDWire{TenantID: "tenant-1", ProjectID: "proj-1"})
	require.NoError(t, err)
	resp := h.HandleRequest(context.Background(), &Request{
		Opcode:  OpOrchestrator,
		Token:   "bearer-token",
		Payload: append([]byte{SubOpProjectDelete}, body...),
	})

	assert.Equal(t, StatusPermissionDenied, resp.Status)
	assert.False(t, reached, "a denied request must never reach the project service")
	assert.Equal(t, "project:delete", v.gotRequiredPerm)
}

// TestNonProjectOrchestratorSubOpsKeepOrchestratorPermission proves the project
// branch did not capture the rest of the OpOrchestrator surface.
func TestNonProjectOrchestratorSubOpsKeepOrchestratorPermission(t *testing.T) {
	resource, action, ok := getQRDBPermission(OpOrchestrator, []byte{SubOpOrchestratorSpawn})
	require.True(t, ok, "orchestrator spawn lost its permission mapping")
	assert.Equal(t, "orchestrator", resource)
	assert.Equal(t, "status", action)

	resource, action, ok = getQRDBPermission(OpOrchestrator, []byte{SubOpAccountCreate})
	require.True(t, ok, "account create lost its permission mapping")
	assert.Equal(t, "credentials", resource)
	assert.Equal(t, "store", action)
}
