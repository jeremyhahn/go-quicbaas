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

package service

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// =============================================================================
// Agents and pools — the tenant's AI agent catalog
// =============================================================================
//
// Every transport reaches the catalog through these functions, never through
// the directory itself: each one names the tenant it acts on, is authorized
// against the caller's tenant (targetTenantContext), and runs in that tenant's
// storage namespace.

// AgentCatalogService is the service over one tenant agent catalog.
type AgentCatalogService struct {
	directory *orchestration.AgentDirectory
}

// NewAgentCatalogService wraps directory, which is required.
func NewAgentCatalogService(directory *orchestration.AgentDirectory) (*AgentCatalogService, error) {
	if directory == nil {
		return nil, &orchestration.AgentCatalogUnavailableError{}
	}
	return &AgentCatalogService{directory: directory}, nil
}

// AgentCatalogServiceFrom returns the catalog service of an agent service that
// holds one, refusing with ErrAgentCatalogUnavailable when it holds none.
func AgentCatalogServiceFrom(agents orchestration.AgentService) (*AgentCatalogService, error) {
	accessor, ok := agents.(orchestration.AgentDirectoryAccessor)
	if !ok {
		return nil, &orchestration.AgentCatalogUnavailableError{}
	}
	return NewAgentCatalogService(accessor.AgentDirectory())
}

// scope authorizes the caller against tenantID and scopes the context to it.
func (s *AgentCatalogService) scope(ctx context.Context, tenantID string) (context.Context, error) {
	if strings.TrimSpace(tenantID) == "" {
		return ctx, &orchestration.InputValidationError{Field: "tenant_id", Message: "the agent catalog is tenant-scoped"}
	}
	return targetTenantContext(ctx, tenantID)
}

// ListAgents lists tenantID's Agents, ID-ordered.
func (s *AgentCatalogService) ListAgents(ctx context.Context, tenantID string) ([]*orchestration.Agent, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.ListAgents(ctx, tenantID)
}

// GetAgent reads one Agent.
func (s *AgentCatalogService) GetAgent(ctx context.Context, tenantID, agentID string) (*orchestration.Agent, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.GetAgent(ctx, tenantID, agentID)
}

// CreateAgent validates an Agent against the live catalog and stores it.
func (s *AgentCatalogService) CreateAgent(ctx context.Context, tenantID string, agent *orchestration.Agent) (*orchestration.Agent, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.CreateAgent(ctx, tenantID, agent)
}

// UpdateAgent replaces the Agent agentID. The body's ID, when it names one,
// must be agentID: an Agent is renamed by its Name, never re-keyed.
func (s *AgentCatalogService) UpdateAgent(ctx context.Context, tenantID, agentID string, agent *orchestration.Agent) (*orchestration.Agent, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, &orchestration.InputValidationError{Field: "agent", Message: "must not be nil"}
	}
	if agent.ID != "" && agent.ID != agentID {
		return nil, &orchestration.InputValidationError{Field: "agent.id", Message: "an agent's ID is its key and cannot change; change its name"}
	}
	agent.ID = agentID
	return s.directory.UpdateAgent(ctx, tenantID, agent)
}

// DeleteAgent removes an Agent nothing references.
func (s *AgentCatalogService) DeleteAgent(ctx context.Context, tenantID, agentID string) error {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return err
	}
	return s.directory.DeleteAgent(ctx, tenantID, agentID)
}

// AgentUsedBy lists everything that references the Agent agentID.
func (s *AgentCatalogService) AgentUsedBy(ctx context.Context, tenantID, agentID string) ([]orchestration.AgentUsage, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.UsedBy(ctx, tenantID, orchestration.AgentRefTo(agentID))
}

// ListPools lists tenantID's pools, ID-ordered.
func (s *AgentCatalogService) ListPools(ctx context.Context, tenantID string) ([]*orchestration.AgentPool, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.ListPools(ctx, tenantID)
}

// GetPool reads one pool.
func (s *AgentCatalogService) GetPool(ctx context.Context, tenantID, poolID string) (*orchestration.AgentPool, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.GetPool(ctx, tenantID, poolID)
}

// CreatePool stores a new pool.
func (s *AgentCatalogService) CreatePool(ctx context.Context, tenantID string, pool *orchestration.AgentPool) (*orchestration.AgentPool, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.CreatePool(ctx, tenantID, pool)
}

// UpdatePool replaces the pool poolID, under the same re-keying rule as
// UpdateAgent.
func (s *AgentCatalogService) UpdatePool(ctx context.Context, tenantID, poolID string, pool *orchestration.AgentPool) (*orchestration.AgentPool, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if pool == nil {
		return nil, &orchestration.InputValidationError{Field: "pool", Message: "must not be nil"}
	}
	if pool.ID != "" && pool.ID != poolID {
		return nil, &orchestration.InputValidationError{Field: "pool.id", Message: "a pool's ID is its key and cannot change; change its name"}
	}
	pool.ID = poolID
	return s.directory.UpdatePool(ctx, tenantID, pool)
}

// DeletePool removes a pool nothing references.
func (s *AgentCatalogService) DeletePool(ctx context.Context, tenantID, poolID string) error {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return err
	}
	return s.directory.DeletePool(ctx, tenantID, poolID)
}

// PoolUsedBy lists everything that references the pool poolID.
func (s *AgentCatalogService) PoolUsedBy(ctx context.Context, tenantID, poolID string) ([]orchestration.AgentUsage, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return s.directory.UsedBy(ctx, tenantID, orchestration.PoolRefTo(poolID))
}

// Catalogue reads tenantID's whole catalog, for the worker image refresh.
func (s *AgentCatalogService) Catalogue(ctx context.Context, tenantID string) (orchestration.AgentCatalogue, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return orchestration.AgentCatalogue{}, err
	}
	return s.directory.Catalogue(ctx, tenantID)
}

// ReplaceAgentImage repoints one Agent whose image is stale, for the worker
// image refresh's promotion.
func (s *AgentCatalogService) ReplaceAgentImage(ctx context.Context, tenantID, agentID string, stale []string, image string) (bool, error) {
	ctx, err := s.scope(ctx, tenantID)
	if err != nil {
		return false, err
	}
	return s.directory.ReplaceAgentImage(ctx, tenantID, agentID, stale, image)
}
