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
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// attestationTestUser is the principal used across these tests.
func attestationTestUser() *User {
	return &User{
		ID:       "user-1",
		TenantID: "tenant-1",
		Email:    "admin@example.com",
		Name:     "Admin",
		Roles:    []string{"Admin"},
	}
}

// TestIssueHardwareTokens_RefusesHardwareACRWithoutProof is the control that
// makes acr=hardware-attested mean something.
//
// Both production callers used to pass the literal string "hardware" with
// nothing having checked anything: the PIV login handler for ANY TLS client
// certificate, and self-service registration for ANY WebAuthn credential. The
// claim was a label the handler chose, so a consumer downstream could not tell
// a YubiKey login from a virtual-authenticator one.
func TestIssueHardwareTokens_RefusesHardwareACRWithoutProof(t *testing.T) {
	service, _ := createTestTokenService(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name        string
		attestation *AttestationResult
		wantProven  AttestLevel
	}{
		{"no evidence at all", nil, AttestLevelNone},
		{"evidence proving nothing", &AttestationResult{level: AttestLevelNone, method: AttestMethodWebAuthn}, AttestLevelNone},
		{"self-attested software", &AttestationResult{level: AttestLevelSoftware, format: "basic_surrogate"}, AttestLevelSoftware},
		{"unattested PIV opt-out", UnattestedPIVCredential(), AttestLevelSoftware},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, err := service.IssueHardwareTokens(ctx, HardwareTokenOptions{
				User:        attestationTestUser(),
				AMR:         []string{"webauthn", "hwk"},
				ACR:         ACRHardwareAttested,
				Attestation: tc.attestation,
			})
			if err == nil {
				t.Fatal("a hardware-attested token was minted without hardware evidence")
			}
			if set != nil {
				t.Fatalf("a refusal must mint no tokens, got %+v", set)
			}
			var required *AttestationRequiredError
			if !errors.As(err, &required) {
				t.Fatalf("want *AttestationRequiredError, got %T: %v", err, err)
			}
			if required.Proven != tc.wantProven {
				t.Errorf("Proven = %q, want %q", required.Proven, tc.wantProven)
			}
		})
	}
}

// TestIssueHardwareTokens_AllowsHardwareACRWithProof is the positive control:
// evidence a verifier in this package produced does mint the hardware ACR, and
// the attestation claims are DERIVED from that evidence rather than supplied.
func TestIssueHardwareTokens_AllowsHardwareACRWithProof(t *testing.T) {
	service, privateKey := createTestTokenService(t)

	set, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User:        attestationTestUser(),
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation("cb69481e8ff7403993ec0a2729a154a8"),
	})
	if err != nil {
		t.Fatalf("proven hardware was refused: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.IDToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	if got := claims["attest_level"]; got != string(AttestLevelHardware) {
		t.Errorf("attest_level = %v, want %q", got, AttestLevelHardware)
	}
	if got := claims["attest_aaguid"]; got != "cb69481e8ff7403993ec0a2729a154a8" {
		t.Errorf("attest_aaguid = %v", got)
	}
	if got := claims["acr"]; got != ACRHardwareAttested {
		t.Errorf("acr = %v, want %q", got, ACRHardwareAttested)
	}
}

// TestIssueHardwareTokens_NonHardwareACRRecordsTheProvenLevel covers the
// explicit-downgrade path: a login that did not prove hardware still issues
// tokens, and the token says what was actually proven rather than staying
// silent. Silence would be indistinguishable from a path that never asked.
func TestIssueHardwareTokens_NonHardwareACRRecordsTheProvenLevel(t *testing.T) {
	service, privateKey := createTestTokenService(t)

	set, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User:        attestationTestUser(),
		AMR:         []string{"smartcard"},
		Attestation: UnattestedPIVCredential(),
	})
	if err != nil {
		t.Fatalf("an unattested login was refused where no hardware ACR was requested: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.IDToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	if got := claims["attest_level"]; got != string(AttestLevelSoftware) {
		t.Errorf("attest_level = %v, want %q", got, AttestLevelSoftware)
	}
	if got, ok := claims["acr"]; ok && got == ACRHardwareAttested {
		t.Error("a token with no proven hardware carries the hardware-attested ACR")
	}
}

