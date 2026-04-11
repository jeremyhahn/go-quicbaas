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
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// =============================================================================
// Test Logger
// =============================================================================

type orchestratorTestLogger struct{}

func (l *orchestratorTestLogger) Debug(msg string, args ...any)                                          {}
func (l *orchestratorTestLogger) Debugf(format string, args ...interface{})                 {}
func (l *orchestratorTestLogger) Info(msg string, args ...any)                                           {}
func (l *orchestratorTestLogger) Infof(format string, args ...interface{})                  {}
func (l *orchestratorTestLogger) Warn(msg string, args ...any)                                           {}
func (l *orchestratorTestLogger) Warnf(format string, args ...interface{})                  {}
func (l *orchestratorTestLogger) Error(msg string, args ...any)                                          {}
func (l *orchestratorTestLogger) Errorf(format string, args ...interface{})                 {}
func (l *orchestratorTestLogger) Fatal(msg string)                                          {}
func (l *orchestratorTestLogger) Fatalf(format string, args ...interface{})                 {}
func (l *orchestratorTestLogger) WithField(key string, value interface{}) interfaces.Logger { return l }
func (l *orchestratorTestLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}
func (l *orchestratorTestLogger) WithError(err error) interfaces.Logger { return l }

var _ interfaces.Logger = (*orchestratorTestLogger)(nil)

// =============================================================================
// Helper
// =============================================================================

func newTestAuthService(t *testing.T) *OrchestratorAuthService {
	t.Helper()
	store := NewInMemoryServiceAccountStore()
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("NewOrchestratorAuthService: %v", err)
	}
	return svc
}

func validCreateRequest() *CreateServiceAccountRequest {
	return &CreateServiceAccountRequest{
		TenantID:    "tenant-1",
		Name:        "claude-code-worker-1",
		BackendType: "acp",
		Scopes:      []string{"agent:spawn", "agent:prompt"},
		Metadata:    map[string]string{"env": "test"},
	}
}

// =============================================================================
// Constructor Tests
// =============================================================================

