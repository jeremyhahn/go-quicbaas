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

package grpc

import (
	"context"
	"testing"
	"time"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// projectCreatedAt / projectUpdatedAt are fixed so timestamp fidelity is
// asserted against a known value rather than wall-clock drift.
var (
	projectCreatedAt = time.Unix(1_700_000_000, 0).UTC()
	projectUpdatedAt = time.Unix(1_700_003_600, 0).UTC()
)

// sampleProtoProject returns a fully populated proto record so every nested
// SDK struct (workspace, git, permissions, quotas) has something to lose.
func sampleProtoProject() *pb.Project {
	return &pb.Project{
		Id:               "proj-1",
		TenantId:         "tenant-1",
		Name:             "quicbaas",
		Description:      "self hosting",
		WorkspaceAgentId: "agent-1",
		WorkspacePath:    "/home/agent/src/go-quicbaas",
		HostAffinity:     "host-a",
		DefaultLeadAgent: "agent-lead",
		GitRepoUrl:       "https://git.example.test/go-quicbaas.git",
		GitDefaultBranch: "main",
		GitCurrentSha:    "abc123",
		AgentRoles:       map[string]string{"agent-1": "lead", "agent-2": "observer"},
		TokenBudget:      1000,
		GpuMinutes:       60,
		MaxConcurrency:   4,
		Metadata:         map[string]string{"team": "platform"},
		CreatedAtUnix:    projectCreatedAt.Unix(),
		UpdatedAtUnix:    projectUpdatedAt.Unix(),
	}
}

// fakeProjectServer is a configurable in-memory ProjectService backend. It
// echoes the request fields it receives so the tests can assert the SDK →
// proto direction as well as the response conversion.
type fakeProjectServer struct {
	pb.UnimplementedProjectServiceServer
	fail bool

	lastCreate *pb.CreateProjectRequest
	lastGet    *pb.GetProjectRequest
	lastByName *pb.GetProjectByNameRequest
	lastUpdate *pb.UpdateProjectRequest
	lastDelete *pb.DeleteProjectRequest
	lastList   *pb.ListProjectsRequest
	lastGrant  *pb.GrantAgentRoleRequest
	lastRevoke *pb.RevokeAgentRoleRequest

	// roles is the server-side agent role map. Grant/Revoke mutate a single
	// entry so the tests can prove the other entries survive.
	roles map[string]string
}

func (f *fakeProjectServer) CreateProject(_ context.Context, req *pb.CreateProjectRequest) (*pb.CreateProjectResponse, error) {
	f.lastCreate = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.CreateProjectResponse{Project: sampleProtoProject(), Timestamp: projectCreatedAt.Unix()}, nil
}

func (f *fakeProjectServer) GetProject(_ context.Context, req *pb.GetProjectRequest) (*pb.GetProjectResponse, error) {
	f.lastGet = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.GetProjectResponse{Project: sampleProtoProject(), Timestamp: projectCreatedAt.Unix()}, nil
}

func (f *fakeProjectServer) GetProjectByName(_ context.Context, req *pb.GetProjectByNameRequest) (*pb.GetProjectResponse, error) {
	f.lastByName = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.GetProjectResponse{Project: sampleProtoProject(), Timestamp: projectCreatedAt.Unix()}, nil
}

func (f *fakeProjectServer) UpdateProject(_ context.Context, req *pb.UpdateProjectRequest) (*pb.UpdateProjectResponse, error) {
	f.lastUpdate = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.UpdateProjectResponse{Project: sampleProtoProject(), Timestamp: projectUpdatedAt.Unix()}, nil
}

func (f *fakeProjectServer) DeleteProject(_ context.Context, req *pb.DeleteProjectRequest) (*pb.DeleteProjectResponse, error) {
	f.lastDelete = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.DeleteProjectResponse{ProjectId: req.ProjectId, Deleted: true, Timestamp: projectUpdatedAt.Unix()}, nil
}

func (f *fakeProjectServer) ListProjects(_ context.Context, req *pb.ListProjectsRequest) (*pb.ListProjectsResponse, error) {
	f.lastList = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	return &pb.ListProjectsResponse{
		Projects:  []*pb.Project{sampleProtoProject()},
		Total:     1,
		Timestamp: projectUpdatedAt.Unix(),
	}, nil
}

func (f *fakeProjectServer) GrantAgentRole(_ context.Context, req *pb.GrantAgentRoleRequest) (*pb.GrantAgentRoleResponse, error) {
	f.lastGrant = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.roles[req.AgentId] = req.Role
	return &pb.GrantAgentRoleResponse{
		ProjectId: req.ProjectId,
		AgentId:   req.AgentId,
		Role:      req.Role,
		Timestamp: projectUpdatedAt.Unix(),
	}, nil
}

func (f *fakeProjectServer) RevokeAgentRole(_ context.Context, req *pb.RevokeAgentRoleRequest) (*pb.RevokeAgentRoleResponse, error) {
	f.lastRevoke = req
	if err := failErr(f.fail); err != nil {
		return nil, err
	}
	f.roles[req.AgentId] = string(project.ProjectRoleDenied)
	return &pb.RevokeAgentRoleResponse{
		ProjectId: req.ProjectId,
		AgentId:   req.AgentId,
		Timestamp: projectUpdatedAt.Unix(),
	}, nil
}

// newProjectHandlerWithServer starts an in-memory ProjectService and returns a
// handler bound to it, along with the fake so callers can inspect the request
// the handler produced.
func newProjectHandlerWithServer(t *testing.T, fail bool) (*projectHandler, *fakeProjectServer) {
	t.Helper()
	fake := &fakeProjectServer{fail: fail, roles: map[string]string{
		"agent-1": "lead",
		"agent-2": "observer",
	}}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterProjectServiceServer(s, fake)
	})
	return newProjectHandler(srv.dial(t)), fake
}

