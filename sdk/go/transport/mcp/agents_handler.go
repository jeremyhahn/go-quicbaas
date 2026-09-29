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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsAgents declares the Agents service on the MCP transport.
func (t *Transport) SupportsAgents() bool { return true }

var _ agents.CapableTransport = (*Transport)(nil)

// isMCPAgentsService reports whether service is the Agents service.
func isMCPAgentsService(service string) bool { return service == agents.ServiceName }

// agentsTool is one Agents verb's tool and the typed answer it decodes into.
type agentsTool struct {
	name   string
	answer func() interface{}
}

// agentsTools maps each Agents verb to its quicbaas_agents_* /
// quicbaas_agent_pools_* tool. The SDK request types carry the tools'
// argument names as their JSON tags, so the arguments need no transform.
var agentsTools = map[string]agentsTool{
	"ListAgents":  {"quicbaas_agents_list", func() interface{} { return &agents.AgentList{} }},
	"GetAgent":    {"quicbaas_agents_get", func() interface{} { return &agents.Agent{} }},
	"CreateAgent": {"quicbaas_agents_create", func() interface{} { return &agents.Agent{} }},
	"UpdateAgent": {"quicbaas_agents_update", func() interface{} { return &agents.Agent{} }},
	"DeleteAgent": {"quicbaas_agents_delete", func() interface{} { return &agents.Deleted{} }},
	"AgentUsedBy": {"quicbaas_agents_used_by", func() interface{} { return &agents.UsageList{} }},
	"ListPools":   {"quicbaas_agent_pools_list", func() interface{} { return &agents.PoolList{} }},
	"GetPool":     {"quicbaas_agent_pools_get", func() interface{} { return &agents.Pool{} }},
	"CreatePool":  {"quicbaas_agent_pools_create", func() interface{} { return &agents.Pool{} }},
	"UpdatePool":  {"quicbaas_agent_pools_update", func() interface{} { return &agents.Pool{} }},
	"DeletePool":  {"quicbaas_agent_pools_delete", func() interface{} { return &agents.Deleted{} }},
	"PoolUsedBy":  {"quicbaas_agent_pools_used_by", func() interface{} { return &agents.UsageList{} }},
}

// agentsToolName names the tool of one Agents verb.
func agentsToolName(method string) (string, bool) {
	tool, ok := agentsTools[method]
	return tool.name, ok
}

// requestAgents calls one Agents verb's tool and decodes its answer.
func (t *Transport) requestAgents(ctx context.Context, method string, req interface{}) (interface{}, error) {
	op := "mcp.agents." + method
	tool, ok := agentsTools[method]
	if !ok {
		return nil, errors.NewUnimplemented(op, "agents method is not exposed by the MCP tool set")
	}
	result, err := t.callTool(ctx, tool.name, applyTenantFromContext(ctx, t.convertToArguments(req)))
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, errors.NewInternal(op, "empty tool result")
	}
	raw, err := json.Marshal(result)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to re-encode tool result")
	}
	answer := tool.answer()
	if err := json.Unmarshal(raw, answer); err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to decode tool result")
	}
	return answer, nil
}
