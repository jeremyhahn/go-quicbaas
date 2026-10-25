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

package server

import (
	"context"
	"errors"
	"testing"

	qrdbauth "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// enrolledFactorClient is a UserAuthFactorClient that answers one factor
// lookup. Only GetAuthFactor is exercised; the rest satisfy the interface.
type enrolledFactorClient struct {
	qrdbtransport.UserAuthFactorClient

	wantFactorID string
	metadata     map[string]string
	err          error

	gets int
}

func (c *enrolledFactorClient) GetAuthFactor(
	_ context.Context, req *qrdbtransport.AuthFactorGetRequest,
) (*qrdbtransport.AuthFactorGetResponse, error) {
	c.gets++
	if c.err != nil {
		return nil, c.err
	}
	if req == nil || req.FactorID != c.wantFactorID {
		return nil, errors.New("no such factor")
	}
	return &qrdbtransport.AuthFactorGetResponse{
		Factor: qrdbtransport.AuthFactorRecord{ID: req.FactorID, Metadata: c.metadata},
	}, nil
}

// webauthnLoginIdentity is what qrdb's default identity hydrator hands the
// issuer after a WebAuthn login: the principal, the factor TYPE, and the factor
// ID. Note what it does NOT carry — the factor's own metadata. The node exposes
// no seam to replace that hydrator, which is why the issuer reads the record.
func webauthnLoginIdentity(factorID string) *qrdbauth.Identity {
	return &qrdbauth.Identity{
		Subject: "user-42",
		// The system tenant: every other tenant's login is refused before
		// issuance until go-quicbaas #1816 re-pins go-qrdb.
		TenantID: qrdbauth.SystemTenantID,
		Metadata: map[string]string{
			authMethodMetadataKey: "fido2",
			factorIDMetadataKey:   factorID,
		},
	}
}

// TestOIDCSessionIssuer_WebAuthnLoginReplaysEnrolledAttestation is the
// regression for #394.
//
// A WebAuthn REGISTRATION issued its session through IssueHardwareTokens and so
// carried attest_level; a later LOGIN with the same key went through this
// issuer and carried nothing. The evidence was in the factor record the whole
// time — AuthFactorMetadataAttestLevel, written at enrollment and, before this,
// read nowhere in the repository.
func TestOIDCSessionIssuer_WebAuthnLoginReplaysEnrolledAttestation(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, qrdbauth.SystemTenantID, "user-42", []string{"admin"})

	factors := &enrolledFactorClient{
		wantFactorID: "factor-1",
		metadata: map[string]string{
			iam.AuthFactorMetadataAttestLevel:  string(iam.AttestLevelHardware),
			iam.AuthFactorMetadataAttestAAGUID: "aaguid-abc",
		},
	}

	issuer := newOIDCSessionIssuer(nil)
	issuer.SetProvider(provider)
	issuer.SetAuthFactorClient(factors)

	ctx := context.Background()
	token, _, err := issuer.Issue(ctx, webauthnLoginIdentity("factor-1"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}

	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AttestLevel != string(iam.AttestLevelHardware) {
		t.Fatalf("attest_level = %q, want %q — a login did not replay what its factor proved at enrollment",
			claims.AttestLevel, iam.AttestLevelHardware)
	}
	if claims.AttestAAGUID != "aaguid-abc" {
		t.Fatalf("attest_aaguid = %q, want the enrolled authenticator's", claims.AttestAAGUID)
	}
	if factors.gets != 1 {
		t.Fatalf("the factor was read %d times, want exactly 1", factors.gets)
	}
}

// TestOIDCSessionIssuer_ReplaysWhateverWasEnrolled proves the issuer copies
// rather than decides. A factor enrolled as software must not come back as
// hardware, and an unattested one must carry nothing.
func TestOIDCSessionIssuer_ReplaysWhateverWasEnrolled(t *testing.T) {
	for _, tc := range []struct {
		name     string
		enrolled map[string]string
		want     string
	}{
		{
			name:     "hardware stays hardware",
			enrolled: map[string]string{iam.AuthFactorMetadataAttestLevel: string(iam.AttestLevelHardware)},
			want:     string(iam.AttestLevelHardware),
		},
		{
			name:     "none stays none",
			enrolled: map[string]string{iam.AuthFactorMetadataAttestLevel: string(iam.AttestLevelNone)},
			want:     string(iam.AttestLevelNone),
		},
		{
			name:     "a factor recording nothing claims nothing",
			enrolled: map[string]string{},
			want:     "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, users := newTestOIDCProviderWithUsers(t)
			seedSessionUser(t, users, qrdbauth.SystemTenantID, "user-42", []string{"admin"})

			issuer := newOIDCSessionIssuer(nil)
			issuer.SetProvider(provider)
			issuer.SetAuthFactorClient(&enrolledFactorClient{
				wantFactorID: "factor-1", metadata: tc.enrolled,
			})

			ctx := context.Background()
			token, _, err := issuer.Issue(ctx, webauthnLoginIdentity("factor-1"))
			if err != nil {
				t.Fatalf("Issue: %v", err)
			}
			claims, err := provider.ValidateAccessToken(ctx, token)
			if err != nil {
				t.Fatalf("ValidateAccessToken: %v", err)
			}
			if claims.AttestLevel != tc.want {
				t.Fatalf("attest_level = %q, want %q", claims.AttestLevel, tc.want)
			}
		})
	}
}

