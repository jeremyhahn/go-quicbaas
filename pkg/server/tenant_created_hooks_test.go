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
	"slices"
	"testing"
)

// TestTenantCreatedHooks_TellsEveryObserverOnce pins that one creation tells
// each registered observer once, in registration order, for that tenant,
// with the caller's cancellation detached.
func TestTenantCreatedHooks_TellsEveryObserverOnce(t *testing.T) {
	var hooks tenantCreatedHooks
	var told []string
	for _, name := range []string{"first", "second"} {
		hooks.Register(tenantCreatedFunc(func(ctx context.Context, tenantID string) {
			if ctx.Err() != nil {
				t.Errorf("observer %s got a cancelled context", name)
			}
			told = append(told, name+":"+tenantID)
		}))
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	hooks.TenantCreated(ctx, "acme")

	if want := []string{"first:acme", "second:acme"}; !slices.Equal(told, want) {
		t.Fatalf("told = %v, want %v", told, want)
	}
}

// TestTenantCreatedHooks_NilAndEmpty pins that empty and nil hooks tell no
// one without panicking, and that a nil observer is not registered.
func TestTenantCreatedHooks_NilAndEmpty(t *testing.T) {
	var nilHooks *tenantCreatedHooks
	nilHooks.TenantCreated(context.Background(), "acme")

	var hooks tenantCreatedHooks
	hooks.TenantCreated(context.Background(), "acme")
	hooks.Register(nil)
	if got := hooks.observers.Load(); got != nil {
		t.Fatalf("observers after registering nil = %v, want none", *got)
	}
}
