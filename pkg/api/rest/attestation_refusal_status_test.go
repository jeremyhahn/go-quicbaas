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
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// =============================================================================
// A refused authenticator is a rejected request, not a broken server.
//
// The attestation gate can now REFUSE a registration: with
// iam.webauthn.assurance=hardware an operator whose browser presents a
// software authenticator, or a manufacturer statement no trusted root vouches
// for, is turned away. Every registration surface has to say so with a status
// the caller can act on.
//
// A 500 here is not a cosmetic defect. It tells the client the server failed,
// so a retry is reasonable — and it will fail identically forever. It hides an
// actionable message ("enroll a hardware security key", "set
// iam.webauthn.attestation_roots_file") behind a generic fault, and it puts a
// policy refusal into the same bucket as an outage in every dashboard and
// alert that counts 5xx.
// =============================================================================

// attestationRefusals returns the gate's refusals as the gate really produces
// them.
//
// They are obtained by RUNNING the production gate rather than by writing down
// the error values it is believed to return. A hand-built error would assert
// this repo's transports against a shape nothing guarantees: the defect these
// tests exist for was precisely that a refusal's real shape reached a handler
// that did not recognise it.
//
// The gate is built with hardware assurance, no allowlist and no admin
// resolver — its strict configuration, which is also the default an operator
// gets by configuring nothing.
func attestationRefusals(t *testing.T) []struct {
	name string
	err  error
} {
	t.Helper()

	gate := iam.NewWebAuthnAttestationGate(iam.WebAuthnAssuranceHardware, nil, nil)
	aaguid := []byte{0xcb, 0x69, 0x48, 0x1e, 0x8f, 0xf7, 0x40, 0x39,
		0x93, 0xec, 0x0a, 0x27, 0x29, 0xa1, 0x54, 0xa8}

	refusal := func(attestationType string) error {
		_, err := gate.Evaluate(context.Background(), "acme", "alice@acme.com", attestationType, aaguid)
		require.Error(t, err, "the gate admitted %q where hardware was required", attestationType)
		return err
	}

	return []struct {
		name string
		err  error
	}{
		// The everyday case: a platform or virtual authenticator whose
		// statement is self-signed and proves nothing about key storage.
		{name: "software authenticator", err: refusal("basic_surrogate")},
		// The dangerous case: the authenticator CLAIMED a manufacturer
		// attestation and no trusted root confirmed the claim.
		{name: "unverified attestation chain", err: refusal("basic_full")},
	}
}

// selfRegistrationFixture prepares the self-service registration surface: the
// feature enabled, a live ceremony session, and a WebAuthn manager whose
// finish is about to refuse.
func selfRegistrationFixture(t *testing.T, finishErr error) (*activationFixture, string, func()) {
	t.Helper()

	f, cleanup := setupActivation(t, writeSucceeds)
	f.handler.server.config.IAMSelfServiceRegistration = config.IAMSelfServiceRegistrationConfig{
		Enabled: true,
	}
	f.webauthn.finishErr = finishErr

	sessionID := "reg-session-" + t.Name()
	globalRegSessionStore.put(sessionID, &registrationSession{
		tenantID:   "acme-new",
		tenantName: "Acme New",
		email:      "alice@acme.com",
		expiresAt:  time.Now().Add(5 * time.Minute),
	})

	return f, sessionID, func() {
		globalRegSessionStore.delete(sessionID)
		cleanup()
	}
}

// TestRegisterSelfFinish_AttestationRefusalIsNotAServerFault covers
// POST /api/v1/auth/register/finish, the surface a brand-new operator meets
// first. Refusing them with a 500 tells them the product is broken when the
// truth is that their authenticator does not meet the deployment's policy.
func TestRegisterSelfFinish_AttestationRefusalIsNotAServerFault(t *testing.T) {
	for _, tc := range attestationRefusals(t) {
		t.Run(tc.name, func(t *testing.T) {
			f, sessionID, cleanup := selfRegistrationFixture(t, tc.err)
			defer cleanup()

			w := postJSON(t, f.handler.RegisterSelfFinish, SelfRegisterFinishRequest{
				SessionID:  sessionID,
				Credential: map[string]interface{}{"id": "abc"},
			})

			assert.Equal(t, http.StatusForbidden, w.Code,
				"a refused authenticator was reported as a server fault: %s", w.Body.String())
			assert.Contains(t, w.Body.String(), "attestation",
				"the refusal does not tell the registrant what was wrong")
		})
	}
}

