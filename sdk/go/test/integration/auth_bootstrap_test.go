// Copyright (c) 2025 Jeremy Hahn
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

package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// dataPlaneScopes are the qrdb-canonical colon "resource:action" scopes the SDK
// integration suite requests for its single bootstrap token under the
// default-deny gate (QUICBAAS_SECURITY_REQUIRE_AUTHZ=true).
//
// Why requested scopes end up in the token's scp: the suite authenticates via
// the public client_credentials grant (DCR client, no user, no qrdb roles). For
// that grant the issuer's scp precedence (pkg/iam/oidc_tokens.go
// GenerateAccessToken) resolves the subject's effective qrdb permissions FIRST,
// but a roleless service account resolves to the empty set
// (go-qrdb pkg/role/store.go ResolvePermissions over zero roles), so scp falls
// through to parseScopes(claims.Scope) — i.e. the OAuth-requested scopes carried
// here. The offline platform validator's RequireScope then matches scp against
// the colon perm each PEP demands. Requesting the full registered colon set keeps
// one bootstrap token usable across every authed helper.
//
// Each entry is a REGISTERED qrdb permission (resource:action) and aligns with
// the REST gate tables the suite is exercised against:
//   - default-deny scope table (pkg/api/rest/authz_default_deny.go)
//   - PEP CRUD + special-rule maps  (pkg/api/rest/authz_pep_mapping.go)
//
// Surfaces the suite actually touches today: entity KV (/api/v1/entities ->
// kv:*) and the scheduler (/api/v1/scheduler/jobs* -> scheduler:*; the scheduler
// is absent from the default-deny scope table so it needs only a valid token,
// but the scheduler verbs are requested anyway so the token authorizes if the
// gate is ever tightened). The remaining data-plane families are requested so a
// single token covers the broader REST data plane (kv/keys/certs/credentials/
// tenants plus the broker data-plane families that have a registered perm set).
var dataPlaneScopes = []string{
	// KV data plane (entities/keys/kv/query/entity-types/batch).
	"kv:get", "kv:put", "kv:delete", "kv:list",

	// Cryptographic key operations (kms/crypto/dek).
	"keys:get", "keys:list", "keys:create", "keys:delete",

	// Certificate / trust / ACME control plane.
	"certs:read", "certs:list", "certs:generate", "certs:revoke",

	// Secret / named-credential rotation.
	"credentials:retrieve", "credentials:list", "credentials:store", "credentials:delete",

	// IAM tenant/user/role admin surface.
	"tenants:read", "tenants:list", "tenants:create", "tenants:delete",

	// Transactions / timeseries data services.
	"transaction:execute",
	"timeseries:query", "timeseries:write",

	// Tenant barrier + ceremony lifecycle.
	"tenant_barrier:status", "tenant_barrier:init", "tenant_barrier:seal", "tenant_barrier:unseal",
	"ceremony:status", "ceremony:initialize",

	// Scheduler job lifecycle (heavily exercised by scheduler_test.go).
	"scheduler:read", "scheduler:list", "scheduler:create", "scheduler:update",
	"scheduler:delete", "scheduler:trigger", "scheduler:pause", "scheduler:resume",
	"scheduler:cancel", "scheduler:history", "scheduler:status",

	// Object storage.
	"objstore:get", "objstore:list", "objstore:put", "objstore:delete",

	// Orchestrator.
	"orchestrator:status", "orchestrator:list", "orchestrator:spawn", "orchestrator:close",

	// Messaging.
	"messaging:channel_read", "messaging:channel_list", "messaging:channel_create",
	"messaging:channel_delete", "messaging:publish", "messaging:subscribe",

	// Parameter store.
	"parameters:read", "parameters:list", "parameters:write", "parameters:delete",

	// Dynamic secrets.
	"dynamic_secrets:role_read", "dynamic_secrets:role_list", "dynamic_secrets:role_create",
	"dynamic_secrets:role_delete", "dynamic_secrets:generate",

	// Project + membership.
	"project:read", "project:list", "project:create", "project:delete",

	// Ollama embedding inference.
	"ollama:health", "ollama:model_list", "ollama:embed",
}

var (
	// bootstrapToken caches the platform access token obtained once via the
	// public OAuth2 bootstrap (DCR + client_credentials) so every authed
	// data-plane helper reuses the same Bearer.
	bootstrapToken string

	// bootstrapTokenOnce guards the one-time token acquisition.
	bootstrapTokenOnce sync.Once

	// bootstrapTokenErr records a failure from the one-time acquisition so
	// callers surface it rather than silently sending anonymous requests.
	bootstrapTokenErr error
)

// acquirePlatformToken obtains a platform access token for the integration
// suite. TLS is mandatory and POST /oauth2/register requires an RFC 7591
// initial access token, so the bootstrap chains off the server's cert-based
// admin identity (there are no invite tokens or password bootstrap):
//
//  1. POST /api/v1/auth/login/piv over mTLS presenting the fixture admin
//     client certificate (auth.bootstrap_admin_piv_cert_path binds the
//     bootstrap admin user to its SPKI thumbprint at startup) to obtain an
//     admin bearer — the initial access token.
//  2. POST /oauth2/register (RFC 7591 Dynamic Client Registration) with that
//     bearer to mint a confidential client allowed the client_credentials
//     grant.
//  3. POST /oauth2/token (public) with grant_type=client_credentials and the
//     required data-plane scopes to mint the suite's Bearer access token.
//
// Every HTTPS client pins the integration CA fixture (see newHTTPClient).
// The token is cached for the whole suite. Returns an error when the
// bootstrap is not reachable so the suite fails loudly rather than falling
// back to anonymous requests (which the default-deny gate rejects).
func acquirePlatformToken(ctx context.Context) (string, error) {
	bootstrapTokenOnce.Do(func() {
		bootstrapToken, bootstrapTokenErr = doAcquirePlatformToken(ctx)
	})
	return bootstrapToken, bootstrapTokenErr
}

