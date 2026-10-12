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
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// fakeTenantAuthzModeResolver is a controllable tenantAuthzModeResolver for the
// per-tenant mode-resolution tests.
type fakeTenantAuthzModeResolver struct {
	mode string
	err  error
}

func (f *fakeTenantAuthzModeResolver) TenantAuthzMode(_ context.Context, _ string) (string, error) {
	return f.mode, f.err
}

func TestNewTenantModeResolver_PerTenantOverride(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{mode: "online"},
		platformtoken.AuthzModeHybrid, // deployment default differs from override
	)
	assert.Equal(t, platformtoken.AuthzModeOnline, resolver("tenant-x"),
		"per-tenant override must win over the deployment default")
}

func TestNewTenantModeResolver_FallsBackWhenUnset(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{mode: ""}, // no override
		platformtoken.AuthzModeOffline,
	)
	assert.Equal(t, platformtoken.AuthzModeOffline, resolver("tenant-x"),
		"empty override must fall back to the deployment default")
}

func TestNewTenantModeResolver_FailsSafeOnLookupError(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{err: errors.New("tenant store down")},
		platformtoken.AuthzModeOnline,
	)
	assert.Equal(t, platformtoken.AuthzModeOnline, resolver("tenant-x"),
		"a lookup error must fail SAFE to the deployment default, never panic")
}

func TestNewTenantModeResolver_UnknownOverrideFallsBack(t *testing.T) {
	resolver := newTenantModeResolver(
		&fakeTenantAuthzModeResolver{mode: "garbage-mode"},
		platformtoken.AuthzModeHybrid,
	)
	assert.Equal(t, platformtoken.AuthzModeHybrid, resolver("tenant-x"),
		"an unparseable per-tenant override must fall back to the deployment default")
}

func TestPlatformBearerAuthenticator_AcceptsPlatformTokenOnUnifiedCarrierTransports(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	issuer := newPlatformBearerTestIssuer(t, "platform-kid")
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       "https://issuer.example",
		Audience:     "https://quicbaas.example",
		JWKS:         issuer.jwks(),
		Clock:        func() time.Time { return now },
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	require.NoError(t, err)
	token := issuer.sign(t, platformBearerTestClaims{
		issuer:   "https://issuer.example",
		audience: []string{"https://quicbaas.example"},
		subject:  "agent-session",
		tenant:   "tenant-a",
		scopes:   []string{"kms:keys:get"},
		expires:  now.Add(10 * time.Minute),
		issued:   now,
	})
	authenticator := &platformBearerAuthenticator{validator: validator}

	for _, transport := range []string{"grpc", "mcp", "unix"} {
		t.Run(transport, func(t *testing.T) {
			identity, err := authenticator.authenticateAuthorization(context.Background(), "Bearer "+token)
			require.NoError(t, err)
			require.Equal(t, "agent-session", identity.Subject)
			require.Equal(t, "tenant-a", identity.TenantID)
			require.Equal(t, []string{"kms:keys:get"}, identity.ScopePermissions)
		})
	}
}

// A platform bearer's sub is a login name, never an account ID: a numeric sub
// projects as a username, and the account is the verified uid claim.
func TestPlatformBearerAuthenticator_AccountIsVerifiedUserID(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	issuer := newPlatformBearerTestIssuer(t, "platform-kid")
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       "https://issuer.example",
		Audience:     "https://quicbaas.example",
		JWKS:         issuer.jwks(),
		Clock:        func() time.Time { return now },
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	require.NoError(t, err)
	authenticator := &platformBearerAuthenticator{validator: validator}

	for name, tc := range map[string]struct {
		userID   string
		wantUser string
	}{
		"with uid":    {userID: "67890", wantUser: "67890"},
		"without uid": {},
	} {
		t.Run(name, func(t *testing.T) {
			token := issuer.sign(t, platformBearerTestClaims{
				issuer:   "https://issuer.example",
				audience: []string{"https://quicbaas.example"},
				subject:  "12345",
				userID:   tc.userID,
				tenant:   "tenant-a",
				expires:  now.Add(10 * time.Minute),
				issued:   now,
			})
			identity, err := authenticator.authenticateAuthorization(context.Background(), "Bearer "+token)
			require.NoError(t, err)
			subject := auth.SubjectFromIdentity(identity)
			require.Equal(t, "12345", subject.ID)
			require.Equal(t, interfaces.SubjectIDUsername, subject.IDKind)
			require.Equal(t, tc.wantUser, subject.UserID)
		})
	}
}

