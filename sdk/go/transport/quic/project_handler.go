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
	"encoding/json"
	"strings"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// projectServicePrefix is the service name prefix for project operations.
const projectServicePrefix = "quicbaas.Project"

// isProjectService reports whether svc names the Project service.
func isProjectService(svc string) bool {
	return strings.HasPrefix(svc, projectServicePrefix) || svc == "Project"
}

// The server encodes the snake_case project envelopes declared in
// pkg/api/quic/handler_project.go. The SDK project request types already carry
// the matching snake_case tags, so requests need no transform; only responses
// are decoded here, into the canonical SDK types every other transport returns.

// projectWireEnvelope is the single-project response envelope.
type projectWireEnvelope struct {
	Project *project.ProjectInfo `json:"project"`
}

// projectWireList is the list response envelope.
type projectWireList struct {
	Projects []*project.ProjectInfo `json:"projects"`
	Total    int                    `json:"total"`
}

// projectWireDelete is the delete response envelope.
type projectWireDelete struct {
	ProjectID string `json:"project_id"`
	Deleted   bool   `json:"deleted"`
}

// projectWireAgentRole is the grant / revoke response envelope.
type projectWireAgentRole struct {
	ProjectID string              `json:"project_id"`
	AgentID   string              `json:"agent_id"`
	Role      project.ProjectRole `json:"role"`
	Timestamp time.Time           `json:"timestamp"`
}

// projectResponseConverter turns raw wire bytes into the typed SDK value.
type projectResponseConverter func(data []byte) (interface{}, error)

// projectResponseConverters maps method names to their response converters for
// O(1) dispatch.
var projectResponseConverters = map[string]projectResponseConverter{
	"Create":          convertProjectEnvelope,
	"Get":             convertProjectEnvelope,
	"GetByName":       convertProjectEnvelope,
	"Update":          convertProjectEnvelope,
	"List":            convertProjectList,
	"Delete":          convertProjectDelete,
	"GrantAgentRole":  convertProjectGrant,
	"RevokeAgentRole": convertProjectRevoke,
}

// decodeProjectResponse decodes raw wire bytes into the SDK project type for
// method. An unknown method is a programming error on the SDK side: the project
// surface is a closed set, so it fails loudly rather than degrading to an
// untyped map the client would then reject as "invalid response type".
func decodeProjectResponse(method string, data []byte) (interface{}, error) {
	if len(data) == 0 {
		return nil, nil
	}
	converter, ok := projectResponseConverters[method]
	if !ok {
		return nil, errors.NewInvalidInput("quic.decodeProjectResponse",
			"unknown project method: "+method)
	}
	return converter(data)
}

func convertProjectEnvelope(data []byte) (interface{}, error) {
	var env projectWireEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertProjectEnvelope", "failed to unmarshal response")
	}
	if env.Project == nil {
		return nil, errors.NewInternal("quic.convertProjectEnvelope", "server returned no project")
	}
	return env.Project, nil
}

func convertProjectList(data []byte) (interface{}, error) {
	var env projectWireList
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertProjectList", "failed to unmarshal response")
	}
	if env.Projects == nil {
		env.Projects = []*project.ProjectInfo{}
	}
	return &project.ListProjectsResponse{Projects: env.Projects, Total: env.Total}, nil
}

func convertProjectDelete(data []byte) (interface{}, error) {
	var env projectWireDelete
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.convertProjectDelete", "failed to unmarshal response")
	}
	return &project.DeleteProjectResponse{Deleted: env.Deleted}, nil
}

func convertProjectGrant(data []byte) (interface{}, error) {
	env, err := unmarshalProjectAgentRole(data)
	if err != nil {
		return nil, err
	}
	return &project.GrantAgentRoleResponse{
		ProjectID: env.ProjectID,
		AgentID:   env.AgentID,
		Role:      env.Role,
		Timestamp: env.Timestamp,
	}, nil
}

func convertProjectRevoke(data []byte) (interface{}, error) {
	env, err := unmarshalProjectAgentRole(data)
	if err != nil {
		return nil, err
	}
	return &project.RevokeAgentRoleResponse{
		ProjectID: env.ProjectID,
		AgentID:   env.AgentID,
		Timestamp: env.Timestamp,
	}, nil
}

func unmarshalProjectAgentRole(data []byte) (*projectWireAgentRole, error) {
	var env projectWireAgentRole
	if err := json.Unmarshal(data, &env); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal,
			"quic.unmarshalProjectAgentRole", "failed to unmarshal response")
	}
	return &env, nil
}
