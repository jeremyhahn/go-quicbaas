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
	"slices"
	"testing"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// newUpdateTestApp registers a client_credentials app for update tests.
func newUpdateTestApp(t *testing.T) (*AppManager, AppStorage, *Application) {
	t.Helper()
	storage := NewMemoryStorage()
	mgr := NewAppManager(storage, nil)
	app, err := mgr.RegisterApp(context.Background(), "tenant-1", RegisterAppOptions{
		Name:       "svc",
		GrantTypes: []string{GrantTypeClientCredentials},
	})
	if err != nil {
		t.Fatalf("RegisterApp: %v", err)
	}
	return mgr, storage, app
}

func TestUpdateAppInTenant_NilOptionsLeaveAppUnchanged(t *testing.T) {
	t.Parallel()
	mgr, _, before := newUpdateTestApp(t)

	after, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", before.ClientID, UpdateAppOptions{})
	if err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}

	after.Updated = before.Updated
	if after.Name != before.Name || after.ResourcePrefix != before.ResourcePrefix ||
		after.DefaultAudience != before.DefaultAudience ||
		after.TokenEndpointAuthMethod != before.TokenEndpointAuthMethod ||
		after.MTLSCertThumbprint != before.MTLSCertThumbprint ||
		!slices.Equal(after.RedirectURIs, before.RedirectURIs) ||
		!slices.Equal(after.GrantTypes, before.GrantTypes) ||
		!slices.Equal(after.ResponseTypes, before.ResponseTypes) ||
		!slices.Equal(after.Scopes, before.Scopes) {
		t.Fatalf("app changed by an empty update:\nbefore %+v\nafter  %+v", before, after)
	}
}

func TestUpdateAppInTenant_Scopes(t *testing.T) {
	t.Parallel()
	mgr, _, app := newGrantorTestApp(t, &fakePermissionResolver{client: newGrantorRBAC()})

	scopes := []string{"orchestrator:agent_heartbeat"}
	ctx := grantorContext("orchestrator-admin")
	updated, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{Scopes: &scopes})
	if err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}
	if !slices.Equal(updated.Scopes, scopes) {
		t.Fatalf("Scopes = %v, want %v", updated.Scopes, scopes)
	}
}

func TestUpdateAppInTenant_RedirectURIsValidated(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)
	ctx := context.Background()

	good := []string{"https://app.example/cb"}
	updated, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{RedirectURIs: &good})
	if err != nil {
		t.Fatalf("UpdateAppInTenant(valid): %v", err)
	}
	if !slices.Equal(updated.RedirectURIs, good) {
		t.Fatalf("RedirectURIs = %v, want %v", updated.RedirectURIs, good)
	}

	bad := []string{"http://app.example/cb"}
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{RedirectURIs: &bad}); err == nil {
		t.Fatal("expected a plain-HTTP non-localhost redirect URI to be refused")
	}
}

func TestUpdateAppInTenant_GrantTypesValidated(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)
	ctx := context.Background()

	good := []string{GrantTypeClientCredentials, "refresh_token"}
	updated, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{GrantTypes: &good})
	if err != nil {
		t.Fatalf("UpdateAppInTenant(valid): %v", err)
	}
	if !slices.Equal(updated.GrantTypes, good) {
		t.Fatalf("GrantTypes = %v, want %v", updated.GrantTypes, good)
	}

	bad := []string{"password"}
	_, err = mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{GrantTypes: &bad})
	assertInvalidInput(t, err)
}

// assertInvalidInput fails unless err is a QBError coded as invalid input,
// the code the transports map to InvalidArgument and HTTP 400.
func assertInvalidInput(t *testing.T, err error) {
	t.Helper()
	var coded *dberrors.QBError
	if !errors.As(err, &coded) || coded.Code != dberrors.ErrInvalidInput {
		t.Fatalf("err = %v, want a QBError coded %s", err, dberrors.ErrInvalidInput)
	}
}

func TestRegisterApp_UnsupportedGrantIsInvalidInput(t *testing.T) {
	t.Parallel()
	mgr := NewAppManager(NewMemoryStorage(), nil)

	_, err := mgr.RegisterApp(context.Background(), "tenant-1", RegisterAppOptions{
		Name:       "svc",
		GrantTypes: []string{"password"},
	})
	assertInvalidInput(t, err)
}

