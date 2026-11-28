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
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// newEmbeddedAppClient returns the SDK app client over a real embedded
// transport whose service exposes a real, empty in-memory AppManager.
func newEmbeddedAppClient(t *testing.T) (iamsdk.AppClient, *iam.AppManager) {
	t.Helper()
	apps := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	tr, err := New(&mockQuicBaaSForApps{apps: apps})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return iamsdk.NewAppClient(tr), apps
}

// TestEmbeddedAppClient_RoundTrip checks that the SDK app client over the
// embedded transport registers, reads, updates, rotates and deletes an app in
// the caller's tenant, and that register keeps the resource prefix and grant
// types and returns the one-time client secret.
func TestEmbeddedAppClient_RoundTrip(t *testing.T) {
	t.Parallel()
	client, apps := newEmbeddedAppClient(t)
	ctx := transport.ContextWithTenant(context.Background(), "tenant-a")
	const prefix = "apps/client-rt/"
	want := []iamsdk.GrantType{iamsdk.GrantTypeAuthorizationCode, iamsdk.GrantTypeClientCredentials}

	app, err := client.Register(ctx, "tenant-a", &iamsdk.RegisterAppRequest{
		Name:           "Client RT",
		RedirectURIs:   []string{"https://client-rt.example/callback"},
		ResourcePrefix: prefix,
		GrantTypes:     want,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if app.ClientID == "" {
		t.Fatal("register returned no client id")
	}
	if app.TenantID != "tenant-a" {
		t.Errorf("register tenant = %q, want tenant-a", app.TenantID)
	}
	if app.ResourcePrefix != prefix {
		t.Errorf("register resource prefix = %q, want %q", app.ResourcePrefix, prefix)
	}
	if !slices.Equal(app.GrantTypes, want) {
		t.Errorf("register grant types = %v, want %v", app.GrantTypes, want)
	}
	if app.ClientSecret == "" {
		t.Fatal("register returned no client secret")
	}
	valid, err := apps.ValidateClientCredentials(context.Background(), app.ClientID, app.ClientSecret)
	if err != nil || !valid {
		t.Fatalf("the registered secret does not validate: valid=%v err=%v", valid, err)
	}

	got, err := client.Get(ctx, "tenant-a", app.ClientID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ResourcePrefix != prefix {
		t.Errorf("get resource prefix = %q, want %q", got.ResourcePrefix, prefix)
	}
	if !slices.Equal(got.GrantTypes, want) {
		t.Errorf("get grant types = %v, want %v", got.GrantTypes, want)
	}
	if got.ClientSecret != "" {
		t.Errorf("get carries a client secret %q, want none", got.ClientSecret)
	}

	list, err := client.List(ctx, "tenant-a")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.TotalCount != 1 || len(list.Applications) != 1 || list.Applications[0].ClientID != app.ClientID {
		t.Errorf("list = %+v, want the one registered app %q", list, app.ClientID)
	}

	updated, err := client.Update(ctx, "tenant-a", app.ClientID, &iamsdk.UpdateAppRequest{Name: "Client RT renamed"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != "Client RT renamed" {
		t.Errorf("update name = %q, want %q", updated.Name, "Client RT renamed")
	}
	if updated.ResourcePrefix != prefix {
		t.Errorf("update resource prefix = %q, want %q", updated.ResourcePrefix, prefix)
	}
	stored, err := apps.GetAppInTenant(context.Background(), "tenant-a", app.ClientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if stored.Name != "Client RT renamed" {
		t.Errorf("stored name = %q, want %q", stored.Name, "Client RT renamed")
	}

	rot, err := client.RotateSecret(ctx, "tenant-a", app.ClientID)
	if err != nil {
		t.Fatalf("RotateSecret: %v", err)
	}
	if rot.ClientID != app.ClientID {
		t.Errorf("rotate client id = %q, want %q", rot.ClientID, app.ClientID)
	}
	if rot.ClientSecret == "" || rot.ClientSecret == app.ClientSecret {
		t.Fatalf("rotate returned secret %q, want a new non-empty secret", rot.ClientSecret)
	}
	if _, err := apps.ValidateClient(context.Background(), app.ClientID, rot.ClientSecret); err != nil {
		t.Errorf("the rotated secret does not validate: %v", err)
	}

	if err := client.Delete(ctx, "tenant-a", app.ClientID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := client.Get(ctx, "tenant-a", app.ClientID); err == nil {
		t.Error("get after delete succeeded, want an error")
	}
}

// TestEmbeddedAppClient_StaysInCallerTenant checks that every SDK app client
// verb over the embedded transport stays in the caller's tenant: naming
// another tenant is refused, another tenant's client id is not found under the
// caller's tenant, and that tenant's app survives.
func TestEmbeddedAppClient_StaysInCallerTenant(t *testing.T) {
	t.Parallel()
	client, apps := newEmbeddedAppClient(t)
	victim, err := apps.RegisterApp(context.Background(), "tenant-b", iam.RegisterAppOptions{
		Name:         "Victim",
		RedirectURIs: []string{"https://victim.example/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}
	ctxA := transport.ContextWithTenant(context.Background(), "tenant-a")

	cases := []struct {
		name string
		call func() error
	}{
		{"get naming tenant-b", func() error {
			_, err := client.Get(ctxA, "tenant-b", victim.ClientID)
			return err
		}},
		{"update naming tenant-b", func() error {
			_, err := client.Update(ctxA, "tenant-b", victim.ClientID, &iamsdk.UpdateAppRequest{Name: "Hijacked"})
			return err
		}},
		{"rotate naming tenant-b", func() error {
			_, err := client.RotateSecret(ctxA, "tenant-b", victim.ClientID)
			return err
		}},
		{"delete naming tenant-b", func() error {
			return client.Delete(ctxA, "tenant-b", victim.ClientID)
		}},
		{"list naming tenant-b", func() error {
			_, err := client.List(ctxA, "tenant-b")
			return err
		}},
		{"get victim id in tenant-a", func() error {
			_, err := client.Get(ctxA, "tenant-a", victim.ClientID)
			return err
		}},
		{"update victim id in tenant-a", func() error {
			_, err := client.Update(ctxA, "tenant-a", victim.ClientID, &iamsdk.UpdateAppRequest{Name: "Hijacked"})
			return err
		}},
		{"rotate victim id in tenant-a", func() error {
			_, err := client.RotateSecret(ctxA, "tenant-a", victim.ClientID)
			return err
		}},
		{"delete victim id in tenant-a", func() error {
			return client.Delete(ctxA, "tenant-a", victim.ClientID)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); err == nil {
				t.Errorf("%s succeeded, want an error", tc.name)
			}
		})
	}

	stored, err := apps.GetAppInTenant(context.Background(), "tenant-b", victim.ClientID)
	if err != nil {
		t.Fatalf("the victim app is gone: %v", err)
	}
	if stored.Name != "Victim" {
		t.Errorf("victim name = %q, want Victim", stored.Name)
	}
	if _, err := apps.ValidateClient(context.Background(), victim.ClientID, victim.ClientSecret); err != nil {
		t.Errorf("the victim secret no longer validates: %v", err)
	}
}
