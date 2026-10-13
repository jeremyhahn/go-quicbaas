//go:build rest

package rest

// OIDC/OAuth2 interop leg (go-quicbaas#203).
//
// Every other OAuth2 test in this package drives the handlers with requests
// this repository built. This leg stands the real router up on a loopback
// listener and drives it with implementations we did not write:
// golang.org/x/oauth2 as the relying party, and lestrrat-go/jwx as the JOSE
// verifier (the server signs with go-jose, so a shared bug in one JOSE
// library cannot hide here). A wire defect that our own client tolerates, an
// external client rejects.

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/jeremyhahn/go-xkms/sdk/go/secure"
	"github.com/lestrrat-go/jwx/v2/jwa"
	"github.com/lestrrat-go/jwx/v2/jwk"
	"github.com/lestrrat-go/jwx/v2/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

const (
	interopTenant      = "tenant-interop"
	interopClientID    = "app_tenant-interop_rp"
	interopSecret      = "interop-relying-party-secret"
	interopRedirectURI = "https://rp.example/callback"
)

// interopDiscovery is the subset of the OIDC discovery document the leg
// drives. Each field must be present for the leg to mean anything.
type interopDiscovery struct {
	Issuer                string   `json:"issuer"`
	AuthorizationEndpoint string   `json:"authorization_endpoint"`
	TokenEndpoint         string   `json:"token_endpoint"`
	JWKSURI               string   `json:"jwks_uri"`
	IntrospectionEndpoint string   `json:"introspection_endpoint"`
	CodeChallengeMethods  []string `json:"code_challenge_methods_supported"`
}

type interopFixture struct {
	issuer    string
	client    *http.Client
	discovery interopDiscovery
	provider  *iam.OIDCProvider
	user      *iam.User
}

// newInteropFixture serves the full REST router on a loopback listener whose
// URL is the provider's issuer, so every endpoint the discovery document
// advertises is dialable by an external client.
func newInteropFixture(t *testing.T) *interopFixture {
	t.Helper()

	ts := httptest.NewUnstartedServer(nil)
	issuer := "http://" + ts.Listener.Addr().String()

	// The xkms-backed key manager is the signing path the server ships
	// (pkg/server/iam_init.go); it stamps the kid an external verifier
	// selects the JWKS key by.
	initInteropXKMS(t)
	signingKeys, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, "")
	require.NoError(t, err)

	backend := iam.NewBackendStorage(newIntrospectMemKV())
	identityStore := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: iam.NewMemoryStorage(),
		RoleStorage: iam.NewMemoryStorage(),
	})
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            issuer,
		SigningKeyManager: signingKeys,
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        15 * time.Minute,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: identityStore,
		AppStorage:    backend,
		TokenStorage:  backend,
	})
	require.NoError(t, err)

	secret, err := secure.NewSecret([]byte(interopSecret))
	require.NoError(t, err)
	hashed, err := iam.HashClientSecret(secret)
	secret.Destroy()
	require.NoError(t, err)
	require.NoError(t, backend.CreateApp(context.Background(), &iam.Application{
		ClientID:      interopClientID,
		ClientSecret:  hashed,
		TenantID:      interopTenant,
		RedirectURIs:  []string{interopRedirectURI},
		GrantTypes:    []string{iam.GrantTypeAuthorizationCode, iam.GrantTypeClientCredentials},
		ResponseTypes: []string{"code"},
		Scopes:        iam.DefaultFleetRuntimeScopes(),
	}))

	user, err := identityStore.CreateUser(context.Background(), interopTenant, iam.CreateUserOptions{
		Username:  "interop",
		Email:     "interop@" + interopTenant + ".example.com",
		Name:      "Interop User",
		Lifecycle: iam.LifecycleActive,
	})
	require.NoError(t, err)

	auditLogger, err := iam.NewAuditLogger()
	require.NoError(t, err)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mockSvc := &introspectMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		provider:     provider,
		auditLogger:  auditLogger,
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc

	ts.Config.Handler = server.router
	ts.Start()
	t.Cleanup(func() {
		ts.Close()
		server.Close()
		require.NoError(t, auditLogger.Close())
	})

	f := &interopFixture{issuer: issuer, client: ts.Client(), provider: provider, user: user}
	f.discovery = fetchInteropDiscovery(t, f)
	return f
}

// initInteropXKMS initializes the process-wide xkms service over in-memory
// backends, as the server wires it, and resets it when the test ends. The
// leg's tests are not parallel, so the global never outlives them.
func initInteropXKMS(t *testing.T) {
	t.Helper()

	xkms.Reset()
	t.Cleanup(xkms.Reset)

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	keyStorage, err := xkmssdk.NewMemoryBackend()
	require.NoError(t, err)
	software, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keyStorage})
	require.NoError(t, err)
	backend, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
		Backend:     software,
		CertStorage: xkmssdk.NewStorageMemory(),
	})
	require.NoError(t, err)
	keyVersions, err := xkmssdk.NewKeyVersionStore(xkmssdk.NewStorageMemory())
	require.NoError(t, err)

	const backendName = "software"
	require.NoError(t, xkms.Initialize(&xkms.Config{
		DefaultBackend: backendName,
		Backends:       map[string]xkmssdk.ServiceBackend{backendName: backend},
		KeyVersions:    keyVersions,
		Logger:         logger.NewAdapter(log),
	}))
}