func TestUpdateAppInTenant_SwitchesToClientSecretBasic(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)
	ctx := context.Background()

	post := AuthMethodClientSecretPost
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{TokenEndpointAuthMethod: &post}); err != nil {
		t.Fatalf("UpdateAppInTenant(post): %v", err)
	}
	basic := AuthMethodClientSecretBasic
	updated, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{TokenEndpointAuthMethod: &basic})
	if err != nil {
		t.Fatalf("UpdateAppInTenant(basic): %v", err)
	}
	if updated.TokenEndpointAuthMethod != basic {
		t.Fatalf("TokenEndpointAuthMethod = %q, want %q", updated.TokenEndpointAuthMethod, basic)
	}
}

func TestUpdateAppInTenant_TokenEndpointAuthMethod(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)
	ctx := context.Background()

	post := AuthMethodClientSecretPost
	updated, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{TokenEndpointAuthMethod: &post})
	if err != nil {
		t.Fatalf("UpdateAppInTenant(valid): %v", err)
	}
	if updated.TokenEndpointAuthMethod != post {
		t.Fatalf("TokenEndpointAuthMethod = %q, want %q", updated.TokenEndpointAuthMethod, post)
	}

	bad := "private_key_jwt"
	_, err = mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{TokenEndpointAuthMethod: &bad})
	if !errors.Is(err, ErrInvalidTokenEndpointAuthMethod) {
		t.Fatalf("err = %v, want ErrInvalidTokenEndpointAuthMethod", err)
	}
}

func TestUpdateAppInTenant_ThumbprintApplied(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)

	thumbprint := CertThumbprintS256(newSelfSignedLeaf(t, "client"))
	updated, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", app.ClientID, UpdateAppOptions{MTLSCertThumbprint: &thumbprint})
	if err != nil {
		t.Fatalf("UpdateAppInTenant: %v", err)
	}
	if updated.MTLSCertThumbprint != thumbprint {
		t.Fatalf("MTLSCertThumbprint = %q, want %q", updated.MTLSCertThumbprint, thumbprint)
	}
}

func TestUpdateAppInTenant_ThumbprintMalformedRefused(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)
	valid := CertThumbprintS256(newSelfSignedLeaf(t, "client"))

	cases := map[string]string{
		"padded":        valid + "=",
		"not base64url": "+/+/" + valid[4:],
		"too short":     valid[:20],
		"hex digest":    "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}
	for name, thumbprint := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", app.ClientID, UpdateAppOptions{MTLSCertThumbprint: &thumbprint})
			if !errors.Is(err, ErrMTLSInvalidThumbprint) {
				t.Fatalf("err = %v, want ErrMTLSInvalidThumbprint", err)
			}
		})
	}
}

func TestUpdateAppInTenant_ThumbprintRebind(t *testing.T) {
	t.Parallel()
	mgr, storage, app := newUpdateTestApp(t)
	ctx := context.Background()
	oldCert := newSelfSignedLeaf(t, "old")
	newCert := newSelfSignedLeaf(t, "new")

	method := AuthMethodSelfSignedTLSClientAuth
	oldPin := CertThumbprintS256(oldCert)
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{
		TokenEndpointAuthMethod: &method,
		MTLSCertThumbprint:      &oldPin,
	}); err != nil {
		t.Fatalf("pin old cert: %v", err)
	}
	auth := NewSelfSignedClientAuth(storage)
	if _, err := auth.AuthenticateClient(ctx, oldCert, app.ClientID); err != nil {
		t.Fatalf("old cert before re-bind: %v", err)
	}

	newPin := CertThumbprintS256(newCert)
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{MTLSCertThumbprint: &newPin}); err != nil {
		t.Fatalf("re-bind: %v", err)
	}

	if _, err := auth.AuthenticateClient(ctx, oldCert, app.ClientID); !errors.Is(err, ErrMTLSThumbprintMismatch) {
		t.Fatalf("old cert after re-bind: err = %v, want ErrMTLSThumbprintMismatch", err)
	}
	if _, err := auth.AuthenticateClient(ctx, newCert, app.ClientID); err != nil {
		t.Fatalf("new cert after re-bind: %v", err)
	}
}