// =============================================================================
// Routing
// =============================================================================

// TestIsProjectServiceGRPC pins the routing predicate. The gRPC dispatch chain
// is prefix-based and ordered, so a predicate that also matched a sibling
// service would hijack it — and, symmetrically, an earlier predicate that
// claimed "quicbaas.Project" is what left the project surface falling through
// to the untyped conn.Invoke path.
func TestIsProjectServiceGRPC(t *testing.T) {
	if !isProjectService("quicbaas.Project") {
		t.Error(`isProjectService("quicbaas.Project") = false`)
	}
	for _, name := range []string{
		"quicbaas.Auth", "quicbaas.Account", "quicbaas.Orchestrator",
		"quicbaas.KV", "quicbaas.IAM.User", "quicbaas.Operations",
	} {
		if isProjectService(name) {
			t.Errorf("isProjectService(%q) = true", name)
		}
	}

	// No predicate that runs before the project branch may claim the project
	// service, or the request never reaches this handler.
	claimants := map[string]func(string) bool{
		"auth":         isAuthService,
		"iam":          isIAMService,
		"kv":           isKVService,
		"health":       isHealthService,
		"cluster":      isClusterService,
		"vector":       isVectorService,
		"rag":          isRAGService,
		"embedding":    isEmbeddingService,
		"rbac":         isRBACService,
		"orchestrator": isOrchestratorService,
		"account":      isAccountService,
	}
	for name, claims := range claimants {
		if claims("quicbaas.Project") {
			t.Errorf("the %s predicate claims quicbaas.Project", name)
		}
	}
}

// TestProjectMethodMapIsComplete pins the dispatch table against the SDK
// client's method set. A missing entry answers "unknown project method"
// instead of reaching the server.
func TestProjectMethodMapIsComplete(t *testing.T) {
	for _, method := range []string{
		"Create", "Get", "GetByName", "Update", "Delete", "List",
		"GrantAgentRole", "RevokeAgentRole",
	} {
		if _, ok := projectMethodMap[method]; !ok {
			t.Errorf("project method %q has no gRPC handler", method)
		}
	}
	if len(projectMethodMap) != 8 {
		t.Errorf("dispatch table has %d entries, want 8", len(projectMethodMap))
	}
}

