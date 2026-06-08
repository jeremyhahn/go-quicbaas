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
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// dataPlaneScopes are the "<svc>.<verb>" scopes the SDK integration suite needs
// to exercise the protected data plane under the default-deny gate
// (QUICBAAS_SECURITY_REQUIRE_AUTHZ=true). They mirror the gate's scope table in
// pkg/api/rest/authz_default_deny.go for the surfaces the suite touches: entity
// KV (kv.read / kv.write) plus the scheduler (no specific scope, just a valid
// token). Requesting them broadly keeps a single bootstrap token usable across
// every authed helper.
var dataPlaneScopes = []string{
	"kv.read", "kv.write",
	"iam.read", "iam.write",
	"secrets.read", "secrets.write",
	"kms.read", "kms.write",
	"certificate.read", "certificate.write",
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
// suite using ONLY public, unauthenticated OAuth2 endpoints, so it works on the
// TLS-disabled single-node integration server which has no mTLS/PIV bootstrap:
//
//  1. POST /oauth2/register (RFC 7591 Dynamic Client Registration, public) to
//     mint a confidential client allowed the client_credentials grant.
//  2. POST /oauth2/token (public) with grant_type=client_credentials and the
//     required data-plane scopes to mint a Bearer access token.
//
// The token is cached for the whole suite. Returns an error when the bootstrap
// is not reachable so the suite fails loudly rather than falling back to
// anonymous requests (which the default-deny gate rejects).
func acquirePlatformToken(ctx context.Context) (string, error) {
	bootstrapTokenOnce.Do(func() {
		bootstrapToken, bootstrapTokenErr = doAcquirePlatformToken(ctx)
	})
	return bootstrapToken, bootstrapTokenErr
}

func doAcquirePlatformToken(ctx context.Context) (string, error) {
	clientID, clientSecret, err := registerBootstrapClient(ctx)
	if err != nil {
		return "", err
	}
	return clientCredentialsToken(ctx, clientID, clientSecret)
}

// registerBootstrapClient performs RFC 7591 Dynamic Client Registration against
// the public /oauth2/register endpoint and returns the issued credentials.
func registerBootstrapClient(ctx context.Context) (clientID, clientSecret string, err error) {
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

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
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

	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
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
