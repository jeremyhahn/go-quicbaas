//go:build mcp

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
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// projectToolExecutors names the executor behind each project tool, so the
// schema test below can read the argument keys straight out of its source.
var projectToolExecutors = map[string]string{
	"project_create":      "executeProjectCreate",
	"project_get":         "executeProjectGet",
	"project_get_by_name": "executeProjectGetByName",
	"project_list":        "executeProjectList",
	"project_update":      "executeProjectUpdate",
	"project_delete":      "executeProjectDelete",
}

// argumentKeysRead returns every literal key an executor in project_tools.go
// reads with a getX(args, "key") call.
func argumentKeysRead(t *testing.T, funcName string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "project_tools.go", nil, 0)
	require.NoError(t, err)

	var keys []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != funcName {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			if ident, ok := call.Args[0].(*ast.Ident); !ok || ident.Name != "args" {
				return true
			}
			lit, ok := call.Args[1].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, err := strconv.Unquote(lit.Value)
			require.NoError(t, err)
			keys = append(keys, key)
			return true
		})
		return keys
	}
	t.Fatalf("executor %s is not in project_tools.go", funcName)
	return nil
}

// TestProjectToolSchemas_AdvertiseEveryKeyTheExecutorReads: a key an executor
// reads but its schema omits is invisible to every MCP client, so the tool
// silently ignores an input no client knows to send. project_update read
// agent_roles, spend, the quotas and four more keys it never advertised
// (go-quicbaas#1261).
func TestProjectToolSchemas_AdvertiseEveryKeyTheExecutorReads(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	schemas := map[string]map[string]interface{}{}
	for _, tool := range reg.ListTools() {
		if props, ok := tool.InputSchema["properties"].(map[string]interface{}); ok {
			schemas[tool.Name] = props
		}
	}

	for tool, executor := range projectToolExecutors {
		t.Run(tool, func(t *testing.T) {
			props, ok := schemas[tool]
			require.True(t, ok, "tool %s is not registered", tool)
			keys := argumentKeysRead(t, executor)
			require.NotEmpty(t, keys, "%s reads no arguments; the scan is broken", executor)
			for _, key := range keys {
				assert.Contains(t, props, key, "%s reads %q but its schema does not advertise it", tool, key)
			}
		})
	}
}

// TestExecuteProjectCreate_HonoursRolesMetadataAndManagedWorkspace: the create
// tool used to drop agent_roles and metadata, and had no way to request a
// managed workspace.
func TestExecuteProjectCreate_HonoursRolesMetadataAndManagedWorkspace(t *testing.T) {
	var captured *service.CreateProjectRequest
	reg := newProjectAdminRegistry(t, &mockProjectService{
		createFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			captured = req
			return &service.CreateProjectResponse{}, nil
		},
	}, nil)

	_, err := reg.executeProjectTool(context.Background(), "project_create", map[string]interface{}{
		"tenant_id":                 "t1",
		"name":                      "api",
		"git_repo_url":              "https://forge.example/org/api",
		"agent_roles":               map[string]interface{}{"node-1": "lead", "node-2": 7},
		"metadata":                  map[string]interface{}{"team": "core"},
		"workspace_managed":         true,
		"git_credential_secret_ref": "forge-token",
		"git_credential_username":   "x-access-token",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)

	assert.Equal(t, map[string]orchestration.ProjectRole{"node-1": orchestration.ProjectRoleLead},
		captured.FleetNodeRoles, "a non-string role is skipped, a string role is kept")
	assert.Equal(t, map[string]string{"team": "core"}, captured.Metadata)
	assert.True(t, captured.WorkspaceManaged)
	assert.Equal(t, "forge-token", captured.GitCredentialSecretRef)
	assert.Equal(t, "x-access-token", captured.GitCredentialUsername)
}

