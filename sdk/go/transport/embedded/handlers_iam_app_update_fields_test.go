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

package embedded

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/rbactest"
	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// appUpdateFieldsFixture registers an app and keeps the storage, so a test
// can authenticate against it after an update.
type appUpdateFieldsFixture struct {
	ctx     context.Context
	svc     *mockQuicBaaSForApps
	storage iam.AppStorage
	app     *iam.Application
}

func newAppUpdateFieldsFixture(t *testing.T) *appUpdateFieldsFixture {
	t.Helper()
	storage := iam.NewMemoryStorage()
	apps := iam.NewAppManager(storage, nil,
		iam.WithGrantorPermissions(rbactest.New(map[string][]string{"kv-reader": {"kv:read"}})))
	app, err := apps.RegisterApp(context.Background(), "tenant-a", iam.RegisterAppOptions{
		Name:         "Original",
		RedirectURIs: []string{"https://original.example/callback"},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}
	// The caller holds kv:read, so the grantor gate lets it grant kv:read.
	ctx := rbactest.CallerContext(tenantctx.WithTenant(context.Background(), "tenant-a"), "alice", "tenant-a", "kv-reader")
	return &appUpdateFieldsFixture{
		ctx:     ctx,
		svc:     &mockQuicBaaSForApps{apps: apps},
		storage: storage,
		app:     app,
	}
}

// update sends one embedded update and fails the test on error.
func (f *appUpdateFieldsFixture) update(t *testing.T, app *iamsdk.UpdateAppRequest) {
	t.Helper()
	req := &embeddedUpdateAppRequest{ClientID: f.app.ClientID, App: app}
	if _, err := handleIAMAppUpdate(f.ctx, f.svc, req); err != nil {
		t.Fatalf("update: %v", err)
	}
}

// get reads the app back through the embedded transport.
func (f *appUpdateFieldsFixture) get(t *testing.T) *iamsdk.Application {
	t.Helper()
	got, err := handleIAMAppGet(f.ctx, f.svc, &embeddedAppRequest{ClientID: f.app.ClientID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return got.(*iamsdk.Application)
}

// newSelfSignedCert mints a throwaway self-signed client certificate.
func newSelfSignedCert(t *testing.T, cn string) *x509.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("ParseCertificate: %v", err)
	}
	return cert
}

// TestHandleIAMAppUpdate_StoredFieldRoundTrips sends each stored field on its
// own through the embedded transport and reads it back through Get.
func TestHandleIAMAppUpdate_StoredFieldRoundTrips(t *testing.T) {
	t.Parallel()
	post := iamsdk.TokenEndpointAuthClientSecretPost
	thumbprint := iam.CertThumbprintS256(newSelfSignedCert(t, "pin"))

	tests := []struct {
		name  string
		app   iamsdk.UpdateAppRequest
		check func(*iamsdk.Application) bool
	}{
		{"name", iamsdk.UpdateAppRequest{Name: "Renamed"}, func(a *iamsdk.Application) bool { return a.Name == "Renamed" }},
		{"redirect_uris", iamsdk.UpdateAppRequest{RedirectURIs: []string{"https://new.example/cb"}}, func(a *iamsdk.Application) bool {
			return slices.Equal(a.RedirectURIs, []string{"https://new.example/cb"})
		}},
		{"resource_prefix", iamsdk.UpdateAppRequest{ResourcePrefix: "orders/"}, func(a *iamsdk.Application) bool { return a.ResourcePrefix == "orders/" }},
		{"grant_types", iamsdk.UpdateAppRequest{GrantTypes: []iamsdk.GrantType{iamsdk.GrantTypeClientCredentials}}, func(a *iamsdk.Application) bool {
			return slices.Equal(a.GrantTypes, []iamsdk.GrantType{iamsdk.GrantTypeClientCredentials})
		}},
		{"response_types", iamsdk.UpdateAppRequest{ResponseTypes: []string{"code", "id_token"}}, func(a *iamsdk.Application) bool {
			return slices.Equal(a.ResponseTypes, []string{"code", "id_token"})
		}},
		{"scopes", iamsdk.UpdateAppRequest{Scopes: []string{"kv:read"}}, func(a *iamsdk.Application) bool { return slices.Equal(a.Scopes, []string{"kv:read"}) }},
		{"token_endpoint_auth_method", iamsdk.UpdateAppRequest{TokenEndpointAuthMethod: &post}, func(a *iamsdk.Application) bool { return a.TokenEndpointAuthMethod == post }},
		{"mtls_cert_thumbprint", iamsdk.UpdateAppRequest{MTLSCertThumbprint: thumbprint}, func(a *iamsdk.Application) bool { return a.MTLSCertThumbprint == thumbprint }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newAppUpdateFieldsFixture(t)
			app := tt.app
			f.update(t, &app)
			if got := f.get(t); !tt.check(got) {
				t.Fatalf("field %s did not round-trip: %+v", tt.name, got)
			}
		})
	}
}

