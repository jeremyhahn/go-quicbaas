// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package sdlc

import (
	"context"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Store — the module's two records, tenant-resolved
// =============================================================================
//
// TENANCY IS THE RESOLVER'S, NOT THE KEY'S. Every operation names its tenant
// and routes through qrdbsdk.TenantStoreResolver, which hands back a store
// already confined to that tenant. The resolver runs on EVERY call and
// nothing is cached against a tenant id: a tenant whose barrier is sealed must
// fail here, and a tenant whose isolation changed must get its new store.
//
// Records are addressed by the orchestration id they reference (a binding by
// its Project, a delivery by its Task) through a derived entity id, so no
// secondary index is needed and a read is one key. A delivery update is a
// guarded write on Delivery.Version.

const (
	bindingEntityType  = "sdlc_binding"
	deliveryEntityType = "sdlc_delivery"

	bindingEntityName  = "project binding"
	deliveryEntityName = "delivery"
)

// Store reads and writes the module's tenant-owned records.
type Store struct {
	resolver qrdbsdk.TenantStoreResolver
	now      func() time.Time
}

// NewStore builds a store over the tenant resolver.
func NewStore(resolver qrdbsdk.TenantStoreResolver) (*Store, error) {
	if resolver == nil {
		return nil, &InvalidInputError{
			Field:   "resolver",
			Message: "the store confines every record to its tenant through the resolver and cannot run without one",
		}
	}
	return &Store{resolver: resolver, now: func() time.Time { return time.Now().UTC() }}, nil
}

// entityDAO resolves the tenant's store and opens the DAO for one entity
// type on it. A free function because Go methods cannot be generic.
func entityDAO[E qrdbsdk.Entity](
	ctx context.Context,
	s *Store,
	op, tenantID, entityType string,
	newEntity func() E,
) (qrdbsdk.GenericDAO[E], error) {
	store, err := s.resolver.Resolve(ctx, tenantID)
	if err != nil {
		return nil, backendError(op+".resolve", err)
	}
	dao, err := qrdbsdk.NewDAO(store, entityType, newEntity)
	if err != nil {
		return nil, backendError(op+".dao", err)
	}
	return dao, nil
}

// bindingKey is where the project's binding lives.
func bindingKey(projectID string) string {
	return recordKey(bindingEntityType, entityIDFor(projectID))
}

// deliveryKey is where the task's delivery lives.
func deliveryKey(taskID string) string {
	return recordKey(deliveryEntityType, entityIDFor(taskID))
}

// =============================================================================
// Bindings
// =============================================================================

// GetBinding returns the binding for projectID.
func (s *Store) GetBinding(ctx context.Context, tenantID, projectID string) (*ProjectBinding, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	store, err := s.tenantKV(ctx, "GetBinding", tenantID)
	if err != nil {
		return nil, err
	}
	binding, _, err := loadRecord[ProjectBinding](ctx, "GetBinding", store, bindingKey(projectID))
	if err != nil {
		return nil, err
	}
	if binding == nil || binding.ProjectID != projectID {
		// Absent, or a digest collision: the slot holds another project's binding.
		return nil, &NotFoundError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
	}
	// The entity id is derived, never stored; a record read back carries it
	// again so a later write addresses the same slot.
	binding.ID = entityIDFor(projectID)
	return binding, nil
}

// PutBinding creates or replaces the binding for its project, stamping a
// fresh WriteToken on it, and returns the record it replaced (nil when it
// created one). The prior record is read in the same compare-and-swap round
// that writes, so whether this put created or replaced is decided atomically
// with the write.
//
// An error from the write itself may mean the write landed: the backend can
// lose the response of a committed write. In that case PutBinding returns
// the record that write would have replaced, and binding.WriteToken carries
// the token stamped on the attempt, so the caller can withdraw exactly that
// write. Every other error returns a nil record. A caller whose write
// returned an error must settle it with SettleBindingWrite before treating
// the binding as absent, because the write may still commit later.
func (s *Store) PutBinding(ctx context.Context, tenantID string, binding *ProjectBinding) (*ProjectBinding, error) {
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	store, err := s.guardedKV(ctx, "PutBinding", tenantID)
	if err != nil {
		return nil, err
	}
	epoch, err := newNonce()
	if err != nil {
		return nil, backendError("PutBinding.epoch", err)
	}
	token, err := newNonce()
	if err != nil {
		return nil, backendError("PutBinding.write_token", err)
	}
	binding.ID = entityIDFor(binding.ProjectID)
	key := bindingKey(binding.ProjectID)
	for {
		held, raw, err := loadRecord[ProjectBinding](ctx, "PutBinding", store, key)
		if err != nil {
			return nil, err
		}
		var prior *ProjectBinding
		now := s.now()
		binding.UpdatedAt = now
		binding.CreatedAt = now
		binding.Epoch = epoch
		binding.WriteToken = token
		if held != nil && held.ProjectID == binding.ProjectID {
			binding.CreatedAt = held.CreatedAt
			binding.Epoch = held.Epoch
			prior = held
			prior.ID = binding.ID
		}
		applied, err := storeRecord(ctx, "PutBinding", store, key, raw, binding)
		if err != nil {
			// The outcome is unknown: report what this write would have
			// replaced so the caller can withdraw it by its token.
			return prior, err
		}
		if applied {
			return prior, nil
		}
		if err := ctx.Err(); err != nil {
			return nil, backendError("PutBinding", err)
		}
	}
}

// WithdrawBinding undoes the PutBinding that stamped writeToken: while the
// stored record still carries that token, it is swapped back to prior (the
// record that put replaced) or, when prior is nil, removed. A record carrying
// another token (a later bind's, or none) is left alone and withdrawn is
// false; so is an absent record. It undoes a write known to have landed: a
// write whose outcome is unknown is first settled with SettleBindingWrite.
func (s *Store) WithdrawBinding(ctx context.Context, tenantID, projectID, writeToken string, prior *ProjectBinding) (bool, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return false, err
	}
	if writeToken == "" {
		return false, &InvalidInputError{Field: "write_token", Message: "a withdrawal needs the token of the write it undoes"}
	}
	store, err := s.guardedKV(ctx, "WithdrawBinding", tenantID)
	if err != nil {
		return false, err
	}
	key := bindingKey(projectID)
	for attempt := 0; attempt < fenceWriteAttempts; attempt++ {
		held, raw, err := loadRecord[ProjectBinding](ctx, "WithdrawBinding", store, key)
		if err != nil {
			return false, err
		}
		if held == nil || held.ProjectID != projectID || held.WriteToken != writeToken {
			return false, nil
		}
		var applied bool
		if prior == nil {
			applied, err = store.CompareAndDelete(ctx, key, raw)
			if err != nil {
				return false, backendError("WithdrawBinding", err)
			}
		} else {
			applied, err = storeRecord(ctx, "WithdrawBinding", store, key, raw, prior)
			if err != nil {
				return false, err
			}
		}
		if applied {
			return true, nil
		}
		if err := ctx.Err(); err != nil {
			return false, backendError("WithdrawBinding", err)
		}
	}
	return false, &VersionConflictError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
}