// TestOIDCSessionIssuer_AttestationFailuresUnderstateNeverOverstate covers every
// way the replay can come up empty. Each must issue a session that claims LESS
// than the credential proved, and none may fail the login: a transient
// factor-store failure must not become an outage for every hardware user, to
// protect a claim whose absence already denies.
func TestOIDCSessionIssuer_AttestationFailuresUnderstateNeverOverstate(t *testing.T) {
	for _, tc := range []struct {
		name    string
		factors qrdbtransport.UserAuthFactorClient
		id      *qrdbauth.Identity
		wire    bool
	}{
		{
			name: "no factor client wired",
			id:   webauthnLoginIdentity("factor-1"),
		},
		{
			name:    "lookup fails",
			factors: &enrolledFactorClient{wantFactorID: "factor-1", err: errors.New("factor store unavailable")},
			id:      webauthnLoginIdentity("factor-1"),
			wire:    true,
		},
		{
			name:    "identity carries no factor id",
			factors: &enrolledFactorClient{wantFactorID: "factor-1"},
			id: &qrdbauth.Identity{
				Subject: "user-42", TenantID: qrdbauth.SystemTenantID,
				Metadata: map[string]string{authMethodMetadataKey: "fido2"},
			},
			wire: true,
		},
		{
			name:    "identity carries no metadata at all",
			factors: &enrolledFactorClient{wantFactorID: "factor-1"},
			id:      &qrdbauth.Identity{Subject: "user-42", TenantID: qrdbauth.SystemTenantID},
			wire:    true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			provider, users := newTestOIDCProviderWithUsers(t)
			seedSessionUser(t, users, qrdbauth.SystemTenantID, "user-42", []string{"admin"})

			issuer := newOIDCSessionIssuer(nil)
			issuer.SetProvider(provider)
			if tc.wire {
				issuer.SetAuthFactorClient(tc.factors)
			}

			ctx := context.Background()
			token, _, err := issuer.Issue(ctx, tc.id)
			if err != nil {
				t.Fatalf("the login must still succeed: %v", err)
			}
			claims, err := provider.ValidateAccessToken(ctx, token)
			if err != nil {
				t.Fatalf("ValidateAccessToken: %v", err)
			}
			if claims.AttestLevel != "" || claims.AttestAAGUID != "" {
				t.Fatalf("a session claimed attestation it could not read: level=%q aaguid=%q",
					claims.AttestLevel, claims.AttestAAGUID)
			}
		})
	}
}

