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
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
)

// Failures on the SECOND write of a two-write verb used to be discarded. They
// are now reported as typed errors that say what settled and what did not.

// deleteRefusingKV is a tenantNamespacedKV whose deletes fail, so a credential
// envelope cannot be destroyed while everything else works.
type deleteRefusingKV struct {
	*tenantNamespacedKV
	err error
}

func (kv *deleteRefusingKV) Delete(context.Context, string) error { return kv.err }

func (kv *deleteRefusingKV) CompareAndDelete(context.Context, string, []byte) (bool, error) {
	return false, kv.err
}

// faultingAccountStore is an AccountStore whose Create or Touch fail on demand.
type faultingAccountStore struct {
	orchestration.AccountStore
	createErr error
	touchErr  error
}

func (s *faultingAccountStore) Create(ctx context.Context, account *orchestration.Account) error {
	if s.createErr != nil {
		return s.createErr
	}
	return s.AccountStore.Create(ctx, account)
}

func (s *faultingAccountStore) Touch(ctx context.Context, tenantID, accountID string, opts orchestration.TouchAccountOptions) error {
	if s.touchErr != nil {
		return s.touchErr
	}
	return s.AccountStore.Touch(ctx, tenantID, accountID, opts)
}

type cleanupFixture struct {
	store   *faultingAccountStore
	secrets *orchestration.AccountSecretStore
	service AccountService
}

func newCleanupFixture(t *testing.T, kvDeleteErr error) *cleanupFixture {
	t.Helper()
	base := newTenantNamespacedKV()
	var backend secrets.Backend = secrets.NewKVBackend(base)
	if kvDeleteErr != nil {
		backend = secrets.NewKVBackend(&deleteRefusingKV{tenantNamespacedKV: base, err: kvDeleteErr})
	}
	mgr, err := secrets.NewManager(&secrets.ManagerOptions{Backend: backend})
	if err != nil {
		t.Fatalf("secrets.NewManager() error = %v", err)
	}
	store := &faultingAccountStore{AccountStore: orchestration.NewAccountStore(base)}
	secretStore := orchestration.NewAccountSecretStore(mgr)
	return &cleanupFixture{store: store, secrets: secretStore, service: NewAccountService(store, secretStore)}
}

type kvDeleteRefusedError struct{}

func (kvDeleteRefusedError) Error() string { return "kv: delete refused" }

func TestAccountService_CreateAccount_RollbackFailureIsReported(t *testing.T) {
	f := newCleanupFixture(t, kvDeleteRefusedError{})
	f.store.createErr = &orchestration.AccountStoreError{Message: "raft not leader"}

	_, err := f.service.CreateAccount(systemAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-ant-x"},
	})

	var cleanup *AccountCredentialCleanupError
	if !errors.As(err, &cleanup) {
		t.Fatalf("CreateAccount() error = %v, want AccountCredentialCleanupError", err)
	}
	if cleanup.TenantID != nsTenant || cleanup.AccountID == "" {
		t.Fatalf("AccountCredentialCleanupError = %+v, want tenant and account set", cleanup)
	}
	// The triggering error stays reachable for transports mapping on type.
	if !errors.Is(err, orchestration.ErrAccountStore) {
		t.Fatalf("errors.Is(err, ErrAccountStore) = false for %v", err)
	}
	// The secret store reports the KV failure as its own typed store error,
	// carrying the KV's message; both must survive to the caller.
	if !errors.Is(cleanup.Cleanup, orchestration.ErrAccountStore) || !strings.Contains(err.Error(), "kv: delete refused") {
		t.Fatalf("cleanup failure not carried: %v", err)
	}
	// The envelope really is orphaned: still readable under the tenant.
	if _, getErr := f.secrets.Get(spawnContext(nsTenant), nsTenant, cleanup.AccountID); getErr != nil {
		t.Fatalf("orphaned envelope not readable: %v", getErr)
	}
}

func TestAccountService_CreateAccount_RollbackSucceedsReturnsCreateError(t *testing.T) {
	f := newCleanupFixture(t, nil)
	f.store.createErr = &orchestration.AccountInvalidError{Field: "id", Message: "duplicate"}

	_, err := f.service.CreateAccount(systemAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-ant-x"},
	})

	var invalid *AccountInvalidError
	if !errors.As(err, &invalid) {
		t.Fatalf("CreateAccount() error = %v, want AccountInvalidError", err)
	}
	var cleanup *AccountCredentialCleanupError
	if errors.As(err, &cleanup) {
		t.Fatalf("CreateAccount() reported a cleanup failure when the rollback succeeded: %v", err)
	}
	listed, listErr := f.service.ListAccounts(systemAdminContext(), &ListAccountsRequest{TenantID: nsTenant})
	if listErr != nil || listed.Total != 0 {
		t.Fatalf("ListAccounts() = %+v, %v; want an empty pool after the rolled-back create", listed, listErr)
	}
}

