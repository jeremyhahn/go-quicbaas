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
	"slices"
	"strings"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// errRBACUnavailable is the RBAC failure the fail-closed test injects.
var errRBACUnavailable = errors.New("rbac unavailable")

// newGrantorRBAC returns the qrdb RBAC double the grantor tests share: each
// role grants the colon permissions listed here.
func newGrantorRBAC() *fakeRBACClient {
	return &fakeRBACClient{permsByRole: map[string][]string{
		"reader":             {"x:read"},
		"x-admin":            {"x:*"},
		"orchestrator-admin": {"orchestrator:agent_heartbeat"},
	}}
}

// grantorContext binds a verified qrdb identity in tenant-1 holding roles.
func grantorContext(roles ...string) context.Context {
	return qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject:  "alice",
		TenantID: "tenant-1",
		Roles:    roles,
	})
}

// newGrantorTestApp registers a client-credentials app in tenant-1 on an
// AppManager whose grantor gate reads resolver.
func newGrantorTestApp(t *testing.T, resolver PermissionResolver) (*AppManager, AppStorage, *Application) {
	t.Helper()
	storage := NewMemoryStorage()
	var opts []AppManagerOption
	if resolver != nil {
		opts = append(opts, WithGrantorPermissions(resolver))
	}
	mgr := NewAppManager(storage, nil, opts...)
	app, err := mgr.RegisterApp(context.Background(), "tenant-1", RegisterAppOptions{
		Name:       "svc",
		GrantTypes: []string{GrantTypeClientCredentials},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}
	return mgr, storage, app
}

// assertForbidden fails unless err is coded permission denied.
func assertForbidden(t *testing.T, err error) {
	t.Helper()
	var coded *dberrors.QBError
	if !errors.As(err, &coded) || coded.Code != dberrors.ErrForbidden {
		t.Fatalf("err = %v, want a QBError coded %s", err, dberrors.ErrForbidden)
	}
}

// assertAppUnchanged fails unless the stored app still has the given name
// and scopes.
func assertAppUnchanged(t *testing.T, mgr *AppManager, clientID, name string, scopes []string) {
	t.Helper()
	got, err := mgr.GetAppInTenant(context.Background(), "tenant-1", clientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if got.Name != name {
		t.Fatalf("Name = %q, want %q (refused update must not land)", got.Name, name)
	}
	if !slices.Equal(got.Scopes, scopes) {
		t.Fatalf("Scopes = %v, want %v (refused update must not land)", got.Scopes, scopes)
	}
}

func TestUpdateAppGrantorGate_GrantedScopesLand(t *testing.T) {
	t.Parallel()
	rbac := newGrantorRBAC()
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: rbac})

	scopes := []string{"x:read", "openid"}
	if _, err := mgr.UpdateAppInTenant(grantorContext("reader"), "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &scopes}); err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}
	assertAppUnchanged(t, mgr, app.ClientID, "svc", scopes)
	if rbac.lastIdentity == nil || rbac.lastIdentity.Subject != "alice" {
		t.Fatalf("RBAC resolved %+v, want the caller alice", rbac.lastIdentity)
	}
}

func TestUpdateAppGrantorGate_UncoveredScopeRefused(t *testing.T) {
	t.Parallel()
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: newGrantorRBAC()})

	name := "renamed"
	scopes := []string{"x:read", "y:write"}
	_, err := mgr.UpdateAppInTenant(grantorContext("reader"), "tenant-1", app.ClientID,
		UpdateAppOptions{Name: &name, Scopes: &scopes})

	var notGrantable *ScopeNotGrantableError
	if !errors.As(err, &notGrantable) {
		t.Fatalf("err = %v, want *ScopeNotGrantableError", err)
	}
	if notGrantable.Scope != "y:write" || notGrantable.ClientID != app.ClientID {
		t.Fatalf("refusal = %+v, want scope y:write on %s", notGrantable, app.ClientID)
	}
	if !errors.Is(err, ErrScopeNotGrantable) {
		t.Fatalf("err = %v, want ErrScopeNotGrantable", err)
	}
	assertForbidden(t, err)
	assertAppUnchanged(t, mgr, app.ClientID, "svc", nil)
}

func TestUpdateAppGrantorGate_WildcardHolderGrants(t *testing.T) {
	t.Parallel()
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: newGrantorRBAC()})

	scopes := []string{"x:read", "x:write"}
	if _, err := mgr.UpdateAppInTenant(grantorContext("x-admin"), "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &scopes}); err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}
	assertAppUnchanged(t, mgr, app.ClientID, "svc", scopes)
}

func TestUpdateAppGrantorGate_SubjectProjectionGrants(t *testing.T) {
	t.Parallel()
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: newGrantorRBAC()})

	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "bob", TenantID: "tenant-1", Roles: []string{"reader"}, Authenticated: true,
	})
	scopes := []string{"x:read"}
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &scopes}); err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}
	assertAppUnchanged(t, mgr, app.ClientID, "svc", scopes)
}

