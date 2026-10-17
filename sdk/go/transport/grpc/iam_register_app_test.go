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
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// registeringAppClient records the RegisterAppRequest it receives and answers
// GetApp with the method the test sets, as the server's Application reply.
type registeringAppClient struct {
	iampb.IAMServiceClient
	registered *iampb.RegisterAppRequest
	stored     string
	err        error
}

func (c *registeringAppClient) RegisterApp(_ context.Context, in *iampb.RegisterAppRequest, _ ...grpc.CallOption) (*iampb.Application, error) {
	if c.err != nil {
		return nil, c.err
	}
	c.registered = in
	return &iampb.Application{ClientId: "c1", TenantId: in.TenantId, TokenEndpointAuthMethod: c.stored}, nil
}

func (c *registeringAppClient) GetApp(_ context.Context, in *iampb.GetAppRequest, _ ...grpc.CallOption) (*iampb.Application, error) {
	return &iampb.Application{ClientId: in.ClientId, TenantId: in.TenantId, TokenEndpointAuthMethod: c.stored}, nil
}

func registerAppEnvelope(app *iam.RegisterAppRequest) map[string]interface{} {
	return map[string]interface{}{"tenant_id": "t1", "app": app}
}

// TestIAMHandler_RegisterApp_TokenEndpointAuthMethod proves the gRPC and Unix
// codec sends the registered method on the wire and maps the server's
// Application reply back onto the SDK enum, an empty reply being
// client_secret_basic (#1473).
func TestIAMHandler_RegisterApp_TokenEndpointAuthMethod(t *testing.T) {
	tests := []struct {
		name     string
		method   iam.TokenEndpointAuthMethod
		wantWire string
		stored   string
		want     iam.TokenEndpointAuthMethod
	}{
		{"client_secret_post", iam.TokenEndpointAuthClientSecretPost, "client_secret_post", "client_secret_post", iam.TokenEndpointAuthClientSecretPost},
		{"zero value is basic", iam.TokenEndpointAuthClientSecretBasic, "client_secret_basic", "client_secret_basic", iam.TokenEndpointAuthClientSecretBasic},
		{"tls_client_auth", iam.TokenEndpointAuthTLSClientAuth, "tls_client_auth", "tls_client_auth", iam.TokenEndpointAuthTLSClientAuth},
		{"empty reply is basic", iam.TokenEndpointAuthClientSecretBasic, "client_secret_basic", "", iam.TokenEndpointAuthClientSecretBasic},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &registeringAppClient{stored: tt.stored}
			h := &iamHandler{}
			created, err := h.handleRegisterApp(context.Background(), client, registerAppEnvelope(&iam.RegisterAppRequest{
				Name: "app", TokenEndpointAuthMethod: tt.method,
			}))
			if err != nil {
				t.Fatalf("handleRegisterApp() = %v", err)
			}
			if got := client.registered.GetTokenEndpointAuthMethod(); got != tt.wantWire {
				t.Fatalf("wire method = %q, want %q", got, tt.wantWire)
			}
			if got := created.(*iam.Application).TokenEndpointAuthMethod; got != tt.want {
				t.Fatalf("register reply method = %v, want %v", got, tt.want)
			}
			fetched, err := h.handleGetApp(context.Background(), client, map[string]interface{}{"tenant_id": "t1", "client_id": "c1"})
			if err != nil {
				t.Fatalf("handleGetApp() = %v", err)
			}
			if got := fetched.(*iam.Application).TokenEndpointAuthMethod; got != tt.want {
				t.Fatalf("GetApp method = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestIAMHandler_RegisterApp_RefusedMethodIsInvalidInput proves the server's
// InvalidArgument refusal of an unsupported method reaches the caller as
// INVALID_INPUT.
func TestIAMHandler_RegisterApp_RefusedMethodIsInvalidInput(t *testing.T) {
	client := &registeringAppClient{err: status.Error(codes.InvalidArgument, "invalid token endpoint auth method")}
	_, err := (&iamHandler{}).handleRegisterApp(context.Background(), client, registerAppEnvelope(&iam.RegisterAppRequest{
		Name: "app", TokenEndpointAuthMethod: iam.TokenEndpointAuthPrivateKeyJWT,
	}))
	if !errors.IsInvalidInput(err) {
		t.Fatalf("handleRegisterApp() = %v, want INVALID_INPUT", err)
	}
}
