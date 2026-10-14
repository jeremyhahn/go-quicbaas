//go:build rest

// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// stubRealmResolver resolves any email domain to a fixed tenant for testing.
type stubRealmResolver struct {
	byEmail map[string]string
}

func (s *stubRealmResolver) Resolve(ctx context.Context, hints RealmHints) (string, error) {
	if t, ok := s.byEmail[hints.Email]; ok {
		return t, nil
	}
	return "", ErrLoginTenantUnresolved
}

func beginLoginRequest(t *testing.T, tenant, provider string) (*http.Request, *httptest.ResponseRecorder) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/auth/providers/"+tenant+"/"+provider+"/login?redirect_uri=https://app.example.com/cb&login_hint=you@acme.com", nil)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("tenant_id", tenant)
	rctx.URLParams.Add("provider_name", provider)
	req = req.WithContext(callerRouteContext(req.Context(), rctx))
	return req, httptest.NewRecorder()
}

func TestBeginFederatedLogin_HRDResolvesTenant(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["p1"] = &iam.ExternalProvider{
		ID: "p1", TenantID: "tenant-acme", Name: "okta", Type: iam.ProviderTypeOIDC, Enabled: true,
	}
	handler, server := createFederationTestHandler(fedMgr)
	server.config = &Config{HomeRealmResolver: &stubRealmResolver{byEmail: map[string]string{"you@acme.com": "tenant-acme"}}}

	// tenant "_" triggers HRD; provider named explicitly.
	req, rec := beginLoginRequest(t, "_", "okta")
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
}

func TestBeginFederatedLogin_HRDUnresolvedFailsClosed(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, server := createFederationTestHandler(fedMgr)
	server.config = &Config{HomeRealmResolver: &stubRealmResolver{byEmail: map[string]string{}}}

	req, rec := beginLoginRequest(t, "_", "okta")
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBeginFederatedLogin_AutoSelectsSingleProvider(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["p1"] = &iam.ExternalProvider{
		ID: "p1", TenantID: "tenant-acme", Name: "okta", Type: iam.ProviderTypeOIDC, Enabled: true,
	}
	handler, server := createFederationTestHandler(fedMgr)
	server.config = &Config{}

	// provider "_" triggers single-provider auto-selection.
	req, rec := beginLoginRequest(t, "tenant-acme", "_")
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusFound, rec.Code)
}

func TestBeginFederatedLogin_AmbiguousProviderRejected(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["p1"] = &iam.ExternalProvider{
		ID: "p1", TenantID: "tenant-acme", Name: "okta", Type: iam.ProviderTypeOIDC, Enabled: true,
	}
	fedMgr.providers["p2"] = &iam.ExternalProvider{
		ID: "p2", TenantID: "tenant-acme", Name: "cognito", Type: iam.ProviderTypeOIDC, Enabled: true,
	}
	handler, server := createFederationTestHandler(fedMgr)
	server.config = &Config{}

	req, rec := beginLoginRequest(t, "tenant-acme", "_")
	handler.BeginFederatedLogin(rec, req)

	// Two enabled providers: the caller must name one.
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestBeginFederatedLogin_NoProviderRejected(t *testing.T) {
	fedMgr := newMockFederationManager()
	handler, server := createFederationTestHandler(fedMgr)
	server.config = &Config{}

	req, rec := beginLoginRequest(t, "tenant-acme", "_")
	handler.BeginFederatedLogin(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestSelectTenantProvider(t *testing.T) {
	fedMgr := newMockFederationManager()
	fedMgr.providers["p1"] = &iam.ExternalProvider{ID: "p1", TenantID: "t", Name: "okta", Enabled: true}
	fedMgr.providers["p2"] = &iam.ExternalProvider{ID: "p2", TenantID: "t", Name: "disabled", Enabled: false}
	handler, server := createFederationTestHandler(fedMgr)
	server.config = &Config{}

	name, err := handler.selectTenantProvider(context.Background(), fedMgr, "t")
	assert.NoError(t, err)
	assert.Equal(t, "okta", name)
}

func TestIsAutoSelector(t *testing.T) {
	for _, v := range []string{"", "_", "auto", " _ "} {
		assert.True(t, isAutoSelector(v), v)
	}
	for _, v := range []string{"tenant-1", "okta", "x"} {
		assert.False(t, isAutoSelector(v), v)
	}
}