// TestOIDCSessionIssuer_SetAuthFactorClientNilDetaches pins that passing nil
// detaches cleanly rather than panicking on the next login.
func TestOIDCSessionIssuer_SetAuthFactorClientNilDetaches(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, qrdbauth.SystemTenantID, "user-42", []string{"admin"})

	issuer := newOIDCSessionIssuer(nil)
	issuer.SetProvider(provider)
	issuer.SetAuthFactorClient(&enrolledFactorClient{
		wantFactorID: "factor-1",
		metadata:     map[string]string{iam.AuthFactorMetadataAttestLevel: string(iam.AttestLevelHardware)},
	})
	issuer.SetAuthFactorClient(nil)

	ctx := context.Background()
	token, _, err := issuer.Issue(ctx, webauthnLoginIdentity("factor-1"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AttestLevel != "" {
		t.Fatalf("attest_level = %q, want empty after the client was detached", claims.AttestLevel)
	}
}

// boundReadKey marks a context the issuer's read binder returned.
type boundReadKey struct{}

// boundFactorClient answers GetAuthFactor only on a context the read binder
// bound, as an auth-enabled node answers only an authenticated read.
type boundFactorClient struct {
	qrdbtransport.UserAuthFactorClient
	metadata map[string]string
}

func (c *boundFactorClient) GetAuthFactor(ctx context.Context, _ *qrdbtransport.AuthFactorGetRequest) (*qrdbtransport.AuthFactorGetResponse, error) {
	if ctx.Value(boundReadKey{}) == nil {
		return nil, qrdbauth.ErrUnauthenticated
	}
	resp := &qrdbtransport.AuthFactorGetResponse{}
	resp.Factor.Metadata = c.metadata
	return resp, nil
}

// The factor read runs under the context the read binder returns for the
// login's tenant: the FinishLogin context carries no identity.
func TestOIDCSessionIssuer_FactorReadRunsUnderTheReadBinder(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, qrdbauth.SystemTenantID, "user-42", []string{"admin"})

	var boundTenant string
	issuer := newOIDCSessionIssuer(func(ctx context.Context, tenantID string) (context.Context, error) {
		boundTenant = tenantID
		return context.WithValue(ctx, boundReadKey{}, true), nil
	})
	issuer.SetProvider(provider)
	issuer.SetAuthFactorClient(&boundFactorClient{
		metadata: map[string]string{iam.AuthFactorMetadataAttestLevel: string(iam.AttestLevelHardware)},
	})

	ctx := context.Background()
	token, _, err := issuer.Issue(ctx, webauthnLoginIdentity("factor-1"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AttestLevel != string(iam.AttestLevelHardware) {
		t.Fatalf("attest_level = %q, want %q read under the bound context", claims.AttestLevel, iam.AttestLevelHardware)
	}
	if boundTenant != qrdbauth.SystemTenantID {
		t.Fatalf("the read was bound for tenant %q, want the login's", boundTenant)
	}
}

// A read binder that fails leaves the session without attestation: the
// issuer understates and never falls back to the unauthenticated read.
func TestOIDCSessionIssuer_AFailedReadBindUnderstates(t *testing.T) {
	provider, users := newTestOIDCProviderWithUsers(t)
	seedSessionUser(t, users, qrdbauth.SystemTenantID, "user-42", []string{"admin"})

	issuer := newOIDCSessionIssuer(func(context.Context, string) (context.Context, error) {
		return nil, errors.New("no service principal")
	})
	issuer.SetProvider(provider)
	factors := &enrolledFactorClient{
		wantFactorID: "factor-1",
		metadata:     map[string]string{iam.AuthFactorMetadataAttestLevel: string(iam.AttestLevelHardware)},
	}
	issuer.SetAuthFactorClient(factors)

	ctx := context.Background()
	token, _, err := issuer.Issue(ctx, webauthnLoginIdentity("factor-1"))
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	claims, err := provider.ValidateAccessToken(ctx, token)
	if err != nil {
		t.Fatalf("ValidateAccessToken: %v", err)
	}
	if claims.AttestLevel != "" {
		t.Fatalf("attest_level = %q, want empty when the read cannot be bound", claims.AttestLevel)
	}
	if factors.gets != 0 {
		t.Fatalf("the factor was read %d times on an unbound context, want 0", factors.gets)
	}
}
