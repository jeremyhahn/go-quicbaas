// Copyright (c) 2025 Automate The Things, LLC
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
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// dcrRegisterPath is the RFC 7591 dynamic client registration endpoint.
const dcrRegisterPath = "/oauth2/register"

// dcrTokenScope is the scope list the SDK integration bootstrap requests
// (dataPlaneScopes in sdk/go/test/integration/auth_bootstrap_test.go),
// copied here so the client_credentials grant below asks for exactly what
// the SDK suite asks for when it bootstraps through the same chain.
const dcrTokenScope = "kv:get kv:put kv:delete kv:list " +
	"keys:get keys:list keys:create keys:delete " +
	"certs:read certs:list certs:generate certs:revoke " +
	"credentials:retrieve credentials:list credentials:store credentials:delete " +
	"tenants:read tenants:list tenants:create tenants:delete " +
	"transaction:execute timeseries:query timeseries:write " +
	"tenant_barrier:status tenant_barrier:init tenant_barrier:seal tenant_barrier:unseal " +
	"ceremony:status ceremony:initialize " +
	"scheduler:read scheduler:list scheduler:create scheduler:update " +
	"scheduler:delete scheduler:trigger scheduler:pause scheduler:resume " +
	"scheduler:cancel scheduler:history scheduler:status " +
	"objstore:get objstore:list objstore:put objstore:delete " +
	"orchestrator:status orchestrator:list orchestrator:spawn orchestrator:close " +
	"messaging:channel_read messaging:channel_list messaging:channel_create " +
	"messaging:channel_delete messaging:publish messaging:subscribe " +
	"parameters:read parameters:list parameters:write parameters:delete " +
	"dynamic_secrets:role_read dynamic_secrets:role_list dynamic_secrets:role_create " +
	"dynamic_secrets:role_delete dynamic_secrets:generate " +
	"project:read project:list project:create project:delete " +
	"ollama:health ollama:model_list ollama:embed"

// dcrRegistrationBody is the client_credentials registration request the SDK
// bootstrap sends, with a unique client name per test.
func dcrRegistrationBody() map[string]interface{} {
	return map[string]interface{}{
		"client_name":                "dcr-integration-" + generateUniqueTenantID(),
		"grant_types":                []string{"client_credentials"},
		"token_endpoint_auth_method": "client_secret_post",
		"scope":                      dcrTokenScope,
	}
}

// doFormPost POSTs an application/x-www-form-urlencoded body with no
// Authorization header and returns the status and response body.
func doFormPost(t *testing.T, cluster *testCluster, path string, form url.Values) (int, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cluster.httpEndpoint+path,
		strings.NewReader(form.Encode()))
	require.NoError(t, err, "build POST %s", path)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := cluster.client.Do(req)
	require.NoError(t, err, "POST %s", path)
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err, "read POST %s response", path)
	return resp.StatusCode, body
}

// TestDCR_InitialAccessTokenRegistersClient drives the SDK bootstrap chain
// against the real unified server: the PIV admin bearer is presented as the
// initial access token, registration returns a client, and that client gets
// a client_credentials access token.
func TestDCR_InitialAccessTokenRegistersClient(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()
	initialAccessToken := helpers.AuthPIV(t, cluster.httpEndpoint)

	status, body := doBearerRequest(t, cluster, http.MethodPost, dcrRegisterPath,
		initialAccessToken, dcrRegistrationBody())
	require.Equal(t, http.StatusCreated, status,
		"DCR with an initial access token must register the client: %s", body)

	var client struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	require.NoError(t, json.Unmarshal(body, &client), "decode DCR response: %s", body)
	require.NotEmpty(t, client.ClientID, "DCR response has no client_id: %s", body)
	require.NotEmpty(t, client.ClientSecret, "DCR response has no client_secret: %s", body)

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", client.ClientID)
	form.Set("client_secret", client.ClientSecret)
	form.Set("scope", dcrTokenScope)
	status, body = doFormPost(t, cluster, "/oauth2/token", form)
	require.Equal(t, http.StatusOK, status,
		"the registered client must get a client_credentials token: %s", body)

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	require.NoError(t, json.Unmarshal(body, &tok), "decode token response: %s", body)
	require.NotEmpty(t, tok.AccessToken, "token response has no access_token: %s", body)
}

// TestDCR_RegistrationRequiresInitialAccessToken proves registration stays
// protected on the integration node: no bearer is refused with 401, and a
// bearer that is not a real token is refused without creating a client.
func TestDCR_RegistrationRequiresInitialAccessToken(t *testing.T) {
	cluster := setupTestCluster(t)
	defer cluster.Close()

	status, body := doBearerRequest(t, cluster, http.MethodPost, dcrRegisterPath, "", dcrRegistrationBody())
	require.Equal(t, http.StatusUnauthorized, status,
		"DCR without an initial access token must be refused: %s", body)
	require.Contains(t, string(body), "initial access token is required",
		"the refusal must name the missing initial access token: %s", body)

	status, body = doBearerRequest(t, cluster, http.MethodPost, dcrRegisterPath,
		"not-a-real-token", dcrRegistrationBody())
	require.GreaterOrEqual(t, status, 400, "DCR with a garbage bearer must be refused: %s", body)
	require.Less(t, status, 500, "DCR with a garbage bearer must be a client error: %s", body)
	require.NotEqual(t, http.StatusNotFound, status,
		"DCR must be routed on the integration node: %s", body)
}
