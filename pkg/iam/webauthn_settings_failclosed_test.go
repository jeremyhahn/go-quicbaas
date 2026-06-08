//go:build iam

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

package iam

import (
	"context"
	"errors"
	"testing"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// settingsTenantManager is a purpose-built TenantManager fake for the
// WebAuthnSettingsFor fail-closed tests. GetTenant returns the configured
// tenant or error so each case (found / not-found / read-error) can be driven
// independently.
type settingsTenantManager struct {
	tenant *Tenant
	err    error
}

func (m *settingsTenantManager) CreateTenant(ctx context.Context, opts CreateTenantOptions) (*Tenant, error) {
	return nil, nil
}

func (m *settingsTenantManager) GetTenant(ctx context.Context, id string) (*Tenant, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.tenant, nil
}

func (m *settingsTenantManager) UpdateTenant(ctx context.Context, id string, opts UpdateTenantOptions) (*Tenant, error) {
	return nil, nil
}

func (m *settingsTenantManager) DeleteTenant(ctx context.Context, id string) error { return nil }

func (m *settingsTenantManager) ListTenants(ctx context.Context, opts ListOptions) ([]*Tenant, error) {
	return nil, nil
}

func (m *settingsTenantManager) CheckQuota(ctx context.Context, tenantID, resourceType string) error {
	return nil
}

// TestWebAuthnSettingsFor_NotConfiguredTenant verifies the legitimate
// not-configured / not-found case falls back to the global RP (nil, nil),
// preserving today's behavior.
func TestWebAuthnSettingsFor_NotConfiguredTenant(t *testing.T) {
	adapter := NewTenantManagerWebAuthnSettings(&settingsTenantManager{
		err: NewTenantNotFoundError("missing-tenant"),
	})

	got, err := adapter.WebAuthnSettingsFor(context.Background(), "missing-tenant")
	if err != nil {
		t.Fatalf("not-found tenant must not error (global fallback), got: %v", err)
	}
	if got != nil {
		t.Fatalf("not-found tenant must yield nil override (global RP), got: %+v", got)
	}
}

// TestWebAuthnSettingsFor_StoreReadError verifies a genuine store/read error
// fails closed with a typed WebAuthnSettingsReadError rather than silently
// falling back to the global RP (isolation leak).
func TestWebAuthnSettingsFor_StoreReadError(t *testing.T) {
	cause := &dberrors.QBError{
		Code: dberrors.ErrStorage,
		Op:   "iam.tenant.get",
		Err:  errors.New("backend unavailable"),
	}
	adapter := NewTenantManagerWebAuthnSettings(&settingsTenantManager{err: cause})

	got, err := adapter.WebAuthnSettingsFor(context.Background(), "tenant-a")
	if err == nil {
		t.Fatal("store read error must fail closed, got nil error (global fallback leak)")
	}
	var readErr *WebAuthnSettingsReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("expected *WebAuthnSettingsReadError, got %T: %v", err, err)
	}
	if readErr.TenantID != "tenant-a" {
		t.Fatalf("expected TenantID tenant-a, got %q", readErr.TenantID)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("read error must wrap the underlying cause for errors.Is, got: %v", err)
	}
	if got != nil {
		t.Fatalf("fail-closed must not return an override, got: %+v", got)
	}
}

// TestWebAuthnSettingsFor_PerTenantOverride verifies a configured per-tenant
// override is still returned unchanged.
func TestWebAuthnSettingsFor_PerTenantOverride(t *testing.T) {
	override := &TenantWebAuthnConfig{
		RPID:          "tenant.example.com",
		RPDisplayName: "Tenant Example",
		RPOrigins:     []string{"https://tenant.example.com"},
	}
	tenant := &Tenant{
		ID:       "tenant-a",
		Settings: TenantSettings{WebAuthn: override},
	}
	adapter := NewTenantManagerWebAuthnSettings(&settingsTenantManager{tenant: tenant})

	got, err := adapter.WebAuthnSettingsFor(context.Background(), "tenant-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != override {
		t.Fatalf("expected per-tenant override returned, got: %+v", got)
	}
}

// TestWebAuthnSettingsFor_NilAdapter verifies the nil-safe path yields the
// global fallback.
func TestWebAuthnSettingsFor_NilAdapter(t *testing.T) {
	var adapter *TenantManagerWebAuthnSettings
	got, err := adapter.WebAuthnSettingsFor(context.Background(), "tenant-a")
	if err != nil || got != nil {
		t.Fatalf("nil adapter must yield (nil, nil), got (%+v, %v)", got, err)
	}

	adapter = NewTenantManagerWebAuthnSettings(nil)
	got, err = adapter.WebAuthnSettingsFor(context.Background(), "tenant-a")
	if err != nil || got != nil {
		t.Fatalf("nil manager must yield (nil, nil), got (%+v, %v)", got, err)
	}
}

// TestWebAuthnSettingsReadError_Message verifies the typed error renders a
// useful message with and without a cause.
func TestWebAuthnSettingsReadError_Message(t *testing.T) {
	withCause := &WebAuthnSettingsReadError{TenantID: "t1", Cause: errors.New("boom")}
	if withCause.Error() == "" {
		t.Fatal("expected non-empty message")
	}
	if withCause.Unwrap() == nil {
		t.Fatal("expected Unwrap to return the cause")
	}

	noCause := &WebAuthnSettingsReadError{TenantID: "t1"}
	if noCause.Error() == "" {
		t.Fatal("expected non-empty message without cause")
	}
	if noCause.Unwrap() != nil {
		t.Fatal("expected nil Unwrap when no cause")
	}

	var nilErr *WebAuthnSettingsReadError
	if nilErr.Error() == "" {
		t.Fatal("nil receiver must still render a message")
	}
	if nilErr.Unwrap() != nil {
		t.Fatal("nil receiver Unwrap must be nil")
	}
}
