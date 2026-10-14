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
	stderrors "errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

func addCredentialCaller(id, tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            id,
		TenantID:      tenantID,
		Authenticated: true,
		IDKind:        interfaces.SubjectIDUsername,
	})
}

// accountWebAuthnGetter is a WebAuthn manager getter that also supplies the
// identity store BeginAddCredential resolves the caller in, as *quicBaaS does.
type accountWebAuthnGetter struct {
	fakeWebAuthnManagerGetter
	store *iam.IdentityStore
	err   error
}

func (g *accountWebAuthnGetter) GetIdentityStore(context.Context) (*iam.IdentityStore, error) {
	return g.store, g.err
}

// TestFIDO2Service_BeginAddCredential_RefusesUnauthenticatedCaller pins that
// the account is never taken from anywhere but an authenticated identity: no
// subject, or an unauthenticated one, is a typed 401-class refusal.
func TestFIDO2Service_BeginAddCredential_RefusesUnauthenticatedCaller(t *testing.T) {
	svc := NewFIDO2Service(&fakeWebAuthnManagerGetter{})
	unauthenticated := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "42", TenantID: "tenant-a"})

	for name, ctx := range map[string]context.Context{
		"no subject":              context.Background(),
		"unauthenticated subject": unauthenticated,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := svc.BeginAddCredential(ctx, "tenant-a")
			if !IsCallerAccountRefused(err) {
				t.Fatalf("BeginAddCredential = %v, want a caller refusal", err)
			}
			if !stderrors.Is(err, &errors.QBError{Code: errors.ErrUnauthorized}) {
				t.Fatalf("BeginAddCredential = %v, want ErrUnauthorized", err)
			}
		})
	}
}

// TestFIDO2Service_BeginAddCredential_RefusesAnotherTenant pins that a caller
// cannot add a passkey in a tenant other than its own, whatever tenant the
// request names.
func TestFIDO2Service_BeginAddCredential_RefusesAnotherTenant(t *testing.T) {
	svc := NewFIDO2Service(&fakeWebAuthnManagerGetter{})

	_, err := svc.BeginAddCredential(addCredentialCaller("42", "tenant-a"), "tenant-b")
	var cross *tenantctx.CrossTenantError
	if !stderrors.As(err, &cross) {
		t.Fatalf("BeginAddCredential = %v, want *tenantctx.CrossTenantError", err)
	}
	if cross.CallerTenant != "tenant-a" || cross.TargetTenant != "tenant-b" {
		t.Fatalf("cross-tenant error = %+v, want tenant-a -> tenant-b", cross)
	}
	if !stderrors.Is(err, &errors.QBError{Code: errors.ErrForbidden}) {
		t.Fatalf("BeginAddCredential = %v, want ErrForbidden", err)
	}
}

// TestFIDO2Service_BeginAddCredential_RefusesCallerWithoutAccount pins the
// identity shapes that cannot name a user account: no tenant, and a subject
// id that is not a qrdb user id.
func TestFIDO2Service_BeginAddCredential_RefusesCallerWithoutAccount(t *testing.T) {
	accounts := newCallerAccounts(t)
	svc := NewFIDO2Service(&accountWebAuthnGetter{store: accounts.store})

	if _, err := svc.BeginAddCredential(addCredentialCaller("admin", ""), ""); !IsFIDO2RequestInvalid(err) {
		t.Errorf("tenantless caller = %v, want ErrFIDO2TenantRequired", err)
	}
	for _, subject := range []string{"svc-account", "bob"} {
		_, err := svc.BeginAddCredential(addCredentialCaller(subject, "tenant-a"), "")
		if !IsCallerAccountRefused(err) || !stderrors.Is(err, &errors.QBError{Code: errors.ErrForbidden}) {
			t.Errorf("subject %q = %v, want a forbidden caller refusal", subject, err)
		}
	}
}

// An mTLS administrator names its account by username; BeginAddCredential
// resolves it and goes on to the WebAuthn step, which this getter leaves
// unconfigured.
func TestFIDO2Service_BeginAddCredential_ResolvesMTLSAdmin(t *testing.T) {
	accounts := newCallerAccounts(t)
	svc := NewFIDO2Service(&accountWebAuthnGetter{
		fakeWebAuthnManagerGetter: fakeWebAuthnManagerGetter{mgr: &fakeLegacyWebAuthnManager{}},
		store:                     accounts.store,
	})

	_, err := svc.BeginAddCredential(addCredentialCaller("admin", "tenant-a"), "tenant-a")
	if IsCallerAccountRefused(err) {
		t.Fatalf("mTLS admin refused: %v", err)
	}
	if !IsFIDO2ServiceUnavailable(err) {
		t.Fatalf("BeginAddCredential = %v, want to reach the WebAuthn step", err)
	}
}

func TestFIDO2Service_BeginAddCredential_IdentityStoreFails(t *testing.T) {
	svc := NewFIDO2Service(&accountWebAuthnGetter{err: stderrors.New("store offline")})

	_, err := svc.BeginAddCredential(addCredentialCaller("admin", "tenant-a"), "")
	if !stderrors.Is(err, &errors.QBError{Code: errors.ErrConfiguration}) {
		t.Fatalf("BeginAddCredential = %v, want a configuration error", err)
	}
}

// TestFIDO2Service_BeginAddCredential_NotConfigured pins that an authorized
// caller still gets the typed unavailable error when no xkms-backed manager
// is wired, and that the request tenant defaults to the caller's.
func TestFIDO2Service_BeginAddCredential_NotConfigured(t *testing.T) {
	ctx := addCredentialCaller("admin", "tenant-a")

	// A legacy WebAuthn manager is covered by ResolvesMTLSAdmin.
	for name, svc := range map[string]FIDO2Service{
		"nil getter":        NewFIDO2Service(nil),
		"no identity store": NewFIDO2Service(&fakeWebAuthnManagerGetter{}),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := svc.BeginAddCredential(ctx, ""); !IsFIDO2ServiceUnavailable(err) {
				t.Fatalf("BeginAddCredential = %v, want ErrFIDO2NotConfigured", err)
			}
		})
	}
}
