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
	"errors"
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

func (s *Store) bindingDAO(ctx context.Context, op, tenantID string) (qrdbsdk.GenericDAO[*ProjectBinding], error) {
	return entityDAO(ctx, s, op, tenantID, bindingEntityType, func() *ProjectBinding { return &ProjectBinding{} })
}

func (s *Store) deliveryDAO(ctx context.Context, op, tenantID string) (qrdbsdk.GenericDAO[*Delivery], error) {
	return entityDAO(ctx, s, op, tenantID, deliveryEntityType, func() *Delivery { return &Delivery{} })
}

// =============================================================================
// Bindings
// =============================================================================

// GetBinding returns the binding for projectID.
func (s *Store) GetBinding(ctx context.Context, tenantID, projectID string) (*ProjectBinding, error) {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return nil, err
	}
	dao, err := s.bindingDAO(ctx, "GetBinding", tenantID)
	if err != nil {
		return nil, err
	}
	binding, err := dao.Get(ctx, entityIDFor(projectID))
	if err != nil {
		if qrdbsdk.IsDAONotFound(err) {
			return nil, &NotFoundError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
		}
		return nil, backendError("GetBinding", err)
	}
	if binding.ProjectID != projectID {
		// A digest collision: the slot holds another project's binding.
		return nil, &NotFoundError{Entity: bindingEntityName, TenantID: tenantID, ID: projectID}
	}
	// The entity id is derived, never stored; a record read back carries it
	// again so a later write addresses the same slot.
	binding.ID = entityIDFor(projectID)
	return binding, nil
}

// PutBinding creates or replaces the binding for its project.
func (s *Store) PutBinding(ctx context.Context, tenantID string, binding *ProjectBinding) error {
	if err := binding.Validate(); err != nil {
		return err
	}
	dao, err := s.bindingDAO(ctx, "PutBinding", tenantID)
	if err != nil {
		return err
	}
	now := s.now()
	binding.ID = entityIDFor(binding.ProjectID)
	binding.UpdatedAt = now
	if held, getErr := dao.Get(ctx, binding.ID); getErr == nil && held.ProjectID == binding.ProjectID {
		binding.CreatedAt = held.CreatedAt
	} else {
		binding.CreatedAt = now
	}
	if err := dao.Save(ctx, binding); err != nil {
		return backendError("PutBinding", err)
	}
	return nil
}

// DeleteBinding removes the binding for projectID. Deleting an absent
// binding is not an error.
func (s *Store) DeleteBinding(ctx context.Context, tenantID, projectID string) error {
	if err := validateKeySegment("project_id", projectID); err != nil {
		return err
	}
	dao, err := s.bindingDAO(ctx, "DeleteBinding", tenantID)
	if err != nil {
		return err
	}
	held, getErr := dao.Get(ctx, entityIDFor(projectID))
	if getErr != nil {
		if qrdbsdk.IsDAONotFound(getErr) {
			return nil
		}
		return backendError("DeleteBinding", getErr)
	}
	if held.ProjectID != projectID {
		return nil
	}
	held.ID = entityIDFor(projectID)
	if err := dao.Delete(ctx, held); err != nil {
		return backendError("DeleteBinding", err)
	}
	return nil
}

// ListBindings returns every binding in the tenant.
func (s *Store) ListBindings(ctx context.Context, tenantID string) ([]*ProjectBinding, error) {
	dao, err := s.bindingDAO(ctx, "ListBindings", tenantID)
	if err != nil {
		return nil, err
	}
	var out []*ProjectBinding
	pageErr := dao.ForEachPage(ctx, qrdbsdk.PageQuery{PageSize: listPageSize}, func(page qrdbsdk.PageResult[*ProjectBinding]) error {
		out = append(out, page.Entities...)
		return nil
	})
	if pageErr != nil {
		return nil, backendError("ListBindings", pageErr)
	}
	return out, nil
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
	dao, err := s.deliveryDAO(ctx, "GetDelivery", tenantID)
	if err != nil {
		return nil, err
	}
	delivery, err := dao.Get(ctx, entityIDFor(taskID))
	if err != nil {
		if qrdbsdk.IsDAONotFound(err) {
			return nil, &NotFoundError{Entity: deliveryEntityName, TenantID: tenantID, ID: taskID}
		}
		return nil, backendError("GetDelivery", err)
	}
	if delivery.TaskID != taskID {
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
	dao, err := s.deliveryDAO(ctx, "PutDelivery", tenantID)
	if err != nil {
		return err
	}
	now := s.now()
	delivery.ID = entityIDFor(delivery.TaskID)
	delivery.UpdatedAt = now
	if held, getErr := dao.Get(ctx, delivery.ID); getErr == nil && held.TaskID == delivery.TaskID {
		delivery.CreatedAt = held.CreatedAt
		delivery.Version = held.Version + 1
	} else {
		delivery.CreatedAt = now
		delivery.Version = 1
	}
	if err := dao.Save(ctx, delivery); err != nil {
		return backendError("PutDelivery", err)
	}
	return nil
}

// UpdateDelivery writes delivery only if the stored record still carries the
// version the caller read; otherwise it returns VersionConflictError and
// writes nothing. On success the delivery's Version is advanced in place.
func (s *Store) UpdateDelivery(ctx context.Context, tenantID string, delivery *Delivery) error {
	if err := delivery.Validate(); err != nil {
		return err
	}
	dao, err := s.deliveryDAO(ctx, "UpdateDelivery", tenantID)
	if err != nil {
		return err
	}
	expected := delivery.Version
	next := *delivery
	next.ID = entityIDFor(delivery.TaskID)
	next.Version = expected + 1
	next.UpdatedAt = s.now()
	cond := qrdbsdk.Condition{Target: qrdbsdk.ConditionVersion, Operator: qrdbsdk.OpEqual, Value: expected}
	if err := dao.SaveWithCondition(ctx, &next, cond); err != nil {
		var failed qrdbsdk.DAOConditionFailedError
		if errors.As(err, &failed) {
			return &VersionConflictError{
				Entity: deliveryEntityName, TenantID: tenantID, ID: delivery.TaskID, Expected: expected,
			}
		}
		return backendError("UpdateDelivery", err)
	}
	*delivery = next
	return nil
}