// TestActivateFIDO2Finish_AttestationRefusalIsNotAServerFault covers
// POST /api/v1/iam/{tenant}/invites/{token}/activate/fido2/finish. An invited
// operator hitting the hardware bar needs to be told to use a security key,
// and their invite must remain spendable — which a 5xx obscures.
func TestActivateFIDO2Finish_AttestationRefusalIsNotAServerFault(t *testing.T) {
	for _, tc := range attestationRefusals(t) {
		t.Run(tc.name, func(t *testing.T) {
			f, cleanup := setupActivation(t, writeSucceeds)
			defer cleanup()
			f.webauthn.finishErr = tc.err

			w := f.finish(t)

			assert.Equal(t, http.StatusForbidden, w.Code,
				"a refused authenticator was reported as a server fault: %s", w.Body.String())
		})
	}
}

// TestHandleXKMSFIDO2FinishRegistration_AttestationRefusalIsNotAServerFault
// covers the compat bridge POST /api/v1/xkms/fido2/register/finish. Automation
// drives this endpoint, and automation retries 5xx: classifying a permanent
// policy refusal as a transient fault turns one rejected authenticator into a
// retry loop.
func TestHandleXKMSFIDO2FinishRegistration_AttestationRefusalIsNotAServerFault(t *testing.T) {
	for _, tc := range attestationRefusals(t) {
		t.Run(tc.name, func(t *testing.T) {
			s := newXKMSFIDO2Server(t, &fakeFIDO2Service{err: tc.err})

			req := httptest.NewRequest(http.MethodPost,
				"/api/v1/xkms/fido2/register/finish?tenant_id=acme",
				strings.NewReader(`{"session_id":"s1","attestation_json":"{}"}`))
			w := httptest.NewRecorder()
			s.HandleXKMSFIDO2FinishRegistration(w, req)

			assert.Equal(t, http.StatusForbidden, w.Code,
				"a refused authenticator was reported as a server fault: %s", w.Body.String())
		})
	}
}

// TestHandleXKMSFIDO2FinishRegistration_AAGUIDRefusalIsNotAServerFault is the
// sibling case the compat handler ALSO got wrong, and the reason the fix
// belongs in its error translator rather than in a special case for the
// attestation error types.
//
// The AAGUID allowlist refusal has always been a QBError carrying
// ErrForbidden — the repo's canonical classification, which the canonical REST
// mount, gRPC and the SDK transports all honour. This handler ignored it and
// returned 500, so the next typed refusal to reach it would have been wrong
// again for exactly the same reason.
func TestHandleXKMSFIDO2FinishRegistration_AAGUIDRefusalIsNotAServerFault(t *testing.T) {
	s := newXKMSFIDO2Server(t, &fakeFIDO2Service{
		err: iam.NewErrAAGUIDNotAllowed("cb69481e8ff7403993ec0a2729a154a8", "Some Key"),
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/finish?tenant_id=acme",
		strings.NewReader(`{"session_id":"s1","attestation_json":"{}"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2FinishRegistration(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code,
		"a disallowed authenticator model was reported as a server fault: %s", w.Body.String())
}

// TestHandleXKMSFIDO2FinishRegistration_RealFailureStaysAServerFault is the
// control that keeps the classification honest. Widening 500 to 403 would be
// no better than the defect: a genuine ceremony failure must still read as one,
// or an outage becomes invisible.
func TestHandleXKMSFIDO2FinishRegistration_RealFailureStaysAServerFault(t *testing.T) {
	s := newXKMSFIDO2Server(t, &fakeFIDO2Service{
		err: &iam.IAMMessageError{Message: "credential store unavailable"},
	})

	req := httptest.NewRequest(http.MethodPost,
		"/api/v1/xkms/fido2/register/finish?tenant_id=acme",
		strings.NewReader(`{"session_id":"s1","attestation_json":"{}"}`))
	w := httptest.NewRecorder()
	s.HandleXKMSFIDO2FinishRegistration(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code,
		"a real ceremony failure stopped reading as a server fault: %s", w.Body.String())
}
