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

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingAppClient records the UpdateApp request that reaches the wire.
type recordingAppClient struct {
	iampb.IAMServiceClient
	got *iampb.UpdateAppRequest
	err error
}

func (c *recordingAppClient) UpdateApp(_ context.Context, in *iampb.UpdateAppRequest, _ ...grpc.CallOption) (*iampb.Application, error) {
	c.got = in
	if c.err != nil {
		return nil, c.err
	}
	return &iampb.Application{ClientId: in.ClientId, TenantId: in.TenantId}, nil
}

// updateAppEnvelope is what the SDK app client hands the transport.
func updateAppEnvelope(app *iam.UpdateAppRequest) map[string]interface{} {
	return map[string]interface{}{"tenant_id": "t1", "client_id": "c1", "app": app}
}

func TestIAMHandler_UpdateApp_CarriesEveryField(t *testing.T) {
	enabled := false
	post := iam.TokenEndpointAuthClientSecretPost
	basic := iam.TokenEndpointAuthClientSecretBasic
	tests := []struct {
		name  string
		app   iam.UpdateAppRequest
		check func(*iampb.UpdateAppRequest) bool
	}{
		{"name", iam.UpdateAppRequest{Name: "n"}, func(r *iampb.UpdateAppRequest) bool { return r.Name == "n" }},
		{"description", iam.UpdateAppRequest{Description: "d"}, func(r *iampb.UpdateAppRequest) bool { return r.GetDescription() == "d" }},
		{"redirect_uris", iam.UpdateAppRequest{RedirectURIs: []string{"https://a/cb"}}, func(r *iampb.UpdateAppRequest) bool {
			return reflect.DeepEqual(r.RedirectUris, []string{"https://a/cb"})
		}},
		{"resource_prefix", iam.UpdateAppRequest{ResourcePrefix: "p"}, func(r *iampb.UpdateAppRequest) bool { return r.ResourcePrefix == "p" }},
		{"post_logout_redirect_uris", iam.UpdateAppRequest{PostLogoutRedirectURIs: []string{"https://a/out"}}, func(r *iampb.UpdateAppRequest) bool {
			return reflect.DeepEqual(r.PostLogoutRedirectUris, []string{"https://a/out"})
		}},
		{"grant_types", iam.UpdateAppRequest{GrantTypes: []iam.GrantType{iam.GrantTypeClientCredentials}}, func(r *iampb.UpdateAppRequest) bool {
			return reflect.DeepEqual(r.GrantTypes, []string{"client_credentials"})
		}},
		{"response_types", iam.UpdateAppRequest{ResponseTypes: []string{"code"}}, func(r *iampb.UpdateAppRequest) bool { return reflect.DeepEqual(r.ResponseTypes, []string{"code"}) }},
		{"scopes", iam.UpdateAppRequest{Scopes: []string{"kv:read"}}, func(r *iampb.UpdateAppRequest) bool { return reflect.DeepEqual(r.Scopes, []string{"kv:read"}) }},
		{"token_endpoint_auth_method", iam.UpdateAppRequest{TokenEndpointAuthMethod: &post}, func(r *iampb.UpdateAppRequest) bool {
			return r.GetTokenEndpointAuthMethod() == "client_secret_post"
		}},
		{"token_endpoint_auth_method client_secret_basic", iam.UpdateAppRequest{TokenEndpointAuthMethod: &basic}, func(r *iampb.UpdateAppRequest) bool {
			return r.TokenEndpointAuthMethod != nil && r.GetTokenEndpointAuthMethod() == "client_secret_basic"
		}},
		{"contacts", iam.UpdateAppRequest{Contacts: []string{"ops@example.com"}}, func(r *iampb.UpdateAppRequest) bool {
			return reflect.DeepEqual(r.Contacts, []string{"ops@example.com"})
		}},
		{"logo_uri", iam.UpdateAppRequest{LogoURI: "https://a/l"}, func(r *iampb.UpdateAppRequest) bool { return r.GetLogoUri() == "https://a/l" }},
		{"policy_uri", iam.UpdateAppRequest{PolicyURI: "https://a/p"}, func(r *iampb.UpdateAppRequest) bool { return r.GetPolicyUri() == "https://a/p" }},
		{"tos_uri", iam.UpdateAppRequest{TOSURI: "https://a/t"}, func(r *iampb.UpdateAppRequest) bool { return r.GetTosUri() == "https://a/t" }},
		{"jwks_uri", iam.UpdateAppRequest{JWKSURI: "https://a/j"}, func(r *iampb.UpdateAppRequest) bool { return r.GetJwksUri() == "https://a/j" }},
		{"jwks", iam.UpdateAppRequest{JWKS: `{"keys":[]}`}, func(r *iampb.UpdateAppRequest) bool { return r.GetJwks() == `{"keys":[]}` }},
		{"access_token_ttl", iam.UpdateAppRequest{AccessTokenTTL: time.Hour}, func(r *iampb.UpdateAppRequest) bool { return r.GetAccessTokenTtl().AsDuration() == time.Hour }},
		{"refresh_token_ttl", iam.UpdateAppRequest{RefreshTokenTTL: 30 * 24 * time.Hour}, func(r *iampb.UpdateAppRequest) bool {
			return r.GetRefreshTokenTtl().AsDuration() == 30*24*time.Hour
		}},
		{"id_token_ttl", iam.UpdateAppRequest{IDTokenTTL: time.Minute}, func(r *iampb.UpdateAppRequest) bool { return r.GetIdTokenTtl().AsDuration() == time.Minute }},
		{"metadata", iam.UpdateAppRequest{Metadata: map[string]string{"k": "v"}}, func(r *iampb.UpdateAppRequest) bool { return r.Metadata["k"] == "v" }},
		{"enabled", iam.UpdateAppRequest{Enabled: &enabled}, func(r *iampb.UpdateAppRequest) bool { return r.Enabled != nil && !*r.Enabled }},
		{"mtls_cert_thumbprint", iam.UpdateAppRequest{MTLSCertThumbprint: "abc"}, func(r *iampb.UpdateAppRequest) bool { return r.GetMtlsCertThumbprint() == "abc" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &recordingAppClient{}
			app := tt.app
			if _, err := (&iamHandler{}).handleUpdateApp(context.Background(), client, updateAppEnvelope(&app)); err != nil {
				t.Fatalf("handleUpdateApp() = %v", err)
			}
			if client.got == nil || !tt.check(client.got) {
				t.Fatalf("field %s did not reach the wire: %v", tt.name, client.got)
			}
		})
	}
}

func TestIAMHandler_UpdateApp_UnsetFieldsStayAbsent(t *testing.T) {
	client := &recordingAppClient{}
	if _, err := (&iamHandler{}).handleUpdateApp(context.Background(), client, updateAppEnvelope(&iam.UpdateAppRequest{Name: "n"})); err != nil {
		t.Fatalf("handleUpdateApp() = %v", err)
	}
	r := client.got
	if r.Description != nil || r.Enabled != nil || r.MtlsCertThumbprint != nil || r.AccessTokenTtl != nil || r.TokenEndpointAuthMethod != nil {
		t.Fatalf("unset optional fields became present: %v", r)
	}
}

func TestIAMHandler_UpdateApp_RefusedFieldIsInvalidInput(t *testing.T) {
	client := &recordingAppClient{err: status.Error(codes.InvalidArgument, "application does not store field(s): logo_uri")}
	_, err := (&iamHandler{}).handleUpdateApp(context.Background(), client, updateAppEnvelope(&iam.UpdateAppRequest{LogoURI: "https://a/l"}))
	if !errors.IsInvalidInput(err) {
		t.Fatalf("handleUpdateApp() = %v, want INVALID_INPUT", err)
	}
}
