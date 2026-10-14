package iam

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// introspectionAttestationAAGUID is the authenticator model the hardware
// tests attest; any non-empty hex AAGUID serves.
const introspectionAttestationAAGUID = "cb69481e8ff7403993ec0a2729a154a8"

// issueIntrospectionTestToken issues an access token for the liveness test
// user through the production hardware-token path.
func issueIntrospectionTestToken(t *testing.T, provider *OIDCProvider, opts HardwareTokenOptions) string {
	t.Helper()
	opts.User = &User{
		ID:       liveUserID,
		TenantID: "tenant-a",
		Email:    fmt.Sprintf("%d@tenant-a.example.com", liveUserID),
		Name:     fmt.Sprintf("Liveness %d", liveUserID),
	}
	set, err := provider.IssueHardwareTokens(context.Background(), opts)
	if err != nil {
		t.Fatalf("IssueHardwareTokens: %v", err)
	}
	return set.AccessToken
}

// TestOIDCIntrospect_ReportsTheAttestationTheTokenCarries proves an
// introspecting resource server sees the same attestation a JWT-parsing one
// reads from the bearer (go-quicbaas#393).
func TestOIDCIntrospect_ReportsTheAttestationTheTokenCarries(t *testing.T) {
	provider, privateKey, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", liveUserID, LifecycleActive)

	token := issueIntrospectionTestToken(t, provider, HardwareTokenOptions{
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation(introspectionAttestationAAGUID),
	})
	claims, err := xkms.JWTVerifyWithKeyAndClaims(token, &privateKey.PublicKey, "RS256")
	if err != nil {
		t.Fatalf("verify access token: %v", err)
	}

	resp, err := provider.Introspect(context.Background(), &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("expected active=true for a live attested token")
	}
	if resp.AttestLevel != string(AttestLevelHardware) || resp.AttestLevel != claims["attest_level"] {
		t.Errorf("attest_level = %q, token carries %v", resp.AttestLevel, claims["attest_level"])
	}
	if resp.AttestAAGUID != introspectionAttestationAAGUID || resp.AttestAAGUID != claims["attest_aaguid"] {
		t.Errorf("attest_aaguid = %q, token carries %v", resp.AttestAAGUID, claims["attest_aaguid"])
	}
}

// TestOIDCIntrospect_OmitsAnAbsentAttestation proves a token issued without
// proven attestation is introspected without either claim: absence must read
// as "no evidence", never as hardware.
func TestOIDCIntrospect_OmitsAnAbsentAttestation(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", liveUserID, LifecycleActive)

	token := issueIntrospectionTestToken(t, provider, HardwareTokenOptions{AMR: []string{"webauthn"}})

	resp, err := provider.Introspect(context.Background(), &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	if !resp.Active {
		t.Fatalf("expected active=true for a live token")
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal introspection response: %v", err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil {
		t.Fatalf("unmarshal introspection response: %v", err)
	}
	for _, name := range []string{"attest_level", "attest_aaguid"} {
		if got, ok := fields[name]; ok {
			t.Errorf("introspection asserts %s %v without evidence", name, got)
		}
	}
}

// TestOIDCIntrospect_InactiveTokenRevealsNoAttestation proves an attested
// token that is no longer live introspects as exactly {"active":false}.
func TestOIDCIntrospect_InactiveTokenRevealsNoAttestation(t *testing.T) {
	provider, _, _, _, _ := createTestOIDCProvider(t)
	seedLivenessUser(t, provider, "tenant-a", liveUserID, LifecycleDisabled)

	token := issueIntrospectionTestToken(t, provider, HardwareTokenOptions{
		AMR:         []string{"webauthn", "hwk"},
		ACR:         ACRHardwareAttested,
		Attestation: testHardwareAttestation(introspectionAttestationAAGUID),
	})

	resp, err := provider.Introspect(context.Background(), &IntrospectRequest{Token: token})
	if err != nil {
		t.Fatalf("Introspect() error = %v", err)
	}
	body, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal introspection response: %v", err)
	}
	if string(body) != `{"active":false}` {
		t.Errorf("inactive introspection = %s, want {\"active\":false}", body)
	}
}
