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

//go:build integration

package iam_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// The self-registration on/off matrix over REST (#358). The owner ruled on
// 2026-09-01 that with iam.self_service_registration.enabled false (the
// default) registration is invite-only, and with it true an anonymous
// registrant creates a tenant and becomes its Admin. Each test boots its own
// daemon in its own temp dir and drives a real WebAuthn ceremony from a
// software authenticator, so the matrix is proved against the running
// handlers, the WebAuthn relying party and the identity store together.

const (
	// selfRegRPID and selfRegOrigin are the relying party the daemon serves
	// and the origin the software authenticator signs its client data for.
	selfRegRPID   = "localhost"
	selfRegOrigin = "https://localhost"

	selfRegBeginPath  = "/api/v1/auth/register/begin"
	selfRegFinishPath = "/api/v1/auth/register/finish"
	loginBeginPath    = "/api/v1/auth/login/begin"
	loginFinishPath   = "/api/v1/auth/login/finish"
)

// selfRegDaemon is one in-process daemon serving REST over TLS.
type selfRegDaemon struct {
	srv     *server.Server
	baseURL string
	client  *http.Client
}

// selfRegBeginResponse is the begin endpoint's answer.
type selfRegBeginResponse struct {
	Options   json.RawMessage `json:"public_key_credential_creation_options"`
	SessionID string          `json:"session_id"`
}

// selfRegFinishResponse is the finish endpoint's answer.
type selfRegFinishResponse struct {
	Status      string `json:"status"`
	TenantID    string `json:"tenant_id"`
	UserID      string `json:"user_id"`
	AccessToken string `json:"access_token"`
}

// selfRegErrorResponse is the IAM error body.
type selfRegErrorResponse struct {
	Error string `json:"error"`
}

// selfRegistrant is a completed registration: who registered, the passkey
// they registered with, and what the daemon answered.
type selfRegistrant struct {
	email   string
	passkey *helpers.SoftwarePasskey
	finish  selfRegFinishResponse
}

// startSelfRegDaemon boots a daemon with self-service registration switched
// on or off, and a REST listener on a kernel-assigned port.
func startSelfRegDaemon(t *testing.T, enabled bool) *selfRegDaemon {
	t.Helper()

	cfg := minimalBootstrapConfig(t.TempDir())
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false
	cfg.Server.Embedded = false
	cfg.Server.REST.Enabled = true

	cfg.IAM.WebAuthn.RPID = selfRegRPID
	cfg.IAM.WebAuthn.RPOrigins = []string{selfRegOrigin}
	// The registrant becomes the tenant's Admin, so the enrollment is
	// administrative and the default "hardware" assurance refuses a software
	// authenticator, as it must. This is the documented test-environment
	// policy that lets one stand in for a security key.
	cfg.IAM.WebAuthn.Assurance = "insecure-allow-software-administrators"

	cfg.IAM.SelfServiceRegistration.Enabled = enabled
	// Approval would leave the registrant pending with no session; the
	// matrix is about the active path, where finish mints tokens.
	cfg.IAM.SelfServiceRegistration.RequireAdminApproval = false

	srv, err := startBootstrapServer(t, cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	restAddr := srv.RESTAddr()
	require.NotNil(t, restAddr, "REST listener must be bound when Embedded=false and REST is enabled")
	scheme := "https"
	if !cfg.Security.TLSEnabled {
		scheme = "http"
	}
	return &selfRegDaemon{
		srv:     srv,
		baseURL: fmt.Sprintf("%s://%s", scheme, restAddr.String()),
		client:  daemonCAHTTPClient(t, srv),
	}
}

// do sends a JSON request, with a bearer when one is given, and returns the
// status and body.
func (d *selfRegDaemon) do(t *testing.T, method, path, bearer string, body interface{}) (int, []byte) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(encoded)
	}
	req, err := http.NewRequest(method, d.baseURL+path, reader)
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := d.client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp.StatusCode, respBody
}

// identityStore reaches the daemon's identity store, to check what a
// ceremony wrote without going through the surface under test.
func (d *selfRegDaemon) identityStore(t *testing.T) *iam.IdentityStore {
	t.Helper()
	provider, ok := d.srv.Service().(interface {
		GetIdentityStore(ctx context.Context) (*iam.IdentityStore, error)
	})
	require.True(t, ok, "the daemon's service must expose its identity store")
	store, err := provider.GetIdentityStore(context.Background())
	require.NoError(t, err)
	require.NotNil(t, store)
	return store
}

