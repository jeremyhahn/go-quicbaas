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
)

// issuanceBoundKey marks a context the test binder produced.
type issuanceBoundKey struct{}

// issuanceBinderRefusedError is the failure the refusing test binder returns.
type issuanceBinderRefusedError struct{}

func (*issuanceBinderRefusedError) Error() string { return "issuance binder refused" }

// recordingIssuanceBinder records the tenant of every read it binds and marks
// the context it returns, so a test can prove the bound context is the one
// the read ran under.
type recordingIssuanceBinder struct {
	tenants []string
}

func (b *recordingIssuanceBinder) bind(ctx context.Context, tenantID string) (context.Context, error) {
	b.tenants = append(b.tenants, tenantID)
	return context.WithValue(ctx, issuanceBoundKey{}, tenantID), nil
}

// TestIssueSessionToken_BindsIssuanceReads pins #1727: under go-qrdb v0.51 the
// embedded node authenticates every role and permission read, and issuance has
// no caller yet. The role-name read and the effective-permission read must
// both run under the wired binder, for the user's own tenant.
func TestIssueSessionToken_BindsIssuanceReads(t *testing.T) {
	rbac := &fakeRBACClient{permsByRole: map[string][]string{operatorRoleID: {"orchestrator:status"}}}
	binder := &recordingIssuanceBinder{}
	provider, storage := newCeremonyProviderWithBinder(t, &fakePermissionResolver{client: rbac}, binder.bind)
	seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{operatorRoleID})

	_, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:     burninOperator,
		TenantID:    burninTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if err != nil {
		t.Fatalf("IssueSessionToken: %v", err)
	}

	if len(binder.tenants) != 2 {
		t.Fatalf("binder bound %d reads (%v), want the role-name and permission reads", len(binder.tenants), binder.tenants)
	}
	for _, tenant := range binder.tenants {
		if tenant != burninTenant {
			t.Fatalf("binder was asked for tenant %q, want the user's tenant %q", tenant, burninTenant)
		}
	}
	if rbac.lastCtx == nil || rbac.lastCtx.Value(issuanceBoundKey{}) != burninTenant {
		t.Fatal("the permission read did not run under the bound context")
	}
}

// TestIssueSessionToken_IssuanceBinderErrorFailsIssuance is the error half: a
// binder that cannot bind fails issuance with its error instead of reading
// under the unauthenticated caller's context.
func TestIssueSessionToken_IssuanceBinderErrorFailsIssuance(t *testing.T) {
	refused := &issuanceBinderRefusedError{}
	provider, storage := newCeremonyProviderWithBinder(t, nil, func(ctx context.Context, _ string) (context.Context, error) {
		return nil, refused
	})
	seedCeremonyUser(t, storage, burninTenant, burninOperator, []string{operatorRoleID})

	token, _, err := provider.IssueSessionToken(context.Background(), SessionIdentity{
		Subject:     burninOperator,
		TenantID:    burninTenant,
		AuthMethod:  "fido2",
		MFAVerified: true,
	})
	if !errors.Is(err, refused) {
		t.Fatalf("IssueSessionToken error = %v, want the binder's refusal", err)
	}
	if token != "" {
		t.Fatal("a token was issued although its reads could not be bound")
	}
}

// TestResolveEffectivePermissions_BinderErrorFails pins the token-service half
// on its own: the permission read is refused, not run unbound.
func TestResolveEffectivePermissions_BinderErrorFails(t *testing.T) {
	refused := &issuanceBinderRefusedError{}
	rbac := &fakeRBACClient{}
	svc := &TokenService{
		permResolver: &fakePermissionResolver{client: rbac},
		issuanceReadBinder: func(ctx context.Context, _ string) (context.Context, error) {
			return nil, refused
		},
	}

	_, err := svc.resolveEffectivePermissions(context.Background(), &Claims{TenantID: burninTenant})
	if !errors.Is(err, refused) {
		t.Fatalf("resolveEffectivePermissions error = %v, want the binder's refusal", err)
	}
	if rbac.lastIdentity != nil {
		t.Fatal("the permission read ran although its context could not be bound")
	}
}
