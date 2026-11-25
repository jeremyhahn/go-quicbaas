// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Helper
// =============================================================================

func newTestTenantGuard() *TenantGuard {
	return NewTenantGuard(&agentTestLogger{})
}

// tenantCtx creates a context with a tenant ID set.
func tenantCtx(tenantID string) context.Context {
	return ContextWithTenant(context.Background(), tenantID)
}

// superAdminCtx creates a context with both tenant and super admin flags set.
// =============================================================================
// ValidateTenantAccess Tests
// =============================================================================

func TestValidateTenantAccess_SameTenant_Allowed(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	err := guard.ValidateTenantAccess(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("expected no error for same tenant, got: %v", err)
	}
}

func TestValidateTenantAccess_DifferentTenant_Blocked(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	err := guard.ValidateTenantAccess(ctx, "tenant-2")
	if err == nil {
		t.Fatal("expected error for cross-tenant access")
	}
	if !errors.Is(err, ErrTenantAccessDenied) {
		t.Errorf("expected ErrTenantAccessDenied, got: %v", err)
	}

	var accessErr *TenantAccessDeniedError
	if !errors.As(err, &accessErr) {
		t.Fatal("expected TenantAccessDeniedError type")
	}
	if accessErr.RequestingTenant != "tenant-1" {
		t.Errorf("RequestingTenant = %q, want %q", accessErr.RequestingTenant, "tenant-1")
	}
	if accessErr.TargetTenant != "tenant-2" {
		t.Errorf("TargetTenant = %q, want %q", accessErr.TargetTenant, "tenant-2")
	}
}

func TestValidateTenantAccess_EmptyTenant_Blocked(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := context.Background() // no tenant set

	err := guard.ValidateTenantAccess(ctx, "tenant-1")
	if err == nil {
		t.Fatal("expected error for empty tenant")
	}
	if !errors.Is(err, ErrTenantRequired) {
		t.Errorf("expected ErrTenantRequired, got: %v", err)
	}

	var reqErr *TenantRequiredError
	if !errors.As(err, &reqErr) {
		t.Fatal("expected TenantRequiredError type")
	}
}

func TestValidateTenantAccess_EmptyTargetTenant_Blocked(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	err := guard.ValidateTenantAccess(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty target tenant")
	}
	if !errors.Is(err, ErrTenantRequired) {
		t.Errorf("expected ErrTenantRequired, got: %v", err)
	}
}

func TestValidateTenantAccess_CancelledContext(t *testing.T) {
	guard := newTestTenantGuard()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := guard.ValidateTenantAccess(ctx, "tenant-1")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// IsSuperAdminBlocked Tests
// =============================================================================

// =============================================================================
// EnforceTenantPrefix Tests
// =============================================================================

func TestEnforceTenantPrefix_AddsPrefix(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	key, err := guard.EnforceTenantPrefix(ctx, "sessions/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "tenant-1/sessions/abc" {
		t.Errorf("key = %q, want %q", key, "tenant-1/sessions/abc")
	}
}

func TestEnforceTenantPrefix_AlreadyPrefixed(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	key, err := guard.EnforceTenantPrefix(ctx, "tenant-1/sessions/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "tenant-1/sessions/abc" {
		t.Errorf("key = %q, want %q", key, "tenant-1/sessions/abc")
	}
}

func TestEnforceTenantPrefix_SimpleKeyNoSlash(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	key, err := guard.EnforceTenantPrefix(ctx, "mykey")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "tenant-1/mykey" {
		t.Errorf("key = %q, want %q", key, "tenant-1/mykey")
	}
}

func TestEnforceTenantPrefix_RejectsCrossTenantKey(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	// Path traversal attacks are rejected
	_, err := guard.EnforceTenantPrefix(ctx, "../tenant-2/sessions/abc")
	if err == nil {
		t.Fatal("expected error for path traversal key")
	}
	if !errors.Is(err, ErrInvalidTenantPrefix) {
		t.Errorf("expected ErrInvalidTenantPrefix, got: %v", err)
	}

	var prefixErr *InvalidTenantPrefixError
	if !errors.As(err, &prefixErr) {
		t.Fatal("expected InvalidTenantPrefixError type")
	}
	if prefixErr.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", prefixErr.TenantID, "tenant-1")
	}
}

