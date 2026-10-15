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
// +build integration

package iam_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	webauthnclient "github.com/jeremyhahn/go-xkms/pkg/webauthn/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// TestSelfRegistration_DisabledByDefault is the "off" leg of the #358
// registration matrix. The integration daemon leaves
// iam.self_service_registration.enabled at its default, false, so the
// anonymous registration ceremony must be refused at both steps with the
// typed self_registration_disabled code. The other half of the off leg, the
// system admin provisioning a tenant and its first admin, runs against this
// same daemon in tenant_provision_test.go.
func TestSelfRegistration_DisabledByDefault(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	for _, path := range []string{"/api/v1/auth/register/begin", "/api/v1/auth/register/finish"} {
		t.Run(path, func(t *testing.T) {
			status, body := doAnonymousPost(t, cluster, path, map[string]interface{}{
				"tenant_id": "selfreg-off",
				"username":  "founder",
				"email":     "founder@selfreg-off.example",
			})
			assert.Equal(t, http.StatusForbidden, status, "%s: %s", path, body)

			var refusal struct {
				Error string `json:"error"`
			}
			require.NoError(t, json.Unmarshal(body, &refusal), "%s: decode refusal %s", path, body)
			assert.Equal(t, "self_registration_disabled", refusal.Error, "%s: %s", path, body)
		})
	}
}

// TestSelfRegistration_EnabledCreatesTenantWithAdmin is the "on" leg of the
// #358 registration matrix. It runs against test-selfreg, a separate
// single-node daemon started with iam.self_service_registration.enabled=true
// and admin approval off. An anonymous registrant completes the WebAuthn
// ceremony with a software authenticator, which must create a tenant with
// its built-in roles seeded and the registrant holding Admin in it, and the
// session the registrant receives must be able to invite a member.
func TestSelfRegistration_EnabledCreatesTenantWithAdmin(t *testing.T) {
	cluster := setupSelfRegistrationDaemon(t)
	name := generateUniqueTenantID()

	status, body := doAnonymousPost(t, cluster, "/api/v1/auth/register/begin", map[string]interface{}{
		"tenant_name":  name,
		"email":        "founder@" + name + ".example",
		"display_name": "Founder",
	})
	require.Equal(t, http.StatusOK, status, "register begin: %s", body)
	var begin struct {
		Options   map[string]interface{} `json:"public_key_credential_creation_options"`
		SessionID string                 `json:"session_id"`
	}
	require.NoError(t, json.Unmarshal(body, &begin), "decode register begin: %s", body)
	require.NotEmpty(t, begin.SessionID, "register begin session id")

	credential := makeSoftwareCredential(t, begin.Options)
	status, body = doAnonymousPost(t, cluster, "/api/v1/auth/register/finish", map[string]interface{}{
		"session_id": begin.SessionID,
		"credential": credential,
	})
	require.Equal(t, http.StatusOK, status, "register finish: %s", body)
	var finish struct {
		Status      string `json:"status"`
		TenantID    string `json:"tenant_id"`
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(body, &finish), "decode register finish: %s", body)
	require.Equal(t, "active", finish.Status, "registrant with approval off is active at once: %s", body)
	require.NotEmpty(t, finish.TenantID, "tenant id")
	require.NotEmpty(t, finish.UserID, "user id")
	require.NotEmpty(t, finish.AccessToken, "access token")
	bearer := finish.AccessToken
	tenantPath := "/api/v1/tenants/" + url.PathEscape(finish.TenantID)

	status, body = doBearerRequest(t, cluster, http.MethodGet, tenantPath, bearer, nil)
	require.Equal(t, http.StatusOK, status, "the registered tenant exists: %s", body)

	t.Run("built-in roles are seeded", func(t *testing.T) {
		status, body := doBearerRequest(t, cluster, http.MethodGet, tenantPath+"/roles/", bearer, nil)
		require.Equal(t, http.StatusOK, status, "list roles: %s", body)
		var roles struct {
			Roles []struct {
				Name string `json:"name"`
			} `json:"roles"`
		}
		require.NoError(t, json.Unmarshal(body, &roles), "decode roles: %s", body)
		seeded := make(map[string]bool, len(roles.Roles))
		for _, role := range roles.Roles {
			seeded[role.Name] = true
		}
		for _, want := range qrdbsdk.BuiltInRoles(finish.TenantID) {
			assert.True(t, seeded[want.Name], "built-in role %q missing from %s", want.Name, body)
		}
	})

	t.Run("registrant is the tenant admin", func(t *testing.T) {
		status, body := doBearerRequest(t, cluster, http.MethodGet,
			tenantUsersPath(finish.TenantID)+url.PathEscape(finish.UserID), bearer, nil)
		require.Equal(t, http.StatusOK, status, "get registrant: %s", body)
		var user struct {
			Roles []string `json:"roles"`
		}
		require.NoError(t, json.Unmarshal(body, &user), "decode registrant: %s", body)
		assert.Contains(t, user.Roles, builtInRoleID(t, finish.TenantID, qrdbsdk.RoleNameAdmin),
			"registrant must hold Admin in the tenant it registered: %s", body)
	})

	t.Run("registrant invites a member", func(t *testing.T) {
		status, body := doBearerRequest(t, cluster, http.MethodPost, tenantUsersPath(finish.TenantID), bearer,
			map[string]interface{}{
				"email":    "member@" + name + ".example",
				"username": "member",
				"name":     "Member",
			})
		require.Equal(t, http.StatusCreated, status, "invite member: %s", body)
		var invited struct {
			InviteToken string `json:"invite_token"`
		}
		require.NoError(t, json.Unmarshal(body, &invited), "decode invite: %s", body)
		require.NotEmpty(t, invited.InviteToken, "member invite token: %s", body)

		invitePath := "/api/v1/iam/" + url.PathEscape(finish.TenantID) + "/invites/" + url.PathEscape(invited.InviteToken)
		// Anonymous: the invitee reads the invite before holding any credential.
		status, body = doBearerRequest(t, cluster, http.MethodGet, invitePath, "", nil)
		assert.Equal(t, http.StatusOK, status, "the member's invite is redeemable: %s", body)
	})
}