func TestPlatformBearerAuthenticator_RefusesInvalidAndTenantlessTokens(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	issuer := newPlatformBearerTestIssuer(t, "platform-kid")
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       "https://issuer.example",
		Audience:     "https://quicbaas.example",
		JWKS:         issuer.jwks(),
		Clock:        func() time.Time { return now },
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	require.NoError(t, err)
	authenticator := &platformBearerAuthenticator{validator: validator}
	tenantless := issuer.sign(t, platformBearerTestClaims{
		issuer:   "https://issuer.example",
		audience: []string{"https://quicbaas.example"},
		subject:  "agent-session",
		expires:  now.Add(10 * time.Minute),
		issued:   now,
	})

	for _, tc := range []struct {
		name    string
		header  string
		wantErr any
	}{
		{name: "invalid token", header: "Bearer not-a-jwt", wantErr: (*platformtoken.InvalidTokenError)(nil)},
		{name: "empty tenant", header: "Bearer " + tenantless, wantErr: (*PlatformBearerTenantRequiredError)(nil)},
		{name: "missing bearer", header: "Basic " + tenantless, wantErr: (*PlatformBearerInvalidHeaderError)(nil)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity, err := authenticator.authenticateAuthorization(context.Background(), tc.header)
			require.Nil(t, identity)
			switch tc.wantErr.(type) {
			case *platformtoken.InvalidTokenError:
				var target *platformtoken.InvalidTokenError
				require.ErrorAs(t, err, &target)
			case *PlatformBearerTenantRequiredError:
				var target *PlatformBearerTenantRequiredError
				require.ErrorAs(t, err, &target)
			case *PlatformBearerInvalidHeaderError:
				var target *PlatformBearerInvalidHeaderError
				require.ErrorAs(t, err, &target)
			default:
				t.Fatalf("unhandled expected error %T", tc.wantErr)
			}
		})
	}
}

func TestPlatformBearerAuthenticator_DeclinesX5TBearerWhenPeerCertificatePresent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	issuer := newPlatformBearerTestIssuer(t, "platform-kid")
	validator, err := platformtoken.NewValidator(platformtoken.Options{
		Issuer:       "https://issuer.example",
		Audience:     "https://quicbaas.example",
		JWKS:         issuer.jwks(),
		Clock:        func() time.Time { return now },
		ModeResolver: func(string) platformtoken.AuthzMode { return platformtoken.AuthzModeOffline },
	})
	require.NoError(t, err)
	authenticator := &platformBearerAuthenticator{validator: validator}
	boundCert := &x509.Certificate{Raw: []byte("platform bearer bound cert")}
	otherCert := &x509.Certificate{Raw: []byte("platform bearer other cert")}
	token := issuer.sign(t, platformBearerTestClaims{
		issuer:   "https://issuer.example",
		audience: []string{"https://quicbaas.example"},
		subject:  "agent-session",
		tenant:   "tenant-a",
		scopes:   []string{"qrdb:read"},
		x5tS256:  iam.CertThumbprintS256(boundCert),
		expires:  now.Add(10 * time.Minute),
		issued:   now,
	})

	identity, err := authenticator.authenticateAuthorization(platformBearerTLSContext(boundCert), "Bearer "+token)
	require.Nil(t, identity)
	require.NoError(t, err)

	identity, err = authenticator.authenticateAuthorization(platformBearerTLSContext(otherCert), "Bearer "+token)
	require.Nil(t, identity)
	require.NoError(t, err)

	identity, err = authenticator.authenticateAuthorization(context.Background(), "Bearer "+token)
	require.Nil(t, identity)
	var required *PlatformBearerCertificateRequiredError
	require.ErrorAs(t, err, &required)
}

func TestPlatformBearerAuthenticator_MissingAuthorizationWithPeerCertificateDoesNotOverwriteMTLSFailure(t *testing.T) {
	authenticator := &platformBearerAuthenticator{}
	ctx := platformBearerTLSContext(&x509.Certificate{Raw: []byte("mtls peer")})

	identity, err := authenticator.authenticateAuthorization(ctx, "")
	require.Nil(t, identity)
	require.NoError(t, err)
}

func platformBearerTLSContext(cert *x509.Certificate) context.Context {
	return qrdbsdk.WithTLSState(context.Background(), &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}})
}

type platformBearerTestIssuer struct {
	private *ecdsa.PrivateKey
	kid     string
}

type platformBearerTestClaims struct {
	issuer   string
	audience []string
	subject  string
	userID   string
	tenant   string
	scopes   []string
	x5tS256  string
	expires  time.Time
	issued   time.Time
}

func newPlatformBearerTestIssuer(t *testing.T, kid string) *platformBearerTestIssuer {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return &platformBearerTestIssuer{private: private, kid: kid}
}

func (i *platformBearerTestIssuer) jwks() *jose.JSONWebKeySet {
	return &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key:       i.private.Public(),
		KeyID:     i.kid,
		Algorithm: "ES256",
		Use:       "sig",
	}}}
}

func (i *platformBearerTestIssuer) sign(t *testing.T, claims platformBearerTestClaims) string {
	t.Helper()
	values := jwt.MapClaims{
		"iss": claims.issuer,
		"aud": claims.audience,
		"sub": claims.subject,
		"exp": claims.expires.Unix(),
		"iat": claims.issued.Unix(),
		"jti": "jti-" + claims.subject,
	}
	if claims.tenant != "" {
		values["tenant_id"] = claims.tenant
	}
	if claims.userID != "" {
		values["uid"] = claims.userID
	}
	if len(claims.scopes) > 0 {
		values["scp"] = claims.scopes
	}
	if claims.x5tS256 != "" {
		values["cnf"] = map[string]any{"x5t#S256": claims.x5tS256}
	}
	token := jwt.NewWithClaims(jwt.SigningMethodES256, values)
	token.Header["kid"] = i.kid
	token.Header["typ"] = "at+jwt"
	signed, err := token.SignedString(i.private)
	require.NoError(t, err)
	return signed
}
