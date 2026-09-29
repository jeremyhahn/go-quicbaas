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

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// quicbaas_agents_* / quicbaas_agent_pools_* — the tenant AI agent catalog
// =============================================================================
//
// An Agent is a named runtime + model + provider a role slot references; a
// pool is the ordered list of Agents a slot fails over along. Neither is a
// fleet node.

// agentObjectSchema is the input schema of an Agent.
var agentObjectSchema = map[string]interface{}{
	"type": "object",
	"description": "The Agent: id, name, description, roles (coding, review, triage, lead, project_manager, general), " +
		"backend, model, provider {name, base_url, wire_api, requires_auth}, image, reasoning_effort, " +
		"account {strategy, accounts}, prompt {charter_set, charter | system}, mcp_scopes, capabilities, " +
		"context_tokens, max_concurrent, placement {prefer, reserve, exclusive}, " +
		"spend {windows: [{name, size (nanoseconds), max_tokens, max_equivalent_micros}]} — the Agent's own spend limits, checked when a session of it is spawned.",
}

// poolObjectSchema is the input schema of a pool.
var poolObjectSchema = map[string]interface{}{
	"type":        "object",
	"description": "The pool: id, name, description, agents (Agent IDs, most preferred first — the failover order).",
}

// agentRefSchema is the input schema of an Agent reference.
func agentRefSchema(description string) map[string]interface{} {
	return map[string]interface{}{
		"type":        "object",
		"description": description + " Exactly one of agent_id or pool_id.",
		"properties": map[string]interface{}{
			"agent_id": map[string]interface{}{"type": "string"},
			"pool_id":  map[string]interface{}{"type": "string"},
		},
	}
}

// getAgentRef reads an Agent reference argument; absent is the zero
// reference.
func getAgentRef(args map[string]interface{}, key string) orchestration.AgentRef {
	raw, ok := args[key].(map[string]interface{})
	if !ok {
		return orchestration.AgentRef{}
	}
	return orchestration.AgentRef{AgentID: getStringFromMap(raw, "agent_id"), PoolID: getStringFromMap(raw, "pool_id")}
}

// agentToolSchema builds one tool's input schema from its properties.
func agentToolSchema(required []string, properties map[string]interface{}) map[string]interface{} {
	properties["tenant_id"] = map[string]interface{}{"type": "string", "description": "Tenant whose catalog to act on. Required."}
	return map[string]interface{}{"type": "object", "properties": properties, "required": append([]string{"tenant_id"}, required...)}
}

var (
	agentIDProperty = map[string]interface{}{"type": "string", "description": "The Agent's ID."}
	poolIDProperty  = map[string]interface{}{"type": "string", "description": "The pool's ID."}
)

// agentToolDefinitions are the catalog's tools.
var agentToolDefinitions = []ToolDefinition{
	{Name: "quicbaas_agents_list", Description: "List a tenant's AI Agents.", InputSchema: agentToolSchema(nil, map[string]interface{}{})},
	{Name: "quicbaas_agents_get", Description: "Read one Agent.", InputSchema: agentToolSchema([]string{"agent_id"}, map[string]interface{}{"agent_id": agentIDProperty})},
	{Name: "quicbaas_agents_create", Description: "Create an Agent. Its backend and model are checked against the live catalog; it always names a model.",
		InputSchema: agentToolSchema([]string{"agent"}, map[string]interface{}{"agent": agentObjectSchema})},
	{Name: "quicbaas_agents_update", Description: "Replace an Agent. A role a slot still binds it to cannot be dropped.",
		InputSchema: agentToolSchema([]string{"agent_id", "agent"}, map[string]interface{}{"agent_id": agentIDProperty, "agent": agentObjectSchema})},
	{Name: "quicbaas_agents_delete", Description: "Delete an Agent. Refused, naming every user, while anything references it.",
		InputSchema: agentToolSchema([]string{"agent_id"}, map[string]interface{}{"agent_id": agentIDProperty})},
	{Name: "quicbaas_agents_used_by", Description: "List everything that references an Agent: pools, bindings, projects, configuration.",
		InputSchema: agentToolSchema([]string{"agent_id"}, map[string]interface{}{"agent_id": agentIDProperty})},
	{Name: "quicbaas_agent_pools_list", Description: "List a tenant's Agent pools.", InputSchema: agentToolSchema(nil, map[string]interface{}{})},
	{Name: "quicbaas_agent_pools_get", Description: "Read one pool.", InputSchema: agentToolSchema([]string{"pool_id"}, map[string]interface{}{"pool_id": poolIDProperty})},
	{Name: "quicbaas_agent_pools_create", Description: "Create a pool: an ordered failover list of Agents.",
		InputSchema: agentToolSchema([]string{"pool"}, map[string]interface{}{"pool": poolObjectSchema})},
	{Name: "quicbaas_agent_pools_update", Description: "Replace a pool; order is the failover chain.",
		InputSchema: agentToolSchema([]string{"pool_id", "pool"}, map[string]interface{}{"pool_id": poolIDProperty, "pool": poolObjectSchema})},
	{Name: "quicbaas_agent_pools_delete", Description: "Delete a pool. Refused while anything references it.",
		InputSchema: agentToolSchema([]string{"pool_id"}, map[string]interface{}{"pool_id": poolIDProperty})},
	{Name: "quicbaas_agent_pools_used_by", Description: "List everything that references a pool.",
		InputSchema: agentToolSchema([]string{"pool_id"}, map[string]interface{}{"pool_id": poolIDProperty})},
}

