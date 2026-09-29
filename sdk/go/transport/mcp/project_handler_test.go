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
	"reflect"
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
		Workspace:        orchestration.ProjectWorkspace{FleetNodeID: "agent-1", Path: "/srv/web-api"},
		HostAffinity:     "builder-3",
		DefaultLeadAgent: orchestration.AgentRefTo("agent-1"),
		Git:              orchestration.ProjectGit{RepoURL: "https://example.com/r.git", DefaultBranch: "main", CurrentSHA: "abc123"},
		Permissions: orchestration.ProjectPermissions{
			FleetNodeRoles: map[string]orchestration.ProjectRole{"agent-1": orchestration.ProjectRoleLead},
		},
		Quotas: orchestration.ProjectQuotas{GPUMinutes: 60, MaxConcurrency: 4},
		Spend: orchestration.SpendLimits{Windows: []orchestration.QuotaWindow{
			{Name: "daily", Size: 24 * time.Hour, MaxTokens: 1000},
		}},
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
		"Create":              "project_create",
		"Get":                 "project_get",
		"List":                "project_list",
		"Update":              "project_update",
		"Delete":              "project_delete",
		"GrantFleetNodeRole":  "project_grant_fleet_node",
		"RevokeFleetNodeRole": "project_revoke_fleet_node",
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
		"Create":              "project_create",
		"GrantFleetNodeRole":  "project_grant_fleet_node",
		"RevokeFleetNodeRole": "project_revoke_fleet_node",
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
		if info.Workspace.FleetNodeID != "agent-1" || info.Workspace.Path != "/srv/web-api" {
			t.Errorf("%s lost workspace: %#v", method, info.Workspace)
		}
		if info.HostAffinity != "builder-3" || info.DefaultLeadAgent.AgentID != "agent-1" {
			t.Errorf("%s lost affinity fields: %#v", method, info)
		}
		if info.Git.RepoURL == "" || info.Git.DefaultBranch != "main" || info.Git.CurrentSHA != "abc123" {
			t.Errorf("%s lost git metadata: %#v", method, info.Git)
		}
		if info.Permissions.FleetNodeRoles["agent-1"] != project.ProjectRoleLead {
			t.Errorf("%s lost the role matrix: %#v", method, info.Permissions)
		}
		if len(info.Spend.Windows) != 1 || info.Spend.Windows[0].MaxTokens != 1000 || info.Spend.Windows[0].Size != 24*time.Hour ||
			info.Quotas.GPUMinutes != 60 || info.Quotas.MaxConcurrency != 4 {
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
	out, err = h.transformResponse("GrantFleetNodeRole", toolResult(t, &service.GrantFleetNodeRoleResponse{
		ProjectID:   "proj-1",
		FleetNodeID: "agent-2",
		Role:        orchestration.ProjectRoleWorker,
		Timestamp:   stamp,
	}))
	if err != nil {
		t.Fatalf("GrantFleetNodeRole: %v", err)
	}
	grant, ok := out.(*project.GrantFleetNodeRoleResponse)
	if !ok {
		t.Fatalf("GrantFleetNodeRole decoded to %T", out)
	}
	if grant.ProjectID != "proj-1" || grant.FleetNodeID != "agent-2" ||
		grant.Role != project.ProjectRoleWorker || !grant.Timestamp.Equal(stamp) {
		t.Errorf("GrantFleetNodeRole decoded to %#v", grant)
	}

	out, err = h.transformResponse("RevokeFleetNodeRole", toolResult(t, &service.RevokeFleetNodeRoleResponse{
		ProjectID:   "proj-1",
		FleetNodeID: "agent-2",
		Timestamp:   stamp,
	}))
	if err != nil {
		t.Fatalf("RevokeFleetNodeRole: %v", err)
	}
	revoke, ok := out.(*project.RevokeFleetNodeRoleResponse)
	if !ok {
		t.Fatalf("RevokeFleetNodeRole decoded to %T", out)
	}
	if revoke.ProjectID != "proj-1" || revoke.FleetNodeID != "agent-2" || !revoke.Timestamp.Equal(stamp) {
		t.Errorf("RevokeFleetNodeRole decoded to %#v", revoke)
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
	if _, err := h.transformResponse("GrantFleetNodeRole", map[string]interface{}{"Role": 42}); err == nil {
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

// TestProjectMCPTransformResponse_CarriesGitIdentity: the MCP tool result is
// the service's project record, whose nested git metadata must land on the
// SDK's nested shape unchanged.
func TestProjectMCPTransformResponse_CarriesGitIdentity(t *testing.T) {
	h := newProjectMCPHandler()
	result := map[string]interface{}{
		"project": map[string]interface{}{
			"id": "p1", "tenant_id": "t1", "name": "api",
			"default_workflow": "sdlc",
			"git": map[string]interface{}{
				"repo_url": "ssh://git@forge.example.test/acme/api.git",
				"author":   map[string]interface{}{"name": "agent-api", "email": "agent-api@example.test"},
				"signing": map[string]interface{}{
					"enabled": true, "algorithm": "ECDSA-P256",
					"key": map[string]interface{}{"name": "api-signing", "scope": "tenant", "owner": "t1"},
				},
				"ssh_key": map[string]interface{}{"name": "api-access", "scope": "service", "owner": "deploy"},
			},
		},
	}
	out, err := h.transformResponse("Get", result)
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	info, ok := out.(*project.ProjectInfo)
	if !ok {
		t.Fatalf("wrong type %T", out)
	}
	want := project.ProjectGit{
		RepoURL: "ssh://git@forge.example.test/acme/api.git",
		Author:  project.GitAuthor{Name: "agent-api", Email: "agent-api@example.test"},
		Signing: project.CommitSigning{
			Enabled: true, Algorithm: "ECDSA-P256",
			Key: project.KMSKeyRef{Name: "api-signing", Scope: "tenant", Owner: "t1"},
		},
		SSHKey: project.KMSKeyRef{Name: "api-access", Scope: "service", Owner: "deploy"},
	}
	if info.DefaultWorkflow != "sdlc" || !reflect.DeepEqual(info.Git, want) {
		t.Errorf("git identity lost in the MCP transform:\n got %+v\nwant %+v", info.Git, want)
	}
}

// =============================================================================
// Autonomy + context repositories over MCP
// =============================================================================

// TestProjectMCPArgumentsCarryAutonomyAndContextRepos asserts the tool
// arguments the transport derives from the SDK request match the names the
// server's project tools declare. A renamed key would be silently ignored.
func TestProjectMCPArgumentsCarryAutonomyAndContextRepos(t *testing.T) {
	tr := &Transport{}

	createArgs := tr.convertToArguments(&project.CreateProjectRequest{
		TenantID: "t1", Name: "api",
		Autonomous: true,
		ContextRepos: []project.ContextRepo{
			{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
		},
	})
	if createArgs["autonomous"] != true {
		t.Errorf("create autonomous = %v, want true", createArgs["autonomous"])
	}
	repos, ok := createArgs["context_repos"].([]interface{})
	if !ok || len(repos) != 1 {
		t.Fatalf("create context_repos = %#v, want one element", createArgs["context_repos"])
	}
	repo, ok := repos[0].(map[string]interface{})
	if !ok {
		t.Fatalf("context_repos[0] = %#v, want an object", repos[0])
	}
	for key, want := range map[string]string{
		"name":     "handbook",
		"repo_url": "https://forge.example/org/handbook",
		"ref":      "main",
	} {
		if repo[key] != want {
			t.Errorf("context_repos[0][%q] = %v, want %q", key, repo[key], want)
		}
	}

	// An unset switch must not appear at all, so the tool reads it as
	// "unchanged" rather than "off".
	unchanged := tr.convertToArguments(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1"})
	if _, present := unchanged["autonomous"]; present {
		t.Error("an unset autonomous appeared in the tool arguments")
	}
	if _, present := unchanged["context_repos"]; present {
		t.Error("an unset context_repos appeared in the tool arguments")
	}

	off := false
	updateArgs := tr.convertToArguments(&project.UpdateProjectRequest{
		TenantID: "t1", ProjectID: "p1", Autonomous: &off, ClearContextRepos: true,
	})
	if updateArgs["autonomous"] != false {
		t.Errorf("update autonomous = %v, want false", updateArgs["autonomous"])
	}
	if updateArgs["clear_context_repos"] != true {
		t.Errorf("update clear_context_repos = %v, want true", updateArgs["clear_context_repos"])
	}
}

// TestProjectMCPTransformResponse_CarriesAutonomyAndContextRepos drives the
// real server envelopes for create → get and update → get through the
// transform, asserting both fields reach the SDK type.
func TestProjectMCPTransformResponse_CarriesAutonomyAndContextRepos(t *testing.T) {
	h := newProjectMCPHandler()

	stored := sampleProject()
	stored.Autonomous = true
	stored.ContextRepos = []orchestration.ContextRepo{
		{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"},
	}

	decode := func(method string, envelope interface{}) *project.ProjectInfo {
		t.Helper()
		out, err := h.transformResponse(method, toolResult(t, envelope))
		if err != nil {
			t.Fatalf("transformResponse(%s): %v", method, err)
		}
		info, ok := out.(*project.ProjectInfo)
		if !ok {
			t.Fatalf("transformResponse(%s) returned %T", method, out)
		}
		return info
	}

	created := decode("Create", &service.CreateProjectResponse{Project: stored, Timestamp: time.Now()})
	if !created.Autonomous {
		t.Error("Autonomous lost in the create transform")
	}
	if len(created.ContextRepos) != 1 || created.ContextRepos[0] !=
		(project.ContextRepo{Name: "handbook", RepoURL: "https://forge.example/org/handbook", Ref: "main"}) {
		t.Fatalf("ContextRepos lost in the create transform: %+v", created.ContextRepos)
	}

	got := decode("Get", &service.GetProjectResponse{Project: stored, Timestamp: time.Now()})
	if !got.Autonomous || len(got.ContextRepos) != 1 {
		t.Fatalf("create did not survive a get: %+v", got)
	}

	// The update the tool applied: autonomy off, one different repository.
	stored.Autonomous = false
	stored.ContextRepos = []orchestration.ContextRepo{
		{Name: "specs", RepoURL: "https://forge.example/org/specs", Ref: "v2"},
	}
	updated := decode("Update", &service.UpdateProjectResponse{Project: stored, Timestamp: time.Now()})
	if updated.Autonomous {
		t.Error("Autonomous is still on after an update that turned it off")
	}
	if len(updated.ContextRepos) != 1 || updated.ContextRepos[0].Name != "specs" {
		t.Fatalf("ContextRepos = %+v, want exactly the specs repo", updated.ContextRepos)
	}

	// And a cleared list comes back empty rather than stale.
	stored.ContextRepos = nil
	cleared := decode("Get", &service.GetProjectResponse{Project: stored, Timestamp: time.Now()})
	if len(cleared.ContextRepos) != 0 {
		t.Fatalf("ContextRepos = %+v after clear, want none", cleared.ContextRepos)
	}
}

// TestProjectMCP_ParkSwitchAndRecordCrossTheWire asserts the park switch
// reaches the project_update tool under the name the tool declares, stays
// absent when unset, and that the park record decodes off the server's
// envelope.
func TestProjectMCP_ParkSwitchAndRecordCrossTheWire(t *testing.T) {
	tr := &Transport{}
	if _, present := tr.convertToArguments(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1"})["parked"]; present {
		t.Error("an unset parked appeared in the tool arguments")
	}
	park := true
	if args := tr.convertToArguments(&project.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1", Parked: &park}); args["parked"] != true {
		t.Errorf("update parked = %v, want true", args["parked"])
	}

	stored := sampleProject()
	parkedAt := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	stored.Park("ops", parkedAt)
	out, err := newProjectMCPHandler().transformResponse("Update",
		toolResult(t, &service.UpdateProjectResponse{Project: stored, Timestamp: time.Now()}))
	if err != nil {
		t.Fatalf("transformResponse: %v", err)
	}
	info, ok := out.(*project.ProjectInfo)
	if !ok {
		t.Fatalf("transformResponse returned %T", out)
	}
	if !info.Parked || !info.ParkedAt.Equal(parkedAt) || info.ParkedBy != "ops" {
		t.Fatalf("decoded = %v %v %q, want the park record", info.Parked, info.ParkedAt, info.ParkedBy)
	}
}
