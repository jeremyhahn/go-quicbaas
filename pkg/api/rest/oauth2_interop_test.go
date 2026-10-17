//go:build rest

package rest

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// The tests in this file drive the real REST OAuth 2.0 / OIDC routes the way
// a third-party relying party would: golang.org/x/oauth2 for the grants,
// go-jose for ID token verification and plain net/http for everything else.
// Nothing here goes through the QuicBaaS SDK. The server side is the
// in-memory harness newInteropFixture (oidc_interop_test.go).

// interopIDTokenAlgorithms is every JWS algorithm a relying party accepts
// from the provider; "none" and HMAC are deliberately absent.
var interopIDTokenAlgorithms = []jose.SignatureAlgorithm{
	jose.RS256, jose.RS384, jose.RS512,
	jose.PS256, jose.PS384, jose.PS512,
	jose.ES256, jose.ES384, jose.ES512,
	jose.EdDSA,
}

// getInteropJSON fetches url with plain net/http and decodes its JSON body.
func getInteropJSON(t *testing.T, f *interopFixture, target string, out interface{}) {
	t.Helper()

	resp, err := f.client.Get(target)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s", target)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}

// verifyIDTokenWithGoJose verifies rawIDToken's signature against the
// published JWKS and its iss, aud and exp claims per OIDC Core §3.1.3.7.
func verifyIDTokenWithGoJose(t *testing.T, f *interopFixture, rawIDToken string) jwt.Claims {
	t.Helper()

	var keySet jose.JSONWebKeySet
	getInteropJSON(t, f, f.discovery.JWKSURI, &keySet)
	require.NotEmpty(t, keySet.Keys, "published JWKS is empty")

	parsed, err := jwt.ParseSigned(rawIDToken, interopIDTokenAlgorithms)
	require.NoError(t, err, "go-jose cannot parse the ID token")
	require.Len(t, parsed.Headers, 1)
	require.NotEmpty(t, parsed.Headers[0].KeyID, "ID token header carries no kid")
	keys := keySet.Key(parsed.Headers[0].KeyID)
	require.Len(t, keys, 1, "kid %q is not in the published JWKS", parsed.Headers[0].KeyID)

	var claims jwt.Claims
	require.NoError(t, parsed.Claims(keys[0].Public(), &claims), "ID token signature does not verify")
	require.NoError(t, claims.ValidateWithLeeway(jwt.Expected{
		Issuer:      f.issuer,
		AnyAudience: jwt.Audience{interopClientID},
		Time:        time.Now(),
	}, 0), "ID token claims")
	require.NotNil(t, claims.Expiry, "ID token carries no exp")
	assert.NotEmpty(t, claims.Subject)
	return claims
}

// revokeInterop revokes token at the discovered revocation endpoint with
// HTTP Basic client authentication (RFC 7009 §2.1). It returns the HTTP
// status and the OAuth error code, if any.
func revokeInterop(t *testing.T, f *interopFixture, endpoint, token string) (int, string) {
	t.Helper()

	form := url.Values{"token": {token}}
	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(interopClientID, interopSecret)

	resp, err := f.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return resp.StatusCode, ""
	}
	var body struct {
		Error string `json:"error"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return resp.StatusCode, body.Error
}

func TestOAuth2Interop_Discovery(t *testing.T) {
	f := newInteropFixture(t)

	var doc map[string]interface{}
	getInteropJSON(t, f, f.issuer+"/.well-known/openid-configuration", &doc)

	assert.Equal(t, f.issuer, doc["issuer"], "OIDC Discovery §4.3: issuer must equal the URL prefix")
	for _, field := range []string{"authorization_endpoint", "token_endpoint", "jwks_uri", "introspection_endpoint", "revocation_endpoint"} {
		value, ok := doc[field].(string)
		require.True(t, ok, "discovery document has no %s", field)
		assert.True(t, strings.HasPrefix(value, f.issuer), "%s %q is not under the issuer", field, value)
	}
	methods, ok := doc["code_challenge_methods_supported"].([]interface{})
	require.True(t, ok, "discovery document has no code_challenge_methods_supported")
	assert.Contains(t, methods, "S256")
}

func TestOAuth2Interop_AuthorizationCodePKCEAndRefresh(t *testing.T) {
	f := newInteropFixture(t)
	rp := f.relyingParty()

	// interop defect: RFC 6749 §3.1 and OIDC Core §3.1.2.3 require the
	// authorization server to authenticate the end user before it issues a
	// code. GET /oauth2/authorize (iam_oidc.go Authorize) never does: it
	// redirects with a code that is bound to no user, and the token endpoint
	// then fails the exchange with server_error "failed to get user".
	unboundVerifier := oauth2.GenerateVerifier()
	unboundCode := f.authorize(t, rp, unboundVerifier)
	_, err := rp.Exchange(f.context(), unboundCode, oauth2.VerifierOption(unboundVerifier))
	var retrieveErr *oauth2.RetrieveError
	require.ErrorAs(t, err, &retrieveErr)
	assert.Equal(t, "server_error", retrieveErr.ErrorCode)

	// Until the authorize endpoint authenticates the user, the code is minted
	// for an authenticated user through the provider, as a login front end
	// would; the client takes it from there.
	verifier := oauth2.GenerateVerifier()
	code := f.authorizeAsUser(t, verifier)
	token, err := rp.Exchange(f.context(), code, oauth2.VerifierOption(verifier))
	require.NoError(t, err, "x/oauth2 code exchange")
	require.NotEmpty(t, token.AccessToken)

	rawIDToken, ok := token.Extra("id_token").(string)
	require.True(t, ok, "token response carries no id_token for the openid scope")
	verifyIDTokenWithGoJose(t, f, rawIDToken)

	require.NotEmpty(t, token.RefreshToken, "token response carries no refresh_token")
	expired := *token
	expired.Expiry = time.Now().Add(-time.Minute)
	refreshed, err := rp.TokenSource(f.context(), &expired).Token()
	require.NoError(t, err, "x/oauth2 refresh grant")
	assert.NotEmpty(t, refreshed.AccessToken)
	assert.NotEqual(t, token.AccessToken, refreshed.AccessToken, "refresh must mint a new access token")
	assert.True(t, refreshed.Expiry.After(time.Now()))
	assert.Equal(t, true, f.introspect(t, refreshed.AccessToken)["active"])
}

func TestOAuth2Interop_WrongVerifierIsRefused(t *testing.T) {
	f := newInteropFixture(t)
	rp := f.relyingParty()

	code := f.authorize(t, rp, oauth2.GenerateVerifier())
	_, err := rp.Exchange(f.context(), code, oauth2.VerifierOption(oauth2.GenerateVerifier()))
	var retrieveErr *oauth2.RetrieveError
	require.ErrorAs(t, err, &retrieveErr, "RFC 7636 §4.6: a mismatched verifier must be refused")
	assert.Equal(t, "invalid_grant", retrieveErr.ErrorCode)
}

func TestOAuth2Interop_ClientCredentialsIntrospectAndRevoke(t *testing.T) {
	f := newInteropFixture(t)

	var doc struct {
		RevocationEndpoint string `json:"revocation_endpoint"`
	}
	getInteropJSON(t, f, f.issuer+"/.well-known/openid-configuration", &doc)
	require.NotEmpty(t, doc.RevocationEndpoint)

	// AuthStyle is left at AuthStyleAutoDetect, x/oauth2's default, which
	// tries HTTP Basic first (RFC 6749 §2.3.1).
	cc := &clientcredentials.Config{
		ClientID:     interopClientID,
		ClientSecret: interopSecret,
		TokenURL:     f.discovery.TokenEndpoint,
	}
	token, err := cc.Token(f.context())
	require.NoError(t, err, "x/oauth2 client credentials grant")
	require.NotEmpty(t, token.AccessToken)

	assert.Equal(t, true, f.introspect(t, token.AccessToken)["active"], "RFC 7662 §2.2: live token")

	// RFC 7009 §2.1: the confidential client authenticates to the
	// revocation endpoint per RFC 6749 §2.3.1 (HTTP Basic). RFC 7009 §2.2:
	// a successful revocation answers HTTP 200 with no error, and the token
	// then introspects as inactive (RFC 7662 §2.2).
	status, errorCode := revokeInterop(t, f, doc.RevocationEndpoint, token.AccessToken)
	assert.Equal(t, http.StatusOK, status, "RFC 7009 §2.2: revocation succeeds")
	assert.Empty(t, errorCode)
	assert.Equal(t, false, f.introspect(t, token.AccessToken)["active"],
		"RFC 7662 §2.2: a revoked token is inactive")

	// RFC 7662 §2.2: an invalid token introspects as inactive.
	assert.Equal(t, false, f.introspect(t, token.AccessToken+"tampered")["active"])
}

func TestOAuth2Interop_BadClientSecretIsRefused(t *testing.T) {
	f := newInteropFixture(t)

	cc := &clientcredentials.Config{
		ClientID:     interopClientID,
		ClientSecret: interopSecret + "-wrong",
		TokenURL:     f.discovery.TokenEndpoint,
		AuthStyle:    oauth2.AuthStyleInHeader,
	}
	_, err := cc.Token(f.context())
	var retrieveErr *oauth2.RetrieveError
	require.ErrorAs(t, err, &retrieveErr, "RFC 6749 §5.2: a bad client secret must be refused")
	assert.Equal(t, "invalid_client", retrieveErr.ErrorCode)
}