// UpdateBinding writes binding only if the stored record is still the one
// the caller read: its UpdatedAt must match the modification stamp stored
// for the project. A binding replaced or deleted in between is refused with
// VersionConflictError and nothing is written, so a read-modify-write can
// neither undo a concurrent Bind nor resurrect an Unbind. On success the
// binding's UpdatedAt is advanced in place.
//
// The guard is the stored modification stamp rather than a version counter
// because every binding already carries one (PutBinding stamps it on every
// write), so records written before this guard existed are guarded too.
func (s *Store) UpdateBinding(ctx context.Context, tenantID string, binding *ProjectBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	if binding.UpdatedAt.IsZero() {
		// A zero stamp compares equal to an absent record, which is exactly
		// the resurrection this write exists to refuse.
		return &InvalidInputError{Field: "updated_at", Message: "a guarded update needs the stamp of the record it read"}
	}
	store, err := s.guardedKV(ctx, "UpdateBinding", tenantID)
	if err != nil {
		return err
	}
	key := bindingKey(binding.ProjectID)
	held, raw, err := loadRecord[ProjectBinding](ctx, "UpdateBinding", store, key)
	if err != nil {
		return err
	}
	expected := binding.UpdatedAt.UnixNano()
	conflict := &VersionConflictError{Entity: bindingEntityName, TenantID: tenantID, ID: binding.ProjectID, Expected: expected}
	if held == nil || held.ProjectID != binding.ProjectID || held.UpdatedAt.UnixNano() != expected {
		return conflict
	}
	next := *binding
	next.ID = entityIDFor(binding.ProjectID)
	next.UpdatedAt = s.now()
	if !next.UpdatedAt.After(binding.UpdatedAt) {
		// Keep the stamp strictly advancing so the next guarded write can
		// tell this one from the record it replaced.
		next.UpdatedAt = binding.UpdatedAt.Add(time.Nanosecond)
	}
	applied, err := storeRecord(ctx, "UpdateBinding", store, key, raw, &next)
	if err != nil {
		return err
	}
	if !applied {
		return conflict
	}
	*binding = next
	return nil
}

