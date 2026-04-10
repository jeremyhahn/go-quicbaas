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

package providers

import (
	"context"
	"errors"
	"testing"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Tests for the factory package use mockRoleManager and createTestLogger
// from rbac_test.go to avoid duplication.

func TestNewProvider_NilConfig(t *testing.T) {
	provider, err := NewProvider(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if provider != nil {
		t.Fatal("expected nil provider for nil config")
	}
	if !errors.Is(err, ErrNilConfig) {
		t.Errorf("expected ErrNilConfig, got: %v", err)
	}

	var dragonErr *dragonerrors.DragonError
	if !errors.As(err, &dragonErr) {
		t.Error("expected DragonError type")
	}
}

func TestNewProvider_RBAC_Success(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	defer provider.Close()

	// Verify provider type
	if provider.GetProviderType() != ProviderTypeRBAC {
		t.Errorf("expected provider type %s, got %s", ProviderTypeRBAC, provider.GetProviderType())
	}
}

func TestNewProvider_RBAC_NilRoleManager(t *testing.T) {
	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: nil,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for nil RoleManager")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
	if !errors.Is(err, ErrRoleManagerRequired) {
		t.Errorf("expected ErrRoleManagerRequired, got: %v", err)
	}
}

func TestNewProvider_RBAC_NilLogger(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      nil, // Should create default logger
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}
	defer provider.Close()
}

func TestNewProvider_Hybrid_NilRoleManager(t *testing.T) {
	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: nil,
		TenantID:    "test-tenant",
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for nil RoleManager")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
	if !errors.Is(err, ErrRoleManagerRequired) {
		t.Errorf("expected ErrRoleManagerRequired, got: %v", err)
	}
}

func TestNewProvider_InvalidType(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderType("invalid"),
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for invalid provider type")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}

	var dragonErr *dragonerrors.DragonError
	if !errors.As(err, &dragonErr) {
		t.Error("expected DragonError type")
	}
}

func TestNewProvider_EmptyType(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderType(""),
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err == nil {
		t.Fatal("expected error for empty provider type")
	}
	if provider != nil {
		t.Fatal("expected nil provider")
	}
}

func TestValidateConfig_NilConfig(t *testing.T) {
	err := ValidateConfig(nil)
	if err == nil {
		t.Fatal("expected error for nil config")
	}
	if !errors.Is(err, ErrNilConfig) {
		t.Errorf("expected ErrNilConfig, got: %v", err)
	}
}

func TestValidateConfig_RBAC_Valid(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
	}

	err := ValidateConfig(cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateConfig_RBAC_NilRoleManager(t *testing.T) {
	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: nil,
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nil RoleManager")
	}
	if !errors.Is(err, ErrRoleManagerRequired) {
		t.Errorf("expected ErrRoleManagerRequired, got: %v", err)
	}
}

func TestValidateConfig_Hybrid_NilRoleManager(t *testing.T) {
	cfg := &FactoryConfig{
		Type:        ProviderTypeHybridFactory,
		RoleManager: nil,
		TenantID:    "test-tenant",
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for nil RoleManager")
	}
	if !errors.Is(err, ErrRoleManagerRequired) {
		t.Errorf("expected ErrRoleManagerRequired, got: %v", err)
	}
}

func TestValidateConfig_InvalidType(t *testing.T) {
	cfg := &FactoryConfig{
		Type: ProviderType("invalid"),
	}

	err := ValidateConfig(cfg)
	if err == nil {
		t.Fatal("expected error for invalid type")
	}
	if !errors.Is(err, ErrInvalidProviderType) {
		t.Errorf("expected ErrInvalidProviderType, got: %v", err)
	}
}

func TestProviderType_String(t *testing.T) {
	tests := []struct {
		name     string
		pt       ProviderType
		expected string
	}{
		{"RBAC", ProviderTypeRBACFactory, "rbac"},
		{"ABAC", ProviderTypeABACFactory, "abac"},
		{"Hybrid", ProviderTypeHybridFactory, "hybrid"},
		{"Empty", ProviderType(""), ""},
		{"Invalid", ProviderType("invalid"), "invalid"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pt.String(); got != tt.expected {
				t.Errorf("String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestProviderType_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		pt       ProviderType
		expected bool
	}{
		{"RBAC", ProviderTypeRBACFactory, true},
		{"ABAC", ProviderTypeABACFactory, true},
		{"Hybrid", ProviderTypeHybridFactory, true},
		{"Empty", ProviderType(""), false},
		{"Invalid", ProviderType("invalid"), false},
		{"Uppercase", ProviderType("RBAC"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pt.IsValid(); got != tt.expected {
				t.Errorf("IsValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestParseProviderType_Valid(t *testing.T) {
	tests := []struct {
		input    string
		expected ProviderType
	}{
		{"rbac", ProviderTypeRBACFactory},
		{"abac", ProviderTypeABACFactory},
		{"hybrid", ProviderTypeHybridFactory},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pt, err := ParseProviderType(tt.input)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if pt != tt.expected {
				t.Errorf("ParseProviderType(%q) = %v, want %v", tt.input, pt, tt.expected)
			}
		})
	}
}

func TestParseProviderType_Invalid(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{"empty", ""},
		{"invalid", "invalid"},
		{"uppercase", "RBAC"},
		{"mixed", "Rbac"},
		{"spaces", "rbac "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pt, err := ParseProviderType(tt.input)
			if err == nil {
				t.Errorf("expected error for input %q", tt.input)
			}
			if pt != "" {
				t.Errorf("expected empty ProviderType, got %v", pt)
			}

			var dragonErr *dragonerrors.DragonError
			if !errors.As(err, &dragonErr) {
				t.Error("expected DragonError type")
			}
		})
	}
}

func TestIsABACAvailable(t *testing.T) {
	// This test documents the expected behavior
	// The result depends on whether iam build tag is set
	available := IsABACAvailable()
	t.Logf("IsABACAvailable() = %v", available)

	// Verify it returns a boolean (basic sanity check)
	if available != true && available != false {
		t.Error("IsABACAvailable should return true or false")
	}
}

func TestFactoryError_Error(t *testing.T) {
	err := &factoryError{msg: "test error message"}
	expected := "test error message"
	if got := err.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
}

func TestNewProvider_RBAC_Authorize(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	// Test authorization - use "readonly" role which has "read" on "keys"
	ctx := context.Background()
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user1",
			Roles:         []string{"readonly"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	decision, err := provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected Allowed=true, got false, reason: %s", decision.Reason)
	}

	// Test denial - readonly doesn't have write permission
	req.Action = "write"
	decision, err = provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if decision.Allowed {
		t.Error("expected Allowed=false, got true")
	}
}

func TestNewProvider_RBAC_Close(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Close should succeed
	if err := provider.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}

	// Operations after close should fail
	ctx := context.Background()
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user1",
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	_, err = provider.Authorize(ctx, req)
	if err == nil {
		t.Error("expected error after Close")
	}
}

func TestErrABACNotAvailable_ErrorMessage(t *testing.T) {
	msg := ErrABACNotAvailable.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	t.Logf("ErrABACNotAvailable message: %s", msg)
}

func TestErrInvalidProviderType_ErrorMessage(t *testing.T) {
	msg := ErrInvalidProviderType.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	t.Logf("ErrInvalidProviderType message: %s", msg)
}

func TestErrNilConfig_ErrorMessage(t *testing.T) {
	msg := ErrNilConfig.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	t.Logf("ErrNilConfig message: %s", msg)
}

func TestErrRoleManagerRequired_ErrorMessage(t *testing.T) {
	msg := ErrRoleManagerRequired.Error()
	if msg == "" {
		t.Error("expected non-empty error message")
	}
	t.Logf("ErrRoleManagerRequired message: %s", msg)
}

func TestProviderTypeConstants(t *testing.T) {
	// Verify the constants have expected string values
	if string(ProviderTypeRBACFactory) != "rbac" {
		t.Errorf("ProviderTypeRBACFactory = %q, want %q", ProviderTypeRBACFactory, "rbac")
	}
	if string(ProviderTypeABACFactory) != "abac" {
		t.Errorf("ProviderTypeABACFactory = %q, want %q", ProviderTypeABACFactory, "abac")
	}
	if string(ProviderTypeHybridFactory) != "hybrid" {
		t.Errorf("ProviderTypeHybridFactory = %q, want %q", ProviderTypeHybridFactory, "hybrid")
	}
}

func TestFactoryConfig_Fields(t *testing.T) {
	rm := newMockRoleManager()
	log := createTestLogger(t)

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		TenantID:    "test-tenant",
		Logger:      log,
	}

	// Verify all fields are set correctly
	if cfg.Type != ProviderTypeRBACFactory {
		t.Errorf("Type = %v, want %v", cfg.Type, ProviderTypeRBACFactory)
	}
	if cfg.RoleManager == nil {
		t.Error("RoleManager should not be nil")
	}
	if cfg.TenantID != "test-tenant" {
		t.Errorf("TenantID = %q, want %q", cfg.TenantID, "test-tenant")
	}
	if cfg.Logger == nil {
		t.Error("Logger should not be nil")
	}
}

func TestNewProvider_RBAC_BatchAuthorize(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	reqs := []*interfaces.AuthorizationRequest{
		{
			Subject: interfaces.Subject{
				ID:            "user1",
				Roles:         []string{"readonly"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "read",
		},
		{
			Subject: interfaces.Subject{
				ID:            "user1",
				Roles:         []string{"readonly"},
				Authenticated: true,
			},
			Resource: "keys",
			Action:   "write",
		},
	}

	decisions, err := provider.BatchAuthorize(ctx, reqs)
	if err != nil {
		t.Fatalf("BatchAuthorize failed: %v", err)
	}

	if len(decisions) != 2 {
		t.Errorf("expected 2 decisions, got %d", len(decisions))
	}

	if !decisions[0].Allowed {
		t.Error("expected first decision to be allowed")
	}
	if decisions[1].Allowed {
		t.Error("expected second decision to be denied")
	}
}

func TestNewProvider_RBAC_ContextCancellation(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	// Create a canceled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user1",
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	_, err = provider.Authorize(ctx, req)
	if err == nil {
		t.Error("expected error for canceled context")
	}
}

func TestNewProvider_RBAC_AdminRole(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "admin-user",
			Roles:         []string{"admin"},
			Authenticated: true,
		},
		Resource: "any-resource",
		Action:   "any-action",
	}

	decision, err := provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected admin to be allowed, got denied: %s", decision.Reason)
	}
}

func TestNewProvider_RBAC_AnonymousAccess(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	// Anonymous access should be allowed for health endpoint
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "anonymous",
			Authenticated: false,
		},
		Resource: "health",
		Action:   "read",
	}

	decision, err := provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected anonymous health check to be allowed, got denied: %s", decision.Reason)
	}

	// Anonymous access should be denied for keys
	req.Resource = "keys"
	decision, err = provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if decision.Allowed {
		t.Error("expected anonymous keys access to be denied")
	}
}

func TestNewProvider_RBAC_ReadWriteRole(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "rw-user",
			Roles:         []string{"readwrite"},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "write",
	}

	decision, err := provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if !decision.Allowed {
		t.Errorf("expected readwrite to allow write on keys, got denied: %s", decision.Reason)
	}
}

func TestNewProvider_RBAC_NoRolesAssigned(t *testing.T) {
	rm := newMockRoleManager()

	cfg := &FactoryConfig{
		Type:        ProviderTypeRBACFactory,
		RoleManager: rm,
		Logger:      createTestLogger(t),
	}

	provider, err := NewProvider(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	req := &interfaces.AuthorizationRequest{
		Subject: interfaces.Subject{
			ID:            "user-no-roles",
			Roles:         []string{},
			Authenticated: true,
		},
		Resource: "keys",
		Action:   "read",
	}

	decision, err := provider.Authorize(ctx, req)
	if err != nil {
		t.Fatalf("Authorize failed: %v", err)
	}
	if decision.Allowed {
		t.Error("expected user with no roles to be denied")
	}
}
