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
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// rejectingLoginClient fails every ceremony leg with a configurable error, so
// a test can replay each distinguishable back-end outcome through the unified
// login ceremony handlers (/api/v1/auth/login/{begin,finish}).
type rejectingLoginClient struct {
	err error
}

func (c *rejectingLoginClient) ResolveIdentity(_ context.Context, _ string) (*qrdbtransport.Identity, error) {
	return nil, c.err
}

func (c *rejectingLoginClient) BeginLogin(_ context.Context, _ qrdbtransport.BeginLoginRequest) (*qrdbtransport.BeginLoginResponse, error) {
	return nil, c.err
}

func (c *rejectingLoginClient) FinishLogin(_ context.Context, _ qrdbtransport.FinishLoginRequest) (*qrdbtransport.FinishLoginResponse, error) {
	return nil, c.err
}

func (c *rejectingLoginClient) Logout(_ context.Context, _ string) error { return c.err }

// setupRejectingCeremonyHandler builds an IAM handler whose login ceremony
// always fails with err.
func setupRejectingCeremonyHandler(t *testing.T, err error) (*IAMHandler, func()) {
	t.Helper()
	handler, _, cleanup := setupIAMHandler(t)
	handler.server.service.SetLoginService(service.NewLoginService(&rejectingLoginClient{err: err}))
	return handler, cleanup
}

// distinguishableLoginFailures are the back-end outcomes that must NOT produce
// different HTTP responses. Each one answers a question an anonymous caller
// has no right to ask: does this tenant exist, does this user exist, does
// this user hold a security key.
var distinguishableLoginFailures = []struct {
	name string
	err  error
}{
	{
		name: "unknown tenant",
		err:  qberrors.New(qberrors.ErrNotFound, "ServiceFor", "tenant \"acme-corp\" not found"),
	},
	{
		name: "known tenant, unknown user",
		err:  qberrors.New(qberrors.ErrNotFound, "GetUserByEmail", "user \"ghost@example.com\" not found"),
	},
	{
		name: "known user with no registered authenticator",
		err:  qberrors.New(qberrors.ErrInvalidInput, "BeginLogin", "no webauthn credentials registered"),
	},
	{
		name: "back-end unavailable",
		err:  qberrors.New(qberrors.ErrUnavailable, "BeginLogin", "identity store unreachable"),
	},
}

// serveCeremonyBeginLogin runs the unified login-begin handler against a
// ceremony that fails with err and returns the recorded response.
func serveCeremonyBeginLogin(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	handler, cleanup := setupRejectingCeremonyHandler(t, err)
	defer cleanup()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/begin",
		strings.NewReader(`{"type":"fido2","hint":"alice@example.com","tenant_id":"acme-corp"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.CeremonyBeginLogin(w, req)
	return w
}

// TestCeremonyBeginLogin_NoEnumerationViaErrorResponse is the security
// regression guard for the login-begin oracle on the unified ceremony (the
// surface browser sign-in rides). If login-begin answers "no such tenant"
// differently from "no such user", an anonymous caller can enumerate tenant
// and user namespaces one request at a time. Every failure must be
// byte-identical.
func TestCeremonyBeginLogin_NoEnumerationViaErrorResponse(t *testing.T) {
	baseline := serveCeremonyBeginLogin(t, distinguishableLoginFailures[0].err)

	require.Equal(t, http.StatusUnauthorized, baseline.Code,
		"login-begin failures must be a uniform 401")

	for _, tc := range distinguishableLoginFailures[1:] {
		t.Run(tc.name, func(t *testing.T) {
			w := serveCeremonyBeginLogin(t, tc.err)

			assert.Equal(t, baseline.Code, w.Code,
				"status differs from %q — the status code is an enumeration oracle",
				distinguishableLoginFailures[0].name)
			assert.Equal(t, baseline.Body.String(), w.Body.String(),
				"body differs from %q — the error text is an enumeration oracle",
				distinguishableLoginFailures[0].name)
		})
	}
}

// TestCeremonyBeginLogin_RejectionLeaksNoBackendDetail asserts the uniform
// response carries none of the identifiers or back-end vocabulary the
// underlying error contained.
func TestCeremonyBeginLogin_RejectionLeaksNoBackendDetail(t *testing.T) {
	for _, tc := range distinguishableLoginFailures {
		t.Run(tc.name, func(t *testing.T) {
			w := serveCeremonyBeginLogin(t, tc.err)
			body := w.Body.String()

			for _, leak := range []string{"tenant", "user", "not found", "credential", "unreachable", "acme-corp", "ghost@example.com"} {
				assert.NotContains(t, strings.ToLower(body), leak,
					"login rejection leaked %q: %s", leak, body)
			}
			assert.Contains(t, body, loginRejectedDescription,
				"login rejection must carry the fixed generic description")
		})
	}
}

// TestCeremonyFinishLogin_NoEnumerationViaErrorResponse pins the same
// uniformity on the second leg of the ceremony: a forged proof against a
// non-existent tenant must be indistinguishable from a genuinely failed
// proof.
func TestCeremonyFinishLogin_NoEnumerationViaErrorResponse(t *testing.T) {
	serve := func(err error) *httptest.ResponseRecorder {
		handler, cleanup := setupRejectingCeremonyHandler(t, err)
		defer cleanup()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish",
			strings.NewReader(`{"session_id":"s1","type":"fido2","lookup_key":"Y3JlZC0x"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		handler.CeremonyFinishLogin(w, req)
		return w
	}

	baseline := serve(distinguishableLoginFailures[0].err)
	require.Equal(t, http.StatusUnauthorized, baseline.Code)

	for _, tc := range distinguishableLoginFailures[1:] {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(tc.err)
			assert.Equal(t, baseline.Code, w.Code)
			assert.Equal(t, baseline.Body.String(), w.Body.String())
		})
	}
}

// TestRespondLoginRejected_IsConstant proves the uniform responder is not
// parameterized: there is no seam through which caller-specific detail can be
// threaded back into the response.
func TestRespondLoginRejected_IsConstant(t *testing.T) {
	first := httptest.NewRecorder()
	RespondLoginRejected(first, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/begin", nil))

	second := httptest.NewRecorder()
	RespondLoginRejected(second, httptest.NewRequest(http.MethodPost, "/api/v1/auth/login/finish", nil))

	assert.Equal(t, http.StatusUnauthorized, first.Code)
	assert.Equal(t, first.Code, second.Code)
	assert.Equal(t, first.Body.String(), second.Body.String())
}

// TestLoginRejectedError_UnwrapsToSentinel covers the typed-error contract so
// callers can match the rejection with errors.Is.
func TestLoginRejectedError_UnwrapsToSentinel(t *testing.T) {
	err := &LoginRejectedError{}
	assert.Equal(t, "iam.login: authentication failed", err.Error())
	assert.ErrorIs(t, err, ErrLoginRejected)
}
