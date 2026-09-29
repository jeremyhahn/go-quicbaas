//go:build desktop

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

package main

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	agentsSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/agents"
)

// AgentCatalogBinding is the desktop's reach into the tenant AI agent
// catalog: Agents and the pools slots fail over along. The request and
// response types are the SDK's, as the TypeScript Wails client passes and
// expects them.
type AgentCatalogBinding struct {
	logger *logger.Logger
	conn   *ConnectionBinding
	ctx    context.Context
}

// NewAgentCatalogBinding creates the binding over the shared connection.
func NewAgentCatalogBinding(log *logger.Logger, conn *ConnectionBinding) *AgentCatalogBinding {
	return &AgentCatalogBinding{logger: log, conn: conn}
}

func (b *AgentCatalogBinding) startup(ctx context.Context) { b.ctx = ctx }

func (b *AgentCatalogBinding) shutdown(context.Context) {}

// catalog resolves the SDK Agents client.
func (b *AgentCatalogBinding) catalog() (agentsSDK.Client, error) {
	client, err := b.conn.loadClient()
	if err != nil {
		return nil, err
	}
	return client.Agents(), nil
}

// guiCall runs one verb, wrapping a transport failure with the verb's name.
func guiCall[T any](b *AgentCatalogBinding, op string, fn func(agentsSDK.Client) (T, error)) (T, error) {
	var zero T
	client, err := b.catalog()
	if err != nil {
		return zero, err
	}
	out, err := fn(client)
	if err != nil {
		return zero, NewGUITransportError("AgentCatalogBinding."+op, err)
	}
	return out, nil
}

// ListAgents lists the tenant's Agents.
func (b *AgentCatalogBinding) ListAgents(req *agentsSDK.ListAgentsRequest) (*agentsSDK.AgentList, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.ListAgents")
	}
	return guiCall(b, "ListAgents", func(c agentsSDK.Client) (*agentsSDK.AgentList, error) {
		list, err := c.ListAgents(b.ctx, req.TenantID)
		return &agentsSDK.AgentList{Agents: list}, err
	})
}

// GetAgent reads one Agent.
func (b *AgentCatalogBinding) GetAgent(req *agentsSDK.AgentRequest) (*agentsSDK.Agent, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.GetAgent")
	}
	return guiCall(b, "GetAgent", func(c agentsSDK.Client) (*agentsSDK.Agent, error) {
		return c.GetAgent(b.ctx, req.TenantID, req.AgentID)
	})
}

// CreateAgent creates an Agent.
func (b *AgentCatalogBinding) CreateAgent(req *agentsSDK.SaveAgentRequest) (*agentsSDK.Agent, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.CreateAgent")
	}
	return guiCall(b, "CreateAgent", func(c agentsSDK.Client) (*agentsSDK.Agent, error) {
		return c.CreateAgent(b.ctx, req.TenantID, req.Agent)
	})
}

// UpdateAgent replaces an Agent.
func (b *AgentCatalogBinding) UpdateAgent(req *agentsSDK.SaveAgentRequest) (*agentsSDK.Agent, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.UpdateAgent")
	}
	return guiCall(b, "UpdateAgent", func(c agentsSDK.Client) (*agentsSDK.Agent, error) {
		return c.UpdateAgent(b.ctx, req.TenantID, req.AgentID, req.Agent)
	})
}

// DeleteAgent deletes an Agent nothing references.
func (b *AgentCatalogBinding) DeleteAgent(req *agentsSDK.AgentRequest) (*agentsSDK.Deleted, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.DeleteAgent")
	}
	return guiCall(b, "DeleteAgent", func(c agentsSDK.Client) (*agentsSDK.Deleted, error) {
		return &agentsSDK.Deleted{Deleted: true}, c.DeleteAgent(b.ctx, req.TenantID, req.AgentID)
	})
}

// AgentUsedBy lists everything that references an Agent.
func (b *AgentCatalogBinding) AgentUsedBy(req *agentsSDK.AgentRequest) (*agentsSDK.UsageList, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.AgentUsedBy")
	}
	return guiCall(b, "AgentUsedBy", func(c agentsSDK.Client) (*agentsSDK.UsageList, error) {
		usages, err := c.AgentUsedBy(b.ctx, req.TenantID, req.AgentID)
		return &agentsSDK.UsageList{UsedBy: usages}, err
	})
}

// ListPools lists the tenant's pools.
func (b *AgentCatalogBinding) ListPools(req *agentsSDK.ListPoolsRequest) (*agentsSDK.PoolList, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.ListPools")
	}
	return guiCall(b, "ListPools", func(c agentsSDK.Client) (*agentsSDK.PoolList, error) {
		pools, err := c.ListPools(b.ctx, req.TenantID)
		return &agentsSDK.PoolList{Pools: pools}, err
	})
}

// GetPool reads one pool.
func (b *AgentCatalogBinding) GetPool(req *agentsSDK.PoolRequest) (*agentsSDK.Pool, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.GetPool")
	}
	return guiCall(b, "GetPool", func(c agentsSDK.Client) (*agentsSDK.Pool, error) {
		return c.GetPool(b.ctx, req.TenantID, req.PoolID)
	})
}

// CreatePool creates a pool.
func (b *AgentCatalogBinding) CreatePool(req *agentsSDK.SavePoolRequest) (*agentsSDK.Pool, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.CreatePool")
	}
	return guiCall(b, "CreatePool", func(c agentsSDK.Client) (*agentsSDK.Pool, error) {
		return c.CreatePool(b.ctx, req.TenantID, req.Pool)
	})
}

// UpdatePool replaces a pool.
func (b *AgentCatalogBinding) UpdatePool(req *agentsSDK.SavePoolRequest) (*agentsSDK.Pool, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.UpdatePool")
	}
	return guiCall(b, "UpdatePool", func(c agentsSDK.Client) (*agentsSDK.Pool, error) {
		return c.UpdatePool(b.ctx, req.TenantID, req.PoolID, req.Pool)
	})
}

// DeletePool deletes a pool nothing references.
func (b *AgentCatalogBinding) DeletePool(req *agentsSDK.PoolRequest) (*agentsSDK.Deleted, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.DeletePool")
	}
	return guiCall(b, "DeletePool", func(c agentsSDK.Client) (*agentsSDK.Deleted, error) {
		return &agentsSDK.Deleted{Deleted: true}, c.DeletePool(b.ctx, req.TenantID, req.PoolID)
	})
}

// PoolUsedBy lists everything that references a pool.
func (b *AgentCatalogBinding) PoolUsedBy(req *agentsSDK.PoolRequest) (*agentsSDK.UsageList, error) {
	if req == nil {
		return nil, NewGUIRequestRequiredError("AgentCatalogBinding.PoolUsedBy")
	}
	return guiCall(b, "PoolUsedBy", func(c agentsSDK.Client) (*agentsSDK.UsageList, error) {
		usages, err := c.PoolUsedBy(b.ctx, req.TenantID, req.PoolID)
		return &agentsSDK.UsageList{UsedBy: usages}, err
	})
}
