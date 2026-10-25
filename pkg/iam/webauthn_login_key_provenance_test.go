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

package iam

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	// The in-memory WebAuthn stores and the software authenticator are not
	// re-exported by go-xkms/sdk/go (only NewWebAuthnMemorySessionStore is),
	// so this test reaches the same pkg/webauthn package the production
	// credential adapter in webauthn_credentials.go already imports. Nothing
	// new is coupled: the SDK aliases are used everywhere they exist.
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
)

// =============================================================================
// WHERE THE VERIFICATION KEY COMES FROM.
// =============================================================================
//
// go-quicbaas#484 raised the possibility that the FIDO2 login path takes the
// public key it verifies an assertion with from CLIENT-SUPPLIED method
// metadata ("cose_public_key"). If it did, anyone could present their own key
// and a matching signature and authenticate as any enrolled user.
//
// It does not, and this test is the standing proof rather than a reading of
// the code. The path is:
//
//	pkg/server login_factor_verifiers.go tenantWebAuthnVerifier.VerifyTenant
//	  -> go-xkms sdk/go WebAuthnFactorVerifier.Verify
//	    -> go-xkms pkg/webauthn Service.FinishLogin
//	      -> go-webauthn ValidateLogin(user, session, assertion)
//
// ValidateLogin matches the assertion's credential id against the credentials
// the USER STORE returns and verifies with the public key recorded there at
// registration. The request's metadata is never a source of key material.
//
// The test is DIFFERENTIAL because that is the only way to prove a negative
// about an input: the attacker's assertion is verifiable under the key the
// attacker also supplies, so if the key were read from the request it would
// pass. It is rejected, and it is rejected identically whether the key is
// present or absent.

// provenanceRPID and provenanceOrigin mirror the burn-in relying party the
// issue was reproduced against.
const (
	provenanceRPID   = "localhost"
	provenanceOrigin = "https://localhost:18443"
	provenanceEmail  = "provenance@example.com"
)

// provenanceHarness is one relying party with one enrolled credential.
type provenanceHarness struct {
	service   *xkmssdk.WebAuthnService
	verifier  *xkmssdk.WebAuthnFactorVerifier
	user      xkmswebauthn.User
	enrolled  *xkmswebauthn.MockAuthenticator
	credID    []byte
	credIDB64 string
	factor    *qrdbsdk.AuthFactor
}

// newProvenanceHarness builds a relying party over in-memory stores and runs a
// REAL registration ceremony so the credential under test was enrolled the way
// a live one is, not injected into the store.
func newProvenanceHarness(t *testing.T) *provenanceHarness {
	t.Helper()
	ctx := context.Background()

	users, creds := newTestWebAuthnStores(t)

	// Public registration enrols the first passkey of a NEW account in one
	// real ceremony. go-xkms#1294 closed the other unauthenticated path --
	// a pre-provisioned account enrolling its first passkey through
	// BeginRegistration -- so the account is created by the ceremony itself.
	// Which path enrolled the credential is immaterial to what this harness
	// tests (the provenance of the key a LOGIN verifies with).
	allowPublic := true
	service, err := xkmssdk.NewWebAuthnService(xkmssdk.WebAuthnServiceParams{
		Config: &xkmssdk.WebAuthnConfig{
			RPID:                    provenanceRPID,
			RPDisplayName:           "QuicBaaS",
			RPOrigins:               []string{provenanceOrigin},
			AllowPublicRegistration: &allowPublic,
		},
		UserStore:       users,
		SessionStore:    xkmssdk.NewWebAuthnMemorySessionStore(),
		CredentialStore: creds,
	})
	if err != nil {
		t.Fatalf("build relying party: %v", err)
	}

	options, sessionID, err := service.BeginRegistration(ctx, provenanceEmail, "Provenance User")
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	user, err := users.GetByEmail(ctx, provenanceEmail)
	if err != nil {
		t.Fatalf("resolve registrant: %v", err)
	}

	enrolled, err := xkmswebauthn.NewMockAuthenticator(provenanceRPID)
	if err != nil {
		t.Fatalf("build authenticator: %v", err)
	}
	attestation, err := enrolled.CreateAttestationObject(
		options.Response.Challenge, user.WebAuthnID(), provenanceOrigin)
	if err != nil {
		t.Fatalf("create attestation: %v", err)
	}
	if _, _, err := service.FinishRegistration(ctx, sessionID, attestation); err != nil {
		t.Fatalf("finish registration: %v", err)
	}

	credIDB64 := base64.RawURLEncoding.EncodeToString(enrolled.CredentialID)
	verifier, err := xkmssdk.NewWebAuthnFactorVerifier(service)
	if err != nil {
		t.Fatalf("build factor verifier: %v", err)
	}

	return &provenanceHarness{
		service:   service,
		verifier:  verifier,
		user:      user,
		enrolled:  enrolled,
		credID:    enrolled.CredentialID,
		credIDB64: credIDB64,
		factor: &qrdbsdk.AuthFactor{
			ID:     "factor-provenance",
			Type:   qrdbsdk.AuthFactorFIDO2,
			UserID: 0, // discoverable path; the session carries the user handle
			// The Credential blob is EXACTLY what enrollFactor writes:
			// the base64url credential-id string as bytes.
			Credential: []byte(credIDB64),
		},
	}
}

