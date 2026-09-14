//go:build rest

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

package rest

import (
	"encoding/json"
	stderrors "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// serverSecrets are strings that describe the DEPLOYMENT rather than the
// request. None of them may appear in a response body.
//
// Asserted as a property rather than by pinning the exact response text, the
// way iam_login_piv_disclosure_test.go does: pinning a string passes as soon as
// the message is reworded, while "no filesystem path reaches the wire" keeps
// meaning the same thing after a refactor.
var serverSecrets = []string{
	"/etc/quicbaas/secrets/tenant-ca.key",
	"permission denied",
	"10.0.7.22:5432",
	"CN=internal-issuing-ca",
	"service.LoadTenantCA",
}

// disclosureProbe is an error shaped like the ones that actually reach this
// helper: a wrapped cause naming a path, an operation naming an internal
// method, and Context carrying a backend address.
func disclosureProbe(code qberrors.ErrorCode) *qberrors.QBError {
	return &qberrors.QBError{
		Code: code,
		Op:   "service.LoadTenantCA",
		Err:  stderrors.New("open /etc/quicbaas/secrets/tenant-ca.key: permission denied"),
		Context: map[string]interface{}{
			"backend": "10.0.7.22:5432",
			"issuer":  "CN=internal-issuing-ca",
		},
	}
}

// TestHandleServiceError_ServerStateIsNotDisclosed is the regression for #427.
//
// Every code that describes the server must produce a stable body. Before the
// fix each of these returned the wrapped cause verbatim -- measured, including
// on the helper serving the RouteAuthPublic federated-login routes -- and
// nothing was logged, so the caller held the only copy.
func TestHandleServiceError_ServerStateIsNotDisclosed(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	for _, code := range []qberrors.ErrorCode{
		qberrors.ErrInternal,
		qberrors.ErrUnknown,
		qberrors.ErrStorage,
		qberrors.ErrRaft,
		qberrors.ErrUnavailable,
		qberrors.ErrLeaderNotAvailable,
		qberrors.ErrClusterNotReady,
		qberrors.ErrQuorumLost,
	} {
		t.Run(code.String(), func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)

			server.handleServiceError(w, r, disclosureProbe(code))

			body := w.Body.String()
			for _, secret := range serverSecrets {
				if strings.Contains(body, secret) {
					t.Errorf("the response body disclosed %q to the caller.\nbody: %s", secret, body)
				}
			}
		})
	}
}

// TestHandleServiceError_SanitizedResponseKeepsCodeAndRetryability pins the
// half that makes the sanitisation safe to adopt.
//
// Stripping the message must not change what a client branches on. A leadership
// transition that stopped reading as retryable would turn a backoff-and-retry
// into a hard failure -- a correctness regression introduced by a security fix,
// which is the trade this test exists to refuse.
func TestHandleServiceError_SanitizedResponseKeepsCodeAndRetryability(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/probe", nil)
	server.handleServiceError(w, r, disclosureProbe(qberrors.ErrLeaderNotAvailable))

	var resp struct {
		Error     string                 `json:"error"`
		Code      string                 `json:"code"`
		Retryable bool                   `json:"retryable"`
		Details   map[string]interface{} `json:"details"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body %s)", err, w.Body.String())
	}

	if resp.Code != qberrors.ErrLeaderNotAvailable.String() {
		t.Errorf("code = %q, want %q: clients branch on the code, so sanitising the message must not change it",
			resp.Code, qberrors.ErrLeaderNotAvailable.String())
	}
	if !resp.Retryable {
		t.Error("retryable = false for a leadership transition: a client that stops retrying turns a transient failure into a hard one")
	}
	if !strings.Contains(resp.Error, ErrMsgServerFailure) {
		t.Errorf("error = %q, want it to carry the stable message %q", resp.Error, ErrMsgServerFailure)
	}
	if len(resp.Details) != 0 {
		t.Errorf("details = %v, want empty: Context is serialized into details, so leaving it attached leaks through the field nobody reads",
			resp.Details)
	}
}

// TestHandleServiceError_CallerInputIsStillEchoed guards the boundary. The fix
// must not become "say nothing to anyone": an error about what the caller sent
// is exactly what the caller needs, and over-sanitising would make the API
// undebuggable while protecting nothing.
func TestHandleServiceError_CallerInputIsStillEchoed(t *testing.T) {
	server, _, cleanup := setupMockServer(t)
	defer cleanup()

	// No quotes in the detail: the body is JSON, so a quoted string arrives
	// escaped and a naive Contains would fail for an encoding reason rather
	// than a behavioural one.
	const detail = "the email field must be a valid address"

	for _, code := range []qberrors.ErrorCode{
		qberrors.ErrValidation,
		qberrors.ErrInvalidInput,
		qberrors.ErrConflict,
		qberrors.ErrAlreadyExists,
	} {
		t.Run(code.String(), func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPost, "/api/v1/probe", nil)

			server.handleServiceError(w, r, &qberrors.QBError{
				Code: code, Op: "service.CreateUser", Err: stderrors.New(detail),
			})

			if !strings.Contains(w.Body.String(), detail) {
				t.Errorf("a %s error stopped telling the caller what was wrong with their request.\nbody: %s",
					code.String(), w.Body.String())
			}
		})
	}
}
