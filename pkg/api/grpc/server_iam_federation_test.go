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

//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// federationTenantCalls calls every tenant-scoped Federation RPC naming
// tenant "acme".
func federationTenantCalls(srv *FederationServer) map[string]func(context.Context) error {
	return map[string]func(context.Context) error{
		"CreateProvider": func(ctx context.Context) error {
			_, err := srv.CreateProvider(ctx, &iampb.CreateProviderRequest{TenantId: "acme", Name: "google", ClientId: "c1"})
			return err
		},
		"GetProvider": func(ctx context.Context) error {
			_, err := srv.GetProvider(ctx, &iampb.GetProviderRequest{TenantId: "acme", ProviderId: "p1"})
			return err
		},
		"UpdateProvider": func(ctx context.Context) error {
			_, err := srv.UpdateProvider(ctx, &iampb.UpdateProviderRequest{TenantId: "acme", ProviderId: "p1", Name: "updated"})
			return err
		},
		"DeleteProvider": func(ctx context.Context) error {
			_, err := srv.DeleteProvider(ctx, &iampb.DeleteProviderRequest{TenantId: "acme", ProviderId: "p1"})
			return err
		},
		"ListProviders": func(ctx context.Context) error {
			_, err := srv.ListProviders(ctx, &iampb.ListProvidersRequest{TenantId: "acme"})
			return err
		},
		"LinkAccount": func(ctx context.Context) error {
			_, err := srv.LinkAccount(ctx, &iampb.LinkAccountRequest{UserId: "30001", TenantId: "acme", ProviderId: "p1", ExternalToken: "x"})
			return err
		},
		"UnlinkAccount": func(ctx context.Context) error {
			_, err := srv.UnlinkAccount(ctx, &iampb.UnlinkAccountRequest{UserId: "30001", TenantId: "acme", ProviderId: "p1"})
			return err
		},
		"GetUserLinks": func(ctx context.Context) error {
			_, err := srv.GetUserLinks(ctx, &iampb.GetUserLinksRequest{UserId: "30001", TenantId: "acme"})
			return err
		},
	}
}

// boundToAcme matches a context that carries tenant "acme".
func boundToAcme() interface{} {
	return mock.MatchedBy(func(ctx context.Context) bool {
		tenantID, err := tenantctx.FromContext(ctx, "grpc.Test")
		return err == nil && tenantID == "acme"
	})
}

// newAcmeFederationServer returns a Federation server whose manager answers
// every call only when the call's context is bound to tenant "acme".
func newAcmeFederationServer(t *testing.T) (*FederationServer, *mockFederationManager) {
	t.Helper()
	srv, mgr := newTestFederationServer(t)
	provider := &iam.ExternalProvider{ID: "p1", TenantID: "acme", Name: "google", Type: iam.ProviderTypeOIDC, ClientID: "c1", IssuerURL: "https://issuer.example.com"}
	mgr.On("RegisterProvider", boundToAcme(), mock.Anything).Return(nil)
	mgr.On("GetProvider", boundToAcme(), "acme", "p1").Return(provider, nil)
	mgr.On("UpdateProvider", boundToAcme(), mock.Anything).Return(nil)
	mgr.On("DeleteProvider", boundToAcme(), "acme", "p1").Return(nil)
	mgr.On("ListProviders", boundToAcme(), "acme", mock.Anything).Return([]*iam.ExternalProvider{provider}, nil)
	mgr.On("LinkAccount", boundToAcme(), uint64(30001), "acme", "p1", "x").Return(nil)
	mgr.On("UnlinkAccount", boundToAcme(), uint64(30001), "acme", "p1").Return(nil)
	mgr.On("GetUserLinks", boundToAcme(), uint64(30001), "acme").Return([]*iam.FederatedIdentity{}, nil)
	return srv, mgr
}

// TestFederationServer_SystemCallerActsInTheNamedTenant mirrors the REST rule
// (pkg/api/rest/tenant_lift_test.go, go-quicbaas#2741): a system-tenant caller
// that names tenant acme acts in acme, and the manager is called with a
// context bound to acme.
func TestFederationServer_SystemCallerActsInTheNamedTenant(t *testing.T) {
	srv, _ := newAcmeFederationServer(t)
	for method, call := range federationTenantCalls(srv) {
		t.Run(method, func(t *testing.T) {
			require.NoError(t, call(tenantctx.WithSystem(context.Background())))
		})
	}
}

func TestFederationServer_TenantCallerActsInItsOwnTenant(t *testing.T) {
	srv, _ := newAcmeFederationServer(t)
	for method, call := range federationTenantCalls(srv) {
		t.Run(method, func(t *testing.T) {
			require.NoError(t, call(callerContext("acme")))
		})
	}
}

// TestFederationServer_RefusesAnotherTenantAndAnUnboundCaller: a tenant
// caller naming another tenant, and a call with no tenant bound, are refused
// before the manager is reached.
func TestFederationServer_RefusesAnotherTenantAndAnUnboundCaller(t *testing.T) {
	srv, mgr := newTestFederationServer(t)
	callers := map[string]context.Context{
		"another tenant": callerContext("burnin"),
		"no tenant":      context.Background(),
	}
	for method, call := range federationTenantCalls(srv) {
		for caller, ctx := range callers {
			t.Run(method+"/"+caller, func(t *testing.T) {
				err := call(ctx)
				require.Equal(t, codes.PermissionDenied, status.Code(err), "err = %v", err)
			})
		}
	}
	require.Empty(t, mgr.Calls)
}