// TestExecuteProjectCreate_RolesAndMetadataAbsent: omitting the maps leaves
// them nil rather than empty, so the service applies its defaults.
func TestExecuteProjectCreate_RolesAndMetadataAbsent(t *testing.T) {
	var captured *service.CreateProjectRequest
	reg := newProjectAdminRegistry(t, &mockProjectService{
		createFunc: func(_ context.Context, req *service.CreateProjectRequest) (*service.CreateProjectResponse, error) {
			captured = req
			return &service.CreateProjectResponse{}, nil
		},
	}, nil)

	_, err := reg.executeProjectTool(context.Background(), "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api", "agent_roles": "lead",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Nil(t, captured.FleetNodeRoles, "a non-object agent_roles must not become an empty role matrix")
	assert.Nil(t, captured.Metadata)
	assert.False(t, captured.WorkspaceManaged)
}

// TestExecuteProjectGetByName_ResolvesThroughTheService drives the new
// by-name tool against the real project service.
func TestExecuteProjectGetByName_ResolvesThroughTheService(t *testing.T) {
	svc := service.NewProjectService(orchestration.NewInMemoryProjectStore())
	reg := newProjectAdminRegistry(t, svc, nil)
	ctx := context.Background()

	created, err := svc.CreateProject(ctx, &service.CreateProjectRequest{
		TenantID: "t1", Name: "my app", WorkspacePath: "/ws/app",
	})
	require.NoError(t, err)

	result, err := reg.executeProjectTool(ctx, "project_get_by_name", map[string]interface{}{
		"tenant_id": "t1", "name": "my app",
	})
	require.NoError(t, err)
	resp, ok := result.(*service.GetProjectResponse)
	require.True(t, ok, "result is %T", result)
	require.NotNil(t, resp.Project)
	assert.Equal(t, created.Project.ID, resp.Project.ID)
	assert.Equal(t, "/ws/app", resp.Project.Workspace.Path)

	_, err = reg.executeProjectTool(ctx, "project_get_by_name", map[string]interface{}{
		"tenant_id": "t1", "name": "missing",
	})
	var notFound *service.ProjectNotFoundError
	require.ErrorAs(t, err, &notFound, "an unknown name must surface the typed not-found error")
}

// TestExecuteProjectTool_AuthzErrorsStayTyped: the MCP executors hand service
// errors to the tool registry, which classifies them; the typed authorization
// errors must reach it intact rather than flattened into a message.
func TestExecuteProjectTool_AuthzErrorsStayTyped(t *testing.T) {
	cases := []struct {
		name  string
		err   error
		check func(error) bool
	}{
		{"access denied", &service.ProjectAccessDeniedError{ProjectID: "p1", FleetNodeID: "n1"},
			func(err error) bool { var e *service.ProjectAccessDeniedError; return errors.As(err, &e) }},
		{"unresolved", &service.ProjectUnresolvedError{TenantID: "t1", ProjectID: "p1"},
			func(err error) bool { var e *service.ProjectUnresolvedError; return errors.As(err, &e) }},
		{"invalid role", &service.ProjectRoleInvalidError{Role: "boss"},
			func(err error) bool { var e *service.ProjectRoleInvalidError; return errors.As(err, &e) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reg := newProjectAdminRegistry(t, &mockProjectService{
				getFunc: func(context.Context, *service.GetProjectRequest) (*service.GetProjectResponse, error) {
					return nil, tc.err
				},
				getByNameFunc: func(context.Context, *service.GetProjectByNameRequest) (*service.GetProjectResponse, error) {
					return nil, tc.err
				},
				updateFunc: func(context.Context, *service.UpdateProjectRequest) (*service.UpdateProjectResponse, error) {
					return nil, tc.err
				},
			}, nil)
			for _, tool := range []string{"project_get", "project_get_by_name", "project_update"} {
				_, err := reg.executeProjectTool(context.Background(), tool, map[string]interface{}{
					"tenant_id": "t1", "project_id": "p1", "name": "p1",
				})
				assert.True(t, tc.check(err), "%s: %v lost its type", tool, err)
			}
		})
	}
}
