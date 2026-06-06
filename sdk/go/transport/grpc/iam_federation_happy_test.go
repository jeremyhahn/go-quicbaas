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

package grpc

import (
	"context"
	"testing"

	"google.golang.org/grpc"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
)

// fakeFederationServer implements FederationServiceServer.
type fakeFederationServer struct {
	iampb.UnimplementedFederationServiceServer
	fail bool
}

func (f *fakeFederationServer) err() error { return failErr(f.fail) }

func sampleProvider() *iampb.ExternalProvider {
	return &iampb.ExternalProvider{
		Id: "pr1", TenantId: "t1", Name: "okta", Type: iampb.ProviderType_PROVIDER_TYPE_OIDC,
		Enabled: true, ClientId: "c1", IssuerUrl: "https://issuer", Scopes: []string{"openid"},
		AutoProvision: true, AllowLinking: true, DefaultRoles: []string{"user"},
		CreatedAt: iamTime(), UpdatedAt: iamTime(),
	}
}

func sampleFederatedIdentity() *iampb.FederatedIdentity {
	return &iampb.FederatedIdentity{
		Id: "fi1", UserId: "u1", TenantId: "t1", ProviderId: "pr1",
		ExternalId: "ext1", ExternalEmail: "a@b.c", ExternalName: "Alice",
		CreatedAt: iamTime(), LastLogin: iamTime(),
	}
}

func (f *fakeFederationServer) CreateProvider(context.Context, *iampb.CreateProviderRequest) (*iampb.ExternalProvider, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleProvider(), nil
}
func (f *fakeFederationServer) GetProvider(context.Context, *iampb.GetProviderRequest) (*iampb.ExternalProvider, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleProvider(), nil
}
func (f *fakeFederationServer) UpdateProvider(context.Context, *iampb.UpdateProviderRequest) (*iampb.ExternalProvider, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return sampleProvider(), nil
}
func (f *fakeFederationServer) DeleteProvider(context.Context, *iampb.DeleteProviderRequest) (*iampb.DeleteProviderResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.DeleteProviderResponse{}, nil
}
func (f *fakeFederationServer) ListProviders(context.Context, *iampb.ListProvidersRequest) (*iampb.ListProvidersResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.ListProvidersResponse{Providers: []*iampb.ExternalProvider{sampleProvider()}, Total: 1}, nil
}
func (f *fakeFederationServer) LinkAccount(context.Context, *iampb.LinkAccountRequest) (*iampb.LinkAccountResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.LinkAccountResponse{Success: true}, nil
}
func (f *fakeFederationServer) UnlinkAccount(context.Context, *iampb.UnlinkAccountRequest) (*iampb.UnlinkAccountResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.UnlinkAccountResponse{}, nil
}
func (f *fakeFederationServer) GetUserLinks(context.Context, *iampb.GetUserLinksRequest) (*iampb.GetUserLinksResponse, error) {
	if err := f.err(); err != nil {
		return nil, err
	}
	return &iampb.GetUserLinksResponse{Identities: []*iampb.FederatedIdentity{sampleFederatedIdentity()}}, nil
}

func newFederationHandler(t *testing.T, fail bool) *iamHandler {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		iampb.RegisterFederationServiceServer(s, &fakeFederationServer{fail: fail})
	})
	return newIAMHandler(srv.dial(t))
}

var iamFederationCases = []iamCase{
	{"CreateProvider", "quicbaas.IAM.Federation", "CreateProvider", map[string]interface{}{"tenant_id": "t1", "provider_id": "pr1", "name": "okta", "type": "oidc", "scopes": []string{"openid"}}},
	{"GetProvider", "quicbaas.IAM.Federation", "GetProvider", map[string]interface{}{"tenant_id": "t1", "provider_id": "pr1"}},
	{"UpdateProvider", "quicbaas.IAM.Federation", "UpdateProvider", map[string]interface{}{"tenant_id": "t1", "provider_id": "pr1", "name": "okta2", "enabled": true}},
	{"DeleteProvider", "quicbaas.IAM.Federation", "DeleteProvider", map[string]interface{}{"tenant_id": "t1", "provider_id": "pr1"}},
	{"ListProviders", "quicbaas.IAM.Federation", "ListProviders", map[string]interface{}{"tenant_id": "t1"}},
	// Identity service maps to FederationService.
	{"LinkIdentity", "quicbaas.IAM.Identity", "LinkIdentity", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "provider_id": "pr1", "auth_code": "code"}},
	{"UnlinkIdentity", "quicbaas.IAM.Identity", "UnlinkIdentity", map[string]interface{}{"tenant_id": "t1", "user_id": "u1", "provider_id": "pr1"}},
	{"ListUserIdentities", "quicbaas.IAM.Identity", "ListUserIdentities", map[string]interface{}{"tenant_id": "t1", "user_id": "u1"}},
}

func TestIAMFederation_Success(t *testing.T) {
	h := newFederationHandler(t, false)
	for _, tc := range iamFederationCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), tc.service, tc.method, tc.req)
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.name, err)
			}
		})
	}
}

func TestIAMFederation_ServerError(t *testing.T) {
	h := newFederationHandler(t, true)
	for _, tc := range iamFederationCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), tc.service, tc.method, tc.req)
			if err == nil {
				t.Fatalf("%s: expected error from failing server", tc.name)
			}
		})
	}
}
