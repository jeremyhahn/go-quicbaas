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

// storageToolResult is the storage member of a project tool result as it
// travels: the tool result is JSON-encoded, and the project record carries
// its storage nested under "storage" (see sdk/go/client/project.ProjectInfo).
type storageToolResult struct {
	Project struct {
		ID      string                       `json:"id"`
		Storage orchestration.ProjectStorage `json:"storage"`
	} `json:"project"`
}

func decodeStorageToolResult(t *testing.T, result interface{}) storageToolResult {
	t.Helper()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	var out storageToolResult
	require.NoError(t, json.Unmarshal(raw, &out), "tool result %s", raw)
	return out
}

// TestExecuteProjectTool_StorageRoundTrip drives project_create → project_get
// and project_update → project_get over the REAL project service, asserting
// the tier survives, an omitted size is left alone and a zero size clears.
func TestExecuteProjectTool_StorageRoundTrip(t *testing.T) {
	reg := newProjectAdminRegistry(t, service.NewProjectService(orchestration.NewInMemoryProjectStore()), nil)
	ctx := context.Background()

	// JSON numbers arrive as float64, which is the shape used here.
	created, err := reg.executeProjectTool(ctx, "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api",
		"workspace_tier": "ephemeral", "workspace_tmpfs_mb": float64(4096), "container_memory_mb": float64(8192),
	})
	require.NoError(t, err)
	id := decodeStorageToolResult(t, created).Project.ID
	require.NotEmpty(t, id)

	want := orchestration.ProjectStorage{
		WorkspaceTier:     orchestration.WorkspaceTierEphemeral,
		WorkspaceTmpfsMB:  4096,
		ContainerMemoryMB: 8192,
	}
	get := func() orchestration.ProjectStorage {
		t.Helper()
		got, err := reg.executeProjectTool(ctx, "project_get", map[string]interface{}{"tenant_id": "t1", "project_id": id})
		require.NoError(t, err)
		return decodeStorageToolResult(t, got).Project.Storage
	}
	assert.Equal(t, want, get())

	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": id, "description": "renamed",
	})
	require.NoError(t, err)
	assert.Equal(t, want, get(), "an update omitting the sizes must leave them")

	_, err = reg.executeProjectTool(ctx, "project_update", map[string]interface{}{
		"tenant_id": "t1", "project_id": id, "workspace_tmpfs_mb": float64(0), "container_memory_mb": float64(0),
	})
	require.NoError(t, err)
	assert.Equal(t, orchestration.ProjectStorage{WorkspaceTier: orchestration.WorkspaceTierEphemeral}, get(),
		"an update sending zero must clear the sizes")
}

// TestExecuteProjectTool_InvalidTierIsRefusedByTheService: the tool forwards
// the tier and the service refuses it, with the same typed error every
// transport maps.
func TestExecuteProjectTool_InvalidTierIsRefusedByTheService(t *testing.T) {
	reg := newProjectAdminRegistry(t, service.NewProjectService(orchestration.NewInMemoryProjectStore()), nil)
	ctx := context.Background()

	_, err := reg.executeProjectTool(ctx, "project_create", map[string]interface{}{
		"tenant_id": "t1", "name": "api", "workspace_tier": "ramdisk",
	})
	var invalid *service.ProjectInvalidError
	require.True(t, errors.As(err, &invalid), "err = %v", err)
	assert.Equal(t, "workspace_tier", invalid.Field)
	var input *orchestration.InputValidationError
	assert.True(t, errors.As(err, &input), "err = %v does not carry the domain refusal", err)
}

// TestProjectToolSchemas_DeclareStorage: a key the schema does not declare is
// one an MCP client is never told it can send.
func TestProjectToolSchemas_DeclareStorage(t *testing.T) {
	reg := newProjectAdminRegistry(t, &mockProjectService{}, nil)
	want := map[string]string{
		"workspace_tier":      "string",
		"workspace_tmpfs_mb":  "integer",
		"container_memory_mb": "integer",
	}
	seen := 0
	for _, tool := range reg.ListTools() {
		if tool.Name != "project_create" && tool.Name != "project_update" {
			continue
		}
		seen++
		props, ok := tool.InputSchema["properties"].(map[string]interface{})
		require.True(t, ok, "%s has no properties", tool.Name)
		for key, typ := range want {
			prop, ok := props[key].(map[string]interface{})
			if !assert.True(t, ok, "%s does not declare %s", tool.Name, key) {
				continue
			}
			assert.Equal(t, typ, prop["type"], "%s.%s type", tool.Name, key)
		}
	}
	assert.Equal(t, 2, seen, "project_create and project_update must both be registered")
}
