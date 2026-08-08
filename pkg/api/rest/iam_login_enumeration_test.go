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

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// rejectingWebAuthnManager fails every ceremony with a configurable error, so a
// test can replay each distinguishable back-end outcome through the handler.
type rejectingWebAuthnManager struct {
	err error
}

func (m *rejectingWebAuthnManager) BeginRegistration(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return nil, m.err
}

func (m *rejectingWebAuthnManager) FinishRegistration(_ context.Context, _, _ string, _ map[string]interface{}) error {
	return m.err
}

func (m *rejectingWebAuthnManager) BeginLogin(_ context.Context, _, _ string) (map[string]interface{}, error) {
	return nil, m.err
}

func (m *rejectingWebAuthnManager) FinishLogin(_ context.Context, _ string, _ map[string]interface{}) (*iam.User, error) {
	return nil, m.err
}

// setupRejectingWebAuthnHandler builds an IAM handler whose WebAuthn manager
// always fails with err.
func setupRejectingWebAuthnHandler(t *testing.T, err error) *IAMHandler {
	t.Helper()

	log, logErr := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, logErr)

	mockSvc := &webAuthnService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		mgr:          &rejectingWebAuthnManager{err: err},
		audit:        &captureAuditLogger{},
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, srvErr := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, srvErr)
	server.service = mockSvc
	return NewIAMHandler(IAMHandlerDependencies{Server: server})
}

// distinguishableLoginFailures are the back-end outcomes that used to produce
// DIFFERENT HTTP responses. Each one answers a question an anonymous caller has
// no right to ask: does this tenant exist, does this user exist, does this user
// hold a security key.
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

// serveBeginLogin runs the WebAuthn login-begin handler against a manager that
// fails with err and returns the recorded response.
func serveBeginLogin(t *testing.T, err error) *httptest.ResponseRecorder {
	t.Helper()
	handler := setupRejectingWebAuthnHandler(t, err)
	req := webAuthnReq(http.MethodPost,
		map[string]string{"email": "alice@example.com"},
		map[string]string{"tenant_id": "acme-corp"})
	w := httptest.NewRecorder()
	handler.BeginLogin(w, req)
	return w
}

// TestBeginLogin_NoEnumerationViaErrorResponse is the security regression guard
// for the login-begin oracle. Deleting the anonymous tenant-listing endpoint is
// only half the fix: if login-begin still answers "no such tenant" differently
// from "no such user", an anonymous caller can enumerate the same namespaces one
// request at a time. Every failure must be byte-identical.
func TestBeginLogin_NoEnumerationViaErrorResponse(t *testing.T) {
	baseline := serveBeginLogin(t, distinguishableLoginFailures[0].err)

	require.Equal(t, http.StatusUnauthorized, baseline.Code,
		"login-begin failures must be a uniform 401")

	for _, tc := range distinguishableLoginFailures[1:] {
		t.Run(tc.name, func(t *testing.T) {
			w := serveBeginLogin(t, tc.err)

			assert.Equal(t, baseline.Code, w.Code,
				"status differs from %q — the status code is an enumeration oracle",
				distinguishableLoginFailures[0].name)
			assert.Equal(t, baseline.Body.String(), w.Body.String(),
				"body differs from %q — the error text is an enumeration oracle",
				distinguishableLoginFailures[0].name)
		})
	}
}

// TestBeginLogin_RejectionLeaksNoBackendDetail asserts the uniform response
// carries none of the identifiers or back-end vocabulary the underlying error
// contained.
func TestBeginLogin_RejectionLeaksNoBackendDetail(t *testing.T) {
	for _, tc := range distinguishableLoginFailures {
		t.Run(tc.name, func(t *testing.T) {
			w := serveBeginLogin(t, tc.err)
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

// TestFinishLogin_NoEnumerationViaErrorResponse pins the same uniformity on the
// second leg of the ceremony: a forged assertion against a non-existent tenant
// must be indistinguishable from a genuinely failed assertion.
func TestFinishLogin_NoEnumerationViaErrorResponse(t *testing.T) {
	serve := func(err error) *httptest.ResponseRecorder {
		handler := setupRejectingWebAuthnHandler(t, err)
		req := webAuthnReq(http.MethodPost,
			map[string]interface{}{"credential": map[string]interface{}{"id": "cred-1"}},
			map[string]string{"tenant_id": "acme-corp"})
		w := httptest.NewRecorder()
		handler.FinishLogin(w, req)
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
	RespondLoginRejected(first, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/a/webauthn/login/begin", nil))

	second := httptest.NewRecorder()
	RespondLoginRejected(second, httptest.NewRequest(http.MethodPost, "/api/v1/tenants/zzz/webauthn/login/begin", nil))

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