func TestNewOrchestratorAuthService_Success(t *testing.T) {
	store := NewInMemoryServiceAccountStore()
	svc, err := NewOrchestratorAuthService(store, &orchestratorTestLogger{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if svc == nil {
		t.Fatal("service should not be nil")
	}
}

func TestNewOrchestratorAuthService_NilStore(t *testing.T) {
	_, err := NewOrchestratorAuthService(nil, &orchestratorTestLogger{})
	if err == nil {
		t.Fatal("expected error for nil store")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestNewOrchestratorAuthService_NilLogger(t *testing.T) {
	store := NewInMemoryServiceAccountStore()
	_, err := NewOrchestratorAuthService(store, nil)
	if err == nil {
		t.Fatal("expected error for nil logger")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

// =============================================================================
// CreateServiceAccount Tests
// =============================================================================

func TestCreateServiceAccount_Success(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	account, creds, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if account.ID == "" {
		t.Error("account ID should not be empty")
	}
	if account.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", account.TenantID, "tenant-1")
	}
	if account.Name != "claude-code-worker-1" {
		t.Errorf("Name = %q, want %q", account.Name, "claude-code-worker-1")
	}
	if account.BackendType != "acp" {
		t.Errorf("BackendType = %q, want %q", account.BackendType, "acp")
	}
	if len(account.Scopes) != 2 {
		t.Errorf("Scopes length = %d, want 2", len(account.Scopes))
	}
	if account.Revoked {
		t.Error("account should not be revoked")
	}
	if creds.ClientID == "" {
		t.Error("ClientID should not be empty")
	}
	if creds.ClientSecret == "" {
		t.Error("ClientSecret should not be empty")
	}
	if account.CreatedAt.IsZero() {
		t.Error("CreatedAt should not be zero")
	}
}

func TestCreateServiceAccount_WithExpiry(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.ExpiresIn = 24 * time.Hour

	account, _, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if account.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}

	expectedMin := time.Now().UTC().Add(23 * time.Hour)
	if account.ExpiresAt.Before(expectedMin) {
		t.Errorf("ExpiresAt too early: %v", account.ExpiresAt)
	}
}

func TestCreateServiceAccount_WithDeviceCert(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.DeviceCert = "device-cert-cn-001"

	account, _, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if account.DeviceCert != "device-cert-cn-001" {
		t.Errorf("DeviceCert = %q, want %q", account.DeviceCert, "device-cert-cn-001")
	}
}

func TestCreateServiceAccount_DuplicateName(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	// Second create with same name should fail
	_, _, err = svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for duplicate name")
	}
	if !errors.Is(err, ErrServiceAccountExists) {
		t.Errorf("expected ErrServiceAccountExists, got: %v", err)
	}

	var existsErr *ServiceAccountExistsError
	if !errors.As(err, &existsErr) {
		t.Fatal("expected ServiceAccountExistsError type")
	}
	if existsErr.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", existsErr.TenantID, "tenant-1")
	}
}

func TestCreateServiceAccount_InvalidScope(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.Scopes = []string{"agent:spawn", "nocolon"}

	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for invalid scope")
	}
	if !errors.Is(err, ErrInvalidScope) {
		t.Errorf("expected ErrInvalidScope, got: %v", err)
	}

	var scopeErr *InvalidScopeError
	if !errors.As(err, &scopeErr) {
		t.Fatal("expected InvalidScopeError type")
	}
	if scopeErr.Scope != "nocolon" {
		t.Errorf("Scope = %q, want %q", scopeErr.Scope, "nocolon")
	}
}

func TestCreateServiceAccount_MissingTenantID(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.TenantID = ""

	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for missing tenant ID")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestCreateServiceAccount_MissingName(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.Name = ""

	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestCreateServiceAccount_MissingBackendType(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.BackendType = ""

	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for missing backend type")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestCreateServiceAccount_EmptyScopes(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.Scopes = nil

	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for empty scopes")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestCreateServiceAccount_DuplicateDeviceCert(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req1 := validCreateRequest()
	req1.DeviceCert = "shared-cert-cn"
	_, _, err := svc.CreateServiceAccount(ctx, req1)
	if err != nil {
		t.Fatalf("first create failed: %v", err)
	}

	req2 := validCreateRequest()
	req2.Name = "worker-2"
	req2.DeviceCert = "shared-cert-cn"
	_, _, err = svc.CreateServiceAccount(ctx, req2)
	if err == nil {
		t.Fatal("expected error for duplicate device cert")
	}
	if !errors.Is(err, ErrDeviceCertAlreadyBound) {
		t.Errorf("expected ErrDeviceCertAlreadyBound, got: %v", err)
	}
}

func TestCreateServiceAccount_CancelledContext(t *testing.T) {
	svc := newTestAuthService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := validCreateRequest()
	_, _, err := svc.CreateServiceAccount(ctx, req)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

// =============================================================================
// RotateCredentials Tests
// =============================================================================

func TestRotateCredentials_Success(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	account, originalCreds, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	newCreds, err := svc.RotateCredentials(ctx, account.ID)
	if err != nil {
		t.Fatalf("rotate failed: %v", err)
	}

	if newCreds.ClientID != originalCreds.ClientID {
		t.Error("ClientID should remain the same after rotation")
	}
	if newCreds.ClientSecret == originalCreds.ClientSecret {
		t.Error("ClientSecret should change after rotation")
	}
	if newCreds.ClientSecret == "" {
		t.Error("new ClientSecret should not be empty")
	}
}

func TestRotateCredentials_NotFound(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.RotateCredentials(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
	if !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("expected ErrServiceAccountNotFound, got: %v", err)
	}
}

func TestRotateCredentials_EmptyAccountID(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.RotateCredentials(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty account ID")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestRotateCredentials_RevokedAccount(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, _, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err = svc.RevokeServiceAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	_, err = svc.RotateCredentials(ctx, account.ID)
	if err == nil {
		t.Fatal("expected error for revoked account")
	}
	if !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("expected ErrServiceAccountNotFound, got: %v", err)
	}
}

// =============================================================================
// RevokeServiceAccount Tests
// =============================================================================

func TestRevokeServiceAccount_Success(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, _, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err = svc.RevokeServiceAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	// Verify account is revoked by checking it no longer appears in list
	accounts, err := svc.ListServiceAccounts(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	for _, a := range accounts {
		if a.ID == account.ID {
			t.Error("revoked account should not appear in list")
		}
	}
}

func TestRevokeServiceAccount_NotFound(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	err := svc.RevokeServiceAccount(ctx, "nonexistent-id")
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
	if !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("expected ErrServiceAccountNotFound, got: %v", err)
	}
}

func TestRevokeServiceAccount_EmptyAccountID(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	err := svc.RevokeServiceAccount(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty account ID")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

// =============================================================================
// BindDeviceCertificate Tests
// =============================================================================

func TestBindDeviceCert_Success(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, _, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err = svc.BindDeviceCertificate(ctx, account.ID, "new-cert-cn")
	if err != nil {
		t.Fatalf("bind failed: %v", err)
	}

	// Verify we can look up by cert
	found, err := svc.GetByDeviceCert(ctx, "new-cert-cn")
	if err != nil {
		t.Fatalf("get by cert failed: %v", err)
	}
	if found.ID != account.ID {
		t.Errorf("found ID = %q, want %q", found.ID, account.ID)
	}
}

func TestBindDeviceCert_AlreadyBound(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req1 := validCreateRequest()
	req1.DeviceCert = "existing-cert-cn"
	_, _, err := svc.CreateServiceAccount(ctx, req1)
	if err != nil {
		t.Fatalf("create first account failed: %v", err)
	}

	// Create a second account and try to bind the same cert
	req2 := validCreateRequest()
	req2.Name = "worker-2"
	account2, _, err := svc.CreateServiceAccount(ctx, req2)
	if err != nil {
		t.Fatalf("create second account failed: %v", err)
	}

	err = svc.BindDeviceCertificate(ctx, account2.ID, "existing-cert-cn")
	if err == nil {
		t.Fatal("expected error for already bound cert")
	}
	if !errors.Is(err, ErrDeviceCertAlreadyBound) {
		t.Errorf("expected ErrDeviceCertAlreadyBound, got: %v", err)
	}

	var boundErr *DeviceCertAlreadyBoundError
	if !errors.As(err, &boundErr) {
		t.Fatal("expected DeviceCertAlreadyBoundError type")
	}
	if boundErr.CertCN != "existing-cert-cn" {
		t.Errorf("CertCN = %q, want %q", boundErr.CertCN, "existing-cert-cn")
	}
}

func TestBindDeviceCert_EmptyAccountID(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	err := svc.BindDeviceCertificate(ctx, "", "some-cert")
	if err == nil {
		t.Fatal("expected error for empty account ID")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestBindDeviceCert_EmptyCertCN(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	err := svc.BindDeviceCertificate(ctx, "account-id", "")
	if err == nil {
		t.Fatal("expected error for empty cert CN")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestBindDeviceCert_AccountNotFound(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	err := svc.BindDeviceCertificate(ctx, "nonexistent", "cert-cn")
	if err == nil {
		t.Fatal("expected error for nonexistent account")
	}
	if !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("expected ErrServiceAccountNotFound, got: %v", err)
	}
}

// =============================================================================
// GetByDeviceCert Tests
// =============================================================================

func TestGetByDeviceCert_Success(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	req := validCreateRequest()
	req.DeviceCert = "lookup-cert-cn"
	account, _, err := svc.CreateServiceAccount(ctx, req)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	found, err := svc.GetByDeviceCert(ctx, "lookup-cert-cn")
	if err != nil {
		t.Fatalf("get by cert failed: %v", err)
	}
	if found.ID != account.ID {
		t.Errorf("found ID = %q, want %q", found.ID, account.ID)
	}
	if found.DeviceCert != "lookup-cert-cn" {
		t.Errorf("DeviceCert = %q, want %q", found.DeviceCert, "lookup-cert-cn")
	}
}

func TestGetByDeviceCert_NotFound(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.GetByDeviceCert(ctx, "nonexistent-cert")
	if err == nil {
		t.Fatal("expected error for nonexistent cert")
	}
	if !errors.Is(err, ErrServiceAccountNotFound) {
		t.Errorf("expected ErrServiceAccountNotFound, got: %v", err)
	}
}

func TestGetByDeviceCert_EmptyCertCN(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.GetByDeviceCert(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty cert CN")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

// =============================================================================
// ListServiceAccounts Tests
// =============================================================================

func TestListServiceAccounts_TenantIsolation(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	// Create accounts for tenant-1
	req1 := validCreateRequest()
	req1.TenantID = "tenant-1"
	req1.Name = "worker-1"
	_, _, err := svc.CreateServiceAccount(ctx, req1)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	req2 := validCreateRequest()
	req2.TenantID = "tenant-1"
	req2.Name = "worker-2"
	_, _, err = svc.CreateServiceAccount(ctx, req2)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// Create account for tenant-2
	req3 := validCreateRequest()
	req3.TenantID = "tenant-2"
	req3.Name = "worker-3"
	_, _, err = svc.CreateServiceAccount(ctx, req3)
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	// List tenant-1 accounts
	accounts1, err := svc.ListServiceAccounts(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(accounts1) != 2 {
		t.Errorf("tenant-1 accounts = %d, want 2", len(accounts1))
	}

	// List tenant-2 accounts
	accounts2, err := svc.ListServiceAccounts(ctx, "tenant-2")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(accounts2) != 1 {
		t.Errorf("tenant-2 accounts = %d, want 1", len(accounts2))
	}

	// Verify tenant-1 accounts don't contain tenant-2 data
	for _, a := range accounts1 {
		if a.TenantID != "tenant-1" {
			t.Errorf("found account with wrong tenant: %q", a.TenantID)
		}
	}

	// Verify tenant-2 accounts don't contain tenant-1 data
	for _, a := range accounts2 {
		if a.TenantID != "tenant-2" {
			t.Errorf("found account with wrong tenant: %q", a.TenantID)
		}
	}
}

func TestListServiceAccounts_EmptyTenantID(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	_, err := svc.ListServiceAccounts(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty tenant ID")
	}
	if !errors.Is(err, ErrServiceAccountRequired) {
		t.Errorf("expected ErrServiceAccountRequired, got: %v", err)
	}
}

func TestListServiceAccounts_EmptyResult(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	accounts, err := svc.ListServiceAccounts(ctx, "nonexistent-tenant")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("expected empty list, got %d accounts", len(accounts))
	}
}

func TestListServiceAccounts_ExcludesRevoked(t *testing.T) {
	svc := newTestAuthService(t)
	ctx := context.Background()

	account, _, err := svc.CreateServiceAccount(ctx, validCreateRequest())
	if err != nil {
		t.Fatalf("create failed: %v", err)
	}

	err = svc.RevokeServiceAccount(ctx, account.ID)
	if err != nil {
		t.Fatalf("revoke failed: %v", err)
	}

	accounts, err := svc.ListServiceAccounts(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	if len(accounts) != 0 {
		t.Errorf("expected empty list after revocation, got %d", len(accounts))
	}
}

// =============================================================================
// Error Type Tests
// =============================================================================

func TestServiceAccountNotFoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ServiceAccountNotFoundError{AccountID: "acc-123"}
		want := "orchestrator: service account not found: acc-123"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ServiceAccountNotFoundError{AccountID: "acc-123"}
		if !errors.Is(err, ErrServiceAccountNotFound) {
			t.Error("errors.Is should match ErrServiceAccountNotFound")
		}
	})

	t.Run("NotOtherSentinel", func(t *testing.T) {
		err := &ServiceAccountNotFoundError{AccountID: "x"}
		if errors.Is(err, ErrServiceAccountExists) {
			t.Error("should not match ErrServiceAccountExists")
		}
	})
}

func TestServiceAccountExistsError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ServiceAccountExistsError{TenantID: "t1", Name: "worker"}
		want := "orchestrator: service account already exists: tenant=t1 name=worker"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ServiceAccountExistsError{TenantID: "t1", Name: "worker"}
		if !errors.Is(err, ErrServiceAccountExists) {
			t.Error("errors.Is should match ErrServiceAccountExists")
		}
	})
}

func TestDeviceCertAlreadyBoundError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &DeviceCertAlreadyBoundError{CertCN: "cn1", AccountID: "acc1"}
		want := "orchestrator: device certificate already bound: cn=cn1 account=acc1"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &DeviceCertAlreadyBoundError{CertCN: "cn1", AccountID: "acc1"}
		if !errors.Is(err, ErrDeviceCertAlreadyBound) {
			t.Error("errors.Is should match ErrDeviceCertAlreadyBound")
		}
	})
}

func TestInvalidScopeError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &InvalidScopeError{Scope: "bad:scope"}
		want := "orchestrator: invalid scope: bad:scope"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &InvalidScopeError{Scope: "bad:scope"}
		if !errors.Is(err, ErrInvalidScope) {
			t.Error("errors.Is should match ErrInvalidScope")
		}
	})
}

func TestServiceAccountRequiredError(t *testing.T) {
	t.Run("Error", func(t *testing.T) {
		err := &ServiceAccountRequiredError{Field: "tenant_id"}
		want := "orchestrator: required field missing: tenant_id"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})

	t.Run("Unwrap", func(t *testing.T) {
		err := &ServiceAccountRequiredError{Field: "tenant_id"}
		if !errors.Is(err, ErrServiceAccountRequired) {
			t.Error("errors.Is should match ErrServiceAccountRequired")
		}
	})
}