// TestIssueHardwareTokens_NoAttestationOmitsTheClaims proves an absent
// AttestationResult does not emit an empty or placeholder attest_level. A claim
// that is present but meaningless is worse than one that is absent.
func TestIssueHardwareTokens_NoAttestationOmitsTheClaims(t *testing.T) {
	service, privateKey := createTestTokenService(t)

	set, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User: attestationTestUser(),
		AMR:  []string{"webauthn"},
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.IDToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify id token: %v", err)
	}
	if _, ok := claims["attest_level"]; ok {
		t.Errorf("attest_level is present without evidence: %v", claims["attest_level"])
	}
	if _, ok := claims["attest_aaguid"]; ok {
		t.Errorf("attest_aaguid is present without evidence: %v", claims["attest_aaguid"])
	}
}

// TestIssueHardwareTokens_RefusesForgedHardwareAMR closes the other half of the
// assertion boundary.
//
// The ACR is the obvious way to claim hardware, but RFC 8176 amr values say the
// same thing in a different field: "hwk" is proof-of-possession of a HARDWARE
// key and "sc" is a smart card. A caller that set only those would produce a
// token WITHOUT the hardware-attested ACR that still tells every relying party
// it holds a hardware credential — and this function is the issuance boundary,
// so a handler deriving amr correctly today does not close the contract for the
// next in-process caller.
func TestIssueHardwareTokens_RefusesForgedHardwareAMR(t *testing.T) {
	service, _ := createTestTokenService(t)

	for _, tc := range []struct {
		name string
		amr  []string
	}{
		{"hardware key", []string{"webauthn", "hwk"}},
		{"smart card", []string{"sc"}},
		{"case and spacing do not evade it", []string{" HWK "}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
				User: attestationTestUser(),
				AMR:  tc.amr,
				// No ACR, no attestation: the amr alone carries the claim.
			})
			if err == nil {
				t.Fatalf("amr %v claimed hardware without evidence", tc.amr)
			}
			if set != nil {
				t.Fatalf("a refusal must mint no tokens, got %+v", set)
			}
			var required *AttestationRequiredError
			if !errors.As(err, &required) {
				t.Fatalf("want *AttestationRequiredError, got %T: %v", err, err)
			}
			if !strings.Contains(required.Operation, "amr=") {
				t.Errorf("the refusal does not name the offending claim: %q", required.Operation)
			}
		})
	}
}

// TestIssueHardwareTokens_AllowsCeremonyAMRWithoutProof keeps the refusal
// narrow. Values that describe the CEREMONY rather than the key's storage say
// how the user authenticated, not where the key lives, so they need no
// attestation.
func TestIssueHardwareTokens_AllowsCeremonyAMRWithoutProof(t *testing.T) {
	service, _ := createTestTokenService(t)

	for _, amr := range [][]string{
		{"webauthn"},
		{"smartcard"},
		{"pwd", "mfa"},
		nil,
	} {
		if _, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
			User: attestationTestUser(),
			AMR:  amr,
		}); err != nil {
			t.Errorf("amr %v was refused though it claims no hardware: %v", amr, err)
		}
	}
}

// TestIssueHardwareTokens_AllowsHardwareAMRWithProof is the positive control.
func TestIssueHardwareTokens_AllowsHardwareAMRWithProof(t *testing.T) {
	service, _ := createTestTokenService(t)

	if _, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User:        attestationTestUser(),
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation("cb69481e"),
	}); err != nil {
		t.Fatalf("proven hardware was refused: %v", err)
	}
}

// =============================================================================
// The attestation has to reach the token that AUTHORIZES, not only the one
// that describes the login.
// =============================================================================

// TestIssueHardwareTokens_AccessTokenCarriesTheAttestation is the claim that
// makes attestation enforceable.
//
// The ID token describes the login to the client; the ACCESS token is what a
// resource server is handed and authorizes from. An attestation carried only
// by the ID token is therefore invisible at exactly the point where a
// hardware-only policy is applied: the bearer would be indistinguishable from
// an unattested session's.
func TestIssueHardwareTokens_AccessTokenCarriesTheAttestation(t *testing.T) {
	service, privateKey := createTestTokenService(t)

	set, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User:        attestationTestUser(),
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation("cb69481e8ff7403993ec0a2729a154a8"),
	})
	if err != nil {
		t.Fatalf("proven hardware was refused: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.AccessToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if got := claims["attest_level"]; got != string(AttestLevelHardware) {
		t.Errorf("access token attest_level = %v, want %q", got, AttestLevelHardware)
	}
	if got := claims["attest_aaguid"]; got != "cb69481e8ff7403993ec0a2729a154a8" {
		t.Errorf("access token attest_aaguid = %v", got)
	}
}

