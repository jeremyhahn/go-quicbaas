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
	"strings"
	"time"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// TenantPlanStore persists each tenant's one TenantPlan. Writes are
// serialized by revision compare-and-set. Every method acts on the named
// tenant only and refuses a context scoped to another tenant.
type TenantPlanStore interface {
	// PutTenantPlan stores plan when the held revision equals
	// expectedRevision (0 means no plan is held); plan.Revision must be
	// expectedRevision+1. It returns *TenantPlanConflictError otherwise and
	// keeps CreatedAt across revisions.
	PutTenantPlan(ctx context.Context, tenantID string, plan *TenantPlan, expectedRevision uint64) error

	// GetTenantPlan reads the tenant's plan, or returns
	// *UnknownTenantPlanError when none is set.
	GetTenantPlan(ctx context.Context, tenantID string) (*TenantPlan, error)

	// DeleteTenantPlan removes the tenant's plan. It is idempotent.
	DeleteTenantPlan(ctx context.Context, tenantID string) error
}

// tenantPlanStore is the single KV-backed TenantPlanStore. Like the agent
// catalog it holds metadata only.
type tenantPlanStore struct {
	kv      interfaces.KVStore
	auditor StoreAuditor
	now     func() time.Time

	// recordMu serializes each tenant's read-modify-write of its one record
	// within this process.
	recordMu keyedMutex
}

// Compile-time interface satisfaction check.
var _ TenantPlanStore = (*tenantPlanStore)(nil)

// NewTenantPlanStore returns a TenantPlanStore backed by kv.
func NewTenantPlanStore(kv interfaces.KVStore, opts ...StoreOption) TenantPlanStore {
	cfg := newStoreConfig(opts...)
	return &tenantPlanStore{kv: kv, auditor: cfg.auditor, now: func() time.Time { return time.Now().UTC() }}
}

// NewInMemoryTenantPlanStore returns a memory-backed binding of the store
// returned by NewTenantPlanStore, for tests and embedded use.
func NewInMemoryTenantPlanStore() TenantPlanStore {
	return NewTenantPlanStore(kvstore.NewMemory())
}

// begin runs the checks every call makes before touching the KV.
func (s *tenantPlanStore) begin(ctx context.Context, tenantID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.kv == nil {
		return &AgentCatalogStoreError{Message: "kv store must not be nil"}
	}
	return scopeCheck(ctx, normalizeStoreTenant(tenantID))
}

func tenantPlanKey(tenantID string) (string, error) {
	key, err := entityKey(tenantPlanKeyspace, normalizeStoreTenant(tenantID), "tenant_plan_id", TenantPlanRecordID)
	if err != nil {
		return "", catalogKeyError(err)
	}
	return key, nil
}

// PutTenantPlan stores the tenant's next plan revision.
func (s *tenantPlanStore) PutTenantPlan(ctx context.Context, tenantID string, plan *TenantPlan, expectedRevision uint64) error {
	err := s.putTenantPlan(ctx, tenantID, plan, expectedRevision)
	action := AuditActionUpdate
	if expectedRevision == 0 {
		action = AuditActionCreate
	}
	s.audit(ctx, action, tenantID, err)
	return err
}

