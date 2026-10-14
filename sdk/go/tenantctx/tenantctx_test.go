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

package tenantctx_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	pkgtenantctx "github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/tenantctx"
)

// Compile-time proof each re-export keeps the pkg/tenantctx signature.
var (
	_ func(context.Context, string) context.Context         = tenantctx.WithTenant
	_ func(context.Context, string) (string, error)         = tenantctx.FromContext
	_ func(context.Context, string, string) (string, error) = tenantctx.Target
)

func TestReexports_AreThePkgFunctions(t *testing.T) {
	tests := []struct {
		name     string
		sdk, pkg any
	}{
		{name: "WithTenant", sdk: tenantctx.WithTenant, pkg: pkgtenantctx.WithTenant},
		{name: "FromContext", sdk: tenantctx.FromContext, pkg: pkgtenantctx.FromContext},
		{name: "Target", sdk: tenantctx.Target, pkg: pkgtenantctx.Target},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if reflect.ValueOf(tt.sdk).Pointer() != reflect.ValueOf(tt.pkg).Pointer() {
				t.Fatalf("sdk/go/tenantctx.%s is not pkg/tenantctx.%s", tt.name, tt.name)
			}
		})
	}
}

func TestFromContext_ReadsTheBoundTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(t.Context(), "acme")

	got, err := tenantctx.FromContext(ctx, "op")
	if err != nil {
		t.Fatalf("FromContext: %v", err)
	}
	if got != "acme" {
		t.Fatalf("FromContext = %q, want acme", got)
	}
}

func TestFromContext_RefusesAnUnboundContext(t *testing.T) {
	// An empty tenant binds nothing, so it must not read as the system tenant.
	ctx := tenantctx.WithTenant(t.Context(), "")

	_, err := tenantctx.FromContext(ctx, "op")
	var missing *pkgtenantctx.MissingTenantError
	if !errors.As(err, &missing) {
		t.Fatalf("FromContext error = %v, want *MissingTenantError", err)
	}
}

func TestTarget(t *testing.T) {
	ctx := tenantctx.WithTenant(t.Context(), "acme")

	tests := []struct {
		name      string
		requested string
		want      string
	}{
		{name: "empty means the caller's own tenant", requested: "", want: "acme"},
		{name: "the caller's own tenant", requested: "acme", want: "acme"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tenantctx.Target(ctx, "op", tt.requested)
			if err != nil {
				t.Fatalf("Target: %v", err)
			}
			if got != tt.want {
				t.Fatalf("Target = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTarget_RefusesAnotherTenant(t *testing.T) {
	ctx := tenantctx.WithTenant(t.Context(), "acme")

	_, err := tenantctx.Target(ctx, "op", "globex")
	var cross *pkgtenantctx.CrossTenantError
	if !errors.As(err, &cross) {
		t.Fatalf("Target error = %v, want *CrossTenantError", err)
	}
	if cross.CallerTenant != "acme" || cross.TargetTenant != "globex" {
		t.Fatalf("CrossTenantError = %+v, want caller acme, target globex", cross)
	}
}