// begin starts a self-registration and returns the creation options.
func (d *selfRegDaemon) begin(t *testing.T, tenantName, email string) selfRegBeginResponse {
	t.Helper()
	status, body := d.do(t, http.MethodPost, selfRegBeginPath, "", selfRegBeginBody(tenantName, email))
	require.Equalf(t, http.StatusOK, status, "register/begin: %s", body)
	var begin selfRegBeginResponse
	require.NoError(t, json.Unmarshal(body, &begin))
	require.NotEmpty(t, begin.SessionID, "begin must return a session id")
	require.NotEmpty(t, begin.Options, "begin must return credential-creation options")
	return begin
}

// register completes a full self-registration ceremony for a new tenant.
func (d *selfRegDaemon) register(t *testing.T, tenantName, email string) (selfRegistrant, map[string]interface{}) {
	t.Helper()
	begin := d.begin(t, tenantName, email)

	passkey, err := helpers.NewSoftwarePasskey(selfRegRPID, selfRegOrigin)
	require.NoError(t, err)
	credential, err := passkey.Register(begin.Options)
	require.NoError(t, err, "the software authenticator must answer the creation options")

	finishBody := map[string]interface{}{"session_id": begin.SessionID, "credential": credential}
	status, body := d.do(t, http.MethodPost, selfRegFinishPath, "", finishBody)
	require.Equalf(t, http.StatusOK, status, "register/finish: %s", body)
	var finish selfRegFinishResponse
	require.NoError(t, json.Unmarshal(body, &finish))
	return selfRegistrant{email: email, passkey: passkey, finish: finish}, finishBody
}

func selfRegBeginBody(tenantName, email string) map[string]string {
	return map[string]string{
		"tenant_name":  tenantName,
		"email":        email,
		"display_name": "Self Registrant",
	}
}

func requireIAMErrorCode(t *testing.T, body []byte, code string) {
	t.Helper()
	var apiErr selfRegErrorResponse
	require.NoErrorf(t, json.Unmarshal(body, &apiErr), "error body must be JSON: %s", body)
	assert.Equalf(t, code, apiErr.Error, "error code: %s", body)
}

// TestSelfRegistration_Disabled_RefusesBeginAndFinish: with the flag off (the
// default) both ceremony endpoints answer 403 self_registration_disabled and
// nothing is written.
func TestSelfRegistration_Disabled_RefusesBeginAndFinish(t *testing.T) {
	d := startSelfRegDaemon(t, false)
	// slugify("selfreg-off") is "selfreg-off": the id begin would derive.
	const tenantName = "selfreg-off"

	status, body := d.do(t, http.MethodPost, selfRegBeginPath, "", selfRegBeginBody(tenantName, "off@example.com"))
	assert.Equalf(t, http.StatusForbidden, status, "register/begin: %s", body)
	requireIAMErrorCode(t, body, "self_registration_disabled")

	// The refusal must not depend on the request being well formed: a finish
	// carrying a session and a credential is refused before either is read.
	status, body = d.do(t, http.MethodPost, selfRegFinishPath, "", map[string]interface{}{
		"session_id": "00000000-0000-0000-0000-000000000000",
		"credential": map[string]interface{}{"id": "AAAA", "type": "public-key"},
	})
	assert.Equalf(t, http.StatusForbidden, status, "register/finish: %s", body)
	requireIAMErrorCode(t, body, "self_registration_disabled")

	_, err := d.identityStore(t).GetTenant(context.Background(), tenantName)
	assert.Error(t, err, "a refused self-registration must not create a tenant")
}

