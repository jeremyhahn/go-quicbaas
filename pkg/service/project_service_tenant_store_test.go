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

package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// The project record must live in its owning tenant's namespace
// =============================================================================
//
// The durable store the server injects routes every operation to the tenant
// bound to the operation's CONTEXT, while a project record's key names the
// tenant the REQUEST states. When those two disagree the record is written
// somewhere no tenant-stamped reader looks, and the orchestrator's ClaimTask
// project PEP — which always runs under the task's tenant — sees an
// unresolvable project on every scoped task. That is go-quicbaas#634.
//
// routingKV is that store's behaviour reduced to what these tests need: one
// independent partition per context tenant, and none of the read/write paths
// the server layer adds on top.

type routingKV struct {
	mu         sync.Mutex
	partitions map[string]interfaces.KVStore
}

func newRoutingKV() *routingKV {
	return &routingKV{partitions: make(map[string]interfaces.KVStore)}
}

// partition returns the store the context tenant's records live in.
func (r *routingKV) partition(ctx context.Context) interfaces.KVStore {
	tenantID := orchestration.TenantFromContext(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	store, ok := r.partitions[tenantID]
	if !ok {
		store = orchestration.NewMemoryTaskKV()
		r.partitions[tenantID] = store
	}
	return store
}

func (r *routingKV) Put(ctx context.Context, key string, value []byte) error {
	return r.partition(ctx).Put(ctx, key, value)
}

func (r *routingKV) Get(ctx context.Context, key string) ([]byte, error) {
	return r.partition(ctx).Get(ctx, key)
}

func (r *routingKV) Delete(ctx context.Context, key string) error {
	return r.partition(ctx).Delete(ctx, key)
}

func (r *routingKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return r.partition(ctx).Scan(ctx, prefix, fn)
}

func (r *routingKV) List(ctx context.Context, prefix string) ([]string, error) {
	return r.partition(ctx).List(ctx, prefix)
}

func (r *routingKV) Exists(ctx context.Context, key string) (bool, error) {
	return r.partition(ctx).Exists(ctx, key)
}

// PutIfAbsent forwards the guarded create the project store's name
// reservation needs.
func (r *routingKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	creator, ok := r.partition(ctx).(interface {
		PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error)
	})
	if !ok {
		return false, errors.New("routingKV: partition cannot guard a create")
	}
	return creator.PutIfAbsent(ctx, key, value)
}

// CompareAndDelete forwards the guarded release half of the reservation.
func (r *routingKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	deleter, ok := r.partition(ctx).(interface {
		CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
	})
	if !ok {
		return false, errors.New("routingKV: partition cannot guard a delete")
	}
	return deleter.CompareAndDelete(ctx, key, expected)
}

var _ interfaces.KVStore = (*routingKV)(nil)

// TestProjectService_RoutedStoreFindsProjectUnderRequestTenant is the unit-level
// statement of go-quicbaas#634: a project created and granted roles through a
// context that carries no tenant must still be the project the orchestrator's
// tenant-stamped PEP reads, or the role matrix is enforced against nothing.
func TestProjectService_RoutedStoreFindsProjectUnderRequestTenant(t *testing.T) {
	const (
		tenantID = "tenant-634"
		agentID  = "agent-634"
	)

	routed := newRoutingKV()
	store := orchestration.NewProjectStore(routed)
	svc := service.NewProjectService(store)

	// The caller has no tenant on its context — an in-process caller, and the
	// shape the orchestrator integration suite exercises.
	plainCtx := context.Background()
	created, err := svc.CreateProject(plainCtx, &service.CreateProjectRequest{
		TenantID: tenantID,
		Name:     "routed-project",
		FleetNodeRoles: map[string]orchestration.ProjectRole{
			agentID: orchestration.ProjectRoleWorker,
		},
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	projectID := created.Project.ID

	// The PEP always runs under the task's tenant.
	authz := orchestration.NewProjectAuthz(store)
	tenantCtx := orchestration.ContextWithTenant(context.Background(), tenantID)

	if authzErr := authz.AuthorizeFleetNodeForTenant(
		tenantCtx, tenantID, projectID, agentID, orchestration.ProjectRoleWorker,
	); authzErr != nil {
		t.Fatalf("the worker the project granted must be authorized: %v", authzErr)
	}

	// A grant made through the same untenanted caller must reach the same
	// record, or a revocation would be invisible to the PEP.
	if _, grantErr := svc.GrantFleetNodeRole(plainCtx, &service.GrantFleetNodeRoleRequest{
		TenantID:    tenantID,
		ProjectID:   projectID,
		FleetNodeID: agentID,
		Role:        orchestration.ProjectRoleDenied,
	}); grantErr != nil {
		t.Fatalf("grant denied role: %v", grantErr)
	}

	denyErr := authz.AuthorizeFleetNodeForTenant(
		tenantCtx, tenantID, projectID, agentID, orchestration.ProjectRoleWorker)
	if denyErr == nil {
		t.Fatal("a denied agent must be refused; the PEP read a stale or absent record")
	}
	var denied *orchestration.ProjectAccessDeniedError
	if !errors.As(denyErr, &denied) {
		t.Fatalf("expected *ProjectAccessDeniedError, got %T: %v", denyErr, denyErr)
	}
	if denied.ActualRole != orchestration.ProjectRoleDenied {
		t.Errorf("ActualRole = %q, want %q", denied.ActualRole, orchestration.ProjectRoleDenied)
	}
}

// TestProjectService_RoutedStoreKeepsTheContextTenant proves the addressing
// helper never redirects a write: a context that already names a tenant
// decides where the record lands, because which tenant a caller may act as is
// an admission decision made above this layer.
func TestProjectService_RoutedStoreKeepsTheContextTenant(t *testing.T) {
	const (
		requestTenant = "tenant-request"
		contextTenant = "tenant-context"
	)

	routed := newRoutingKV()
	store := orchestration.NewProjectStore(routed)
	svc := service.NewProjectService(store)

	callerCtx := orchestration.ContextWithTenant(context.Background(), contextTenant)
	created, err := svc.CreateProject(callerCtx, &service.CreateProjectRequest{
		TenantID: requestTenant,
		Name:     "kept-project",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	// The record is in the caller's partition, keyed by the request tenant.
	if _, getErr := store.Get(callerCtx, requestTenant, created.Project.ID); getErr != nil {
		t.Fatalf("the record must stay in the context tenant's partition: %v", getErr)
	}

	// And it is not in the request tenant's own partition, which is what the
	// helper would have produced had it overridden the caller's context.
	otherCtx := orchestration.ContextWithTenant(context.Background(), requestTenant)
	if _, getErr := store.Get(otherCtx, requestTenant, created.Project.ID); getErr == nil {
		t.Fatal("the helper must not redirect a write into another tenant's namespace")
	}
}
