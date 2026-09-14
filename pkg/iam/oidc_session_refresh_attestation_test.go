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
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// =============================================================================
// Attestation across a session's lifetime.
//
// A refresh renews a session from the context frozen at login, so whatever the
// login proved about the credential has to survive there or it is lost at the
// first renewal.
// =============================================================================

// TestRefreshSession_ReplaysTheAttestationOfTheLoginThatProvedIt closes the
// other end of the session's lifetime.
//
// A refresh RENEWS a session; it does not re-authenticate, so the renewed
// bearer is rebuilt entirely from the context frozen at login. An attestation
// that is not frozen there is gone after the first renewal: a session that
// really did prove a YubiKey silently becomes indistinguishable from an
// unattested one within one access-token lifetime, and a hardware-only policy
// starts refusing — or worse, an operator relaxes the policy to compensate.
func TestRefreshSession_ReplaysTheAttestationOfTheLoginThatProvedIt(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	// The refresh gate resolves the session's principal by its immutable id, so
	// the account has to exist for the renewal to be granted at all.
	user := seedCeremonyUser(t, storage, "acme", "hw-admin", []string{"admin"})

	set, err := provider.tokenService.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User:        user,
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation("cb69481e8ff7403993ec0a2729a154a8"),
	})
	if err != nil {
		t.Fatalf("proven hardware was refused: %v", err)
	}

	tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: set.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(tokens.AccessToken, provider.config.SigningKey.Public(), "RS256")
	if err != nil {
		t.Fatalf("verify renewed access token: %v", err)
	}
	if got := claims["attest_level"]; got != string(AttestLevelHardware) {
		t.Errorf("renewed access token attest_level = %v, want %q", got, AttestLevelHardware)
	}
	if got := claims["attest_aaguid"]; got != "cb69481e8ff7403993ec0a2729a154a8" {
		t.Errorf("renewed access token attest_aaguid = %v", got)
	}
}

// TestRefreshSession_InventsNoAttestationForAnUnattestedSession is the
// fail-closed control for the replay above: renewal repeats what the login
// proved, and a login that proved nothing must renew into a bearer that still
// says nothing. A refresh is not an opportunity to acquire assurance.
func TestRefreshSession_InventsNoAttestationForAnUnattestedSession(t *testing.T) {
	provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	user := seedCeremonyUser(t, storage, "acme", "sw-user", []string{"admin"})

	set, err := provider.tokenService.IssueHardwareTokens(ctx, HardwareTokenOptions{
		User: user,
		AMR:  []string{"webauthn"},
	})
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}

	tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: set.RefreshToken})
	if err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}

	claims, err := xkms.JWTVerifyWithKeyAndClaims(tokens.AccessToken, provider.config.SigningKey.Public(), "RS256")
	if err != nil {
		t.Fatalf("verify renewed access token: %v", err)
	}
	if got, ok := claims["attest_level"]; ok {
		t.Errorf("renewal invented attest_level %v for a session that proved nothing", got)
	}
	if got, ok := claims["attest_aaguid"]; ok {
		t.Errorf("renewal invented attest_aaguid %v for a session that proved nothing", got)
	}
}

// TestRefreshSession_MFAAssuranceSurvivesRenewalWithoutBeingInvented is the
// assurance half of the same lifetime property.
//
// A renewal rebuilds the bearer entirely from the context frozen at login, so
// mfa_verified is only as honest as what was frozen there. It used to be the
// literal true for every hardware-token login including the unattested PIV
// opt-out, which meant the false assertion was not merely issued once: it was
// persisted, and every renewal for the whole refresh lifetime repeated it long
// after the login that made it was gone.
func TestRefreshSession_MFAAssuranceSurvivesRenewalWithoutBeingInvented(t *testing.T) {
	for _, tc := range []struct {
		name        string
		amr         []string
		acr         string
		attestation *AttestationResult
		wantMFA     bool
	}{
		{
			name:        "a proven hardware login renews as multi-factor",
			amr:         []string{"webauthn", "hwk"},
			acr:         ACRHardwareAttested,
			attestation: testHardwareAttestation("cb69481e8ff7403993ec0a2729a154a8"),
			wantMFA:     true,
		},
		{
			name:        "an unattested PIV login never renews into multi-factor",
			amr:         []string{"mtls"},
			attestation: UnattestedPIVCredential(),
			wantMFA:     false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, storage := newSessionRefreshProvider(t, 15*time.Minute, 7*24*time.Hour)
			ctx := context.Background()
			user := seedCeremonyUser(t, storage, "acme", "renewing-user", []string{"admin"})

			set, err := provider.tokenService.IssueHardwareTokens(ctx, HardwareTokenOptions{
				User:        user,
				AMR:         tc.amr,
				ACR:         tc.acr,
				Attestation: tc.attestation,
			})
			if err != nil {
				t.Fatalf("IssueHardwareTokens: %v", err)
			}

			tokens, err := provider.RefreshSession(ctx, SessionRefreshRequest{RefreshToken: set.RefreshToken})
			if err != nil {
				t.Fatalf("RefreshSession: %v", err)
			}

			claims, err := xkms.JWTVerifyWithKeyAndClaims(tokens.AccessToken, provider.config.SigningKey.Public(), "RS256")
			if err != nil {
				t.Fatalf("verify renewed access token: %v", err)
			}
			if got := claims["mfa_verified"]; got != tc.wantMFA {
				t.Errorf("renewed access token mfa_verified = %v, want %v", got, tc.wantMFA)
			}
		})
	}
}
