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
)

// =============================================================================
// The tenant's own spend limits — go-quicbaas#1058, ruling F5
// =============================================================================
//
// An account's limits live on the account and a project's on the project; a
// tenant has no orchestration record of its own to carry them, so they live
// in one record per tenant, in the tenant's own orchestration KV:
//
//	orch/spendpolicy/{tenant}
//
// The limits are the one SpendLimits type, measured over EVERY turn charged
// to the tenant — every project, pooled and local — from the tenant quota
// plane. A tenant with no record is UNMETERED, the same honest default an
// account and a project have: inventing a tenant budget would invent a fact
// about someone's spend.

// tenantSpendPolicyKeyspace prefixes the one record per tenant.
const tenantSpendPolicyKeyspace = "orch/spendpolicy/"

// TenantSpendPolicy is a tenant's own spend limits.
type TenantSpendPolicy struct {
	TenantID string      `json:"tenant_id"`
	Limits   SpendLimits `json:"limits,omitzero"`

	// UpdatedAt is when the policy was last written; zero for a tenant that
	// never set one.
	UpdatedAt time.Time `json:"updated_at,omitzero"`
}

// TenantSpendPolicyStore persists each tenant's spend limits.
type TenantSpendPolicyStore interface {
	// Get returns the tenant's policy. A tenant that never set one gets an
	// unmetered policy and no error: absence is a fact, not a failure.
	Get(ctx context.Context, tenantID string) (*TenantSpendPolicy, error)

	// Put validates and replaces the tenant's policy. Limits that meter
	// nothing remove it.
	Put(ctx context.Context, policy *TenantSpendPolicy) error
}

// kvTenantSpendPolicyStore is the KV-backed TenantSpendPolicyStore.
type kvTenantSpendPolicyStore struct {
	kv interfaces.KVStore
}

// Compile-time interface satisfaction check.
var _ TenantSpendPolicyStore = (*kvTenantSpendPolicyStore)(nil)

// NewTenantSpendPolicyStore returns a TenantSpendPolicyStore backed by kv —
// the tenant-routed orchestration KV, so each record lives inside its
// tenant's barrier.
func NewTenantSpendPolicyStore(kv interfaces.KVStore) TenantSpendPolicyStore {
	return &kvTenantSpendPolicyStore{kv: kv}
}

// NewInMemoryTenantSpendPolicyStore returns a memory-backed binding of the
// same store, for tests and embedded use.
func NewInMemoryTenantSpendPolicyStore() TenantSpendPolicyStore {
	return NewTenantSpendPolicyStore(NewMemoryTaskKV())
}

// Get implements TenantSpendPolicyStore.
func (s *kvTenantSpendPolicyStore) Get(ctx context.Context, tenantID string) (*TenantSpendPolicy, error) {
	if err := s.check(ctx, tenantID); err != nil {
		return nil, err
	}
	raw, err := s.kv.Get(ctx, tenantSpendPolicyKeyspace+tenantID)
	if err != nil {
		if qerrors.IsNotFound(err) {
			return &TenantSpendPolicy{TenantID: tenantID}, nil
		}
		return nil, &TenantSpendPolicyStoreError{TenantID: tenantID, Op: "read", Cause: err}
	}
	// Some stores answer an absent key with no bytes rather than NotFound;
	// both mean the tenant never set limits.
	if len(raw) == 0 {
		return &TenantSpendPolicy{TenantID: tenantID}, nil
	}
	var policy TenantSpendPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		return nil, &TenantSpendPolicyStoreError{TenantID: tenantID, Op: "decode", Cause: err}
	}
	return &policy, nil
}

// Put implements TenantSpendPolicyStore.
func (s *kvTenantSpendPolicyStore) Put(ctx context.Context, policy *TenantSpendPolicy) error {
	if policy == nil {
		return &TenantSpendPolicyInvalidError{Field: "policy", Message: "must not be nil"}
	}
	if err := s.check(ctx, policy.TenantID); err != nil {
		return err
	}
	if err := policy.Limits.Validate(); err != nil {
		return err
	}
	key := tenantSpendPolicyKeyspace + policy.TenantID
	if !policy.Limits.Metered() {
		if err := s.kv.Delete(ctx, key); err != nil && !qerrors.IsNotFound(err) {
			return &TenantSpendPolicyStoreError{TenantID: policy.TenantID, Op: "delete", Cause: err}
		}
		return nil
	}
	stored := *policy
	stored.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(stored)
	if err != nil {
		return &TenantSpendPolicyStoreError{TenantID: policy.TenantID, Op: "encode", Cause: err}
	}
	if err := s.kv.Put(ctx, key, data); err != nil {
		return &TenantSpendPolicyStoreError{TenantID: policy.TenantID, Op: "write", Cause: err}
	}
	policy.UpdatedAt = stored.UpdatedAt
	return nil
}

