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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// TestResolveRequestTenant pins the single tenant-scoping rule the audit
// surfaces apply to a caller-supplied tenant_id: every caller — the
// system-tenant principal included — is confined to its own authenticated
// tenant. The system-tenant cases are the go-quicbaas#356 regression
// guard: the former blanket lift let the platform operator read any
// tenant's audit chain, contradicting qrdb's scope model (cross-tenant
// reach exists only behind permissions registered ScopeCross, and no
// surface routed through this helper is gated on one).
func TestResolveRequestTenant(t *testing.T) {
	tests := []struct {
		name string
		// callerTenant is bound into the context; on a denial it is also
		// the caller tenant the error must report.
		callerTenant string
		requested    string
		want         string
		wantErr      bool
	}{
		{
			name:         "empty requested tenant defaults to the caller's own tenant",
			callerTenant: "tenant-a",
			requested:    "",
			want:         "tenant-a",
		},
		{
			name:         "caller's own tenant is honored",
			callerTenant: "tenant-a",
			requested:    "tenant-a",
			want:         "tenant-a",
		},
		{
			name:         "another tenant is rejected for an ordinary tenant caller",
			callerTenant: "tenant-a",
			requested:    "tenant-b",
			wantErr:      true,
		},
		{
			name:         "another tenant is rejected for a system-tenant caller (no operator lift)",
			callerTenant: qrdbsdk.SystemTenantID,
			requested:    "tenant-b",
			wantErr:      true,
		},
		{
			name:         "system-tenant caller naming its own system tenant is honored",
			callerTenant: qrdbsdk.SystemTenantID,
			requested:    qrdbsdk.SystemTenantID,
			want:         qrdbsdk.SystemTenantID,
		},
		{
			name:         "system-tenant caller with empty requested tenant stays in the system tenant",
			callerTenant: qrdbsdk.SystemTenantID,
			requested:    "",
			want:         qrdbsdk.SystemTenantID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tenantctx.WithTenant(context.Background(), tt.callerTenant)
			got, err := ResolveRequestTenant(ctx, tt.requested)

			if tt.wantErr {
				if err == nil {
					t.Fatalf("ResolveRequestTenant(%q from %q) = %q, want TenantScopeMismatchError",
						tt.requested, tt.callerTenant, got)
				}
				var mismatch *TenantScopeMismatchError
				if !errors.As(err, &mismatch) {
					t.Fatalf("ResolveRequestTenant error type = %T, want *TenantScopeMismatchError", err)
				}
				if mismatch.CallerTenant != tt.callerTenant || mismatch.RequestedTenant != tt.requested {
					t.Fatalf("TenantScopeMismatchError = {caller %q, requested %q}, want {caller %q, requested %q}",
						mismatch.CallerTenant, mismatch.RequestedTenant, tt.callerTenant, tt.requested)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveRequestTenant(%q from %q) unexpected error: %v", tt.requested, tt.callerTenant, err)
			}
			if got != tt.want {
				t.Fatalf("ResolveRequestTenant(%q from %q) = %q, want %q", tt.requested, tt.callerTenant, got, tt.want)
			}
		})
	}
}

// TestTenantScopeMismatchError_Message pins the error text both transports
// surface to a rejected caller: it names the caller's tenant and the
// tenant it asked for, and nothing else — no hint about whether the target
// tenant exists or holds events.
func TestTenantScopeMismatchError_Message(t *testing.T) {
	err := &TenantScopeMismatchError{CallerTenant: "tenant-a", RequestedTenant: "tenant-b"}
	want := "tenant scope: caller tenant tenant-a may not access tenant tenant-b"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
}

// TestResolveRequestTenant_UnboundCallerIsRefused pins go-quicbaas#1150: a
// caller with no tenant bound is refused with *tenantctx.MissingTenantError
// whatever it requests. It used to be canonicalized to the system tenant,
// which read an unknown caller as the platform principal; an empty tenant
// still never reaches storage as an unscoped filter, because nothing does.
func TestResolveRequestTenant_UnboundCallerIsRefused(t *testing.T) {
	for _, requested := range []string{"", "tenant-b", qrdbsdk.SystemTenantID} {
		got, err := ResolveRequestTenant(context.Background(), requested)
		var missing *tenantctx.MissingTenantError
		if !errors.As(err, &missing) {
			t.Fatalf("ResolveRequestTenant(%q) unbound = (%q, %v), want *tenantctx.MissingTenantError", requested, got, err)
		}
		if got != "" {
			t.Fatalf("ResolveRequestTenant(%q) unbound returned tenant %q alongside the refusal", requested, got)
		}
	}
}
