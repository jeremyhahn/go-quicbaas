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

package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// toolResult marshals a server-side response type and decodes it back into the
// generic value the transport hands to transformResponse. Driving the tests off
// the real pkg/service types is what makes them a contract test: a tag change
// on either side breaks them.
func toolResult(t *testing.T, v interface{}) interface{} {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal server response: %v", err)
	}
	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal server response: %v", err)
	}
	return out
}

func sampleProject() *orchestration.Project {
	return &orchestration.Project{
		ID:               "proj-1",
		TenantID:         "acme",
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

// TestIsMCPProjectService pins the routing predicate.
func TestIsMCPProjectService(t *testing.T) {
	for _, name := range []string{"Project", "quicbaas.Project", "quicbaas.project.v1.Project"} {
		if !isMCPProjectService(name) {
			t.Errorf("isMCPProjectService(%q) = false", name)
		}
	}
	for _, name := range []string{"quicbaas.Orchestrator", "quicbaas.Account", "quicbaas.IAM.Users", ""} {
		if isMCPProjectService(name) {
			t.Errorf("isMCPProjectService(%q) = true", name)
		}
	}
}

// TestProjectMethodToolNames pins the SDK method -> MCP tool mapping against
// the names the server registers. Drift here surfaces at runtime as
// "tool not registered", which is the bug this mapping was added to fix.
func TestProjectMethodToolNames(t *testing.T) {
	want := map[string]string{
		"Create":          "project_create",
		"Get":             "project_get",
		"List":            "project_list",
		"Update":          "project_update",
		"Delete":          "project_delete",
		"GrantAgentRole":  "project_grant_agent",
		"RevokeAgentRole": "project_revoke_agent",
	}
	for method, expected := range want {
		got, ok := projectToolName(method)
		if !ok || got != expected {
			t.Errorf("%s -> %q (ok=%v), want %q", method, got, ok, expected)
		}
	}
	if len(projectMethodTools) != len(want) {
		t.Errorf("tool map has %d entries, want %d", len(projectMethodTools), len(want))
	}

	// GetByName has no server-side tool; it must stay unmapped so the transport
	// reports Unimplemented instead of calling a tool that does not exist.
	if _, ok := projectToolName("GetByName"); ok {
		t.Error("GetByName must not be mapped until the server registers a by-name tool")
	}

	// Every mapped method needs a response transform, or the call would succeed
	// on the wire and then fail to decode.
	for method := range projectMethodTools {
		if _, ok := projectMCPTransforms[method]; !ok {
			t.Errorf("%s has a tool name but no response transform", method)
		}
	}
}

// TestProjectMCPToolNameDerivation checks the transport resolves the Project
// service through the mapping rather than the generic quicbaas_<svc>_<method>
// derivation.
func TestProjectMCPToolNameDerivation(t *testing.T) {
	tr := &Transport{}
	for method, want := range map[string]string{
		"Create":          "project_create",
		"GrantAgentRole":  "project_grant_agent",
		"RevokeAgentRole": "project_revoke_agent",
	} {
		if got := tr.mapToToolName("quicbaas.Project", method); got != want {
			t.Errorf("mapToToolName(Project, %s) = %q, want %q", method, got, want)
		}
	}
}

// TestProjectMCPTransformResponse_HappyPaths drives every transform against the
// exact JSON the server-side tools emit.
func TestProjectMCPTransformResponse_HappyPaths(t *testing.T) {
	h := newProjectMCPHandler()
	proj := sampleProject()

	envelopes := map[string]interface{}{
		"Create": toolResult(t, &service.CreateProjectResponse{Project: proj, Timestamp: proj.CreatedAt}),
		"Get":    toolResult(t, &service.GetProjectResponse{Project: proj, Timestamp: proj.CreatedAt}),
		"Update": toolResult(t, &service.UpdateProjectResponse{Project: proj, Timestamp: proj.UpdatedAt}),
	}
	for method, result := range envelopes {
		out, err := h.transformResponse(method, result)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		info, ok := out.(*project.ProjectInfo)
		if !ok {
			t.Fatalf("%s decoded to %T", method, out)
		}
		if info.ID != "proj-1" || info.TenantID != "acme" || info.Name != "web-api" {
			t.Errorf("%s lost identity fields: %#v", method, info)
		}
		if info.Workspace.AgentID != "agent-1" || info.Workspace.Path != "/srv/web-api" {
			t.Errorf("%s lost workspace: %#v", method, info.Workspace)
		}
		if info.HostAffinity != "builder-3" || info.DefaultLeadAgent != "agent-1" {
			t.Errorf("%s lost affinity fields: %#v", method, info)
		}
		if info.Git.RepoURL == "" || info.Git.DefaultBranch != "main" || info.Git.CurrentSHA != "abc123" {
			t.Errorf("%s lost git metadata: %#v", method, info.Git)
		}
		if info.Permissions.AgentRoles["agent-1"] != project.ProjectRoleLead {
			t.Errorf("%s lost the role matrix: %#v", method, info.Permissions)
		}
		if info.Quotas.TokenBudget != 1000 || info.Quotas.GPUMinutes != 60 || info.Quotas.MaxConcurrency != 4 {
			t.Errorf("%s lost quotas: %#v", method, info.Quotas)
		}
		if info.Metadata["env"] != "prod" {
			t.Errorf("%s lost metadata: %#v", method, info.Metadata)
		}
		if !info.CreatedAt.Equal(proj.CreatedAt) || !info.UpdatedAt.Equal(proj.UpdatedAt) {
			t.Errorf("%s lost timestamps: %v / %v", method, info.CreatedAt, info.UpdatedAt)
		}
	}

	out, err := h.transformResponse("List", toolResult(t, &service.ListProjectsResponse{
		Projects: []*orchestration.Project{proj},
		Total:    1,
	}))
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	list, ok := out.(*project.ListProjectsResponse)
	if !ok || list.Total != 1 || len(list.Projects) != 1 || list.Projects[0].ID != "proj-1" {
		t.Fatalf("List decoded to %#v", out)
	}

	out, err = h.transformResponse("Delete", toolResult(t, &service.DeleteProjectResponse{
		ProjectID: "proj-1",
		Deleted:   true,
	}))
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if del, ok := out.(*project.DeleteProjectResponse); !ok || !del.Deleted {
		t.Fatalf("Delete decoded to %#v", out)
	}

	stamp := time.Unix(1700000200, 0).UTC()
	out, err = h.transformResponse("GrantAgentRole", toolResult(t, &service.GrantAgentRoleResponse{
		ProjectID: "proj-1",
		AgentID:   "agent-2",
		Role:      orchestration.ProjectRoleWorker,
		Timestamp: stamp,
	}))
	if err != nil {
		t.Fatalf("GrantAgentRole: %v", err)
	}
	grant, ok := out.(*project.GrantAgentRoleResponse)
	if !ok {
		t.Fatalf("GrantAgentRole decoded to %T", out)
	}
	if grant.ProjectID != "proj-1" || grant.AgentID != "agent-2" ||
		grant.Role != project.ProjectRoleWorker || !grant.Timestamp.Equal(stamp) {
		t.Errorf("GrantAgentRole decoded to %#v", grant)
	}

	out, err = h.transformResponse("RevokeAgentRole", toolResult(t, &service.RevokeAgentRoleResponse{
		ProjectID: "proj-1",
		AgentID:   "agent-2",
		Timestamp: stamp,
	}))
	if err != nil {
		t.Fatalf("RevokeAgentRole: %v", err)
	}
	revoke, ok := out.(*project.RevokeAgentRoleResponse)
	if !ok {
		t.Fatalf("RevokeAgentRole decoded to %T", out)
	}
	if revoke.ProjectID != "proj-1" || revoke.AgentID != "agent-2" || !revoke.Timestamp.Equal(stamp) {
		t.Errorf("RevokeAgentRole decoded to %#v", revoke)
	}
}

// TestProjectMCPTransformResponse_ErrorPaths covers the failure modes.
func TestProjectMCPTransformResponse_ErrorPaths(t *testing.T) {
	h := newProjectMCPHandler()

	err := func() error {
		_, err := h.transformResponse("GetByName", map[string]interface{}{})
		return err
	}()
	if err == nil {
		t.Fatal("GetByName must fail: no MCP tool implements it")
	}
	if !sdkerrors.IsUnimplemented(err) {
		t.Errorf("GetByName error must be Unimplemented, got %v", sdkerrors.Code(err))
	}

	if _, err := h.transformResponse("Teleport", map[string]interface{}{}); err == nil {
		t.Error("an unsupported method must fail")
	}
	if _, err := h.transformResponse("Get", nil); err == nil {
		t.Error("an empty tool result must fail")
	}
	if _, err := h.transformResponse("Get", map[string]interface{}{}); err == nil {
		t.Error("an envelope with no project must fail")
	}
	if _, err := h.transformResponse("Get", map[string]interface{}{"project": "not-an-object"}); err == nil {
		t.Error("a malformed project envelope must fail")
	}
	if _, err := h.transformResponse("List", map[string]interface{}{"projects": "not-an-array"}); err == nil {
		t.Error("a malformed list envelope must fail")
	}
	if _, err := h.transformResponse("GrantAgentRole", map[string]interface{}{"Role": 42}); err == nil {
		t.Error("a malformed role envelope must fail")
	}

	out, err := h.transformResponse("List", map[string]interface{}{"Total": 0})
	if err != nil {
		t.Fatalf("List with no projects: %v", err)
	}
	if list, ok := out.(*project.ListProjectsResponse); !ok || list.Projects == nil {
		t.Fatalf("List must normalize a missing array to an empty slice; got %#v", out)
	}
}

// TestProjectMCPUnsupportedMethodError pins the operation name carried by the
// Unimplemented error so callers can tell which transport refused.
func TestProjectMCPUnsupportedMethodError(t *testing.T) {
	err := newProjectUnsupportedError("GetByName")
	qb, ok := err.(*sdkerrors.QBError)
	if !ok {
		t.Fatalf("expected *QBError, got %T", err)
	}
	if qb.Code != sdkerrors.CodeUnimplemented {
		t.Errorf("code = %v, want Unimplemented", qb.Code)
	}
	if qb.Op != "mcp.project.GetByName" {
		t.Errorf("op = %q, want mcp.project.GetByName", qb.Op)
	}
}