func TestHandleIAMAppUpdate_UngrantableScopeIsForbidden(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)

	req := &embeddedUpdateAppRequest{ClientID: f.app.ClientID, App: &iamsdk.UpdateAppRequest{
		Name:   "Renamed",
		Scopes: []string{"kv:read", "kv:write"},
	}}
	_, err := handleIAMAppUpdate(f.ctx, f.svc, req)
	var notGrantable *iam.ScopeNotGrantableError
	if !errors.As(err, &notGrantable) || notGrantable.Scope != "kv:write" {
		t.Fatalf("update err = %v, want *iam.ScopeNotGrantableError for kv:write", err)
	}
	if !errors.Is(err, iam.ErrScopeNotGrantable) {
		t.Fatalf("update err = %v, want the coded forbidden sentinel", err)
	}

	got := f.get(t)
	if got.Name != "Original" || len(got.Scopes) != 0 {
		t.Fatalf("refused update changed the app: %+v", got)
	}
}

func TestHandleIAMAppUpdate_GrantableScopesLand(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)

	f.update(t, &iamsdk.UpdateAppRequest{Name: "Renamed", Scopes: []string{"kv:read", "openid"}})

	got := f.get(t)
	if got.Name != "Renamed" || !slices.Equal(got.Scopes, []string{"kv:read", "openid"}) {
		t.Fatalf("granted update did not land: %+v", got)
	}
}

// TestHandleIAMAppUpdate_UnstoredFieldIsRefused proves a field the app cannot
// store is refused, as on the other transports, and the app is left unchanged.
func TestHandleIAMAppUpdate_UnstoredFieldIsRefused(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)

	req := &embeddedUpdateAppRequest{ClientID: f.app.ClientID, App: &iamsdk.UpdateAppRequest{
		Name:    "Renamed",
		LogoURI: "https://app.example/logo.png",
	}}
	_, err := handleIAMAppUpdate(f.ctx, f.svc, req)
	var refused *iam.AppFieldNotStoredError
	if !errors.As(err, &refused) || !slices.Equal(refused.Fields, []string{"logo_uri"}) {
		t.Fatalf("err = %v, want AppFieldNotStoredError{logo_uri}", err)
	}
	if got := f.get(t); got.Name != "Original" {
		t.Fatalf("refused update changed the name to %q", got.Name)
	}
}

// TestHandleIAMAppUpdate_ThumbprintRotation re-binds a self-signed client to
// a new certificate through the embedded transport: the old certificate stops
// authenticating and the new one starts.
func TestHandleIAMAppUpdate_ThumbprintRotation(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)
	oldCert := newSelfSignedCert(t, "old")
	newCert := newSelfSignedCert(t, "new")
	auth := iam.NewSelfSignedClientAuth(f.storage)

	selfSigned := iamsdk.TokenEndpointAuthSelfSignedTLSClientAuth
	f.update(t, &iamsdk.UpdateAppRequest{
		TokenEndpointAuthMethod: &selfSigned,
		MTLSCertThumbprint:      iam.CertThumbprintS256(oldCert),
	})
	if _, err := auth.AuthenticateClient(context.Background(), oldCert, f.app.ClientID); err != nil {
		t.Fatalf("old cert before rotation: %v", err)
	}

	f.update(t, &iamsdk.UpdateAppRequest{MTLSCertThumbprint: iam.CertThumbprintS256(newCert)})

	if _, err := auth.AuthenticateClient(context.Background(), oldCert, f.app.ClientID); !errors.Is(err, iam.ErrMTLSThumbprintMismatch) {
		t.Fatalf("old cert after rotation: err = %v, want ErrMTLSThumbprintMismatch", err)
	}
	if _, err := auth.AuthenticateClient(context.Background(), newCert, f.app.ClientID); err != nil {
		t.Fatalf("new cert after rotation: %v", err)
	}
}

