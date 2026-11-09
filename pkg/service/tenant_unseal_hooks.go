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
	"fmt"
	"sync"
	"sync/atomic"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Tenant unseal hooks
// =============================================================================
//
// A TENANT WHOSE BARRIER IS SEALED AT BOOT CANNOT BE MIGRATED AT BOOT. Its
// records are unreadable until a tenant admin unseals it, so a boot migration
// that visits it fails and moves on. The hooks let such a migration run the
// moment the barrier opens instead: every per-tenant barrier facade tells the
// registered observers after an unseal succeeds, before the unseal call
// returns, so the tenant's first request after the unseal already reads
// migrated records. Gates run before the observers and can refuse the unseal.
// From the moment an unseal is asked for until its gates pass, the tenant is
// held: Admit refuses it, and the request and store paths that call Admit keep
// it closed. A refused tenant stays held, and the unseal call fails, until a
// later unseal runs its gates again and they pass.

// TenantUnsealGate decides whether a tenant whose barrier has just opened may
// serve requests. A gate runs after the barrier opens and before any
// observer; if it fails, the tenant stays held (closed to every request and
// routed store) and the unseal returns the error, so a tenant never serves
// requests in a state the gate refused.
type TenantUnsealGate interface {
	AdmitUnsealedTenant(ctx context.Context, tenantID string) error
}

// TenantUnsealGateFunc adapts a function to a TenantUnsealGate.
type TenantUnsealGateFunc func(ctx context.Context, tenantID string) error

// AdmitUnsealedTenant calls f.
func (f TenantUnsealGateFunc) AdmitUnsealedTenant(ctx context.Context, tenantID string) error {
	return f(ctx, tenantID)
}

// TenantUnsealObserver is told that a tenant's barrier has just been
// unsealed. It runs on the unsealing request's goroutine and must be
// idempotent: a barrier can be sealed and unsealed any number of times.
type TenantUnsealObserver interface {
	TenantUnsealed(ctx context.Context, tenantID string)
}

// TenantUnsealHooks is the set of observers the per-tenant barrier facades
// tell. The zero value holds none and is ready to use; observers may be
// registered after a facade was built with the hooks. The hooks also hold the
// tenants whose unseal has not yet passed its gates; see Admit.
type TenantUnsealHooks struct {
	observers atomic.Pointer[[]TenantUnsealObserver]
	gates     atomic.Pointer[[]TenantUnsealGate]

	mu   sync.Mutex
	held map[string]struct{}
}

// Admit reports whether tenantID may serve requests: it returns nil unless
// the tenant is held because its unseal has not yet passed its gates, and an
// error wrapping qrdbsdk.ErrTenantStoreSealed if it is. A nil receiver and an
// empty tenantID admit.
func (h *TenantUnsealHooks) Admit(tenantID string) error {
	if h == nil || tenantID == "" {
		return nil
	}
	h.mu.Lock()
	_, held := h.held[tenantID]
	h.mu.Unlock()
	if held {
		return fmt.Errorf("%w: tenant %q is still running its unseal checks", qrdbsdk.ErrTenantStoreSealed, tenantID)
	}
	return nil
}

// hold closes tenantID to Admit and reports whether it was already held. A
// nil receiver holds nothing.
func (h *TenantUnsealHooks) hold(tenantID string) (wasHeld bool) {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, wasHeld = h.held[tenantID]; wasHeld {
		return true
	}
	if h.held == nil {
		h.held = make(map[string]struct{})
	}
	h.held[tenantID] = struct{}{}
	return false
}

// release opens tenantID to Admit again.
func (h *TenantUnsealHooks) release(tenantID string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	delete(h.held, tenantID)
	h.mu.Unlock()
}

// RegisterGate adds a gate. A nil gate is ignored.
func (h *TenantUnsealHooks) RegisterGate(gate TenantUnsealGate) {
	if gate == nil {
		return
	}
	for {
		current := h.gates.Load()
		var next []TenantUnsealGate
		if current != nil {
			next = append(next, *current...)
		}
		next = append(next, gate)
		if h.gates.CompareAndSwap(current, &next) {
			return
		}
	}
}

// admit runs every gate, in order, for tenantID and returns the first
// failure. Like notify, it is not cut short by the request's cancellation.
func (h *TenantUnsealHooks) admit(ctx context.Context, tenantID string) error {
	if h == nil {
		return nil
	}
	gates := h.gates.Load()
	if gates == nil {
		return nil
	}
	ctx = context.WithoutCancel(ctx)
	for _, gate := range *gates {
		if err := gate.AdmitUnsealedTenant(ctx, tenantID); err != nil {
			return err
		}
	}
	return nil
}

// Register adds an observer. A nil observer is ignored.
func (h *TenantUnsealHooks) Register(observer TenantUnsealObserver) {
	if observer == nil {
		return
	}
	for {
		current := h.observers.Load()
		var next []TenantUnsealObserver
		if current != nil {
			next = append(next, *current...)
		}
		next = append(next, observer)
		if h.observers.CompareAndSwap(current, &next) {
			return
		}
	}
}

// notify tells every observer that tenantID was unsealed. The request's
// cancellation does not reach the observers: an unseal that succeeded is
// followed through even when its caller has gone.
func (h *TenantUnsealHooks) notify(ctx context.Context, tenantID string) {
	if h == nil {
		return
	}
	observers := h.observers.Load()
	if observers == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	for _, observer := range *observers {
		observer.TenantUnsealed(ctx, tenantID)
	}
}

// TenantBarrierOption configures a per-tenant barrier facade.
type TenantBarrierOption func(*tenantBarrierService)

// WithTenantUnsealHooks makes the facade tell hooks after every successful
// unseal.
func WithTenantUnsealHooks(hooks *TenantUnsealHooks) TenantBarrierOption {
	return func(s *tenantBarrierService) { s.unsealed = hooks }
}
