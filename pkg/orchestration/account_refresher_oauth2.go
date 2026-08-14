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

package orchestration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// =============================================================================
// OAuth2TokenRefresher — a generic, standards-compliant RFC 6749 section 6
// "refresh_token" grant client.
//
// This is intentionally provider-agnostic: it does not know or assume
// Anthropic's, OpenAI's, or any other vendor's token endpoint, client_id, or
// token response shape beyond what RFC 6749 mandates
// (access_token/refresh_token/expires_in as JSON). The concrete
// TokenEndpoint and ClientID for a given account come from that account's
// AccountCredential, set at account-creation time — see
// docs/orchestration/accounts.md "Owner decisions required" for why this
// package does not hardcode Anthropic's (unofficial, reverse-engineered)
// OAuth application details.
// =============================================================================

// OAuth2RefresherConfig configures OAuth2TokenRefresher.
type OAuth2RefresherConfig struct {
	// HTTPClient issues the refresh request. A nil value selects
	// http.DefaultClient.
	HTTPClient *http.Client

	// Timeout bounds one refresh HTTP round trip. Zero selects
	// DefaultOAuth2RefreshTimeout.
	Timeout time.Duration
}

// DefaultOAuth2RefreshTimeout bounds a refresh_token grant round trip.
const DefaultOAuth2RefreshTimeout = 15 * time.Second

// OAuth2TokenRefresher implements TokenRefresher via a standard
// "grant_type=refresh_token" POST to the account credential's TokenEndpoint.
type OAuth2TokenRefresher struct {
	cfg OAuth2RefresherConfig
}

// Compile-time interface satisfaction check.
var _ TokenRefresher = (*OAuth2TokenRefresher)(nil)

// NewOAuth2TokenRefresher returns a refresher using cfg.
func NewOAuth2TokenRefresher(cfg OAuth2RefresherConfig) *OAuth2TokenRefresher {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = http.DefaultClient
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = DefaultOAuth2RefreshTimeout
	}
	return &OAuth2TokenRefresher{cfg: cfg}
}

// oauth2TokenResponse is the RFC 6749 section 5.1 access token response
// shape this refresher understands. A server that omits refresh_token is
// treated as "did not rotate" — the caller keeps presenting the same
// refresh_token, matching non-rotating OAuth2 servers.
type oauth2TokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int64  `json:"expires_in"`
	TokenType        string `json:"token_type"`
	Scope            string `json:"scope"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// Refresh implements TokenRefresher.
func (r *OAuth2TokenRefresher) Refresh(ctx context.Context, cred *AccountCredential) (*AccountCredential, error) {
	if cred == nil {
		return nil, &AccountInvalidError{Field: "credential", Message: "must not be nil"}
	}
	if cred.TokenEndpoint == "" {
		return nil, &AccountInvalidError{Field: "token_endpoint", Message: "must not be empty for an oauth2 refresh"}
	}
	if cred.RefreshToken == "" {
		return nil, &AccountInvalidError{Field: "refresh_token", Message: "must not be empty for an oauth2 refresh"}
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", cred.RefreshToken)
	if cred.ClientID != "" {
		form.Set("client_id", cred.ClientID)
	}

	reqCtx, cancel := context.WithTimeout(ctx, r.cfg.Timeout)
	defer cancel()

	httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, cred.TokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, &AccountRefreshFailedError{Cause: err}
	}
	httpReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := r.cfg.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, &AccountRefreshFailedError{Cause: err}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, &AccountRefreshFailedError{Cause: err}
	}

	var tokenResp oauth2TokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		return nil, &AccountRefreshFailedError{Cause: &OAuth2ResponseDecodeError{Status: resp.StatusCode, Cause: err}}
	}

	if resp.StatusCode != http.StatusOK || tokenResp.AccessToken == "" {
		return nil, &AccountRefreshFailedError{Cause: &OAuth2TokenEndpointError{
			Status:      resp.StatusCode,
			Code:        tokenResp.Error,
			Description: tokenResp.ErrorDescription,
		}}
	}

	renewed := *cred
	renewed.AccessToken = tokenResp.AccessToken
	if tokenResp.RefreshToken != "" {
		renewed.RefreshToken = tokenResp.RefreshToken
	}
	if tokenResp.ExpiresIn > 0 {
		renewed.ExpiresAt = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}
	if tokenResp.Scope != "" {
		renewed.Scopes = strings.Fields(tokenResp.Scope)
	}
	return &renewed, nil
}

// OAuth2ResponseDecodeError indicates the token endpoint's response body was
// not valid JSON.
type OAuth2ResponseDecodeError struct {
	Status int
	Cause  error
}

// Error implements the error interface.
func (e *OAuth2ResponseDecodeError) Error() string {
	return "oauth2 refresh: failed to decode response (status " + strconv.Itoa(e.Status) + "): " + e.Cause.Error()
}

// Unwrap returns the underlying decode error for errors.Is / errors.As.
func (e *OAuth2ResponseDecodeError) Unwrap() error { return e.Cause }

// OAuth2TokenEndpointError indicates the token endpoint rejected the refresh
// request per RFC 6749 section 5.2.
type OAuth2TokenEndpointError struct {
	Status      int
	Code        string
	Description string
}

// Error implements the error interface.
func (e *OAuth2TokenEndpointError) Error() string {
	msg := "oauth2 refresh: token endpoint returned status " + strconv.Itoa(e.Status)
	if e.Code != "" {
		msg += ": " + e.Code
	}
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}
