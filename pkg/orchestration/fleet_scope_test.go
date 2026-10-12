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
	"errors"
	"testing"

	qerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// The fleet-listing scope (go-quicbaas#680)
//
// The store does not refuse a foreign prefix: it scans inside the caller's own
// keyspace and finds nothing, so a caller naming another tenant used to get an
// empty fleet instead of a refusal. The service now refuses it before the scan.
// =============================================================================

// fleetScopeService returns a service whose registry holds one tenant-1 node.
func fleetScopeService(t *testing.T) AgentService {
	t.Helper()

	svc := pagedTestService(t)
	registry := NewFleetRegistry(NewMemoryTaskKV())
	svc.SetFleetRegistry(registry)
	ctx := ContextWithTenant(context.Background(), "tenant-1")
	if err := registry.RegisterFleetNode(ctx, &FleetNode{ID: "a1", TenantID: "tenant-1", Name: "one"}); err != nil {
		t.Fatalf("RegisterFleetNode() error = %v", err)
	}
	return svc
}

// callerContext binds caller as both the authenticated tenant and the
// orchestration tenant, as the transports do.
func callerContext(caller string) context.Context {
	return ContextWithTenant(tenantctx.WithTenant(context.Background(), caller), caller)
}

func TestListFleetNodes_CallerNamingAnotherTenantIsForbidden(t *testing.T) {
	t.Parallel()

	for _, caller := range []string{"tenant-2", tenantctx.SystemTenantID} {
		t.Run(caller, func(t *testing.T) {
			t.Parallel()

			svc := fleetScopeService(t)
			nodes, err := svc.ListFleetNodes(callerContext(caller), "tenant-1", nil)
			if qerrors.Code(err) != qerrors.ErrForbidden {
				t.Fatalf("ListFleetNodes() error = %v, want ErrForbidden", err)
			}
			var crossTenant *tenantctx.CrossTenantError
			if !errors.As(err, &crossTenant) {
				t.Errorf("ListFleetNodes() error = %v, want a CrossTenantError", err)
			}
			if nodes != nil {
				t.Errorf("ListFleetNodes() = %v, want nil on a refusal", nodes)
			}
		})
	}
}

func TestListFleetNodesPage_CallerNamingAnotherTenantIsForbidden(t *testing.T) {
	t.Parallel()

	svc := fleetScopeService(t)
	page, err := svc.ListFleetNodesPage(callerContext("tenant-2"), "tenant-1", nil, &Page{})
	if qerrors.Code(err) != qerrors.ErrForbidden {
		t.Fatalf("ListFleetNodesPage() error = %v, want ErrForbidden", err)
	}
	if page != nil {
		t.Errorf("ListFleetNodesPage() = %v, want nil on a refusal", page)
	}
}

func TestListFleetNodes_CallerListsItsOwnTenant(t *testing.T) {
	t.Parallel()

	svc := fleetScopeService(t)
	nodes, err := svc.ListFleetNodes(callerContext("tenant-1"), "tenant-1", nil)
	if err != nil {
		t.Fatalf("ListFleetNodes() error = %v, want nil", err)
	}
	if len(nodes) != 1 || nodes[0].ID != "a1" {
		t.Errorf("ListFleetNodes() = %v, want the one tenant-1 node", nodes)
	}
}

// TestListFleetNodes_UnboundCallerIsGovernedByTheTenantGuard pins that an
// in-process loop binding only the orchestration key still lists its fleet.
func TestListFleetNodes_UnboundCallerIsGovernedByTheTenantGuard(t *testing.T) {
	t.Parallel()

	svc := fleetScopeService(t)
	nodes, err := svc.ListFleetNodes(ContextWithTenant(context.Background(), "tenant-1"), "tenant-1", nil)
	if err != nil {
		t.Fatalf("ListFleetNodes() error = %v, want nil", err)
	}
	if len(nodes) != 1 {
		t.Errorf("ListFleetNodes() = %v, want the one tenant-1 node", nodes)
	}
}

// TestListFleetNodes_StoreDenialPropagatesAsForbidden pins that a store whose
// Scan refuses the prefix (go-qrdb#648) reaches the caller as the same typed
// forbidden error, not as an empty fleet.
func TestListFleetNodes_StoreDenialPropagatesAsForbidden(t *testing.T) {
	t.Parallel()

	svc := pagedTestService(t)
	svc.SetFleetRegistry(NewFleetRegistry(newFailingAgentKV(agentFaultDenyScan)))

	nodes, err := svc.ListFleetNodes(callerContext("tenant-1"), "tenant-1", nil)
	if qerrors.Code(err) != qerrors.ErrForbidden {
		t.Fatalf("ListFleetNodes() error = %v, want ErrForbidden", err)
	}
	if nodes != nil {
		t.Errorf("ListFleetNodes() = %v, want nil on a denial", nodes)
	}
}

func TestAuthorizeCallerTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		ctx           context.Context
		requested     string
		wantForbidden bool
	}{
		{name: "no caller bound", ctx: context.Background(), requested: "tenant-1"},
		{name: "own tenant", ctx: tenantctx.WithTenant(context.Background(), "tenant-1"), requested: "tenant-1"},
		{name: "empty means own", ctx: tenantctx.WithTenant(context.Background(), "tenant-1"), requested: ""},
		{name: "other tenant", ctx: tenantctx.WithTenant(context.Background(), "tenant-1"), requested: "tenant-2", wantForbidden: true},
		{name: "system gets no lift", ctx: tenantctx.WithSystem(context.Background()), requested: "tenant-2", wantForbidden: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := authorizeCallerTarget(tt.ctx, "test.op", tt.requested)
			if tt.wantForbidden {
				if qerrors.Code(err) != qerrors.ErrForbidden {
					t.Errorf("authorizeCallerTarget() error = %v, want ErrForbidden", err)
				}
				return
			}
			if err != nil {
				t.Errorf("authorizeCallerTarget() error = %v, want nil", err)
			}
		})
	}
}
