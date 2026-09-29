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
	"sync/atomic"
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
// migrated records.

// TenantUnsealObserver is told that a tenant's barrier has just been
// unsealed. It runs on the unsealing request's goroutine and must be
// idempotent: a barrier can be sealed and unsealed any number of times.
type TenantUnsealObserver interface {
	TenantUnsealed(ctx context.Context, tenantID string)
}

// TenantUnsealHooks is the set of observers the per-tenant barrier facades
// tell. The zero value holds none and is ready to use; observers may be
// registered after a facade was built with the hooks.
type TenantUnsealHooks struct {
	observers atomic.Pointer[[]TenantUnsealObserver]
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