// TestIssueHardwareTokens_AccessTokenOmitsAnAbsentAttestation is the
// fail-closed half: a login that proved nothing must leave the claim absent on
// the bearer rather than emitting an empty string, so a policy reading
// attest_level gets "no evidence" and never a value it might read as an answer.
func TestIssueHardwareTokens_AccessTokenOmitsAnAbsentAttestation(t *testing.T) {
	service, privateKey := createTestTokenService(t)

	set, err := service.IssueHardwareTokens(context.Background(), HardwareTokenOptions{
		User: attestationTestUser(),
		AMR:  []string{"webauthn"},
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(set.AccessToken, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}
	if got, ok := claims["attest_level"]; ok {
		t.Errorf("access token asserts attest_level %v without evidence", got)
	}
	if got, ok := claims["attest_aaguid"]; ok {
		t.Errorf("access token asserts attest_aaguid %v without evidence", got)
	}
}

// TestIssueHardwareTokens_MFAVerifiedFollowsTheEvidence is the assurance
// counterpart of the attestation controls above.
//
// mfa_verified used to be the literal true on every path through this
// function. An operator who opted into iam.piv_login.allow_unattested therefore
// got an ID token asserting multi-factor for a certificate that failed
// smartcard attestation — an ordinary software-held TLS key, one factor, no
// card. A relying party enforcing an MFA policy admitted the weakest login the
// endpoint can issue.
//
// The claim now says what the ceremony proved, on BOTH the ID token that
// describes the login and the bearer that authorizes with it.
func TestIssueHardwareTokens_MFAVerifiedFollowsTheEvidence(t *testing.T) {
	service, privateKey := createTestTokenService(t)
	ctx := context.Background()

	for _, tc := range []struct {
		name        string
		amr         []string
		acr         string
		attestation *AttestationResult
		wantMFA     bool
	}{
		{
			name:        "proven hardware is multi-factor",
			amr:         []string{"smartcard", "mfa"},
			acr:         ACRHardwareAttested,
			attestation: testHardwareAttestation("cb69481e8ff7403993ec0a2729a154a8"),
			wantMFA:     true,
		},
		{
			name:        "unattested PIV opt-out proves one software factor",
			amr:         []string{"mtls"},
			attestation: UnattestedPIVCredential(),
			wantMFA:     false,
		},
		{
			name:        "self-attested webauthn proves one software factor",
			amr:         []string{"webauthn"},
			attestation: &AttestationResult{level: AttestLevelSoftware, format: "basic_surrogate", method: AttestMethodWebAuthn},
			wantMFA:     false,
		},
		{
			name:    "no evidence at all",
			amr:     []string{"webauthn"},
			wantMFA: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			set, err := service.IssueHardwareTokens(ctx, HardwareTokenOptions{
				User:        attestationTestUser(),
				AMR:         tc.amr,
				ACR:         tc.acr,
				Attestation: tc.attestation,
			})
			if err != nil {
				t.Fatalf("IssueHardwareTokens: %v", err)
			}

			idClaims, err := xkms.JWTVerifyWithKeyAndClaims(set.IDToken, &privateKey.PublicKey, "RS256")
			if err != nil {
				t.Fatalf("verify id token: %v", err)
			}
			if got := idClaims["mfa_verified"]; got != tc.wantMFA {
				t.Errorf("id token mfa_verified = %v, want %v", got, tc.wantMFA)
			}

			// The bearer carries the same answer as the ID token. A bearer that
			// omitted it would acquire the claim at its first renewal, which
			// reads downstream as an assurance UPGRADE no ceremony granted.
			accessClaims, err := xkms.JWTVerifyWithKeyAndClaims(set.AccessToken, &privateKey.PublicKey, "RS256")
			if err != nil {
				t.Fatalf("verify access token: %v", err)
			}
			if got := accessClaims["mfa_verified"]; got != tc.wantMFA {
				t.Errorf("access token mfa_verified = %v, want %v", got, tc.wantMFA)
			}
		})
	}
}
