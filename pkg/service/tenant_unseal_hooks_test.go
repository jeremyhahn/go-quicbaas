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
	"errors"
	"slices"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
)

// recordingObserver records every tenant it was told about, and whether the
// context it was handed had been cancelled.
type recordingObserver struct {
	mu        sync.Mutex
	tenants   []string
	cancelled bool
}

func (o *recordingObserver) TenantUnsealed(ctx context.Context, tenantID string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tenants = append(o.tenants, tenantID)
	o.cancelled = o.cancelled || ctx.Err() != nil
}

func TestTenantUnsealHooks_TellEveryObserverAfterAnUnseal(t *testing.T) {
	hooks := &TenantUnsealHooks{}
	first, second := &recordingObserver{}, &recordingObserver{}
	hooks.Register(first)
	hooks.Register(nil)
	svc := NewTenantBarrierService(&mockTenantClient{}, WithTenantUnsealHooks(hooks))
	// Registered after the facade was built: the facade holds the hooks, not
	// a copy of the observers.
	hooks.Register(second)

	ctx, cancel := context.WithCancel(tenantCtx("acme"))
	cancel()
	if err := svc.TenantBarrierUnseal(ctx, "acme", []byte("key")); err != nil {
		t.Fatalf("TenantBarrierUnseal: %v", err)
	}
	for _, observer := range []*recordingObserver{first, second} {
		if !slices.Equal(observer.tenants, []string{"acme"}) {
			t.Fatalf("observer told %v", observer.tenants)
		}
		if observer.cancelled {
			t.Fatal("an unseal that succeeded is followed through even when its caller has gone")
		}
	}
}

func TestTenantUnsealHooks_TellNoOneWhenTheBarrierStaysSealed(t *testing.T) {
	hooks := &TenantUnsealHooks{}
	observer := &recordingObserver{}
	hooks.Register(observer)

	refused := NewTenantBarrierService(&mockTenantClient{unsealErr: errors.New("wrong key")}, WithTenantUnsealHooks(hooks))
	if err := refused.TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("bad")); err == nil {
		t.Fatal("the refusal must surface")
	}
	partial := NewTenantBarrierService(&mockTenantClient{shareResp: &qrdbtransport.TenantBarrierUnsealProgress{
		TenantID: "acme", Required: 2, Submitted: 1, Sealed: true,
	}}, WithTenantUnsealHooks(hooks))
	if _, err := partial.TenantBarrierUnsealShare(tenantCtx("acme"), "acme", "share-1"); err != nil {
		t.Fatal(err)
	}
	failed := NewTenantBarrierService(&mockTenantClient{shareErr: errors.New("bad share")}, WithTenantUnsealHooks(hooks))
	if _, err := failed.TenantBarrierUnsealShare(tenantCtx("acme"), "acme", "share-x"); err == nil {
		t.Fatal("the refusal must surface")
	}
	if _, err := failed.TenantBarrierUnsealShare(tenantCtx("other"), "acme", "share-x"); err == nil {
		t.Fatal("another tenant's caller is refused before anything is told")
	}
	if len(observer.tenants) != 0 {
		t.Fatalf("observer told %v for a barrier that stayed sealed", observer.tenants)
	}

	complete := NewTenantBarrierService(&mockTenantClient{shareResp: &qrdbtransport.TenantBarrierUnsealProgress{
		TenantID: "acme", Required: 2, Submitted: 2, Complete: true,
	}}, WithTenantUnsealHooks(hooks))
	if _, err := complete.TenantBarrierUnsealShare(tenantCtx("acme"), "acme", "share-2"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(observer.tenants, []string{"acme"}) {
		t.Fatalf("the quorum's last share must tell the observers, got %v", observer.tenants)
	}
}

func TestTenantUnsealHooks_ZeroAndNilAreSafe(t *testing.T) {
	var none *TenantUnsealHooks
	none.notify(context.Background(), "acme")
	(&TenantUnsealHooks{}).notify(context.Background(), "acme")
	if err := NewTenantBarrierService(&mockTenantClient{}).TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("k")); err != nil {
		t.Fatalf("a facade without hooks unseals as before: %v", err)
	}
}

func TestTenantUnsealHooks_RegisterIsSafeConcurrently(t *testing.T) {
	hooks := &TenantUnsealHooks{}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hooks.Register(&recordingObserver{})
		}()
	}
	wg.Wait()
	if got := len(*hooks.observers.Load()); got != 16 {
		t.Fatalf("registered %d observers, want 16", got)
	}
}

func TestTenantUnsealHooks_AFailedGateSealsTheBarrierAgain(t *testing.T) {
	gateErr := errors.New("policy reload failed")
	hooks := &TenantUnsealHooks{}
	hooks.RegisterGate(TenantUnsealGateFunc(func(context.Context, string) error { return gateErr }))
	observer := &recordingObserver{}
	hooks.Register(observer)

	client := &mockTenantClient{}
	err := NewTenantBarrierService(client, WithTenantUnsealHooks(hooks)).
		TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("key"))
	if !errors.Is(err, gateErr) {
		t.Fatalf("the gate's failure must fail the unseal, got %v", err)
	}
	if client.sealReq == nil || client.sealReq.TenantID != "acme" {
		t.Fatalf("the barrier must be sealed again, got %+v", client.sealReq)
	}

	shareClient := &mockTenantClient{shareResp: &qrdbtransport.TenantBarrierUnsealProgress{
		TenantID: "acme", Required: 2, Submitted: 2, Complete: true,
	}}
	progress, err := NewTenantBarrierService(shareClient, WithTenantUnsealHooks(hooks)).
		TenantBarrierUnsealShare(tenantCtx("acme"), "acme", "share-2")
	if progress != nil || !errors.Is(err, gateErr) {
		t.Fatalf("the quorum's last share must fail with the gate, got %+v, %v", progress, err)
	}
	if shareClient.sealReq == nil {
		t.Fatal("the barrier opened by the quorum must be sealed again")
	}

	sealErr := errors.New("seal failed")
	err = NewTenantBarrierService(&mockTenantClient{sealErr: sealErr}, WithTenantUnsealHooks(hooks)).
		TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("key"))
	if !errors.Is(err, gateErr) || !errors.Is(err, sealErr) {
		t.Fatalf("both the gate's and the reseal's failure must surface, got %v", err)
	}
	if len(observer.tenants) != 0 {
		t.Fatalf("observer told %v for a tenant the gate refused", observer.tenants)
	}

	admitting := &TenantUnsealHooks{}
	admitting.RegisterGate(TenantUnsealGateFunc(func(context.Context, string) error { return nil }))
	admitting.Register(observer)
	client = &mockTenantClient{}
	if err := NewTenantBarrierService(client, WithTenantUnsealHooks(admitting)).
		TenantBarrierUnseal(tenantCtx("acme"), "acme", []byte("key")); err != nil {
		t.Fatal(err)
	}
	if client.sealReq != nil {
		t.Fatalf("an admitted tenant must stay unsealed, got %+v", client.sealReq)
	}
	if !slices.Equal(observer.tenants, []string{"acme"}) {
		t.Fatalf("an admitted unseal must tell the observers, got %v", observer.tenants)
	}
}