// DeleteBinding removes the binding for projectID. Deleting an absent
// binding is not an error. The store removes only the exact record read, so
// a binding updated after that read is kept and the delete is refused with a
// VersionConflictError.
func (s *Store) DeleteBinding(ctx context.Context, tenantID, projectID string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "DeleteBinding", tenantID)
	if err != nil {
		return err
	}
	held, changed, err := removeRecord(ctx, "DeleteBinding", store, bindingKey(projectID), func(b *ProjectBinding) bool {
		return b.ProjectID == projectID
	})
	if err != nil {
		return err
	}
	if changed {
		return &VersionConflictError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID, Expected: held.UpdatedAt.UnixNano()}
	}
	return nil
}

// ListBindings returns every binding in the tenant.
func (s *Store) ListBindings(ctx context.Context, tenantID string) ([]*ProjectBinding, error) {
	store, err := s.tenantKV(ctx, "ListBindings", tenantID)
	if err != nil {
		return nil, err
	}
	bindings, err := scanRecords[ProjectBinding](ctx, "ListBindings", store, bindingEntityType)
	if err != nil {
		return nil, err
	}
	// A record with no ProjectID is a void marker SettleBindingWrite left in
	// a slot a pending write might otherwise have filled: it binds nothing.
	listed := bindings[:0]
	for _, binding := range bindings {
		if binding.ProjectID == "" {
			continue
		}
		binding.ID = entityIDFor(binding.ProjectID)
		listed = append(listed, binding)
	}
	return listed, nil
}

// listPageSize bounds one page of a listing.
const listPageSize = 200

// =============================================================================
// Deliveries
// =============================================================================

// GetDelivery returns the delivery recorded for taskID.
func (s *Store) GetDelivery(ctx context.Context, tenantID, taskID string) (*Delivery, error) {
	if err := validateKeySegment("task_id", taskID); err != nil {
		return nil, err
	}
	store, err := s.tenantKV(ctx, "GetDelivery", tenantID)
	if err != nil {
		return nil, err
	}
	delivery, _, err := loadRecord[Delivery](ctx, "GetDelivery", store, deliveryKey(taskID))
	if err != nil {
		return nil, err
	}
	if delivery == nil || delivery.TaskID != taskID {
		return nil, &NotFoundError{Entity: deliveryEntityName, TenantID: tenantID, ID: taskID}
	}
	delivery.ID = entityIDFor(taskID)
	return delivery, nil
}

// PutDelivery creates or replaces the delivery for its task. It is what
// Prepare writes at the start of an attempt; every later change goes through
// UpdateDelivery so a concurrent writer is refused rather than erased.
func (s *Store) PutDelivery(ctx context.Context, tenantID string, delivery *Delivery) error {
	if err := delivery.Validate(); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "PutDelivery", tenantID)
	if err != nil {
		return err
	}
	delivery.ID = entityIDFor(delivery.TaskID)
	return replaceRecord(ctx, "PutDelivery", store, deliveryKey(delivery.TaskID), func(held *Delivery) any {
		now := s.now()
		delivery.UpdatedAt = now
		delivery.CreatedAt = now
		delivery.Version = 1
		if held != nil && held.TaskID == delivery.TaskID {
			delivery.CreatedAt = held.CreatedAt
			delivery.Version = held.Version + 1
		}
		return delivery
	})
}

// UpdateDelivery writes delivery only if the stored record still carries the
// version the caller read; otherwise it returns VersionConflictError and
// writes nothing. On success the delivery's Version is advanced in place.
func (s *Store) UpdateDelivery(ctx context.Context, tenantID string, delivery *Delivery) error {
	if err := delivery.Validate(); err != nil {
		return err
	}
	store, err := s.guardedKV(ctx, "UpdateDelivery", tenantID)
	if err != nil {
		return err
	}
	key := deliveryKey(delivery.TaskID)
	held, raw, err := loadRecord[Delivery](ctx, "UpdateDelivery", store, key)
	if err != nil {
		return err
	}
	expected := delivery.Version
	conflict := &VersionConflictError{Entity: deliveryEntityName, TenantID: tenantID, ID: delivery.TaskID, Expected: expected}
	switch {
	case held == nil && expected != 0:
		// Only a writer that read no record, at version 0, may create one.
		return conflict
	case held != nil && (held.TaskID != delivery.TaskID || held.Version != expected):
		return conflict
	}
	next := *delivery
	next.ID = entityIDFor(delivery.TaskID)
	next.Version = expected + 1
	next.UpdatedAt = s.now()
	applied, err := storeRecord(ctx, "UpdateDelivery", store, key, raw, &next)
	if err != nil {
		return err
	}
	if !applied {
		return conflict
	}
	*delivery = next
	return nil
}