// setupSelfRegistrationDaemon returns a REST-only handle on test-selfreg, the
// daemon with self-service registration enabled. QUICBAAS_SELFREG_URL
// overrides the default address.
func setupSelfRegistrationDaemon(t *testing.T) *testCluster {
	t.Helper()
	endpoint := os.Getenv("QUICBAAS_SELFREG_URL")
	if endpoint == "" {
		endpoint = "https://test-selfreg:8080"
	}
	tlsCfg, err := helpers.IntegrationTLSConfig()
	require.NoError(t, err, "load integration TLS config")
	return &testCluster{httpEndpoint: endpoint, client: newIntegrationHTTPClient(tlsCfg)}
}

// makeSoftwareCredential answers the registration challenge with a software
// authenticator and returns the attestation as the finish request carries it.
// The origin is the one the integration config lists for rp_id localhost.
func makeSoftwareCredential(t *testing.T, options map[string]interface{}) map[string]interface{} {
	t.Helper()
	if _, wrapped := options["publicKey"]; !wrapped {
		options = map[string]interface{}{"publicKey": options}
	}
	optionsJSON, err := json.Marshal(options)
	require.NoError(t, err, "marshal creation options")

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	attestation, err := webauthnclient.NewSoftwareAdapter().MakeCredential(ctx, &webauthnclient.CeremonyOptions{
		OptionsJSON: optionsJSON,
		Origin:      "https://localhost",
	})
	require.NoError(t, err, "software authenticator MakeCredential")

	var credential map[string]interface{}
	require.NoError(t, json.Unmarshal(attestation, &credential), "decode attestation")
	return credential
}

// builtInRoleID returns the deterministic ID qrdb gives the named built-in
// role in tenantID.
func builtInRoleID(t *testing.T, tenantID, name string) string {
	t.Helper()
	for _, role := range qrdbsdk.BuiltInRoles(tenantID) {
		if role.Name == name {
			return role.ID
		}
	}
	require.Failf(t, "unknown built-in role", "%q", name)
	return ""
}

// doAnonymousPost sends body to path with no credential, as a prospective
// self-registrant would, and returns the status and response body.
func doAnonymousPost(t *testing.T, cluster *testCluster, path string, body interface{}) (int, []byte) {
	t.Helper()
	return doBearerRequest(t, cluster, http.MethodPost, path, "", body)
}