func TestEnforceTenantPrefix_CrossTenantKeySafelyNamespaced(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	// A key that looks like another tenant's data gets safely namespaced
	// under the requesting tenant, preventing data leakage
	key, err := guard.EnforceTenantPrefix(ctx, "tenant-2/sessions/abc")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "tenant-1/tenant-2/sessions/abc" {
		t.Errorf("key = %q, want %q", key, "tenant-1/tenant-2/sessions/abc")
	}
}

func TestEnforceTenantPrefix_RejectsPathTraversal(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	traversalKeys := []string{
		"../secret",
		"data/../other-tenant/key",
		"sessions/..data",
	}

	for _, key := range traversalKeys {
		_, err := guard.EnforceTenantPrefix(ctx, key)
		if err == nil {
			t.Errorf("expected error for path traversal key %q", key)
		}
		if !errors.Is(err, ErrInvalidTenantPrefix) {
			t.Errorf("key %q: expected ErrInvalidTenantPrefix, got: %v", key, err)
		}
	}
}

func TestEnforceTenantPrefix_EmptyTenant_Blocked(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := context.Background()

	_, err := guard.EnforceTenantPrefix(ctx, "some-key")
	if err == nil {
		t.Fatal("expected error for empty tenant")
	}
	if !errors.Is(err, ErrTenantRequired) {
		t.Errorf("expected ErrTenantRequired, got: %v", err)
	}
}

func TestEnforceTenantPrefix_EmptyKey(t *testing.T) {
	guard := newTestTenantGuard()
	ctx := tenantCtx("tenant-1")

	_, err := guard.EnforceTenantPrefix(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !errors.Is(err, ErrInvalidTenantPrefix) {
		t.Errorf("expected ErrInvalidTenantPrefix, got: %v", err)
	}
}

func TestEnforceTenantPrefix_CancelledContext(t *testing.T) {
	guard := newTestTenantGuard()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := guard.EnforceTenantPrefix(ctx, "some-key")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// Context Helper Tests
// =============================================================================

func TestTenantFromContext_Set(t *testing.T) {
	ctx := ContextWithTenant(context.Background(), "tenant-abc")
	got := TenantFromContext(ctx)
	if got != "tenant-abc" {
		t.Errorf("TenantFromContext = %q, want %q", got, "tenant-abc")
	}
}

func TestTenantFromContext_NotSet(t *testing.T) {
	got := TenantFromContext(context.Background())
	if got != "" {
		t.Errorf("TenantFromContext = %q, want empty", got)
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestTenantAccessDeniedError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &TenantAccessDeniedError{RequestingTenant: "t1", TargetTenant: "t2"}
		want := "orchestration: tenant access denied: requesting=t1 target=t2"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &TenantAccessDeniedError{RequestingTenant: "t1", TargetTenant: "t2"}
		if !errors.Is(err, ErrTenantAccessDenied) {
			t.Error("errors.Is should match ErrTenantAccessDenied")
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &TenantAccessDeniedError{RequestingTenant: "t1", TargetTenant: "t2"}
		if errors.Is(err, ErrTenantRequired) {
			t.Error("should not match ErrTenantRequired")
		}
	})
}

func TestTenantRequiredError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &TenantRequiredError{Operation: "SpawnSession"}
		want := "orchestration: tenant ID required: SpawnSession"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &TenantRequiredError{Operation: "SpawnSession"}
		if !errors.Is(err, ErrTenantRequired) {
			t.Error("errors.Is should match ErrTenantRequired")
		}
	})
}

func TestInvalidTenantPrefixError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &InvalidTenantPrefixError{Key: "bad/key", TenantID: "t1"}
		want := "orchestration: invalid tenant prefix: key=bad/key tenant=t1"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &InvalidTenantPrefixError{Key: "bad/key", TenantID: "t1"}
		if !errors.Is(err, ErrInvalidTenantPrefix) {
			t.Error("errors.Is should match ErrInvalidTenantPrefix")
		}
	})
}

// TestOrchestrationSystemTenantIDMatchesQRDB guards the literal duplicated in
// this package (necessary so the dependency-light standalone agent binary
// avoids importing go-qrdb/sdk/go) against drift from qrdb's canonical system
// tenant id. This test file is compiled only for `go test`, so it does not
// pull qrdbsdk into the pkg/fleet production binary.
func TestOrchestrationSystemTenantIDMatchesQRDB(t *testing.T) {
	if SystemTenantID != qrdbsdk.SystemTenantID {
		t.Errorf("SystemTenantID = %q, want %q (qrdbsdk.SystemTenantID)", SystemTenantID, qrdbsdk.SystemTenantID)
	}
}