// putTenantPlan is PutTenantPlan without the audit. The tenant argument is
// authoritative: the plan's TenantID is overwritten with it before
// validation, so a plan can never name a tenant other than its key.
func (s *tenantPlanStore) putTenantPlan(ctx context.Context, tenantID string, plan *TenantPlan, expectedRevision uint64) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	if plan == nil {
		return &InputValidationError{Field: "tenant_plan", Message: "must not be nil"}
	}
	tenant := normalizeStoreTenant(tenantID)
	plan.TenantID = tenant
	if err := plan.Validate(); err != nil {
		return err
	}
	switch {
	case strings.TrimSpace(plan.Principal) == "":
		return &InputValidationError{Field: "tenant_plan.principal", Message: "required"}
	case strings.TrimSpace(plan.SigningKeyID) == "":
		return &InputValidationError{Field: "tenant_plan.signing_key_id", Message: "required"}
	case len(plan.Signature) == 0:
		return &InputValidationError{Field: "tenant_plan.signature", Message: "required"}
	case plan.Revision != expectedRevision+1:
		return &InputValidationError{Field: "tenant_plan.revision", Message: "must be the expected revision plus one"}
	}
	key, err := tenantPlanKey(tenant)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(tenant)
	defer unlock()

	held, err := s.loadTenantPlan(ctx, key, tenant)
	if err != nil && !errors.Is(err, ErrTenantPlanUnknown) {
		return err
	}
	var heldRevision uint64
	if held != nil {
		heldRevision = held.Revision
	}
	if heldRevision != expectedRevision {
		return &TenantPlanConflictError{TenantID: tenant, Expected: expectedRevision, Held: heldRevision}
	}
	now := s.now()
	plan.UpdatedAt = now
	plan.CreatedAt = now
	if held != nil {
		plan.CreatedAt = held.CreatedAt
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return &AgentCatalogStoreError{Message: "failed to marshal tenant plan: " + err.Error(), Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &AgentCatalogStoreError{Message: "failed to write tenant plan: " + err.Error(), Cause: err}
	}
	return nil
}

// GetTenantPlan reads the tenant's plan.
func (s *tenantPlanStore) GetTenantPlan(ctx context.Context, tenantID string) (*TenantPlan, error) {
	if err := s.begin(ctx, tenantID); err != nil {
		return nil, err
	}
	key, err := tenantPlanKey(tenantID)
	if err != nil {
		return nil, err
	}
	return s.loadTenantPlan(ctx, key, normalizeStoreTenant(tenantID))
}

func (s *tenantPlanStore) loadTenantPlan(ctx context.Context, key, tenantID string) (*TenantPlan, error) {
	data, err := s.kv.Get(ctx, key)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return nil, &UnknownTenantPlanError{TenantID: tenantID}
		}
		return nil, &AgentCatalogStoreError{Message: "failed to read tenant plan: " + err.Error(), Cause: err}
	}
	if len(data) == 0 {
		return nil, &UnknownTenantPlanError{TenantID: tenantID}
	}
	var plan TenantPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, &AgentCatalogStoreError{Message: "failed to unmarshal tenant plan: " + err.Error(), Cause: err}
	}
	plan.TenantID = tenantID
	return &plan, nil
}

// DeleteTenantPlan removes the tenant's plan; a missing one is success.
func (s *tenantPlanStore) DeleteTenantPlan(ctx context.Context, tenantID string) error {
	err := s.deleteTenantPlan(ctx, tenantID)
	s.audit(ctx, AuditActionDelete, tenantID, err)
	return err
}

func (s *tenantPlanStore) deleteTenantPlan(ctx context.Context, tenantID string) error {
	if err := s.begin(ctx, tenantID); err != nil {
		return err
	}
	tenant := normalizeStoreTenant(tenantID)
	key, err := tenantPlanKey(tenant)
	if err != nil {
		return err
	}

	unlock := s.recordMu.lock(tenant)
	defer unlock()

	if err := s.kv.Delete(ctx, key); err != nil {
		if qerrors.IsNotFound(err) {
			return nil
		}
		return &AgentCatalogStoreError{Message: "failed to delete tenant plan: " + err.Error(), Cause: err}
	}
	return nil
}

// audit emits exactly one store event per mutating call, recording a refused
// attempt with Success false.
func (s *tenantPlanStore) audit(ctx context.Context, action, tenantID string, err error) {
	emitStoreAudit(ctx, s.auditor, StoreAuditEvent{
		TenantID:   normalizeStoreTenant(tenantID),
		Resource:   AuditResourceTenantPlan,
		ResourceID: TenantPlanRecordID,
		Action:     action,
		Success:    err == nil,
	})
}