func TestAccountService_DeleteAccount_CredentialCleanupFailureIsReported(t *testing.T) {
	f := newCleanupFixture(t, kvDeleteRefusedError{})
	created, err := f.service.CreateAccount(systemAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-ant-x"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	// The store writes through the unwrapped KV, so the metadata half of the
	// delete succeeds; only the envelope destroy (through the refusing KV
	// beneath the secrets backend) fails.
	_, err = f.service.DeleteAccount(systemAdminContext(), &DeleteAccountRequest{TenantID: nsTenant, AccountID: created.Account.ID})

	var cleanup *AccountCredentialCleanupError
	if !errors.As(err, &cleanup) {
		t.Fatalf("DeleteAccount() error = %v, want AccountCredentialCleanupError", err)
	}
	if cleanup.Cause != nil {
		t.Fatalf("AccountCredentialCleanupError.Cause = %v for a delete, want nil (metadata delete succeeded)", cleanup.Cause)
	}
	if !errors.Is(cleanup.Cleanup, orchestration.ErrAccountStore) || !strings.Contains(err.Error(), "kv: delete refused") {
		t.Fatalf("cleanup failure not carried: %v", err)
	}
	if _, getErr := f.service.GetAccount(systemAdminContext(), &GetAccountRequest{TenantID: nsTenant, AccountID: created.Account.ID}); getErr == nil {
		t.Fatalf("GetAccount() succeeded after DeleteAccount(); the metadata record must be gone")
	}
}

func TestAccountService_RotateAccountCredential_RecordUpdateFailureIsReported(t *testing.T) {
	f := newCleanupFixture(t, nil)
	created, err := f.service.CreateAccount(systemAdminContext(), &CreateAccountRequest{
		TenantID: nsTenant, Provider: orchestration.AccountProviderClaude,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-ant-old"},
	})
	if err != nil {
		t.Fatalf("CreateAccount() error = %v", err)
	}
	f.store.touchErr = &orchestration.AccountStoreError{Message: "raft not leader"}

	_, err = f.service.RotateAccountCredential(systemAdminContext(), &RotateAccountCredentialRequest{
		TenantID: nsTenant, AccountID: created.Account.ID,
		Credential: &orchestration.AccountCredential{AccessToken: "sk-ant-new"},
	})

	var incomplete *AccountRotationIncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("RotateAccountCredential() error = %v, want AccountRotationIncompleteError", err)
	}
	if incomplete.TenantID != nsTenant || incomplete.AccountID != created.Account.ID {
		t.Fatalf("AccountRotationIncompleteError = %+v, want tenant/account of the rotated record", incomplete)
	}
	if !errors.Is(err, orchestration.ErrAccountStore) {
		t.Fatalf("errors.Is(err, ErrAccountStore) = false for %v", err)
	}
	// The replacement envelope IS the live one, as the error says.
	cred, getErr := f.secrets.Get(spawnContext(nsTenant), nsTenant, created.Account.ID)
	if getErr != nil || cred.AccessToken != "sk-ant-new" {
		t.Fatalf("stored credential = %+v, %v; want the replacement", cred, getErr)
	}
}

func TestAccountCredentialCleanupError_ErrorAndUnwrap(t *testing.T) {
	withCause := &AccountCredentialCleanupError{TenantID: "t1", AccountID: "a1", Cause: errors.New("create failed"), Cleanup: errors.New("destroy failed")}
	want := "account credential cleanup failed: tenant=t1 id=a1: envelope orphaned: destroy failed (after: create failed)"
	if withCause.Error() != want {
		t.Fatalf("Error() = %q, want %q", withCause.Error(), want)
	}
	if got := withCause.Unwrap(); len(got) != 2 {
		t.Fatalf("Unwrap() = %d errors, want 2", len(got))
	}

	deleteOnly := &AccountCredentialCleanupError{TenantID: "t1", AccountID: "a1", Cleanup: errors.New("destroy failed")}
	want = "account credential cleanup failed: tenant=t1 id=a1: envelope orphaned: destroy failed"
	if deleteOnly.Error() != want {
		t.Fatalf("Error() = %q, want %q", deleteOnly.Error(), want)
	}
	if got := deleteOnly.Unwrap(); len(got) != 1 {
		t.Fatalf("Unwrap() = %d errors, want 1", len(got))
	}
}

func TestAccountRotationIncompleteError_ErrorAndUnwrap(t *testing.T) {
	cause := errors.New("touch failed")
	err := &AccountRotationIncompleteError{TenantID: "t1", AccountID: "a1", Cause: cause}
	want := "account rotation incomplete: tenant=t1 id=a1: credential stored but record not updated: touch failed"
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(err, cause) = false")
	}
}
