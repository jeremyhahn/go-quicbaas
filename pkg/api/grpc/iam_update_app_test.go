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

package grpc

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// TestUpdateAppOptionsFromProto_CarriesEveryField sets each proto field on its
// own and checks that it reaches the matching update option.
func TestUpdateAppOptionsFromProto_CarriesEveryField(t *testing.T) {
	tests := []struct {
		name  string
		req   *iampb.UpdateAppRequest
		check func(*testing.T, iam.UpdateAppOptions)
	}{
		{"name", &iampb.UpdateAppRequest{Name: "n"}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.Name)
			assert.Equal(t, "n", *o.Name)
		}},
		{"redirect_uris", &iampb.UpdateAppRequest{RedirectUris: []string{"https://a/cb"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.RedirectURIs)
			assert.Equal(t, []string{"https://a/cb"}, *o.RedirectURIs)
		}},
		{"resource_prefix", &iampb.UpdateAppRequest{ResourcePrefix: "p/"}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.ResourcePrefix)
			assert.Equal(t, "p/", *o.ResourcePrefix)
		}},
		{"grant_types", &iampb.UpdateAppRequest{GrantTypes: []string{"client_credentials"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.GrantTypes)
			assert.Equal(t, []string{"client_credentials"}, *o.GrantTypes)
		}},
		{"response_types", &iampb.UpdateAppRequest{ResponseTypes: []string{"code"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.ResponseTypes)
			assert.Equal(t, []string{"code"}, *o.ResponseTypes)
		}},
		{"scopes", &iampb.UpdateAppRequest{Scopes: []string{"kv:read"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.Scopes)
			assert.Equal(t, []string{"kv:read"}, *o.Scopes)
		}},
		{"token_endpoint_auth_method", &iampb.UpdateAppRequest{TokenEndpointAuthMethod: proto.String("client_secret_post")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.TokenEndpointAuthMethod)
			assert.Equal(t, "client_secret_post", *o.TokenEndpointAuthMethod)
		}},
		{"token_endpoint_auth_method client_secret_basic", &iampb.UpdateAppRequest{TokenEndpointAuthMethod: proto.String("client_secret_basic")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.TokenEndpointAuthMethod, "client_secret_basic must stay present")
			assert.Equal(t, "client_secret_basic", *o.TokenEndpointAuthMethod)
		}},
		{"mtls_cert_thumbprint cleared", &iampb.UpdateAppRequest{MtlsCertThumbprint: proto.String("")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.MTLSCertThumbprint, "an explicit empty thumbprint unbinds and must stay present")
			assert.Empty(t, *o.MTLSCertThumbprint)
		}},
		{"description", &iampb.UpdateAppRequest{Description: proto.String("d")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.Description)
			assert.Equal(t, "d", *o.Description)
		}},
		{"post_logout_redirect_uris", &iampb.UpdateAppRequest{PostLogoutRedirectUris: []string{"https://a/out"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.PostLogoutRedirectURIs)
			assert.Equal(t, []string{"https://a/out"}, *o.PostLogoutRedirectURIs)
		}},
		{"contacts", &iampb.UpdateAppRequest{Contacts: []string{"ops@example.com"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.Contacts)
			assert.Equal(t, []string{"ops@example.com"}, *o.Contacts)
		}},
		{"logo_uri", &iampb.UpdateAppRequest{LogoUri: proto.String("https://a/l")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.LogoURI)
			assert.Equal(t, "https://a/l", *o.LogoURI)
		}},
		{"policy_uri", &iampb.UpdateAppRequest{PolicyUri: proto.String("https://a/p")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.PolicyURI)
			assert.Equal(t, "https://a/p", *o.PolicyURI)
		}},
		{"tos_uri", &iampb.UpdateAppRequest{TosUri: proto.String("https://a/t")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.TOSURI)
			assert.Equal(t, "https://a/t", *o.TOSURI)
		}},
		{"jwks_uri", &iampb.UpdateAppRequest{JwksUri: proto.String("https://a/j")}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.JWKSURI)
			assert.Equal(t, "https://a/j", *o.JWKSURI)
		}},
		{"jwks", &iampb.UpdateAppRequest{Jwks: proto.String(`{"keys":[]}`)}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.JWKS)
			assert.Equal(t, `{"keys":[]}`, *o.JWKS)
		}},
		{"access_token_ttl", &iampb.UpdateAppRequest{AccessTokenTtl: durationpb.New(time.Hour)}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.AccessTokenTTL)
			assert.Equal(t, time.Hour, *o.AccessTokenTTL)
		}},
		{"refresh_token_ttl", &iampb.UpdateAppRequest{RefreshTokenTtl: durationpb.New(24 * time.Hour)}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.RefreshTokenTTL)
			assert.Equal(t, 24*time.Hour, *o.RefreshTokenTTL)
		}},
		{"id_token_ttl", &iampb.UpdateAppRequest{IdTokenTtl: durationpb.New(time.Minute)}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.IDTokenTTL)
			assert.Equal(t, time.Minute, *o.IDTokenTTL)
		}},
		{"metadata", &iampb.UpdateAppRequest{Metadata: map[string]string{"k": "v"}}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.Metadata)
			assert.Equal(t, map[string]string{"k": "v"}, *o.Metadata)
		}},
		{"enabled false", &iampb.UpdateAppRequest{Enabled: proto.Bool(false)}, func(t *testing.T, o iam.UpdateAppOptions) {
			require.NotNil(t, o.Enabled, "an explicit false must stay present")
			assert.False(t, *o.Enabled)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, updateAppOptionsFromProto(tt.req))
		})
	}
}