// fetchInteropDiscovery is the leg's baseline. If the discovery document
// does not parse, or lacks an endpoint the leg drives, every later step
// would test nothing, so the leg stops here with the reason.
func fetchInteropDiscovery(t *testing.T, f *interopFixture) interopDiscovery {
	t.Helper()

	resp, err := f.client.Get(f.issuer + "/.well-known/openid-configuration")
	require.NoError(t, err, "baseline: discovery document is not reachable")
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "baseline: discovery document status")

	var doc interopDiscovery
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&doc), "baseline: discovery document does not parse as JSON")
	require.Equal(t, f.issuer, doc.Issuer, "baseline: discovery issuer")
	for name, endpoint := range map[string]string{
		"authorization_endpoint": doc.AuthorizationEndpoint,
		"token_endpoint":         doc.TokenEndpoint,
		"jwks_uri":               doc.JWKSURI,
		"introspection_endpoint": doc.IntrospectionEndpoint,
	} {
		require.Truef(t, strings.HasPrefix(endpoint, f.issuer+"/"),
			"baseline: discovery %s %q is not served by the issuer %q", name, endpoint, f.issuer)
	}
	require.Contains(t, doc.CodeChallengeMethods, "S256", "baseline: discovery does not advertise PKCE S256")
	return doc
}

// context returns a context that makes x/oauth2 dial through the fixture's
// client.
func (f *interopFixture) context() context.Context {
	return context.WithValue(context.Background(), oauth2.HTTPClient, f.client)
}

// relyingParty is the x/oauth2 configuration an external RP would build from
// the discovery document alone.
func (f *interopFixture) relyingParty() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     interopClientID,
		ClientSecret: interopSecret,
		RedirectURL:  interopRedirectURI,
		Scopes:       []string{"openid"},
		Endpoint: oauth2.Endpoint{
			AuthURL:  f.discovery.AuthorizationEndpoint,
			TokenURL: f.discovery.TokenEndpoint,
			// Pinned, not auto-detected: on a refusal auto-detect retries
			// with the other style, and the retry reports the already
			// consumed one-time code instead of the first error.
			AuthStyle: oauth2.AuthStyleInHeader,
		},
	}
}

// authorize follows the RP's authorization URL and returns the code from the
// redirect, checking that state round-trips.
func (f *interopFixture) authorize(t *testing.T, rp *oauth2.Config, verifier string) string {
	t.Helper()

	const state = "interop-state"
	authURL := rp.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("nonce", "interop-nonce"))

	noFollow := *f.client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Get(authURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusFound, resp.StatusCode, "authorize must redirect to the RP")

	location, err := url.Parse(resp.Header.Get("Location"))
	require.NoError(t, err)
	require.Equal(t, state, location.Query().Get("state"), "authorize must return the RP's state")
	code := location.Query().Get("code")
	require.NotEmpty(t, code, "authorize redirect carries no code")
	return code
}

// authorizeAsUser issues an authorization code for the fixture's user
// through the provider's Authorize, with the same parameters the RP's
// authorization URL carries. It stands in for the user-authentication step
// GET /oauth2/authorize lacks: that handler never sets the resource owner,
// so a code it issues names no user and its exchange fails with
// server_error "failed to get user". The exchange, ID token and
// introspection that follow all cross the wire.
func (f *interopFixture) authorizeAsUser(t *testing.T, verifier string) string {
	t.Helper()

	resp, err := f.provider.Authorize(context.Background(), &iam.AuthorizeRequest{
		ClientID:            interopClientID,
		RedirectURI:         interopRedirectURI,
		ResponseType:        "code",
		Scope:               []string{"openid"},
		CodeChallenge:       oauth2.S256ChallengeFromVerifier(verifier),
		CodeChallengeMethod: iam.CodeChallengeMethodS256,
		Nonce:               "interop-nonce",
		UserID:              f.user.ID,
		TenantID:            interopTenant,
	})
	require.NoError(t, err)
	return resp.Code
}

// introspect POSTs a plain RFC 7662 form, authenticated as the client.
func (f *interopFixture) introspect(t *testing.T, token string) map[string]interface{} {
	t.Helper()

	form := url.Values{"token": {token}}
	req, err := http.NewRequest(http.MethodPost, f.discovery.IntrospectionEndpoint, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(interopClientID, interopSecret)

	resp, err := f.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "introspection status")

	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body
}