// check refuses a store with no KV, a missing tenant, and a context scoped to
// another tenant.
func (s *kvTenantSpendPolicyStore) check(ctx context.Context, tenantID string) error {
	if s.kv == nil {
		return &TenantSpendPolicyStoreError{TenantID: tenantID, Op: "open", Cause: ErrTenantSpendPolicyStore}
	}
	if err := validateKeySegment("tenant_id", tenantID); err != nil {
		return &TenantSpendPolicyInvalidError{Field: "tenant_id", Message: err.Error()}
	}
	return scopeCheck(ctx, tenantID)
}

var (
	// ErrTenantSpendPolicyInvalid is the sentinel for a refused tenant policy.
	ErrTenantSpendPolicyInvalid = errors.New("orchestration: invalid tenant spend policy")

	// ErrTenantSpendPolicyStore is the sentinel for a store that could not
	// answer.
	ErrTenantSpendPolicyStore = errors.New("orchestration: tenant spend policy store failed")
)

// TenantSpendPolicyInvalidError is a tenant policy refused at the write.
type TenantSpendPolicyInvalidError struct {
	Field   string
	Message string
}

// Error implements the error interface.
func (e *TenantSpendPolicyInvalidError) Error() string {
	return ErrTenantSpendPolicyInvalid.Error() + ": " + e.Field + ": " + e.Message
}

// Unwrap returns ErrTenantSpendPolicyInvalid.
func (e *TenantSpendPolicyInvalidError) Unwrap() error { return ErrTenantSpendPolicyInvalid }

// TenantSpendPolicyStoreError is a store that could not answer.
type TenantSpendPolicyStoreError struct {
	TenantID string
	Op       string
	Cause    error
}

// Error implements the error interface.
func (e *TenantSpendPolicyStoreError) Error() string {
	return ErrTenantSpendPolicyStore.Error() + ": " + e.Op + " tenant " + e.TenantID + ": " + e.Cause.Error()
}

// Unwrap exposes the sentinel and the cause.
func (e *TenantSpendPolicyStoreError) Unwrap() []error {
	return []error{ErrTenantSpendPolicyStore, e.Cause}
}

// =============================================================================
// The read and write every transport serves
// =============================================================================

// TenantSpendPolicyAccessor is implemented by an agent service that holds the
// tenant spend-policy store. Every transport reaches it through this one
// declaration, as the spend report reaches the ledger through
// SpendLedgerAccessor.
type TenantSpendPolicyAccessor interface {
	// TenantSpendPolicies returns the store, or nil when none is wired.
	TenantSpendPolicies() TenantSpendPolicyStore
}

// TenantSpendPolicyReport is a tenant's limits and the live usage against each.
type TenantSpendPolicyReport struct {
	TenantSpendPolicy

	// Windows is each limit window with the tenant's usage in it, read from
	// the tenant quota plane. Empty when the tenant meters nothing, or when
	// no ledger is wired to measure it by.
	Windows []SpendWindowStatus `json:"windows,omitempty"`
}

// ReadTenantSpendPolicy returns a tenant's policy and, when a ledger is given,
// its usage against every window. A store that is not wired fails the read:
// "no limits" and "limits unknown" are different answers.
func ReadTenantSpendPolicy(ctx context.Context, store TenantSpendPolicyStore, ledger ConsumptionLedger, tenantID string, now time.Time) (*TenantSpendPolicyReport, error) {
	if store == nil {
		return nil, &TenantSpendPolicyStoreError{TenantID: tenantID, Op: "open", Cause: ErrTenantSpendPolicyStore}
	}
	policy, err := store.Get(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	report := &TenantSpendPolicyReport{TenantSpendPolicy: *policy}
	if ledger == nil {
		return report, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	now = now.UTC()
	windows, err := windowStatuses(policy.Limits, now, func(from, to time.Time) (*ConsumptionRollup, error) {
		return ledger.TenantWindowUsage(ctx, tenantID, from, to)
	})
	if err != nil {
		return nil, err
	}
	report.Windows = windows
	return report, nil
}

// WriteTenantSpendPolicy replaces a tenant's limits and returns what was
// stored. Limits that meter nothing make the tenant unmetered.
func WriteTenantSpendPolicy(ctx context.Context, store TenantSpendPolicyStore, tenantID string, limits SpendLimits) (*TenantSpendPolicy, error) {
	if store == nil {
		return nil, &TenantSpendPolicyStoreError{TenantID: tenantID, Op: "open", Cause: ErrTenantSpendPolicyStore}
	}
	policy := &TenantSpendPolicy{TenantID: tenantID, Limits: limits}
	if err := store.Put(ctx, policy); err != nil {
		return nil, err
	}
	return policy, nil
}