func doAcquirePlatformToken(ctx context.Context) (string, error) {
	initialAccessToken, err := adminPIVToken(ctx)
	if err != nil {
		return "", err
	}
	clientID, clientSecret, err := registerBootstrapClient(ctx, initialAccessToken)
	if err != nil {
		return "", err
	}
	return clientCredentialsToken(ctx, clientID, clientSecret)
}

// adminPIVToken performs the cert-bound bootstrap-admin login: POST
// /api/v1/auth/login/piv over mTLS presenting the fixture admin client
// certificate. The server bound the bootstrap admin user to this
// certificate's SPKI thumbprint at startup, so the handshake itself is the
// credential — no pre-existing token is needed. The returned bearer is the
// RFC 7591 initial access token the DCR step presents.
//
// Cert bundle paths default to the shared integration fixtures and honor the
// same override env vars the test/integration helpers use
// (QUICBAAS_TEST_PIV_CERT / _KEY).
func adminPIVToken(ctx context.Context) (string, error) {
	certPath := getEnvOrDefault("QUICBAAS_TEST_PIV_CERT", filepath.Join(certsFixtureDir, "admin.crt"))
	keyPath := getEnvOrDefault("QUICBAAS_TEST_PIV_KEY", filepath.Join(certsFixtureDir, "admin.key"))

	clientCert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "load admin client certificate", Cause: err}
	}
	pinned, err := suiteTLS()
	if err != nil {
		return "", err
	}
	tlsCfg := pinned.Clone()
	tlsCfg.Certificates = []tls.Certificate{clientCert}

	client := &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   tlsCfg,
			DisableKeepAlives: true,
		},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		GetRESTAddress()+"/api/v1/auth/login/piv", nil)
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "build PIV login request", Cause: err}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "PIV login request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &qberrors.MessageError{
			Message: "PIV login failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return "", &qberrors.WrappedMessageError{Message: "decode PIV login response", Cause: err}
	}
	if tok.AccessToken == "" {
		return "", &qberrors.MessageError{Message: "PIV login response missing access_token: " + string(respBody)}
	}
	return tok.AccessToken, nil
}

// registerBootstrapClient performs RFC 7591 Dynamic Client Registration
// against /oauth2/register, presenting the admin bearer as the initial
// access token, and returns the issued credentials.
func registerBootstrapClient(ctx context.Context, initialAccessToken string) (clientID, clientSecret string, err error) {
	body, _ := json.Marshal(map[string]interface{}{
		"client_name":                "sdk-integration-bootstrap",
		"grant_types":                []string{"client_credentials"},
		"token_endpoint_auth_method": "client_secret_post",
		"scope":                      strings.Join(dataPlaneScopes, " "),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		GetRESTAddress()+"/oauth2/register", bytes.NewReader(body))
	if err != nil {
		return "", "", &qberrors.WrappedMessageError{Message: "build DCR request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+initialAccessToken)

	resp, err := newHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return "", "", &qberrors.WrappedMessageError{Message: "DCR request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return "", "", &qberrors.MessageError{
			Message: "DCR failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var dcr struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(respBody, &dcr); err != nil {
		return "", "", &qberrors.WrappedMessageError{Message: "decode DCR response", Cause: err}
	}
	if dcr.ClientID == "" || dcr.ClientSecret == "" {
		return "", "", &qberrors.MessageError{Message: "DCR returned empty client credentials: " + string(respBody)}
	}
	return dcr.ClientID, dcr.ClientSecret, nil
}

// clientCredentialsToken exchanges the bootstrap client credentials for a
// platform access token carrying the data-plane scopes via the public
// /oauth2/token endpoint (RFC 6749 client_credentials grant).
func clientCredentialsToken(ctx context.Context, clientID, clientSecret string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("scope", strings.Join(dataPlaneScopes, " "))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		GetRESTAddress()+"/oauth2/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "build token request", Cause: err}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := newHTTPClient(15 * time.Second).Do(req)
	if err != nil {
		return "", &qberrors.WrappedMessageError{Message: "token request failed", Cause: err}
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", &qberrors.MessageError{
			Message: "token request failed with status " + strconv.Itoa(resp.StatusCode) + ": " + string(respBody),
		}
	}

	var tok struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		Scope       string `json:"scope"`
	}
	if err := json.Unmarshal(respBody, &tok); err != nil {
		return "", &qberrors.WrappedMessageError{Message: "decode token response", Cause: err}
	}
	if tok.AccessToken == "" {
		return "", &qberrors.MessageError{Message: "token response missing access_token: " + string(respBody)}
	}
	return tok.AccessToken, nil
}

// authorize attaches the cached platform Bearer token to a request so it passes
// the server's default-deny scope -> RBAC gate. It is a no-op (anonymous) when
// no token was acquired, which is what the negative test relies on.
func authorize(req *http.Request) {
	if bootstrapToken != "" {
		req.Header.Set("Authorization", "Bearer "+bootstrapToken)
	}
}
