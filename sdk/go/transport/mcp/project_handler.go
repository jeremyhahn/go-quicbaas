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
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isMCPProjectService reports whether service names the Project service.
func isMCPProjectService(service string) bool {
	svc := service
	if idx := strings.LastIndex(service, "."); idx >= 0 {
		svc = service[idx+1:]
	}
	return svc == "Project"
}

// projectMethodTools maps SDK Project methods to the MCP tool names the server
// registers. The lifecycle tools live in pkg/api/mcp/project_tools.go and the
// two role tools in pkg/api/mcp/fleet_admin_tools.go. Both groups name the tool
// after the resource and operation (project_grant_fleet_node), not the RPC
// (GrantFleetNodeRole), and neither carries the quicbaas_ prefix, so the
// transport's generic service_method derivation would ask for tools that do
// not exist.
var projectMethodTools = map[string]string{
	"Create":              "project_create",
	"Get":                 "project_get",
	"GetByName":           "project_get_by_name",
	"List":                "project_list",
	"Update":              "project_update",
	"Delete":              "project_delete",
	"GrantFleetNodeRole":  "project_grant_fleet_node",
	"RevokeFleetNodeRole": "project_revoke_fleet_node",
}

// projectToolName returns the MCP tool backing an SDK Project method and
// whether the server registers one.
func projectToolName(method string) (string, bool) {
	name, ok := projectMethodTools[method]
	return name, ok
}

// newProjectUnsupportedError reports a Project method the MCP transport cannot
// serve because no server-side tool implements it.
func newProjectUnsupportedError(method string) error {
	return errors.NewUnimplemented("mcp.project."+method,
		"project method is not supported by the MCP transport")
}

// projectMCPHandler converts MCP tool results into the typed SDK project
// responses.
//
// Every project tool serializes a pkg/service response type verbatim. Those
// types carry no JSON tags, so their Go field names ARE the wire keys:
// {"Project": {...}, "Timestamp": ...}. Single-word keys (Project, Projects,
// Total, Deleted) decode through encoding/json's case-insensitive field match,
// but the multi-word ones (ProjectID, FleetNodeID) do not — "projectid" is not
// "project_id" — so the role envelope names them exactly as emitted. The
// nested project object is a pkg/orchestration.Project, which IS tagged
// snake_case at source and therefore decodes straight into
// project.ProjectInfo.
type projectMCPHandler struct{}

// newProjectMCPHandler creates a new project MCP response handler.
func newProjectMCPHandler() *projectMCPHandler {
	return &projectMCPHandler{}
}

// projectMCPTransform decodes one MCP tool result into its SDK type.
type projectMCPTransform func(raw []byte) (interface{}, error)

// projectMCPTransforms gives O(1) dispatch from method to result decoder.
var projectMCPTransforms = map[string]projectMCPTransform{
	"Create":              decodeMCPProjectEnvelope,
	"Get":                 decodeMCPProjectEnvelope,
	"GetByName":           decodeMCPProjectEnvelope,
	"Update":              decodeMCPProjectEnvelope,
	"List":                decodeMCPProjectList,
	"Delete":              decodeMCPProjectDelete,
	"GrantFleetNodeRole":  decodeMCPProjectGrant,
	"RevokeFleetNodeRole": decodeMCPProjectRevoke,
}

// transformResponse converts the raw MCP tool result to the SDK type for
// method.
func (h *projectMCPHandler) transformResponse(method string, result interface{}) (interface{}, error) {
	transform, ok := projectMCPTransforms[method]
	if !ok {
		return nil, newProjectUnsupportedError(method)
	}
	if result == nil {
		return nil, errors.NewInternal("mcp.project."+method, "empty tool result")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.project."+method,
			"failed to re-encode tool result")
	}
	return transform(raw)
}

func decodeMCPProjectEnvelope(raw []byte) (interface{}, error) {
	var env struct {
		Project *project.ProjectInfo `json:"project"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.project",
			"failed to decode project result")
	}
	if env.Project == nil {
		return nil, errors.NewInternal("mcp.project", "tool returned no project")
	}
	return env.Project, nil
}

func decodeMCPProjectList(raw []byte) (interface{}, error) {
	var env struct {
		Projects []*project.ProjectInfo `json:"projects"`
		Total    int                    `json:"total"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.project",
			"failed to decode project list result")
	}
	if env.Projects == nil {
		env.Projects = []*project.ProjectInfo{}
	}
	return &project.ListProjectsResponse{Projects: env.Projects, Total: env.Total}, nil
}

func decodeMCPProjectDelete(raw []byte) (interface{}, error) {
	var env struct {
		Deleted bool `json:"deleted"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.project",
			"failed to decode project delete result")
	}
	return &project.DeleteProjectResponse{Deleted: env.Deleted}, nil
}

// projectFleetNodeRoleWire mirrors service.GrantFleetNodeRoleResponse and
// service.RevokeFleetNodeRoleResponse, which the role tools return verbatim. The
// tags spell the untagged Go field names those types emit.
type projectFleetNodeRoleWire struct {
	ProjectID   string              `json:"ProjectID"`
	FleetNodeID string              `json:"FleetNodeID"`
	Role        project.ProjectRole `json:"Role"`
	Timestamp   time.Time           `json:"Timestamp"`
}

func decodeMCPProjectGrant(raw []byte) (interface{}, error) {
	wire, err := decodeProjectFleetNodeRoleWire(raw)
	if err != nil {
		return nil, err
	}
	return &project.GrantFleetNodeRoleResponse{
		ProjectID:   wire.ProjectID,
		FleetNodeID: wire.FleetNodeID,
		Role:        wire.Role,
		Timestamp:   wire.Timestamp,
	}, nil
}

func decodeMCPProjectRevoke(raw []byte) (interface{}, error) {
	wire, err := decodeProjectFleetNodeRoleWire(raw)
	if err != nil {
		return nil, err
	}
	return &project.RevokeFleetNodeRoleResponse{
		ProjectID:   wire.ProjectID,
		FleetNodeID: wire.FleetNodeID,
		Timestamp:   wire.Timestamp,
	}, nil
}

func decodeProjectFleetNodeRoleWire(raw []byte) (*projectFleetNodeRoleWire, error) {
	var wire projectFleetNodeRoleWire
	if err := json.Unmarshal(raw, &wire); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "mcp.project",
			"failed to decode project agent role result")
	}
	return &wire, nil
}
