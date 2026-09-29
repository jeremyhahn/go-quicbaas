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

package rest

import (
	"context"
	"net/http"
	neturl "net/url"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// SupportsAgents declares the Agents service on the REST transport, and so on
// the QUIC transport that delegates to it.
func (t *Transport) SupportsAgents() bool { return true }

var _ agents.CapableTransport = (*Transport)(nil)

// isAgentsService reports whether service is the Agents service.
func isAgentsService(service string) bool { return service == agents.ServiceName }

// agentsCall is one Agents verb: its HTTP method, its route, the body it
// sends and the answer it decodes.
type agentsCall struct {
	httpMethod string
	path       string
	body       interface{}
	answer     interface{}
}

// agentsRoutes builds each Agents verb's call from its request.
var agentsRoutes = map[string]func(req interface{}) (agentsCall, error){
	"ListAgents": agentsRoute(func(r *agents.ListAgentsRequest) agentsCall {
		return agentsCall{http.MethodGet, agentsPath("/api/v1/agents", r.TenantID), nil, &agents.AgentList{}}
	}),
	"GetAgent": agentsRoute(func(r *agents.AgentRequest) agentsCall {
		return agentsCall{http.MethodGet, agentsPath(agentPath(r.AgentID), r.TenantID), nil, &agents.Agent{}}
	}),
	"CreateAgent": agentsRoute(func(r *agents.SaveAgentRequest) agentsCall {
		return agentsCall{http.MethodPost, agentsPath("/api/v1/agents", r.TenantID), r.Agent, &agents.Agent{}}
	}),
	"UpdateAgent": agentsRoute(func(r *agents.SaveAgentRequest) agentsCall {
		return agentsCall{http.MethodPut, agentsPath(agentPath(r.AgentID), r.TenantID), r.Agent, &agents.Agent{}}
	}),
	"DeleteAgent": agentsRoute(func(r *agents.AgentRequest) agentsCall {
		return agentsCall{http.MethodDelete, agentsPath(agentPath(r.AgentID), r.TenantID), nil, &agents.Deleted{}}
	}),
	"AgentUsedBy": agentsRoute(func(r *agents.AgentRequest) agentsCall {
		return agentsCall{http.MethodGet, agentsPath(agentPath(r.AgentID)+"/used-by", r.TenantID), nil, &agents.UsageList{}}
	}),
	"ListPools": agentsRoute(func(r *agents.ListPoolsRequest) agentsCall {
		return agentsCall{http.MethodGet, agentsPath("/api/v1/agent-pools", r.TenantID), nil, &agents.PoolList{}}
	}),
	"GetPool": agentsRoute(func(r *agents.PoolRequest) agentsCall {
		return agentsCall{http.MethodGet, agentsPath(poolPath(r.PoolID), r.TenantID), nil, &agents.Pool{}}
	}),
	"CreatePool": agentsRoute(func(r *agents.SavePoolRequest) agentsCall {
		return agentsCall{http.MethodPost, agentsPath("/api/v1/agent-pools", r.TenantID), r.Pool, &agents.Pool{}}
	}),
	"UpdatePool": agentsRoute(func(r *agents.SavePoolRequest) agentsCall {
		return agentsCall{http.MethodPut, agentsPath(poolPath(r.PoolID), r.TenantID), r.Pool, &agents.Pool{}}
	}),
	"DeletePool": agentsRoute(func(r *agents.PoolRequest) agentsCall {
		return agentsCall{http.MethodDelete, agentsPath(poolPath(r.PoolID), r.TenantID), nil, &agents.Deleted{}}
	}),
	"PoolUsedBy": agentsRoute(func(r *agents.PoolRequest) agentsCall {
		return agentsCall{http.MethodGet, agentsPath(poolPath(r.PoolID)+"/used-by", r.TenantID), nil, &agents.UsageList{}}
	}),
}

// agentsRoute adapts a typed route builder to the dispatch table, refusing a
// request of the wrong type (or a nil one) before the builder reads it.
func agentsRoute[T any](build func(*T) agentsCall) func(req interface{}) (agentsCall, error) {
	return func(req interface{}) (agentsCall, error) {
		r, ok := req.(*T)
		if !ok || r == nil {
			return agentsCall{}, errors.NewInvalidInput("rest.agents", "invalid request type")
		}
		return build(r), nil
	}
}

// handleAgentsRequest sends one Agents verb.
func (t *Transport) handleAgentsRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	route, ok := agentsRoutes[method]
	if !ok {
		return nil, errors.NewInvalidInput("rest.agents."+method, "unknown agents method")
	}
	call, err := route(req)
	if err != nil {
		return nil, err
	}
	if err := t.doJSONRequest(ctx, call.httpMethod, call.path, call.body, call.answer); err != nil {
		return nil, err
	}
	return call.answer, nil
}

func agentPath(agentID string) string { return "/api/v1/agents/" + neturl.PathEscape(agentID) }

func poolPath(poolID string) string { return "/api/v1/agent-pools/" + neturl.PathEscape(poolID) }

// agentsPath names the tenant when the request does; the server otherwise
// answers for the caller's own tenant.
func agentsPath(path, tenantID string) string {
	if tenantID == "" {
		return path
	}
	return path + "?" + neturl.Values{"tenant_id": {tenantID}}.Encode()
}
