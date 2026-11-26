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

package iam

import (
	"context"
	"errors"
	"slices"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// orderedCreatedObserver records each TenantCreated call in the shared order.
type orderedCreatedObserver struct{ order *seedOrder }

func (o orderedCreatedObserver) TenantCreated(ctx context.Context, tenantID string) {
	if ctx.Err() != nil {
		o.order.steps = append(o.order.steps, "cancelled:"+tenantID)
		return
	}
	o.order.steps = append(o.order.steps, "created:"+tenantID)
}

// TestCreateTenant_TellsCreatedObserversAfterSeeding pins that an observer is
// told once, for the new tenant, after its roles and prompts are seeded, and
// that the creating request's cancellation does not reach it.
func TestCreateTenant_TellsCreatedObserversAfterSeeding(t *testing.T) {
	mgr := newTestTenantManager()
	order := &seedOrder{}
	mgr.SetRoleSeeder(orderedRoleSeeder{order: order})
	mgr.SetPromptSeeder(orderedPromptSeeder{order: order})
	mgr.AddCreatedObserver(orderedCreatedObserver{order: order})

	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	want := []string{"roles:burnin", "prompts:burnin", "created:burnin"}
	if !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want %v", order.steps, want)
	}
}

// TestCreateTenant_FailedCreationTellsNoObserver pins that a creation rolled
// back by a failed prompt seed tells no observer.
func TestCreateTenant_FailedCreationTellsNoObserver(t *testing.T) {
	mgr := newTestTenantManager()
	order := &seedOrder{}
	boom := errors.New("prompt store sealed")
	mgr.SetPromptSeeder(orderedPromptSeeder{order: order, err: boom})
	mgr.SetQRDBTenantProvisioner(&recordingQRDBProvisioner{})
	mgr.AddCreatedObserver(orderedCreatedObserver{order: order})

	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); !errors.Is(err, boom) {
		t.Fatalf("CreateTenant error = %v, want it to wrap the seed cause", err)
	}
	if want := []string{"prompts:burnin"}; !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want %v (no observer told)", order.steps, want)
	}
}

// TestCreateTenant_TellsObserverForEveryIsolation pins that the observer is
// told whatever isolation the tenant asks for: CreateTenantOptions carries
// the isolation choice, and a prefix-isolated (or default) tenant has no
// barrier whose unseal could tell anyone instead.
func TestCreateTenant_TellsObserverForEveryIsolation(t *testing.T) {
	isolations := []qrdbsdk.TenantIsolationType{
		"",
		qrdbsdk.TenantIsolationPrefix,
		qrdbsdk.TenantIsolationBarrier,
		qrdbsdk.TenantIsolationVolume,
		qrdbsdk.TenantIsolationRaft,
	}
	for i, isolation := range isolations {
		mgr := newTestTenantManager()
		engine := &recordingQRDBProvisioner{}
		mgr.SetQRDBTenantProvisioner(engine)
		order := &seedOrder{}
		mgr.AddCreatedObserver(orderedCreatedObserver{order: order})

		id := "iso" + string(rune('a'+i))
		if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: id, Name: "Iso", IsolationType: isolation}); err != nil {
			t.Fatalf("CreateTenant(isolation %q): %v", isolation, err)
		}
		if engine.createIsolation != string(isolation) {
			t.Fatalf("engine isolation = %q, want %q", engine.createIsolation, isolation)
		}
		if want := []string{"created:" + id}; !slices.Equal(order.steps, want) {
			t.Fatalf("isolation %q: steps = %v, want %v", isolation, order.steps, want)
		}
	}
}

// TestCreateTenant_TellsEveryObserver pins that each registered observer is
// told, in registration order.
func TestCreateTenant_TellsEveryObserver(t *testing.T) {
	mgr := newTestTenantManager()
	order := &seedOrder{}
	mgr.AddCreatedObserver(orderedCreatedObserver{order: order})
	mgr.AddCreatedObserver(orderedCreatedObserver{order: order})

	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
	if want := []string{"created:burnin", "created:burnin"}; !slices.Equal(order.steps, want) {
		t.Fatalf("steps = %v, want %v", order.steps, want)
	}
}

// TestAddCreatedObserver_IgnoresNil pins that a nil observer is not kept, so
// CreateTenant never calls through it.
func TestAddCreatedObserver_IgnoresNil(t *testing.T) {
	mgr := newTestTenantManager()
	mgr.AddCreatedObserver(nil)
	if len(mgr.createdObservers) != 0 {
		t.Fatalf("createdObservers = %v, want none", mgr.createdObservers)
	}
	if _, err := mgr.CreateTenant(context.Background(), CreateTenantOptions{ID: "burnin", Name: "Burn-in"}); err != nil {
		t.Fatalf("CreateTenant: %v", err)
	}
}