// verifyWithServedJWKS parses a JWT with jwx against the JWKS the server
// publishes, enforcing signature, iss, aud and exp.
func (f *interopFixture) verifyWithServedJWKS(t *testing.T, raw string) (jwt.Token, error) {
	t.Helper()

	set, err := jwk.Fetch(context.Background(), f.discovery.JWKSURI, jwk.WithHTTPClient(f.client))
	require.NoError(t, err, "served JWKS does not parse as a JWK set")
	return jwt.ParseString(raw,
		jwt.WithKeySet(set),
		jwt.WithIssuer(f.issuer),
		jwt.WithAudience(interopClientID),
		jwt.WithValidate(true))
}

func TestOIDCInterop_AuthorizationCodeWithPKCE(t *testing.T) {
	f := newInteropFixture(t)
	rp := f.relyingParty()

	verifier := oauth2.GenerateVerifier()
	code := f.authorizeAsUser(t, verifier)

	token, err := rp.Exchange(f.context(), code, oauth2.VerifierOption(verifier))
	require.NoError(t, err, "x/oauth2 code exchange")
	require.NotEmpty(t, token.AccessToken)

	rawIDToken, ok := token.Extra("id_token").(string)
	require.True(t, ok, "token response carries no id_token for the openid scope")
	idToken, err := f.verifyWithServedJWKS(t, rawIDToken)
	require.NoError(t, err, "jwx rejects the ID token")
	assert.True(t, idToken.Expiration().After(time.Now()))
	assert.NotEmpty(t, idToken.Subject())
	nonce, ok := idToken.Get("nonce")
	require.True(t, ok, "ID token carries no nonce")
	assert.Equal(t, "interop-nonce", nonce)

	body := f.introspect(t, token.AccessToken)
	assert.Equal(t, true, body["active"])
}

func TestOIDCInterop_AuthorizeRedirectsWithCodeAndState(t *testing.T) {
	f := newInteropFixture(t)

	code := f.authorize(t, f.relyingParty(), oauth2.GenerateVerifier())
	assert.NotEmpty(t, code)
}

func TestOIDCInterop_WrongPKCEVerifierIsRefused(t *testing.T) {
	f := newInteropFixture(t)
	rp := f.relyingParty()

	// A user-bound code, so the exchange would succeed but for the
	// verifier: the PKCE comparison alone decides this refusal.
	code := f.authorizeAsUser(t, oauth2.GenerateVerifier())

	_, err := rp.Exchange(f.context(), code, oauth2.VerifierOption(oauth2.GenerateVerifier()))
	var retrieveErr *oauth2.RetrieveError
	require.True(t, errors.As(err, &retrieveErr), "want a token endpoint refusal, got %v", err)
	assert.Equal(t, "invalid_grant", retrieveErr.ErrorCode)
}

func TestOIDCInterop_ClientCredentialsAndIntrospection(t *testing.T) {
	f := newInteropFixture(t)

	cc := &clientcredentials.Config{
		ClientID:     interopClientID,
		ClientSecret: interopSecret,
		TokenURL:     f.discovery.TokenEndpoint,
	}
	token, err := cc.Token(f.context())
	require.NoError(t, err, "x/oauth2 client credentials grant")
	require.NotEmpty(t, token.AccessToken)
	assert.Equal(t, "bearer", strings.ToLower(token.TokenType))

	body := f.introspect(t, token.AccessToken)
	assert.Equal(t, true, body["active"])
	assert.Equal(t, interopClientID, body["client_id"])

	inactive := f.introspect(t, token.AccessToken+"tampered")
	assert.Equal(t, false, inactive["active"])
}

func TestOIDCInterop_ForeignKeySignatureFailsVerification(t *testing.T) {
	f := newInteropFixture(t)

	set, err := jwk.Fetch(context.Background(), f.discovery.JWKSURI, jwk.WithHTTPClient(f.client))
	require.NoError(t, err)
	served, ok := set.Key(0)
	require.True(t, ok, "served JWKS is empty")

	require.Equal(t, jwa.ES256.String(), served.Algorithm().String(), "served signing algorithm")

	// Forge a token that claims the served key id and algorithm but is
	// signed by a key the server never held.
	foreignEC, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	require.NoError(t, err)
	foreign, err := jwk.FromRaw(foreignEC)
	require.NoError(t, err)
	require.NoError(t, foreign.Set(jwk.KeyIDKey, served.KeyID()))

	forged, err := jwt.NewBuilder().
		Issuer(f.issuer).
		Audience([]string{interopClientID}).
		Subject("forged").
		Expiration(time.Now().Add(time.Hour)).
		Build()
	require.NoError(t, err)
	signed, err := jwt.Sign(forged, jwt.WithKey(jwa.ES256, foreign))
	require.NoError(t, err)

	_, err = f.verifyWithServedJWKS(t, string(signed))
	require.Error(t, err, "a token signed by a foreign key must not verify")
	assert.ErrorContains(t, err, "could not verify")
}
