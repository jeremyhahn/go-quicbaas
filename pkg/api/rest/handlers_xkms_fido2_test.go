//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	transportTypes "github.com/jeremyhahn/go-xkms/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// fakeFIDO2Service records the tenant scope and returns canned ceremony
// responses (or a configured error) for every verb.
type fakeFIDO2Service struct {
	tenantID string
	err      error
}

func (f *fakeFIDO2Service) BeginRegistration(_ context.Context, tenantID string, _ *transportTypes.BeginRegistrationRequest) (*transportTypes.BeginRegistrationResponse, error) {
	f.tenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return &transportTypes.BeginRegistrationResponse{SessionID: "s1", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Service) BeginAddCredential(_ context.Context, tenantID string) (*transportTypes.BeginRegistrationResponse, error) {
	f.tenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return &transportTypes.BeginRegistrationResponse{SessionID: "add-sess", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Service) FinishRegistration(_ context.Context, tenantID string, _ *transportTypes.FinishRegistrationRequest) (*transportTypes.FinishRegistrationResponse, error) {
	f.tenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return &transportTypes.FinishRegistrationResponse{Success: true, CredentialID: "cred-1"}, nil
}

func (f *fakeFIDO2Service) BeginAuthentication(_ context.Context, tenantID string, _ *transportTypes.BeginAuthenticationRequest) (*transportTypes.BeginAuthenticationResponse, error) {
	f.tenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return &transportTypes.BeginAuthenticationResponse{SessionID: "s2", OptionsJSON: `{"publicKey":{}}`}, nil
}

func (f *fakeFIDO2Service) FinishAuthentication(_ context.Context, tenantID string, _ *transportTypes.FinishAuthenticationRequest) (*transportTypes.FinishAuthenticationResponse, error) {
	f.tenantID = tenantID
	if f.err != nil {
		return nil, f.err
	}
	return &transportTypes.FinishAuthenticationResponse{Success: true, Token: "tok"}, nil
}

// newXKMSFIDO2Server builds a test server whose service returns the supplied
// fake FIDO2 composition service.
func newXKMSFIDO2Server(t *testing.T, fake service.FIDO2Service) *Server {
	t.Helper()
	s, mock := newTestServer(t)
	mock.FIDO2Svc = fake
	return s
}

func TestHandleXKMSFIDO2BeginRegistration_Happy(t *testing.T) {
	fake := &fakeFIDO2Service{}
	s := newXKMSFIDO2Server(t, fake)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/begin?tenant_id=acme",
		strings.NewReader(`{"username":"alice@example.com","display_name":"Alice"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2BeginRegistration(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "acme", fake.tenantID, "tenant_id query must scope the ceremony")

	var resp transportTypes.BeginRegistrationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "s1", resp.SessionID)
	assert.NotEmpty(t, resp.OptionsJSON)
}

func TestHandleXKMSFIDO2BeginRegistration_MissingTenant(t *testing.T) {
	s := newXKMSFIDO2Server(t, &fakeFIDO2Service{})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/fido2/register/begin",
		strings.NewReader(`{"username":"alice@example.com"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2BeginRegistration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "tenant is required")
}

func TestHandleXKMSFIDO2BeginRegistration_InvalidBody(t *testing.T) {
	s := newXKMSFIDO2Server(t, &fakeFIDO2Service{})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/begin?tenant_id=acme", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2BeginRegistration(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleXKMSFIDO2FinishRegistration_Happy(t *testing.T) {
	fake := &fakeFIDO2Service{}
	s := newXKMSFIDO2Server(t, fake)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/finish?tenant_id=acme",
		strings.NewReader(`{"session_id":"s1","attestation_json":"{}"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2FinishRegistration(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp transportTypes.FinishRegistrationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "cred-1", resp.CredentialID)
}

func TestHandleXKMSFIDO2FinishRegistration_CeremonyError(t *testing.T) {
	fake := &fakeFIDO2Service{err: &XKMSOperationError{Op: "finish", Detail: "attestation rejected"}}
	s := newXKMSFIDO2Server(t, fake)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/finish?tenant_id=acme",
		strings.NewReader(`{"session_id":"s1","attestation_json":"{}"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2FinishRegistration(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// TestHandleXKMSFIDO2BeginAddCredential_ScopesToTheRequestTenant pins that
// the add-credential leg takes its tenant from the request context the
// authentication middleware set, and never from a tenant_id parameter.
func TestHandleXKMSFIDO2BeginAddCredential_ScopesToTheRequestTenant(t *testing.T) {
	fake := &fakeFIDO2Service{}
	s := newXKMSFIDO2Server(t, fake)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/fido2/credentials/begin?tenant_id=other", nil)
	req = req.WithContext(tenantctx.WithTenant(req.Context(), "acme"))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2BeginAddCredential(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "acme", fake.tenantID, "the context tenant must scope the ceremony, not the query")
	var resp transportTypes.BeginRegistrationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "add-sess", resp.SessionID)
	assert.NotEmpty(t, resp.OptionsJSON)
}

// TestHandleXKMSFIDO2BeginAddCredential_RefusalStatus pins that the service's
// classified refusals keep their status on this bridge: no caller is 401, a
// caller of another tenant is 403, an unwired service is 503.
func TestHandleXKMSFIDO2BeginAddCredential_RefusalStatus(t *testing.T) {
	cases := map[string]struct {
		err  error
		want int
	}{
		"unauthenticated": {
			err:  &dberrors.QBError{Code: dberrors.ErrUnauthorized, Err: &service.CallerAccountRefusedError{Reason: "no caller"}},
			want: http.StatusUnauthorized,
		},
		"cross-tenant": {
			err:  &dberrors.QBError{Code: dberrors.ErrForbidden, Err: &tenantctx.CrossTenantError{CallerTenant: "acme", TargetTenant: "other"}},
			want: http.StatusForbidden,
		},
		"not configured": {err: service.ErrFIDO2NotConfigured, want: http.StatusServiceUnavailable},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			s := newXKMSFIDO2Server(t, &fakeFIDO2Service{err: tc.err})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/xkms/fido2/credentials/begin", nil)
			w := httptest.NewRecorder()
			s.HandleXKMSFIDO2BeginAddCredential(w, req)
			assert.Equal(t, tc.want, w.Code, w.Body.String())
		})
	}
}

func TestHandleXKMSFIDO2BeginAuthentication_Happy(t *testing.T) {
	fake := &fakeFIDO2Service{}
	s := newXKMSFIDO2Server(t, fake)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/authenticate/begin?tenant_id=acme",
		strings.NewReader(`{"username":"alice@example.com"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2BeginAuthentication(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp transportTypes.BeginAuthenticationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "s2", resp.SessionID)
}

// TestHandleXKMSFIDO2Authentication_UniformFailure pins that a failed
// authentication leg answers a generic 401 that leaks nothing about WHY the
// proof failed (unknown user, no credentials, bad signature must be
// indistinguishable).
func TestHandleXKMSFIDO2Authentication_UniformFailure(t *testing.T) {
	failures := []error{
		&XKMSOperationError{Op: "resolve user", Detail: `user "ghost@example.com" not found`},
		&XKMSOperationError{Op: "begin authentication", Detail: "no webauthn credentials registered"},
		&XKMSOperationError{Op: "finish authentication", Detail: "signature verification failed"},
	}

	var bodies []string
	for _, ferr := range failures {
		s := newXKMSFIDO2Server(t, &fakeFIDO2Service{err: ferr})
		req := httptest.NewRequest(http.MethodPost,
			"/api/v1/xkms/fido2/authenticate/finish?tenant_id=acme",
			strings.NewReader(`{"session_id":"s2","username":"x","assertion_json":"{}"}`))
		w := httptest.NewRecorder()
		s.HandleXKMSFIDO2FinishAuthentication(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
		assert.NotContains(t, w.Body.String(), "ghost@example.com")
		assert.NotContains(t, w.Body.String(), "credentials")
		bodies = append(bodies, w.Body.String())
	}
	for i := 1; i < len(bodies); i++ {
		assert.Equal(t, bodies[0], bodies[i], "authentication failures must be byte-identical")
	}
}

func TestHandleXKMSFIDO2FinishAuthentication_Happy(t *testing.T) {
	fake := &fakeFIDO2Service{}
	s := newXKMSFIDO2Server(t, fake)

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/authenticate/finish?tenant_id=acme",
		strings.NewReader(`{"session_id":"s2","username":"alice@example.com","assertion_json":"{}"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2FinishAuthentication(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp transportTypes.FinishAuthenticationResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Success)
	assert.Equal(t, "tok", resp.Token)
}

func TestHandleXKMSFIDO2_NotConfigured(t *testing.T) {
	s, _ := newTestServer(t) // mock FIDO2() returns nil

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/begin?tenant_id=acme",
		strings.NewReader(`{"username":"alice@example.com"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2BeginRegistration(w, req)

	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

// TestXKMSFIDO2Routes_PEPVocabulary pins the compat FIDO2 legs onto the
// user_authfactors permission family: enrollment adds a factor,
// authentication proves one.
func TestXKMSFIDO2Routes_PEPVocabulary(t *testing.T) {
	tests := []struct {
		path         string
		wantResource string
		wantAction   string
	}{
		{"/api/v1/xkms/fido2/register/begin", "user_authfactors", "add"},
		{"/api/v1/xkms/fido2/register/finish", "user_authfactors", "add"},
		{"/api/v1/xkms/fido2/credentials/begin", "user_authfactors", "add"},
		{"/api/v1/xkms/fido2/authenticate/begin", "user_authfactors", "get"},
		{"/api/v1/xkms/fido2/authenticate/finish", "user_authfactors", "get"},
	}
	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			resource, action, ok := routePermissionForPath(tc.path, http.MethodPost)
			require.True(t, ok)
			assert.Equal(t, tc.wantResource, resource)
			assert.Equal(t, tc.wantAction, action)
		})
	}
}

// TestXKMSFIDO2Routes_NotPublic pins that no compat FIDO2 leg is pre-auth
// public: passkey login rides the canonical public surfaces instead.
func TestXKMSFIDO2Routes_NotPublic(t *testing.T) {
	for _, path := range []string{
		"/api/v1/xkms/fido2/register/begin",
		"/api/v1/xkms/fido2/register/finish",
		"/api/v1/xkms/fido2/credentials/begin",
		"/api/v1/xkms/fido2/authenticate/begin",
		"/api/v1/xkms/fido2/authenticate/finish",
	} {
		auth, ok := routeAuthForPath(path, http.MethodPost)
		require.True(t, ok, "%s must be in the frozen route table", path)
		assert.Equal(t, RouteAuthPermission, auth, "%s must stay gated", path)
	}
}
