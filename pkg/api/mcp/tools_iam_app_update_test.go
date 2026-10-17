//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	mcptransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport/mcp"
)

// captureAppUpdate runs quicbaas_iam_app_update and returns the request that
// reached the IAM service.
func captureAppUpdate(t *testing.T, args map[string]interface{}) *service.UpdateAppRequest {
	t.Helper()
	var got *service.UpdateAppRequest
	reg := newIAMTestRegistry(&mockIAMService{
		updateAppFunc: func(_ context.Context, req *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
			got = req
			return &service.UpdateAppResponse{App: &iam.Application{ClientID: req.ClientID}}, nil
		},
	})
	_, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_app_update", args)
	require.NoError(t, err)
	require.NotNil(t, got)
	return got
}

// TestAppUpdate_EveryArgReachesRequest sends each argument on its own, in the
// form a JSON-RPC client decodes it, and checks the service request field.
func TestAppUpdate_EveryArgReachesRequest(t *testing.T) {
	tests := []struct {
		arg   string
		value interface{}
		check func(*testing.T, *service.UpdateAppRequest)
	}{
		{"name", "n", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "n", *r.Name) }},
		{"redirect_uris", []interface{}{"https://a/cb"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, []string{"https://a/cb"}, *r.RedirectURIs)
		}},
		{"grant_types", []interface{}{"client_credentials"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, []string{"client_credentials"}, *r.GrantTypes)
		}},
		{"response_types", []interface{}{"code"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, []string{"code"}, *r.ResponseTypes)
		}},
		{"resource_prefix", "p/", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "p/", *r.ResourcePrefix) }},
		{"default_audience", "api://orders", func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, "api://orders", *r.DefaultAudience)
		}},
		{"scopes", []interface{}{"kv:read"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, []string{"kv:read"}, *r.Scopes)
		}},
		{"token_endpoint_auth_method", "client_secret_post", func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, "client_secret_post", *r.TokenEndpointAuthMethod)
		}},
		{"mtls_cert_thumbprint", "", func(t *testing.T, r *service.UpdateAppRequest) {
			require.NotNil(t, r.MTLSCertThumbprint, "an explicit empty thumbprint unbinds and must stay present")
			assert.Empty(t, *r.MTLSCertThumbprint)
		}},
		{"pkce_required", false, func(t *testing.T, r *service.UpdateAppRequest) {
			require.NotNil(t, r.PKCERequired)
			assert.False(t, *r.PKCERequired)
		}},
		{"description", "d", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "d", *r.Description) }},
		{"post_logout_redirect_uris", []interface{}{"https://a/out"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, []string{"https://a/out"}, *r.PostLogoutRedirectURIs)
		}},
		{"contacts", []interface{}{"ops@example.com"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, []string{"ops@example.com"}, *r.Contacts)
		}},
		{"logo_uri", "https://a/l", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "https://a/l", *r.LogoURI) }},
		{"policy_uri", "https://a/p", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "https://a/p", *r.PolicyURI) }},
		{"tos_uri", "https://a/t", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "https://a/t", *r.TOSURI) }},
		{"jwks_uri", "https://a/j", func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, "https://a/j", *r.JWKSURI) }},
		{"jwks", `{"keys":[]}`, func(t *testing.T, r *service.UpdateAppRequest) { assert.Equal(t, `{"keys":[]}`, *r.JWKS) }},
		{"access_token_ttl", float64(time.Hour), func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, time.Hour, *r.AccessTokenTTL)
		}},
		{"refresh_token_ttl", float64(24 * time.Hour), func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, 24*time.Hour, *r.RefreshTokenTTL)
		}},
		{"id_token_ttl", float64(time.Minute), func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, time.Minute, *r.IDTokenTTL)
		}},
		{"metadata", map[string]interface{}{"k": "v"}, func(t *testing.T, r *service.UpdateAppRequest) {
			assert.Equal(t, map[string]string{"k": "v"}, *r.Metadata)
		}},
		{"enabled", false, func(t *testing.T, r *service.UpdateAppRequest) {
			require.NotNil(t, r.Enabled, "an explicit false must stay present")
			assert.False(t, *r.Enabled)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.arg, func(t *testing.T) {
			got := captureAppUpdate(t, map[string]interface{}{"client_id": "c1", tt.arg: tt.value})
			assert.Equal(t, "c1", got.ClientID)
			tt.check(t, got)
		})
	}
}

// TestAppUpdate_AuthMethodPresence proves client_secret_basic reaches the
// request when named, and an update that omits the method leaves it unset so
// the stored one is unchanged (#1473).
func TestAppUpdate_AuthMethodPresence(t *testing.T) {
	got := captureAppUpdate(t, map[string]interface{}{"client_id": "c1", "token_endpoint_auth_method": "client_secret_basic"})
	require.NotNil(t, got.TokenEndpointAuthMethod, "client_secret_basic must stay present")
	assert.Equal(t, "client_secret_basic", *got.TokenEndpointAuthMethod)

	got = captureAppUpdate(t, map[string]interface{}{"client_id": "c1", "name": "renamed"})
	assert.Nil(t, got.TokenEndpointAuthMethod, "an omitted method must not be sent")
}

func TestAppUpdate_RefusedFieldIsInvalidInput(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{
		updateAppFunc: func(_ context.Context, _ *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
			return nil, &iam.AppFieldNotStoredError{Fields: []string{"logo_uri"}}
		},
	})

	_, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_app_update",
		map[string]interface{}{"client_id": "c1", "logo_uri": "https://a/l"})

	require.Error(t, err)
	assert.Equal(t, mcptransport.MCPInvalidInput, toolErrorCode(err))
}

func TestAppUpdate_MistypedArgIsInvalidInput(t *testing.T) {
	reg := newIAMTestRegistry(&mockIAMService{
		updateAppFunc: func(_ context.Context, _ *service.UpdateAppRequest) (*service.UpdateAppResponse, error) {
			t.Fatal("a mistyped argument must not reach the IAM service")
			return nil, nil
		},
	})

	_, err := reg.executeIAMTool(context.Background(), "quicbaas_iam_app_update",
		map[string]interface{}{"client_id": "c1", "redirect_uris": "https://a/cb"})

	var argsErr *AppUpdateArgsError
	require.ErrorAs(t, err, &argsErr)
	assert.Equal(t, mcptransport.MCPInvalidInput, toolErrorCode(err))
}
