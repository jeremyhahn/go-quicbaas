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
)

// catalogAgentService is an agent service holding a real agent directory.
type catalogAgentService struct {
	*mockAgentService
	directory *orchestration.AgentDirectory
}

func (a *catalogAgentService) AgentDirectory() *orchestration.AgentDirectory { return a.directory }

// anyModels serves every backend with no curated model list.
type anyModels struct{}

func (anyModels) ListBackendModels(context.Context, string) ([]string, error) { return nil, nil }

func newCatalogRegistry(t *testing.T) *ToolRegistry {
	t.Helper()
	dir, err := orchestration.NewAgentDirectory(orchestration.AgentDirectoryConfig{
		Store: orchestration.NewInMemoryAgentCatalogStore(), Models: anyModels{},
	})
	require.NoError(t, err)
	return newFleetRegistry(t, &catalogAgentService{mockAgentService: &mockAgentService{}, directory: dir})
}

// callCatalog runs one catalog tool and decodes its JSON answer into out.
func callCatalog(t *testing.T, reg *ToolRegistry, tool string, args map[string]interface{}, out interface{}) error {
	t.Helper()
	result, err := reg.executeAgentCatalogTool(context.Background(), tool, args)
	if err != nil {
		return err
	}
	if out != nil {
		raw, marshalErr := json.Marshal(result)
		require.NoError(t, marshalErr)
		var text string
		if json.Unmarshal(raw, &text) == nil {
			raw = []byte(text)
		}
		require.NoError(t, json.Unmarshal(raw, out))
	}
	return nil
}

func TestAgentCatalogTools_Lifecycle(t *testing.T) {
	reg := newCatalogRegistry(t)
	agent := map[string]interface{}{
		"id": "claude-coder", "name": "Claude coder", "roles": []interface{}{"coding"},
		"backend": "claude", "model": "claude-opus-5-5",
	}
	require.NoError(t, callCatalog(t, reg, "quicbaas_agents_create", map[string]interface{}{"tenant_id": "acme", "agent": agent}, nil))

	var list struct {
		Agents []orchestration.Agent `json:"agents"`
	}
	require.NoError(t, callCatalog(t, reg, "quicbaas_agents_list", map[string]interface{}{"tenant_id": "acme"}, &list))
	require.Len(t, list.Agents, 1)

	agent["name"] = "Coder"
	require.NoError(t, callCatalog(t, reg, "quicbaas_agents_update",
		map[string]interface{}{"tenant_id": "acme", "agent_id": "claude-coder", "agent": agent}, nil))
	var got orchestration.Agent
	require.NoError(t, callCatalog(t, reg, "quicbaas_agents_get", map[string]interface{}{"tenant_id": "acme", "agent_id": "claude-coder"}, &got))
	assert.Equal(t, "Coder", got.Name)

	pool := map[string]interface{}{"id": "coders", "name": "Coders", "agents": []interface{}{"claude-coder"}}
	require.NoError(t, callCatalog(t, reg, "quicbaas_agent_pools_create", map[string]interface{}{"tenant_id": "acme", "pool": pool}, nil))
	var used struct {
		UsedBy []orchestration.AgentUsage `json:"used_by"`
	}
	require.NoError(t, callCatalog(t, reg, "quicbaas_agents_used_by", map[string]interface{}{"tenant_id": "acme", "agent_id": "claude-coder"}, &used))
	require.Len(t, used.UsedBy, 1)

	err := callCatalog(t, reg, "quicbaas_agents_delete", map[string]interface{}{"tenant_id": "acme", "agent_id": "claude-coder"}, nil)
	assert.True(t, errors.Is(err, orchestration.ErrAgentInUse), "%v", err)

	for _, step := range []struct {
		tool string
		args map[string]interface{}
	}{
		{"quicbaas_agent_pools_list", map[string]interface{}{"tenant_id": "acme"}},
		{"quicbaas_agent_pools_get", map[string]interface{}{"tenant_id": "acme", "pool_id": "coders"}},
		{"quicbaas_agent_pools_update", map[string]interface{}{"tenant_id": "acme", "pool_id": "coders", "pool": pool}},
		{"quicbaas_agent_pools_used_by", map[string]interface{}{"tenant_id": "acme", "pool_id": "coders"}},
		{"quicbaas_agent_pools_delete", map[string]interface{}{"tenant_id": "acme", "pool_id": "coders"}},
		{"quicbaas_agents_delete", map[string]interface{}{"tenant_id": "acme", "agent_id": "claude-coder"}},
	} {
		require.NoError(t, callCatalog(t, reg, step.tool, step.args, nil), step.tool)
	}
}

func TestAgentCatalogTools_Refusals(t *testing.T) {
	reg := newCatalogRegistry(t)
	ctx := context.Background()
	if _, err := reg.executeAgentCatalogTool(ctx, "quicbaas_agents_list", map[string]interface{}{}); !isRequiredFieldError(err, "tenant_id") {
		t.Fatalf("no tenant = %v", err)
	}
	if _, err := reg.executeAgentCatalogTool(ctx, "quicbaas_agents_create", map[string]interface{}{"tenant_id": "acme", "agent": "nope"}); err == nil {
		t.Fatal("an agent that is not an object must be refused")
	}
	if _, err := reg.executeAgentCatalogTool(ctx, "quicbaas_agent_pools_update", map[string]interface{}{"tenant_id": "acme", "pool_id": "p"}); err == nil {
		t.Fatal("a pool update with no pool must be refused")
	}
	if _, err := reg.executeAgentCatalogTool(ctx, "quicbaas_no_such_tool", map[string]interface{}{"tenant_id": "acme"}); err == nil {
		t.Fatal("an unknown tool must be refused")
	}
	bare := newFleetRegistry(t, &mockAgentService{})
	_, err := bare.executeAgentCatalogTool(ctx, "quicbaas_agents_list", map[string]interface{}{"tenant_id": "acme"})
	assert.True(t, errors.Is(err, orchestration.ErrAgentCatalogUnavailable), "%v", err)
}

func TestAgentCatalogTools_AreRegisteredAndAuthorized(t *testing.T) {
	reg := newCatalogRegistry(t)
	for _, tool := range agentToolDefinitions {
		assert.True(t, isAgentCatalogTool(tool.Name), tool.Name)
		toolByName(t, reg, tool.Name)
		resource, _, ok := getToolQRDBPermission(tool.Name)
		assert.True(t, ok && resource == "agents", "%s must be gated agents:*", tool.Name)
	}
}

func TestGetAgentRef(t *testing.T) {
	ref := getAgentRef(map[string]interface{}{"lead": map[string]interface{}{"pool_id": "leads"}}, "lead")
	assert.Equal(t, orchestration.PoolRefTo("leads"), ref)
	assert.True(t, getAgentRef(map[string]interface{}{"lead": "x"}, "lead").IsZero())
	assert.True(t, getAgentRef(map[string]interface{}{}, "lead").IsZero())
}

func TestAgentToolArgumentError(t *testing.T) {
	cause := errors.New("bad json")
	err := &AgentToolArgumentError{Field: "agent", Cause: cause}
	assert.Contains(t, err.Error(), "agent must be an object: bad json")
	assert.True(t, errors.Is(err, cause))
	assert.Equal(t, "mcp: pool must be an object", (&AgentToolArgumentError{Field: "pool"}).Error())
}
