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
	"errors"
	"slices"
	"testing"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// embeddedUpdateAppRequest mirrors the SDK app client's update envelope.
type embeddedUpdateAppRequest struct {
	TenantID string
	ClientID string
	App      *iamsdk.UpdateAppRequest
}

// newAppUpdateRotateFixture returns a tenant-a bound context, the service
// exposing a real AppManager, and one app registered in tenant-a with its
// plaintext secret.
func newAppUpdateRotateFixture(t *testing.T) (context.Context, *mockQuicBaaSForApps, *iam.Application) {
	t.Helper()
	apps := iam.NewAppManager(iam.NewMemoryStorage(), nil)
	app, err := apps.RegisterApp(context.Background(), "tenant-a", iam.RegisterAppOptions{
		Name:         "Original",
		RedirectURIs: []string{"https://original.example/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}
	return tenantctx.WithTenant(context.Background(), "tenant-a"), &mockQuicBaaSForApps{apps: apps}, app
}

func TestIAMAppUpdateAndRotateSecret_Registered(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"quicbaas.IAM.App/Update", "quicbaas.IAM.App/RotateSecret"} {
		if _, ok := iamUnaryHandlers[method]; !ok {
			t.Errorf("%s is not in the embedded IAM dispatch table", method)
		}
	}
}

func TestHandleIAMAppUpdate_AppliesFields(t *testing.T) {
	t.Parallel()
	ctx, svc, app := newAppUpdateRotateFixture(t)

	req := &embeddedUpdateAppRequest{
		TenantID: "tenant-a",
		ClientID: app.ClientID,
		App: &iamsdk.UpdateAppRequest{
			Name:          "Renamed",
			RedirectURIs:  []string{"https://renamed.example/callback"},
			ResponseTypes: []string{"code"},
		},
	}
	resp, err := handleIAMAppUpdate(ctx, svc, req)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	updated, ok := resp.(*iamsdk.Application)
	if !ok {
		t.Fatalf("update returned %T, want *iam.Application", resp)
	}
	if updated.Name != "Renamed" {
		t.Fatalf("update result name = %q, want Renamed", updated.Name)
	}

	got, err := handleIAMAppGet(ctx, svc, &embeddedAppRequest{ClientID: app.ClientID})
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	stored := got.(*iamsdk.Application)
	if stored.Name != "Renamed" {
		t.Errorf("stored name = %q, want Renamed", stored.Name)
	}
	if !slices.Equal(stored.RedirectURIs, []string{"https://renamed.example/callback"}) {
		t.Errorf("stored redirect URIs = %v", stored.RedirectURIs)
	}
	if !slices.Equal(stored.ResponseTypes, []string{"code"}) {
		t.Errorf("stored response types = %v", stored.ResponseTypes)
	}
}

func TestHandleIAMAppUpdate_LeavesUnsetFieldsAlone(t *testing.T) {
	t.Parallel()
	ctx, svc, app := newAppUpdateRotateFixture(t)

	req := &embeddedUpdateAppRequest{ClientID: app.ClientID, App: &iamsdk.UpdateAppRequest{Name: "Renamed"}}
	if _, err := handleIAMAppUpdate(ctx, svc, req); err != nil {
		t.Fatalf("update: %v", err)
	}
	got, err := handleIAMAppGet(ctx, svc, &embeddedAppRequest{ClientID: app.ClientID})
	if err != nil {
		t.Fatalf("get after update: %v", err)
	}
	if uris := got.(*iamsdk.Application).RedirectURIs; !slices.Equal(uris, app.RedirectURIs) {
		t.Errorf("redirect URIs changed to %v, want %v", uris, app.RedirectURIs)
	}
}

func TestHandleIAMAppRotateSecret_ReplacesTheSecret(t *testing.T) {
	t.Parallel()
	ctx, svc, app := newAppUpdateRotateFixture(t)

	resp, err := handleIAMAppRotateSecret(ctx, svc, &embeddedAppRequest{TenantID: "tenant-a", ClientID: app.ClientID})
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	rotated, ok := resp.(*iamsdk.RotateSecretResponse)
	if !ok {
		t.Fatalf("rotate returned %T, want *iam.RotateSecretResponse", resp)
	}
	if rotated.ClientID != app.ClientID {
		t.Errorf("rotate client id = %q, want %q", rotated.ClientID, app.ClientID)
	}
	if rotated.ClientSecret == "" || rotated.ClientSecret == app.ClientSecret {
		t.Fatal("rotate did not return a new secret")
	}
	if _, err := svc.apps.ValidateClient(context.Background(), app.ClientID, app.ClientSecret); err == nil {
		t.Error("the old secret still validates after rotation")
	}
	if _, err := svc.apps.ValidateClient(context.Background(), app.ClientID, rotated.ClientSecret); err != nil {
		t.Errorf("the new secret does not validate: %v", err)
	}
}

func TestHandleIAMAppUpdateAndRotate_RejectBadRequests(t *testing.T) {
	t.Parallel()
	ctx, svc, app := newAppUpdateRotateFixture(t)

	update := map[string]interface{}{
		"nil":            nil,
		"wrong type":     "not a request",
		"missing App":    &embeddedAppRequest{ClientID: app.ClientID},
		"nil App":        &embeddedUpdateAppRequest{ClientID: app.ClientID},
		"nil envelope":   (*embeddedUpdateAppRequest)(nil),
		"register shape": &embeddedRegisterAppRequest{App: &iamsdk.RegisterAppRequest{Name: "x"}},
	}
	for name, req := range update {
		var invalid *InvalidRequestTypeError
		if _, err := handleIAMAppUpdate(ctx, svc, req); !errors.As(err, &invalid) {
			t.Errorf("update with %s: want *InvalidRequestTypeError, got %v", name, err)
		}
	}
	rotate := map[string]interface{}{
		"nil":        nil,
		"wrong type": 42,
		"no client":  &embeddedRegisterAppRequest{},
	}
	for name, req := range rotate {
		var invalid *InvalidRequestTypeError
		if _, err := handleIAMAppRotateSecret(ctx, svc, req); !errors.As(err, &invalid) {
			t.Errorf("rotate with %s: want *InvalidRequestTypeError, got %v", name, err)
		}
	}

	got, err := handleIAMAppGet(ctx, svc, &embeddedAppRequest{ClientID: app.ClientID})
	if err != nil {
		t.Fatalf("get after rejected calls: %v", err)
	}
	if name := got.(*iamsdk.Application).Name; name != "Original" {
		t.Errorf("a rejected update changed the name to %q", name)
	}
	if _, err := svc.apps.ValidateClient(context.Background(), app.ClientID, app.ClientSecret); err != nil {
		t.Errorf("a rejected rotate changed the secret: %v", err)
	}
}

func TestHandleIAMAppUpdateAndRotate_UnknownClientIsNotFound(t *testing.T) {
	t.Parallel()
	ctx, svc, _ := newAppUpdateRotateFixture(t)

	update := &embeddedUpdateAppRequest{ClientID: "no-such-client", App: &iamsdk.UpdateAppRequest{Name: "x"}}
	if _, err := handleIAMAppUpdate(ctx, svc, update); !pkgerrors.IsNotFound(err) {
		t.Errorf("update of an unknown client: want not found, got %v", err)
	}
	if _, err := handleIAMAppRotateSecret(ctx, svc, &embeddedAppRequest{ClientID: "no-such-client"}); !pkgerrors.IsNotFound(err) {
		t.Errorf("rotate of an unknown client: want not found, got %v", err)
	}
}

// TestHandleIAMAppUpdateAndRotate_StayInCallerTenant applies the tenant
// targeting the other app verbs prove: naming another tenant is refused,
// another tenant's client id is not found, and its app is untouched.
func TestHandleIAMAppUpdateAndRotate_StayInCallerTenant(t *testing.T) {
	t.Parallel()
	_, svc, victim := newAppUpdateRotateFixture(t)
	tenantB := tenantctx.WithTenant(context.Background(), "tenant-b")

	named := &embeddedUpdateAppRequest{TenantID: "tenant-a", ClientID: victim.ClientID, App: &iamsdk.UpdateAppRequest{Name: "Hijacked"}}
	if _, err := handleIAMAppUpdate(tenantB, svc, named); !isCrossTenant(err) {
		t.Errorf("update naming another tenant: want cross-tenant refusal, got %v", err)
	}
	if _, err := handleIAMAppRotateSecret(tenantB, svc, &embeddedAppRequest{TenantID: "tenant-a", ClientID: victim.ClientID}); !isCrossTenant(err) {
		t.Errorf("rotate naming another tenant: want cross-tenant refusal, got %v", err)
	}
	unnamed := &embeddedUpdateAppRequest{ClientID: victim.ClientID, App: &iamsdk.UpdateAppRequest{Name: "Hijacked"}}
	if _, err := handleIAMAppUpdate(tenantB, svc, unnamed); err == nil {
		t.Error("update of another tenant's client id under the caller's tenant succeeded")
	}
	if _, err := handleIAMAppRotateSecret(tenantB, svc, &embeddedAppRequest{ClientID: victim.ClientID}); err == nil {
		t.Error("rotate of another tenant's client id under the caller's tenant succeeded")
	}
	if _, err := handleIAMAppRotateSecret(context.Background(), svc, &embeddedAppRequest{ClientID: victim.ClientID}); err == nil {
		t.Error("rotate with no tenant bound succeeded")
	}

	if _, err := svc.apps.ValidateClient(context.Background(), victim.ClientID, victim.ClientSecret); err != nil {
		t.Errorf("the victim's secret changed: %v", err)
	}
	stored, err := svc.apps.GetAppInTenant(context.Background(), "tenant-a", victim.ClientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if stored.Name != "Original" {
		t.Errorf("the victim's name changed to %q", stored.Name)
	}
}
