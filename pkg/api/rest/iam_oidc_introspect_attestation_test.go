//go:build rest

package rest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// mintIntrospectUserToken seeds an active user in the fixture's tenant and
// issues it an access token carrying the given attestation (nil: none).
func mintIntrospectUserToken(t *testing.T, f *introspectFixture, attestation *iam.AttestationResult) string {
	t.Helper()
	ctx := context.Background()
	user, err := f.identityStore.CreateUser(ctx, introspectTenant, iam.CreateUserOptions{
		Username:  "attested",
		Email:     "attested@" + introspectTenant + ".example.com",
		Name:      "Attested User",
		Lifecycle: iam.LifecycleActive,
	})
	require.NoError(t, err)
	set, err := f.provider.IssueHardwareTokens(ctx, iam.HardwareTokenOptions{
		User:        user,
		AMR:         []string{"smartcard"},
		Attestation: attestation,
	})
	require.NoError(t, err)
	return set.AccessToken
}

// introspectBody posts token to the RFC 7662 endpoint and decodes the reply.
func introspectBody(t *testing.T, f *introspectFixture, token string) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	f.handler.Introspect(w, introspectRequest(token, true))
	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]interface{}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	return body
}

// TestIntrospect_AttestationSurvivesTheWire proves the attestation an access
// token carries reaches a resource server that introspects it over REST,
// with the same json names and values the JWT carries (go-quicbaas#393).
// Only an unattested PIV result can be minted outside pkg/iam, so the level
// is "software"; pkg/iam covers the hardware level and AAGUID.
func TestIntrospect_AttestationSurvivesTheWire(t *testing.T) {
	f := newIntrospectFixture(t)
	defer f.cleanup()

	token := mintIntrospectUserToken(t, f, iam.UnattestedPIVCredential())
	claims := unverifiedJWTClaims(t, token)
	require.Equal(t, string(iam.AttestLevelSoftware), claims["attest_level"])

	body := introspectBody(t, f, token)
	assert.Equal(t, true, body["active"])
	assert.Equal(t, claims["attest_level"], body["attest_level"])
	assert.NotContains(t, claims, "attest_aaguid")
	assert.NotContains(t, body, "attest_aaguid")
}

// TestIntrospect_UnattestedTokenOmitsAttestation proves a token with no
// attestation introspects without the fields: absence never reads as a level.
func TestIntrospect_UnattestedTokenOmitsAttestation(t *testing.T) {
	f := newIntrospectFixture(t)
	defer f.cleanup()

	body := introspectBody(t, f, mintIntrospectUserToken(t, f, nil))
	assert.Equal(t, true, body["active"])
	assert.NotContains(t, body, "attest_level")
	assert.NotContains(t, body, "attest_aaguid")
}

// TestIntrospect_InactiveTokenIsOnlyActiveFalse proves an inactive reply
// carries nothing but active=false (RFC 7662 §2.2), attestation included.
func TestIntrospect_InactiveTokenIsOnlyActiveFalse(t *testing.T) {
	f := newIntrospectFixture(t)
	defer f.cleanup()

	token := mintIntrospectUserToken(t, f, iam.UnattestedPIVCredential())
	body := introspectBody(t, f, token+"tampered")
	assert.Equal(t, map[string]interface{}{"active": false}, body)
}