func TestUpdateAppGrantorGate_FailsClosed(t *testing.T) {
	t.Parallel()
	failing := newGrantorRBAC()
	failing.err = errRBACUnavailable
	unauthenticated := auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID: "bob", TenantID: "tenant-1", Roles: []string{"reader"},
	})

	tests := map[string]struct {
		resolver PermissionResolver
		ctx      context.Context
	}{
		"no identity":               {resolver: &fakePermissionResolver{client: newGrantorRBAC()}, ctx: context.Background()},
		"unauthenticated subject":   {resolver: &fakePermissionResolver{client: newGrantorRBAC()}, ctx: unauthenticated},
		"no permission resolver":    {resolver: nil, ctx: grantorContext("reader")},
		"nil RBAC client":           {resolver: &fakePermissionResolver{}, ctx: grantorContext("reader")},
		"RBAC error":                {resolver: &fakePermissionResolver{client: failing}, ctx: grantorContext("reader")},
		"identity without the role": {resolver: &fakePermissionResolver{client: newGrantorRBAC()}, ctx: grantorContext()},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			mgr, _, app := newGrantorTestApp(t, tc.resolver)
			scopes := []string{"x:read"}
			_, err := mgr.UpdateAppInTenant(tc.ctx, "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &scopes})
			if err == nil {
				t.Fatal("update granting x:read succeeded, want a refusal")
			}
			assertForbidden(t, err)
			assertAppUnchanged(t, mgr, app.ClientID, "svc", nil)
		})
	}
}

func TestUpdateAppGrantorGate_RBACErrorIsKept(t *testing.T) {
	t.Parallel()
	failing := newGrantorRBAC()
	failing.err = errRBACUnavailable
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: failing})

	scopes := []string{"x:read"}
	_, err := mgr.UpdateAppInTenant(grantorContext("reader"), "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &scopes})
	var unresolved *GrantorUnresolvedError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v, want *GrantorUnresolvedError", err)
	}
	if !errors.Is(err, ErrGrantorUnresolved) || !errors.Is(err, errRBACUnavailable) {
		t.Fatalf("err = %v, want ErrGrantorUnresolved wrapping the RBAC error", err)
	}
}

func TestUpdateAppGrantorGate_UngatedUpdates(t *testing.T) {
	t.Parallel()
	// No resolver and no identity: anything that grants no permission passes.
	mgr, _, app := newGrantorTestApp(t, nil)

	name := "renamed"
	if _, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", app.ClientID, UpdateAppOptions{Name: &name}); err != nil {
		t.Fatalf("scope-free update: %v", err)
	}
	protocolOnly := []string{"openid", "profile"}
	if _, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &protocolOnly}); err != nil {
		t.Fatalf("protocol-scope update: %v", err)
	}
	assertAppUnchanged(t, mgr, app.ClientID, "renamed", protocolOnly)
}

func TestUpdateAppGrantorGate_RemovalIsGated(t *testing.T) {
	t.Parallel()
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: newGrantorRBAC()})
	held := []string{"x:read", "y:write"}
	if _, err := mgr.GrantScopes(context.Background(), "tenant-1", app.ClientID, held); err != nil {
		t.Fatalf("GrantScopes: %v", err)
	}

	// Dropping y:write still submits x:read, which a role-less caller cannot grant.
	kept := []string{"x:read"}
	_, err := mgr.UpdateAppInTenant(grantorContext(), "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &kept})
	assertForbidden(t, err)
	assertAppUnchanged(t, mgr, app.ClientID, "svc", held)

	// A caller holding x:read may submit the reduced set.
	if _, err := mgr.UpdateAppInTenant(grantorContext("reader"), "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &kept}); err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}
	assertAppUnchanged(t, mgr, app.ClientID, "svc", kept)
}

// TestUpdateAppGrantorGate_TokenCarriesOnlyGrantedScopes pins the end of the
// chain: a refused update leaves nothing a client-credentials token can carry,
// and a granted update is what the token endpoint then honours.
func TestUpdateAppGrantorGate_TokenCarriesOnlyGrantedScopes(t *testing.T) {
	t.Parallel()
	const tenantID = "automatethethings"
	provider, _, storage := newTenantRoutedOIDCProvider(t, tenantID)
	mgr := NewAppManager(storage, nil, WithGrantorPermissions(&fakePermissionResolver{client: newGrantorRBAC()}))
	app, err := mgr.RegisterApp(context.Background(), tenantID, RegisterAppOptions{
		Name:       "svc",
		GrantTypes: []string{GrantTypeClientCredentials},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}
	caller := qrdbsdk.WithIdentity(context.Background(), &qrdbsdk.Identity{
		Subject: "alice", TenantID: tenantID, Roles: []string{"reader"},
	})
	tokenCtx := tenantctx.WithTenant(context.Background(), TenantFromClientID(app.ClientID))
	token := func(scopes ...string) (*TokenResponse, error) {
		return provider.Token(tokenCtx, &TokenRequest{
			GrantType:    GrantTypeClientCredentials,
			ClientID:     app.ClientID,
			ClientSecret: app.ClientSecret,
			Scope:        scopes,
		})
	}

	refused := []string{"x:read", "y:write"}
	if _, err := mgr.UpdateAppInTenant(caller, tenantID, app.ClientID, UpdateAppOptions{Scopes: &refused}); err == nil {
		t.Fatal("update granting y:write succeeded, want a refusal")
	}
	var oauthErr *OAuthError
	if _, err := token("y:write"); !errors.As(err, &oauthErr) || oauthErr.ErrorCode != ErrOAuthInvalidScope {
		t.Fatalf("token for y:write after the refused update: err = %v, want invalid_scope", err)
	}
	resp, err := token()
	if err != nil {
		t.Fatalf("token with no requested scope: %v", err)
	}
	if strings.Contains(resp.Scope, "y:write") {
		t.Fatalf("token scope = %q, carries the refused y:write", resp.Scope)
	}

	granted := []string{"x:read"}
	if _, err := mgr.UpdateAppInTenant(caller, tenantID, app.ClientID, UpdateAppOptions{Scopes: &granted}); err != nil {
		t.Fatalf("granted update: %v", err)
	}
	resp, err = token("x:read")
	if err != nil {
		t.Fatalf("token for the granted x:read: %v", err)
	}
	if !slices.Contains(strings.Fields(resp.Scope), "x:read") {
		t.Fatalf("token scope = %q, want x:read", resp.Scope)
	}
}