// challenge opens a login ceremony and returns the challenge plus the RP
// session id the verifier consumes.
func (h *provenanceHarness) challenge(t *testing.T) (challenge []byte, sessionID string) {
	t.Helper()
	options, sessionID, err := h.service.BeginLogin(context.Background(), h.user.WebAuthnID())
	if err != nil {
		t.Fatalf("begin login: %v", err)
	}
	return options.Response.Challenge, sessionID
}

// assertionJSON drives an authenticator and serialises the result into the
// wire JSON the verifier parses.
func assertionJSON(t *testing.T, auth *xkmswebauthn.MockAuthenticator, challenge, userHandle []byte) []byte {
	t.Helper()
	assertion, err := auth.CreateAssertionResponse(challenge, userHandle, provenanceOrigin)
	if err != nil {
		t.Fatalf("create assertion: %v", err)
	}
	encoded, err := json.Marshal(assertion.Raw)
	if err != nil {
		t.Fatalf("marshal assertion: %v", err)
	}
	return encoded
}

// TestWebAuthnLogin_EnrolledKeyAuthenticates is the control. Without it the
// rejection test below could pass because the harness is broken rather than
// because the invariant holds.
func TestWebAuthnLogin_EnrolledKeyAuthenticates(t *testing.T) {
	h := newProvenanceHarness(t)
	challenge, sessionID := h.challenge(t)

	err := h.verifier.Verify(context.Background(), h.factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypeFIDO2,
		Signature: assertionJSON(t, h.enrolled, challenge, h.user.WebAuthnID()),
		MethodMetadata: map[string]string{
			"session_id":    sessionID,
			"credential_id": h.credIDB64,
		},
	})
	if err != nil {
		t.Fatalf("the enrolled credential must authenticate: %v", err)
	}
}

// TestWebAuthnLogin_ClientSuppliedCOSEKeyCannotAuthenticate is the invariant.
//
// The attacker holds a different private key, presents an assertion that is
// internally consistent and correctly signed UNDER THAT KEY, reuses the
// victim's credential id so every identifier matches, and supplies the
// matching COSE public key in the request metadata. The only thing they do not
// have is the enrolled key. That must be enough to refuse them.
func TestWebAuthnLogin_ClientSuppliedCOSEKeyCannotAuthenticate(t *testing.T) {
	h := newProvenanceHarness(t)

	attacker, err := xkmswebauthn.NewMockAuthenticator(provenanceRPID,
		xkmswebauthn.WithCredentialID(h.credID))
	if err != nil {
		t.Fatalf("build attacker authenticator: %v", err)
	}
	attackerKey, err := attacker.PublicKeyBytes()
	if err != nil {
		t.Fatalf("attacker public key: %v", err)
	}

	challenge, sessionID := h.challenge(t)
	err = h.verifier.Verify(context.Background(), h.factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypeFIDO2,
		Signature: assertionJSON(t, attacker, challenge, h.user.WebAuthnID()),
		MethodMetadata: map[string]string{
			"session_id":      sessionID,
			"credential_id":   h.credIDB64,
			"cose_public_key": base64.StdEncoding.EncodeToString(attackerKey),
		},
	})
	if err == nil {
		t.Fatal("SECURITY: an assertion signed by an unenrolled key was accepted " +
			"when the request supplied the matching public key — the verification " +
			"key is being taken from the request instead of the enrollment")
	}
}

