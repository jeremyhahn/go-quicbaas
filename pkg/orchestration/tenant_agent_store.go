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

package orchestration

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// TenantAgentStore persists each tenant's one TenantAgent appointment. Every
// method acts on the named tenant only and refuses a context scoped to
// another tenant.
type TenantAgentStore interface {
	// CreateTenantAgent stores the tenant's appointment; it refuses
	// (*TenantAgentExistsError) when one exists.
	CreateTenantAgent(ctx context.Context, tenantID string, agent *TenantAgent) error

	// UpdateTenantAgent replaces the tenant's appointment; it refuses
	// (*UnknownTenantAgentError) when none exists and keeps CreatedAt.
	UpdateTenantAgent(ctx context.Context, tenantID string, agent *TenantAgent) error

	// GetTenantAgent reads the tenant's appointment, or returns
	// *UnknownTenantAgentError when none exists.
	GetTenantAgent(ctx context.Context, tenantID string) (*TenantAgent, error)

	// DeleteTenantAgent removes the tenant's appointment. It is idempotent.
	DeleteTenantAgent(ctx context.Context, tenantID string) error
}

// tenantAgentStore is the single KV-backed TenantAgentStore. Like the agent
// catalog it holds metadata only.
type tenantAgentStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor
	now     func() time.Time

	// recordMu serializes each tenant's read-modify-write of its one record
	// within this process.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ TenantAgentStore = (*tenantAgentStore)(nil)

// NewTenantAgentStore returns a TenantAgentStore backed by kv.
func NewTenantAgentStore(kv interfaces.KVStore, opts ...StoreOption) TenantAgentStore {
	cfg := newStoreConfig(opts...)
	return &tenantAgentStore{kv: kv, auditor: cfg.auditor, now: func() time.Time { return time.Now().UTC() }}
}

// NewInMemoryTenantAgentStore returns a memory-backed binding of the store
// returned by NewTenantAgentStore, for tests and embedded use.
func NewInMemoryTenantAgentStore() TenantAgentStore {
	return NewTenantAgentStore(kvstore.NewMemory())
}

// begin runs the checks every call makes before touching the KV.
func (s *tenantAgentStore) begin(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.kv == nil {
		return &AgentCatalogStoreError{Message: "kv store must not be nil"}
	}
	return scopeCheck(ctx, normalizeStoreTenant(tenantID))
}

func tenantAgentKey(tenantID string) (string, error) {
	key, err := entityKey(tenantAgentKeyspace, normalizeStoreTenant(tenantID), "tenant_agent_id", TenantAgentRecordID)
	if err != nil {
		return "", catalogKeyError(err)
	}
	return key, nil
}

// CreateTenantAgent stores the tenant's first appointment.
func (s *tenantAgentStore) CreateTenantAgent(ctx context.Context, tenantID string, agent *TenantAgent) error {
	err := s.writeTenantAgent(ctx, tenantID, agent, true)
	s.audit(ctx, AuditActionCreate, tenantID, err)
	return err
}

// UpdateTenantAgent replaces the tenant's existing appointment.
func (s *tenantAgentStore) UpdateTenantAgent(ctx context.Context, tenantID string, agent *TenantAgent) error {
	err := s.writeTenantAgent(ctx, tenantID, agent, false)
	s.audit(ctx, AuditActionUpdate, tenantID, err)
	return err
}

// writeTenantAgent is CreateTenantAgent and UpdateTenantAgent. The tenant
// argument is authoritative: the record's TenantID is overwritten with it
// before validation, so a record can never name a tenant other than its key.
func (s *tenantAgentStore) writeTenantAgent(ctx context.Context, tenantID string, agent *TenantAgent, create bool) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	if agent == nil {
		return &InputValidationError{Field: "tenant_agent", Message: "must not be nil"}
	}
	tenant := normalizeStoreTenant(tenantID)
	agent.TenantID = tenant
	if err := agent.Validate(); err != nil {
		return err
	}
	key, err := tenantAgentKey(tenant)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(tenant)
	defer unlock()

	held, err := s.loadTenantAgent(ctx, key, tenant)
	switch {
	case err == nil && create:
		return &TenantAgentExistsError{TenantID: tenant, Principal: held.Principal}
	case err != nil && (!create || !errors.Is(err, ErrTenantAgentUnknown)):
		return err
	}
	now := s.now()
	agent.UpdatedAt = now
	agent.CreatedAt = now
	if held != nil {
		agent.CreatedAt = held.CreatedAt
	}
	data, err := json.Marshal(agent)
	if err != nil {
		return &AgentCatalogStoreError{Message: "failed to marshal tenant agent: " + err.Error(), Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentCatalogStoreError{Message: "failed to write tenant agent: " + err.Error(), Cause: err}
	}
	return nil
}

// GetTenantAgent reads the tenant's appointment.
func (s *tenantAgentStore) GetTenantAgent(ctx context.Context, tenantID string) (*TenantAgent, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return nil, err
	}
	key, err := tenantAgentKey(tenantID)
	if err != nil {
		return nil, err
	}
	return s.loadTenantAgent(ctx, key, normalizeStoreTenant(tenantID))
}

func (s *tenantAgentStore) loadTenantAgent(ctx context.Context, key, tenantID string) (*TenantAgent, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &UnknownTenantAgentError{TenantID: tenantID}
		}
		return nil, &AgentCatalogStoreError{Message: "failed to read tenant agent: " + err.Error(), Cause: err}
	}
	if len(data) == 0 {
		return nil, &UnknownTenantAgentError{TenantID: tenantID}
	}
	var agent TenantAgent
	if err := json.Unmarshal(data, &agent); err != nil {
		return nil, &AgentCatalogStoreError{Message: "failed to unmarshal tenant agent: " + err.Error(), Cause: err}
	}
	agent.TenantID = tenantID
	return &agent, nil
}

// DeleteTenantAgent removes the tenant's appointment; a missing one is
// success.
func (s *tenantAgentStore) DeleteTenantAgent(ctx context.Context, tenantID string) error {
	err := s.deleteTenantAgent(ctx, tenantID)
	s.audit(ctx, AuditActionDelete, tenantID, err)
	return err
}

func (s *tenantAgentStore) deleteTenantAgent(ctx context.Context, tenantID string) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	tenant := normalizeStoreTenant(tenantID)
	key, err := tenantAgentKey(tenant)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(tenant)
	defer unlock()

	if err := s.kv.Delete(ctx, key); err != nil {
		if qerrors.IsNotFound(err) {
			return nil
		}
		return &AgentCatalogStoreError{Message: "failed to delete tenant agent: " + err.Error(), Cause: err}
	}
	return nil
}

// audit emits exactly one store event per mutating call, recording a refused
// attempt with Success false.
func (s *tenantAgentStore) audit(ctx context.Context, action, tenantID string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   normalizeStoreTenant(tenantID),
		Resource:   AuditResourceTenantAgent,
		ResourceID: TenantAgentRecordID,
		Action:     action,
		Success:    err == nil,
	})
}