// TestHandleIAMAppUpdate_SelectsClientSecretBasic switches an app from
// client_secret_post to client_secret_basic, the zero value of the enum, and
// proves an update without the field leaves the method alone.
func TestHandleIAMAppUpdate_SelectsClientSecretBasic(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)

	post := iamsdk.TokenEndpointAuthClientSecretPost
	f.update(t, &iamsdk.UpdateAppRequest{TokenEndpointAuthMethod: &post})
	f.update(t, &iamsdk.UpdateAppRequest{Name: "Renamed"})
	if got := f.get(t).TokenEndpointAuthMethod; got != post {
		t.Fatalf("update without the field changed the method to %v", got)
	}

	basic := iamsdk.TokenEndpointAuthClientSecretBasic
	f.update(t, &iamsdk.UpdateAppRequest{TokenEndpointAuthMethod: &basic})
	stored, err := f.svc.apps.GetAppInTenant(context.Background(), "tenant-a", f.app.ClientID)
	if err != nil {
		t.Fatalf("GetAppInTenant: %v", err)
	}
	if stored.TokenEndpointAuthMethod != iam.AuthMethodClientSecretBasic {
		t.Fatalf("stored method = %q, want %q", stored.TokenEndpointAuthMethod, iam.AuthMethodClientSecretBasic)
	}
	if got := f.get(t).TokenEndpointAuthMethod; got != basic {
		t.Fatalf("Get method = %v, want client_secret_basic", got)
	}
}

// TestUpdateAppOptionsFromSDK_OmittedFieldsStayNil proves an empty SDK request
// sets no option, so it cannot clear stored data.
func TestUpdateAppOptionsFromSDK_OmittedFieldsStayNil(t *testing.T) {
	t.Parallel()
	opts, err := updateAppOptionsFromSDK(&iamsdk.UpdateAppRequest{})
	if err != nil {
		t.Fatalf("updateAppOptionsFromSDK: %v", err)
	}
	if opts != (iam.UpdateAppOptions{}) {
		t.Fatalf("empty request set options: %+v", opts)
	}
}

// TestHandleIAMAppUpdate_UnknownAuthMethodIsRefused proves an auth method
// outside the enum reaches the app manager and is refused, never dropped.
func TestHandleIAMAppUpdate_UnknownAuthMethodIsRefused(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)
	unknown := iamsdk.TokenEndpointAuthMethod(99)

	req := &embeddedUpdateAppRequest{ClientID: f.app.ClientID, App: &iamsdk.UpdateAppRequest{TokenEndpointAuthMethod: &unknown}}
	if _, err := handleIAMAppUpdate(f.ctx, f.svc, req); !errors.Is(err, iam.ErrInvalidTokenEndpointAuthMethod) {
		t.Fatalf("err = %v, want ErrInvalidTokenEndpointAuthMethod", err)
	}
}

// TestHandleIAMAppRegister_TokenEndpointAuthMethod registers through the
// embedded transport and reads the method back through Get: client_secret_post
// is stored, the zero value is client_secret_basic, and a method the manager
// does not support is refused with its typed error (#1473).
func TestHandleIAMAppRegister_TokenEndpointAuthMethod(t *testing.T) {
	t.Parallel()
	f := newAppUpdateFieldsFixture(t)
	register := func(method iamsdk.TokenEndpointAuthMethod) (*iamsdk.Application, error) {
		got, err := handleIAMAppRegister(f.ctx, f.svc, &embeddedRegisterAppRequest{
			TenantID: "tenant-a",
			App: &iamsdk.RegisterAppRequest{
				Name:                    "Registered",
				RedirectURIs:            []string{"https://registered.example/cb"},
				TokenEndpointAuthMethod: method,
			},
		})
		if err != nil {
			return nil, err
		}
		return got.(*iamsdk.Application), nil
	}

	for _, method := range []iamsdk.TokenEndpointAuthMethod{
		iamsdk.TokenEndpointAuthClientSecretPost,
		iamsdk.TokenEndpointAuthClientSecretBasic,
	} {
		created, err := register(method)
		if err != nil {
			t.Fatalf("register %v: %v", method, err)
		}
		fetched, err := handleIAMAppGet(f.ctx, f.svc, &embeddedAppRequest{ClientID: created.ClientID})
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if got := fetched.(*iamsdk.Application).TokenEndpointAuthMethod; got != method {
			t.Fatalf("registered %v, Get read back %v", method, got)
		}
	}

	if _, err := register(iamsdk.TokenEndpointAuthPrivateKeyJWT); !errors.Is(err, iam.ErrInvalidTokenEndpointAuthMethod) {
		t.Fatalf("register private_key_jwt: err = %v, want ErrInvalidTokenEndpointAuthMethod", err)
	}
}

func TestAppUpdateDecodeError_UnwrapsCause(t *testing.T) {
	t.Parallel()
	cause := &json.UnsupportedValueError{Str: "bad"}
	err := error(&AppUpdateDecodeError{Cause: cause})
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is(%v, cause) = false", err)
	}
	if err.Error() == "" {
		t.Fatal("empty error message")
	}
}
