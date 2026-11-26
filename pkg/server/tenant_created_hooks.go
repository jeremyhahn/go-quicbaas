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

package server

import (
	"context"
	"sync/atomic"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// tenantCreatedHooks is the set of observers told after the tenant manager
// creates a tenant. IAM init registers the hooks themselves with the tenant
// manager, and the subsystems that keep per-tenant defaults register here,
// so the two may be wired in either order. The zero value holds none and is
// ready to use.
type tenantCreatedHooks struct {
	observers atomic.Pointer[[]iam.TenantCreatedObserver]
}

var _ iam.TenantCreatedObserver = (*tenantCreatedHooks)(nil)

// Register adds an observer. A nil observer is ignored.
func (h *tenantCreatedHooks) Register(observer iam.TenantCreatedObserver) {
	if observer == nil {
		return
	}
	for {
		current := h.observers.Load()
		var next []iam.TenantCreatedObserver
		if current != nil {
			next = append(next, *current...)
		}
		next = append(next, observer)
		if h.observers.CompareAndSwap(current, &next) {
			return
		}
	}
}

// TenantCreated tells every observer that tenantID was created. The
// request's cancellation does not reach the observers: a creation that
// succeeded is followed through even when its caller has gone.
func (h *tenantCreatedHooks) TenantCreated(ctx context.Context, tenantID string) {
	if h == nil {
		return
	}
	observers := h.observers.Load()
	if observers == nil {
		return
	}
	ctx = context.WithoutCancel(ctx)
	for _, observer := range *observers {
		observer.TenantCreated(ctx, tenantID)
	}
}

// tenantCreatedFunc adapts a function to iam.TenantCreatedObserver.
type tenantCreatedFunc func(ctx context.Context, tenantID string)

// TenantCreated implements iam.TenantCreatedObserver.
func (f tenantCreatedFunc) TenantCreated(ctx context.Context, tenantID string) { f(ctx, tenantID) }
