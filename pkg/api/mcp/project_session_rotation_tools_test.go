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
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// sessionRotationToolResult is the session rotation member of a project tool
// result as it travels.
type sessionRotationToolResult struct {
	Project struct {
		ID              string                              `json:"id"`
		SessionRotation orchestration.SessionRotationPolicy `json:"session_rotation"`
	} `json:"project"`
}

func decodeSessionRotationToolResult(t *testing.T, result interface{}) sessionRotationToolResult {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var out sessionRotationToolResult
	require.NoError(t, json.Unmarshal(raw, &out), "tool result %s", raw)
	return out
}

// TestExecuteProjectTool_SessionRotationRoundTrip drives project_create and
// project_update over the REAL project service: the policy survives, an update
// that omits it leaves it, and one that names it replaces it.
func TestExecuteProjectTool_SessionRotationRoundTrip(t *testing.T) {
	reg := newProjectAdminRegistry(t, service.NewProjectService(orchestration.NewInMemoryProjectStore()), nil)
	ctx := context.Background()

	created, err := reg.executeProjectTool(ctx, "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api", "session_rotation": "fresh",
	})
	require.NoError(t, err)
	id := decodeSessionRotationToolResult(t, created).Project.ID
	require.NotEmpty(t, id)

	get := func() orchestration.SessionRotationPolicy {
		t.Helper()
		got, err := reg.executeProjectTool(ctx, "project_get", map[string]interface{}{"tenant_id": "t1", "project_id": id})
		require.NoError(t, err)
		return decodeSessionRotationToolResult(t, got).Project.SessionRotation
	}
	assert.Equal(t, orchestration.SessionRotationFresh, get())

	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": id, "description": "renamed",
	})
	require.NoError(t, err)
	assert.Equal(t, orchestration.SessionRotationFresh, get(), "an update naming no policy must leave it")

	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": id, "session_rotation": "resume",
	})
	require.NoError(t, err)
	assert.Equal(t, orchestration.SessionRotationResume, get())
}

// TestExecuteProjectTool_AnUnknownSessionRotationIsRefused: the refusal is the
// service's, carrying the domain error.
func TestExecuteProjectTool_AnUnknownSessionRotationIsRefused(t *testing.T) {
	reg := newProjectAdminRegistry(t, service.NewProjectService(orchestration.NewInMemoryProjectStore()), nil)
	_, err := reg.executeProjectTool(context.Background(), "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api", "session_rotation": "restart",
	})
	require.Error(t, err)
	assert.True(t, errors.Is(err, orchestration.ErrSessionRotationPolicyInvalid), "err = %v", err)
}