// registerAgentCatalogTools registers the catalog's tools.
func (r *ToolRegistry) registerAgentCatalogTools() {
	for _, tool := range agentToolDefinitions {
		r.addTool(tool)
	}
}

// agentToolHandler runs one catalog tool against the catalog service and the
// tenant it names.
type agentToolHandler func(ctx context.Context, catalog *service.AgentCatalogService, tenantID string, args map[string]interface{}) (interface{}, error)

var agentToolHandlers = map[string]agentToolHandler{
	"quicbaas_agents_list": func(ctx context.Context, c *service.AgentCatalogService, t string, _ map[string]interface{}) (interface{}, error) {
		agents, err := c.ListAgents(ctx, t)
		return map[string]interface{}{"agents": agents}, err
	},
	"quicbaas_agents_get": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		return c.GetAgent(ctx, t, getString(a, "agent_id"))
	},
	"quicbaas_agents_create": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		agent, err := decodeToolObject[orchestration.Agent](a, "agent")
		if err != nil {
			return nil, err
		}
		return c.CreateAgent(ctx, t, agent)
	},
	"quicbaas_agents_update": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		agent, err := decodeToolObject[orchestration.Agent](a, "agent")
		if err != nil {
			return nil, err
		}
		return c.UpdateAgent(ctx, t, getString(a, "agent_id"), agent)
	},
	"quicbaas_agents_delete": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"deleted": true}, c.DeleteAgent(ctx, t, getString(a, "agent_id"))
	},
	"quicbaas_agents_used_by": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		usages, err := c.AgentUsedBy(ctx, t, getString(a, "agent_id"))
		return map[string]interface{}{"used_by": usages}, err
	},
	"quicbaas_agent_pools_list": func(ctx context.Context, c *service.AgentCatalogService, t string, _ map[string]interface{}) (interface{}, error) {
		pools, err := c.ListPools(ctx, t)
		return map[string]interface{}{"pools": pools}, err
	},
	"quicbaas_agent_pools_get": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		return c.GetPool(ctx, t, getString(a, "pool_id"))
	},
	"quicbaas_agent_pools_create": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		pool, err := decodeToolObject[orchestration.AgentPool](a, "pool")
		if err != nil {
			return nil, err
		}
		return c.CreatePool(ctx, t, pool)
	},
	"quicbaas_agent_pools_update": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		pool, err := decodeToolObject[orchestration.AgentPool](a, "pool")
		if err != nil {
			return nil, err
		}
		return c.UpdatePool(ctx, t, getString(a, "pool_id"), pool)
	},
	"quicbaas_agent_pools_delete": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		return map[string]interface{}{"deleted": true}, c.DeletePool(ctx, t, getString(a, "pool_id"))
	},
	"quicbaas_agent_pools_used_by": func(ctx context.Context, c *service.AgentCatalogService, t string, a map[string]interface{}) (interface{}, error) {
		usages, err := c.PoolUsedBy(ctx, t, getString(a, "pool_id"))
		return map[string]interface{}{"used_by": usages}, err
	},
}

// isAgentCatalogTool reports whether name is one of the catalog's tools.
func isAgentCatalogTool(name string) bool {
	_, ok := agentToolHandlers[name]
	return ok
}

// executeAgentCatalogTool runs one catalog tool.
func (r *ToolRegistry) executeAgentCatalogTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	handler, ok := agentToolHandlers[name]
	if !ok {
		return nil, api.NewUnknownToolError(name)
	}
	tenantID := getString(args, "tenant_id")
	if tenantID == "" {
		return nil, api.NewRequiredFieldError("tenant_id")
	}
	agentSvc, err := r.getAgentService()
	if err != nil {
		return nil, err
	}
	catalog, err := service.AgentCatalogServiceFrom(agentSvc)
	if err != nil {
		return nil, err
	}
	result, err := handler(ctx, catalog, tenantID, args)
	if err != nil {
		return nil, err
	}
	return marshalResult(result)
}

// AgentToolArgumentError reports a tool argument that is not the object the
// tool takes.
type AgentToolArgumentError struct {
	Field string
	Cause error
}

// Error implements the error interface.
func (e *AgentToolArgumentError) Error() string {
	msg := "mcp: " + e.Field + " must be an object"
	if e.Cause != nil {
		msg += ": " + e.Cause.Error()
	}
	return msg
}

// Unwrap returns the decode failure.
func (e *AgentToolArgumentError) Unwrap() error { return e.Cause }

// decodeToolObject decodes the object argument key into T.
func decodeToolObject[T any](args map[string]interface{}, key string) (*T, error) {
	raw, ok := args[key].(map[string]interface{})
	if !ok {
		return nil, &AgentToolArgumentError{Field: key}
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, &AgentToolArgumentError{Field: key, Cause: err}
	}
	var out T
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, &AgentToolArgumentError{Field: key, Cause: err}
	}
	return &out, nil
}
