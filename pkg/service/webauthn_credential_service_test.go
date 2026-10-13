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
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// newCredentialTestStore builds an identity store over the in-memory IAM
// storage so the user-id-to-email translation runs against a real store.
func newCredentialTestStore(t *testing.T) (*iam.IdentityStore, string) {
	t.Helper()
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: mem,
		RoleStorage: mem,
	})
	user, err := store.CreateUser(context.Background(), "t1", iam.CreateUserOptions{
		Email:    "alice@example.com",
		Username: "alice",
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	return store, user.ID
}

// TestWebAuthnCredentialServiceScopeValidation fails closed on an incomplete
// scope. Every transport delegates its validation here, so a gap would be a gap
// on all of them at once.
func TestWebAuthnCredentialServiceScopeValidation(t *testing.T) {
	svc := NewWebAuthnCredentialServiceFor(nil, nil)
	ctx := context.Background()

	if _, err := svc.ListCredentials(ctx, "", "u1"); !errors.Is(err, ErrWebAuthnCredentialTenantRequired) {
		t.Errorf("missing tenant: err = %v, want ErrWebAuthnCredentialTenantRequired", err)
	}
	if _, err := svc.ListCredentials(ctx, "t1", ""); !errors.Is(err, ErrWebAuthnCredentialUserRequired) {
		t.Errorf("missing user: err = %v, want ErrWebAuthnCredentialUserRequired", err)
	}
	if err := svc.DeleteCredential(ctx, "", "u1", "id"); !errors.Is(err, ErrWebAuthnCredentialTenantRequired) {
		t.Errorf("delete missing tenant: err = %v", err)
	}
	if _, err := svc.DeleteCredentials(ctx, "t1", ""); !errors.Is(err, ErrWebAuthnCredentialUserRequired) {
		t.Errorf("delete-all missing user: err = %v", err)
	}
}

// TestWebAuthnCredentialServiceUnconfigured keeps a node without WebAuthn
// honest. Reporting an empty credential list instead would tell an operator
// investigating a blocked account that nothing is stored, which is the one
// wrong answer that ends the investigation.
func TestWebAuthnCredentialServiceUnconfigured(t *testing.T) {
	store, userID := newCredentialTestStore(t)
	svc := NewWebAuthnCredentialServiceFor(store, nil)

	if _, err := svc.ListCredentials(context.Background(), "t1", userID); !errors.Is(err, ErrWebAuthnCredentialNotConfigured) {
		t.Errorf("err = %v, want ErrWebAuthnCredentialNotConfigured", err)
	}
}

// TestWebAuthnCredentialServiceUnknownUser refuses a user that does not exist
// in the named tenant, rather than reaching the credential store with an empty
// key. This is the check that keeps every transport's verbs scoped to the same
// subject its authorization gate ran against.
func TestWebAuthnCredentialServiceUnknownUser(t *testing.T) {
	store, _ := newCredentialTestStore(t)
	svc := NewWebAuthnCredentialServiceFor(store, &stubWebAuthnManager{})

	if _, err := svc.ListCredentials(context.Background(), "t1", "no-such-user"); err == nil {
		t.Error("expected an error for a user that does not exist")
	}
	// A user that exists in another tenant must not resolve either.
	if _, err := svc.ListCredentials(context.Background(), "t2", "no-such-user"); err == nil {
		t.Error("expected an error for a user outside the named tenant")
	}
}

// TestWebAuthnCredentialServiceRejectsNonXKMSManager fails closed when the node
// runs a WebAuthn manager that is not the multi-tenant xkms one, rather than
// silently doing nothing.
func TestWebAuthnCredentialServiceRejectsNonXKMSManager(t *testing.T) {
	store, userID := newCredentialTestStore(t)
	svc := NewWebAuthnCredentialServiceFor(store, &stubWebAuthnManager{})

	_, err := svc.ListCredentials(context.Background(), "t1", userID)
	if !errors.Is(err, ErrWebAuthnCredentialNotConfigured) {
		t.Errorf("err = %v, want ErrWebAuthnCredentialNotConfigured", err)
	}
}

// stubWebAuthnManager is a WebAuthnManager that is not the xkms-backed
// implementation the credential verbs require.
type stubWebAuthnManager struct{}

func (stubWebAuthnManager) BeginRegistration(context.Context, string, string) (map[string]interface{}, error) {
	return nil, nil
}

func (stubWebAuthnManager) BeginInviteRegistration(context.Context, string, string) (map[string]interface{}, error) {
	return nil, nil
}

func (stubWebAuthnManager) FinishRegistration(context.Context, string, string, map[string]interface{}) (*iam.AttestationResult, error) {
	return nil, nil
}

func (stubWebAuthnManager) FinishInviteRegistration(context.Context, string, string, map[string]interface{}) error {
	return nil
}

func (stubWebAuthnManager) BeginLogin(context.Context, string, string) (map[string]interface{}, error) {
	return nil, nil
}

func (stubWebAuthnManager) FinishLogin(context.Context, string, map[string]interface{}) (*iam.User, error) {
	return nil, nil
}
