//go:build !integration

// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC

package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	xkmslib "github.com/jeremyhahn/go-xkms/pkg/xkms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestXKMSPasskeyLogin_TokenVerifiesAgainstJWKS drives a real FIDO2 login
// through the go-xkms module as the node hosts it: a passkey registered with
// the module's own ceremony, then a login with it. The token the login
// answers is the node's platform token: its kid is in the published JWKS, it
// verifies against that key, and the platform authenticator accepts it.
func TestXKMSPasskeyLogin_TokenVerifiesAgainstJWKS(t *testing.T) {
	key, _ := newPasskeyAuthenticator(t)
	srv, components := newPasskeyJWKSNode(t)
	ctx := context.Background()
	const email = "passkey-user-test@example.com"

	hosted, err := srv.hostXKMSModule(newXKMSPasskeyLoginModule(t, srv))
	require.NoError(t, err)
	router := moduleRouter(hosted)

	// Register the passkey through the module's own ceremony.
	options, session := postPasskeyCeremony(t, router, "/api/v1/webauthn/registration/begin", "",
		map[string]any{"email": email})
	_, _ = postPasskeyCeremony(t, router, "/api/v1/webauthn/registration/finish", session,
		key.register(options))

	// Log in with it.
	options, session = postPasskeyCeremony(t, router, "/api/v1/webauthn/login/begin", "",
		map[string]any{"email": email})
	encoded, err := json.Marshal(options)
	require.NoError(t, err)
	var assertion map[string]any
	require.NoError(t, json.Unmarshal([]byte(key.assert(string(encoded))), &assertion))
	answer, _ := postPasskeyCeremony(t, router, "/api/v1/webauthn/login/finish", session, assertion)

	token, ok := answer["token"].(string)
	require.True(t, ok, "the login answers a token: %v", answer)
	assert.Equal(t, answer["access_token"], token, "token is the platform access token")
	body, err := json.Marshal(answer)
	require.NoError(t, err)

	// The kid is in the JWKS and the token verifies against it.
	assertPasskeyPlatformToken(t, components, string(body))
	claims := verifyAgainstJWKS(t, components.GetOIDCProvider(), token)
	assert.Equal(t, qrdbsdk.SystemTenantID, claims["tenant_id"])

	// The platform authenticator accepts it as a bearer, for the same user.
	authn, ok := newPlatformBearerAuthenticator(srv).(*platformBearerAuthenticator)
	require.True(t, ok)
	identity, err := authn.authenticateAuthorization(ctx, "Bearer "+token)
	require.NoError(t, err, "the platform authenticator accepts the passkey login token")
	require.NotNil(t, identity)
	assert.Equal(t, qrdbsdk.SystemTenantID, identity.TenantID)

	// The module's handle for the user names the IAM user the token is for.
	userID, ok := answer["user_id"].(string)
	require.True(t, ok && userID != "", "the login answers the user handle: %v", answer)
	handle, err := base64.RawURLEncoding.DecodeString(userID)
	if err != nil {
		handle, err = base64.StdEncoding.DecodeString(userID)
	}
	require.NoError(t, err)
	require.Len(t, handle, 8, "the handle is the 8-byte user id")
	user, err := components.GetIdentityStore().GetUser(ctx, qrdbsdk.SystemTenantID, binary.BigEndian.Uint64(handle))
	require.NoError(t, err, "the handle names an IAM user")
	assert.Equal(t, email, user.Email)
	assert.Equal(t, strconv.FormatUint(user.ID, 10), claims["sub"], "the token is for that user")
}

// newPasskeyJWKSNode builds the node newOperatorTLSIntegrationNode builds,
// with a WebAuthn relying party configured so IAM composes its passkey
// surface and publishes the factory the module's credential store uses. The
// enrollment assurance is the test environments' "any", which still holds
// administrators to hardware; the passkey user here is not one.
func newPasskeyJWKSNode(t *testing.T) (*Server, service.IAMComponents) {
	t.Helper()
	setupOIDCTestXKMS(t)
	srv := withTempServer(t)
	certFile, keyFile := writeOperatorPair(t, t.TempDir())
	srv.config.Security.TLSEnabled = true
	srv.config.Security.TLSClientAuth = false
	srv.config.Security.TLSCertFile = certFile
	srv.config.Security.TLSKeyFile = keyFile
	srv.config.Security.TLSCAFile = certFile
	srv.config.CA.KeysDir = filepath.Join(t.TempDir(), "ca-keys")
	srv.config.IAM.WebAuthn.RPID = passkeyRPID
	srv.config.IAM.WebAuthn.RPOrigins = []string{passkeyOrigin}
	srv.config.IAM.WebAuthn.RPDisplayName = "QuicBaaS Test"
	srv.config.IAM.WebAuthn.Assurance = "any"
	require.NoError(t, srv.initializeCA(), "initializeCA")
	wireCAAdapter(t, srv)

	require.NoError(t, srv.initStandaloneQRDBNode(), "qrdb node init")
	t.Cleanup(func() { assert.NoError(t, srv.stopQRDBNode(), "stopping the qrdb node") })
	srv.qrdbClient = newNodeBackedShardedClient(srv.qrdbNode)

	srv.config.IAM.SPIFFETrustDomain = testTrustDomain
	adminCertPath, _ := writeBootstrapPIVCert(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = adminCertPath
	components, err := srv.initializeIAMComponents()
	require.NoError(t, err, "first boot")
	require.NotNil(t, components.GetWebAuthnManager(), "IAM composes the WebAuthn surface")
	srv.iamComponents = components
	srv.quicBaaSService = &grpcOIDCGetterService{provider: components.GetOIDCProvider()}
	concrete, ok := components.(*iamComponents)
	require.True(t, ok)
	srv.bindXKMSModuleWebAuthnFactory(concrete)
	return srv, components
}

// newXKMSPasskeyLoginModule builds the go-xkms module with its passkey
// ceremony, configured as the node configures it.
func newXKMSPasskeyLoginModule(t *testing.T, srv *Server) qrdbsdk.ServiceModule {
	t.Helper()
	decorator, err := srv.xkmsModuleWebAuthnCredentialStoreDecorator()
	require.NoError(t, err)
	module, err := xkmssdk.NewServiceModule(
		func() *xkmslib.XKMSService {
			svc, getErr := xkmssdk.Get()
			if getErr != nil {
				return nil
			}
			return svc
		},
		xkmssdk.WithServiceModuleQRDBClient(srv.GetQRDBNodeClient),
		xkmssdk.WithServiceModuleWebAuthn(quicbaasWebAuthnConfigToXKMS(srv.config.IAM.WebAuthn, true)),
		xkmssdk.WithServiceModuleWebAuthnCredentialStoreDecorator(decorator),
		xkmssdk.WithServiceModuleSession(&xkmssdk.ServiceModuleSessionOptions{}),
	)
	require.NoError(t, err)
	return module
}

// postPasskeyCeremony posts body to one ceremony route, carrying session as
// the ceremony's session header, and returns the decoded answer and the
// session header it set.
func postPasskeyCeremony(t *testing.T, router http.Handler, path, session string, body any) (map[string]any, string) {
	t.Helper()
	encoded, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", passkeyOrigin)
	if session != "" {
		req.Header.Set("X-Session-Id", session)
	}
	ctx, cancel := context.WithTimeout(req.Context(), time.Minute)
	defer cancel()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req.WithContext(ctx))
	require.Equal(t, http.StatusOK, rec.Code, "%s: %s", path, rec.Body.String())
	var answer map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &answer), "%s: %s", path, rec.Body.String())
	return answer, rec.Header().Get("X-Session-Id")
}