// TestSelfRegistration_Enabled_RegistrantBecomesTenantAdmin: with the flag on
// a full ceremony creates the tenant and its Admin, the session it mints is
// confined to that tenant, the finish cannot be replayed, and the passkey
// signs the registrant in afterwards.
func TestSelfRegistration_Enabled_RegistrantBecomesTenantAdmin(t *testing.T) {
	d := startSelfRegDaemon(t, true)
	ctx := context.Background()
	store := d.identityStore(t)

	alpha, alphaFinish := d.register(t, "selfreg-alpha", "alpha@example.com")

	t.Run("finish creates the tenant and binds the registrant to its Admin role", func(t *testing.T) {
		require.Equal(t, "active", alpha.finish.Status)
		require.NotEmpty(t, alpha.finish.TenantID)
		require.NotEmpty(t, alpha.finish.UserID)
		require.NotEmpty(t, alpha.finish.AccessToken, "an active registration must mint a session")

		_, err := store.GetTenant(ctx, alpha.finish.TenantID)
		require.NoError(t, err, "the registrant's tenant must exist")

		alphaUserID, err := strconv.ParseUint(alpha.finish.UserID, 10, 64)
		require.NoError(t, err, "user_id must be the registrant's numeric account id")
		user, err := store.GetUser(ctx, alpha.finish.TenantID, alphaUserID)
		require.NoError(t, err, "the registrant must be a user of the new tenant")
		assert.Equal(t, alpha.email, user.Email)

		admin, err := store.GetRoleByName(ctx, alpha.finish.TenantID, "Admin")
		require.NoError(t, err, "the new tenant must carry the built-in Admin role")
		assert.Contains(t, user.Roles, admin.ID, "the registrant must hold the tenant's Admin role")
	})

	t.Run("replaying the finish is refused", func(t *testing.T) {
		status, body := d.do(t, http.MethodPost, selfRegFinishPath, "", alphaFinish)
		assert.Equalf(t, http.StatusBadRequest, status, "replayed register/finish: %s", body)
		requireIAMErrorCode(t, body, "registration_session_expired")

		// Begin derives a collision suffix, so a replay that slipped through
		// would have landed in a second tenant.
		_, err := store.GetTenant(ctx, "selfreg-alpha-2")
		assert.Error(t, err, "a replayed finish must not create another tenant")
	})

	t.Run("the session cannot reach another tenant", func(t *testing.T) {
		beta, _ := d.register(t, "selfreg-beta", "beta@example.com")
		require.NotEqual(t, alpha.finish.TenantID, beta.finish.TenantID)

		ownUser := fmt.Sprintf("/api/v1/tenants/%s/users/%s", alpha.finish.TenantID, alpha.finish.UserID)
		status, body := d.do(t, http.MethodGet, ownUser, alpha.finish.AccessToken, nil)
		require.Equalf(t, http.StatusOK, status, "the registrant must reach their own tenant: %s", body)

		for _, path := range []string{
			fmt.Sprintf("/api/v1/tenants/%s/users/", beta.finish.TenantID),
			fmt.Sprintf("/api/v1/tenants/%s/users/%s", beta.finish.TenantID, beta.finish.UserID),
		} {
			status, body := d.do(t, http.MethodGet, path, alpha.finish.AccessToken, nil)
			assert.Containsf(t, []int{http.StatusForbidden, http.StatusNotFound}, status,
				"GET %s with another tenant's Admin session must be refused: %s", path, body)
			assert.NotContainsf(t, string(body), beta.finish.UserID,
				"GET %s must not disclose the other tenant's user", path)
		}
	})

	// The registrant's tenant is not the system tenant, so on this release the
	// verified passkey login is refused with 412 tenant_login_refused until
	// go-quicbaas #1816 re-pins go-qrdb (v0.54.0-alpha). The ceremony still
	// verifies the passkey first: begin must succeed and the refusal must be
	// the typed one, not the uniform login rejection.
	t.Run("the passkey login is refused for the registrant's tenant until #1816", func(t *testing.T) {
		status, body := d.do(t, http.MethodPost, loginBeginPath, "", map[string]string{
			"type":      "fido2",
			"hint":      alpha.email,
			"tenant_id": alpha.finish.TenantID,
		})
		require.Equalf(t, http.StatusOK, status, "login/begin: %s", body)
		var begin struct {
			SessionID         string            `json:"session_id"`
			ChallengeMetadata map[string]string `json:"challenge_metadata"`
		}
		require.NoError(t, json.Unmarshal(body, &begin))
		require.NotEmpty(t, begin.ChallengeMetadata["options"], "fido2 begin must carry request options")

		assertion, err := alpha.passkey.Assert(begin.ChallengeMetadata["options"])
		require.NoError(t, err, "the registered passkey must answer the request options")

		credentialID := alpha.passkey.CredentialID()
		status, body = d.do(t, http.MethodPost, loginFinishPath, "", map[string]interface{}{
			"session_id": begin.SessionID,
			"type":       "fido2",
			"lookup_key": []byte(credentialID),
			"signature":  []byte(assertion),
			"method_metadata": map[string]string{
				"session_id":    begin.ChallengeMetadata["session_id"],
				"credential_id": credentialID,
			},
		})
		require.Equalf(t, http.StatusPreconditionFailed, status, "login/finish: %s", body)
		var refusal struct {
			Error  string `json:"error"`
			Bearer string `json:"bearer"`
		}
		require.NoError(t, json.Unmarshal(body, &refusal))
		assert.Equal(t, "tenant_login_refused", refusal.Error,
			"a non-system tenant login must carry the typed refusal (#1816)")
		assert.Empty(t, refusal.Bearer, "a refused login must not mint a bearer")
	})
}