func TestUpdateAppInTenant_ThumbprintClear(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)
	ctx := context.Background()

	pin := CertThumbprintS256(newSelfSignedLeaf(t, "client"))
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{MTLSCertThumbprint: &pin}); err != nil {
		t.Fatalf("pin: %v", err)
	}
	empty := ""
	updated, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{MTLSCertThumbprint: &empty})
	if err != nil {
		t.Fatalf("clear pin on a secret-auth client: %v", err)
	}
	if updated.MTLSCertThumbprint != "" {
		t.Fatalf("MTLSCertThumbprint = %q, want cleared", updated.MTLSCertThumbprint)
	}
}

func TestUpdateAppInTenant_ThumbprintClearRefusedForSelfSigned(t *testing.T) {
	t.Parallel()
	mgr, storage, app := newUpdateTestApp(t)
	ctx := context.Background()

	method := AuthMethodSelfSignedTLSClientAuth
	pin := CertThumbprintS256(newSelfSignedLeaf(t, "client"))
	if _, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{
		TokenEndpointAuthMethod: &method,
		MTLSCertThumbprint:      &pin,
	}); err != nil {
		t.Fatalf("pin: %v", err)
	}

	empty := ""
	_, err := mgr.UpdateAppInTenant(ctx, "tenant-1", app.ClientID, UpdateAppOptions{MTLSCertThumbprint: &empty})
	if !errors.Is(err, ErrMTLSThumbprintRequired) {
		t.Fatalf("err = %v, want ErrMTLSThumbprintRequired", err)
	}
	stored, err := storage.GetApp(ctx, app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if stored.MTLSCertThumbprint != pin {
		t.Fatalf("stored pin = %q, want it kept as %q", stored.MTLSCertThumbprint, pin)
	}
}

func TestUpdateAppInTenant_SelfSignedWithoutPinRefused(t *testing.T) {
	t.Parallel()
	mgr, _, app := newUpdateTestApp(t)

	method := AuthMethodSelfSignedTLSClientAuth
	_, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", app.ClientID, UpdateAppOptions{TokenEndpointAuthMethod: &method})
	if !errors.Is(err, ErrMTLSThumbprintRequired) {
		t.Fatalf("err = %v, want ErrMTLSThumbprintRequired", err)
	}
}

func TestUpdateAppInTenant_UnstoredFieldsRefused(t *testing.T) {
	t.Parallel()
	mgr, storage, app := newUpdateTestApp(t)
	text := "x"
	list := []string{"x"}
	ttl := time.Hour
	flag := true
	meta := map[string]string{"k": "v"}

	cases := map[string]UpdateAppOptions{
		"pkce_required":             {PKCERequired: &flag},
		"description":               {Description: &text},
		"post_logout_redirect_uris": {PostLogoutRedirectURIs: &list},
		"contacts":                  {Contacts: &list},
		"logo_uri":                  {LogoURI: &text},
		"policy_uri":                {PolicyURI: &text},
		"tos_uri":                   {TOSURI: &text},
		"jwks_uri":                  {JWKSURI: &text},
		"jwks":                      {JWKS: &text},
		"access_token_ttl":          {AccessTokenTTL: &ttl},
		"refresh_token_ttl":         {RefreshTokenTTL: &ttl},
		"id_token_ttl":              {IDTokenTTL: &ttl},
		"metadata":                  {Metadata: &meta},
		"enabled":                   {Enabled: &flag},
	}
	for field, opts := range cases {
		t.Run(field, func(t *testing.T) {
			name := "renamed"
			opts.Name = &name
			_, err := mgr.UpdateAppInTenant(context.Background(), "tenant-1", app.ClientID, opts)
			var notStored *AppFieldNotStoredError
			if !errors.As(err, &notStored) || !slices.Equal(notStored.Fields, []string{field}) {
				t.Fatalf("err = %v, want AppFieldNotStoredError{%s}", err, field)
			}
			if !errors.Is(err, ErrAppFieldNotStored) {
				t.Fatalf("err = %v does not match ErrAppFieldNotStored", err)
			}
		})
	}

	stored, err := storage.GetApp(context.Background(), app.ClientID)
	if err != nil {
		t.Fatalf("GetApp: %v", err)
	}
	if stored.Name != app.Name {
		t.Fatalf("refused update still wrote Name = %q", stored.Name)
	}
}