// TestProjectHandlerRejectsUnknownMethod covers the dispatcher's error path.
func TestProjectHandlerRejectsUnknownMethod(t *testing.T) {
	h := newProjectHandler(nil)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "Teleport", nil)
	if err == nil {
		t.Fatal("expected an error for an unknown project method")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("unknown method error code = %s, want invalid input", errors.Code(err))
	}
}

// TestProjectHandlersRejectWrongRequestType proves each mapped handler rejects
// a wrong-typed request before it touches the (nil) connection, so a type
// mismatch is a clean error rather than a nil-pointer panic.
func TestProjectHandlersRejectWrongRequestType(t *testing.T) {
	for method, handler := range projectMethodMap {
		if _, err := handler(context.Background(), nil, "not-a-request"); err == nil {
			t.Errorf("%s accepted a wrong-typed request", method)
		}
	}
}

// =============================================================================
// Method handlers — happy paths and server errors
// =============================================================================

func TestProjectHandler_Create(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "Create",
		&project.CreateProjectRequest{
			TenantID:         "tenant-1",
			Name:             "quicbaas",
			Description:      "self hosting",
			WorkspaceAgentID: "agent-1",
			WorkspacePath:    "/home/agent/src/go-quicbaas",
			HostAffinity:     "host-a",
			DefaultLeadAgent: "agent-lead",
			GitRepoURL:       "https://git.example.test/go-quicbaas.git",
			GitDefaultBranch: "main",
			TokenBudget:      1000,
			GPUMinutes:       60,
			MaxConcurrency:   4,
			Metadata:         map[string]string{"team": "platform"},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, ok := resp.(*project.ProjectInfo)
	if !ok {
		t.Fatalf("wrong response type: %T", resp)
	}
	if info.ID != "proj-1" || info.Name != "quicbaas" {
		t.Fatalf("unexpected project: %+v", info)
	}

	// The outbound conversion must carry the flattened nested fields.
	got := fake.lastCreate
	if got.WorkspaceAgentId != "agent-1" || got.WorkspacePath != "/home/agent/src/go-quicbaas" {
		t.Errorf("workspace lost on the wire: %+v", got)
	}
	if got.GitRepoUrl != "https://git.example.test/go-quicbaas.git" || got.GitDefaultBranch != "main" {
		t.Errorf("git config lost on the wire: %+v", got)
	}
	if got.TokenBudget != 1000 || got.GpuMinutes != 60 || got.MaxConcurrency != 4 {
		t.Errorf("quotas lost on the wire: %+v", got)
	}
	if got.Metadata["team"] != "platform" {
		t.Errorf("metadata lost on the wire: %+v", got.Metadata)
	}
}

func TestProjectHandler_Create_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "Create",
		&project.CreateProjectRequest{TenantID: "tenant-1", Name: "quicbaas"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestProjectHandler_Get(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "Get",
		&project.GetProjectRequest{TenantID: "tenant-1", ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.(*project.ProjectInfo).ID != "proj-1" {
		t.Fatalf("unexpected project: %+v", resp)
	}
	if fake.lastGet.TenantId != "tenant-1" || fake.lastGet.ProjectId != "proj-1" {
		t.Errorf("request fields lost: %+v", fake.lastGet)
	}
}

func TestProjectHandler_Get_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "Get",
		&project.GetProjectRequest{TenantID: "tenant-1", ProjectID: "proj-1"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestProjectHandler_GetByName(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "GetByName",
		&project.GetProjectByNameRequest{TenantID: "tenant-1", Name: "quicbaas"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.(*project.ProjectInfo).Name != "quicbaas" {
		t.Fatalf("unexpected project: %+v", resp)
	}
	if fake.lastByName.TenantId != "tenant-1" || fake.lastByName.Name != "quicbaas" {
		t.Errorf("request fields lost: %+v", fake.lastByName)
	}
}

func TestProjectHandler_GetByName_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "GetByName",
		&project.GetProjectByNameRequest{TenantID: "tenant-1", Name: "quicbaas"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestProjectHandler_Update(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "Update",
		&project.UpdateProjectRequest{
			TenantID:         "tenant-1",
			ProjectID:        "proj-1",
			Name:             "renamed",
			ClearDescription: true,
			WorkspacePath:    "/srv/ws",
			GitCurrentSHA:    "abc123",
			AgentRoles:       map[string]project.ProjectRole{"agent-2": project.ProjectRoleWorker},
			MaxConcurrency:   8,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.(*project.ProjectInfo).ID != "proj-1" {
		t.Fatalf("unexpected project: %+v", resp)
	}
	got := fake.lastUpdate
	if !got.ClearDescription || got.Name != "renamed" || got.WorkspacePath != "/srv/ws" {
		t.Errorf("update fields lost: %+v", got)
	}
	if got.GitCurrentSha != "abc123" || got.MaxConcurrency != 8 {
		t.Errorf("update fields lost: %+v", got)
	}
	if got.AgentRoles["agent-2"] != "worker" {
		t.Errorf("agent roles lost: %+v", got.AgentRoles)
	}
}

func TestProjectHandler_Update_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "Update",
		&project.UpdateProjectRequest{TenantID: "tenant-1", ProjectID: "proj-1"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestProjectHandler_Delete(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "Delete",
		&project.DeleteProjectRequest{TenantID: "tenant-1", ProjectID: "proj-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := resp.(*project.DeleteProjectResponse)
	if !ok {
		t.Fatalf("wrong response type: %T", resp)
	}
	if !out.Deleted {
		t.Error("deleted flag lost in conversion")
	}
	if fake.lastDelete.ProjectId != "proj-1" {
		t.Errorf("request fields lost: %+v", fake.lastDelete)
	}
}

func TestProjectHandler_Delete_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "Delete",
		&project.DeleteProjectRequest{TenantID: "tenant-1", ProjectID: "proj-1"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

// TestProjectHandler_List is the direct regression for the reported bug:
// `quicbaas project list` over gRPC/Unix used to fail to marshal the plain SDK
// struct because no handler claimed the project service.
func TestProjectHandler_List(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "List",
		&project.ListProjectsRequest{TenantID: "tenant-1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := resp.(*project.ListProjectsResponse)
	if !ok {
		t.Fatalf("wrong response type: %T", resp)
	}
	if out.Total != 1 || len(out.Projects) != 1 || out.Projects[0].ID != "proj-1" {
		t.Fatalf("unexpected list response: %+v", out)
	}
	if fake.lastList.TenantId != "tenant-1" {
		t.Errorf("request fields lost: %+v", fake.lastList)
	}
}

func TestProjectHandler_List_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "List",
		&project.ListProjectsRequest{TenantID: "tenant-1"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

// TestProjectHandler_GrantAgentRole covers the outbound field mapping and the
// response conversion for the dedicated grant RPC.
func TestProjectHandler_GrantAgentRole(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "GrantAgentRole",
		&project.GrantAgentRoleRequest{
			TenantID: "tenant-1", ProjectID: "proj-1",
			AgentID: "agent-3", Role: project.ProjectRoleWorker,
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := resp.(*project.GrantAgentRoleResponse)
	if !ok {
		t.Fatalf("wrong response type: %T", resp)
	}
	if out.ProjectID != "proj-1" || out.AgentID != "agent-3" || out.Role != project.ProjectRoleWorker {
		t.Errorf("response fields lost: %+v", out)
	}
	if !out.Timestamp.Equal(projectUpdatedAt) {
		t.Errorf("timestamp = %s, want %s", out.Timestamp, projectUpdatedAt)
	}

	got := fake.lastGrant
	if got.TenantId != "tenant-1" || got.ProjectId != "proj-1" ||
		got.AgentId != "agent-3" || got.Role != "worker" {
		t.Errorf("request fields lost on the wire: %+v", got)
	}
}

// TestProjectHandler_GrantAgentRole_PreservesOtherAgents is the regression for
// the data-loss scenario the dedicated RPC exists to avoid: emulating the verb
// through UpdateProject would send the single-entry role map and REPLACE the
// project's whole map, silently deleting every other agent's role.
func TestProjectHandler_GrantAgentRole_PreservesOtherAgents(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)

	if _, err := h.handleRequest(context.Background(), "quicbaas.Project", "GrantAgentRole",
		&project.GrantAgentRoleRequest{
			TenantID: "tenant-1", ProjectID: "proj-1",
			AgentID: "agent-3", Role: project.ProjectRoleWorker,
		}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The grant must not have travelled as an UpdateProject.
	if fake.lastUpdate != nil {
		t.Fatalf("grant was emulated through UpdateProject: %+v", fake.lastUpdate)
	}
	want := map[string]string{"agent-1": "lead", "agent-2": "observer", "agent-3": "worker"}
	if len(fake.roles) != len(want) {
		t.Fatalf("role map = %v, want %v", fake.roles, want)
	}
	for agentID, role := range want {
		if fake.roles[agentID] != role {
			t.Errorf("role for %q = %q, want %q", agentID, fake.roles[agentID], role)
		}
	}
}

func TestProjectHandler_GrantAgentRole_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "GrantAgentRole",
		&project.GrantAgentRoleRequest{
			TenantID: "tenant-1", ProjectID: "proj-1",
			AgentID: "agent-1", Role: project.ProjectRoleLead,
		})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestProjectHandler_GrantAgentRole_WrongRequestType(t *testing.T) {
	_, err := handleProjectGrantAgentRole(context.Background(), nil,
		&project.RevokeAgentRoleRequest{TenantID: "tenant-1"})
	if err == nil {
		t.Fatal("expected an error for a wrong-typed request")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("error code = %s, want invalid input", errors.Code(err))
	}
}

// TestProjectHandler_RevokeAgentRole covers the outbound field mapping and the
// response conversion for the dedicated revoke RPC.
func TestProjectHandler_RevokeAgentRole(t *testing.T) {
	h, fake := newProjectHandlerWithServer(t, false)
	resp, err := h.handleRequest(context.Background(), "quicbaas.Project", "RevokeAgentRole",
		&project.RevokeAgentRoleRequest{TenantID: "tenant-1", ProjectID: "proj-1", AgentID: "agent-2"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := resp.(*project.RevokeAgentRoleResponse)
	if !ok {
		t.Fatalf("wrong response type: %T", resp)
	}
	if out.ProjectID != "proj-1" || out.AgentID != "agent-2" {
		t.Errorf("response fields lost: %+v", out)
	}
	if !out.Timestamp.Equal(projectUpdatedAt) {
		t.Errorf("timestamp = %s, want %s", out.Timestamp, projectUpdatedAt)
	}

	got := fake.lastRevoke
	if got.TenantId != "tenant-1" || got.ProjectId != "proj-1" || got.AgentId != "agent-2" {
		t.Errorf("request fields lost on the wire: %+v", got)
	}

	// Revoking agent-2 must not disturb agent-1.
	if fake.roles["agent-1"] != "lead" {
		t.Errorf("agent-1 role = %q, want lead", fake.roles["agent-1"])
	}
	if fake.roles["agent-2"] != string(project.ProjectRoleDenied) {
		t.Errorf("agent-2 role = %q, want denied", fake.roles["agent-2"])
	}
}

func TestProjectHandler_RevokeAgentRole_ServerError(t *testing.T) {
	h, _ := newProjectHandlerWithServer(t, true)
	_, err := h.handleRequest(context.Background(), "quicbaas.Project", "RevokeAgentRole",
		&project.RevokeAgentRoleRequest{TenantID: "tenant-1", ProjectID: "proj-1", AgentID: "agent-1"})
	if err == nil {
		t.Fatal("expected error from server")
	}
}

func TestProjectHandler_RevokeAgentRole_WrongRequestType(t *testing.T) {
	_, err := handleProjectRevokeAgentRole(context.Background(), nil,
		&project.GrantAgentRoleRequest{TenantID: "tenant-1"})
	if err == nil {
		t.Fatal("expected an error for a wrong-typed request")
	}
	if !errors.IsInvalidInput(err) {
		t.Errorf("error code = %s, want invalid input", errors.Code(err))
	}
}

// =============================================================================
// Converters
// =============================================================================

// TestProjectInfoFromProto_Fidelity asserts every nested struct survives the
// flat proto round trip.
func TestProjectInfoFromProto_Fidelity(t *testing.T) {
	info := projectInfoFromProto(sampleProtoProject())
	if info == nil {
		t.Fatal("conversion returned nil")
	}
	if info.ID != "proj-1" || info.TenantID != "tenant-1" || info.Name != "quicbaas" ||
		info.Description != "self hosting" || info.HostAffinity != "host-a" ||
		info.DefaultLeadAgent != "agent-lead" {
		t.Fatalf("scalar fields lost: %+v", info)
	}
	if info.Workspace.AgentID != "agent-1" || info.Workspace.Path != "/home/agent/src/go-quicbaas" {
		t.Errorf("workspace lost: %+v", info.Workspace)
	}
	if info.Git.RepoURL != "https://git.example.test/go-quicbaas.git" ||
		info.Git.DefaultBranch != "main" || info.Git.CurrentSHA != "abc123" {
		t.Errorf("git lost: %+v", info.Git)
	}
	if info.Quotas.TokenBudget != 1000 || info.Quotas.GPUMinutes != 60 || info.Quotas.MaxConcurrency != 4 {
		t.Errorf("quotas lost: %+v", info.Quotas)
	}
	if info.Permissions.AgentRoles["agent-1"] != project.ProjectRoleLead ||
		info.Permissions.AgentRoles["agent-2"] != project.ProjectRoleObserver {
		t.Errorf("permissions lost: %+v", info.Permissions.AgentRoles)
	}
	if info.Metadata["team"] != "platform" {
		t.Errorf("metadata lost: %+v", info.Metadata)
	}
	if !info.CreatedAt.Equal(projectCreatedAt) || !info.UpdatedAt.Equal(projectUpdatedAt) {
		t.Errorf("timestamps lost: created=%s updated=%s", info.CreatedAt, info.UpdatedAt)
	}
}

// TestProjectInfoFromProto_NilSafety covers the empty/absent cases: a nil
// message, absent nested maps, and unset timestamps (which must stay zero
// rather than becoming 1970-01-01).
func TestProjectInfoFromProto_NilSafety(t *testing.T) {
	if projectInfoFromProto(nil) != nil {
		t.Fatal("nil project must convert to nil")
	}
	info := projectInfoFromProto(&pb.Project{Id: "proj-1"})
	if info.Permissions.AgentRoles != nil {
		t.Errorf("absent agent roles became %v, want nil", info.Permissions.AgentRoles)
	}
	if info.Metadata != nil {
		t.Errorf("absent metadata became %v, want nil", info.Metadata)
	}
	if info.Workspace != (project.ProjectWorkspace{}) || info.Git != (project.ProjectGit{}) ||
		info.Quotas != (project.ProjectQuotas{}) {
		t.Errorf("absent nested structs are not zero: %+v", info)
	}
	if !info.CreatedAt.IsZero() || !info.UpdatedAt.IsZero() {
		t.Errorf("unset timestamps became epoch values: %+v", info)
	}
}

// TestProjectRolesConversionRoundTrip pins both directions of the role map,
// including nil preservation — a nil map means "no change" to the server,
// while an empty map means "clear".
func TestProjectRolesConversionRoundTrip(t *testing.T) {
	if projectRolesToProto(nil) != nil {
		t.Error("nil role map must convert to nil")
	}
	if projectRolesFromProto(nil) != nil {
		t.Error("nil proto role map must convert to nil")
	}
	if got := projectRolesToProto(map[string]project.ProjectRole{}); got == nil || len(got) != 0 {
		t.Errorf("empty role map = %v, want a non-nil empty map", got)
	}

	in := map[string]project.ProjectRole{
		"a": project.ProjectRoleLead,
		"b": project.ProjectRoleWorker,
		"c": project.ProjectRoleObserver,
		"d": project.ProjectRoleDenied,
	}
	out := projectRolesFromProto(projectRolesToProto(in))
	if len(out) != len(in) {
		t.Fatalf("round trip changed size: %d -> %d", len(in), len(out))
	}
	for agentID, role := range in {
		if out[agentID] != role {
			t.Errorf("role for %q = %q, want %q", agentID, out[agentID], role)
		}
	}
}