// TestWebAuthnLogin_COSEKeyMetadataChangesNothing closes the other half of the
// invariant: the metadata key is not merely insufficient, it is ignored.
//
// A verifier that consulted the request key would behave DIFFERENTLY with and
// without it. Both runs are rejected, and the enrolled key still succeeds with
// a bogus key present — so the field has no effect in either direction.
func TestWebAuthnLogin_COSEKeyMetadataChangesNothing(t *testing.T) {
	attackerRejectedWithout := func(t *testing.T) {
		h := newProvenanceHarness(t)
		attacker, err := xkmswebauthn.NewMockAuthenticator(provenanceRPID,
			xkmswebauthn.WithCredentialID(h.credID))
		if err != nil {
			t.Fatalf("build attacker authenticator: %v", err)
		}
		challenge, sessionID := h.challenge(t)
		if err := h.verifier.Verify(context.Background(), h.factor, qrdbtransport.VerifyChallenge{
			Type:      qrdbtransport.FactorTypeFIDO2,
			Signature: assertionJSON(t, attacker, challenge, h.user.WebAuthnID()),
			MethodMetadata: map[string]string{
				"session_id":    sessionID,
				"credential_id": h.credIDB64,
			},
		}); err == nil {
			t.Fatal("an unenrolled key was accepted with no cose_public_key at all")
		}
	}

	enrolledAcceptedWithBogusKey := func(t *testing.T) {
		h := newProvenanceHarness(t)
		challenge, sessionID := h.challenge(t)
		if err := h.verifier.Verify(context.Background(), h.factor, qrdbtransport.VerifyChallenge{
			Type:      qrdbtransport.FactorTypeFIDO2,
			Signature: assertionJSON(t, h.enrolled, challenge, h.user.WebAuthnID()),
			MethodMetadata: map[string]string{
				"session_id":      sessionID,
				"credential_id":   h.credIDB64,
				"cose_public_key": base64.StdEncoding.EncodeToString([]byte("not-a-cose-key")),
			},
		}); err != nil {
			t.Fatalf("a bogus cose_public_key changed the outcome for the enrolled key: %v", err)
		}
	}

	t.Run("unenrolled key rejected without the metadata", attackerRejectedWithout)
	t.Run("enrolled key accepted despite bogus metadata", enrolledAcceptedWithBogusKey)
}

// TestWebAuthnLogin_CredentialIDMustMatchTheEnrolledFactor asserts the
// verifier's own identity check: an assertion for some other credential cannot
// be presented against this factor even if it verifies on its own terms.
func TestWebAuthnLogin_CredentialIDMustMatchTheEnrolledFactor(t *testing.T) {
	h := newProvenanceHarness(t)
	challenge, sessionID := h.challenge(t)

	other, err := xkmswebauthn.NewMockAuthenticator(provenanceRPID)
	if err != nil {
		t.Fatalf("build second authenticator: %v", err)
	}
	otherID := base64.RawURLEncoding.EncodeToString(other.CredentialID)

	if err := h.verifier.Verify(context.Background(), h.factor, qrdbtransport.VerifyChallenge{
		Type:      qrdbtransport.FactorTypeFIDO2,
		Signature: assertionJSON(t, other, challenge, h.user.WebAuthnID()),
		MethodMetadata: map[string]string{
			"session_id":    sessionID,
			"credential_id": otherID,
		},
	}); err == nil {
		t.Fatal("an assertion for a different credential was accepted against this factor")
	}
}
