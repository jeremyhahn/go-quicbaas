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

//go:build !iam

package providers

import (
	"errors"
	"testing"
)

// These tests verify behavior when ABAC is NOT available (iam build tag not set).
// They are only compiled when building without the iam tag.

func TestNewProvider_ABAC_NotAvailable(t *testing.T) {
	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		TenantID: "test-tenant",
		Logger:   createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for ABAC when not available")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
	if !errors.Is(err, ErrABACNotAvailable) {
		t.Errorf("expected ErrABACNotAvailable, got: %v", err)
	}
}

func TestNewProvider_Hybrid_NotAvailable(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: rm,
		TenantID:    "test-tenant",
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for Hybrid when ABAC not available")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
	if !errors.Is(err, ErrABACNotAvailable) {
		t.Errorf("expected ErrABACNotAvailable, got: %v", err)
	}
}

func TestValidateConfig_ABAC_NotAvailable(t *testing.T) {
	cfg := &FactoryConfig{
		Type:     ProviderTypeABACFactory,
		TenantID: "test-tenant",
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for ABAC when not available")
	}
	if !errors.Is(err, ErrABACNotAvailable) {
		t.Errorf("expected ErrABACNotAvailable, got: %v", err)
	}
}

func TestValidateConfig_Hybrid_NotAvailable(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: rm,
		TenantID:    "test-tenant",
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for Hybrid when ABAC not available")
	}
	if !errors.Is(err, ErrABACNotAvailable) {
		t.Errorf("expected ErrABACNotAvailable, got: %v", err)
	}
}