// TestUpdateAppOptionsFromProto_EmptyRequestSetsNothing guards against an
// unset proto field turning into an update that clears stored data.
func TestUpdateAppOptionsFromProto_EmptyRequestSetsNothing(t *testing.T) {
	opts := reflect.ValueOf(updateAppOptionsFromProto(&iampb.UpdateAppRequest{TenantId: "t1", ClientId: "c1"}))
	for i := 0; i < opts.NumField(); i++ {
		assert.True(t, opts.Field(i).IsNil(), "option %s set from an empty request", opts.Type().Field(i).Name)
	}
}

func TestIAMServer_UpdateApp_RefusedFieldIsInvalidArgument(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateApp", mock.Anything, "t1", "c1", mock.AnythingOfType("iam.UpdateAppOptions")).
		Return(nil, &iam.AppFieldNotStoredError{Fields: []string{"logo_uri"}})

	_, err := srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: "c1", LogoUri: proto.String("https://a/l"),
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "got %v", err)
}

// TestIAMServer_UpdateApp_UnsupportedGrantIsInvalidArgument feeds the server
// the error the app manager really returns for a "password" grant and proves
// it reaches the caller as InvalidArgument, not Internal.
func TestIAMServer_UpdateApp_UnsupportedGrantIsInvalidArgument(t *testing.T) {
	_, grantErr := iam.NewAppManager(iam.NewMemoryStorage(), nil).RegisterApp(context.Background(), "t1",
		iam.RegisterAppOptions{Name: "svc", GrantTypes: []string{"password"}})
	require.Error(t, grantErr)

	srv, svc := newTestIAMServer(t)
	svc.On("UpdateApp", mock.Anything, "t1", "c1", mock.AnythingOfType("iam.UpdateAppOptions")).
		Return(nil, grantErr)

	_, err := srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: "c1", GrantTypes: []string{"password"},
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "got %v", err)
}

// TestIAMServer_UpdateApp_InvalidAuthMethodIsInvalidArgument proves the
// manager's typed refusal of an unknown method reaches the caller as
// InvalidArgument.
func TestIAMServer_UpdateApp_InvalidAuthMethodIsInvalidArgument(t *testing.T) {
	srv, svc := newTestIAMServer(t)
	svc.On("UpdateApp", mock.Anything, "t1", "c1", mock.AnythingOfType("iam.UpdateAppOptions")).
		Return(nil, iam.ErrInvalidTokenEndpointAuthMethod)

	_, err := srv.UpdateApp(callerContext("t1"), &iampb.UpdateAppRequest{
		TenantId: "t1", ClientId: "c1", TokenEndpointAuthMethod: proto.String("client_secret_magic"),
	})

	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "got %v", err)
}

// TestIAMServer_RegisterApp_TokenEndpointAuthMethod proves RegisterApp stores
// the method through the adapter and a real app manager: client_secret_post
// reads back through GetApp, an omitted method reads back as
// client_secret_basic, and an unsupported one is InvalidArgument (#1473).
func TestIAMServer_RegisterApp_TokenEndpointAuthMethod(t *testing.T) {
	t.Parallel()
	srv := newRedirectURITestServer(t)
	ctx := callerContext("t1")
	register := func(method string) (*iampb.Application, error) {
		return srv.RegisterApp(ctx, &iampb.RegisterAppRequest{
			TenantId: "t1", Name: "MyApp", GrantTypes: []string{iam.GrantTypeClientCredentials},
			TokenEndpointAuthMethod: method,
		})
	}

	for method, want := range map[string]string{
		iam.AuthMethodClientSecretPost: iam.AuthMethodClientSecretPost,
		"":                             iam.AuthMethodClientSecretBasic,
	} {
		created, err := register(method)
		require.NoError(t, err)
		assert.Equal(t, want, created.TokenEndpointAuthMethod, "register reply for %q", method)
		got, err := srv.GetApp(ctx, &iampb.GetAppRequest{TenantId: "t1", ClientId: created.ClientId})
		require.NoError(t, err)
		assert.Equal(t, want, got.TokenEndpointAuthMethod, "GetApp after register with %q", method)
	}

	_, err := register("client_secret_magic")
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err), "got %v", err)
}
