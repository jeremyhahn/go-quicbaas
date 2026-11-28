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

package embedded

import (
	"context"
	"slices"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// newAppRegisterFixture returns a tenant-a bound context and the service
// exposing a real, empty AppManager.
func newAppRegisterFixture(t *testing.T) (context.Context, *mockQuicBaaSForApps) {
	t.Helper()
	apps := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	return tenantctx.WithTenant(context.Background(), "tenant-a"), &mockQuicBaaSForApps{apps: apps}
}

// registerThroughHandler registers one app through the embedded handler and
// returns the reply.
func registerThroughHandler(t *testing.T, ctx context.Context, svc *mockQuicBaaSForApps, prefix string) *iamsdk.Application {
	t.Helper()
	req := &embeddedRegisterAppRequest{App: &iamsdk.RegisterAppRequest{
		Name:           "Registered",
		RedirectURIs:   []string{"https://registered.example/callback"},
		ResourcePrefix: prefix,
	}}
	resp, err := handleIAMAppRegister(ctx, svc, req)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	out, ok := resp.(*iamsdk.Application)
	if !ok {
		t.Fatalf("register returned %T, want *iam.Application", resp)
	}
	if out.ClientID == "" {
		t.Fatal("register returned no client id")
	}
	return out
}

func TestHandleIAMAppRegister_KeepsResourcePrefix(t *testing.T) {
	t.Parallel()
	ctx, svc := newAppRegisterFixture(t)
	const prefix = "apps/tenant-a/"

	out := registerThroughHandler(t, ctx, svc, prefix)
	if out.ResourcePrefix != prefix {
		t.Errorf("reply resource prefix = %q, want %q", out.ResourcePrefix, prefix)
	}
	stored, err := svc.apps.GetAppInTenant(context.Background(), "tenant-a", out.ClientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if stored.ResourcePrefix != prefix {
		t.Errorf("stored resource prefix = %q, want %q", stored.ResourcePrefix, prefix)
	}
}

func TestHandleIAMAppRegister_ReturnsTheIssuedSecretOnce(t *testing.T) {
	t.Parallel()
	ctx, svc := newAppRegisterFixture(t)

	out := registerThroughHandler(t, ctx, svc, "")
	if out.ClientSecret == "" {
		t.Fatal("register reply carries no client secret")
	}
	valid, err := svc.apps.ValidateClientCredentials(context.Background(), out.ClientID, out.ClientSecret)
	if err != nil || !valid {
		t.Fatalf("the returned secret does not validate: valid=%v err=%v", valid, err)
	}

	got, err := handleIAMAppGet(ctx, svc, &embeddedAppRequest{ClientID: out.ClientID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if secret := got.(*iamsdk.Application).ClientSecret; secret != "" {
		t.Errorf("get reply carries a client secret %q, want none", secret)
	}
}

func TestHandleIAMAppRegister_ReturnsTheGrantTypes(t *testing.T) {
	t.Parallel()
	ctx, svc := newAppRegisterFixture(t)
	want := []iamsdk.GrantType{iamsdk.GrantTypeClientCredentials, iamsdk.GrantTypeAuthorizationCode}

	req := &embeddedRegisterAppRequest{App: &iamsdk.RegisterAppRequest{
		Name:         "Grants",
		RedirectURIs: []string{"https://grants.example/callback"},
		GrantTypes:   want,
	}}
	resp, err := handleIAMAppRegister(ctx, svc, req)
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	out, ok := resp.(*iamsdk.Application)
	if !ok {
		t.Fatalf("register returned %T, want *iam.Application", resp)
	}
	if !slices.Equal(out.GrantTypes, want) {
		t.Errorf("reply grant types = %v, want %v", out.GrantTypes, want)
	}

	stored, err := svc.apps.GetAppInTenant(context.Background(), "tenant-a", out.ClientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if wantStored := []string{"client_credentials", "authorization_code"}; !slices.Equal(stored.GrantTypes, wantStored) {
		t.Errorf("stored grant types = %v, want %v", stored.GrantTypes, wantStored)
	}

	got, err := handleIAMAppGet(ctx, svc, &embeddedAppRequest{ClientID: out.ClientID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	app, ok := got.(*iamsdk.Application)
	if !ok {
		t.Fatalf("get returned %T, want *iam.Application", got)
	}
	if !slices.Equal(app.GrantTypes, want) {
		t.Errorf("get grant types = %v, want %v", app.GrantTypes, want)
	}
}
